package assignment

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"destekmasa/internal/sla"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

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

// Assignment writes are serialized for deterministic scoring and round robin at this project's scale.
// Required order: calendar shared lock -> assignment lock -> ticket/expert row locks.
func Lock(ctx context.Context, tx pgx.Tx) error {
	if err := sla.LockShared(ctx, tx); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7420183)`)
	return err
}

const expertSelect = `SELECT e.user_id,u.display_name,u.email,e.capacity,
    (SELECT count(*) FROM tickets t WHERE t.assignee_id=e.user_id AND t.status IN ('open','in_progress','waiting_customer') AND t.id<>$1),
    e.enabled,e.on_leave,e.timezone,e.shift_start_minute,e.shift_end_minute,e.weekdays_mask,e.last_assignment_seq,e.version,
    COALESCE((SELECT jsonb_agg(jsonb_build_object('code',s.skill_code,'level',s.level) ORDER BY s.skill_code) FROM expert_skills s WHERE s.expert_id=e.user_id),'[]'::jsonb)
    FROM expert_profiles e JOIN users u ON u.id=e.user_id WHERE u.role_code='specialist' ORDER BY e.user_id `

func loadExperts(ctx context.Context, tx pgx.Tx, excludeTicket int64, lock bool, now time.Time) ([]Expert, error) {
	query := expertSelect
	if lock {
		query += ` FOR UPDATE OF e`
	}
	rows, err := tx.Query(ctx, query, excludeTicket)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Expert{}
	for rows.Next() {
		var expert Expert
		var mask int
		var skills []byte
		if err = rows.Scan(&expert.ID, &expert.DisplayName, &expert.Email, &expert.Capacity, &expert.Load, &expert.Enabled, &expert.OnLeave, &expert.Timezone, &expert.ShiftStartMinute, &expert.ShiftEndMinute, &mask, &expert.LastAssignmentSeq, &expert.Version, &skills); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(skills, &expert.Skills); err != nil {
			return nil, err
		}
		expert.Weekdays = weekdays(mask)
		expert.OnShift = onShift(expert, now)
		result = append(result, expert)
	}
	return result, rows.Err()
}
func active(status string) bool {
	return status == "open" || status == "in_progress" || status == "waiting_customer"
}
func (s *Service) record(ctx context.Context, tx pgx.Tx, ticketID int64, from, to, actor *int64, reason string, score any, at time.Time) error {
	data, err := json.Marshal(score)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO assignment_history(ticket_id,from_expert_id,to_expert_id,actor_id,reason,score,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, ticketID, from, to, actor, reason, data, at); err != nil {
		return err
	}
	oldJSON, err := json.Marshal(map[string]any{"assignee_id": from})
	if err != nil {
		return err
	}
	newJSON, err := json.Marshal(map[string]any{"assignee_id": to, "reason": reason, "score": score})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_log(actor_id,action,entity_type,entity_id,old_value,new_value) VALUES($1,'ticket.assign','ticket',$2,$3,$4)`, actor, ticketID, oldJSON, newJSON)
	return err
}

