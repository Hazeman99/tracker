// Package store is the data access layer. Every query that touches user data
// is scoped by the authenticated user's id in the SQL itself; child rows
// (tasks, log entries, tag links) are reached only through a project that is
// checked to belong to that user in the same statement.
package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"tracker/api/internal/auth"
)

// Errors returned by the store.
var (
	// ErrNotFound means the row does not exist or belongs to another user.
	ErrNotFound = errors.New("not found")
	// ErrConflict means a uniqueness rule would be violated.
	ErrConflict = errors.New("conflict")
	// ErrInvalidOrder means a reorder request did not list every project once.
	ErrInvalidOrder = errors.New("invalid order")
)

// Session lifetimes.
const (
	SessionIdleTTL     = 30 * 24 * time.Hour  // sliding
	SessionAbsoluteTTL = 180 * 24 * time.Hour // absolute
	// touchInterval limits how often last_used_at is written.
	touchInterval = time.Minute
)

// Store wraps the database handle.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// New returns a Store. now is the clock (time.Now in production).
func New(db *sql.DB, now func() time.Time) *Store {
	return &Store{db: db, now: now}
}

func ms(t time.Time) int64 { return t.UnixMilli() }

func fromMs(v int64) time.Time { return time.UnixMilli(v).UTC() }

func fromNullMs(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := fromMs(v.Int64)
	return &t
}

// tx runs fn in a transaction, rolling back on error.
func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func affected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------- users

// User is an account.
type User struct {
	ID           string
	Email        string
	PasswordHash string
	CreatedAt    time.Time
}

// CreateUser inserts a user. It returns ErrConflict if the email is taken.
func (s *Store) CreateUser(ctx context.Context, email, hash string) (User, error) {
	u := User{ID: auth.NewID(), Email: email, PasswordHash: hash, CreatedAt: s.now().UTC()}
	return u, s.tx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE email = ?`, email).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return ErrConflict
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO users (id, email, password_hash, created_at) VALUES (?, ?, ?, ?)`,
			u.ID, u.Email, u.PasswordHash, ms(u.CreatedAt))
		return err
	})
}

func scanUser(row *sql.Row) (User, error) {
	var u User
	var created int64
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	u.CreatedAt = fromMs(created)
	return u, err
}

// UserByEmail looks a user up by email (case-insensitive for ASCII).
func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	return scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, created_at FROM users WHERE email = ?`, email))
}

// UserByID looks a user up by id.
func (s *Store) UserByID(ctx context.Context, id string) (User, error) {
	return scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, created_at FROM users WHERE id = ?`, id))
}

// SetPassword replaces the user's password hash and revokes every session of
// that user except keepSessionID (pass "" to revoke all of them).
func (s *Store) SetPassword(ctx context.Context, userID, hash, keepSessionID string) error {
	now := ms(s.now())
	return s.tx(ctx, func(tx *sql.Tx) error {
		if err := affected(tx.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, hash, userID)); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`UPDATE sessions SET revoked_at = ? WHERE user_id = ? AND id <> ? AND revoked_at IS NULL`,
			now, userID, keepSessionID)
		return err
	})
}

// ---------------------------------------------------------------- sessions

