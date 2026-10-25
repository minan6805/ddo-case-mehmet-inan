package simulator

import (
	"context"
	"errors"
	"time"

	"destekmasa/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Run struct {
	ID               int64      `json:"id"`
	Config           Config     `json:"config"`
	PlanHash         string     `json:"plan_hash"`
	TotalSteps       int        `json:"total_steps"`
	CompletedSteps   int        `json:"completed_steps"`
	GeneratedTickets int        `json:"generated_tickets"`
	EmailReplays     int        `json:"email_replays"`
	ReplyDeliveries  int        `json:"reply_deliveries"`
	ShiftSteps       int        `json:"shift_steps"`
	Status           string     `json:"status"`
	LastError        string     `json:"last_error"`
	StartedAt        time.Time  `json:"started_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	CompletedAt      *time.Time `json:"completed_at"`
}

const selectRun = `SELECT id,config,plan_hash,total_steps,completed_steps,generated_tickets,email_replays,reply_deliveries,shift_steps,status,last_error,started_at,updated_at,completed_at FROM simulation_runs `

func scanRun(row pgx.Row) (Run, error) {
	var r Run
	err := row.Scan(&r.ID, &r.Config, &r.PlanHash, &r.TotalSteps, &r.CompletedSteps, &r.GeneratedTickets, &r.EmailReplays, &r.ReplyDeliveries, &r.ShiftSteps, &r.Status, &r.LastError, &r.StartedAt, &r.UpdatedAt, &r.CompletedAt)
	return r, err
}

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }
func CanRead(role string) bool           { return role == "team_lead" || role == "admin" }
func (s *Store) List(ctx context.Context, user auth.User, before int64, limit int) ([]Run, error) {
	if !CanRead(user.Role) {
		return nil, ErrForbidden
	}
	if before < 0 || limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	rows, err := s.pool.Query(ctx, selectRun+`WHERE ($1::bigint=0 OR id<$1) ORDER BY id DESC LIMIT $2`, before, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Run{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
func (s *Store) Get(ctx context.Context, user auth.User, id int64) (Run, error) {
	if !CanRead(user.Role) {
		return Run{}, ErrForbidden
	}
	if id < 1 {
		return Run{}, ErrInvalid
	}
	r, err := scanRun(s.pool.QueryRow(ctx, selectRun+`WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Run{}, ErrNotFound
	}
	return r, err
}
