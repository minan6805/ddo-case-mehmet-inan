package simulator

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"destekmasa/internal/assets"
	"destekmasa/internal/assignment"
	"destekmasa/internal/auth"
	"destekmasa/internal/testdb"
	"destekmasa/internal/tickets"
)

func TestPostgresSimulator(t *testing.T) {
	ctx, pool, users := testdb.New(t, "simulator_test")
	admin := users["admin@destekmasa.local"]
	employee := users["calisan@destekmasa.local"]
	store := NewStore(pool)
	count := func(query string) int64 {
		t.Helper()
		var n int64
		if err := pool.QueryRow(ctx, query).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	var daily Run
	t.Run("full_800_ticket_day_and_related_data", func(t *testing.T) {
		result, err := NewRunner(pool).Run(ctx, testConfig("full-day", 800))
		if err != nil {
			t.Fatal(err)
		}
		daily = result
		if result.Status != "completed" || result.GeneratedTickets != 800 || result.EmailReplays != 320 || result.ReplyDeliveries != 160 || result.ShiftSteps != 4 || result.CompletedSteps != result.TotalSteps || result.TotalSteps != 3012 || result.CompletedAt == nil {
			t.Fatalf("%+v", result)
		}
		for query, want := range map[string]int64{`SELECT count(*) FROM tickets`: 800, `SELECT count(*) FROM incoming_emails`: 320, `SELECT count(*) FROM comment_attachments`: 160, `SELECT count(*) FROM ticket_comments WHERE visibility='internal'`: 40, `SELECT count(*) FROM tickets WHERE status='open'`: 80, `SELECT count(*) FROM tickets WHERE status='in_progress'`: 80, `SELECT count(*) FROM tickets WHERE status='waiting_customer'`: 80, `SELECT count(*) FROM tickets WHERE status='resolved'`: 240, `SELECT count(*) FROM tickets WHERE status='closed'`: 320, `SELECT count(*) FROM assets WHERE code LIKE 'SIM-%'`: 4} {
			if got := count(query); got != want {
				t.Fatalf("%s got %d want %d", query, got, want)
			}
		}
		if count(`SELECT count(*) FROM assignment_history WHERE reason IN ('expert_update','expert_update:queued')`) == 0 {
			t.Fatal("shifts did not trigger assignment reconciliation")
		}
	})
	t.Run("completed_replay_is_a_database_noop", func(t *testing.T) {
		before := map[string]int64{}
		for _, table := range []string{"tickets", "incoming_emails", "ticket_comments", "comment_attachments", "audit_log", "assignment_history", "assets", "asset_dependencies", "simulation_runs"} {
			before[table] = count(`SELECT count(*) FROM ` + table)
		}
		again, err := NewRunner(pool).Run(ctx, testConfig("full-day", 800))
		if err != nil || again.ID != daily.ID || !again.UpdatedAt.Equal(daily.UpdatedAt) {
			t.Fatalf("%+v %v", again, err)
		}
		for table, want := range before {
			if got := count(`SELECT count(*) FROM ` + table); got != want {
				t.Fatalf("replay changed %s: %d %d", table, got, want)
			}
		}
	})
	t.Run("run_name_rejects_changed_configuration", func(t *testing.T) {
		c := testConfig("full-day", 8)
		if _, err := NewRunner(pool).Run(ctx, c); !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	})
	t.Run("profiles_restored_and_graph_is_cycle_safe", func(t *testing.T) {
		var state checkpoint
		if err := pool.QueryRow(ctx, `SELECT checkpoint FROM simulation_runs WHERE id=$1`, daily.ID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		routing := assignment.New(pool, nil)
		for _, p := range state.Profiles {
			current, err := findExpert(ctx, routing, p.ID)
			if err != nil || !sameProfile(current, p.Original) {
				t.Fatalf("profile changed %+v %v", current, err)
			}
		}
		impact, err := assets.New(pool).Impact(ctx, admin, []int64{state.Assets[0]})
		if err != nil || len(impact.Items) != 4 {
			t.Fatalf("%+v %v", impact, err)
		}
	})
	t.Run("unacknowledged_ticket_resumes_without_duplicates", func(t *testing.T) {
		before := count(`SELECT count(*) FROM tickets`)
		runner := NewRunner(pool)
		injected := false
		runner.afterOperation = func(step Step) error {
			if step.Kind == "ticket" && !injected {
				injected = true
				return errors.New("simulated checkpoint interruption")
			}
			return nil
		}
		failed, err := runner.Run(ctx, testConfig("resume-ticket", 8))
		if err == nil || failed.Status != "failed" || failed.LastError != "WORKLOAD_FAILED" {
			t.Fatalf("%+v %v", failed, err)
		}
		resumed, err := NewRunner(pool).Run(ctx, testConfig("resume-ticket", 8))
		if err != nil || resumed.Status != "completed" || resumed.GeneratedTickets != 8 || count(`SELECT count(*) FROM tickets`)-before != 8 {
			t.Fatalf("%+v %v", resumed, err)
		}
	})
	t.Run("failed_shift_restores_and_can_resume", func(t *testing.T) {
		runner := NewRunner(pool)
		runner.afterOperation = func(step Step) error {
			if step.Kind == "shift" && step.Value == "morning" {
				return errors.New("interrupted after profile commit")
			}
			return nil
		}
		failed, err := runner.Run(ctx, testConfig("resume-shift", 8))
		if err == nil || failed.Status != "failed" {
			t.Fatalf("%+v %v", failed, err)
		}
		var state checkpoint
		if err = pool.QueryRow(ctx, `SELECT checkpoint FROM simulation_runs WHERE id=$1`, failed.ID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		for _, p := range state.Profiles {
			current, err := findExpert(ctx, assignment.New(pool, nil), p.ID)
			if err != nil || !sameProfile(current, p.Original) {
				t.Fatalf("restore failed %+v %v", current, err)
			}
		}
		resumed, err := NewRunner(pool).Run(ctx, testConfig("resume-shift", 8))
		if err != nil || resumed.Status != "completed" || resumed.ShiftSteps != 4 {
			t.Fatalf("%+v %v", resumed, err)
		}
	})
	t.Run("manual_profile_change_is_not_overwritten", func(t *testing.T) {
		runner := NewRunner(pool)
		var changedID int64
		var original assignment.ProfileInput
		runner.afterOperation = func(step Step) error {
			if step.Kind == "shift" && step.Value == "morning" {
				board, err := assignment.New(pool, nil).Board(ctx)
				if err != nil {
					return err
				}
				for _, e := range board.Experts {
					if e.Email == "uzman@destekmasa.local" {
						changedID = e.ID
						original = profileInput(e)
						next := original
						next.Capacity = 77
						_, err = assignment.New(pool, nil).UpdateExpert(ctx, e.ID, admin.ID, next)
						return err
					}
				}
			}
			return nil
		}
		failed, err := runner.Run(ctx, testConfig("manual-change", 8))
		if !errors.Is(err, ErrProfileChanged) || failed.Status != "failed" || failed.LastError != "PROFILE_CHANGED" {
			t.Fatalf("%+v %v", failed, err)
		}
		current, err := findExpert(ctx, assignment.New(pool, nil), changedID)
		if err != nil || current.Capacity != 77 {
			t.Fatalf("human edit overwritten %+v %v", current, err)
		}
		// Restore this test's external edit to the simulator's known scenario, then resume.
		original.Version = current.Version
		if _, err = assignment.New(pool, nil).UpdateExpert(ctx, changedID, admin.ID, original); err != nil {
			t.Fatal(err)
		}
		if resumed, err := NewRunner(pool).Run(ctx, testConfig("manual-change", 8)); err != nil || resumed.Status != "completed" {
			t.Fatalf("%+v %v", resumed, err)
		}
	})
	t.Run("concurrent_runner_is_rejected_before_any_writes", func(t *testing.T) {
		conn, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Release()
		if _, err = conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, runnerLock); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, runnerLock); err != nil {
				t.Error(err)
			}
		}()
		before := count(`SELECT count(*) FROM simulation_runs`)
		if _, err = NewRunner(pool).Run(ctx, testConfig("concurrent", 4)); !errors.Is(err, ErrRunning) {
			t.Fatal(err)
		}
		if count(`SELECT count(*) FROM simulation_runs`) != before {
			t.Fatal("concurrent runner created state")
		}
	})
	t.Run("pacing_and_cancellation_restore_profiles", func(t *testing.T) {
		child, cancel := context.WithCancel(ctx)
		runner := NewRunner(pool)
		runner.afterOperation = func(step Step) error {
			if step.Kind == "shift" {
				cancel()
				return child.Err()
			}
			return nil
		}
		c := testConfig("cancel", 4)
		c.DaySeconds = 1
		failed, err := runner.Run(child, c)
		if !errors.Is(err, context.Canceled) || failed.LastError != "RUN_INTERRUPTED" {
			t.Fatalf("%+v %v", failed, err)
		}
		// Resume with exactly the same pacing/configuration.
		resumed, err := NewRunner(pool).Run(ctx, c)
		if err != nil || resumed.Status != "completed" {
			t.Fatalf("%+v %v", resumed, err)
		}
		if resumed.CompletedAt.Sub(resumed.StartedAt) < time.Second {
			t.Fatal("pacing was skipped")
		}
	})
	t.Run("external_ticket_state_is_not_reopened", func(t *testing.T) {
		runner := NewRunner(pool)
		var changedID int64
		ticketService := tickets.New(pool)
		runner.afterOperation = func(step Step) error {
			if step.Kind == "ticket" && step.Index == 1 {
				if err := pool.QueryRow(ctx, `SELECT id FROM tickets WHERE title=$1`, step.Input.Title).Scan(&changedID); err != nil {
					return err
				}
				for _, status := range []string{"in_progress", "resolved"} {
					if _, err := ticketService.UpdateStatus(ctx, admin, changedID, status); err != nil {
						return err
					}
				}
			}
			return nil
		}
		failed, err := runner.Run(ctx, testConfig("external-ticket", 4))
		if !errors.Is(err, ErrConflict) || failed.Status != "failed" {
			t.Fatalf("%+v %v", failed, err)
		}
		current, err := ticketService.Get(ctx, admin, changedID)
		if err != nil || current.Status != "resolved" {
			t.Fatalf("manual state overwritten %+v %v", current, err)
		}
		// The operator explicitly resumes processing before restarting the plan.
		if _, err = ticketService.UpdateStatus(ctx, admin, changedID, "in_progress"); err != nil {
			t.Fatal(err)
		}
		resumed, err := NewRunner(pool).Run(ctx, testConfig("external-ticket", 4))
		if err != nil || resumed.Status != "completed" {
			t.Fatalf("%+v %v", resumed, err)
		}
	})
	t.Run("startup_failure_is_recorded_and_can_retry", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `UPDATE users SET role_code='employee' WHERE id=$1`, admin.ID); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := pool.Exec(context.Background(), `UPDATE users SET role_code='admin' WHERE id=$1`, admin.ID); err != nil {
				t.Error(err)
			}
		}()
		failed, err := NewRunner(pool).Run(ctx, testConfig("startup-retry", 4))
		if err == nil || failed.Status != "failed" || failed.LastError != "WORKLOAD_FAILED" {
			t.Fatalf("%+v %v", failed, err)
		}
		if _, err := pool.Exec(ctx, `UPDATE users SET role_code='admin' WHERE id=$1`, admin.ID); err != nil {
			t.Fatal(err)
		}
		resumed, err := NewRunner(pool).Run(ctx, testConfig("startup-retry", 4))
		if err != nil || resumed.Status != "completed" {
			t.Fatalf("%+v %v", resumed, err)
		}
	})
	t.Run("status_access_pagination_and_no_checkpoints_in_response", func(t *testing.T) {
		for _, user := range []auth.User{employee, users["uzman@destekmasa.local"], users["varlik@destekmasa.local"]} {
			if _, err := store.List(ctx, user, 0, 20); !errors.Is(err, ErrForbidden) {
				t.Fatal(err)
			}
			if _, err := store.Get(ctx, user, daily.ID); !errors.Is(err, ErrForbidden) {
				t.Fatal(err)
			}
		}
		first, err := store.List(ctx, admin, 0, 1)
		if err != nil || len(first) != 2 {
			t.Fatalf("%+v %v", first, err)
		}
		next, err := store.List(ctx, admin, first[0].ID, 1)
		if err != nil || len(next) == 0 || next[0].ID >= first[0].ID {
			t.Fatalf("%+v %v", next, err)
		}
		item, err := store.Get(ctx, admin, daily.ID)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err = json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		if _, ok := fields["checkpoint"]; ok {
			t.Fatal("checkpoint exposed")
		}
		if _, err = store.Get(ctx, admin, 9999999); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
		if _, err = store.List(ctx, admin, 0, 101); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	})
}
