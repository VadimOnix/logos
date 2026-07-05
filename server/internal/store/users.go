package store

import (
	"context"
	"time"
)

type User struct {
	ID           int64
	Email        string
	PasswordHash string
	CreatedAt    time.Time
	// TOTPSecret is the base32 TOTP secret when 2FA is enabled, else nil.
	TOTPSecret *string
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `select count(*) from users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(ctx context.Context, email, passwordHash string) (*User, error) {
	u := &User{Email: email, PasswordHash: passwordHash}
	err := s.pool.QueryRow(ctx,
		`insert into users (email, password_hash) values ($1, $2) returning id, created_at`,
		email, passwordHash).Scan(&u.ID, &u.CreatedAt)
	return u, err
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	u := &User{}
	err := s.pool.QueryRow(ctx,
		`select id, email, password_hash, created_at, totp_secret from users where email = $1`, email).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt, &u.TOTPSecret)
	if noRows(err) {
		return nil, ErrNotFound
	}
	return u, err
}

func (s *Store) GetUser(ctx context.Context, id int64) (*User, error) {
	u := &User{}
	err := s.pool.QueryRow(ctx,
		`select id, email, password_hash, created_at, totp_secret from users where id = $1`, id).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt, &u.TOTPSecret)
	if noRows(err) {
		return nil, ErrNotFound
	}
	return u, err
}

// Sessions

func (s *Store) CreateSession(ctx context.Context, tokenHash []byte, userID int64, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx,
		`insert into sessions (token_hash, user_id, expires_at) values ($1, $2, $3)`,
		tokenHash, userID, expiresAt)
	return err
}

// GetSessionUser resolves a session token hash to its user, ignoring expired sessions.
func (s *Store) GetSessionUser(ctx context.Context, tokenHash []byte) (*User, error) {
	u := &User{}
	err := s.pool.QueryRow(ctx,
		`select u.id, u.email, u.password_hash, u.created_at, u.totp_secret
		   from sessions s join users u on u.id = s.user_id
		  where s.token_hash = $1 and s.expires_at > now()`, tokenHash).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt, &u.TOTPSecret)
	if noRows(err) {
		return nil, ErrNotFound
	}
	return u, err
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := s.pool.Exec(ctx, `delete from sessions where token_hash = $1`, tokenHash)
	return err
}

func (s *Store) DeleteExpiredSessions(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `delete from sessions where expires_at <= now()`)
	return err
}

// API tokens

type APIToken struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

func (s *Store) CreateAPIToken(ctx context.Context, userID int64, name string, tokenHash []byte) (*APIToken, error) {
	t := &APIToken{Name: name}
	err := s.pool.QueryRow(ctx,
		`insert into api_tokens (user_id, name, token_hash) values ($1, $2, $3) returning id, created_at`,
		userID, name, tokenHash).Scan(&t.ID, &t.CreatedAt)
	return t, err
}

func (s *Store) ListAPITokens(ctx context.Context, userID int64) ([]APIToken, error) {
	rows, err := s.pool.Query(ctx,
		`select id, name, created_at, last_used_at from api_tokens where user_id = $1 order by id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIToken{}
	for rows.Next() {
		var t APIToken
		if err := rows.Scan(&t.ID, &t.Name, &t.CreatedAt, &t.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetAPITokenUser resolves an API token hash to its user and stamps last_used_at.
func (s *Store) GetAPITokenUser(ctx context.Context, tokenHash []byte) (*User, error) {
	u := &User{}
	err := s.pool.QueryRow(ctx,
		`update api_tokens t set last_used_at = now()
		   from users u
		  where t.token_hash = $1 and u.id = t.user_id
		 returning u.id, u.email, u.password_hash, u.created_at, u.totp_secret`, tokenHash).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt, &u.TOTPSecret)
	if noRows(err) {
		return nil, ErrNotFound
	}
	return u, err
}

func (s *Store) DeleteAPIToken(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `delete from api_tokens where id = $1 and user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetUserTOTPSecret enables (non-nil) or disables (nil) TOTP for a user.
func (s *Store) SetUserTOTPSecret(ctx context.Context, userID int64, secret *string) error {
	_, err := s.pool.Exec(ctx, `update users set totp_secret = $2 where id = $1`, userID, secret)
	return err
}
