package tickets

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"destekmasa/internal/auth"
	"destekmasa/internal/testdb"
)

func TestPostgresThreadWorkflow(t *testing.T) {
	ctx, pool, _ := testdb.New(t, "thread_test")
	var err error
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

	var serial atomic.Int64
	create := func(t *testing.T, user auth.User) Ticket {
		t.Helper()
		ticket, err := service.Create(ctx, user, CreateInput{Title: "Düzenlenecek talep", Description: "Açık açıklama", Priority: "normal", Category: "general"}, fmt.Sprintf("thread-%d", serial.Add(1)))
		if err != nil {
			t.Fatal(err)
		}
		return ticket
	}
	comment := func(t *testing.T, id int64, visibility, body string) Comment {
		t.Helper()
		c, err := service.AddComment(ctx, staff, id, CommentInput{Body: body, Visibility: visibility}, fmt.Sprintf("note-%d", serial.Add(1)))
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	t.Run("merge_moves_comments_and_preserves_privacy_and_origin", func(t *testing.T) {
		a, b := create(t, owner), create(t, owner)
		public := comment(t, a.ID, "external", "Paylaşılan")
		private := comment(t, a.ID, "internal", "Gizli")
		comment(t, b.ID, "external", "Hedef mesaj")
		result, err := service.Merge(ctx, manager, a.ID, b.ID, "merge-basic")
		if err != nil || result.Moved != 2 {
			t.Fatalf("%+v %v", result, err)
		}
		source, err := service.Get(ctx, owner, a.ID)
		if err != nil || source.Status != "closed" || source.MergedIntoID == nil || *source.MergedIntoID != b.ID {
			t.Fatalf("%+v %v", source, err)
		}
		items, err := service.Comments(ctx, owner, b.ID, 0, 20)
		if err != nil || len(items) != 2 {
			t.Fatalf("%+v %v", items, err)
		}
		for _, c := range items {
			if c.Visibility != "external" {
				t.Fatal("private leaked")
			}
		}
		items, err = service.Comments(ctx, staff, b.ID, 0, 20)
		if err != nil || len(items) != 3 {
			t.Fatalf("%+v %v", items, err)
		}
		items, err = service.Comments(ctx, owner, a.ID, 0, 20)
		if err != nil || len(items) != 0 {
			t.Fatalf("source %+v %v", items, err)
		}
		var original int64
		for _, id := range []int64{public.ID, private.ID} {
			if err = pool.QueryRow(ctx, `SELECT ticket_id FROM ticket_comments WHERE id=$1`, id).Scan(&original); err != nil || original != a.ID {
				t.Fatal("origin mutated")
			}
		}
	})
	t.Run("cross_owner_merge_and_non_manager_forbidden", func(t *testing.T) {
		a, b := create(t, owner), create(t, other)
		if _, err := service.Merge(ctx, manager, a.ID, b.ID, "cross-owner"); !errors.Is(err, ErrForbidden) {
			t.Fatal(err)
		}
		for _, u := range []auth.User{owner, staff, other} {
			if _, err := service.Merge(ctx, u, a.ID, b.ID, "role"); !errors.Is(err, ErrForbidden) {
				t.Fatal(err)
			}
			if _, err := service.Split(ctx, u, a.ID, SplitInput{Title: "Yeni", CommentIDs: []int64{1}}, "role"); !errors.Is(err, ErrForbidden) {
				t.Fatal(err)
			}
		}
	})
	t.Run("merge_aliases_flatten_and_email_replay_tracks_current_thread", func(t *testing.T) {
		raw := []byte("Message-ID: <thread-alias@example.local>\r\nFrom: " + owner.Email + "\r\nSubject: İlk e-posta\r\n\r\nMetin")
		receipt, err := service.IngestEmail(ctx, manager, raw)
		if err != nil {
			t.Fatal(err)
		}
		b, c := create(t, owner), create(t, owner)
		if _, err = service.Merge(ctx, manager, receipt.TicketID, b.ID, "alias-one"); err != nil {
			t.Fatal(err)
		}
		if _, err = service.Merge(ctx, manager, b.ID, c.ID, "alias-two"); err != nil {
			t.Fatal(err)
		}
		for _, id := range []int64{receipt.TicketID, b.ID} {
			ticket, err := service.Get(ctx, owner, id)
			if err != nil || ticket.MergedIntoID == nil || *ticket.MergedIntoID != c.ID {
				t.Fatalf("alias %+v %v", ticket, err)
			}
		}
		replay, err := service.IngestEmail(ctx, manager, raw)
		if err != nil || !replay.Duplicate || replay.TicketID != c.ID {
			t.Fatalf("replay %+v %v", replay, err)
		}
		reply := []byte(fmt.Sprintf("Message-ID: <thread-alias-reply@example.local>\r\nFrom: %s\r\nSubject: Re: #%d\r\n\r\nYanıt", owner.Email, receipt.TicketID))
		response, err := service.IngestEmail(ctx, manager, reply)
		if err != nil || response.TicketID != c.ID || response.CreatedTicket {
			t.Fatalf("reply %+v %v", response, err)
		}
	})
	t.Run("split_private_note_never_becomes_public_description", func(t *testing.T) {
		a := create(t, owner)
		private := comment(t, a.ID, "internal", "ÇOK GİZLİ bilgi")
		public := comment(t, a.ID, "external", "Açık bilgi")
		result, err := service.Split(ctx, manager, a.ID, SplitInput{Title: "Ayrılan konu", CommentIDs: []int64{private.ID}}, "private-split")
		if err != nil || result.Moved != 1 {
			t.Fatalf("%+v %v", result, err)
		}
		ticket, err := service.Get(ctx, owner, result.TargetID)
		if err != nil || ticket.RequesterID != owner.ID || ticket.Status != "open" || strings.Contains(ticket.Description, "GİZLİ") {
			t.Fatalf("%+v %v", ticket, err)
		}
		items, err := service.Comments(ctx, owner, result.TargetID, 0, 20)
		if err != nil || len(items) != 0 {
			t.Fatalf("%+v %v", items, err)
		}
		items, err = service.Comments(ctx, staff, result.TargetID, 0, 20)
		if err != nil || len(items) != 1 || items[0].ID != private.ID {
			t.Fatalf("%+v %v", items, err)
		}
		items, err = service.Comments(ctx, owner, a.ID, 0, 20)
		if err != nil || len(items) != 1 || items[0].ID != public.ID {
			t.Fatalf("%+v %v", items, err)
		}
		var actor int64
		if err = pool.QueryRow(ctx, `SELECT actor_id FROM audit_log WHERE entity_id=$1 AND action='ticket.create'`, result.TargetID).Scan(&actor); err != nil || actor != manager.ID {
			t.Fatalf("actor %d %v", actor, err)
		}
		var count int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM ticket_sla WHERE ticket_id=$1`, result.TargetID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("sla %d %v", count, err)
		}
	})
	t.Run("split_then_merge_keeps_attachment_authorization", func(t *testing.T) {
		data := []byte("Message-ID: <thread-file@example.local>\r\nFrom: " + owner.Email + "\r\nSubject: Dosya\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n--b\r\nContent-Type: text/plain\r\nContent-Disposition: attachment; filename=a.txt\r\n\r\ndata\r\n--b--\r\n")
		receipt, err := service.IngestEmail(ctx, manager, data)
		if err != nil {
			t.Fatal(err)
		}
		result, err := service.Split(ctx, manager, receipt.TicketID, SplitInput{Title: "Dosyalı konu", CommentIDs: []int64{receipt.CommentID}}, "file-split")
		if err != nil {
			t.Fatal(err)
		}
		files, err := service.Attachments(ctx, owner, result.TargetID, receipt.CommentID)
		if err != nil || len(files) != 1 {
			t.Fatalf("%+v %v", files, err)
		}
		if _, err := service.Attachments(ctx, owner, receipt.TicketID, receipt.CommentID); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
		replay, err := service.IngestEmail(ctx, manager, data)
		if err != nil || replay.TicketID != result.TargetID {
			t.Fatalf("%+v %v", replay, err)
		}
		if _, err := service.Merge(ctx, manager, result.TargetID, receipt.TicketID, "file-merge"); err != nil {
			t.Fatal(err)
		}
		file, err := service.DownloadAttachment(ctx, owner, files[0].ID)
		if err != nil || string(file.Data) != "data" {
			t.Fatalf("%+v %v", file, err)
		}
		if _, err := service.DownloadAttachment(ctx, other, file.ID); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
	})
	t.Run("organizer_retries_and_conflicts", func(t *testing.T) {
		a, b, c := create(t, owner), create(t, owner), create(t, owner)
		note := comment(t, a.ID, "external", "Bir")
		result, err := service.Split(ctx, manager, a.ID, SplitInput{Title: "Yeni", CommentIDs: []int64{note.ID}}, "split-replay")
		if err != nil {
			t.Fatal(err)
		}
		replay, err := service.Split(ctx, manager, a.ID, SplitInput{Title: "Yeni", CommentIDs: []int64{note.ID}}, "split-replay")
		if err != nil || replay != result {
			t.Fatalf("%+v %v", replay, err)
		}
		if _, err := service.Split(ctx, manager, a.ID, SplitInput{Title: "Farklı", CommentIDs: []int64{note.ID}}, "split-replay"); !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
		merged, err := service.Merge(ctx, manager, b.ID, c.ID, "merge-replay")
		if err != nil {
			t.Fatal(err)
		}
		replay, err = service.Merge(ctx, manager, b.ID, c.ID, "merge-replay")
		if err != nil || replay != merged {
			t.Fatalf("%+v %v", replay, err)
		}
		if _, err := service.Merge(ctx, manager, b.ID, a.ID, "merge-replay"); !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	})
	t.Run("concurrent_split_cannot_move_comment_twice", func(t *testing.T) {
		a := create(t, owner)
		note := comment(t, a.ID, "external", "Yarış")
		var wg sync.WaitGroup
		failures := make(chan error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, err := service.Split(ctx, manager, a.ID, SplitInput{Title: "Yeni", CommentIDs: []int64{note.ID}}, fmt.Sprintf("split-race-%d", i))
				failures <- err
			}(i)
		}
		wg.Wait()
		close(failures)
		success, invalid := 0, 0
		for err := range failures {
			if err == nil {
				success++
			} else if errors.Is(err, ErrInvalid) {
				invalid++
			} else {
				t.Fatal(err)
			}
		}
		if success != 1 || invalid != 1 {
			t.Fatalf("%d %d", success, invalid)
		}
	})
	t.Run("invalid_and_closed_operations_leave_no_partial_work", func(t *testing.T) {
		a, b := create(t, owner), create(t, owner)
		note := comment(t, b.ID, "external", "Başka talep")
		for _, input := range []SplitInput{{Title: "Yeni"}, {Title: " ", CommentIDs: []int64{note.ID}}, {Title: "Yeni", CommentIDs: []int64{note.ID}}, {Title: "Yeni", CommentIDs: []int64{note.ID, note.ID}}} {
			if _, err := service.Split(ctx, manager, a.ID, input, "invalid-split"); !errors.Is(err, ErrInvalid) {
				t.Fatal(err)
			}
		}
		for _, status := range []string{"in_progress", "resolved", "closed"} {
			if _, err := service.UpdateStatus(ctx, staff, a.ID, status); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := service.Merge(ctx, manager, b.ID, a.ID, "closed-target"); !errors.Is(err, ErrTransition) {
			t.Fatal(err)
		}
		if _, err := service.Merge(ctx, manager, a.ID, b.ID, "closed-source"); !errors.Is(err, ErrTransition) {
			t.Fatal(err)
		}
		if _, err := service.Merge(ctx, manager, b.ID, b.ID, "self"); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	})
	t.Run("operation_history_is_owned_paginated_and_immutable", func(t *testing.T) {
		a, b := create(t, owner), create(t, owner)
		if _, err := service.Merge(ctx, manager, a.ID, b.ID, "history"); err != nil {
			t.Fatal(err)
		}
		items, err := service.ThreadHistory(ctx, owner, b.ID, 0, 1)
		if err != nil || len(items) != 1 || items[0].SourceID != a.ID {
			t.Fatalf("%+v %v", items, err)
		}
		if _, err := service.ThreadHistory(ctx, other, b.ID, 0, 20); !errors.Is(err, ErrForbidden) {
			t.Fatal(err)
		}
		end, err := service.ThreadHistory(ctx, owner, b.ID, items[0].ID, 1)
		if err != nil || len(end) != 0 {
			t.Fatalf("%+v %v", end, err)
		}
		for _, query := range []string{`DELETE FROM ticket_operations WHERE id=$1`, `UPDATE ticket_operations SET kind='split' WHERE id=$1`} {
			if _, err := pool.Exec(ctx, query, items[0].ID); err == nil {
				t.Fatal("history mutated")
			}
		}
	})
	t.Run("failed_merge_rolls_back_close_and_comment_move", func(t *testing.T) {
		a, b := create(t, owner), create(t, owner)
		note := comment(t, a.ID, "external", "Taşınmamalı")
		_, err := pool.Exec(ctx, `CREATE FUNCTION fail_thread_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='ticket.merge' THEN RAISE EXCEPTION 'audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER test_thread_audit BEFORE INSERT ON audit_log FOR EACH ROW EXECUTE FUNCTION fail_thread_audit()`)
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Exec(ctx, `DROP TRIGGER test_thread_audit ON audit_log; DROP FUNCTION fail_thread_audit()`)
		if _, err := service.Merge(ctx, manager, a.ID, b.ID, "rollback-merge"); err == nil {
			t.Fatal("failure ignored")
		}
		ticket, err := service.Get(ctx, owner, a.ID)
		if err != nil || ticket.Status != "open" || ticket.MergedIntoID != nil {
			t.Fatalf("%+v %v", ticket, err)
		}
		items, err := service.Comments(ctx, owner, a.ID, 0, 20)
		if err != nil || len(items) != 1 || items[0].ID != note.ID {
			t.Fatalf("%+v %v", items, err)
		}
		var count int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM ticket_operations WHERE source_id=$1`, a.ID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("partial history %d %v", count, err)
		}
	})
}