// Session is a signed-in device. The token itself is never stored.
type Session struct {
	ID         string    `json:"id"`
	UserID     string    `json:"-"`
	DeviceName string    `json:"device_name"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// CreateSession stores a new session for userID identified by tokenHash.
func (s *Store) CreateSession(ctx context.Context, userID string, tokenHash []byte, device string) (Session, error) {
	now := s.now().UTC()
	sess := Session{
		ID: auth.NewID(), UserID: userID, DeviceName: device,
		CreatedAt: now, LastUsedAt: now, ExpiresAt: now.Add(SessionAbsoluteTTL),
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions
		(id, user_id, token_hash, device_name, created_at, last_used_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.UserID, tokenHash, sess.DeviceName, ms(now), ms(now), ms(sess.ExpiresAt))
	return sess, err
}

// activeSession is the SQL condition for a usable session at time ?1.
const activeSession = `revoked_at IS NULL AND expires_at > ?1 AND last_used_at > ?1 - ` +
	`86400000 * 30`

// Authenticate returns the active session for tokenHash and slides its idle
// expiry. Expired, revoked and unknown tokens all yield ErrNotFound.
func (s *Store) Authenticate(ctx context.Context, tokenHash []byte) (Session, error) {
	now := s.now().UTC()
	var sess Session
	var created, last, exp int64
	err := s.db.QueryRowContext(ctx, `SELECT id, user_id, device_name, created_at, last_used_at, expires_at
		FROM sessions WHERE token_hash = ?2 AND `+activeSession, ms(now), tokenHash).
		Scan(&sess.ID, &sess.UserID, &sess.DeviceName, &created, &last, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}
	sess.CreatedAt, sess.LastUsedAt, sess.ExpiresAt = fromMs(created), fromMs(last), fromMs(exp)
	if now.Sub(sess.LastUsedAt) >= touchInterval {
		if _, err := s.db.ExecContext(ctx, `UPDATE sessions SET last_used_at = ? WHERE id = ?`, ms(now), sess.ID); err != nil {
			return Session{}, err
		}
		sess.LastUsedAt = now
	}
	return sess, nil
}

// ListSessions returns the user's active sessions, most recently used first.
func (s *Store) ListSessions(ctx context.Context, userID string) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, device_name, created_at, last_used_at, expires_at
		FROM sessions WHERE user_id = ?2 AND `+activeSession+` ORDER BY last_used_at DESC, created_at DESC`,
		ms(s.now()), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var sess Session
		var created, last, exp int64
		if err := rows.Scan(&sess.ID, &sess.UserID, &sess.DeviceName, &created, &last, &exp); err != nil {
			return nil, err
		}
		sess.CreatedAt, sess.LastUsedAt, sess.ExpiresAt = fromMs(created), fromMs(last), fromMs(exp)
		out = append(out, sess)
	}
	return out, rows.Err()
}

// RevokeSession revokes one of the user's active sessions.
func (s *Store) RevokeSession(ctx context.Context, userID, sessionID string) error {
	now := ms(s.now())
	return affected(s.db.ExecContext(ctx, `UPDATE sessions SET revoked_at = ?1
		WHERE id = ?2 AND user_id = ?3 AND `+activeSession, now, sessionID, userID))
}

// RevokeAllSessions revokes every active session of every user and returns
// how many were revoked. It backs the "sessions revoke-all" CLI command.
func (s *Store) RevokeAllSessions(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE sessions SET revoked_at = ? WHERE revoked_at IS NULL`, ms(s.now()))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ---------------------------------------------------------------- audit

// Auth event kinds.
const (
	EventLoginSuccess    = "login_success"
	EventLoginFailure    = "login_failure"
	EventLockoutIP       = "lockout_ip"
	EventLockoutAccount  = "lockout_account"
	EventLoginBlocked    = "login_blocked"
	EventLogout          = "logout"
	EventSessionRevoked  = "session_revoked"
	EventPasswordChanged = "password_changed"
	EventPasswordFailure = "password_change_failure"
	EventPasswordReset   = "password_reset_cli"
	EventRevokeAllCLI    = "sessions_revoked_all_cli"
	EventUserCreated     = "user_created_cli"
)

// RecordAuthEvent appends to the audit trail. userID may be empty.
func (s *Store) RecordAuthEvent(ctx context.Context, userID, kind, ip string) error {
	var uid any
	if userID != "" {
		uid = userID
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO auth_events (id, user_id, kind, ip, created_at) VALUES (?, ?, ?, ?, ?)`,
		auth.NewID(), uid, kind, ip, ms(s.now()))
	return err
}

// AuthEvent is one audit row (used by tests).
type AuthEvent struct {
	UserID string
	Kind   string
	IP     string
}

// AuthEvents returns every audit row in insertion order.
func (s *Store) AuthEvents(ctx context.Context) ([]AuthEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT COALESCE(user_id, ''), kind, ip FROM auth_events ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuthEvent
	for rows.Next() {
		var e AuthEvent
		if err := rows.Scan(&e.UserID, &e.Kind, &e.IP); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
