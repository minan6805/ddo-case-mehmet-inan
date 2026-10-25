package tickets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"destekmasa/internal/assignment"
	"destekmasa/internal/auth"
	mailinput "destekmasa/internal/email"
	"github.com/jackc/pgx/v5"
)

type EmailReceipt struct {
	MessageID     string `json:"message_id"`
	TicketID      int64  `json:"ticket_id"`
	CommentID     int64  `json:"comment_id"`
	CreatedTicket bool   `json:"created_ticket"`
	Duplicate     bool   `json:"duplicate"`
}
type Attachment struct {
	ID        int64     `json:"id"`
	CommentID int64     `json:"comment_id"`
	Filename  string    `json:"filename"`
	MediaType string    `json:"media_type"`
	Size      int       `json:"size_bytes"`
	SHA256    string    `json:"sha256"`
	CreatedAt time.Time `json:"created_at"`
	Data      []byte    `json:"-"`
}

func (s *Service) IngestEmail(ctx context.Context, actor auth.User, raw []byte) (EmailReceipt, error) {
	// This is a trusted demo importer, deliberately separate from employee APIs.
	if actor.Role != "admin" && actor.Role != "team_lead" {
		return EmailReceipt{}, ErrForbidden
	}
	message, err := mailinput.Parse(raw)
	if err != nil {
		return EmailReceipt{}, ErrInvalid
	}
	digest := sha256.Sum256(raw)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return EmailReceipt{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	// Same lock order as web creates/status changes; no nested service transactions.
	if err = assignment.Lock(ctx, tx); err != nil {
		return EmailReceipt{}, err
	}
	var receipt EmailReceipt
	var stored []byte
	err = tx.QueryRow(ctx, `SELECT e.message_id,e.request_hash,COALESCE(l.ticket_id,e.ticket_id),e.comment_id,e.created_ticket FROM incoming_emails e LEFT JOIN comment_locations l ON l.comment_id=e.comment_id WHERE e.message_id=$1`, message.ID).Scan(&receipt.MessageID, &stored, &receipt.TicketID, &receipt.CommentID, &receipt.CreatedTicket)
	if err == nil {
		if string(stored) != string(digest[:]) {
			return EmailReceipt{}, ErrConflict
		}
		receipt.Duplicate = true
		return receipt, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return EmailReceipt{}, err
	}
	var sender auth.User
	err = tx.QueryRow(ctx, `SELECT id,email,display_name,role_code FROM users WHERE email=$1`, message.Sender).Scan(&sender.ID, &sender.Email, &sender.DisplayName, &sender.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return EmailReceipt{}, ErrForbidden
	}
	if err != nil {
		return EmailReceipt{}, err
	}
	ticketID := message.TicketID
	if ticketID == 0 {
		input, err := normalize(CreateInput{Title: message.Subject, Description: message.Body, Priority: "normal", Category: "general"})
		if err != nil {
			return EmailReceipt{}, err
		}
		ticket, err := s.createTx(ctx, tx, sender, input, actor.ID)
		if err != nil {
			return EmailReceipt{}, err
		}
		ticketID = ticket.ID
	} else {
		// Merges flatten aliases, so an old #number keeps routing to the survivor.
		var canonical int64
		err = tx.QueryRow(ctx, `SELECT COALESCE(merged_into_ticket_id,id) FROM tickets WHERE id=$1`, ticketID).Scan(&canonical)
		if errors.Is(err, pgx.ErrNoRows) {
			return EmailReceipt{}, ErrNotFound
		}
		if err != nil {
			return EmailReceipt{}, err
		}
		ticketID = canonical
		var owner int64
		var status string
		err = tx.QueryRow(ctx, `SELECT requester_id,status FROM tickets WHERE id=$1 FOR UPDATE`, ticketID).Scan(&owner, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			return EmailReceipt{}, ErrNotFound
		}
		if err != nil {
			return EmailReceipt{}, err
		}
		if !IsStaff(sender.Role) && sender.ID != owner {
			return EmailReceipt{}, ErrForbidden
		}
		if status == "closed" {
			return EmailReceipt{}, ErrTransition
		}
	}
	var commentID int64
	err = tx.QueryRow(ctx, `INSERT INTO ticket_comments(ticket_id,author_id,visibility,body) VALUES($1,$2,'external',$3) RETURNING id`, ticketID, sender.ID, message.Body).Scan(&commentID)
	if err != nil {
		return EmailReceipt{}, err
	}
	for _, attachment := range message.Attachments {
		hash := sha256.Sum256(attachment.Data)
		_, err = tx.Exec(ctx, `INSERT INTO comment_attachments(comment_id,filename,media_type,size_bytes,sha256,data) VALUES($1,$2,$3,$4,$5,$6)`, commentID, attachment.Filename, attachment.MediaType, len(attachment.Data), hash[:], attachment.Data)
		if err != nil {
			return EmailReceipt{}, err
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO incoming_emails(message_id,request_hash,ticket_id,comment_id,sender_id,imported_by,created_ticket) VALUES($1,$2,$3,$4,$5,$6,$7)`, message.ID, digest[:], ticketID, commentID, sender.ID, actor.ID, message.TicketID == 0)
	if err != nil {
		return EmailReceipt{}, err
	}
	receipt = EmailReceipt{MessageID: message.ID, TicketID: ticketID, CommentID: commentID, CreatedTicket: message.TicketID == 0}
	metadata, err := json.Marshal(map[string]any{"message_id": message.ID, "ticket_id": ticketID, "sender_id": sender.ID, "visibility": "external", "attachment_count": len(message.Attachments)})
	if err != nil {
		return EmailReceipt{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_log(actor_id,action,entity_type,entity_id,new_value) VALUES($1,'email.ingest','comment',$2,$3)`, actor.ID, commentID, metadata)
	if err != nil {
		return EmailReceipt{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE tickets SET updated_at=clock_timestamp() WHERE id=$1`, ticketID)
	if err != nil {
		return EmailReceipt{}, err
	}
	return receipt, tx.Commit(ctx)
}

// Attachment visibility comes from its parent comment. Never accept a caller's
// visibility or filename when serving stored bytes.
func (s *Service) Attachments(ctx context.Context, user auth.User, ticketID, commentID int64) ([]Attachment, error) {
	if _, err := s.Get(ctx, user, ticketID); err != nil {
		return nil, err
	}
	var visible bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ticket_comments c LEFT JOIN comment_locations l ON l.comment_id=c.id WHERE c.id=$1 AND COALESCE(l.ticket_id,c.ticket_id)=$2 AND ($3::boolean OR c.visibility='external'))`, commentID, ticketID, IsStaff(user.Role)).Scan(&visible)
	if err != nil {
		return nil, err
	}
	if !visible {
		return nil, ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `SELECT id,comment_id,filename,media_type,size_bytes,sha256,created_at FROM comment_attachments WHERE comment_id=$1 ORDER BY id`, commentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Attachment{}
	for rows.Next() {
		var a Attachment
		var hash []byte
		if err = rows.Scan(&a.ID, &a.CommentID, &a.Filename, &a.MediaType, &a.Size, &hash, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.SHA256 = hex.EncodeToString(hash)
		result = append(result, a)
	}
	return result, rows.Err()
}
func (s *Service) DownloadAttachment(ctx context.Context, user auth.User, id int64) (Attachment, error) {
	var a Attachment
	var hash []byte
	err := s.pool.QueryRow(ctx, `SELECT a.id,a.comment_id,a.filename,a.media_type,a.size_bytes,a.sha256,a.created_at,a.data
 FROM comment_attachments a JOIN ticket_comments c ON c.id=a.comment_id LEFT JOIN comment_locations l ON l.comment_id=c.id JOIN tickets t ON t.id=COALESCE(l.ticket_id,c.ticket_id)
 WHERE a.id=$1 AND ($2::boolean OR (t.requester_id=$3 AND c.visibility='external'))`, id, IsStaff(user.Role), user.ID).Scan(&a.ID, &a.CommentID, &a.Filename, &a.MediaType, &a.Size, &hash, &a.CreatedAt, &a.Data)
	if errors.Is(err, pgx.ErrNoRows) {
		return Attachment{}, ErrNotFound
	}
	a.SHA256 = hex.EncodeToString(hash)
	return a, err
}
