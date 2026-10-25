package reports_test

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"destekmasa/internal/auth"
	"destekmasa/internal/httpapi"
	"destekmasa/internal/reports"
	"destekmasa/internal/testdb"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type traceKey struct{}
type snapshotTracer struct {
	once  sync.Once
	write func()
}

func (s *snapshotTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, traceKey{}, data.SQL == `SELECT transaction_timestamp()`)
}
func (s *snapshotTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if active, _ := ctx.Value(traceKey{}).(bool); active {
		s.once.Do(s.write)
	}
}

func TestPostgresReports(t *testing.T) {
	ctx, pool, users := testdb.New(t, "report_test")
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	service := reports.New(pool, func() time.Time { return now })
	leader := users["lider@destekmasa.local"]
	employee := users["calisan@destekmasa.local"]
	first := users["uzman@destekmasa.local"]
	second := users["uzman2@destekmasa.local"]
	third := users["uzman3@destekmasa.local"]
	filter := reports.Filter{From: "2026-10-20", To: "2026-10-21"}
	create := func(status, priority, category, at string, assignee *int64) int64 {
		t.Helper()
		var id int64
		err := pool.QueryRow(ctx, `INSERT INTO tickets(requester_id,assignee_id,title,description,status,priority,category_code,created_at) VALUES($1,$2,'Raporda test','Kontrollü rapor verisi',$3,$4,$5,$6::timestamptz) RETURNING id`, employee.ID, assignee, status, priority, category, at).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	a := create("open", "high", "network", "2026-10-19T21:00:00Z", &first.ID)
	b := create("waiting_customer", "critical", "network", "2026-10-20T20:59:59Z", &first.ID)
	c := create("resolved", "normal", "hardware", "2026-10-20T21:00:00Z", &second.ID)
	d := create("closed", "low", "software", "2026-10-21T20:59:59.999999Z", &second.ID)
	outside := create("in_progress", "normal", "general", "2026-10-21T21:00:00Z", nil)
	create("open", "normal", "network", "2026-10-19T20:59:59.999999Z", nil)
	create("open", "normal", "network", "2025-01-01T00:00:00Z", &third.ID)
	for _, row := range []struct {
		id, response, resolution int64
		done                     bool
		checked                  string
	}{{b, 300, 400, false, "2026-10-21T10:00:00Z"}, {c, 600, 1200, true, "2026-10-21T11:00:00Z"}, {d, 120, 1800, true, "2026-10-21T12:00:00Z"}} {
		if _, err := pool.Exec(ctx, `INSERT INTO ticket_sla(ticket_id,response_budget_seconds,resolution_budget_seconds,policy_version,calendar_version,response_elapsed_seconds,resolution_elapsed_seconds,first_response_at,completed_at,checked_at) VALUES($1,600,1800,1,1,$2,$3,'2026-10-20T10:00:00Z',CASE WHEN $4 THEN '2026-10-21T10:00:00Z'::timestamptz ELSE NULL END,$5::timestamptz)`, row.id, row.response, row.resolution, row.done, row.checked); err != nil {
			t.Fatal(err)
		}
	}
	for _, event := range []struct {
		id           int64
		timer, stage string
	}{{b, "response", "warning"}, {b, "resolution", "warning"}, {c, "response", "warning"}, {c, "response", "breach"}, {c, "response", "escalation"}, {d, "resolution", "breach"}, {d, "resolution", "escalation"}, {outside, "response", "breach"}} {
		if _, err := pool.Exec(ctx, `INSERT INTO sla_events(ticket_id,timer,stage,elapsed_seconds,budget_seconds,calendar_version,created_at) VALUES($1,$2,$3,600,600,1,'2026-10-25T10:00:00Z')`, event.id, event.timer, event.stage); err != nil {
			t.Fatal(err)
		}
	}
	get := func(t *testing.T, f reports.Filter) reports.Report {
		t.Helper()
		r, err := service.Overview(ctx, leader, f)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	bucket := func(items []reports.Bucket, key string) int64 {
		for _, item := range items {
			if item.Code == key {
				return item.Count
			}
		}
		return -1
	}
	t.Run("date_boundaries_and_reconciled_buckets", func(t *testing.T) {
		r := get(t, filter)
		if r.Summary.Total != 4 || r.Summary.Active != 2 || r.Summary.Resolved != 1 || r.Summary.Closed != 1 || r.Summary.UnassignedActive != 0 {
			t.Fatalf("%+v", r.Summary)
		}
		for _, items := range [][]reports.Bucket{r.Statuses, r.Priorities, r.Categories} {
			var sum int64
			for _, b := range items {
				sum += b.Count
			}
			if sum != 4 {
				t.Fatalf("%+v", items)
			}
		}
		if bucket(r.Categories, "network") != 2 || bucket(r.Categories, "general") != 0 || bucket(r.Statuses, "in_progress") != 0 || len(r.Daily) != 2 || r.Daily[0].Count != 2 || r.Daily[1].Count != 2 {
			t.Fatalf("%+v", r)
		}
		for _, p := range r.Priorities {
			if p.Count != 1 {
				t.Fatalf("%+v", r.Priorities)
			}
		}
	})
	t.Run("category_filter_is_applied_to_every_cohort_metric", func(t *testing.T) {
		f := filter
		f.Category = "network"
		r := get(t, f)
		if r.Summary.Total != 2 || r.Summary.Active != 2 || r.SLA.Tracked != 1 || r.SLA.Missing != 1 || r.SLA.WarningTickets != 1 || r.SLA.ResponseBreachTickets != 0 || r.SLA.ResolutionBreachTickets != 0 || r.Daily[0].Count != 2 || r.Daily[1].Count != 0 {
			t.Fatalf("%+v", r)
		}
		if bucket(r.Categories, "network") != 2 || bucket(r.Categories, "hardware") != 0 {
			t.Fatal(r.Categories)
		}
	})
	t.Run("sla_distinct_events_completed_samples_and_exact_budget", func(t *testing.T) {
		s := get(t, filter).SLA
		if s.Tracked != 3 || s.Missing != 1 || s.Responded != 3 || s.RespondedWithinBudget != 2 || s.Completed != 2 || s.CompletedWithinBudget != 1 || s.WarningTickets != 2 || s.ResponseBreachTickets != 1 || s.ResolutionBreachTickets != 1 || s.EscalatedTickets != 2 {
			t.Fatalf("%+v", s)
		}
		if s.AverageResponseMinutes == nil || math.Abs(*s.AverageResponseMinutes-17.0/3) > 0.000001 || s.AverageResolutionMinutes == nil || *s.AverageResolutionMinutes != 25 {
			t.Fatalf("%+v", s)
		}
		if s.OldestCheckedAt == nil || s.LatestCheckedAt == nil || s.OldestCheckedAt.Format(time.RFC3339) != "2026-10-21T10:00:00Z" || s.LatestCheckedAt.Format(time.RFC3339) != "2026-10-21T12:00:00Z" {
			t.Fatalf("%+v", s)
		}
	})
	t.Run("current_workload_is_global_and_includes_zero_load_experts", func(t *testing.T) {
		f := filter
		f.Category = "hardware"
		w := get(t, f).Workload
		if w.TotalActive != 5 || w.UnassignedActive != 2 || len(w.Experts) != 3 {
			t.Fatalf("%+v", w)
		}
		loads := map[int64]int64{}
		var assigned int64
		for _, e := range w.Experts {
			loads[e.ID] = e.Active
			assigned += e.Active
			if e.Capacity != 8 {
				t.Fatal(e)
			}
		}
		if loads[first.ID] != 2 || loads[second.ID] != 0 || loads[third.ID] != 1 || assigned+w.UnassignedActive != w.TotalActive {
			t.Fatalf("%+v", w)
		}
	})
	t.Run("empty_cohort_has_zero_days_and_null_averages", func(t *testing.T) {
		r := get(t, reports.Filter{From: "2030-01-01", To: "2030-01-03"})
		if r.Summary.Total != 0 || r.SLA.AverageResponseMinutes != nil || r.SLA.AverageResolutionMinutes != nil || r.SLA.OldestCheckedAt != nil || len(r.Daily) != 3 {
			t.Fatalf("%+v", r)
		}
		for _, day := range r.Daily {
			if day.Count != 0 {
				t.Fatal(day)
			}
		}
		if r.Workload.TotalActive != 5 {
			t.Fatal("global workload was filtered")
		}
	})
	t.Run("default_range_uses_istanbul_calendar", func(t *testing.T) {
		r := get(t, reports.Filter{})
		if r.Range.From != "2026-09-22" || r.Range.To != "2026-10-21" || len(r.Daily) != 30 || r.Range.Timezone != reports.Timezone {
			t.Fatalf("%+v", r.Range)
		}
	})
	t.Run("service_role_and_filter_validation", func(t *testing.T) {
		for _, u := range []auth.User{employee, first, users["varlik@destekmasa.local"]} {
			if _, err := service.Overview(ctx, u, filter); !errors.Is(err, reports.ErrForbidden) {
				t.Fatal(err)
			}
		}
		if _, err := service.Overview(ctx, users["admin@destekmasa.local"], filter); err != nil {
			t.Fatal(err)
		}
		f := filter
		f.Category = "missing"
		if _, err := service.Overview(ctx, leader, f); !errors.Is(err, reports.ErrInvalid) {
			t.Fatal(err)
		}
		for _, f := range []reports.Filter{{From: "2026-02-30", To: "2026-03-01"}, {From: "2026-01-01"}, {From: "2024-01-01", To: "2025-01-01"}} {
			if _, err := service.Overview(ctx, leader, f); !errors.Is(err, reports.ErrInvalid) {
				t.Fatal(err)
			}
		}
	})
	t.Run("read_only_report_does_not_write_audit_or_sla", func(t *testing.T) {
		var before, after int64
		var checkedBefore, checkedAfter time.Time
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log`).Scan(&before); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT checked_at FROM ticket_sla WHERE ticket_id=$1`, b).Scan(&checkedBefore); err != nil {
			t.Fatal(err)
		}
		get(t, filter)
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log`).Scan(&after); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT checked_at FROM ticket_sla WHERE ticket_id=$1`, b).Scan(&checkedAfter); err != nil {
			t.Fatal(err)
		}
		if before != after || !checkedBefore.Equal(checkedAfter) {
			t.Fatal("report mutated data")
		}
	})
	t.Run("http_auth_access_and_filter_contract", func(t *testing.T) {
		authentication, err := auth.New(pool, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		router := httpapi.NewServicesRouter(httpapi.Services{Database: pool, Authentication: authentication, Reports: service})
		for _, tc := range []struct {
			name, path string
			status     int
		}{{"", "/reports", 401}, {"calisan", "/reports", 403}, {"uzman", "/reports", 403}, {"varlik", "/reports", 403}, {"lider", "/reports?from=2026-10-20&to=2026-10-21", 200}, {"admin", "/reports?from=2026-10-20&to=2026-10-21", 200}, {"lider", "/reports?from=oops&to=2026-10-21", 400}, {"lider", "/reports?category=missing", 400}} {
			r := httptest.NewRequest("GET", tc.path, nil)
			if tc.name != "" {
				session, err := authentication.Login(ctx, tc.name+"@destekmasa.local", "DemoDestek2026!", "")
				if err != nil {
					t.Fatal(err)
				}
				r.AddCookie(&http.Cookie{Name: "destekmasa_session", Value: session.Token})
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("%+v %d %s", tc, w.Code, w.Body.String())
			}
			if tc.status == 200 {
				var report reports.Report
				if err = json.Unmarshal(w.Body.Bytes(), &report); err != nil || report.Summary.Total != 4 || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("%s %v", w.Body.String(), err)
				}
			}
		}
	})
	t.Run("concurrent_write_cannot_mix_report_snapshots", func(t *testing.T) {
		var writeErr error
		config := pool.Config()
		config.ConnConfig.Tracer = &snapshotTracer{write: func() {
			_, writeErr = pool.Exec(ctx, `UPDATE tickets SET status='closed',category_code='hardware' WHERE id=$1`, a)
		}}
		traced, err := pgxpool.NewWithConfig(ctx, config)
		if err != nil {
			t.Fatal(err)
		}
		defer traced.Close()
		defer func() {
			if _, err := pool.Exec(ctx, `UPDATE tickets SET status='open',category_code='network' WHERE id=$1`, a); err != nil {
				t.Error(err)
			}
		}()
		r, err := reports.New(traced, nil).Overview(ctx, leader, filter)
		if err != nil || writeErr != nil {
			t.Fatalf("read %v write %v", err, writeErr)
		}
		if r.Summary.Active != 2 || bucket(r.Statuses, "open") != 1 || bucket(r.Categories, "network") != 2 || r.Workload.TotalActive != 5 {
			t.Fatalf("mixed snapshots %+v", r)
		}
		var status string
		if err = pool.QueryRow(ctx, `SELECT status FROM tickets WHERE id=$1`, a).Scan(&status); err != nil || status != "closed" {
			t.Fatalf("concurrent update did not happen %q %v", status, err)
		}
	})
	t.Run("expert_limit_is_explicit_instead_of_truncated", func(t *testing.T) {
		_, err := pool.Exec(ctx, `INSERT INTO users(email,display_name,password_hash,role_code) SELECT 'report-limit-'||i||'@test.local','Ek uzman '||i,'not-for-login','specialist' FROM generate_series(1,498) i; INSERT INTO expert_profiles(user_id) SELECT id FROM users WHERE email LIKE 'report-limit-%@test.local'`)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = service.Overview(ctx, leader, filter); !errors.Is(err, reports.ErrLimit) {
			t.Fatal(err)
		}
	})
}
