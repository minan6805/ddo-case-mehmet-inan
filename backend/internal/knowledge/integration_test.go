package knowledge_test

import (
	"bytes"
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

	"destekmasa/internal/auth"
	"destekmasa/internal/database"
	"destekmasa/internal/httpapi"
	"destekmasa/internal/knowledge"
	"destekmasa/internal/testdb"
)

func TestPostgresKnowledge(t *testing.T) {
	ctx, pool, users := testdb.New(t, "knowledge_test")
	service := knowledge.New(pool)
	staff := users["uzman@destekmasa.local"]
	other := users["uzman2@destekmasa.local"]
	leader := users["lider@destekmasa.local"]
	employee := users["calisan@destekmasa.local"]
	manager := users["varlik@destekmasa.local"]
	var serial atomic.Int64
	input := func() knowledge.Input {
		return knowledge.Input{Title: "VPN bağlantı çözümü", Body: "Bağlantıyı kontrol edin. <script>alert(1)</script>", Category: "network", Status: "draft"}
	}
	create := func(t *testing.T) knowledge.Article {
		t.Helper()
		a, err := service.Create(ctx, staff, input(), fmt.Sprint(serial.Add(1)))
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	edit := func(a knowledge.Article, status string) knowledge.Input {
		return knowledge.Input{Title: a.Title, Body: a.Body, Category: a.Category, Status: status, Version: a.Version}
	}
	save := func(t *testing.T, user auth.User, a knowledge.Article, status string) knowledge.Article {
		t.Helper()
		a, err := service.Update(ctx, user, a.ID, edit(a, status))
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	t.Run("seed_is_repeatable_and_preserves_edits", func(t *testing.T) {
		list, err := service.List(ctx, employee, knowledge.Filter{Limit: 100})
		if err != nil || len(list) != 3 {
			t.Fatalf("%+v %v", list, err)
		}
		a := save(t, leader, list[0], "draft")
		in := edit(a, "draft")
		in.Title = "Korunan demo düzenlemesi"
		a, err = service.Update(ctx, leader, a.ID, in)
		if err != nil {
			t.Fatal(err)
		}
		if err = database.SeedDemo(ctx, pool, "OtherDemoPassword2026!"); err != nil {
			t.Fatal(err)
		}
		next, err := service.Detail(ctx, leader, a.ID)
		if err != nil || next.Title != in.Title || next.Status != "draft" {
			t.Fatalf("%+v %v", next, err)
		}
	})
	t.Run("draft_publish_archive_and_audit", func(t *testing.T) {
		a := create(t)
		if _, err := service.Detail(ctx, employee, a.ID); !errors.Is(err, knowledge.ErrNotFound) {
			t.Fatal(err)
		}
		a = save(t, leader, a, "published")
		if _, err := service.Detail(ctx, employee, a.ID); err != nil {
			t.Fatal(err)
		}
		a = save(t, leader, a, "archived")
		if _, err := service.Detail(ctx, employee, a.ID); !errors.Is(err, knowledge.ErrNotFound) {
			t.Fatal(err)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE entity_type='knowledge_article' AND entity_id=$1`, a.ID).Scan(&count); err != nil || count != 3 {
			t.Fatalf("audit %d %v", count, err)
		}
	})
	t.Run("role_and_author_boundaries", func(t *testing.T) {
		a := create(t)
		for _, u := range []auth.User{employee, manager} {
			if _, err := service.Create(ctx, u, input(), "forbidden"); !errors.Is(err, knowledge.ErrForbidden) {
				t.Fatal(err)
			}
			if _, err := service.Update(ctx, u, a.ID, edit(a, "draft")); !errors.Is(err, knowledge.ErrForbidden) {
				t.Fatal(err)
			}
		}
		for _, pair := range []struct {
			u      auth.User
			status string
		}{{other, "draft"}, {staff, "published"}, {staff, "archived"}} {
			if _, err := service.Update(ctx, pair.u, a.ID, edit(a, pair.status)); !errors.Is(err, knowledge.ErrForbidden) {
				t.Fatal(err)
			}
		}
		a = save(t, staff, a, "draft")
		a = save(t, leader, a, "published")
		if _, err := service.Update(ctx, staff, a.ID, edit(a, "draft")); !errors.Is(err, knowledge.ErrForbidden) {
			t.Fatal(err)
		}
	})
	t.Run("published_revision_requires_new_review", func(t *testing.T) {
		a := save(t, leader, create(t), "published")
		in := edit(a, "published")
		in.Body = "Onaysız yeni içerik"
		if _, err := service.Update(ctx, leader, a.ID, in); !errors.Is(err, knowledge.ErrInvalid) {
			t.Fatal(err)
		}
		a = save(t, leader, a, "draft")
		in = edit(a, "draft")
		in.Body = "Yeni çözüm"
		a, err := service.Update(ctx, staff, a.ID, in)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = service.Detail(ctx, employee, a.ID); !errors.Is(err, knowledge.ErrNotFound) {
			t.Fatal(err)
		}
		a = save(t, leader, a, "published")
		got, err := service.Detail(ctx, employee, a.ID)
		if err != nil || got.Body != "Yeni çözüm" {
			t.Fatalf("%+v %v", got, err)
		}
		a = save(t, leader, a, "archived")
		if _, err = service.Update(ctx, leader, a.ID, edit(a, "published")); !errors.Is(err, knowledge.ErrInvalid) {
			t.Fatal(err)
		}
	})
	t.Run("literal_search_category_pagination_and_hidden_content", func(t *testing.T) {
		a := create(t)
		in := edit(a, "draft")
		in.Title = "Unique SEARCH%_"
		in.Body = "literalneedle"
		a, err := service.Update(ctx, staff, a.ID, in)
		if err != nil {
			t.Fatal(err)
		}
		list, err := service.List(ctx, employee, knowledge.Filter{Limit: 100, Query: "literalneedle"})
		if err != nil || len(list) != 0 {
			t.Fatalf("draft leaked %+v %v", list, err)
		}
		a = save(t, leader, a, "published")
		for _, q := range []string{"literalneedle", "search%_"} {
			list, err = service.List(ctx, employee, knowledge.Filter{Limit: 1, Query: q, Category: "network"})
			if err != nil || len(list) != 1 || list[0].ID != a.ID {
				t.Fatalf("%+v %v", list, err)
			}
		}
		list, err = service.List(ctx, employee, knowledge.Filter{Limit: 1, Query: "literalneedle", Category: "hardware"})
		if err != nil || len(list) != 0 {
			t.Fatalf("%+v %v", list, err)
		}
		list, err = service.List(ctx, employee, knowledge.Filter{Limit: 1, Query: "literalneedle", Before: a.ID})
		if err != nil || len(list) != 0 {
			t.Fatalf("%+v %v", list, err)
		}
		for _, status := range []string{"draft", "archived"} {
			list, err = service.List(ctx, manager, knowledge.Filter{Limit: 100, Status: status})
			if err != nil || len(list) != 0 {
				t.Fatalf("hidden status leaked %+v %v", list, err)
			}
		}
	})
	t.Run("idempotent_concurrent_create_and_key_conflict", func(t *testing.T) {
		var wg sync.WaitGroup
		ids := make(chan int64, 8)
		errs := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				a, err := service.Create(ctx, staff, input(), "same-key")
				if err != nil {
					errs <- err
				} else {
					ids <- a.ID
				}
			}()
		}
		wg.Wait()
		close(ids)
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
		var id int64
		for next := range ids {
			if id != 0 && id != next {
				t.Fatal("duplicate article")
			}
			id = next
		}
		in := input()
		in.Title = "Different"
		if _, err := service.Create(ctx, staff, in, "same-key"); !errors.Is(err, knowledge.ErrConflict) {
			t.Fatal(err)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE entity_type='knowledge_article' AND entity_id=$1`, id).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%d %v", count, err)
		}
	})
	t.Run("concurrent_updates_preserve_one_winner", func(t *testing.T) {
		a := create(t)
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for _, title := range []string{"Birinci", "İkinci"} {
			wg.Add(1)
			go func(title string) {
				defer wg.Done()
				in := edit(a, "draft")
				in.Title = title
				_, err := service.Update(ctx, staff, a.ID, in)
				results <- err
			}(title)
		}
		wg.Wait()
		close(results)
		successes, conflicts := 0, 0
		for err := range results {
			if err == nil {
				successes++
			} else if errors.Is(err, knowledge.ErrConflict) {
				conflicts++
			} else {
				t.Fatal(err)
			}
		}
		if successes != 1 || conflicts != 1 {
			t.Fatalf("%d %d", successes, conflicts)
		}
	})
	t.Run("validation_and_missing_category", func(t *testing.T) {
		for _, change := range []func(*knowledge.Input){func(in *knowledge.Input) { in.Title = " " }, func(in *knowledge.Input) { in.Body = strings.Repeat("x", 10001) }, func(in *knowledge.Input) { in.Title = "a\x00" }, func(in *knowledge.Input) { in.Category = "missing" }, func(in *knowledge.Input) { in.Status = "published" }, func(in *knowledge.Input) { in.Version = 1 }} {
			in := input()
			change(&in)
			if _, err := service.Create(ctx, staff, in, fmt.Sprint(serial.Add(1))); !errors.Is(err, knowledge.ErrInvalid) {
				t.Fatal(err)
			}
		}
		if _, err := service.List(ctx, employee, knowledge.Filter{Limit: 101}); !errors.Is(err, knowledge.ErrInvalid) {
			t.Fatal(err)
		}
		if _, err := service.Detail(ctx, leader, 99999999); !errors.Is(err, knowledge.ErrNotFound) {
			t.Fatal(err)
		}
	})
	t.Run("http_auth_visibility_validation_and_origin", func(t *testing.T) {
		authentication, err := auth.New(pool, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		router := httpapi.NewServicesRouter(httpapi.Services{Database: pool, Authentication: authentication, Knowledge: service})
		tokens := map[string]string{}
		for _, u := range []auth.User{employee, staff, leader, manager} {
			session, err := authentication.Login(ctx, u.Email, "DemoDestek2026!", "")
			if err != nil {
				t.Fatal(err)
			}
			tokens[u.Role] = session.Token
		}
		send := func(role, path, method, body, origin, key string) *httptest.ResponseRecorder {
			t.Helper()
			r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", origin)
			r.Header.Set("Idempotency-Key", key)
			if role != "" {
				r.AddCookie(&http.Cookie{Name: "destekmasa_session", Value: tokens[role]})
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			return w
		}
		a := create(t)
		path := fmt.Sprintf("/knowledge/%d", a.ID)
		for _, tc := range []struct {
			role, path, method, body, origin, key string
			status                                int
		}{{"", "/knowledge", "GET", "", "", "", 401}, {"employee", path, "GET", "", "", "", 404}, {"employee", "/knowledge", "POST", "{}", "", "key", 403}, {"asset_manager", "/knowledge", "POST", "{}", "", "key", 403}, {"specialist", "/knowledge", "POST", "{}", "https://evil.example", "key", 403}, {"specialist", "/knowledge", "POST", "{}", "", "", 400}, {"employee", "/knowledge?limit=no", "GET", "", "", "", 400}, {"specialist", "/knowledge", "POST", "{\"unknown\":1}", "", "key", 400}} {
			w := send(tc.role, tc.path, tc.method, tc.body, tc.origin, tc.key)
			if w.Code != tc.status {
				t.Fatalf("%+v: %d %s", tc, w.Code, w.Body.String())
			}
		}
		a = save(t, leader, a, "published")
		w := send("employee", path, "GET", "", "", "")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var result struct {
			Article knowledge.Article `json:"article"`
		}
		if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Article.Body != a.Body {
			t.Fatalf("%s %v", w.Body.String(), err)
		}
	})
	t.Run("audit_failure_rolls_back_content_and_retry_key", func(t *testing.T) {
		a := create(t)
		_, err := pool.Exec(ctx, `CREATE FUNCTION reject_knowledge_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.entity_type='knowledge_article' THEN RAISE EXCEPTION 'test audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_knowledge_test BEFORE INSERT ON audit_log FOR EACH ROW EXECUTE FUNCTION reject_knowledge_test()`)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := pool.Exec(ctx, `DROP TRIGGER reject_knowledge_test ON audit_log; DROP FUNCTION reject_knowledge_test()`); err != nil {
				t.Error(err)
			}
		}()
		in := edit(a, "draft")
		in.Title = "Must roll back"
		if _, err = service.Update(ctx, staff, a.ID, in); err == nil {
			t.Fatal("expected audit failure")
		}
		got, err := service.Detail(ctx, staff, a.ID)
		if err != nil || got.Version != a.Version || got.Title != a.Title {
			t.Fatalf("%+v %v", got, err)
		}
		if _, err = service.Create(ctx, staff, input(), "failed-audit-key"); err == nil {
			t.Fatal("expected create failure")
		}
		var count int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM idempotency_keys WHERE operation='knowledge.create' AND key='failed-audit-key'`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("retry key persisted %d %v", count, err)
		}
	})
}
