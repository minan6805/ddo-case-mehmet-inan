// Package reports exposes read-only, consistently snapshotted operational summaries.
package reports

import (
	"context"
	"errors"
	"strings"
	"time"

	"destekmasa/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInvalid   = errors.New("invalid report date range or category")
	ErrForbidden = errors.New("reports require leadership role")
	ErrLimit     = errors.New("too many expert profiles for report")
)

const Timezone = "Europe/Istanbul"
const MaxExperts = 500

func CanRead(role string) bool { return role == "team_lead" || role == "admin" }

type Filter struct{ From, To, Category string }
type Range struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Category   string `json:"category"`
	Timezone   string `json:"timezone"`
	start, end time.Time
}

func normalize(f Filter, now time.Time) (Range, error) {
	loc, err := time.LoadLocation(Timezone)
	if err != nil {
		return Range{}, err
	}
	if f.From == "" && f.To == "" {
		f.To = now.In(loc).Format(time.DateOnly)
		f.From = now.In(loc).AddDate(0, 0, -29).Format(time.DateOnly)
	}
	start, err := time.ParseInLocation(time.DateOnly, f.From, loc)
	if err != nil {
		return Range{}, ErrInvalid
	}
	last, err := time.ParseInLocation(time.DateOnly, f.To, loc)
	if err != nil {
		return Range{}, ErrInvalid
	}
	if start.Format(time.DateOnly) != f.From || last.Format(time.DateOnly) != f.To || last.Before(start) || !last.Before(start.AddDate(0, 0, 366)) || len(f.Category) > 64 || strings.ContainsRune(f.Category, 0) {
		return Range{}, ErrInvalid
	}
	return Range{From: f.From, To: f.To, Category: f.Category, Timezone: Timezone, start: start, end: last.AddDate(0, 0, 1)}, nil
}

type Bucket struct {
	Code  string `json:"code"`
	Label string `json:"label"`
	Count int64  `json:"count"`
}
type Day struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}
type Summary struct {
	Total            int64 `json:"total"`
	Active           int64 `json:"active"`
	UnassignedActive int64 `json:"unassigned_active"`
	Resolved         int64 `json:"resolved"`
	Closed           int64 `json:"closed"`
}
type SLA struct {
	Tracked                  int64      `json:"tracked"`
	Missing                  int64      `json:"missing"`
	Responded                int64      `json:"responded"`
	RespondedWithinBudget    int64      `json:"responded_within_budget"`
	Completed                int64      `json:"completed"`
	CompletedWithinBudget    int64      `json:"completed_within_budget"`
	AverageResponseMinutes   *float64   `json:"average_response_minutes"`
	AverageResolutionMinutes *float64   `json:"average_resolution_minutes"`
	WarningTickets           int64      `json:"warning_tickets"`
	ResponseBreachTickets    int64      `json:"response_breach_tickets"`
	ResolutionBreachTickets  int64      `json:"resolution_breach_tickets"`
	EscalatedTickets         int64      `json:"escalated_tickets"`
	OldestCheckedAt          *time.Time `json:"oldest_checked_at"`
	LatestCheckedAt          *time.Time `json:"latest_checked_at"`
}
type Expert struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Capacity int    `json:"capacity"`
	Active   int64  `json:"active"`
	Enabled  bool   `json:"enabled"`
	OnLeave  bool   `json:"on_leave"`
}
type Workload struct {
	TotalActive      int64    `json:"total_active"`
	UnassignedActive int64    `json:"unassigned_active"`
	Experts          []Expert `json:"experts"`
}
type Report struct {
	Range      Range     `json:"range"`
	SnapshotAt time.Time `json:"snapshot_at"`
	Summary    Summary   `json:"summary"`
	SLA        SLA       `json:"sla"`
	Statuses   []Bucket  `json:"statuses"`
	Priorities []Bucket  `json:"priorities"`
	Categories []Bucket  `json:"categories"`
	Daily      []Day     `json:"daily"`
	Workload   Workload  `json:"workload"`
}
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

