package status

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Store persists the latest status update per session in the session
// database. A nil Store is a valid, inert value: reads return an empty
// update and writes are dropped, so agents without a DB degrade
// gracefully.
type Store struct {
	db *sql.DB
}

// NewStore wraps db in a status store.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Get returns the latest status update for sessionID, or an update with
// an empty SessionID when the session has none.
func (s *Store) Get(ctx context.Context, sessionID string) (Update, error) {
	u, err := s.get(ctx, sessionID)
	if err != nil {
		return Update{}, err
	}
	if u == nil {
		return Update{}, nil
	}
	return *u, nil
}

// get returns the stored update or nil when the session has none.
func (s *Store) get(ctx context.Context, sessionID string) (*Update, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT session_id, done, doing, next, COALESCE(blockers, ''), updated_at
		   FROM status_updates WHERE session_id = ?`,
		sessionID)
	var u Update
	if err := row.Scan(&u.SessionID, &u.Done, &u.Doing, &u.Next,
		&u.Blockers, &u.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to fetch status update: %w", err)
	}
	return &u, nil
}

// Upsert records a new status update, replacing any previous one for the
// session.
func (s *Store) Upsert(ctx context.Context, sessionID, done, doing, next, blockers string) (Update, error) {
	if s == nil || s.db == nil {
		return Update{}, nil
	}
	if done == "" || doing == "" || next == "" {
		return Update{}, errors.New("done, doing, and next must not be empty")
	}
	now := time.Now().Unix()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO status_updates (session_id, done, doing, next, blockers, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(session_id) DO UPDATE SET
		   done = excluded.done, doing = excluded.doing, next = excluded.next,
		   blockers = excluded.blockers, updated_at = excluded.updated_at`,
		sessionID, done, doing, next, blockers, now); err != nil {
		return Update{}, fmt.Errorf("failed to upsert status update: %w", err)
	}
	return s.Get(ctx, sessionID)
}
