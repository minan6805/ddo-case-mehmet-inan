package sla

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("SLA ticket not found")
var ErrForbidden = errors.New("SLA access forbidden")

type Service struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func New(pool *pgxpool.Pool, clock func() time.Time) *Service {
	if clock == nil {
		clock = time.Now
	}
	return &Service{pool: pool, now: clock}
}

// All code paths take the calendar lock before ticket row locks to avoid lock inversion.
func LockShared(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(7420182)`)
	return err
}
func loadCalendar(ctx context.Context, tx pgx.Tx) (Calendar, error) {
	var calendar Calendar
	err := tx.QueryRow(ctx, `SELECT timezone,start_minute,end_minute,version FROM sla_calendar WHERE id=1`).Scan(&calendar.Timezone, &calendar.StartMinute, &calendar.EndMinute, &calendar.Version)
	if err != nil {
		return calendar, err
	}
	rows, err := tx.Query(ctx, `SELECT to_char(day,'YYYY-MM-DD'),closed_from_minute,label FROM sla_holidays ORDER BY day`)
	if err != nil {
		return calendar, err
	}
	defer rows.Close()
	calendar.Holidays = []Holiday{}
	for rows.Next() {
		var holiday Holiday
		if err = rows.Scan(&holiday.Date, &holiday.ClosedFromMinute, &holiday.Label); err != nil {
			return calendar, err
		}
		calendar.Holidays = append(calendar.Holidays, holiday)
	}
	return calendar, rows.Err()
}

type Policy struct {
	Category          string `json:"category"`
	Priority          string `json:"priority"`
	ResponseMinutes   int    `json:"response_minutes"`
	ResolutionMinutes int    `json:"resolution_minutes"`
	Version           int64  `json:"version"`
}
type Settings struct {
	Calendar Calendar `json:"calendar"`
	Policies []Policy `json:"policies"`
}

func (s *Service) Settings(ctx context.Context) (Settings, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Settings{}, err
	}
	defer tx.Rollback(context.Background())
	if err = LockShared(ctx, tx); err != nil {
		return Settings{}, err
	}
	calendar, err := loadCalendar(ctx, tx)
	if err != nil {
		return Settings{}, err
	}
	rows, err := tx.Query(ctx, `SELECT category_code,priority,response_minutes,resolution_minutes,version FROM sla_policies ORDER BY category_code,priority`)
	if err != nil {
		return Settings{}, err
	}
	policies := []Policy{}
	for rows.Next() {
		var policy Policy
		if err = rows.Scan(&policy.Category, &policy.Priority, &policy.ResponseMinutes, &policy.ResolutionMinutes, &policy.Version); err != nil {
			rows.Close()
			return Settings{}, err
		}
		policies = append(policies, policy)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return Settings{}, err
	}
	return Settings{Calendar: calendar, Policies: policies}, tx.Commit(ctx)
}

// Initialize and RecordStatus are called inside the ticket transaction.
func (s *Service) Initialize(ctx context.Context, tx pgx.Tx, id int64) error {
	if err := LockShared(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ticket_status_history(ticket_id,status,occurred_at) SELECT id,'open',created_at FROM tickets WHERE id=$1`, id); err != nil {
		return err
	}
	command, err := tx.Exec(ctx, `INSERT INTO ticket_sla(ticket_id,response_budget_seconds,resolution_budget_seconds,policy_version,calendar_version)
        SELECT t.id,p.response_minutes*60,p.resolution_minutes*60,p.version,c.version
        FROM tickets t JOIN sla_policies p ON p.category_code=t.category_code AND p.priority=t.priority CROSS JOIN sla_calendar c WHERE t.id=$1`, id)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return ErrInvalid
	}
	calendar, err := loadCalendar(ctx, tx)
	if err != nil {
		return err
	}
	_, err = s.recompute(ctx, tx, calendar, id, s.now().UTC(), true)
	return err
}
func (s *Service) RecordStatus(ctx context.Context, tx pgx.Tx, id int64, status string) error {
	at := s.now().UTC()
	if _, err := tx.Exec(ctx, `INSERT INTO ticket_status_history(ticket_id,status,occurred_at) VALUES($1,$2,$3)`, id, status, at); err != nil {
		return err
	}
	calendar, err := loadCalendar(ctx, tx)
	if err != nil {
		return err
	}
	_, err = s.recompute(ctx, tx, calendar, id, at, true)
	return err
}
func (s *Service) recompute(ctx context.Context, tx pgx.Tx, calendar Calendar, id int64, asOf time.Time, persist bool) (Snapshot, error) {
	var responseBudget, resolutionBudget int64
	var escalation int
	err := tx.QueryRow(ctx, `SELECT s.response_budget_seconds,s.resolution_budget_seconds,t.escalation_level
        FROM ticket_sla s JOIN tickets t ON t.id=s.ticket_id WHERE s.ticket_id=$1`, id).Scan(&responseBudget, &resolutionBudget, &escalation)
	if errors.Is(err, pgx.ErrNoRows) {
		return Snapshot{}, ErrNotFound
	}
	if err != nil {
		return Snapshot{}, err
	}
	rows, err := tx.Query(ctx, `SELECT status,occurred_at FROM ticket_status_history WHERE ticket_id=$1 ORDER BY occurred_at,id`, id)
	if err != nil {
		return Snapshot{}, err
	}
	history := []StatusEvent{}
	for rows.Next() {
		var event StatusEvent
		if err = rows.Scan(&event.Status, &event.At); err != nil {
			rows.Close()
			return Snapshot{}, err
		}
		history = append(history, event)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return Snapshot{}, err
	}
	snapshot, err := Evaluate(calendar, history, asOf, responseBudget, resolutionBudget)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.EscalationLevel = escalation
	if !persist {
		return snapshot, nil
	}
	if _, err = tx.Exec(ctx, `UPDATE ticket_sla SET calendar_version=$2,response_elapsed_seconds=$3,resolution_elapsed_seconds=$4,
        response_due_at=$5,resolution_due_at=$6,first_response_at=$7,completed_at=$8,paused=$9,checked_at=$10 WHERE ticket_id=$1`,
		id, calendar.Version, snapshot.Response.ElapsedSeconds, snapshot.Resolution.ElapsedSeconds, snapshot.Response.DueAt, snapshot.Resolution.DueAt,
		snapshot.Response.CompletedAt, snapshot.Resolution.CompletedAt, snapshot.Paused, asOf); err != nil {
		return Snapshot{}, err
	}
	for _, entry := range []struct {
		name  string
		timer Timer
	}{{"response", snapshot.Response}, {"resolution", snapshot.Resolution}} {
		timer := entry.timer
		stages := []string{}
		if timer.ElapsedSeconds*100 >= timer.BudgetSeconds*80 {
			stages = append(stages, "warning")
		}
		if timer.ElapsedSeconds >= timer.BudgetSeconds {
			stages = append(stages, "breach", "escalation")
		}
		for _, stage := range stages {
			command, err := tx.Exec(ctx, `INSERT INTO sla_events(ticket_id,timer,stage,elapsed_seconds,budget_seconds,calendar_version,created_at)
                VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(ticket_id,timer,stage) DO NOTHING`, id, entry.name, stage, timer.ElapsedSeconds, timer.BudgetSeconds, calendar.Version, asOf)
			if err != nil {
				return Snapshot{}, err
			}
			if stage == "escalation" && command.RowsAffected() == 1 {
				if _, err = tx.Exec(ctx, `UPDATE tickets SET escalation_level=GREATEST(escalation_level,1) WHERE id=$1`, id); err != nil {
					return Snapshot{}, err
				}
				if _, err = tx.Exec(ctx, `INSERT INTO audit_log(action,entity_type,entity_id,old_value,new_value)
                    VALUES('sla.escalate','ticket',$1,jsonb_build_object('escalation_level',$2::integer),'{"escalation_level":1}'::jsonb)`, id, snapshot.EscalationLevel); err != nil {
					return Snapshot{}, err
				}
				snapshot.EscalationLevel = 1
			}
		}
	}
	return snapshot, nil
}

