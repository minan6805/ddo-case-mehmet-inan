package assets_test

import (
	"destekmasa/internal/assets"
	"destekmasa/internal/auth"
	"destekmasa/internal/database"
	"destekmasa/internal/testdb"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

func TestPostgresAssetGraph(t *testing.T) {
	ctx, pool, users := testdb.New(t, "asset_test")
	service := assets.New(pool)
	manager := users["varlik@destekmasa.local"]
	staff := users["uzman@destekmasa.local"]
	employee := users["calisan@destekmasa.local"]
	var counter atomic.Int64
	create := func(t *testing.T, kind string) assets.Asset {
		t.Helper()
		n := counter.Add(1)
		a, err := service.Create(ctx, manager, assets.Input{Code: fmt.Sprintf("TEST-%d", n), Name: "Test varlığı", Kind: kind, State: "active", OwnerID: &manager.ID}, fmt.Sprint(n))
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	link := func(t *testing.T, a, b int64) {
		t.Helper()
		if err := service.Dependency(ctx, manager, a, b, true); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("seed_chain_is_repeatable_and_preserves_edits", func(t *testing.T) {
		var root, child int64
		if err := pool.QueryRow(ctx, `SELECT id FROM assets WHERE code='DEMO-NET-01'`).Scan(&root); err != nil {
			t.Fatal(err)
		}
		impact, err := service.Impact(ctx, staff, []int64{root})
		if err != nil || len(impact.Items) != 4 {
			t.Fatalf("%+v %v", impact, err)
		}
		child = impact.Items[1].ID
		if err = service.Dependency(ctx, manager, root, child, false); err != nil {
			t.Fatal(err)
		}
		if err = database.SeedDemo(ctx, pool, "OtherDemoPassword2026!"); err != nil {
			t.Fatal(err)
		}
		impact, err = service.Impact(ctx, staff, []int64{root})
		if err != nil || len(impact.Items) != 1 {
			t.Fatalf("seed reset graph %+v %v", impact, err)
		}
	})
	t.Run("create_update_owner_and_audit", func(t *testing.T) {
		a := create(t, "server")
		a.Name = "Güncel sunucu"
		next, err := service.Update(ctx, manager, a.ID, assets.Input{Code: a.Code, Name: a.Name, Kind: a.Kind, State: "maintenance", OwnerID: &employee.ID, Version: a.Version})
		if err != nil || next.Version != 2 || next.OwnerName != employee.DisplayName {
			t.Fatalf("%+v %v", next, err)
		}
		var count int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE entity_type='asset' AND entity_id=$1`, a.ID).Scan(&count); err != nil || count != 2 {
			t.Fatalf("audit %d %v", count, err)
		}
	})
	t.Run("role_boundaries", func(t *testing.T) {
		a := create(t, "device")
		if _, err := service.Detail(ctx, employee, a.ID); !errors.Is(err, assets.ErrForbidden) {
			t.Fatal(err)
		}
		for _, u := range []auth.User{staff, users["lider@destekmasa.local"], employee} {
			if _, err := service.Create(ctx, u, assets.Input{}, "role"); !errors.Is(err, assets.ErrForbidden) {
				t.Fatal(err)
			}
			if err := service.Dependency(ctx, u, a.ID, a.ID+1, true); !errors.Is(err, assets.ErrForbidden) {
				t.Fatal(err)
			}
		}
		if _, err := service.Detail(ctx, staff, a.ID); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("concurrent_create_retry_is_once", func(t *testing.T) {
		in := assets.Input{Code: "RETRY-ASSET", Name: "Retry", Kind: "server", State: "active"}
		var wg sync.WaitGroup
		ids := make(chan int64, 8)
		failures := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				a, err := service.Create(ctx, manager, in, "asset-retry")
				if err != nil {
					failures <- err
				} else {
					ids <- a.ID
				}
			}()
		}
		wg.Wait()
		close(ids)
		close(failures)
		for err := range failures {
			t.Fatal(err)
		}
		var first int64
		for id := range ids {
			if first == 0 {
				first = id
			}
			if first != id {
				t.Fatal("duplicate")
			}
		}
		in.Name = "Farklı"
		if _, err := service.Create(ctx, manager, in, "asset-retry"); !errors.Is(err, assets.ErrConflict) {
			t.Fatal(err)
		}
	})
	t.Run("duplicate_code_and_invalid_owner_roll_back", func(t *testing.T) {
		a := create(t, "server")
		in := assets.Input{Code: a.Code, Name: "Aynı kod", Kind: "server", State: "active"}
		if _, err := service.Create(ctx, manager, in, "duplicate"); !errors.Is(err, assets.ErrConflict) {
			t.Fatal(err)
		}
		missing := int64(9223372036854775807)
		in.Code = "BAD-OWNER"
		in.OwnerID = &missing
		if _, err := service.Create(ctx, manager, in, "bad-owner"); !errors.Is(err, assets.ErrInvalid) {
			t.Fatal(err)
		}
		in.Code = "!"
		if _, err := service.Create(ctx, manager, in, "bad-code"); !errors.Is(err, assets.ErrInvalid) {
			t.Fatal(err)
		}
	})
	t.Run("optimistic_update_race_one_wins", func(t *testing.T) {
		a := create(t, "server")
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, err := service.Update(ctx, manager, a.ID, assets.Input{Code: a.Code, Name: fmt.Sprintf("Name %d", i), Kind: a.Kind, State: a.State, Version: a.Version})
				results <- err
			}(i)
		}
		wg.Wait()
		close(results)
		ok, conflict := 0, 0
		for err := range results {
			if err == nil {
				ok++
			} else if errors.Is(err, assets.ErrConflict) {
				conflict++
			} else {
				t.Fatal(err)
			}
		}
		if ok != 1 || conflict != 1 {
			t.Fatalf("%d/%d", ok, conflict)
		}
	})
	t.Run("cycles_diamonds_and_direction", func(t *testing.T) {
		a, b, c, d, e := create(t, "server"), create(t, "service"), create(t, "service"), create(t, "application"), create(t, "device")
		for _, edge := range [][2]int64{{a.ID, b.ID}, {a.ID, c.ID}, {b.ID, d.ID}, {c.ID, d.ID}, {d.ID, a.ID}, {d.ID, e.ID}} {
			link(t, edge[0], edge[1])
		}
		impact, err := service.Impact(ctx, staff, []int64{a.ID})
		if err != nil || len(impact.Items) != 5 {
			t.Fatalf("%+v %v", impact, err)
		}
		for _, n := range impact.Items {
			if n.ID == d.ID && n.Distance != 2 {
				t.Fatal("not shortest")
			}
		}
		impact, err = service.Impact(ctx, staff, []int64{e.ID})
		if err != nil || len(impact.Items) != 1 {
			t.Fatalf("upstream affected %+v %v", impact, err)
		}
		impact, err = service.Impact(ctx, staff, []int64{a.ID, e.ID})
		if err != nil || len(impact.Items) != 5 {
			t.Fatalf("multiroot %+v %v", impact, err)
		}
	})
	t.Run("edge_retries_do_not_duplicate_audit_or_revision", func(t *testing.T) {
		a, b := create(t, "server"), create(t, "service")
		before, err := service.Impact(ctx, staff, []int64{a.ID})
		if err != nil {
			t.Fatal(err)
		}
		link(t, a.ID, b.ID)
		link(t, a.ID, b.ID)
		after, err := service.Impact(ctx, staff, []int64{a.ID})
		if err != nil || after.GraphVersion != before.GraphVersion+1 {
			t.Fatalf("%+v %v", after, err)
		}
		for i := 0; i < 2; i++ {
			if err = service.Dependency(ctx, manager, a.ID, b.ID, false); err != nil {
				t.Fatal(err)
			}
		}
		after, err = service.Impact(ctx, staff, []int64{a.ID})
		if err != nil || after.GraphVersion != before.GraphVersion+2 {
			t.Fatalf("%+v %v", after, err)
		}
		if err := service.Dependency(ctx, manager, a.ID, a.ID, true); !errors.Is(err, assets.ErrInvalid) {
			t.Fatal(err)
		}
		if err := service.Dependency(ctx, manager, a.ID, 9223372036854775807, true); !errors.Is(err, assets.ErrNotFound) {
			t.Fatal(err)
		}
	})
	t.Run("keyset_filter_and_detail_direction", func(t *testing.T) {
		a, b := create(t, "network"), create(t, "device")
		link(t, a.ID, b.ID)
		d, err := service.Detail(ctx, staff, b.ID)
		if err != nil || len(d.Providers) != 1 || d.Providers[0].ID != a.ID || len(d.Dependents) != 0 {
			t.Fatalf("%+v %v", d, err)
		}
		items, err := service.List(ctx, staff, 0, 1, "network")
		if err != nil || len(items) == 0 || items[0].Kind != "network" {
			t.Fatalf("%+v %v", items, err)
		}
		next, err := service.List(ctx, staff, items[0].ID, 100, "network")
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range next {
			if n.ID >= items[0].ID || n.Kind != "network" {
				t.Fatal("bad page")
			}
		}
	})
	t.Run("audit_failure_rolls_back_asset_and_graph_version", func(t *testing.T) {
		_, err := pool.Exec(ctx, `CREATE FUNCTION fail_asset_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='asset.create' THEN RAISE EXCEPTION 'fixture failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_asset_audit BEFORE INSERT ON audit_log FOR EACH ROW EXECUTE FUNCTION fail_asset_audit()`)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := pool.Exec(ctx, `DROP TRIGGER fail_asset_audit ON audit_log; DROP FUNCTION fail_asset_audit()`); err != nil {
				t.Error(err)
			}
		}()
		var before, after int64
		if err = pool.QueryRow(ctx, `SELECT version FROM asset_graph_revision`).Scan(&before); err != nil {
			t.Fatal(err)
		}
		if _, err = service.Create(ctx, manager, assets.Input{Code: "ROLLBACK-ASSET", Name: "Rollback", Kind: "server", State: "active"}, "rollback"); err == nil {
			t.Fatal("failure ignored")
		}
		var count int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM assets WHERE code='ROLLBACK-ASSET'`).Scan(&count); err != nil || count != 0 {
			t.Fatal("partial asset")
		}
		if err = pool.QueryRow(ctx, `SELECT version FROM asset_graph_revision`).Scan(&after); err != nil || before != after {
			t.Fatal("partial revision")
		}
	})
}
