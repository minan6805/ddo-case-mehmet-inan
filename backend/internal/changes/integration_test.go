package changes_test

import (
	"destekmasa/internal/assets"
	"destekmasa/internal/auth"
	"destekmasa/internal/changes"
	"destekmasa/internal/httpapi"
	"destekmasa/internal/testdb"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPostgresChangeWorkflow(t *testing.T) {
	ctx, pool, users := testdb.New(t, "change_test")
	owner := users["varlik@destekmasa.local"]
	cab := users["lider@destekmasa.local"]
	admin := users["admin@destekmasa.local"]
	employee := users["calisan@destekmasa.local"]
	base := time.Date(2030, 1, 2, 10, 0, 0, 0, time.UTC)
	var clock atomic.Int64
	clock.Store(base.UnixNano())
	service := changes.New(pool, func() time.Time { return time.Unix(0, clock.Load()).UTC() })
	inventory := assets.New(pool)
	var serial atomic.Int64
	asset := func(t *testing.T) assets.Asset {
		t.Helper()
		n := serial.Add(1)
		a, err := inventory.Create(ctx, owner, assets.Input{Code: fmt.Sprintf("CH-CI-%d", n), Name: "Değişiklik varlığı", Kind: "server", State: "active"}, fmt.Sprint(n))
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	input := func(ids ...int64) changes.Input {
		n := serial.Add(1)
		start := base.Add(time.Duration(n) * 24 * time.Hour)
		return changes.Input{Title: "Planlı bakım", Description: "Güncelleme uygulanacak.", RollbackPlan: "Eski sürüme dönülecek.", Risk: "normal", StartsAt: start, EndsAt: start.Add(time.Hour), RootIDs: ids}
	}
	create := func(t *testing.T, in changes.Input) changes.Change {
		t.Helper()
		c, err := service.Create(ctx, owner, in, fmt.Sprintf("create-%d", serial.Add(1)))
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	act := func(t *testing.T, user auth.User, c changes.Change, action string) changes.Change {
		t.Helper()
		next, err := service.Action(ctx, user, c.ID, changes.ActionInput{Action: action, Note: "Test işlem notu", Version: c.Version}, fmt.Sprintf("action-%d", serial.Add(1)))
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		return next
	}
	t.Run("draft_preview_and_independent_cab_lifecycle", func(t *testing.T) {
		a, b := asset(t), asset(t)
		if err := inventory.Dependency(ctx, owner, a.ID, b.ID, true); err != nil {
			t.Fatal(err)
		}
		c := create(t, input(a.ID))
		preview, err := service.Preview(ctx, owner, c.ID)
		if err != nil || len(preview.Impact.Items) != 2 || len(preview.Conflicts) != 0 {
			t.Fatalf("%+v %v", preview, err)
		}
		c = act(t, owner, c, "submit")
		c = act(t, cab, c, "approve")
		if c.ApprovedBy == nil || *c.ApprovedBy != cab.ID {
			t.Fatal(c)
		}
		clock.Store(c.StartsAt.Add(time.Minute).UnixNano())
		defer clock.Store(base.UnixNano())
		c = act(t, owner, c, "start")
		c = act(t, owner, c, "complete")
		if c.Status != "completed" {
			t.Fatal(c)
		}
		stored, err := service.Get(ctx, owner, c.ID)
		if err != nil || stored.Impact == nil || len(stored.Impact.Items) != 2 {
			t.Fatalf("%+v %v", stored, err)
		}
	})
	t.Run("self_approval_and_employee_access_rejected", func(t *testing.T) {
		a := asset(t)
		in := input(a.ID)
		c := create(t, in)
		c = act(t, owner, c, "submit")
		if _, err := service.Action(ctx, owner, c.ID, changes.ActionInput{Action: "approve", Version: c.Version, Note: "Kendim onaylıyorum"}, "self"); !errors.Is(err, changes.ErrForbidden) {
			t.Fatal(err)
		}
		own, err := service.Create(ctx, admin, in, "admin-own")
		if err != nil {
			t.Fatal(err)
		}
		own = act(t, admin, own, "submit")
		if _, err = service.Action(ctx, admin, own.ID, changes.ActionInput{Action: "approve", Version: own.Version, Note: "Kendi onayım"}, "admin-self"); !errors.Is(err, changes.ErrForbidden) {
			t.Fatal(err)
		}
		if _, err := service.Get(ctx, employee, c.ID); !errors.Is(err, changes.ErrForbidden) {
			t.Fatal(err)
		}
		if _, err := service.Create(ctx, employee, in, "employee"); !errors.Is(err, changes.ErrForbidden) {
			t.Fatal(err)
		}
	})
	t.Run("only_owner_or_admin_edits_drafts", func(t *testing.T) {
		a := asset(t)
		in := input(a.ID)
		c := create(t, in)
		in.Version = c.Version
		in.Title = "Yeni başlık"
		if _, err := service.Update(ctx, cab, c.ID, in); !errors.Is(err, changes.ErrForbidden) {
			t.Fatal(err)
		}
		next, err := service.Update(ctx, owner, c.ID, in)
		if err != nil || next.Version != c.Version+1 {
			t.Fatalf("%+v %v", next, err)
		}
		if _, err = service.Update(ctx, owner, c.ID, in); !errors.Is(err, changes.ErrConflict) {
			t.Fatal(err)
		}
		next = act(t, owner, next, "submit")
		in.Version = next.Version
		if _, err = service.Update(ctx, owner, next.ID, in); !errors.Is(err, changes.ErrTransition) {
			t.Fatal(err)
		}
	})
	t.Run("shared_downstream_asset_conflicts_across_distinct_roots", func(t *testing.T) {
		a, b, c := asset(t), asset(t), asset(t)
		for _, e := range [][2]int64{{a.ID, c.ID}, {b.ID, c.ID}, {c.ID, a.ID}} {
			if err := inventory.Dependency(ctx, owner, e[0], e[1], true); err != nil {
				t.Fatal(err)
			}
		}
		in := input(a.ID)
		first := create(t, in)
		in.RootIDs = []int64{b.ID}
		second := create(t, in)
		first = act(t, owner, first, "submit")
		second = act(t, owner, second, "submit")
		first = act(t, cab, first, "approve")
		_, err := service.Action(ctx, cab, second.ID, changes.ActionInput{Action: "approve", Note: "İkinci onay", Version: second.Version}, "shared-approve")
		var overlap *changes.ConflictError
		if !errors.As(err, &overlap) || len(overlap.Items) != 1 || overlap.Items[0].ID != first.ID {
			t.Fatalf("%+v %v", overlap, err)
		}
		preview, err := service.Preview(ctx, owner, second.ID)
		if err != nil || len(preview.Conflicts) != 1 {
			t.Fatalf("%+v %v", preview, err)
		}
	})
	t.Run("touching_windows_and_disjoint_assets_are_allowed", func(t *testing.T) {
		a, b := asset(t), asset(t)
		in := input(a.ID)
		first := create(t, in)
		in.RootIDs = []int64{b.ID}
		other := create(t, in)
		in.RootIDs = []int64{a.ID}
		in.StartsAt = in.EndsAt
		in.EndsAt = in.StartsAt.Add(time.Hour)
		adjacent := create(t, in)
		for _, c := range []changes.Change{first, other, adjacent} {
			c = act(t, owner, c, "submit")
			c = act(t, cab, c, "approve")
			if c.Status != "approved" {
				t.Fatal(c)
			}
		}
	})
	t.Run("two_concurrent_http_approvals_only_one_reservation", func(t *testing.T) {
		a := asset(t)
		in := input(a.ID)
		one, two := create(t, in), create(t, in)
		one = act(t, owner, one, "submit")
		two = act(t, owner, two, "submit")
		authService, err := auth.New(pool, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		session, err := authService.Login(ctx, cab.Email, "DemoDestek2026!", "")
		if err != nil {
			t.Fatal(err)
		}
		router := httpapi.NewServicesRouter(httpapi.Services{Database: pool, Authentication: authService, Changes: service})
		var wg sync.WaitGroup
		results := make(chan int, 2)
		for _, c := range []changes.Change{one, two} {
			wg.Add(1)
			go func(c changes.Change) {
				defer wg.Done()
				body, _ := json.Marshal(changes.ActionInput{Action: "approve", Note: "Eşzamanlı CAB onayı", Version: c.Version})
				r := httptest.NewRequest("POST", fmt.Sprintf("/changes/%d/actions", c.ID), strings.NewReader(string(body)))
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Idempotency-Key", fmt.Sprintf("http-approval-%d", c.ID))
				r.AddCookie(&http.Cookie{Name: "destekmasa_session", Value: session.Token})
				w := httptest.NewRecorder()
				router.ServeHTTP(w, r)
				results <- w.Code
			}(c)
		}
		wg.Wait()
		close(results)
		ok, conflict := 0, 0
		for status := range results {
			switch status {
			case 200:
				ok++
			case 409:
				conflict++
			default:
				t.Fatalf("HTTP %d", status)
			}
		}
		if ok != 1 || conflict != 1 {
			t.Fatalf("%d/%d", ok, conflict)
		}
		var count int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM change_requests WHERE id=ANY($1::bigint[]) AND status='approved'`, []int64{one.ID, two.ID}).Scan(&count); err != nil || count != 1 {
			t.Fatalf("reservations %d %v", count, err)
		}
	})
	t.Run("graph_change_requires_new_review_before_approval", func(t *testing.T) {
		a, b := asset(t), asset(t)
		c := create(t, input(a.ID))
		c = act(t, owner, c, "submit")
		if err := inventory.Dependency(ctx, owner, a.ID, b.ID, true); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Action(ctx, cab, c.ID, changes.ActionInput{Action: "approve", Note: "Eski graf onayı", Version: c.Version}, "stale-approval"); !errors.Is(err, changes.ErrStaleGraph) {
			t.Fatal(err)
		}
		c = act(t, owner, c, "revise")
		c = act(t, owner, c, "submit")
		c = act(t, cab, c, "approve")
		stored, err := service.Get(ctx, owner, c.ID)
		if err != nil || stored.Impact == nil || len(stored.Impact.Items) != 2 {
			t.Fatalf("%+v %v", stored, err)
		}
	})
	t.Run("approved_graph_change_blocks_start_and_allows_revision", func(t *testing.T) {
		a, b := asset(t), asset(t)
		c := create(t, input(a.ID))
		c = act(t, owner, c, "submit")
		c = act(t, cab, c, "approve")
		if err := inventory.Dependency(ctx, owner, a.ID, b.ID, true); err != nil {
			t.Fatal(err)
		}
		clock.Store(c.StartsAt.Add(time.Minute).UnixNano())
		defer clock.Store(base.UnixNano())
		if _, err := service.Action(ctx, owner, c.ID, changes.ActionInput{Action: "start", Note: "Eski grafla uygulama", Version: c.Version}, "stale-start"); !errors.Is(err, changes.ErrStaleGraph) {
			t.Fatal(err)
		}
		c = act(t, owner, c, "revise")
		if c.Status != "draft" || c.ApprovedBy != nil || c.GraphVersion != nil {
			t.Fatal(c)
		}
	})
	t.Run("stale_reservation_conservatively_blocks_overlapping_window", func(t *testing.T) {
		a, b := asset(t), asset(t)
		in := input(a.ID)
		first := create(t, in)
		first = act(t, owner, first, "submit")
		first = act(t, cab, first, "approve")
		if _, err := inventory.Update(ctx, owner, a.ID, assets.Input{Code: a.Code, Name: "Güncel ad", Kind: a.Kind, State: a.State, Version: a.Version}); err != nil {
			t.Fatal(err)
		}
		in.RootIDs = []int64{b.ID}
		second := create(t, in)
		if _, err := service.Action(ctx, owner, second.ID, changes.ActionInput{Action: "submit", Note: "Ayrı kök yeni graf", Version: second.Version}, "stale-reservation"); !errors.Is(err, changes.ErrOverlap) {
			t.Fatal(err)
		}
		first = act(t, owner, first, "cancel")
		second = act(t, owner, second, "submit")
		second = act(t, cab, second, "approve")
	})
	t.Run("decision_retries_and_expired_create_replay_are_stable", func(t *testing.T) {
		a := asset(t)
		in := input(a.ID)
		c, err := service.Create(ctx, owner, in, "stable-create")
		if err != nil {
			t.Fatal(err)
		}
		action := changes.ActionInput{Action: "submit", Note: "Tek sefer gönderim", Version: c.Version}
		next, err := service.Action(ctx, owner, c.ID, action, "stable-submit")
		if err != nil {
			t.Fatal(err)
		}
		replay, err := service.Action(ctx, owner, c.ID, action, "stable-submit")
		if err != nil || replay.Version != next.Version {
			t.Fatalf("%+v %v", replay, err)
		}
		action.Note = "Farklı not"
		if _, err := service.Action(ctx, owner, c.ID, action, "stable-submit"); !errors.Is(err, changes.ErrConflict) {
			t.Fatal(err)
		}
		clock.Store(in.EndsAt.Add(time.Hour).UnixNano())
		defer clock.Store(base.UnixNano())
		replay, err = service.Create(ctx, owner, in, "stable-create")
		if err != nil || replay.ID != c.ID {
			t.Fatalf("late replay %+v %v", replay, err)
		}
	})
	t.Run("request_changes_rejection_and_execution_window", func(t *testing.T) {
		a := asset(t)
		c := create(t, input(a.ID))
		c = act(t, owner, c, "submit")
		c = act(t, cab, c, "request_changes")
		if c.Status != "draft" {
			t.Fatal(c)
		}
		c = act(t, owner, c, "submit")
		c = act(t, cab, c, "approve")
		if _, err := service.Action(ctx, owner, c.ID, changes.ActionInput{Action: "start", Version: c.Version, Note: "Çok erken"}, "early-start"); !errors.Is(err, changes.ErrTransition) {
			t.Fatal(err)
		}
		c = act(t, owner, c, "cancel")
		other := create(t, input(a.ID))
		other = act(t, owner, other, "submit")
		other = act(t, cab, other, "reject")
		if other.Status != "rejected" {
			t.Fatal(other)
		}

		failed := create(t, input(a.ID))
		failed = act(t, owner, failed, "submit")
		failed = act(t, cab, failed, "approve")
		clock.Store(failed.StartsAt.Add(time.Minute).UnixNano())
		defer clock.Store(base.UnixNano())
		failed = act(t, owner, failed, "start")
		failed = act(t, owner, failed, "fail")
		if failed.Status != "failed" {
			t.Fatal(failed)
		}
	})
	t.Run("input_validation_retired_roots_and_missing_plan", func(t *testing.T) {
		a := asset(t)
		in := input(a.ID)
		bad := in
		bad.RootIDs = []int64{a.ID, a.ID}
		if _, err := service.Create(ctx, owner, bad, "bad"); !errors.Is(err, changes.ErrInvalid) {
			t.Fatal(err)
		}
		bad = in
		bad.RollbackPlan = " "
		if _, err := service.Create(ctx, owner, bad, "bad"); !errors.Is(err, changes.ErrInvalid) {
			t.Fatal(err)
		}
		bad = in
		bad.EndsAt = bad.StartsAt
		if _, err := service.Create(ctx, owner, bad, "bad"); !errors.Is(err, changes.ErrInvalid) {
			t.Fatal(err)
		}
		if _, err := inventory.Update(ctx, owner, a.ID, assets.Input{Code: a.Code, Name: a.Name, Kind: a.Kind, State: "retired", Version: a.Version}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Create(ctx, owner, in, "retired-root"); !errors.Is(err, changes.ErrInvalid) {
			t.Fatal(err)
		}
		if _, err := service.Get(ctx, owner, 9223372036854775807); !errors.Is(err, changes.ErrNotFound) {
			t.Fatal(err)
		}
	})
	t.Run("history_pagination_and_immutability", func(t *testing.T) {
		a := asset(t)
		c := create(t, input(a.ID))
		c = act(t, owner, c, "submit")
		items, err := service.History(ctx, cab, c.ID, 0, 1)
		if err != nil || len(items) != 2 || items[0].Action != "submit" {
			t.Fatalf("%+v %v", items, err)
		}
		next, err := service.History(ctx, cab, c.ID, items[0].ID, 1)
		if err != nil || len(next) != 1 || next[0].Action != "create" {
			t.Fatalf("%+v %v", next, err)
		}
		for _, query := range []string{`UPDATE change_events SET note='changed' WHERE id=$1`, `DELETE FROM change_events WHERE id=$1`} {
			if _, err := pool.Exec(ctx, query, items[0].ID); err == nil {
				t.Fatal("history changed")
			}
		}
		if _, err := service.History(ctx, employee, c.ID, 0, 20); !errors.Is(err, changes.ErrForbidden) {
			t.Fatal(err)
		}
	})
	t.Run("failed_approval_audit_rolls_back_reservation", func(t *testing.T) {
		a := asset(t)
		c := create(t, input(a.ID))
		c = act(t, owner, c, "submit")
		_, err := pool.Exec(ctx, `CREATE FUNCTION fail_change_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='change.approve' THEN RAISE EXCEPTION 'fixture failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_change_audit BEFORE INSERT ON audit_log FOR EACH ROW EXECUTE FUNCTION fail_change_audit()`)
		if err != nil {
			t.Fatal(err)
		}
		in := changes.ActionInput{Action: "approve", Note: "Geri alınacak onay", Version: c.Version}
		if _, err = service.Action(ctx, cab, c.ID, in, "rollback-approval"); err == nil {
			t.Fatal("failure ignored")
		}
		stored, err := service.Get(ctx, owner, c.ID)
		if err != nil || stored.Status != "submitted" || stored.ApprovedBy != nil || stored.Version != c.Version {
			t.Fatalf("partial %+v %v", stored, err)
		}
		if _, err = pool.Exec(ctx, `DROP TRIGGER fail_change_audit ON audit_log; DROP FUNCTION fail_change_audit()`); err != nil {
			t.Fatal(err)
		}
		if _, err = service.Action(ctx, cab, c.ID, in, "rollback-approval"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("list_filter_and_create_retries", func(t *testing.T) {
		a := asset(t)
		in := input(a.ID)
		var wg sync.WaitGroup
		results := make(chan int64, 4)
		failures := make(chan error, 4)
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				c, err := service.Create(ctx, owner, in, "concurrent-create")
				if err != nil {
					failures <- err
				} else {
					results <- c.ID
				}
			}()
		}
		wg.Wait()
		close(results)
		close(failures)
		for err := range failures {
			t.Fatal(err)
		}
		var id int64
		for next := range results {
			if id == 0 {
				id = next
			}
			if id != next {
				t.Fatal("duplicate draft")
			}
		}
		list, err := service.List(ctx, owner, 0, 1, "draft")
		if err != nil || len(list) == 0 {
			t.Fatalf("%+v %v", list, err)
		}
		for _, c := range list {
			if c.Status != "draft" {
				t.Fatal("wrong filter")
			}
		}
		more, err := service.List(ctx, owner, list[0].ID, 100, "draft")
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range more {
			if c.ID >= list[0].ID || c.Status != "draft" {
				t.Fatal("bad cursor")
			}
		}
	})
}
