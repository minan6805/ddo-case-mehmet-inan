package tickets

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"destekmasa/internal/assignment"
	"destekmasa/internal/auth"
	"destekmasa/internal/sla"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInvalid    = errors.New("invalid ticket input")
	ErrNotFound   = errors.New("ticket not found")
	ErrForbidden  = errors.New("ticket access forbidden")
	ErrConflict   = errors.New("idempotency key conflict")
	ErrTransition = errors.New("invalid status transition")
)

type Ticket struct {
	MergedIntoID  *int64    `json:"merged_into_ticket_id"`
	ID            int64     `json:"id"`
	RequesterID   int64     `json:"requester_id"`
	RequesterName string    `json:"requester_name"`
	AssigneeID    *int64    `json:"assignee_id"`
	AssigneeName  string    `json:"assignee_name"`
	Title         string    `json:"title"`
	Description   string    `json:"description"`
	Status        string    `json:"status"`
	Priority      string    `json:"priority"`
	Category      string    `json:"category"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type CreateInput struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    string `json:"priority"`
	Category    string `json:"category"`
}

type Category struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}
type ListOptions struct {
	Before int64
	Limit  int
	Status string
	Queue  string
}
type Page struct {
	Items      []Ticket `json:"items"`
	NextCursor string   `json:"next_cursor"`
}
type Service struct {
	pool       *pgxpool.Pool
	sla        *sla.Service
	assignment *assignment.Service
}

func New(pool *pgxpool.Pool) *Service { return NewWithSLA(pool, sla.New(pool, nil)) }
func NewWithSLA(pool *pgxpool.Pool, engine *sla.Service) *Service {
	return NewWithEngines(pool, engine, assignment.New(pool, nil))
}

func NewWithEngines(pool *pgxpool.Pool, engine *sla.Service, routing *assignment.Service) *Service {
	return &Service{pool: pool, sla: engine, assignment: routing}
}

func IsStaff(role string) bool { return role == "specialist" || role == "team_lead" || role == "admin" }
func ValidStatus(value string) bool {
	switch value {
	case "open", "in_progress", "waiting_customer", "resolved", "closed":
		return true
	}
	return false
}
func ValidTransition(from, to string) bool {
	switch from {
	case "open":
		return to == "in_progress"
	case "in_progress":
		return to == "waiting_customer" || to == "resolved"
	case "waiting_customer":
		return to == "in_progress"
	case "resolved":
		return to == "in_progress" || to == "closed"
	}
	return false
}
func normalize(input CreateInput) (CreateInput, error) {
	input.Title, input.Description = strings.TrimSpace(input.Title), strings.TrimSpace(input.Description)
	if !utf8.ValidString(input.Title) || !utf8.ValidString(input.Description) || utf8.RuneCountInString(input.Title) < 1 ||
		utf8.RuneCountInString(input.Title) > 200 || utf8.RuneCountInString(input.Description) < 1 || utf8.RuneCountInString(input.Description) > 10000 {
		return input, ErrInvalid
	}
	switch input.Priority {
	case "low", "normal", "high", "critical":
	default:
		return input, ErrInvalid
	}
	if input.Category == "" || len(input.Category) > 50 {
		return input, ErrInvalid
	}
	return input, nil
}
func ValidKey(key string) bool {
	if len(key) < 1 || len(key) > 128 {
		return false
	}
	for _, char := range key {
		if char < 33 || char > 126 {
			return false
		}
	}
	return true
}

const selectTicket = `SELECT t.id,t.requester_id,u.display_name,t.title,t.description,t.status,t.priority,t.category_code,t.created_at,t.updated_at,t.assignee_id,COALESCE(a.display_name,''),t.merged_into_ticket_id
    FROM tickets t JOIN users u ON u.id=t.requester_id LEFT JOIN users a ON a.id=t.assignee_id `

func scanTicket(row pgx.Row) (Ticket, error) {
	var ticket Ticket
	err := row.Scan(&ticket.ID, &ticket.RequesterID, &ticket.RequesterName, &ticket.Title, &ticket.Description,
		&ticket.Status, &ticket.Priority, &ticket.Category, &ticket.CreatedAt, &ticket.UpdatedAt, &ticket.AssigneeID, &ticket.AssigneeName, &ticket.MergedIntoID)
	return ticket, err
}

func (s *Service) Categories(ctx context.Context) ([]Category, error) {
	rows, err := s.pool.Query(ctx, `SELECT code,label FROM ticket_categories ORDER BY label`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Category{}
	for rows.Next() {
		var category Category
		if err = rows.Scan(&category.Code, &category.Label); err != nil {
			return nil, err
		}
		result = append(result, category)
	}
	return result, rows.Err()
}

func (s *Service) Create(ctx context.Context, user auth.User, input CreateInput, key string) (Ticket, error) {
	input, err := normalize(input)
	if err != nil || !ValidKey(key) {
		return Ticket{}, ErrInvalid
	}
	body, err := json.Marshal(input)
	if err != nil {
		return Ticket{}, err
	}
	hash := sha256.Sum256(body)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Ticket{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = assignment.Lock(ctx, tx); err != nil {
		return Ticket{}, err
	}
	// The UNIQUE key waits for an in-flight transaction using the same key.
	// Both the ticket and its replay response commit together.
	if _, err = tx.Exec(ctx, `INSERT INTO idempotency_keys(user_id,operation,key,request_hash)
        VALUES($1,'ticket.create',$2,$3) ON CONFLICT DO NOTHING`, user.ID, key, hash[:]); err != nil {
		return Ticket{}, err
	}
	var storedHash, response []byte
	if err = tx.QueryRow(ctx, `SELECT request_hash,response_body FROM idempotency_keys
        WHERE user_id=$1 AND operation='ticket.create' AND key=$2 FOR UPDATE`, user.ID, key).Scan(&storedHash, &response); err != nil {
		return Ticket{}, err
	}
	if string(storedHash) != string(hash[:]) {
		return Ticket{}, ErrConflict
	}
	if response != nil {
		var ticket Ticket
		if err = json.Unmarshal(response, &ticket); err != nil {
			return Ticket{}, err
		}
		return ticket, tx.Commit(ctx)
	}
	ticket, err := s.createTx(ctx, tx, user, input, user.ID)
	if err != nil {
		return Ticket{}, err
	}
	response, err = json.Marshal(ticket)
	if err != nil {
		return Ticket{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE idempotency_keys SET response_body=$3 WHERE user_id=$1 AND operation='ticket.create' AND key=$2`, user.ID, key, response); err != nil {
		return Ticket{}, err
	}
	return ticket, tx.Commit(ctx)
}

