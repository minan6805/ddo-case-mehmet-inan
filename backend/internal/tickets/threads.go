package tickets

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"destekmasa/internal/assignment"
	"destekmasa/internal/auth"

	"github.com/jackc/pgx/v5"
)

type ThreadResult struct {
	SourceID int64 `json:"source_id"`
	TargetID int64 `json:"target_id"`
	Moved    int   `json:"moved_comments"`
}
type SplitInput struct {
	Title      string  `json:"title"`
	CommentIDs []int64 `json:"comment_ids"`
}
type ThreadOperation struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"`
	SourceID  int64     `json:"source_id"`
	TargetID  int64     `json:"target_id"`
	ActorName string    `json:"actor_name"`
	CreatedAt time.Time `json:"created_at"`
}

func canOrganize(user auth.User) bool { return user.Role == "team_lead" || user.Role == "admin" }

// The caller holds routing locks. Reservation and response belong to the same
// transaction as the thread move; no successful operation can lack a replay.
func reserveThread(ctx context.Context, tx pgx.Tx, user auth.User, operation, key string, input any) (*ThreadResult, error) {
	payload, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(payload)
	_, err = tx.Exec(ctx, `INSERT INTO idempotency_keys(user_id,operation,key,request_hash) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, user.ID, operation, key, hash[:])
	if err != nil {
		return nil, err
	}
	var stored, response []byte
	err = tx.QueryRow(ctx, `SELECT request_hash,response_body FROM idempotency_keys WHERE user_id=$1 AND operation=$2 AND key=$3 FOR UPDATE`, user.ID, operation, key).Scan(&stored, &response)
	if err != nil {
		return nil, err
	}
	if string(stored) != string(hash[:]) {
		return nil, ErrConflict
	}
	if response != nil {
		var result ThreadResult
		if err = json.Unmarshal(response, &result); err != nil {
			return nil, err
		}
		return &result, nil
	}
	return nil, nil
}
func finishThread(ctx context.Context, tx pgx.Tx, user auth.User, kind, key string, result ThreadResult, ids []int64) error {
	if ids == nil {
		ids = []int64{}
	}
	_, err := tx.Exec(ctx, `INSERT INTO ticket_operations(kind,source_id,target_id,actor_id,comment_ids) VALUES($1,$2,$3,$4,$5)`, kind, result.SourceID, result.TargetID, user.ID, ids)
	if err != nil {
		return err
	}
	response, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE idempotency_keys SET response_body=$4 WHERE user_id=$1 AND operation=$2 AND key=$3`, user.ID, "ticket."+kind, key, response)
	if err != nil {
		return err
	}
	previous, err := json.Marshal(map[string]any{"ticket_id": result.SourceID})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_log(actor_id,action,entity_type,entity_id,old_value,new_value) VALUES($1,$2,'ticket',$3,$4,$5)`, user.ID, "ticket."+kind, result.SourceID, previous, response)
	return err
}
func threadCommentIDs(ctx context.Context, tx pgx.Tx, id int64) ([]int64, error) {
	rows, err := tx.Query(ctx, `SELECT c.id FROM ticket_comments c LEFT JOIN comment_locations l ON l.comment_id=c.id WHERE COALESCE(l.ticket_id,c.ticket_id)=$1 ORDER BY c.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func moveComments(ctx context.Context, tx pgx.Tx, target int64, ids []int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO comment_locations(comment_id,ticket_id) SELECT unnest($1::bigint[]),$2 ON CONFLICT(comment_id) DO UPDATE SET ticket_id=EXCLUDED.ticket_id`, ids, target)
	return err
}
func (s *Service) Merge(ctx context.Context, user auth.User, source, target int64, key string) (ThreadResult, error) {
	if !canOrganize(user) {
		return ThreadResult{}, ErrForbidden
	}
	if source < 1 || target < 1 || source == target || !ValidKey(key) {
		return ThreadResult{}, ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ThreadResult{}, err
	}
	defer tx.Rollback(context.Background())
	if err = assignment.Lock(ctx, tx); err != nil {
		return ThreadResult{}, err
	}
	replay, err := reserveThread(ctx, tx, user, "ticket.merge", key, map[string]int64{"source_id": source, "target_id": target})
	if err != nil {
		return ThreadResult{}, err
	}
	if replay != nil {
		return *replay, tx.Commit(ctx)
	}
	// Every organizer uses the routing lock; lock rows in ID order as well.
	ids := []int64{source, target}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	locked := map[int64]Ticket{}
	for _, id := range ids {
		ticket, err := scanTicket(tx.QueryRow(ctx, selectTicket+`WHERE t.id=$1 FOR UPDATE OF t`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return ThreadResult{}, ErrNotFound
		}
		if err != nil {
			return ThreadResult{}, err
		}
		locked[id] = ticket
	}
	a, b := locked[source], locked[target]
	if a.RequesterID != b.RequesterID {
		return ThreadResult{}, ErrForbidden
	}
	if a.MergedIntoID != nil || b.MergedIntoID != nil || a.Status == "closed" || b.Status == "closed" {
		return ThreadResult{}, ErrTransition
	}
	comments, err := threadCommentIDs(ctx, tx, source)
	if err != nil {
		return ThreadResult{}, err
	}
	if err = moveComments(ctx, tx, target, comments); err != nil {
		return ThreadResult{}, err
	}
	// Existing aliases point directly to the new survivor, avoiding chains/cycles.
	_, err = tx.Exec(ctx, `UPDATE tickets SET merged_into_ticket_id=$2,updated_at=clock_timestamp() WHERE merged_into_ticket_id=$1`, source, target)
	if err != nil {
		return ThreadResult{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE tickets SET status='closed',merged_into_ticket_id=$2,updated_at=clock_timestamp() WHERE id=$1`, source, target)
	if err != nil {
		return ThreadResult{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE tickets SET updated_at=clock_timestamp() WHERE id=$1`, target)
	if err != nil {
		return ThreadResult{}, err
	}
	if err = s.sla.RecordStatus(ctx, tx, source, "closed"); err != nil {
		return ThreadResult{}, err
	}
	if err = s.assignment.ReconcileTx(ctx, tx, source, &user.ID, "ticket_merged"); err != nil {
		return ThreadResult{}, err
	}
	if err = s.assignment.FillQueueTx(ctx, tx, 20, &user.ID, "merge_capacity_released"); err != nil {
		return ThreadResult{}, err
	}
	result := ThreadResult{SourceID: source, TargetID: target, Moved: len(comments)}
	if err = finishThread(ctx, tx, user, "merge", key, result, comments); err != nil {
		return ThreadResult{}, err
	}
	return result, tx.Commit(ctx)
}
func (s *Service) Split(ctx context.Context, user auth.User, source int64, input SplitInput, key string) (ThreadResult, error) {
	if !canOrganize(user) {
		return ThreadResult{}, ErrForbidden
	}
	input.Title = strings.TrimSpace(input.Title)
	if source < 1 || !ValidKey(key) || !utf8.ValidString(input.Title) || strings.ContainsRune(input.Title, 0) || utf8.RuneCountInString(input.Title) < 1 || utf8.RuneCountInString(input.Title) > 200 || len(input.CommentIDs) < 1 || len(input.CommentIDs) > 100 {
		return ThreadResult{}, ErrInvalid
	}
	input.CommentIDs = append([]int64{}, input.CommentIDs...)
	sort.Slice(input.CommentIDs, func(i, j int) bool { return input.CommentIDs[i] < input.CommentIDs[j] })
	for i, id := range input.CommentIDs {
		if id < 1 || (i > 0 && id == input.CommentIDs[i-1]) {
			return ThreadResult{}, ErrInvalid
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ThreadResult{}, err
	}
	defer tx.Rollback(context.Background())
	if err = assignment.Lock(ctx, tx); err != nil {
		return ThreadResult{}, err
	}
	replay, err := reserveThread(ctx, tx, user, "ticket.split", key, map[string]any{"source_id": source, "input": input})
	if err != nil {
		return ThreadResult{}, err
	}
	if replay != nil {
		return *replay, tx.Commit(ctx)
	}
	old, err := scanTicket(tx.QueryRow(ctx, selectTicket+`WHERE t.id=$1 FOR UPDATE OF t`, source))
	if errors.Is(err, pgx.ErrNoRows) {
		return ThreadResult{}, ErrNotFound
	}
	if err != nil {
		return ThreadResult{}, err
	}
	if old.Status == "closed" || old.MergedIntoID != nil {
		return ThreadResult{}, ErrTransition
	}
	// Never derive a public description from an internal note.
	var count int
	err = tx.QueryRow(ctx, `SELECT count(*) FROM ticket_comments c LEFT JOIN comment_locations l ON l.comment_id=c.id WHERE COALESCE(l.ticket_id,c.ticket_id)=$1 AND c.id=ANY($2::bigint[])`, source, input.CommentIDs).Scan(&count)
	if err != nil {
		return ThreadResult{}, err
	}
	if count != len(input.CommentIDs) {
		return ThreadResult{}, ErrInvalid
	}
	var owner auth.User
	err = tx.QueryRow(ctx, `SELECT id,email,display_name,role_code FROM users WHERE id=$1`, old.RequesterID).Scan(&owner.ID, &owner.Email, &owner.DisplayName, &owner.Role)
	if err != nil {
		return ThreadResult{}, err
	}
	ticket, err := s.createTx(ctx, tx, owner, CreateInput{Title: input.Title, Description: "Başka bir talepten ayrılan konu. Ayrıntılar görülebilir mesajlarda yer alır.", Priority: old.Priority, Category: old.Category}, user.ID)
	if err != nil {
		return ThreadResult{}, err
	}
	if err = moveComments(ctx, tx, ticket.ID, input.CommentIDs); err != nil {
		return ThreadResult{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE tickets SET updated_at=clock_timestamp() WHERE id=$1`, source)
	if err != nil {
		return ThreadResult{}, err
	}
	result := ThreadResult{SourceID: source, TargetID: ticket.ID, Moved: len(input.CommentIDs)}
	if err = finishThread(ctx, tx, user, "split", key, result, input.CommentIDs); err != nil {
		return ThreadResult{}, err
	}
	return result, tx.Commit(ctx)
}
func (s *Service) ThreadHistory(ctx context.Context, user auth.User, id, before int64, limit int) ([]ThreadOperation, error) {
	if before < 0 || limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	if _, err := s.Get(ctx, user, id); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT o.id,o.kind,o.source_id,o.target_id,u.display_name,o.created_at FROM ticket_operations o JOIN users u ON u.id=o.actor_id WHERE (o.source_id=$1 OR o.target_id=$1) AND ($2::bigint=0 OR o.id<$2) ORDER BY o.id DESC LIMIT $3`, id, before, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ThreadOperation{}
	for rows.Next() {
		var op ThreadOperation
		if err = rows.Scan(&op.ID, &op.Kind, &op.SourceID, &op.TargetID, &op.ActorName, &op.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, op)
	}
	return result, rows.Err()
}