const cohort = `WITH cohort AS (SELECT t.* FROM tickets t WHERE t.created_at >= $1 AND t.created_at < $2 AND ($3::text='' OR t.category_code=$3)) `

func (s *Service) Overview(ctx context.Context, user auth.User, f Filter) (Report, error) {
	if !CanRead(user.Role) {
		return Report{}, ErrForbidden
	}
	selected, err := normalize(f, s.now())
	if err != nil {
		return Report{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Report{}, err
	}
	defer tx.Rollback(context.Background())
	result := Report{Range: selected, Statuses: []Bucket{}, Priorities: []Bucket{}, Categories: []Bucket{}, Daily: []Day{}, Workload: Workload{Experts: []Expert{}}}
	if err = tx.QueryRow(ctx, `SELECT transaction_timestamp()`).Scan(&result.SnapshotAt); err != nil {
		return Report{}, err
	}
	if f.Category != "" {
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ticket_categories WHERE code=$1)`, f.Category).Scan(&exists); err != nil {
			return Report{}, err
		}
		if !exists {
			return Report{}, ErrInvalid
		}
	}
	args := []any{selected.start, selected.end, selected.Category}
	err = tx.QueryRow(ctx, cohort+`SELECT count(*), count(*) FILTER(WHERE status IN ('open','in_progress','waiting_customer')),count(*) FILTER(WHERE assignee_id IS NULL AND status IN ('open','in_progress','waiting_customer')),count(*) FILTER(WHERE status='resolved'),count(*) FILTER(WHERE status='closed') FROM cohort`, args...).Scan(&result.Summary.Total, &result.Summary.Active, &result.Summary.UnassignedActive, &result.Summary.Resolved, &result.Summary.Closed)
	if err != nil {
		return Report{}, err
	}
	err = tx.QueryRow(ctx, cohort+`SELECT count(s.ticket_id),count(*) FILTER(WHERE s.ticket_id IS NULL),
 count(*) FILTER(WHERE s.first_response_at IS NOT NULL),count(*) FILTER(WHERE s.first_response_at IS NOT NULL AND s.response_elapsed_seconds<s.response_budget_seconds),
 count(*) FILTER(WHERE s.completed_at IS NOT NULL),count(*) FILTER(WHERE s.completed_at IS NOT NULL AND s.resolution_elapsed_seconds<s.resolution_budget_seconds),
 avg(s.response_elapsed_seconds/60.0) FILTER(WHERE s.first_response_at IS NOT NULL),avg(s.resolution_elapsed_seconds/60.0) FILTER(WHERE s.completed_at IS NOT NULL),min(s.checked_at),max(s.checked_at)
 FROM cohort t LEFT JOIN ticket_sla s ON s.ticket_id=t.id`, args...).Scan(&result.SLA.Tracked, &result.SLA.Missing, &result.SLA.Responded, &result.SLA.RespondedWithinBudget, &result.SLA.Completed, &result.SLA.CompletedWithinBudget, &result.SLA.AverageResponseMinutes, &result.SLA.AverageResolutionMinutes, &result.SLA.OldestCheckedAt, &result.SLA.LatestCheckedAt)
	if err != nil {
		return Report{}, err
	}
	// Count distinct tickets; multiple stages/timers must not multiply ticket counts.
	err = tx.QueryRow(ctx, cohort+`SELECT count(DISTINCT e.ticket_id) FILTER(WHERE e.stage='warning'), count(DISTINCT e.ticket_id) FILTER(WHERE e.stage='breach' AND e.timer='response'),count(DISTINCT e.ticket_id) FILTER(WHERE e.stage='breach' AND e.timer='resolution'),count(DISTINCT e.ticket_id) FILTER(WHERE e.stage='escalation') FROM sla_events e JOIN cohort t ON t.id=e.ticket_id`, args...).Scan(&result.SLA.WarningTickets, &result.SLA.ResponseBreachTickets, &result.SLA.ResolutionBreachTickets, &result.SLA.EscalatedTickets)
	if err != nil {
		return Report{}, err
	}
	bucketQueries := []struct {
		query string
		dest  *[]Bucket
	}{
		{`SELECT v.code,v.label,count(t.id) FROM (VALUES ('open','Açık',1),('in_progress','İşlemde',2),('waiting_customer','Yanıt bekleniyor',3),('resolved','Çözüldü',4),('closed','Kapalı',5)) v(code,label,position) LEFT JOIN cohort t ON t.status=v.code GROUP BY v.code,v.label,v.position ORDER BY v.position`, &result.Statuses},
		{`SELECT v.code,v.label,count(t.id) FROM (VALUES ('low','Düşük',1),('normal','Normal',2),('high','Yüksek',3),('critical','Kritik',4)) v(code,label,position) LEFT JOIN cohort t ON t.priority=v.code GROUP BY v.code,v.label,v.position ORDER BY v.position`, &result.Priorities},
		{`SELECT c.code,c.label,count(t.id) FROM ticket_categories c LEFT JOIN cohort t ON t.category_code=c.code GROUP BY c.code,c.label ORDER BY c.code`, &result.Categories},
	}
	for _, q := range bucketQueries {
		rows, err := tx.Query(ctx, cohort+q.query, args...)
		if err != nil {
			return Report{}, err
		}
		for rows.Next() {
			var b Bucket
			if err = rows.Scan(&b.Code, &b.Label, &b.Count); err != nil {
				rows.Close()
				return Report{}, err
			}
			*q.dest = append(*q.dest, b)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return Report{}, err
		}
	}
	// Dates are generated independently of the connection timezone, including zero days.
	rows, err := tx.Query(ctx, cohort+`SELECT to_char(d.day,'YYYY-MM-DD'),count(t.id) FROM generate_series($4::date::timestamp,$5::date::timestamp,interval '1 day') d(day) LEFT JOIN cohort t ON (t.created_at AT TIME ZONE 'Europe/Istanbul')::date=d.day::date GROUP BY d.day ORDER BY d.day`, selected.start, selected.end, selected.Category, selected.From, selected.To)
	if err != nil {
		return Report{}, err
	}
	for rows.Next() {
		var d Day
		if err = rows.Scan(&d.Date, &d.Count); err != nil {
			rows.Close()
			return Report{}, err
		}
		result.Daily = append(result.Daily, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Report{}, err
	}
	// Workload is deliberately independent of the cohort: capacity is global/current.
	err = tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE assignee_id IS NULL) FROM tickets WHERE status IN ('open','in_progress','waiting_customer')`).Scan(&result.Workload.TotalActive, &result.Workload.UnassignedActive)
	if err != nil {
		return Report{}, err
	}
	rows, err = tx.Query(ctx, `SELECT e.user_id,u.display_name,e.capacity,count(t.id),e.enabled,e.on_leave FROM expert_profiles e JOIN users u ON u.id=e.user_id LEFT JOIN tickets t ON t.assignee_id=e.user_id AND t.status IN ('open','in_progress','waiting_customer') GROUP BY e.user_id,u.display_name ORDER BY e.user_id LIMIT 501`)
	if err != nil {
		return Report{}, err
	}
	for rows.Next() {
		var e Expert
		if err = rows.Scan(&e.ID, &e.Name, &e.Capacity, &e.Active, &e.Enabled, &e.OnLeave); err != nil {
			rows.Close()
			return Report{}, err
		}
		result.Workload.Experts = append(result.Workload.Experts, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Report{}, err
	}
	if len(result.Workload.Experts) > MaxExperts {
		return Report{}, ErrLimit
	}
	return result, tx.Commit(ctx)
}
