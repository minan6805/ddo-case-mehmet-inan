package tickets

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"destekmasa/internal/auth"
	"destekmasa/internal/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresTicketWorkflow(t *testing.T) {
	url := os.Getenv("INTEGRATION_DATABASE_URL")
	if url == "" {
		t.Skip("run make test-integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	random := make([]byte, 8)
	if _, err = rand.Read(random); err != nil {
		t.Fatal(err)
	}
	schema := "ticket_test_" + hex.EncodeToString(random)
	if _, err = admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := admin.Exec(cleanup, `DROP SCHEMA `+schema+` CASCADE`); err != nil {
			t.Error(err)
		}
	}()
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err = database.SeedDemo(ctx, pool, "DemoDestek2026!"); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO users(email,display_name,role_code,password_hash)
        SELECT 'ikinci@destekmasa.local','İkinci Çalışan','employee',password_hash FROM users WHERE email='calisan@destekmasa.local'`); err != nil {
		t.Fatal(err)
	}
	service := New(pool)
	getUser := func(email string) auth.User {
		t.Helper()
		var user auth.User
		if err := pool.QueryRow(ctx, `SELECT id,email,display_name,role_code FROM users WHERE email=$1`, email).Scan(&user.ID, &user.Email, &user.DisplayName, &user.Role); err != nil {
			t.Fatal(err)
		}
		return user
	}
	owner := getUser("calisan@destekmasa.local")
	other := getUser("ikinci@destekmasa.local")
	staff := getUser("uzman@destekmasa.local")
	input := CreateInput{Title: "İnternet kesildi", Description: "Ağ bağlantısı çalışmıyor.", Priority: "high", Category: "network"}
	var counter atomic.Int64
	create := func(t *testing.T, user auth.User) Ticket {
		t.Helper()
		ticket, err := service.Create(ctx, user, input, fmt.Sprintf("key-%d", counter.Add(1)))
		if err != nil {
			t.Fatal(err)
		}
		return ticket
	}
	t.Run("create_and_read_with_audit", func(t *testing.T) {
		ticket := create(t, owner)
		stored, err := service.Get(ctx, owner, ticket.ID)
		if err != nil || stored.Title != input.Title || stored.Status != "open" || stored.RequesterID != owner.ID {
			t.Fatalf("read: %+v %v", stored, err)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE entity_type='ticket' AND entity_id=$1 AND action='ticket.create'`, ticket.ID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("audit count: %d", count)
		}
	})
	t.Run("other_requester_cannot_read", func(t *testing.T) {
		ticket := create(t, owner)
		if _, err := service.Get(ctx, other, ticket.ID); !errors.Is(err, ErrForbidden) {
			t.Fatalf("access: %v", err)
		}
	})
	t.Run("list_is_scoped_to_requester", func(t *testing.T) {
		create(t, other)
		items, err := service.List(ctx, owner, ListOptions{Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			if item.RequesterID != owner.ID {
				t.Fatal("other user's ticket leaked")
			}
		}
	})
	t.Run("staff_reads_shared_queue", func(t *testing.T) {
		ticket := create(t, other)
		if _, err := service.Get(ctx, staff, ticket.ID); err != nil {
			t.Fatal(err)
		}
		items, err := service.List(ctx, staff, ListOptions{Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range items {
			if item.ID == ticket.ID {
				found = true
			}
		}
		if !found {
			t.Fatal("staff queue omitted another requester's ticket")
		}
	})
	t.Run("idempotent_retry", func(t *testing.T) {
		first, err := service.Create(ctx, owner, input, "retry")
		if err != nil {
			t.Fatal(err)
		}
		second, err := service.Create(ctx, owner, input, "retry")
		firstJSON, _ := json.Marshal(first)
		secondJSON, _ := json.Marshal(second)
		if err != nil || string(firstJSON) != string(secondJSON) {
			t.Fatalf("replay differs: %+v %+v %v", first, second, err)
		}
	})
	t.Run("key_payload_conflict", func(t *testing.T) {
		if _, err := service.Create(ctx, owner, input, "conflict"); err != nil {
			t.Fatal(err)
		}
		changed := input
		changed.Title = "Farklı içerik"
		if _, err := service.Create(ctx, owner, changed, "conflict"); !errors.Is(err, ErrConflict) {
			t.Fatalf("conflict: %v", err)
		}
	})
	t.Run("same_key_is_scoped_to_user", func(t *testing.T) {
		first, err := service.Create(ctx, owner, input, "shared-key")
		if err != nil {
			t.Fatal(err)
		}
		second, err := service.Create(ctx, other, input, "shared-key")
		if err != nil {
			t.Fatal(err)
		}
		if first.ID == second.ID || second.RequesterID != other.ID {
			t.Fatal("key leaked across users")
		}
	})
	t.Run("concurrent_retry_creates_one_ticket_and_audit", func(t *testing.T) {
		var wg sync.WaitGroup
		ids := make(chan int64, 8)
		problems := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ticket, err := service.Create(ctx, owner, input, "concurrent")
				if err != nil {
					problems <- err
					return
				}
				ids <- ticket.ID
			}()
		}
		wg.Wait()
		close(ids)
		close(problems)
		for err := range problems {
			t.Error(err)
		}
		var expected int64
		count := 0
		for id := range ids {
			if expected == 0 {
				expected = id
			}
			if id != expected {
				t.Error("duplicate ticket")
			}
			count++
		}
		if count != 8 {
			t.Fatalf("responses: %d", count)
		}
		var audits int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE entity_id=$1 AND action='ticket.create'`, expected).Scan(&audits); err != nil {
			t.Fatal(err)
		}
		if audits != 1 {
			t.Fatalf("audits: %d", audits)
		}
	})
	t.Run("invalid_category_rolls_back_key", func(t *testing.T) {
		invalid := input
		invalid.Category = "does-not-exist"
		if _, err := service.Create(ctx, owner, invalid, "rollback"); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid category: %v", err)
		}
		if _, err := service.Create(ctx, owner, input, "rollback"); err != nil {
			t.Fatalf("failed request retained key: %v", err)
		}
	})
	t.Run("keyset_pagination_has_no_duplicates", func(t *testing.T) {
		newest := create(t, owner)
		first, err := service.List(ctx, owner, ListOptions{Limit: 2})
		if err != nil || len(first) < 3 || first[0].ID != newest.ID {
			t.Fatalf("first page: %+v %v", first, err)
		}
		second, err := service.List(ctx, owner, ListOptions{Limit: 2, Before: first[1].ID})
		if err != nil || len(second) == 0 {
			t.Fatalf("second page: %v", err)
		}
		for _, item := range second {
			if item.ID >= first[1].ID {
				t.Fatal("cursor returned duplicate or newer ticket")
			}
		}
	})
	t.Run("requester_cannot_change_status", func(t *testing.T) {
		ticket := create(t, owner)
		if _, err := service.UpdateStatus(ctx, owner, ticket.ID, "in_progress"); !errors.Is(err, ErrForbidden) {
			t.Fatalf("status: %v", err)
		}
	})
	t.Run("status_workflow_and_audit", func(t *testing.T) {
		ticket := create(t, owner)
		if _, err := service.UpdateStatus(ctx, staff, ticket.ID, "closed"); !errors.Is(err, ErrTransition) {
			t.Fatal("invalid transition accepted")
		}
		for _, status := range []string{"in_progress", "waiting_customer", "in_progress", "resolved", "closed"} {
			updated, err := service.UpdateStatus(ctx, staff, ticket.ID, status)
			if err != nil || updated.Status != status {
				t.Fatalf("status %s: %v", status, err)
			}
		}
		if _, err := service.UpdateStatus(ctx, staff, ticket.ID, "in_progress"); !errors.Is(err, ErrTransition) {
			t.Fatal("closed ticket reopened")
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE entity_id=$1 AND action='ticket.status' AND old_value IS NOT NULL AND new_value IS NOT NULL`, ticket.ID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 5 {
			t.Fatalf("audit count: %d", count)
		}
	})
	t.Run("status_retry_does_not_duplicate_audit", func(t *testing.T) {
		ticket := create(t, owner)
		if _, err := service.UpdateStatus(ctx, staff, ticket.ID, "in_progress"); err != nil {
			t.Fatal(err)
		}
		if _, err := service.UpdateStatus(ctx, staff, ticket.ID, "in_progress"); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE entity_id=$1 AND action='ticket.status'`, ticket.ID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("audit count: %d", count)
		}
	})
	t.Run("status_filter", func(t *testing.T) {
		items, err := service.List(ctx, staff, ListOptions{Limit: 100, Status: "closed"})
		if err != nil || len(items) == 0 {
			t.Fatalf("filtered items: %v", err)
		}
		for _, item := range items {
			if item.Status != "closed" {
				t.Fatal("filter ignored")
			}
		}
	})
	t.Run("missing_ticket", func(t *testing.T) {
		if _, err := service.Get(ctx, staff, 9223372036854775807); !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing: %v", err)
		}
	})
	t.Run("comments_visibility_and_ownership", func(t *testing.T) {
		ticket := create(t, owner)
		public, err := service.AddComment(ctx, staff, ticket.ID, CommentInput{Body: "Herkese açık yanıt", Visibility: "external"}, "public")
		if err != nil {
			t.Fatal(err)
		}
		private, err := service.AddComment(ctx, staff, ticket.ID, CommentInput{Body: "Gizli ekip notu", Visibility: "internal"}, "private")
		if err != nil {
			t.Fatal(err)
		}
		items, err := service.Comments(ctx, owner, ticket.ID, 0, 20)
		if err != nil || len(items) != 1 || items[0].ID != public.ID {
			t.Fatalf("requester notes: %+v %v", items, err)
		}
		items, err = service.Comments(ctx, staff, ticket.ID, 0, 20)
		if err != nil || len(items) != 2 || items[0].ID != private.ID {
			t.Fatalf("staff notes: %+v %v", items, err)
		}
		if _, err = service.Comments(ctx, other, ticket.ID, 0, 20); !errors.Is(err, ErrForbidden) {
			t.Fatalf("foreign read: %v", err)
		}
		if _, err = service.AddComment(ctx, other, ticket.ID, CommentInput{Body: "Yabancı", Visibility: "external"}, "foreign"); !errors.Is(err, ErrForbidden) {
			t.Fatalf("foreign write: %v", err)
		}
	})
	t.Run("requester_cannot_write_internal_note", func(t *testing.T) {
		ticket := create(t, owner)
		for _, u := range []auth.User{owner, getUser("varlik@destekmasa.local")} {
			if _, err := service.AddComment(ctx, u, ticket.ID, CommentInput{Body: "İç not", Visibility: "internal"}, "private"); !errors.Is(err, ErrForbidden) {
				t.Fatalf("internal write: %v", err)
			}
		}
		c, err := service.AddComment(ctx, owner, ticket.ID, CommentInput{Body: "Ek bilgi", Visibility: "external"}, "public")
		if err != nil || c.AuthorID != owner.ID {
			t.Fatalf("owner message: %+v %v", c, err)
		}
	})
	t.Run("comment_concurrent_retries_exactly_once", func(t *testing.T) {
		ticket := create(t, owner)
		var wg sync.WaitGroup
		ids := make(chan int64, 8)
		failures := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				c, err := service.AddComment(ctx, owner, ticket.ID, CommentInput{Body: "Tek mesaj", Visibility: "external"}, "retry")
				if err != nil {
					failures <- err
					return
				}
				ids <- c.ID
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
			if id != first {
				t.Fatal("duplicate comment")
			}
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='ticket.comment' AND entity_id=$1`, first).Scan(&count); err != nil || count != 1 {
			t.Fatalf("audit %d %v", count, err)
		}
		items, err := service.Comments(ctx, owner, ticket.ID, 0, 20)
		if err != nil || len(items) != 1 {
			t.Fatalf("duplicates: %+v %v", items, err)
		}
	})
	t.Run("comment_key_conflict_and_scope", func(t *testing.T) {
		a, b := create(t, owner), create(t, owner)
		input := CommentInput{Body: "Bir", Visibility: "external"}
		if _, err := service.AddComment(ctx, owner, a.ID, input, "same"); err != nil {
			t.Fatal(err)
		}
		input.Body = "İki"
		if _, err := service.AddComment(ctx, owner, a.ID, input, "same"); !errors.Is(err, ErrConflict) {
			t.Fatalf("conflict: %v", err)
		}
		input = CommentInput{Body: "Bir", Visibility: "internal"}
		if _, err := service.AddComment(ctx, staff, a.ID, input, "visibility"); err != nil {
			t.Fatal(err)
		}
		input.Visibility = "external"
		if _, err := service.AddComment(ctx, staff, a.ID, input, "visibility"); !errors.Is(err, ErrConflict) {
			t.Fatalf("visibility conflict: %v", err)
		}
		if _, err := service.AddComment(ctx, owner, b.ID, input, "same"); err != nil {
			t.Fatal(err)
		}
		if _, err := service.AddComment(ctx, staff, a.ID, input, "same"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("comment_pagination_filters_before_limit", func(t *testing.T) {
		ticket := create(t, owner)
		var publicIDs []int64
		for i := 0; i < 5; i++ {
			visibility := "internal"
			if i%2 == 0 {
				visibility = "external"
			}
			c, err := service.AddComment(ctx, staff, ticket.ID, CommentInput{Body: fmt.Sprint(i), Visibility: visibility}, fmt.Sprint(i))
			if err != nil {
				t.Fatal(err)
			}
			if visibility == "external" {
				publicIDs = append(publicIDs, c.ID)
			}
		}
		items, err := service.Comments(ctx, owner, ticket.ID, 0, 1)
		if err != nil || len(items) != 2 || items[0].ID != publicIDs[2] || items[1].ID != publicIDs[1] {
			t.Fatalf("page: %+v %v", items, err)
		}
		items, err = service.Comments(ctx, owner, ticket.ID, items[0].ID, 1)
		if err != nil || len(items) != 2 || items[0].ID != publicIDs[1] {
			t.Fatalf("next: %+v %v", items, err)
		}
		items, err = service.Comments(ctx, owner, ticket.ID, publicIDs[0], 1)
		if err != nil || len(items) != 0 {
			t.Fatalf("end: %+v %v", items, err)
		}
	})
	t.Run("comment_closed_ticket_and_retry", func(t *testing.T) {
		ticket := create(t, owner)
		input := CommentInput{Body: "Kapanmadan", Visibility: "external"}
		c, err := service.AddComment(ctx, owner, ticket.ID, input, "before-close")
		if err != nil {
			t.Fatal(err)
		}
		for _, status := range []string{"in_progress", "resolved", "closed"} {
			if _, err = service.UpdateStatus(ctx, staff, ticket.ID, status); err != nil {
				t.Fatal(err)
			}
		}
		replay, err := service.AddComment(ctx, owner, ticket.ID, input, "before-close")
		if err != nil || replay.ID != c.ID {
			t.Fatalf("replay: %+v %v", replay, err)
		}
		if _, err = service.AddComment(ctx, owner, ticket.ID, input, "after-close"); !errors.Is(err, ErrTransition) {
			t.Fatalf("closed: %v", err)
		}
		items, err := service.Comments(ctx, owner, ticket.ID, 0, 20)
		if err != nil || len(items) != 1 {
			t.Fatalf("closed read: %+v %v", items, err)
		}
	})
	t.Run("comment_validation", func(t *testing.T) {
		ticket := create(t, owner)
		for _, input := range []CommentInput{{Body: " ", Visibility: "external"}, {Body: "x", Visibility: "secret"}, {Body: string([]byte{0xff}), Visibility: "external"}, {Body: "x\x00y", Visibility: "external"}, {Body: strings.Repeat("ş", 10001), Visibility: "external"}} {
			if _, err := service.AddComment(ctx, staff, ticket.ID, input, "invalid"); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid: %v", err)
			}
		}
		for _, limit := range []int{0, 101} {
			if _, err := service.Comments(ctx, owner, ticket.ID, 0, limit); !errors.Is(err, ErrInvalid) {
				t.Fatal(err)
			}
		}
		if _, err := service.AddComment(ctx, owner, ticket.ID, CommentInput{Body: "test", Visibility: "external"}, ""); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
		if _, err := service.AddComment(ctx, staff, 9223372036854775807, CommentInput{Body: "test", Visibility: "external"}, "missing"); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
	})
	t.Run("comments_are_immutable_and_audited_without_body", func(t *testing.T) {
		ticket := create(t, owner)
		c, err := service.AddComment(ctx, staff, ticket.ID, CommentInput{Body: "Gizli içerik", Visibility: "internal"}, "immutable")
		if err != nil {
			t.Fatal(err)
		}
		for _, query := range []string{`UPDATE ticket_comments SET body='changed' WHERE id=$1`, `DELETE FROM ticket_comments WHERE id=$1`} {
			if _, err := pool.Exec(ctx, query, c.ID); err == nil {
				t.Fatal("history changed")
			}
		}
		var metadata string
		if err := pool.QueryRow(ctx, `SELECT new_value::text FROM audit_log WHERE action='ticket.comment' AND entity_id=$1`, c.ID).Scan(&metadata); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(metadata, "Gizli") || !strings.Contains(metadata, "internal") {
			t.Fatal(metadata)
		}
	})
	t.Run("comment_does_not_change_status_or_sla", func(t *testing.T) {
		ticket := create(t, owner)
		if _, err := service.AddComment(ctx, staff, ticket.ID, CommentInput{Body: "Not", Visibility: "internal"}, "no-status"); err != nil {
			t.Fatal(err)
		}
		stored, err := service.Get(ctx, owner, ticket.ID)
		if err != nil || stored.Status != "open" {
			t.Fatalf("status: %+v %v", stored, err)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM ticket_status_history WHERE ticket_id=$1`, ticket.ID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("SLA history %d %v", count, err)
		}
	})

}