// createTx requires assignment.Lock and normalized input. The caller owns commit,
// allowing an incoming email, ticket, SLA, routing and attachments to be atomic.
func (s *Service) createTx(ctx context.Context, tx pgx.Tx, user auth.User, input CreateInput, actorID int64) (Ticket, error) {
	var categoryExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ticket_categories WHERE code=$1)`, input.Category).Scan(&categoryExists); err != nil {
		return Ticket{}, err
	}
	if !categoryExists {
		return Ticket{}, ErrInvalid
	}
	var id int64
	if err := tx.QueryRow(ctx, `INSERT INTO tickets(requester_id,title,description,priority,category_code)
        VALUES($1,$2,$3,$4,$5) RETURNING id`, user.ID, input.Title, input.Description, input.Priority, input.Category).Scan(&id); err != nil {
		return Ticket{}, err
	}
	if err := s.assignment.ReconcileTx(ctx, tx, id, &actorID, "ticket_created"); err != nil {
		return Ticket{}, err
	}
	ticket, err := scanTicket(tx.QueryRow(ctx, selectTicket+`WHERE t.id=$1`, id))
	if err != nil {
		return Ticket{}, err
	}
	if err = s.sla.Initialize(ctx, tx, ticket.ID); err != nil {
		return Ticket{}, err
	}
	response, err := json.Marshal(ticket)
	if err != nil {
		return Ticket{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(actor_id,action,entity_type,entity_id,new_value) VALUES($1,'ticket.create','ticket',$2,$3)`, actorID, ticket.ID, response); err != nil {
		return Ticket{}, err
	}
	return ticket, nil
}

