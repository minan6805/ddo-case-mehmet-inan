package sla

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"destekmasa/internal/testdb"
)

func TestPostgresSLAEngine(t *testing.T) {
	ctx, pool, _ := testdb.New(t, "sla_test")
	var err error
	var ownerID, adminID, otherID int64
	if err = pool.QueryRow(ctx, `SELECT id FROM users WHERE role_code='employee'`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT id FROM users WHERE role_code='admin'`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT id FROM users WHERE role_code='asset_manager'`).Scan(&otherID); err != nil {
		t.Fatal(err)
	}
	now := moment("2026-10-26T09:00:00+03:00")
	engine := New(pool, func() time.Time { return now })
	create := func(t *testing.T) int64 {
		t.Helper()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(context.Background())
		if err = LockShared(ctx, tx); err != nil {
			t.Fatal(err)
		}
		var id int64
		err = tx.QueryRow(ctx, `INSERT INTO tickets(requester_id,title,description,priority,category_code,created_at,updated_at)
            VALUES($1,'SLA testi','Kontrollü saat','high','general',$2,$2) RETURNING id`, ownerID, now).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		if err = engine.Initialize(ctx, tx, id); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		return id
	}
	change := func(t *testing.T, id int64, status string) {
		t.Helper()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(context.Background())
		if err = LockShared(ctx, tx); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, `SELECT id FROM tickets WHERE id=$1 FOR UPDATE`, id); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, `UPDATE tickets SET status=$2,updated_at=$3 WHERE id=$1`, id, status, now); err != nil {
			t.Fatal(err)
		}
		if err = engine.RecordStatus(ctx, tx, id, status); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	countEvents := func(t *testing.T, id int64, stage string) int {
		t.Helper()
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM sla_events WHERE ticket_id=$1 AND stage=$2`, id, stage).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	t.Run("policy_and_calendar_seed", func(t *testing.T) {
		settings, err := engine.Settings(ctx)
		if err != nil || len(settings.Policies) != 16 || len(settings.Calendar.Holidays) != 17 {
			t.Fatalf("settings: %+v %v", settings, err)
		}
		if settings.Calendar.StartMinute != 510 || settings.Calendar.EndMinute != 1080 {
			t.Fatal("wrong working hours")
		}
	})
	t.Run("ticket_deadlines_are_persisted", func(t *testing.T) {
		now = moment("2026-10-26T09:00:00+03:00")
		id := create(t)
		result, err := engine.Ticket(ctx, ownerID, false, id)
		if err != nil || result.Response.DueAt == nil || !result.Response.DueAt.Equal(moment("2026-10-26T10:00:00+03:00")) {
			t.Fatalf("deadline: %+v %v", result, err)
		}
		var due time.Time
		if err := pool.QueryRow(ctx, `SELECT response_due_at FROM ticket_sla WHERE ticket_id=$1`, id).Scan(&due); err != nil {
			t.Fatal(err)
		}
		if !due.Equal(*result.Response.DueAt) {
			t.Fatal("persisted deadline differs")
		}
	})
	t.Run("ticket_sla_checks_ownership", func(t *testing.T) {
		id := create(t)
		if _, err := engine.Ticket(ctx, otherID, false, id); !errors.Is(err, ErrForbidden) {
			t.Fatalf("ownership: %v", err)
		}
		if _, err := engine.Ticket(ctx, otherID, true, id); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("warning_breach_escalation_are_deduplicated", func(t *testing.T) {
		now = moment("2026-10-26T09:00:00+03:00")
		id := create(t)
		now = moment("2026-10-26T09:48:00+03:00")
		if _, err := engine.Sweep(ctx, 1000); err != nil {
			t.Fatal(err)
		}
		if countEvents(t, id, "warning") != 1 || countEvents(t, id, "breach") != 0 {
			t.Fatal("80 percent warning missing or premature breach")
		}
		now = moment("2026-10-26T10:00:00+03:00")
		for i := 0; i < 2; i++ {
			if _, err := engine.Sweep(ctx, 1000); err != nil {
				t.Fatal(err)
			}
		}
		if countEvents(t, id, "breach") != 1 || countEvents(t, id, "escalation") != 1 {
			t.Fatal("breach/escalation duplicated or missing")
		}
		var level int
		if err := pool.QueryRow(ctx, `SELECT escalation_level FROM tickets WHERE id=$1`, id).Scan(&level); err != nil || level != 1 {
			t.Fatalf("escalation: %d %v", level, err)
		}
		var recipient string
		if err := pool.QueryRow(ctx, `SELECT recipient_role FROM sla_events WHERE ticket_id=$1 AND stage='escalation'`, id).Scan(&recipient); err != nil || recipient != "team_lead" {
			t.Fatal("wrong escalation recipient")
		}
	})
	t.Run("first_acknowledgement_stops_response", func(t *testing.T) {
		now = moment("2026-10-26T10:00:00+03:00")
		id := create(t)
		now = now.Add(10 * time.Minute)
		change(t, id, "in_progress")
		now = now.Add(time.Hour)
		result, err := engine.Ticket(ctx, ownerID, false, id)
		if err != nil || result.Response.CompletedAt == nil || result.Response.ElapsedSeconds != 600 || result.Resolution.ElapsedSeconds != 4200 {
			t.Fatalf("response: %+v %v", result, err)
		}
	})
	t.Run("waiting_and_resume", func(t *testing.T) {
		now = moment("2026-10-26T11:00:00+03:00")
		id := create(t)
		now = now.Add(10 * time.Minute)
		change(t, id, "in_progress")
		now = now.Add(20 * time.Minute)
		change(t, id, "waiting_customer")
		now = now.Add(2 * time.Hour)
		paused, err := engine.Ticket(ctx, ownerID, false, id)
		if err != nil || !paused.Paused || paused.Resolution.ElapsedSeconds != 1800 || paused.Resolution.DueAt != nil {
			t.Fatalf("pause: %+v %v", paused, err)
		}
		change(t, id, "in_progress")
		now = now.Add(10 * time.Minute)
		resumed, err := engine.Ticket(ctx, ownerID, false, id)
		if err != nil || resumed.Paused || resumed.Resolution.ElapsedSeconds != 2400 {
			t.Fatalf("resume: %+v %v", resumed, err)
		}
	})
	t.Run("resolved_and_reopened", func(t *testing.T) {
		now = moment("2026-10-26T13:00:00+03:00")
		id := create(t)
		now = now.Add(time.Minute)
		change(t, id, "in_progress")
		now = now.Add(29 * time.Minute)
		change(t, id, "resolved")
		now = now.Add(time.Hour)
		resolved, err := engine.Ticket(ctx, ownerID, false, id)
		if err != nil || resolved.Resolution.ElapsedSeconds != 1800 || resolved.Resolution.CompletedAt == nil {
			t.Fatalf("resolve: %+v %v", resolved, err)
		}
		change(t, id, "in_progress")
		now = now.Add(10 * time.Minute)
		resumed, err := engine.Ticket(ctx, ownerID, false, id)
		if err != nil || resumed.Resolution.ElapsedSeconds != 2400 || resumed.Resolution.CompletedAt != nil {
			t.Fatalf("reopen: %+v %v", resumed, err)
		}
	})
	t.Run("policy_change_applies_to_new_tickets_only", func(t *testing.T) {
		now = moment("2026-10-26T14:00:00+03:00")
		oldID := create(t)
		policy, err := engine.UpdatePolicy(ctx, adminID, Policy{Category: "general", Priority: "high", ResponseMinutes: 120, ResolutionMinutes: 600, Version: 1})
		if err != nil || policy.Version != 2 {
			t.Fatalf("policy: %+v %v", policy, err)
		}
		newID := create(t)
		old, err := engine.Ticket(ctx, ownerID, false, oldID)
		if err != nil {
			t.Fatal(err)
		}
		latest, err := engine.Ticket(ctx, ownerID, false, newID)
		if err != nil {
			t.Fatal(err)
		}
		if old.Response.BudgetSeconds != 3600 || latest.Response.BudgetSeconds != 7200 {
			t.Fatal("policy snapshot not preserved")
		}
		policy.Version = 1
		if _, err := engine.UpdatePolicy(ctx, adminID, policy); !errors.Is(err, ErrConflict) {
			t.Fatal("stale policy overwrite accepted")
		}
	})
	t.Run("multiple_workers_do_not_duplicate_events", func(t *testing.T) {
		now = moment("2026-10-26T15:00:00+03:00")
		id := create(t)
		now = moment("2026-10-27T10:00:00+03:00")
		var wg sync.WaitGroup
		problems := make(chan error, 4)
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := engine.Sweep(ctx, 1000)
				if err != nil {
					problems <- err
				}
			}()
		}
		wg.Wait()
		close(problems)
		for err := range problems {
			t.Error(err)
		}
		if countEvents(t, id, "escalation") != 1 {
			t.Fatal("workers duplicated or missed escalation")
		}
	})
	t.Run("calendar_change_recomputes_every_timer_and_audits", func(t *testing.T) {
		now = moment("2026-10-27T11:00:00+03:00")
		id := create(t)
		now = now.Add(30 * time.Minute)
		before, err := engine.Ticket(ctx, ownerID, false, id)
		if err != nil {
			t.Fatal(err)
		}
		settings, err := engine.Settings(ctx)
		if err != nil {
			t.Fatal(err)
		}
		calendar := settings.Calendar
		calendar.Holidays = append(calendar.Holidays, Holiday{Date: "2026-10-27", Label: "Kurum kapanışı"})
		result, err := engine.UpdateCalendar(ctx, adminID, calendar)
		if err != nil || result.Version != calendar.Version+1 {
			t.Fatalf("calendar: %+v %v", result, err)
		}
		after, err := engine.Ticket(ctx, ownerID, false, id)
		if err != nil || before.Response.ElapsedSeconds != 1800 || after.Response.ElapsedSeconds != 0 || after.CalendarVersion != result.Version {
			t.Fatalf("recalc: before %+v after %+v %v", before, after, err)
		}
		if after.Response.DueAt == nil || !after.Response.DueAt.Equal(moment("2026-10-28T10:30:00+03:00")) {
			t.Fatalf("new deadline: %+v", after.Response)
		}
		var stale int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM ticket_sla WHERE calendar_version<>$1`, result.Version).Scan(&stale); err != nil || stale != 0 {
			t.Fatal("stale projections remain")
		}
		if _, err := engine.UpdateCalendar(ctx, adminID, calendar); !errors.Is(err, ErrConflict) {
			t.Fatal("stale calendar accepted")
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='sla.calendar'`).Scan(&count); err != nil || count != 1 {
			t.Fatal("calendar audit missing")
		}
	})
	t.Run("alerts_are_paginated", func(t *testing.T) {
		first, err := engine.Alerts(ctx, 0, 1)
		if err != nil || len(first) != 2 {
			t.Fatalf("alerts: %+v %v", first, err)
		}
		next, err := engine.Alerts(ctx, first[0].ID, 1)
		if err != nil || len(next) == 0 || next[0].ID >= first[0].ID {
			t.Fatal("alert cursor failed")
		}
	})
}
