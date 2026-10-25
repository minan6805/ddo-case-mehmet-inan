package assets

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"regexp"
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
	ErrInvalid   = errors.New("invalid asset input")
	ErrForbidden = errors.New("asset access forbidden")
	ErrNotFound  = errors.New("asset not found")
	ErrConflict  = errors.New("asset version, code or idempotency conflict")
	ErrLimit     = errors.New("graph size exceeds supported limit")
)

const MaxNodes = 5000
const MaxEdges = 20000

// Lock also serializes CAB decisions with graph edits. No ticket/SLA locks are
// acquired by these transactions, avoiding a cross-module lock cycle.
func Lock(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7420184)`)
	return err
}
func CanRead(role string) bool {
	return role == "specialist" || role == "team_lead" || role == "asset_manager" || role == "admin"
}
func CanWrite(role string) bool { return role == "asset_manager" || role == "admin" }

type Asset struct {
	ID        int64     `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	State     string    `json:"state"`
	OwnerID   *int64    `json:"owner_id"`
	OwnerName string    `json:"owner_name"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
type Input struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	State   string `json:"state"`
	OwnerID *int64 `json:"owner_id"`
	Version int64  `json:"version"`
}
type Page struct {
	Items      []Asset `json:"items"`
	NextCursor string  `json:"next_cursor"`
}
type Affected struct {
	Asset
	Distance int     `json:"distance"`
	Path     []int64 `json:"path"`
}
type Impact struct {
	GraphVersion int64      `json:"graph_version"`
	Items        []Affected `json:"items"`
	Edges        []Edge     `json:"edges"`
}
type Detail struct {
	Asset      Asset   `json:"asset"`
	Providers  []Asset `json:"providers"`
	Dependents []Asset `json:"dependents"`
}
type Service struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

var codePattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9._-]{1,39}$`)

func normalize(in Input) (Input, error) {
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	in.Name = strings.TrimSpace(in.Name)
	if !codePattern.MatchString(in.Code) || !utf8.ValidString(in.Name) || strings.ContainsRune(in.Name, 0) || utf8.RuneCountInString(in.Name) < 1 || utf8.RuneCountInString(in.Name) > 160 || (in.OwnerID != nil && *in.OwnerID < 1) {
		return in, ErrInvalid
	}
	switch in.Kind {
	case "server", "service", "application", "network", "device":
	default:
		return in, ErrInvalid
	}
	switch in.State {
	case "active", "maintenance", "retired":
	default:
		return in, ErrInvalid
	}
	return in, nil
}

const selectAsset = `SELECT a.id,a.code,a.name,a.kind,a.state,a.owner_id,COALESCE(u.display_name,''),a.version,a.created_at,a.updated_at FROM assets a LEFT JOIN users u ON u.id=a.owner_id `

