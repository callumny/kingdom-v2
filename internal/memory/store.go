// Package memory persists bounded conversation exchanges in local SQLite.
package memory

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

const (
	MaxUserBytes    = 32 << 10
	MaxReplyBytes   = 64 << 10
	MaxRecallBytes  = 24 << 10
	maxQueryLimit   = 100
	schemaVersion   = 1
	timestampLayout = "2006-01-02T15:04:05.000000000Z07:00"
)

type Exchange struct {
	ID        int64
	SessionID string
	User      string
	Reply     string
	CreatedAt time.Time
}

type Session struct {
	ID            string
	StartedAt     time.Time
	UpdatedAt     time.Time
	ExchangeCount int
}

type Store struct {
	db  *sql.DB
	now func() time.Time
}

func Open(path string) (*Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve memory path: %w", err)
	}
	directory := filepath.Dir(abs)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, fmt.Errorf("create memory directory: %w", err)
	}

	db, err := sql.Open("sqlite", abs)
	if err != nil {
		return nil, fmt.Errorf("open memory database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &Store{db: db, now: time.Now}
	closeOnError := func(err error) (*Store, error) {
		_ = db.Close()
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return closeOnError(fmt.Errorf("connect memory database: %w", err))
	}
	if err := os.Chmod(abs, 0600); err != nil {
		return closeOnError(fmt.Errorf("secure memory database: %w", err))
	}
	for _, pragma := range []string{
		`PRAGMA foreign_keys = ON`,
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA journal_mode = DELETE`,
		`PRAGMA synchronous = NORMAL`,
	} {
		if _, err := db.Exec(pragma); err != nil {
			return closeOnError(fmt.Errorf("configure memory database: %w", err))
		}
	}
	if err := store.migrate(); err != nil {
		return closeOnError(err)
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func NewSessionID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	return "session-" + hex.EncodeToString(bytes), nil
}

func (s *Store) SaveExchange(ctx context.Context, sessionID, user, reply string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sessionID = strings.TrimSpace(sessionID)
	user = strings.TrimSpace(user)
	reply = strings.TrimSpace(reply)
	if sessionID == "" || user == "" || reply == "" {
		return errors.New("session, user, and reply are required")
	}
	if !utf8.ValidString(sessionID) || !utf8.ValidString(user) || !utf8.ValidString(reply) {
		return errors.New("memory text must be valid UTF-8")
	}
	user, _ = truncateUTF8(user, MaxUserBytes)
	reply, _ = truncateUTF8(reply, MaxReplyBytes)
	now := s.now().UTC().Format(timestampLayout)

	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin memory save: %w", err)
	}
	defer transaction.Rollback()
	if _, err = transaction.ExecContext(ctx, `
		INSERT INTO sessions(id, started_at, updated_at) VALUES(?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET updated_at = excluded.updated_at`, sessionID, now, now); err != nil {
		return fmt.Errorf("save memory session: %w", err)
	}
	if _, err = transaction.ExecContext(ctx, `
		INSERT INTO exchanges(session_id, user_text, reply_text, created_at)
		VALUES(?, ?, ?, ?)`, sessionID, user, reply, now); err != nil {
		return fmt.Errorf("save memory exchange: %w", err)
	}
	if err = transaction.Commit(); err != nil {
		return fmt.Errorf("commit memory exchange: %w", err)
	}
	return nil
}

func (s *Store) RecentExchanges(ctx context.Context, limit int) ([]Exchange, error) {
	limit = normalizedLimit(limit)
	if limit == 0 {
		return nil, nil
	}
	return s.queryExchanges(ctx, `
		SELECT id, session_id, user_text, reply_text, created_at FROM (
			SELECT id, session_id, user_text, reply_text, created_at
			FROM exchanges ORDER BY id DESC LIMIT ?
		) ORDER BY id ASC`, limit)
}

func (s *Store) SessionExchanges(ctx context.Context, sessionID string, limit int) ([]Exchange, error) {
	limit = normalizedLimit(limit)
	if limit == 0 {
		return nil, nil
	}
	return s.queryExchanges(ctx, `
		SELECT id, session_id, user_text, reply_text, created_at FROM (
			SELECT id, session_id, user_text, reply_text, created_at
			FROM exchanges WHERE session_id = ? ORDER BY id DESC LIMIT ?
		) ORDER BY id ASC`, sessionID, limit)
}

func (s *Store) ListSessions(ctx context.Context, limit int) ([]Session, error) {
	limit = normalizedLimit(limit)
	if limit == 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id, s.started_at, s.updated_at, COUNT(e.id)
		FROM sessions s LEFT JOIN exchanges e ON e.session_id = s.id
		GROUP BY s.id, s.started_at, s.updated_at
		ORDER BY s.updated_at DESC, s.id ASC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list memory sessions: %w", err)
	}
	defer rows.Close()
	var sessions []Session
	for rows.Next() {
		var session Session
		var startedAt, updatedAt string
		if err := rows.Scan(&session.ID, &startedAt, &updatedAt, &session.ExchangeCount); err != nil {
			return nil, fmt.Errorf("scan memory session: %w", err)
		}
		session.StartedAt, err = time.Parse(timestampLayout, startedAt)
		if err != nil {
			return nil, fmt.Errorf("parse session start: %w", err)
		}
		session.UpdatedAt, err = time.Parse(timestampLayout, updatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse session update: %w", err)
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read memory sessions: %w", err)
	}
	return sessions, nil
}

func (s *Store) DeleteSession(ctx context.Context, sessionID string) (bool, error) {
	if strings.TrimSpace(sessionID) == "" {
		return false, errors.New("session id required")
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, sessionID)
	if err != nil {
		return false, fmt.Errorf("delete memory session: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("inspect deleted memory session: %w", err)
	}
	return count == 1, nil
}

func RenderRecall(exchanges []Exchange) (string, bool) {
	var builder strings.Builder
	for _, exchange := range exchanges {
		block := fmt.Sprintf("Memory from session %s:\nUser: %s\nKing: %s", exchange.SessionID, exchange.User, exchange.Reply)
		separator := ""
		if builder.Len() > 0 {
			separator = "\n\n"
		}
		value := separator + block
		remaining := MaxRecallBytes - builder.Len()
		if len(value) > remaining {
			value, _ = truncateUTF8(value, remaining)
			builder.WriteString(value)
			return builder.String(), true
		}
		builder.WriteString(value)
	}
	return builder.String(), false
}

func (s *Store) queryExchanges(ctx context.Context, query string, args ...any) ([]Exchange, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query memory exchanges: %w", err)
	}
	defer rows.Close()
	var exchanges []Exchange
	for rows.Next() {
		var exchange Exchange
		var createdAt string
		if err := rows.Scan(&exchange.ID, &exchange.SessionID, &exchange.User, &exchange.Reply, &createdAt); err != nil {
			return nil, fmt.Errorf("scan memory exchange: %w", err)
		}
		exchange.CreatedAt, err = time.Parse(timestampLayout, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse exchange time: %w", err)
		}
		exchanges = append(exchanges, exchange)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read memory exchanges: %w", err)
	}
	return exchanges, nil
}

func (s *Store) migrate() error {
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read memory schema version: %w", err)
	}
	if version > schemaVersion {
		return fmt.Errorf("unsupported memory schema version %d", version)
	}
	if version == schemaVersion {
		return nil
	}
	transaction, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin memory migration: %w", err)
	}
	defer transaction.Rollback()
	statements := []string{
		`CREATE TABLE sessions (
			id TEXT PRIMARY KEY,
			started_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE exchanges (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
			user_text TEXT NOT NULL,
			reply_text TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX exchanges_session_id_id ON exchanges(session_id, id)`,
		fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion),
	}
	for _, statement := range statements {
		if _, err := transaction.Exec(statement); err != nil {
			return fmt.Errorf("migrate memory schema: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit memory migration: %w", err)
	}
	return nil
}

func normalizedLimit(limit int) int {
	if limit <= 0 {
		return 0
	}
	if limit > maxQueryLimit {
		return maxQueryLimit
	}
	return limit
}

func truncateUTF8(value string, limit int) (string, bool) {
	if len(value) <= limit {
		return value, false
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value, true
}