func (s *Service) Get(ctx context.Context, user auth.User, id int64) (Ticket, error) {
	ticket, err := scanTicket(s.pool.QueryRow(ctx, selectTicket+`WHERE t.id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Ticket{}, ErrNotFound
	}
	if err != nil {
		return Ticket{}, err
	}
	if !IsStaff(user.Role) && ticket.RequesterID != user.ID {
		return Ticket{}, ErrForbidden
	}
	return ticket, nil
}

func (s *Service) List(ctx context.Context, user auth.User, options ListOptions) ([]Ticket, error) {
	if options.Queue == "" {
		options.Queue = "all"
	}
	if options.Queue != "all" && options.Queue != "mine" && options.Queue != "unassigned" {
		return nil, ErrInvalid
	}
	if options.Queue != "all" && !IsStaff(user.Role) {
		return nil, ErrForbidden
	}
	if options.Limit < 1 || options.Limit > 100 || options.Before < 0 || (options.Status != "" && !ValidStatus(options.Status)) {
		return nil, ErrInvalid
	}
	rows, err := s.pool.Query(ctx, selectTicket+`WHERE ($1::boolean OR t.requester_id=$2)
        AND ($3::bigint=0 OR t.id<$3) AND ($4::text='' OR t.status=$4)
        AND ($6::text='all' OR ($6='mine' AND t.assignee_id=$2) OR ($6='unassigned' AND t.assignee_id IS NULL AND t.status IN ('open','in_progress','waiting_customer')))
        ORDER BY t.id DESC LIMIT $5`, IsStaff(user.Role), user.ID, options.Before, options.Status, options.Limit+1, options.Queue)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Ticket{}
	for rows.Next() {
		ticket, err := scanTicket(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, ticket)
	}
	return result, rows.Err()
}

func (s *Service) UpdateStatus(ctx context.Context, user auth.User, id int64, next string) (Ticket, error) {
	if !IsStaff(user.Role) {
		return Ticket{}, ErrForbidden
	}
	if !ValidStatus(next) {
		return Ticket{}, ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Ticket{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = assignment.Lock(ctx, tx); err != nil {
		return Ticket{}, err
	}
	old, err := scanTicket(tx.QueryRow(ctx, selectTicket+`WHERE t.id=$1 FOR UPDATE OF t`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Ticket{}, ErrNotFound
	}
	if err != nil {
		return Ticket{}, err
	}
	if old.Status == next {
		return old, tx.Commit(ctx)
	}
	if !ValidTransition(old.Status, next) {
		return Ticket{}, ErrTransition
	}
	if _, err = tx.Exec(ctx, `UPDATE tickets SET status=$2,updated_at=clock_timestamp() WHERE id=$1`, id, next); err != nil {
		return Ticket{}, err
	}
	if err = s.assignment.ReconcileTx(ctx, tx, id, &user.ID, "status_changed"); err != nil {
		return Ticket{}, err
	}
	if next == "resolved" || next == "closed" {
		if err = s.assignment.FillQueueTx(ctx, tx, 20, &user.ID, "capacity_released"); err != nil {
			return Ticket{}, err
		}
	}
	ticket, err := scanTicket(tx.QueryRow(ctx, selectTicket+`WHERE t.id=$1`, id))
	if err != nil {
		return Ticket{}, err
	}
	if err = s.sla.RecordStatus(ctx, tx, id, next); err != nil {
		return Ticket{}, err
	}
	previous, err := json.Marshal(map[string]string{"status": old.Status})
	if err != nil {
		return Ticket{}, err
	}
	current, err := json.Marshal(map[string]string{"status": next})
	if err != nil {
		return Ticket{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(actor_id,action,entity_type,entity_id,old_value,new_value)
        VALUES($1,'ticket.status','ticket',$2,$3,$4)`, user.ID, id, previous, current); err != nil {
		return Ticket{}, err
	}
	return ticket, tx.Commit(ctx)
}
