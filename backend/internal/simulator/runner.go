package simulator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"net/textproto"
	"time"

	"destekmasa/internal/assets"
	"destekmasa/internal/assignment"
	"destekmasa/internal/auth"
	"destekmasa/internal/sla"
	"destekmasa/internal/tickets"
	"github.com/jackc/pgx/v5/pgxpool"
)

const runnerLock int64 = 7420185

type profileState struct {
	ID       int64                   `json:"id"`
	Original assignment.ProfileInput `json:"original"`
	Desired  assignment.ProfileInput `json:"desired"`
}
type checkpoint struct {
	Tickets  []int64        `json:"tickets"`
	Assets   []int64        `json:"assets"`
	Profiles []profileState `json:"profiles"`
}
type Runner struct {
	pool           *pgxpool.Pool
	afterOperation func(Step) error
}

func NewRunner(pool *pgxpool.Pool) *Runner { return &Runner{pool: pool} }
func profileInput(e assignment.Expert) assignment.ProfileInput {
	return assignment.ProfileInput{Capacity: e.Capacity, Enabled: e.Enabled, OnLeave: e.OnLeave, Timezone: e.Timezone, ShiftStartMinute: e.ShiftStartMinute, ShiftEndMinute: e.ShiftEndMinute, Weekdays: e.Weekdays, Skills: e.Skills, Version: e.Version}
}
func sameProfile(a, b assignment.ProfileInput) bool {
	a.Version = 0
	b.Version = 0
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}
func shiftInput(original assignment.ProfileInput, index int) assignment.ProfileInput {
	original.Version = 0
	original.Timezone = "Europe/Istanbul"
	original.Weekdays = []int{1, 2, 3, 4, 5}
	original.ShiftStartMinute = 510
	original.ShiftEndMinute = 720
	if index == 1 {
		original.ShiftStartMinute = 720
		original.ShiftEndMinute = 1080
	}
	return original
}
func loadUser(ctx context.Context, pool *pgxpool.Pool, email, role string) (auth.User, error) {
	var u auth.User
	err := pool.QueryRow(ctx, `SELECT id,email,display_name,role_code FROM users WHERE email=$1 AND role_code=$2`, email, role).Scan(&u.ID, &u.Email, &u.DisplayName, &u.Role)
	return u, err
}
func findExpert(ctx context.Context, routing *assignment.Service, id int64) (assignment.ProfileInput, error) {
	board, err := routing.Board(ctx)
	if err != nil {
		return assignment.ProfileInput{}, err
	}
	for _, e := range board.Experts {
		if e.ID == id {
			return profileInput(e), nil
		}
	}
	return assignment.ProfileInput{}, assignment.ErrNotFound
}
func setProfile(ctx context.Context, routing *assignment.Service, actor int64, p profileState, index int, target assignment.ProfileInput) error {
	current, err := findExpert(ctx, routing, p.ID)
	if err != nil {
		return err
	}
	if sameProfile(current, target) {
		return nil
	}
	if !sameProfile(current, p.Original) && !sameProfile(current, p.Desired) && !sameProfile(current, shiftInput(p.Original, index)) {
		return ErrProfileChanged
	}
	target.Version = current.Version
	_, err = routing.UpdateExpert(ctx, p.ID, actor, target)
	return err
}
func mailBytes(p Plan, step Step, ticketID int64) ([]byte, error) {
	reply := step.Kind == "reply" || step.Kind == "reply_replay"
	kind := "new"
	subject := step.Input.Title
	body := step.Input.Description + "\n\n-- \nDemo Simülatör"
	if reply {
		kind = "reply"
		subject = fmt.Sprintf("Re: #%d ek bilgi", ticketID)
		body = "Ek bilgi: bağlantı kontrol edildi.\n\nOn Monday, Demo wrote:\n> Önceki mesaj"
	}
	id := fmt.Sprintf("<%s-%s-%d-%04d-%s@simulator.destekmasa.local>", p.Config.Name, p.Config.Day, p.Config.Seed, step.Index, kind)
	var out bytes.Buffer
	fmt.Fprintf(&out, "Message-ID: %s\r\nFrom: calisan@destekmasa.local\r\nTo: destek@destekmasa.local\r\nSubject: %s\r\nMIME-Version: 1.0\r\n", id, mime.QEncoding.Encode("utf-8", subject))
	if reply {
		out.WriteString("Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\n")
		out.WriteString(base64.StdEncoding.EncodeToString([]byte(body)))
		out.WriteString("\r\n")
		return out.Bytes(), nil
	}
	boundary := fmt.Sprintf("sim-%s-%d", p.Hash[:16], step.Index)
	fmt.Fprintf(&out, "Content-Type: multipart/mixed; boundary=%q\r\n\r\n", boundary)
	writer := multipart.NewWriter(&out)
	if err := writer.SetBoundary(boundary); err != nil {
		return nil, err
	}
	part, err := writer.CreatePart(textproto.MIMEHeader{"Content-Type": {"text/plain; charset=utf-8"}, "Content-Transfer-Encoding": {"base64"}})
	if err != nil {
		return nil, err
	}
	if _, err = part.Write([]byte(base64.StdEncoding.EncodeToString([]byte(body)))); err != nil {
		return nil, err
	}
	attachment, err := writer.CreatePart(textproto.MIMEHeader{"Content-Type": {"application/octet-stream"}, "Content-Disposition": {"attachment; filename=baglanti.txt"}, "Content-Transfer-Encoding": {"base64"}})
	if err != nil {
		return nil, err
	}
	if _, err = attachment.Write([]byte(base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("seed=%d\nitem=%d\n", p.Config.Seed, step.Index))))); err != nil {
		return nil, err
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
func (r *Runner) Run(ctx context.Context, c Config) (result Run, runErr error) {
	plan, err := Build(c)
	if err != nil {
		return Run{}, err
	}
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return Run{}, err
	}
	defer conn.Release()
	var locked bool
	if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, runnerLock).Scan(&locked); err != nil {
		return Run{}, err
	}
	if !locked {
		return Run{}, ErrRunning
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(cleanup, `SELECT pg_advisory_unlock($1)`, runnerLock)
	}()
	configJSON, err := json.Marshal(c)
	if err != nil {
		return Run{}, err
	}
	_, err = conn.Exec(ctx, `INSERT INTO simulation_runs(namespace,config,plan_hash,total_steps) VALUES($1,$2,$3,$4) ON CONFLICT(namespace) DO NOTHING`, c.Namespace(), configJSON, plan.Hash, len(plan.Steps))
	if err != nil {
		return Run{}, err
	}
	result, err = scanRun(conn.QueryRow(ctx, selectRun+`WHERE namespace=$1`, c.Namespace()))
	if err != nil {
		return Run{}, err
	}
	if result.PlanHash != plan.Hash {
		return Run{}, ErrConflict
	}
	if result.Status == "completed" {
		return result, nil
	}
	managed := false
	defer func() {
		if managed || runErr == nil {
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(cleanup, `UPDATE simulation_runs SET status='failed',last_error='WORKLOAD_FAILED',updated_at=clock_timestamp() WHERE id=$1`, result.ID)
		if latest, err := scanRun(conn.QueryRow(cleanup, selectRun+`WHERE id=$1`, result.ID)); err == nil {
			result = latest
		}
	}()
	var state checkpoint
	if err = conn.QueryRow(ctx, `SELECT checkpoint FROM simulation_runs WHERE id=$1`, result.ID).Scan(&state); err != nil {
		return result, err
	}
	admin, err := loadUser(ctx, r.pool, "admin@destekmasa.local", "admin")
	if err != nil {
		return result, err
	}
	employee, err := loadUser(ctx, r.pool, "calisan@destekmasa.local", "employee")
	if err != nil {
		return result, err
	}
	routing := assignment.New(r.pool, nil)
	ticketService := tickets.NewWithEngines(r.pool, sla.New(r.pool, nil), routing)
	assetService := assets.New(r.pool)
	if len(state.Tickets) == 0 {
		state.Tickets = make([]int64, c.Count)
		state.Assets = make([]int64, 4)
		board, err := routing.Board(ctx)
		if err != nil {
			return result, err
		}
		for _, email := range []string{"uzman@destekmasa.local", "uzman2@destekmasa.local"} {
			for _, e := range board.Experts {
				if e.Email == email {
					in := profileInput(e)
					state.Profiles = append(state.Profiles, profileState{ID: e.ID, Original: in, Desired: in})
				}
			}
		}
	}
	if len(state.Tickets) != c.Count || len(state.Assets) != 4 || len(state.Profiles) != 2 {
		return result, ErrInvalid
	}
	save := func(check context.Context) error {
		raw, err := json.Marshal(state)
		if err != nil {
			return err
		}
		_, err = conn.Exec(check, `UPDATE simulation_runs SET completed_steps=$2,generated_tickets=$3,email_replays=$4,reply_deliveries=$5,shift_steps=$6,status=$7,checkpoint=$8,last_error=$9,updated_at=clock_timestamp(),completed_at=CASE WHEN $7='completed' THEN clock_timestamp() ELSE NULL END WHERE id=$1`, result.ID, result.CompletedSteps, result.GeneratedTickets, result.EmailReplays, result.ReplyDeliveries, result.ShiftSteps, result.Status, raw, result.LastError)
		return err
	}
	result.Status = "running"
	result.LastError = ""
	if err = save(ctx); err != nil {
		return result, err
	}
	managed = true
	defer func() {
		// Use a separate deadline to restore only known simulator changes on interrupt.
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := conn.Ping(cleanup); err != nil {
			if runErr == nil {
				runErr = err
			}
			return
		}
		for i, p := range state.Profiles {
			if err := setProfile(cleanup, routing, admin.ID, p, i, p.Original); err != nil && runErr == nil {
				runErr = err
			}
		}
		if runErr != nil {
			result.Status = "failed"
			result.LastError = "WORKLOAD_FAILED"
			if errors.Is(runErr, ErrProfileChanged) {
				result.LastError = "PROFILE_CHANGED"
			}
			if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
				result.LastError = "RUN_INTERRUPTED"
			}
		}
		if err := save(cleanup); err != nil && runErr == nil {
			runErr = err
		}
		latest, err := scanRun(conn.QueryRow(cleanup, selectRun+`WHERE id=$1`, result.ID))
		if err == nil {
			result = latest
		}
	}()
	// Failed runs restore profiles on exit. Reapply their checkpointed scenario on resume.
	for i, p := range state.Profiles {
		if err = setProfile(ctx, routing, admin.ID, p, i, p.Desired); err != nil {
			return result, err
		}
	}
	baseHash := sha256.Sum256([]byte(c.Namespace()))
	prefix := "SIM-" + hex.EncodeToString(baseHash[:6])
	executor := stepExecutor{
		plan: plan, state: &state, result: &result,
		admin: admin, employee: employee,
		routing: routing, tickets: ticketService, assets: assetService,
		assetPrefix: prefix,
	}
	for result.CompletedSteps < len(plan.Steps) {
		step := plan.Steps[result.CompletedSteps]
		target := result.StartedAt.Add(time.Duration(step.OffsetMilliseconds) * time.Millisecond)
		for time.Now().Before(target) {
			wait := time.Until(target)
			if wait > 5*time.Second {
				wait = 5 * time.Second
			}
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return result, ctx.Err()
			case <-timer.C:
			}
			if err = save(ctx); err != nil {
				return result, err
			}
		}
		if err = ctx.Err(); err != nil {
			return result, err
		}
		previous := result
		err = executor.execute(ctx, step)
		if err == nil && r.afterOperation != nil {
			err = r.afterOperation(step)
		}
		if err != nil {
			result = previous
			return result, err
		}
		result.CompletedSteps++
		if err = save(ctx); err != nil {
			result = previous
			return result, err
		}
	}
	result.Status = "completed"
	return result, nil
}