func scan(row pgx.Row) (Asset, error) {
	var a Asset
	err := row.Scan(&a.ID, &a.Code, &a.Name, &a.Kind, &a.State, &a.OwnerID, &a.OwnerName, &a.Version, &a.CreatedAt, &a.UpdatedAt)
	return a, err
}
func translate(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var e *pgconn.PgError
	if errors.As(err, &e) {
		if e.Code == "23505" {
			return ErrConflict
		}
		if e.Code == "23503" || e.Code == "23514" {
			return ErrInvalid
		}
	}
	return err
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
	_, err = tx.Exec(ctx, `INSERT INTO audit_log(actor_id,action,entity_type,entity_id,old_value,new_value) VALUES($1,$2,'asset',$3,$4,$5)`, user.ID, action, id, old, next)
	return err
}
func bump(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `UPDATE asset_graph_revision SET version=version+1`)
	return err
}
func (s *Service) List(ctx context.Context, user auth.User, before int64, limit int, kind string) ([]Asset, error) {
	if !CanRead(user.Role) {
		return nil, ErrForbidden
	}
	if before < 0 || limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	switch kind {
	case "", "server", "service", "application", "network", "device":
	default:
		return nil, ErrInvalid
	}
	rows, err := s.pool.Query(ctx, selectAsset+`WHERE ($1::bigint=0 OR a.id<$1) AND ($2::text='' OR a.kind=$2) ORDER BY a.id DESC LIMIT $3`, before, kind, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Asset{}
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}
func (s *Service) Detail(ctx context.Context, user auth.User, id int64) (Detail, error) {
	if !CanRead(user.Role) {
		return Detail{}, ErrForbidden
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Detail{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	a, err := scan(tx.QueryRow(ctx, selectAsset+`WHERE a.id=$1`, id))
	if err != nil {
		return Detail{}, translate(err)
	}
	result := Detail{Asset: a, Providers: []Asset{}, Dependents: []Asset{}}
	for _, providers := range []bool{true, false} {
		query := selectAsset + `JOIN asset_dependencies d ON d.dependent_id=a.id WHERE d.provider_id=$1 ORDER BY a.id LIMIT 1001`
		if providers {
			query = selectAsset + `JOIN asset_dependencies d ON d.provider_id=a.id WHERE d.dependent_id=$1 ORDER BY a.id LIMIT 1001`
		}
		rows, err := tx.Query(ctx, query, id)
		if err != nil {
			return Detail{}, err
		}
		items := []Asset{}
		for rows.Next() {
			a, err := scan(rows)
			if err != nil {
				rows.Close()
				return Detail{}, err
			}
			items = append(items, a)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return Detail{}, err
		}
		if len(items) > 1000 {
			return Detail{}, ErrLimit
		}
		if providers {
			result.Providers = items
		} else {
			result.Dependents = items
		}
	}
	return result, tx.Commit(ctx)
}
func (s *Service) Create(ctx context.Context, user auth.User, in Input, key string) (Asset, error) {
	if !CanWrite(user.Role) {
		return Asset{}, ErrForbidden
	}
	in, err := normalize(in)
	if err != nil || !tickets.ValidKey(key) || in.Version != 0 {
		return Asset{}, ErrInvalid
	}
	payload, err := json.Marshal(in)
	if err != nil {
		return Asset{}, err
	}
	hash := sha256.Sum256(payload)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Asset{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = Lock(ctx, tx); err != nil {
		return Asset{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO idempotency_keys(user_id,operation,key,request_hash) VALUES($1,'asset.create',$2,$3) ON CONFLICT DO NOTHING`, user.ID, key, hash[:])
	if err != nil {
		return Asset{}, err
	}
	var stored, response []byte
	err = tx.QueryRow(ctx, `SELECT request_hash,response_body FROM idempotency_keys WHERE user_id=$1 AND operation='asset.create' AND key=$2 FOR UPDATE`, user.ID, key).Scan(&stored, &response)
	if err != nil {
		return Asset{}, err
	}
	if string(stored) != string(hash[:]) {
		return Asset{}, ErrConflict
	}
	if response != nil {
		var a Asset
		if err = json.Unmarshal(response, &a); err != nil {
			return Asset{}, err
		}
		return a, tx.Commit(ctx)
	}
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO assets(code,name,kind,state,owner_id) VALUES($1,$2,$3,$4,$5) RETURNING id`, in.Code, in.Name, in.Kind, in.State, in.OwnerID).Scan(&id)
	if err != nil {
		return Asset{}, translate(err)
	}
	a, err := scan(tx.QueryRow(ctx, selectAsset+`WHERE a.id=$1`, id))
	if err != nil {
		return Asset{}, err
	}
	if err = bump(ctx, tx); err != nil {
		return Asset{}, err
	}
	if err = audit(ctx, tx, user, "asset.create", id, nil, a); err != nil {
		return Asset{}, err
	}
	response, err = json.Marshal(a)
	if err != nil {
		return Asset{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE idempotency_keys SET response_body=$3 WHERE user_id=$1 AND operation='asset.create' AND key=$2`, user.ID, key, response)
	if err != nil {
		return Asset{}, err
	}
	return a, tx.Commit(ctx)
}
func (s *Service) Update(ctx context.Context, user auth.User, id int64, in Input) (Asset, error) {
	if !CanWrite(user.Role) {
		return Asset{}, ErrForbidden
	}
	in, err := normalize(in)
	if err != nil || in.Version < 1 {
		return Asset{}, ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Asset{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = Lock(ctx, tx); err != nil {
		return Asset{}, err
	}
	old, err := scan(tx.QueryRow(ctx, selectAsset+`WHERE a.id=$1 FOR UPDATE OF a`, id))
	if err != nil {
		return Asset{}, translate(err)
	}
	if old.Version != in.Version {
		return Asset{}, ErrConflict
	}
	_, err = tx.Exec(ctx, `UPDATE assets SET code=$2,name=$3,kind=$4,state=$5,owner_id=$6,version=version+1,updated_at=clock_timestamp() WHERE id=$1`, id, in.Code, in.Name, in.Kind, in.State, in.OwnerID)
	if err != nil {
		return Asset{}, translate(err)
	}
	a, err := scan(tx.QueryRow(ctx, selectAsset+`WHERE a.id=$1`, id))
	if err != nil {
		return Asset{}, err
	}
	if err = bump(ctx, tx); err != nil {
		return Asset{}, err
	}
	if err = audit(ctx, tx, user, "asset.update", id, old, a); err != nil {
		return Asset{}, err
	}
	return a, tx.Commit(ctx)
}
func (s *Service) Dependency(ctx context.Context, user auth.User, provider, dependent int64, add bool) error {
	if !CanWrite(user.Role) {
		return ErrForbidden
	}
	if provider < 1 || dependent < 1 || provider == dependent {
		return ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = Lock(ctx, tx); err != nil {
		return err
	}
	var count int
	err = tx.QueryRow(ctx, `SELECT count(*) FROM assets WHERE id=ANY($1::bigint[])`, []int64{provider, dependent}).Scan(&count)
	if err != nil {
		return err
	}
	if count != 2 {
		return ErrNotFound
	}
	var command pgconn.CommandTag
	if add {
		command, err = tx.Exec(ctx, `INSERT INTO asset_dependencies(provider_id,dependent_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, provider, dependent)
	} else {
		command, err = tx.Exec(ctx, `DELETE FROM asset_dependencies WHERE provider_id=$1 AND dependent_id=$2`, provider, dependent)
	}
	if err != nil {
		return translate(err)
	}
	if command.RowsAffected() > 0 {
		if err = bump(ctx, tx); err != nil {
			return err
		}
		edge := Edge{ProviderID: provider, DependentID: dependent}
		var old, next any
		if add {
			next = edge
		} else {
			old = edge
		}
		if err = audit(ctx, tx, user, "asset.dependency", provider, old, next); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (s *Service) Impact(ctx context.Context, user auth.User, roots []int64) (Impact, error) {
	if !CanRead(user.Role) {
		return Impact{}, ErrForbidden
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Impact{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	result, err := SnapshotTx(ctx, tx, roots)
	if err != nil {
		return Impact{}, err
	}
	return result, tx.Commit(ctx)
}

// SnapshotTx reads a consistent graph from the caller's repeatable-read snapshot
// or a write transaction holding Lock. Retired CIs remain in topology deliberately.
func SnapshotTx(ctx context.Context, tx pgx.Tx, roots []int64) (Impact, error) {
	if len(roots) < 1 || len(roots) > 20 {
		return Impact{}, ErrInvalid
	}
	result := Impact{Items: []Affected{}, Edges: []Edge{}}
	if err := tx.QueryRow(ctx, `SELECT version FROM asset_graph_revision`).Scan(&result.GraphVersion); err != nil {
		return Impact{}, err
	}
	rows, err := tx.Query(ctx, selectAsset+`ORDER BY a.id LIMIT $1`, MaxNodes+1)
	if err != nil {
		return Impact{}, err
	}
	items := map[int64]Asset{}
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			rows.Close()
			return Impact{}, err
		}
		items[a.ID] = a
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Impact{}, err
	}
	if len(items) > MaxNodes {
		return Impact{}, ErrLimit
	}
	for _, id := range roots {
		if id < 1 {
			return Impact{}, ErrInvalid
		}
		if _, ok := items[id]; !ok {
			return Impact{}, ErrNotFound
		}
	}
	rows, err = tx.Query(ctx, `SELECT provider_id,dependent_id FROM asset_dependencies ORDER BY provider_id,dependent_id LIMIT $1`, MaxEdges+1)
	if err != nil {
		return Impact{}, err
	}
	edges := []Edge{}
	for rows.Next() {
		var e Edge
		if err = rows.Scan(&e.ProviderID, &e.DependentID); err != nil {
			rows.Close()
			return Impact{}, err
		}
		edges = append(edges, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Impact{}, err
	}
	if len(edges) > MaxEdges {
		return Impact{}, ErrLimit
	}
	seen := map[int64]bool{}
	for _, r := range Reachable(roots, edges) {
		result.Items = append(result.Items, Affected{Asset: items[r.ID], Distance: r.Distance, Path: r.Path})
		seen[r.ID] = true
	}
	for _, e := range edges {
		if seen[e.ProviderID] && seen[e.DependentID] {
			result.Edges = append(result.Edges, e)
		}
	}
	return result, nil
}

func (s *Service) Owners(ctx context.Context, user auth.User, before int64, limit int) ([]auth.User, error) {
	if !CanWrite(user.Role) {
		return nil, ErrForbidden
	}
	if before < 0 || limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	rows, err := s.pool.Query(ctx, `SELECT id,email,display_name,role_code FROM users WHERE ($1::bigint=0 OR id<$1) ORDER BY id DESC LIMIT $2`, before, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []auth.User{}
	for rows.Next() {
		var u auth.User
		if err = rows.Scan(&u.ID, &u.Email, &u.DisplayName, &u.Role); err != nil {
			return nil, err
		}
		result = append(result, u)
	}
	return result, rows.Err()
}
