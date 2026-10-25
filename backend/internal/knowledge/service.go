// Package knowledge manages reviewed support articles. Readers never receive drafts.
package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"destekmasa/internal/auth"
	"destekmasa/internal/tickets"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInvalid   = errors.New("invalid article input")
	ErrForbidden = errors.New("article write forbidden")
	ErrNotFound  = errors.New("article not found")
	ErrConflict  = errors.New("article version or idempotency conflict")
)

type Article struct {
	ID         int64     `json:"id"`
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	Category   string    `json:"category"`
	Status     string    `json:"status"`
	AuthorID   int64     `json:"author_id"`
	AuthorName string    `json:"author_name"`
	Version    int64     `json:"version"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}
type Input struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	Category string `json:"category"`
	Status   string `json:"status"`
	Version  int64  `json:"version"`
}
type Filter struct {
	Before                  int64
	Limit                   int
	Query, Category, Status string
}
type Service struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }
func CanWrite(role string) bool       { return tickets.IsStaff(role) }
func CanPublish(role string) bool     { return role == "team_lead" || role == "admin" }
func validText(s string, max int) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0) && utf8.RuneCountInString(s) >= 1 && utf8.RuneCountInString(s) <= max
}
func normalize(in Input) (Input, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Body = strings.TrimSpace(in.Body)
	if !validText(in.Title, 200) || !validText(in.Body, 10000) || !validText(in.Category, 64) {
		return in, ErrInvalid
	}
	switch in.Status {
	case "draft", "published", "archived":
	default:
		return in, ErrInvalid
	}
	return in, nil
}

const selectArticle = `SELECT a.id,a.title,a.body,a.category_code,a.status,a.author_id,u.display_name,a.version,a.created_at,a.updated_at FROM knowledge_articles a JOIN users u ON u.id=a.author_id `

func scan(row pgx.Row) (Article, error) {
	var a Article
	err := row.Scan(&a.ID, &a.Title, &a.Body, &a.Category, &a.Status, &a.AuthorID, &a.AuthorName, &a.Version, &a.CreatedAt, &a.UpdatedAt)
	return a, err
}
func translate(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var p *pgconn.PgError
	if errors.As(err, &p) && (p.Code == "23503" || p.Code == "23514") {
		return ErrInvalid
	}
	return err
}
func (s *Service) List(ctx context.Context, user auth.User, f Filter) ([]Article, error) {
	f.Query = strings.TrimSpace(f.Query)
	if f.Before < 0 || f.Limit < 1 || f.Limit > 100 || !utf8.ValidString(f.Query) || strings.ContainsRune(f.Query, 0) || utf8.RuneCountInString(f.Query) > 200 || (f.Category != "" && !validText(f.Category, 64)) {
		return nil, ErrInvalid
	}
	switch f.Status {
	case "", "draft", "published", "archived":
	default:
		return nil, ErrInvalid
	}
	// strpos searches literal text: '%' and '_' are not wildcard expressions.
	rows, err := s.pool.Query(ctx, selectArticle+`WHERE ($1::bigint=0 OR a.id<$1) AND ($2::text='' OR a.category_code=$2) AND ($3::text='' OR a.status=$3) AND ($4::boolean OR a.status='published') AND ($5::text='' OR strpos(lower(a.title),lower($5))>0 OR strpos(lower(a.body),lower($5))>0) ORDER BY a.id DESC LIMIT $6`, f.Before, f.Category, f.Status, CanWrite(user.Role), f.Query, f.Limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Article{}
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}
func (s *Service) Detail(ctx context.Context, user auth.User, id int64) (Article, error) {
	if id < 1 {
		return Article{}, ErrInvalid
	}
	a, err := scan(s.pool.QueryRow(ctx, selectArticle+`WHERE a.id=$1 AND ($2::boolean OR a.status='published')`, id, CanWrite(user.Role)))
	return a, translate(err)
}
func audit(ctx context.Context, tx pgx.Tx, user auth.User, action string, id int64, before, after any) error {
	old, err := json.Marshal(before)
	if err != nil {
		return err
	}
	next, err := json.Marshal(after)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_log(actor_id,action,entity_type,entity_id,old_value,new_value) VALUES($1,$2,'knowledge_article',$3,$4,$5)`, user.ID, action, id, old, next)
	return err
}
func (s *Service) Create(ctx context.Context, user auth.User, in Input, key string) (Article, error) {
	if !CanWrite(user.Role) {
		return Article{}, ErrForbidden
	}
	in, err := normalize(in)
	if err != nil || !tickets.ValidKey(key) || in.Version != 0 {
		return Article{}, ErrInvalid
	}
	if in.Status != "draft" {
		return Article{}, ErrInvalid
	}
	payload, err := json.Marshal(in)
	if err != nil {
		return Article{}, err
	}
	hash := sha256.Sum256(payload)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Article{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	_, err = tx.Exec(ctx, `INSERT INTO idempotency_keys(user_id,operation,key,request_hash) VALUES($1,'knowledge.create',$2,$3) ON CONFLICT DO NOTHING`, user.ID, key, hash[:])
	if err != nil {
		return Article{}, err
	}
	var stored, response []byte
	err = tx.QueryRow(ctx, `SELECT request_hash,response_body FROM idempotency_keys WHERE user_id=$1 AND operation='knowledge.create' AND key=$2 FOR UPDATE`, user.ID, key).Scan(&stored, &response)
	if err != nil {
		return Article{}, err
	}
	if string(stored) != string(hash[:]) {
		return Article{}, ErrConflict
	}
	if response != nil {
		var a Article
		if err = json.Unmarshal(response, &a); err != nil {
			return Article{}, err
		}
		return a, tx.Commit(ctx)
	}
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO knowledge_articles(title,body,category_code,author_id) VALUES($1,$2,$3,$4) RETURNING id`, in.Title, in.Body, in.Category, user.ID).Scan(&id)
	if err != nil {
		return Article{}, translate(err)
	}
	a, err := scan(tx.QueryRow(ctx, selectArticle+`WHERE a.id=$1`, id))
	if err != nil {
		return Article{}, err
	}
	if err = audit(ctx, tx, user, "knowledge.create", id, nil, a); err != nil {
		return Article{}, err
	}
	response, err = json.Marshal(a)
	if err != nil {
		return Article{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE idempotency_keys SET response_body=$3 WHERE user_id=$1 AND operation='knowledge.create' AND key=$2`, user.ID, key, response)
	if err != nil {
		return Article{}, err
	}
	return a, tx.Commit(ctx)
}
func (s *Service) Update(ctx context.Context, user auth.User, id int64, in Input) (Article, error) {
	if !CanWrite(user.Role) {
		return Article{}, ErrForbidden
	}
	in, err := normalize(in)
	if err != nil || id < 1 || in.Version < 1 {
		return Article{}, ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Article{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	old, err := scan(tx.QueryRow(ctx, selectArticle+`WHERE a.id=$1 FOR UPDATE OF a`, id))
	if err != nil {
		return Article{}, translate(err)
	}
	if !CanPublish(user.Role) && (old.AuthorID != user.ID || old.Status != "draft" || in.Status != "draft") {
		return Article{}, ErrForbidden
	}
	if old.Version != in.Version {
		return Article{}, ErrConflict
	}
	// Published content is never silently edited. Return to draft before revision;
	// that transition preserves the published content, then a new review is needed.
	if old.Status != "draft" && (in.Title != old.Title || in.Body != old.Body || in.Category != old.Category) {
		return Article{}, ErrInvalid
	}
	if old.Status == "archived" && in.Status == "published" {
		return Article{}, ErrInvalid
	}
	_, err = tx.Exec(ctx, `UPDATE knowledge_articles SET title=$2,body=$3,category_code=$4,status=$5,version=version+1,updated_at=clock_timestamp() WHERE id=$1`, id, in.Title, in.Body, in.Category, in.Status)
	if err != nil {
		return Article{}, translate(err)
	}
	a, err := scan(tx.QueryRow(ctx, selectArticle+`WHERE a.id=$1`, id))
	if err != nil {
		return Article{}, err
	}
	if err = audit(ctx, tx, user, "knowledge.update", id, old, a); err != nil {
		return Article{}, err
	}
	return a, tx.Commit(ctx)
}
