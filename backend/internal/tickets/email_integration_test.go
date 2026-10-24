package tickets

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"destekmasa/internal/auth"
	"destekmasa/internal/database"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPostgresEmailWorkflow(t *testing.T) {
	url := os.Getenv("INTEGRATION_DATABASE_URL")
	if url == "" {
		t.Skip("run make test-integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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
	schema := "email_test_" + hex.EncodeToString(random)
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

	manager := getUser("lider@destekmasa.local")
	raw := func(id, from, subject, body string) []byte {
		return []byte(fmt.Sprintf("Message-ID: <%s@example.local>\r\nFrom: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s", id, from, subject, body))
	}
	create := func(t *testing.T, key string) Ticket {
		t.Helper()
		ticket, err := service.Create(ctx, owner, CreateInput{Title: "Mevcut talep", Description: "Açıklama", Category: "general", Priority: "normal"}, key)
		if err != nil {
			t.Fatal(err)
		}
		return ticket
	}
	t.Run("create_routes_and_initializes_sla_atomically", func(t *testing.T) {
		data := raw("new", owner.Email, "Bağlantı sorunu", "Yeni açıklama\r\n-- \r\nİmza")
		receipt, err := service.IngestEmail(ctx, manager, data)
		if err != nil || !receipt.CreatedTicket || receipt.Duplicate {
			t.Fatalf("%+v %v", receipt, err)
		}
		ticket, err := service.Get(ctx, owner, receipt.TicketID)
		if err != nil || ticket.Description != "Yeni açıklama" || ticket.Title != "Bağlantı sorunu" || ticket.AssigneeID == nil {
			t.Fatalf("%+v %v", ticket, err)
		}
		comments, err := service.Comments(ctx, owner, ticket.ID, 0, 20)
		if err != nil || len(comments) != 1 || comments[0].Body != "Yeni açıklama" || comments[0].Visibility != "external" {
			t.Fatalf("%+v %v", comments, err)
		}
		var count int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM ticket_sla WHERE ticket_id=$1`, ticket.ID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("sla %d %v", count, err)
		}
	})
	t.Run("reply_to_subject_reference_without_duplicate_ticket", func(t *testing.T) {
		ticket := create(t, "reply-target")
		receipt, err := service.IngestEmail(ctx, manager, raw("reply", owner.Email, fmt.Sprintf("Re: #%d yanıt", ticket.ID), "Ek bilgi\nOn Monday, Demo wrote:\n> Alıntı"))
		if err != nil || receipt.CreatedTicket || receipt.TicketID != ticket.ID {
			t.Fatalf("%+v %v", receipt, err)
		}
		comments, err := service.Comments(ctx, owner, ticket.ID, 0, 20)
		if err != nil || len(comments) != 1 || comments[0].Body != "Ek bilgi" {
			t.Fatalf("%+v %v", comments, err)
		}
	})
	t.Run("concurrent_message_id_is_exactly_once", func(t *testing.T) {
		data := raw("concurrent", owner.Email, "Tek talep", "Tek açıklama")
		var wg sync.WaitGroup
		results := make(chan EmailReceipt, 8)
		failures := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				receipt, err := service.IngestEmail(ctx, manager, data)
				if err != nil {
					failures <- err
				} else {
					results <- receipt
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
		fresh := 0
		for r := range results {
			if id == 0 {
				id = r.TicketID
			}
			if r.TicketID != id {
				t.Fatal("duplicate ticket")
			}
			if !r.Duplicate {
				fresh++
			}
		}
		if fresh != 1 {
			t.Fatalf("fresh %d", fresh)
		}
		comments, err := service.Comments(ctx, owner, id, 0, 20)
		if err != nil || len(comments) != 1 {
			t.Fatalf("comments %+v %v", comments, err)
		}
		var count int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='email.ingest' AND entity_id=$1`, comments[0].ID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("audit %d %v", count, err)
		}
	})
	t.Run("message_id_different_bytes_conflict", func(t *testing.T) {
		data := raw("conflict", owner.Email, "Konu", "Bir")
		if _, err := service.IngestEmail(ctx, manager, data); err != nil {
			t.Fatal(err)
		}
		if _, err := service.IngestEmail(ctx, manager, raw("conflict", owner.Email, "Konu", "İki")); !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	})
	t.Run("importer_role_and_known_sender_required", func(t *testing.T) {
		data := raw("roles", owner.Email, "Konu", "Metin")
		for _, u := range []auth.User{owner, other, staff, getUser("varlik@destekmasa.local")} {
			if _, err := service.IngestEmail(ctx, u, data); !errors.Is(err, ErrForbidden) {
				t.Fatalf("role %s %v", u.Role, err)
			}
		}
		if _, err := service.IngestEmail(ctx, manager, raw("unknown", "unknown@example.local", "Konu", "Metin")); !errors.Is(err, ErrForbidden) {
			t.Fatal(err)
		}
	})
	t.Run("foreign_sender_cannot_guess_ticket_reference", func(t *testing.T) {
		ticket := create(t, "foreign-target")
		if _, err := service.IngestEmail(ctx, manager, raw("foreign", other.Email, fmt.Sprintf("Re: #%d", ticket.ID), "Yabancı yanıt")); !errors.Is(err, ErrForbidden) {
			t.Fatal(err)
		}
		items, err := service.Comments(ctx, owner, ticket.ID, 0, 20)
		if err != nil || len(items) != 0 {
			t.Fatalf("%+v %v", items, err)
		}
		if _, err := service.IngestEmail(ctx, manager, raw("missing", owner.Email, "Re: #9223372036854775807", "Yanıt")); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
	})
	t.Run("closed_replies_rejected_but_replay_survives", func(t *testing.T) {
		data := raw("closed-original", owner.Email, "Konu", "Metin")
		receipt, err := service.IngestEmail(ctx, manager, data)
		if err != nil {
			t.Fatal(err)
		}
		for _, status := range []string{"in_progress", "resolved", "closed"} {
			if _, err = service.UpdateStatus(ctx, staff, receipt.TicketID, status); err != nil {
				t.Fatal(err)
			}
		}
		replay, err := service.IngestEmail(ctx, manager, data)
		if err != nil || !replay.Duplicate || replay.CommentID != receipt.CommentID {
			t.Fatalf("%+v %v", replay, err)
		}
		if _, err := service.IngestEmail(ctx, manager, raw("closed-new", owner.Email, fmt.Sprintf("Re: #%d", receipt.TicketID), "Geç yanıt")); !errors.Is(err, ErrTransition) {
			t.Fatal(err)
		}
	})
	t.Run("attachments_are_durable_downloadable_and_scoped", func(t *testing.T) {
		data := []byte("Message-ID: <attachment@example.local>\r\nFrom: " + owner.Email + "\r\nSubject: Ek testi\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n--b\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nAçıklama\r\n--b\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=\"../../log.bin\"\r\nContent-Transfer-Encoding: base64\r\n\r\nAAECAw==\r\n--b--\r\n")
		receipt, err := service.IngestEmail(ctx, manager, data)
		if err != nil {
			t.Fatal(err)
		}
		files, err := service.Attachments(ctx, owner, receipt.TicketID, receipt.CommentID)
		if err != nil || len(files) != 1 || files[0].Filename != "log.bin" || files[0].Size != 4 {
			t.Fatalf("%+v %v", files, err)
		}
		comments, err := service.Comments(ctx, owner, receipt.TicketID, 0, 20)
		if err != nil || len(comments[0].Attachments) != 1 {
			t.Fatalf("comment files %+v %v", comments, err)
		}
		file, err := service.DownloadAttachment(ctx, owner, files[0].ID)
		if err != nil || string(file.Data) != string([]byte{0, 1, 2, 3}) {
			t.Fatalf("%+v %v", file, err)
		}
		digest := sha256.Sum256(file.Data)
		if file.SHA256 != hex.EncodeToString(digest[:]) {
			t.Fatal("hash mismatch")
		}
		if _, err := service.DownloadAttachment(ctx, other, file.ID); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
		if _, err := service.Attachments(ctx, other, receipt.TicketID, receipt.CommentID); !errors.Is(err, ErrForbidden) {
			t.Fatal(err)
		}
		replay, err := service.IngestEmail(ctx, manager, data)
		if err != nil || replay.CommentID != receipt.CommentID || !replay.Duplicate {
			t.Fatal(err)
		}
		files, err = service.Attachments(ctx, staff, receipt.TicketID, receipt.CommentID)
		if err != nil || len(files) != 1 {
			t.Fatal(err)
		}
	})
	t.Run("internal_attachment_visibility_inherits_comment", func(t *testing.T) {
		ticket := create(t, "private-file")
		c, err := service.AddComment(ctx, staff, ticket.ID, CommentInput{Body: "Gizli", Visibility: "internal"}, "private-file")
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256([]byte("private"))
		var id int64
		err = pool.QueryRow(ctx, `INSERT INTO comment_attachments(comment_id,filename,media_type,size_bytes,sha256,data) VALUES($1,'private.txt','text/plain',7,$2,$3) RETURNING id`, c.ID, digest[:], []byte("private")).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = service.DownloadAttachment(ctx, owner, id); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
		if _, err = service.Attachments(ctx, owner, ticket.ID, c.ID); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
		if file, err := service.DownloadAttachment(ctx, staff, id); err != nil || string(file.Data) != "private" {
			t.Fatalf("%+v %v", file, err)
		}
	})
	t.Run("attachment_failure_rolls_back_ticket_routing_sla_and_receipt", func(t *testing.T) {
		_, err := pool.Exec(ctx, `CREATE FUNCTION fail_email_attachment() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'attachment storage failure'; END $$; CREATE TRIGGER test_attachment_failure BEFORE INSERT ON comment_attachments FOR EACH ROW EXECUTE FUNCTION fail_email_attachment()`)
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Exec(ctx, `DROP TRIGGER test_attachment_failure ON comment_attachments; DROP FUNCTION fail_email_attachment()`)
		var before, after int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM tickets`).Scan(&before); err != nil {
			t.Fatal(err)
		}
		data := []byte("Message-ID: <rollback@example.local>\r\nFrom: " + owner.Email + "\r\nSubject: Rollback\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n--b\r\nContent-Type: text/plain\r\nContent-Disposition: attachment; filename=a.txt\r\n\r\ndata\r\n--b--\r\n")
		if _, err = service.IngestEmail(ctx, manager, data); err == nil {
			t.Fatal("failure ignored")
		}
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM tickets`).Scan(&after); err != nil || after != before {
			t.Fatalf("partial ticket %d/%d %v", before, after, err)
		}
		var count int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM incoming_emails WHERE message_id='<rollback@example.local>'`).Scan(&count); err != nil || count != 0 {
			t.Fatal("partial receipt")
		}
	})
	t.Run("malformed_input_does_not_write", func(t *testing.T) {
		if _, err := service.IngestEmail(ctx, manager, []byte("broken")); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
		if _, err := service.IngestEmail(ctx, manager, raw("oversize", owner.Email, "Konu", strings.Repeat("x", 10001))); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	})
}