// ReconcileTx is called only while Lock is held. It never changes ticket status or acknowledges an SLA response.
func (s *Service) ReconcileTx(ctx context.Context, tx pgx.Tx, id int64, actor *int64, reason string) error {
	var current *int64
	var category, status string
	err := tx.QueryRow(ctx, `SELECT assignee_id,category_code,status FROM tickets WHERE id=$1 FOR UPDATE`, id).Scan(&current, &category, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	at := s.now().UTC()
	if _, err = tx.Exec(ctx, `UPDATE tickets SET assignment_checked_at=$2 WHERE id=$1`, id, at); err != nil {
		return err
	}
	if !active(status) {
		_, err = tx.Exec(ctx, `DELETE FROM assignment_queue WHERE ticket_id=$1`, id)
		return err
	}
	rows, err := tx.Query(ctx, `SELECT skill_code FROM category_skills WHERE category_code=$1 ORDER BY skill_code`, category)
	if err != nil {
		return err
	}
	required := []string{}
	for rows.Next() {
		var code string
		if err = rows.Scan(&code); err != nil {
			rows.Close()
			return err
		}
		required = append(required, code)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	experts, err := loadExperts(ctx, tx, id, true, at)
	if err != nil {
		return err
	}
	ranked := Rank(experts, required, at)
	// Keep a still-eligible owner; do not churn assignments on each background tick.
	if current != nil {
		for _, score := range ranked {
			if score.ExpertID == *current {
				_, err = tx.Exec(ctx, `DELETE FROM assignment_queue WHERE ticket_id=$1`, id)
				return err
			}
		}
	}
	if len(ranked) == 0 {
		command, err := tx.Exec(ctx, `INSERT INTO assignment_queue(ticket_id,reason,queued_at,attempted_at) VALUES($1,'no_eligible_expert',$2,$2)
            ON CONFLICT(ticket_id) DO UPDATE SET attempted_at=EXCLUDED.attempted_at`, id, at)
		if err != nil {
			return err
		}
		// Only the first queued decision and real deassignments enter history.
		var previousQueueCount int
		if current == nil {
			if err = tx.QueryRow(ctx, `SELECT count(*) FROM assignment_history WHERE ticket_id=$1`, id).Scan(&previousQueueCount); err != nil {
				return err
			}
		}
		if current != nil {
			if _, err = tx.Exec(ctx, `UPDATE tickets SET assignee_id=NULL,updated_at=clock_timestamp() WHERE id=$1`, id); err != nil {
				return err
			}
		}
		if current != nil || (previousQueueCount == 0 && command.RowsAffected() > 0) {
			return s.record(ctx, tx, id, current, nil, actor, reason+":queued", map[string]any{}, at)
		}
		return nil
	}
	choice := ranked[0]
	// The profile lock and the assignment transaction lock protect the load observation above.
	// Re-read immediately before writing to make the capacity invariant explicit.
	var capacity, load int
	if err = tx.QueryRow(ctx, `SELECT capacity,(SELECT count(*) FROM tickets WHERE assignee_id=$1 AND status IN ('open','in_progress','waiting_customer') AND id<>$2)
        FROM expert_profiles WHERE user_id=$1 FOR UPDATE`, choice.ExpertID, id).Scan(&capacity, &load); err != nil {
		return err
	}
	if load >= capacity {
		return ErrConflict
	}
	var sequence int64
	if err = tx.QueryRow(ctx, `UPDATE assignment_rotation SET last_seq=last_seq+1 WHERE id=1 RETURNING last_seq`).Scan(&sequence); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE expert_profiles SET last_assignment_seq=$2 WHERE user_id=$1`, choice.ExpertID, sequence); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE tickets SET assignee_id=$2,updated_at=clock_timestamp() WHERE id=$1`, id, choice.ExpertID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM assignment_queue WHERE ticket_id=$1`, id); err != nil {
		return err
	}
	target := choice.ExpertID
	return s.record(ctx, tx, id, current, &target, actor, reason, choice, at)
}

// FillQueueTx fills released capacity without moving valid assignments. Lock must already be held.
func (s *Service) FillQueueTx(ctx context.Context, tx pgx.Tx, limit int, actor *int64, reason string) error {
	rows, err := tx.Query(ctx, `SELECT q.ticket_id FROM assignment_queue q JOIN tickets t ON t.id=q.ticket_id
        WHERE t.status IN ('open','in_progress','waiting_customer')
        ORDER BY q.attempted_at,CASE t.priority WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'normal' THEN 2 ELSE 3 END,q.queued_at,q.ticket_id LIMIT $1`, limit)
	if err != nil {
		return err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err = s.ReconcileTx(ctx, tx, id, actor, reason); err != nil {
			return err
		}
	}
	return nil
}

type Board struct {
	Experts []Expert    `json:"experts"`
	Skills  []SkillInfo `json:"skills"`
	Queued  int         `json:"queued"`
}

func (s *Service) Board(ctx context.Context) (Board, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Board{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	experts, err := loadExperts(ctx, tx, 0, false, s.now())
	if err != nil {
		return Board{}, err
	}
	rows, err := tx.Query(ctx, `SELECT code,label FROM skills ORDER BY label`)
	if err != nil {
		return Board{}, err
	}
	skills := []SkillInfo{}
	for rows.Next() {
		var skill SkillInfo
		if err = rows.Scan(&skill.Code, &skill.Label); err != nil {
			rows.Close()
			return Board{}, err
		}
		skills = append(skills, skill)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return Board{}, err
	}
	var queued int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM assignment_queue`).Scan(&queued); err != nil {
		return Board{}, err
	}
	return Board{Experts: experts, Skills: skills, Queued: queued}, tx.Commit(ctx)
}
func (s *Service) UpdateExpert(ctx context.Context, id, actor int64, input ProfileInput) (Board, error) {
	if err := validate(input); err != nil {
		return Board{}, err
	}
	mask, _ := weekdayMask(input.Weekdays)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Board{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = Lock(ctx, tx); err != nil {
		return Board{}, err
	}
	var version int64
	var oldLeave bool
	err = tx.QueryRow(ctx, `SELECT e.version,e.on_leave FROM expert_profiles e JOIN users u ON u.id=e.user_id WHERE e.user_id=$1 AND u.role_code='specialist' FOR UPDATE OF e`, id).Scan(&version, &oldLeave)
	if errors.Is(err, pgx.ErrNoRows) {
		return Board{}, ErrNotFound
	}
	if err != nil {
		return Board{}, err
	}
	if input.Version != version {
		return Board{}, ErrConflict
	}
	before, err := loadExperts(ctx, tx, 0, false, s.now())
	if err != nil {
		return Board{}, err
	}
	var previous Expert
	for _, expert := range before {
		if expert.ID == id {
			previous = expert
			break
		}
	}
	for _, skill := range input.Skills {
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM skills WHERE code=$1)`, skill.Code).Scan(&exists); err != nil {
			return Board{}, err
		}
		if !exists {
			return Board{}, ErrInvalid
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE expert_profiles SET capacity=$2,enabled=$3,on_leave=$4,timezone=$5,shift_start_minute=$6,shift_end_minute=$7,weekdays_mask=$8,version=version+1 WHERE user_id=$1`, id, input.Capacity, input.Enabled, input.OnLeave, input.Timezone, input.ShiftStartMinute, input.ShiftEndMinute, mask); err != nil {
		return Board{}, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM expert_skills WHERE expert_id=$1`, id); err != nil {
		return Board{}, err
	}
	for _, skill := range input.Skills {
		if _, err = tx.Exec(ctx, `INSERT INTO expert_skills(expert_id,skill_code,level) VALUES($1,$2,$3)`, id, skill.Code, skill.Level); err != nil {
			return Board{}, err
		}
	}
	rows, err := tx.Query(ctx, `SELECT id FROM tickets WHERE assignee_id=$1 AND status IN ('open','in_progress','waiting_customer') ORDER BY id FOR UPDATE`, id)
	if err != nil {
		return Board{}, err
	}
	ids := []int64{}
	for rows.Next() {
		var ticketID int64
		if err = rows.Scan(&ticketID); err != nil {
			rows.Close()
			return Board{}, err
		}
		ids = append(ids, ticketID)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return Board{}, err
	}
	reason := "expert_update"
	if !oldLeave && input.OnLeave {
		reason = "expert_leave"
	}
	for _, ticketID := range ids {
		if err = s.ReconcileTx(ctx, tx, ticketID, &actor, reason); err != nil {
			return Board{}, err
		}
	}
	if err = s.FillQueueTx(ctx, tx, 100, &actor, "capacity_available"); err != nil {
		return Board{}, err
	}
	oldJSON, err := json.Marshal(previous)
	if err != nil {
		return Board{}, err
	}
	newJSON, err := json.Marshal(input)
	if err != nil {
		return Board{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(actor_id,action,entity_type,entity_id,old_value,new_value) VALUES($1,'expert.update','expert',$2,$3,$4)`, actor, id, oldJSON, newJSON); err != nil {
		return Board{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Board{}, err
	}
	return s.Board(ctx)
}
func (s *Service) Retry(ctx context.Context, id, actor int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = Lock(ctx, tx); err != nil {
		return err
	}
	if err = s.ReconcileTx(ctx, tx, id, &actor, "manual_retry"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Service) Sweep(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 1000 {
		return 0, ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = Lock(ctx, tx); err != nil {
		return 0, err
	}
	rows, err := tx.Query(ctx, `SELECT id FROM tickets WHERE status IN ('open','in_progress','waiting_customer') ORDER BY assignment_checked_at,id LIMIT $1 FOR UPDATE`, limit)
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
	for _, id := range ids {
		if err = s.ReconcileTx(ctx, tx, id, nil, "availability_check"); err != nil {
			return 0, err
		}
	}
	return len(ids), tx.Commit(ctx)
}

type History struct {
	ID        int64           `json:"id"`
	FromID    *int64          `json:"from_expert_id"`
	ToID      *int64          `json:"to_expert_id"`
	FromName  string          `json:"from_name"`
	ToName    string          `json:"to_name"`
	ActorName string          `json:"actor_name"`
	Reason    string          `json:"reason"`
	Score     json.RawMessage `json:"score"`
	CreatedAt time.Time       `json:"created_at"`
}

func (s *Service) History(ctx context.Context, id, before int64, limit int) ([]History, error) {
	if id < 1 || before < 0 || limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tickets WHERE id=$1)`, id).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `SELECT h.id,h.from_expert_id,h.to_expert_id,COALESCE(f.display_name,''),COALESCE(t.display_name,''),COALESCE(a.display_name,'Sistem'),h.reason,h.score,h.created_at
        FROM assignment_history h LEFT JOIN users f ON f.id=h.from_expert_id LEFT JOIN users t ON t.id=h.to_expert_id LEFT JOIN users a ON a.id=h.actor_id
        WHERE h.ticket_id=$1 AND ($2::bigint=0 OR h.id<$2) ORDER BY h.id DESC LIMIT $3`, id, before, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []History{}
	for rows.Next() {
		var item History
		if err = rows.Scan(&item.ID, &item.FromID, &item.ToID, &item.FromName, &item.ToName, &item.ActorName, &item.Reason, &item.Score, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
