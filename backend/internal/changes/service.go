package changes

import (
	"context"
	"crypto/sha256"
	"destekmasa/internal/assets"
	"destekmasa/internal/auth"
	"destekmasa/internal/tickets"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrInvalid    = errors.New("invalid change input")
	ErrForbidden  = errors.New("change access forbidden")
	ErrNotFound   = errors.New("change not found")
	ErrConflict   = errors.New("change version or idempotency conflict")
	ErrTransition = errors.New("invalid change transition")
	ErrStaleGraph = errors.New("graph changed; resubmit for review")
	ErrOverlap    = errors.New("conflicting maintenance window")
)

type Change struct {
	ID            int64          `json:"id"`
	RequesterID   int64          `json:"requester_id"`
	RequesterName string         `json:"requester_name"`
	Title         string         `json:"title"`
	Description   string         `json:"description"`
	RollbackPlan  string         `json:"rollback_plan"`
	Risk          string         `json:"risk"`
	StartsAt      time.Time      `json:"starts_at"`
	EndsAt        time.Time      `json:"ends_at"`
	Status        string         `json:"status"`
	Version       int64          `json:"version"`
	RootIDs       []int64        `json:"root_ids"`
	GraphVersion  *int64         `json:"graph_version"`
	ApprovedBy    *int64         `json:"approved_by"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	Impact        *assets.Impact `json:"impact"`
}
type Input struct {
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	RollbackPlan string    `json:"rollback_plan"`
	Risk         string    `json:"risk"`
	StartsAt     time.Time `json:"starts_at"`
	EndsAt       time.Time `json:"ends_at"`
	RootIDs      []int64   `json:"root_ids"`
	Version      int64     `json:"version"`
}
type ActionInput struct {
	Action  string `json:"action"`
	Note    string `json:"note"`
	Version int64  `json:"version"`
}
type Conflict struct {
	ID         int64     `json:"id"`
	Title      string    `json:"title"`
	Status     string    `json:"status"`
	StartsAt   time.Time `json:"starts_at"`
	EndsAt     time.Time `json:"ends_at"`
	StaleGraph bool      `json:"stale_graph"`
}
type ConflictError struct{ Items []Conflict }

func (e *ConflictError) Error() string { return "overlapping approved changes" }
func (e *ConflictError) Unwrap() error { return ErrOverlap }

type Preview struct {
	Impact    assets.Impact `json:"impact"`
	Conflicts []Conflict    `json:"conflicts"`
	Stale     bool          `json:"stale"`
}
type Event struct {
	ID         int64     `json:"id"`
	ActorName  string    `json:"actor_name"`
	Action     string    `json:"action"`
	FromStatus *string   `json:"from_status"`
	ToStatus   string    `json:"to_status"`
	Note       string    `json:"note"`
	CreatedAt  time.Time `json:"created_at"`
}
type Service struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func New(pool *pgxpool.Pool, clock func() time.Time) *Service {
	if clock == nil {
		clock = time.Now
	}
	return &Service{pool: pool, now: clock}
}
func CanRead(role string) bool { return assets.CanRead(role) }
func canReview(user auth.User, c Change) bool {
	return (user.Role == "team_lead" || user.Role == "admin") && user.ID != c.RequesterID
}
func canOwn(user auth.User, c Change) bool { return user.ID == c.RequesterID || user.Role == "admin" }
func validText(s string, max int) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0) && utf8.RuneCountInString(s) >= 1 && utf8.RuneCountInString(s) <= max
}
func normalize(in Input) (Input, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Description = strings.TrimSpace(in.Description)
	in.RollbackPlan = strings.TrimSpace(in.RollbackPlan)
	in.StartsAt = in.StartsAt.UTC()
	in.EndsAt = in.EndsAt.UTC()
	if !validText(in.Title, 200) || !validText(in.Description, 10000) || !validText(in.RollbackPlan, 10000) || !in.EndsAt.After(in.StartsAt) || in.EndsAt.Sub(in.StartsAt) > 7*24*time.Hour || len(in.RootIDs) < 1 || len(in.RootIDs) > 20 {
		return in, ErrInvalid
	}
	switch in.Risk {
	case "low", "normal", "high":
	default:
		return in, ErrInvalid
	}
	in.RootIDs = append([]int64{}, in.RootIDs...)
	sort.Slice(in.RootIDs, func(i, j int) bool { return in.RootIDs[i] < in.RootIDs[j] })
	for i, id := range in.RootIDs {
		if id < 1 || (i > 0 && id == in.RootIDs[i-1]) {
			return in, ErrInvalid
		}
	}
	return in, nil
}

const selectChange = `SELECT c.id,c.requester_id,u.display_name,c.title,c.description,c.rollback_plan,c.risk,c.starts_at,c.ends_at,c.status,c.version,
 ARRAY(SELECT asset_id FROM change_roots WHERE change_id=c.id ORDER BY asset_id),c.graph_version,c.approved_by,c.created_at,c.updated_at FROM change_requests c JOIN users u ON u.id=c.requester_id `

func scan(row pgx.Row) (Change, error) {
	var c Change
	err := row.Scan(&c.ID, &c.RequesterID, &c.RequesterName, &c.Title, &c.Description, &c.RollbackPlan, &c.Risk, &c.StartsAt, &c.EndsAt, &c.Status, &c.Version, &c.RootIDs, &c.GraphVersion, &c.ApprovedBy, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return c, err
}
func record(ctx context.Context, tx pgx.Tx, user auth.User, action, note string, old *Change, next Change) error {
	var from *string
	if old != nil {
		from = &old.Status
	}
	_, err := tx.Exec(ctx, `INSERT INTO change_events(change_id,actor_id,action,from_status,to_status,note) VALUES($1,$2,$3,$4,$5,$6)`, next.ID, user.ID, action, from, next.Status, note)
	if err != nil {
		return err
	}
	// The full immutable impact is stored on the plan; audit avoids copying it.
	before, err := json.Marshal(old)
	if err != nil {
		return err
	}
	after, err := json.Marshal(next)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_log(actor_id,action,entity_type,entity_id,old_value,new_value) VALUES($1,$2,'change',$3,$4,$5)`, user.ID, "change."+action, next.ID, before, after)
	return err
}
func reserve(ctx context.Context, tx pgx.Tx, user auth.User, operation, key string, input any) (*Change, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(body)
	_, err = tx.Exec(ctx, `INSERT INTO idempotency_keys(user_id,operation,key,request_hash) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, user.ID, operation, key, hash[:])
	if err != nil {
		return nil, err
	}
	var saved, response []byte
	err = tx.QueryRow(ctx, `SELECT request_hash,response_body FROM idempotency_keys WHERE user_id=$1 AND operation=$2 AND key=$3 FOR UPDATE`, user.ID, operation, key).Scan(&saved, &response)
	if err != nil {
		return nil, err
	}
	if string(saved) != string(hash[:]) {
		return nil, ErrConflict
	}
	if response != nil {
		var c Change
		if err = json.Unmarshal(response, &c); err != nil {
			return nil, err
		}
		return &c, nil
	}
	return nil, nil
}
func finish(ctx context.Context, tx pgx.Tx, user auth.User, operation, key string, c Change) error {
	body, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE idempotency_keys SET response_body=$4 WHERE user_id=$1 AND operation=$2 AND key=$3`, user.ID, operation, key, body)
	return err
}
func validateRoots(impact assets.Impact, roots []int64) error {
	for _, id := range roots {
		for _, node := range impact.Items {
			if node.ID == id && node.State == "retired" {
				return ErrInvalid
			}
		}
	}
	return nil
}
func (s *Service) Create(ctx context.Context, user auth.User, in Input, key string) (Change, error) {
	if !CanRead(user.Role) {
		return Change{}, ErrForbidden
	}
	in, err := normalize(in)
	if err != nil || in.Version != 0 || !tickets.ValidKey(key) {
		return Change{}, ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Change{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = assets.Lock(ctx, tx); err != nil {
		return Change{}, err
	}
	replay, err := reserve(ctx, tx, user, "change.create", key, in)
	if err != nil {
		return Change{}, err
	}
	if replay != nil {
		return *replay, tx.Commit(ctx)
	}
	if !in.EndsAt.After(s.now()) || in.StartsAt.After(s.now().Add(366*24*time.Hour)) {
		return Change{}, ErrInvalid
	}
	impact, err := assets.SnapshotTx(ctx, tx, in.RootIDs)
	if err != nil {
		return Change{}, err
	}
	if err = validateRoots(impact, in.RootIDs); err != nil {
		return Change{}, err
	}
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO change_requests(requester_id,title,description,rollback_plan,risk,starts_at,ends_at) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`, user.ID, in.Title, in.Description, in.RollbackPlan, in.Risk, in.StartsAt, in.EndsAt).Scan(&id)
	if err != nil {
		return Change{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO change_roots(change_id,asset_id) SELECT $1,unnest($2::bigint[])`, id, in.RootIDs)
	if err != nil {
		return Change{}, err
	}
	c, err := scan(tx.QueryRow(ctx, selectChange+`WHERE c.id=$1`, id))
	if err != nil {
		return Change{}, err
	}
	if err = record(ctx, tx, user, "create", "Taslak oluşturuldu.", nil, c); err != nil {
		return Change{}, err
	}
	if err = finish(ctx, tx, user, "change.create", key, c); err != nil {
		return Change{}, err
	}
	return c, tx.Commit(ctx)
}
func (s *Service) Update(ctx context.Context, user auth.User, id int64, in Input) (Change, error) {
	if !CanRead(user.Role) {
		return Change{}, ErrForbidden
	}
	in, err := normalize(in)
	if err != nil || in.Version < 1 {
		return Change{}, ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Change{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = assets.Lock(ctx, tx); err != nil {
		return Change{}, err
	}
	old, err := scan(tx.QueryRow(ctx, selectChange+`WHERE c.id=$1 FOR UPDATE OF c`, id))
	if err != nil {
		return Change{}, err
	}
	if !canOwn(user, old) {
		return Change{}, ErrForbidden
	}
	if old.Version != in.Version {
		return Change{}, ErrConflict
	}
	if old.Status != "draft" {
		return Change{}, ErrTransition
	}
	if !in.EndsAt.After(s.now()) || in.StartsAt.After(s.now().Add(366*24*time.Hour)) {
		return Change{}, ErrInvalid
	}
	impact, err := assets.SnapshotTx(ctx, tx, in.RootIDs)
	if err != nil {
		return Change{}, err
	}
	if err = validateRoots(impact, in.RootIDs); err != nil {
		return Change{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE change_requests SET title=$2,description=$3,rollback_plan=$4,risk=$5,starts_at=$6,ends_at=$7,graph_version=NULL,impact_snapshot=NULL,version=version+1,updated_at=clock_timestamp() WHERE id=$1`, id, in.Title, in.Description, in.RollbackPlan, in.Risk, in.StartsAt, in.EndsAt)
	if err != nil {
		return Change{}, err
	}
	_, err = tx.Exec(ctx, `DELETE FROM change_roots WHERE change_id=$1`, id)
	if err != nil {
		return Change{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO change_roots(change_id,asset_id) SELECT $1,unnest($2::bigint[])`, id, in.RootIDs)
	if err != nil {
		return Change{}, err
	}
	c, err := scan(tx.QueryRow(ctx, selectChange+`WHERE c.id=$1`, id))
	if err != nil {
		return Change{}, err
	}
	if err = record(ctx, tx, user, "update", "Taslak planı güncellendi.", &old, c); err != nil {
		return Change{}, err
	}
	return c, tx.Commit(ctx)
}
func conflicts(ctx context.Context, tx pgx.Tx, c Change, impact assets.Impact) ([]Conflict, error) {
	ids := []int64{}
	for _, a := range impact.Items {
		ids = append(ids, a.ID)
	}
	rows, err := tx.Query(ctx, `SELECT c.id,c.title,c.status,c.starts_at,c.ends_at,c.graph_version<>$5 FROM change_requests c WHERE c.id<>$1 AND c.status IN ('approved','implementing')
 AND tstzrange(c.starts_at,c.ends_at,'[)') && tstzrange($2::timestamptz,$3::timestamptz,'[)')
 AND (c.graph_version<>$5 OR EXISTS(SELECT 1 FROM change_impacts i WHERE i.change_id=c.id AND i.asset_id=ANY($4::bigint[]))) ORDER BY c.id LIMIT 101`, c.ID, c.StartsAt, c.EndsAt, ids, impact.GraphVersion)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Conflict{}
	for rows.Next() {
		var item Conflict
		if err = rows.Scan(&item.ID, &item.Title, &item.Status, &item.StartsAt, &item.EndsAt, &item.StaleGraph); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
func (s *Service) Action(ctx context.Context, user auth.User, id int64, in ActionInput, key string) (Change, error) {
	if !CanRead(user.Role) {
		return Change{}, ErrForbidden
	}
	in.Note = strings.TrimSpace(in.Note)
	if in.Version < 1 || !validText(in.Note, 2000) || utf8.RuneCountInString(in.Note) < 3 || !tickets.ValidKey(key) {
		return Change{}, ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Change{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = assets.Lock(ctx, tx); err != nil {
		return Change{}, err
	}
	old, err := scan(tx.QueryRow(ctx, selectChange+`WHERE c.id=$1 FOR UPDATE OF c`, id))
	if err != nil {
		return Change{}, err
	}
	review := in.Action == "approve" || in.Action == "reject" || in.Action == "request_changes"
	if review {
		if !canReview(user, old) {
			return Change{}, ErrForbidden
		}
	} else if !canOwn(user, old) {
		return Change{}, ErrForbidden
	}
	operation := fmt.Sprintf("change.action:%d", id)
	replay, err := reserve(ctx, tx, user, operation, key, in)
	if err != nil {
		return Change{}, err
	}
	if replay != nil {
		return *replay, tx.Commit(ctx)
	}
	if old.Version != in.Version {
		return Change{}, ErrConflict
	}
	next := ""
	switch in.Action {
	case "submit":
		if old.Status == "draft" {
			next = "submitted"
		}
	case "approve":
		if old.Status == "submitted" {
			next = "approved"
		}
	case "reject":
		if old.Status == "submitted" {
			next = "rejected"
		}
	case "request_changes":
		if old.Status == "submitted" {
			next = "draft"
		}
	case "revise":
		if old.Status == "submitted" || old.Status == "approved" {
			next = "draft"
		}
	case "cancel":
		if old.Status == "draft" || old.Status == "submitted" || old.Status == "approved" {
			next = "cancelled"
		}
	case "start":
		if old.Status == "approved" {
			next = "implementing"
		}
	case "complete":
		if old.Status == "implementing" {
			next = "completed"
		}
	case "fail":
		if old.Status == "implementing" {
			next = "failed"
		}
	default:
		return Change{}, ErrInvalid
	}
	if next == "" {
		return Change{}, ErrTransition
	}
	if in.Action == "start" && (s.now().Before(old.StartsAt) || !s.now().Before(old.EndsAt)) {
		return Change{}, ErrTransition
	}
	if in.Action == "submit" || in.Action == "approve" || in.Action == "start" {
		if !s.now().Before(old.EndsAt) {
			return Change{}, ErrInvalid
		}
		impact, err := assets.SnapshotTx(ctx, tx, old.RootIDs)
		if err != nil {
			return Change{}, err
		}
		if err = validateRoots(impact, old.RootIDs); err != nil {
			return Change{}, err
		}
		if in.Action != "submit" && (old.GraphVersion == nil || *old.GraphVersion != impact.GraphVersion) {
			return Change{}, ErrStaleGraph
		}
		overlap, err := conflicts(ctx, tx, old, impact)
		if err != nil {
			return Change{}, err
		}
		if len(overlap) > 0 {
			return Change{}, &ConflictError{Items: overlap}
		}
		if in.Action == "submit" {
			snapshot, err := json.Marshal(impact)
			if err != nil {
				return Change{}, err
			}
			_, err = tx.Exec(ctx, `UPDATE change_requests SET graph_version=$2,impact_snapshot=$3 WHERE id=$1`, id, impact.GraphVersion, snapshot)
			if err != nil {
				return Change{}, err
			}
			_, err = tx.Exec(ctx, `DELETE FROM change_impacts WHERE change_id=$1`, id)
			if err != nil {
				return Change{}, err
			}
			ids := []int64{}
			for _, a := range impact.Items {
				ids = append(ids, a.ID)
			}
			_, err = tx.Exec(ctx, `INSERT INTO change_impacts(change_id,asset_id) SELECT $1,unnest($2::bigint[])`, id, ids)
			if err != nil {
				return Change{}, err
			}
		}
	}
	if next == "draft" {
		_, err = tx.Exec(ctx, `UPDATE change_requests SET status='draft',approved_by=NULL,graph_version=NULL,impact_snapshot=NULL WHERE id=$1`, id)
		if err != nil {
			return Change{}, err
		}
		_, err = tx.Exec(ctx, `DELETE FROM change_impacts WHERE change_id=$1`, id)
		if err != nil {
			return Change{}, err
		}
	}
	if next == "approved" {
		_, err = tx.Exec(ctx, `UPDATE change_requests SET approved_by=$2 WHERE id=$1`, id, user.ID)
		if err != nil {
			return Change{}, err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE change_requests SET status=$2,version=version+1,updated_at=clock_timestamp() WHERE id=$1`, id, next)
	if err != nil {
		return Change{}, err
	}
	c, err := scan(tx.QueryRow(ctx, selectChange+`WHERE c.id=$1`, id))
	if err != nil {
		return Change{}, err
	}
	if err = record(ctx, tx, user, in.Action, in.Note, &old, c); err != nil {
		return Change{}, err
	}
	if err = finish(ctx, tx, user, operation, key, c); err != nil {
		return Change{}, err
	}
	return c, tx.Commit(ctx)
}
func (s *Service) Get(ctx context.Context, user auth.User, id int64) (Change, error) {
	if !CanRead(user.Role) {
		return Change{}, ErrForbidden
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Change{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	c, err := scan(tx.QueryRow(ctx, selectChange+`WHERE c.id=$1`, id))
	if err != nil {
		return Change{}, err
	}
	var data []byte
	if err = tx.QueryRow(ctx, `SELECT impact_snapshot FROM change_requests WHERE id=$1`, id).Scan(&data); err != nil {
		return Change{}, err
	}
	if data != nil {
		var impact assets.Impact
		if err = json.Unmarshal(data, &impact); err != nil {
			return Change{}, err
		}
		c.Impact = &impact
	}
	return c, tx.Commit(ctx)
}
func (s *Service) Preview(ctx context.Context, user auth.User, id int64) (Preview, error) {
	if !CanRead(user.Role) {
		return Preview{}, ErrForbidden
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Preview{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	c, err := scan(tx.QueryRow(ctx, selectChange+`WHERE c.id=$1`, id))
	if err != nil {
		return Preview{}, err
	}
	impact, err := assets.SnapshotTx(ctx, tx, c.RootIDs)
	if err != nil {
		return Preview{}, err
	}
	overlap, err := conflicts(ctx, tx, c, impact)
	if err != nil {
		return Preview{}, err
	}
	result := Preview{Impact: impact, Conflicts: overlap, Stale: c.GraphVersion != nil && *c.GraphVersion != impact.GraphVersion}
	return result, tx.Commit(ctx)
}
func (s *Service) List(ctx context.Context, user auth.User, before int64, limit int, status string) ([]Change, error) {
	if !CanRead(user.Role) {
		return nil, ErrForbidden
	}
	if before < 0 || limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	switch status {
	case "", "draft", "submitted", "approved", "implementing", "completed", "failed", "rejected", "cancelled":
	default:
		return nil, ErrInvalid
	}
	rows, err := s.pool.Query(ctx, selectChange+`WHERE ($1::bigint=0 OR c.id<$1) AND ($2::text='' OR c.status=$2) ORDER BY c.id DESC LIMIT $3`, before, status, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Change{}
	for rows.Next() {
		c, err := scan(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}
func (s *Service) History(ctx context.Context, user auth.User, id, before int64, limit int) ([]Event, error) {
	if !CanRead(user.Role) {
		return nil, ErrForbidden
	}
	if before < 0 || limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM change_requests WHERE id=$1)`, id).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `SELECT e.id,u.display_name,e.action,e.from_status,e.to_status,e.note,e.created_at FROM change_events e JOIN users u ON u.id=e.actor_id WHERE e.change_id=$1 AND ($2::bigint=0 OR e.id<$2) ORDER BY e.id DESC LIMIT $3`, id, before, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Event{}
	for rows.Next() {
		var e Event
		if err = rows.Scan(&e.ID, &e.ActorName, &e.Action, &e.FromStatus, &e.ToStatus, &e.Note, &e.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}