// Ticket evaluates on read without side effects; worker/status transactions persist alerts and projections.
func (s *Service) Ticket(ctx context.Context, userID int64, staff bool, id int64) (Snapshot, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Snapshot{}, err
	}
	defer tx.Rollback(context.Background())
	if err = LockShared(ctx, tx); err != nil {
		return Snapshot{}, err
	}
	var owner int64
	err = tx.QueryRow(ctx, `SELECT requester_id FROM tickets WHERE id=$1`, id).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return Snapshot{}, ErrNotFound
	}
	if err != nil {
		return Snapshot{}, err
	}
	if !staff && userID != owner {
		return Snapshot{}, ErrForbidden
	}
	calendar, err := loadCalendar(ctx, tx)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot, err := s.recompute(ctx, tx, calendar, id, s.now().UTC(), false)
	if err != nil {
		return Snapshot{}, err
	}
	return snapshot, tx.Commit(ctx)
}

// Sweep skips rows held by ticket writers and handles at most one bounded batch per invocation.
func (s *Service) Sweep(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 1000 {
		return 0, ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(context.Background())
	if err = LockShared(ctx, tx); err != nil {
		return 0, err
	}
	calendar, err := loadCalendar(ctx, tx)
	if err != nil {
		return 0, err
	}
	rows, err := tx.Query(ctx, `SELECT t.id FROM tickets t JOIN ticket_sla s ON s.ticket_id=t.id
        WHERE t.status NOT IN ('resolved','closed') OR s.calendar_version<>$2
        ORDER BY s.checked_at,t.id LIMIT $1 FOR UPDATE OF t SKIP LOCKED`, limit, calendar.Version)
	if err != nil {
		return 0, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return 0, err
	}
	now := s.now().UTC()
	for _, id := range ids {
		if _, err = s.recompute(ctx, tx, calendar, id, now, true); err != nil {
			return 0, err
		}
	}
	return len(ids), tx.Commit(ctx)
}

func (s *Service) UpdateCalendar(ctx context.Context, actorID int64, input Calendar) (Calendar, error) {
	if _, err := input.compile(); err != nil {
		return Calendar{}, ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Calendar{}, err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7420182)`); err != nil {
		return Calendar{}, err
	}
	previous, err := loadCalendar(ctx, tx)
	if err != nil {
		return Calendar{}, err
	}
	if previous.Version != input.Version {
		return Calendar{}, ErrConflict
	}
	if _, err = tx.Exec(ctx, `UPDATE sla_calendar SET timezone=$1,start_minute=$2,end_minute=$3,version=version+1 WHERE id=1`, input.Timezone, input.StartMinute, input.EndMinute); err != nil {
		return Calendar{}, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM sla_holidays`); err != nil {
		return Calendar{}, err
	}
	for _, holiday := range input.Holidays {
		if _, err = tx.Exec(ctx, `INSERT INTO sla_holidays(day,closed_from_minute,label) VALUES($1,$2,$3)`, holiday.Date, holiday.ClosedFromMinute, holiday.Label); err != nil {
			return Calendar{}, err
		}
	}
	input.Version++
	rows, err := tx.Query(ctx, `SELECT id FROM tickets ORDER BY id FOR UPDATE`)
	if err != nil {
		return Calendar{}, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return Calendar{}, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return Calendar{}, err
	}
	now := s.now().UTC()
	for _, id := range ids {
		if _, err = s.recompute(ctx, tx, input, id, now, true); err != nil {
			return Calendar{}, err
		}
	}
	oldJSON, err := json.Marshal(previous)
	if err != nil {
		return Calendar{}, err
	}
	newJSON, err := json.Marshal(input)
	if err != nil {
		return Calendar{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(actor_id,action,entity_type,entity_id,old_value,new_value)
        VALUES($1,'sla.calendar','sla_calendar',1,$2,$3)`, actorID, oldJSON, newJSON); err != nil {
		return Calendar{}, err
	}
	return input, tx.Commit(ctx)
}
func (s *Service) UpdatePolicy(ctx context.Context, actorID int64, input Policy) (Policy, error) {
	if input.Version < 1 || input.ResponseMinutes < 1 || input.ResponseMinutes > 43200 || input.ResolutionMinutes < 1 || input.ResolutionMinutes > 43200 {
		return Policy{}, ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Policy{}, err
	}
	defer tx.Rollback(context.Background())
	// Serialize settings writes with calendar edits and ticket policy snapshots.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7420182)`); err != nil {
		return Policy{}, err
	}
	var previous Policy
	err = tx.QueryRow(ctx, `SELECT category_code,priority,response_minutes,resolution_minutes,version FROM sla_policies
        WHERE category_code=$1 AND priority=$2 FOR UPDATE`, input.Category, input.Priority).Scan(&previous.Category, &previous.Priority, &previous.ResponseMinutes, &previous.ResolutionMinutes, &previous.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return Policy{}, ErrInvalid
	}
	if err != nil {
		return Policy{}, err
	}
	if previous.Version != input.Version {
		return Policy{}, ErrConflict
	}
	input.Version++
	if _, err = tx.Exec(ctx, `UPDATE sla_policies SET response_minutes=$3,resolution_minutes=$4,version=$5 WHERE category_code=$1 AND priority=$2`, input.Category, input.Priority, input.ResponseMinutes, input.ResolutionMinutes, input.Version); err != nil {
		return Policy{}, err
	}
	oldJSON, err := json.Marshal(previous)
	if err != nil {
		return Policy{}, err
	}
	newJSON, err := json.Marshal(input)
	if err != nil {
		return Policy{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(actor_id,action,entity_type,entity_id,old_value,new_value)
        VALUES($1,'sla.policy','sla_calendar',1,$2,$3)`, actorID, oldJSON, newJSON); err != nil {
		return Policy{}, err
	}
	return input, tx.Commit(ctx)
}

type Alert struct {
	ID            int64     `json:"id"`
	TicketID      int64     `json:"ticket_id"`
	Title         string    `json:"title"`
	Timer         string    `json:"timer"`
	Stage         string    `json:"stage"`
	RecipientRole string    `json:"recipient_role"`
	CreatedAt     time.Time `json:"created_at"`
}

func (s *Service) Alerts(ctx context.Context, before int64, limit int) ([]Alert, error) {
	if before < 0 || limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	rows, err := s.pool.Query(ctx, `SELECT e.id,e.ticket_id,t.title,e.timer,e.stage,e.recipient_role,e.created_at
        FROM sla_events e JOIN tickets t ON t.id=e.ticket_id WHERE ($1::bigint=0 OR e.id<$1) ORDER BY e.id DESC LIMIT $2`, before, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Alert{}
	for rows.Next() {
		var alert Alert
		if err = rows.Scan(&alert.ID, &alert.TicketID, &alert.Title, &alert.Timer, &alert.Stage, &alert.RecipientRole, &alert.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, alert)
	}
	return result, rows.Err()
}
