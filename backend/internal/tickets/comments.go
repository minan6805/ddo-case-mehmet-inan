package tickets

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"destekmasa/internal/auth"
	"github.com/jackc/pgx/v5"
)

type Comment struct {
	ID          int64        `json:"id"`
	TicketID    int64        `json:"ticket_id"`
	AuthorID    int64        `json:"author_id"`
	AuthorName  string       `json:"author_name"`
	Visibility  string       `json:"visibility"`
	Body        string       `json:"body"`
	CreatedAt   time.Time    `json:"created_at"`
	Attachments []Attachment `json:"attachments"`
}
type CommentInput struct {
	Body       string `json:"body"`
	Visibility string `json:"visibility"`
}

const selectComment = `SELECT c.id,COALESCE(l.ticket_id,c.ticket_id),c.author_id,u.display_name,c.visibility,c.body,c.created_at,
 COALESCE((SELECT jsonb_agg(jsonb_build_object('id',a.id,'comment_id',a.comment_id,'filename',a.filename,'media_type',a.media_type,'size_bytes',a.size_bytes,'sha256',encode(a.sha256,'hex'),'created_at',a.created_at) ORDER BY a.id)
 FROM comment_attachments a WHERE a.comment_id=c.id),'[]'::jsonb)
 FROM ticket_comments c JOIN users u ON u.id=c.author_id LEFT JOIN comment_locations l ON l.comment_id=c.id `

func scanComment(row pgx.Row) (Comment, error) {
	var c Comment
	err := row.Scan(&c.ID, &c.TicketID, &c.AuthorID, &c.AuthorName, &c.Visibility, &c.Body, &c.CreatedAt, &c.Attachments)
	return c, err
}

// Only public comments enter the query for requesters; private content is never
// loaded and filtered in the browser. No total count exposes hidden notes.
func (s *Service) Comments(ctx context.Context, user auth.User, id, before int64, limit int) ([]Comment, error) {
	if before < 0 || limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	if _, err := s.Get(ctx, user, id); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, selectComment+`WHERE COALESCE(l.ticket_id,c.ticket_id)=$1 AND ($2::boolean OR c.visibility='external')
 AND ($3::bigint=0 OR c.id<$3) ORDER BY c.id DESC LIMIT $4`, id, IsStaff(user.Role), before, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Comment{}
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}
func (s *Service) AddComment(ctx context.Context, user auth.User, id int64, input CommentInput, key string) (Comment, error) {
	input.Body = strings.TrimSpace(input.Body)
	if id < 1 || !ValidKey(key) || !utf8.ValidString(input.Body) || strings.ContainsRune(input.Body, 0) || utf8.RuneCountInString(input.Body) < 1 || utf8.RuneCountInString(input.Body) > 10000 || (input.Visibility != "external" && input.Visibility != "internal") {
		return Comment{}, ErrInvalid
	}
	if input.Visibility == "internal" && !IsStaff(user.Role) {
		return Comment{}, ErrForbidden
	}
	body, err := json.Marshal(input)
	if err != nil {
		return Comment{}, err
	}
	hash := sha256.Sum256(body)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Comment{}, err
	}
	defer tx.Rollback(context.Background())
	// Serializes comments against closing the same ticket, without taking routing
	// locks: this transaction never changes assignment or status/SLA.
	var owner int64
	var status string
	err = tx.QueryRow(ctx, `SELECT requester_id,status FROM tickets WHERE id=$1 FOR UPDATE`, id).Scan(&owner, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Comment{}, ErrNotFound
	}
	if err != nil {
		return Comment{}, err
	}
	if !IsStaff(user.Role) && owner != user.ID {
		return Comment{}, ErrForbidden
	}
	operation := fmt.Sprintf("ticket.comment:%d", id)
	_, err = tx.Exec(ctx, `INSERT INTO idempotency_keys(user_id,operation,key,request_hash) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, user.ID, operation, key, hash[:])
	if err != nil {
		return Comment{}, err
	}
	var stored, response []byte
	err = tx.QueryRow(ctx, `SELECT request_hash,response_body FROM idempotency_keys WHERE user_id=$1 AND operation=$2 AND key=$3 FOR UPDATE`, user.ID, operation, key).Scan(&stored, &response)
	if err != nil {
		return Comment{}, err
	}
	if string(stored) != string(hash[:]) {
		return Comment{}, ErrConflict
	}
	if response != nil {
		var c Comment
		if err = json.Unmarshal(response, &c); err != nil {
			return Comment{}, err
		}
		return c, tx.Commit(ctx)
	}
	// A successful retry remains valid even if the ticket was closed afterward.
	if status == "closed" {
		return Comment{}, ErrTransition
	}
	var commentID int64
	err = tx.QueryRow(ctx, `INSERT INTO ticket_comments(ticket_id,author_id,visibility,body) VALUES($1,$2,$3,$4) RETURNING id`, id, user.ID, input.Visibility, input.Body).Scan(&commentID)
	if err != nil {
		return Comment{}, err
	}
	c, err := scanComment(tx.QueryRow(ctx, selectComment+`WHERE c.id=$1`, commentID))
	if err != nil {
		return Comment{}, err
	}
	response, err = json.Marshal(c)
	if err != nil {
		return Comment{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE idempotency_keys SET response_body=$4 WHERE user_id=$1 AND operation=$2 AND key=$3`, user.ID, operation, key, response)
	if err != nil {
		return Comment{}, err
	}
	// Audit contains metadata only, so internal note text is not duplicated.
	metadata, err := json.Marshal(map[string]any{"ticket_id": id, "visibility": input.Visibility})
	if err != nil {
		return Comment{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_log(actor_id,action,entity_type,entity_id,new_value) VALUES($1,'ticket.comment','comment',$2,$3)`, user.ID, c.ID, metadata)
	if err != nil {
		return Comment{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE tickets SET updated_at=clock_timestamp() WHERE id=$1`, id)
	if err != nil {
		return Comment{}, err
	}
	return c, tx.Commit(ctx)
}
