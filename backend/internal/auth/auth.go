package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var ErrUnauthenticated = errors.New("authentication required")

type User struct {
	ID          int64  `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
}

type Session struct {
	User      User
	Token     string
	ExpiresAt time.Time
}

type Service struct {
	pool      *pgxpool.Pool
	ttl       time.Duration
	dummyHash []byte
}

func New(pool *pgxpool.Pool, ttl time.Duration) (*Service, error) {
	if ttl < time.Minute || ttl > 7*24*time.Hour {
		return nil, fmt.Errorf("session TTL must be between 1 minute and 7 days")
	}
	dummy, err := bcrypt.GenerateFromPassword([]byte("not-a-real-user-password"), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	return &Service{pool: pool, ttl: ttl, dummyHash: dummy}, nil
}

func tokenDigest(token string) ([]byte, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		return nil, false
	}
	sum := sha256.Sum256(raw)
	return sum[:], true
}

func (s *Service) Login(ctx context.Context, email, password, oldToken string) (Session, error) {
	var user User
	var hash string
	err := s.pool.QueryRow(ctx, `SELECT id, email, display_name, role_code, password_hash
        FROM users WHERE email=$1`, strings.ToLower(strings.TrimSpace(email))).Scan(
		&user.ID, &user.Email, &user.DisplayName, &user.Role, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
		return Session{}, ErrUnauthenticated
	}
	if err != nil {
		return Session{}, err
	}
	if err = bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return Session{}, ErrUnauthenticated
	}
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return Session{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	digest, _ := tokenDigest(token)
	expires := time.Now().UTC().Add(s.ttl)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Session{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	// Rotate the browser's old session on successful login.
	if previous, ok := tokenDigest(oldToken); ok {
		if _, err = tx.Exec(ctx, `DELETE FROM sessions WHERE token_hash=$1`, previous); err != nil {
			return Session{}, err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,$3)`, digest, user.ID, expires); err != nil {
		return Session{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(actor_id,action,entity_type,entity_id,new_value)
        VALUES($1,'auth.login','user',$1,'{"authenticated":true}'::jsonb)`, user.ID); err != nil {
		return Session{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Session{}, err
	}
	return Session{User: user, Token: token, ExpiresAt: expires}, nil
}

func (s *Service) Current(ctx context.Context, token string) (User, error) {
	digest, ok := tokenDigest(token)
	if !ok {
		return User{}, ErrUnauthenticated
	}
	var user User
	err := s.pool.QueryRow(ctx, `SELECT u.id,u.email,u.display_name,u.role_code
        FROM sessions s JOIN users u ON u.id=s.user_id
        WHERE s.token_hash=$1 AND s.expires_at > now()`, digest).Scan(
		&user.ID, &user.Email, &user.DisplayName, &user.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUnauthenticated
	}
	return user, err
}

func (s *Service) Logout(ctx context.Context, token string) error {
	digest, ok := tokenDigest(token)
	if !ok {
		return ErrUnauthenticated
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var userID int64
	err = tx.QueryRow(ctx, `DELETE FROM sessions WHERE token_hash=$1 RETURNING user_id`, digest).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUnauthenticated
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(actor_id,action,entity_type,entity_id,old_value,new_value)
        VALUES($1,'auth.logout','user',$1,'{"authenticated":true}'::jsonb,'{"authenticated":false}'::jsonb)`, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
