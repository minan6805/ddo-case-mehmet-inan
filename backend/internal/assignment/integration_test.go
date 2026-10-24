package assignment_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"destekmasa/internal/assignment"
	"destekmasa/internal/auth"
	"destekmasa/internal/database"
	"destekmasa/internal/httpapi"
	"destekmasa/internal/sla"
	"destekmasa/internal/testdb"
	"destekmasa/internal/tickets"
)

func TestPostgresAssignmentEngine(t *testing.T) {
	ctx, pool, _ := testdb.New(t, "assignment_test")
	var err error
	at := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	routing := assignment.New(pool, func() time.Time { return at })
	slaEngine := sla.New(pool, nil)
	ticketService := tickets.NewWithEngines(pool, slaEngine, routing)
	authentication, err := auth.New(pool, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	router := httpapi.NewServicesRouter(httpapi.Services{Database: pool, Authentication: authentication, Tickets: ticketService, SLA: slaEngine, Assignment: routing})
	var owner auth.User
	if err = pool.QueryRow(ctx, `SELECT id,email,display_name,role_code FROM users WHERE role_code='employee'`).Scan(&owner.ID, &owner.Email, &owner.DisplayName, &owner.Role); err != nil {
		t.Fatal(err)
	}
	var managerID int64
	if err = pool.QueryRow(ctx, `SELECT id FROM users WHERE role_code='team_lead'`).Scan(&managerID); err != nil {
		t.Fatal(err)
	}
	var sequence atomic.Int64
	input := tickets.CreateInput{Title: "Atama testi", Description: "Eşzamanlılık ve uygunluk kanıtı", Category: "general", Priority: "normal"}
	create := func(t *testing.T) tickets.Ticket {
		t.Helper()
		ticket, err := ticketService.Create(ctx, owner, input, fmt.Sprintf("assignment-%d", sequence.Add(1)))
		if err != nil {
			t.Fatal(err)
		}
		return ticket
	}
	board := func(t *testing.T) assignment.Board {
		t.Helper()
		result, err := routing.Board(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	// Test fixtures reset only this isolated schema; history remains append-only.
	resetState := func(t *testing.T, capacity int) {
		t.Helper()
		for _, query := range []string{`UPDATE tickets SET status='closed'`, `DELETE FROM assignment_queue`, `UPDATE assignment_rotation SET last_seq=0`} {
			if _, err := pool.Exec(ctx, query); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := pool.Exec(ctx, `UPDATE expert_profiles SET capacity=$1,enabled=true,on_leave=false,timezone='Europe/Istanbul',shift_start_minute=0,shift_end_minute=1440,weekdays_mask=127,last_assignment_seq=0`, capacity); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM expert_skills`); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO expert_skills(expert_id,skill_code,level) SELECT e.user_id,s.code,5 FROM expert_profiles e CROSS JOIN skills s`); err != nil {
			t.Fatal(err)
		}
	}
	profileInput := func(expert assignment.Expert) assignment.ProfileInput {
		return assignment.ProfileInput{Capacity: expert.Capacity, Enabled: expert.Enabled, OnLeave: expert.OnLeave, Timezone: expert.Timezone, ShiftStartMinute: expert.ShiftStartMinute, ShiftEndMinute: expert.ShiftEndMinute, Weekdays: expert.Weekdays, Skills: expert.Skills, Version: expert.Version}
	}
	t.Run("three_experts_seed_without_overwriting_edits", func(t *testing.T) {
		before := board(t)
		if len(before.Experts) != 3 || len(before.Skills) != 4 {
			t.Fatal("demo expert seed missing")
		}
		chosen := before.Experts[0]
		changed := profileInput(chosen)
		changed.Capacity = 7
		changed.Skills = []assignment.Skill{{Code: "general", Level: 2}}
		if _, err := routing.UpdateExpert(ctx, chosen.ID, managerID, changed); err != nil {
			t.Fatal(err)
		}
		if err := database.SeedDemo(ctx, pool, "OtherPassword2026!"); err != nil {
			t.Fatal(err)
		}
		after := board(t)
		if after.Experts[0].Capacity != 7 || len(after.Experts[0].Skills) != 1 || after.Experts[0].Skills[0].Level != 2 {
			t.Fatal("seed overwrote expert edits")
		}
	})
	t.Run("round_robin_and_load_distribution", func(t *testing.T) {
		resetState(t, 8)
		ids := map[int64]bool{}
		for i := 0; i < 3; i++ {
			ticket := create(t)
			if ticket.AssigneeID == nil {
				t.Fatal("eligible ticket unassigned")
			}
			ids[*ticket.AssigneeID] = true
		}
		if len(ids) != 3 {
			t.Fatal("equal experts did not rotate")
		}
		for _, expert := range board(t).Experts {
			if expert.Load != 1 {
				t.Fatalf("unbalanced load: %+v", expert)
			}
		}
	})
	t.Run("required_skill_is_mandatory", func(t *testing.T) {
		resetState(t, 8)
		experts := board(t).Experts
		if _, err := pool.Exec(ctx, `DELETE FROM expert_skills WHERE skill_code='network' AND expert_id<>$1`, experts[1].ID); err != nil {
			t.Fatal(err)
		}
		network := input
		network.Category = "network"
		ticket, err := ticketService.Create(ctx, owner, network, "network-only")
		if err != nil || ticket.AssigneeID == nil || *ticket.AssigneeID != experts[1].ID {
			t.Fatalf("skill ignored: %+v %v", ticket, err)
		}
	})
	t.Run("off_shift_experts_are_not_assigned", func(t *testing.T) {
		resetState(t, 8)
		if _, err := pool.Exec(ctx, `UPDATE expert_profiles SET weekdays_mask=64`); err != nil {
			t.Fatal(err)
		}
		ticket := create(t)
		if ticket.AssigneeID != nil || board(t).Queued != 1 {
			t.Fatal("off-shift ticket not queued")
		}
	})
	t.Run("no_capacity_queues_without_overflow", func(t *testing.T) {
		resetState(t, 1)
		for i := 0; i < 5; i++ {
			create(t)
		}
		result := board(t)
		if result.Queued != 2 {
			t.Fatalf("queued: %d", result.Queued)
		}
		for _, expert := range result.Experts {
			if expert.Load != 1 {
				t.Fatal("capacity overflow or lost assignment")
			}
		}
	})
	t.Run("two_concurrent_http_requests_cannot_take_one_slot", func(t *testing.T) {
		resetState(t, 1)
		experts := board(t).Experts
		if _, err := pool.Exec(ctx, `UPDATE expert_profiles SET enabled=false WHERE user_id<>$1`, experts[0].ID); err != nil {
			t.Fatal(err)
		}
		session, err := authentication.Login(ctx, "calisan@destekmasa.local", "DemoDestek2026!", "")
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		codes := make(chan int, 2)
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				<-start
				request := httptest.NewRequest("POST", "http://example.test/tickets", strings.NewReader(`{"title":"Kapasite yarışı","description":"Tek boş yer","priority":"normal","category":"general"}`))
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Idempotency-Key", fmt.Sprintf("race-%d", index))
				request.AddCookie(&http.Cookie{Name: "destekmasa_session", Value: session.Token})
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				codes <- response.Code
			}(i)
		}
		close(start)
		wg.Wait()
		close(codes)
		for code := range codes {
			if code != 201 {
				t.Errorf("HTTP creation: %d", code)
			}
		}
		result := board(t)
		if result.Experts[0].Load != 1 || result.Queued != 1 {
			t.Fatalf("race violated capacity: %+v", result)
		}
		var assigned int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM tickets WHERE assignee_id=$1 AND status='open'`, experts[0].ID).Scan(&assigned); err != nil || assigned != 1 {
			t.Fatalf("assigned count %d %v", assigned, err)
		}
	})
	t.Run("many_concurrent_creations_preserve_total_capacity", func(t *testing.T) {
		resetState(t, 2)
		var wg sync.WaitGroup
		problems := make(chan error, 24)
		for i := 0; i < 24; i++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				_, err := ticketService.Create(ctx, owner, input, fmt.Sprintf("burst-%d", index))
				if err != nil {
					problems <- err
				}
			}(i)
		}
		wg.Wait()
		close(problems)
		for err := range problems {
			t.Error(err)
		}
		result := board(t)
		if result.Queued != 18 {
			t.Fatalf("queue: %d", result.Queued)
		}
		for _, expert := range result.Experts {
			if expert.Load != 2 {
				t.Fatalf("load/capacity: %d/%d", expert.Load, expert.Capacity)
			}
		}
	})
	t.Run("leave_reassigns_all_active_work_with_history", func(t *testing.T) {
		resetState(t, 8)
		first := create(t)
		create(t)
		create(t)
		if first.AssigneeID == nil {
			t.Fatal("initial assignment missing")
		}
		result := board(t)
		var leaving assignment.Expert
		for _, expert := range result.Experts {
			if expert.ID == *first.AssigneeID {
				leaving = expert
			}
		}
		changed := profileInput(leaving)
		changed.OnLeave = true
		if _, err := routing.UpdateExpert(ctx, leaving.ID, managerID, changed); err != nil {
			t.Fatal(err)
		}
		updated, err := ticketService.Get(ctx, owner, first.ID)
		if err != nil || updated.AssigneeID == nil || *updated.AssigneeID == leaving.ID {
			t.Fatalf("leave devolution: %+v %v", updated, err)
		}
		history, err := routing.History(ctx, first.ID, 0, 20)
		if err != nil || len(history) < 2 || history[0].Reason != "expert_leave" {
			t.Fatalf("history: %+v %v", history, err)
		}
		for _, expert := range board(t).Experts {
			if expert.ID == leaving.ID && expert.Load != 0 {
				t.Fatal("leave retained active load")
			}
		}
	})
	t.Run("leave_without_replacement_queues_instead", func(t *testing.T) {
		resetState(t, 8)
		experts := board(t).Experts
		if _, err := pool.Exec(ctx, `UPDATE expert_profiles SET enabled=false WHERE user_id<>$1`, experts[0].ID); err != nil {
			t.Fatal(err)
		}
		ticket := create(t)
		current := board(t).Experts[0]
		changed := profileInput(current)
		changed.OnLeave = true
		if _, err := routing.UpdateExpert(ctx, current.ID, managerID, changed); err != nil {
			t.Fatal(err)
		}
		updated, err := ticketService.Get(ctx, owner, ticket.ID)
		if err != nil || updated.AssigneeID != nil || board(t).Queued != 1 {
			t.Fatal("leave without replacement lost ticket")
		}
		changed.OnLeave = false
		changed.Version++
		if _, err := routing.UpdateExpert(ctx, current.ID, managerID, changed); err != nil {
			t.Fatal(err)
		}
		restored, err := ticketService.Get(ctx, owner, ticket.ID)
		if err != nil || restored.AssigneeID == nil {
			t.Fatal("return from leave did not fill queue")
		}
	})
	t.Run("resolution_releases_capacity_and_reopening_rechecks_it", func(t *testing.T) {
		resetState(t, 1)
		experts := board(t).Experts
		if _, err := pool.Exec(ctx, `UPDATE expert_profiles SET enabled=false WHERE user_id<>$1`, experts[0].ID); err != nil {
			t.Fatal(err)
		}
		first := create(t)
		queued := create(t)
		staff := auth.User{ID: experts[0].ID, Role: "specialist"}
		if _, err := ticketService.UpdateStatus(ctx, staff, first.ID, "in_progress"); err != nil {
			t.Fatal(err)
		}
		if _, err := ticketService.UpdateStatus(ctx, staff, first.ID, "waiting_customer"); err != nil {
			t.Fatal(err)
		}
		if board(t).Experts[0].Load != 1 {
			t.Fatal("waiting ticket stopped consuming capacity")
		}
		if _, err := ticketService.UpdateStatus(ctx, staff, first.ID, "in_progress"); err != nil {
			t.Fatal(err)
		}
		if _, err := ticketService.UpdateStatus(ctx, staff, first.ID, "resolved"); err != nil {
			t.Fatal(err)
		}
		taken, err := ticketService.Get(ctx, owner, queued.ID)
		if err != nil || taken.AssigneeID == nil {
			t.Fatal("released slot not filled")
		}
		reopened, err := ticketService.UpdateStatus(ctx, staff, first.ID, "in_progress")
		if err != nil || reopened.AssigneeID != nil || board(t).Experts[0].Load != 1 || board(t).Queued != 1 {
			t.Fatal("reopen exceeded occupied capacity")
		}
	})
	t.Run("shift_expiry_is_reconciled_by_worker", func(t *testing.T) {
		resetState(t, 8)
		if _, err := pool.Exec(ctx, `UPDATE expert_profiles SET shift_start_minute=510,shift_end_minute=1080,weekdays_mask=31`); err != nil {
			t.Fatal(err)
		}
		ticket := create(t)
		if ticket.AssigneeID == nil {
			t.Fatal("on-shift expert not assigned")
		}
		at = time.Date(2026, 10, 26, 16, 0, 0, 0, time.UTC) // 19:00 Istanbul.
		if _, err := routing.Sweep(ctx, 1000); err != nil {
			t.Fatal(err)
		}
		updated, err := ticketService.Get(ctx, owner, ticket.ID)
		if err != nil || updated.AssigneeID != nil {
			t.Fatal("off-shift assignment retained")
		}
		at = time.Date(2026, 10, 27, 7, 0, 0, 0, time.UTC)
		if _, err := routing.Sweep(ctx, 1000); err != nil {
			t.Fatal(err)
		}
		updated, err = ticketService.Get(ctx, owner, ticket.ID)
		if err != nil || updated.AssigneeID == nil {
			t.Fatal("next shift did not fill queue")
		}
	})

	t.Run("capacity_reduction_moves_excess_work", func(t *testing.T) {
		resetState(t, 8)
		experts := board(t).Experts
		if _, err := pool.Exec(ctx, `UPDATE expert_profiles SET enabled=false WHERE user_id<>$1`, experts[0].ID); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			create(t)
		}
		if _, err := pool.Exec(ctx, `UPDATE expert_profiles SET enabled=true`); err != nil {
			t.Fatal(err)
		}
		current := board(t).Experts[0]
		changed := profileInput(current)
		changed.Capacity = 1
		if _, err := routing.UpdateExpert(ctx, current.ID, managerID, changed); err != nil {
			t.Fatal(err)
		}
		result := board(t)
		if result.Experts[0].Load != 1 || result.Queued != 0 {
			t.Fatal("capacity reduction failed to move excess work")
		}
		total := 0
		for _, expert := range result.Experts {
			total += expert.Load
			if expert.Load > expert.Capacity {
				t.Fatal("replacement capacity exceeded")
			}
		}
		if total != 3 {
			t.Fatal("capacity reduction lost work")
		}
	})
	t.Run("removed_skill_moves_existing_assignments", func(t *testing.T) {
		resetState(t, 8)
		ticket := create(t)
		current := board(t).Experts[0]
		if ticket.AssigneeID == nil || *ticket.AssigneeID != current.ID {
			t.Fatal("fixture assignment missing")
		}
		changed := profileInput(current)
		changed.Skills = []assignment.Skill{{Code: "network", Level: 5}}
		if _, err := routing.UpdateExpert(ctx, current.ID, managerID, changed); err != nil {
			t.Fatal(err)
		}
		updated, err := ticketService.Get(ctx, owner, ticket.ID)
		if err != nil || updated.AssigneeID == nil || *updated.AssigneeID == current.ID {
			t.Fatal("removed required skill did not trigger reassignment")
		}
	})
	t.Run("stale_profile_and_invalid_skill_roll_back", func(t *testing.T) {
		resetState(t, 8)
		chosen := board(t).Experts[0]
		changed := profileInput(chosen)
		changed.Capacity = 6
		if _, err := routing.UpdateExpert(ctx, chosen.ID, managerID, changed); err != nil {
			t.Fatal(err)
		}
		if _, err := routing.UpdateExpert(ctx, chosen.ID, managerID, changed); err == nil {
			t.Fatal("stale profile accepted")
		}
		current := board(t).Experts[0]
		bad := profileInput(current)
		bad.Skills = []assignment.Skill{{Code: "missing", Level: 5}}
		if _, err := routing.UpdateExpert(ctx, current.ID, managerID, bad); err == nil {
			t.Fatal("invalid skill accepted")
		}
		if board(t).Experts[0].Version != current.Version {
			t.Fatal("invalid update committed")
		}
	})
	t.Run("history_is_append_only_and_retries_do_not_churn", func(t *testing.T) {
		resetState(t, 8)
		ticket := create(t)
		if err := routing.Retry(ctx, ticket.ID, managerID); err != nil {
			t.Fatal(err)
		}
		if err := routing.Retry(ctx, ticket.ID, managerID); err != nil {
			t.Fatal(err)
		}
		history, err := routing.History(ctx, ticket.ID, 0, 20)
		if err != nil || len(history) != 1 {
			t.Fatal("retries churned assignment history")
		}
		var score assignment.Score
		if err := json.Unmarshal(history[0].Score, &score); err != nil || score.Total == 0 {
			t.Fatal("decision score missing")
		}
		for _, query := range []string{`UPDATE assignment_history SET reason='tamper'`, `DELETE FROM assignment_history`, `TRUNCATE assignment_history`} {
			if _, err := pool.Exec(ctx, query); err == nil {
				t.Fatal("assignment history mutation accepted")
			}
		}
	})
	t.Run("queue_filters_are_scoped", func(t *testing.T) {
		resetState(t, 1)
		experts := board(t).Experts
		for i := 0; i < 5; i++ {
			create(t)
		}
		staff := auth.User{ID: experts[0].ID, Role: "specialist"}
		mine, err := ticketService.List(ctx, staff, tickets.ListOptions{Limit: 100, Queue: "mine", Status: "open"})
		if err != nil || len(mine) != 1 {
			t.Fatalf("mine filter: %v", err)
		}
		waiting, err := ticketService.List(ctx, staff, tickets.ListOptions{Limit: 100, Queue: "unassigned"})
		if err != nil || len(waiting) != 2 {
			t.Fatalf("queue filter: %v", err)
		}
		if _, err := ticketService.List(ctx, owner, tickets.ListOptions{Limit: 20, Queue: "unassigned"}); err == nil {
			t.Fatal("requester accessed shared queue")
		}
	})
}
