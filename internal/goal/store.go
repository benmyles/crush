package goal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrNoGoal is returned by operations that require an existing goal when
// the session has none (or it was cleared).
var ErrNoGoal = errors.New("no goal is set for this session")

// Store persists goal state in the session database. A nil Store is a
// valid, inert value: every operation returns ErrNoGoal so callers
// (agents without a DB, tests) degrade gracefully.
type Store struct {
	db *sql.DB
}

// NewStore wraps db in a goal store.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Get returns the goal for sessionID, or a StatusNone goal when the
// session has no goal.
func (s *Store) Get(ctx context.Context, sessionID string) (Goal, error) {
	g, err := s.get(ctx, sessionID)
	if err != nil {
		return Goal{}, err
	}
	if g == nil {
		return Goal{}, nil
	}
	return *g, nil
}

// get returns the stored goal or nil when the session has none.
func (s *Store) get(ctx context.Context, sessionID string) (*Goal, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT session_id, text, status, created_at, updated_at,
		        COALESCE(complete_reason, ''), COALESCE(blocked_reason, ''),
		        consecutive_prods, total_prods
		   FROM goals WHERE session_id = ?`,
		sessionID)
	var g Goal
	if err := row.Scan(&g.SessionID, &g.Text, &g.Status, &g.CreatedAt,
		&g.UpdatedAt, &g.CompleteReason, &g.BlockedReason,
		&g.ConsecutiveProds, &g.TotalProds); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to fetch goal: %w", err)
	}
	return &g, nil
}

// Set creates or reactivates the goal for sessionID. Re-setting an
// existing goal keeps its prod counters but clears terminal reasons and
// status; a brand-new goal starts from zero.
func (s *Store) Set(ctx context.Context, sessionID, text string) (Goal, error) {
	if s == nil || s.db == nil {
		return Goal{}, ErrNoGoal
	}
	if text == "" {
		return Goal{}, errors.New("goal text must not be empty")
	}
	now := time.Now().Unix()
	existing, err := s.get(ctx, sessionID)
	if err != nil {
		return Goal{}, err
	}
	if existing == nil {
		_, err = s.db.ExecContext(ctx,
			`INSERT INTO goals (session_id, text, status, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?)`,
			sessionID, text, StatusActive, now, now)
	} else {
		_, err = s.db.ExecContext(ctx,
			`UPDATE goals SET text = ?, status = ?, updated_at = ?,
			        complete_reason = NULL, blocked_reason = NULL
			  WHERE session_id = ?`,
			text, StatusActive, now, sessionID)
	}
	if err != nil {
		return Goal{}, fmt.Errorf("failed to set goal: %w", err)
	}
	return s.Get(ctx, sessionID)
}

// Update replaces the goal text. Active goals keep their status and
// counters, so the agent can narrow or rephrase an in-flight goal.
// Terminal goals (complete, blocked, stalled) are reactivated: the status
// resets to active, terminal reasons clear, and the consecutive prod
// budget restarts so the new text is supervised fresh. It backs the
// agent's update_goal tool.
func (s *Store) Update(ctx context.Context, sessionID, text string) (Goal, error) {
	if s == nil || s.db == nil {
		return Goal{}, ErrNoGoal
	}
	if text == "" {
		return Goal{}, errors.New("goal text must not be empty")
	}
	existing, err := s.get(ctx, sessionID)
	if err != nil {
		return Goal{}, err
	}
	if existing == nil {
		return Goal{}, ErrNoGoal
	}
	if existing.Status == StatusActive {
		if _, err = s.db.ExecContext(ctx,
			`UPDATE goals SET text = ?, updated_at = ? WHERE session_id = ?`,
			text, time.Now().Unix(), sessionID); err != nil {
			return Goal{}, fmt.Errorf("failed to update goal: %w", err)
		}
		return s.Get(ctx, sessionID)
	}
	if _, err = s.db.ExecContext(ctx,
		`UPDATE goals SET text = ?, status = ?, updated_at = ?,
		        complete_reason = NULL, blocked_reason = NULL,
		        consecutive_prods = 0
		  WHERE session_id = ?`,
		text, StatusActive, time.Now().Unix(), sessionID); err != nil {
		return Goal{}, fmt.Errorf("failed to update goal: %w", err)
	}
	return s.Get(ctx, sessionID)
}

// Complete marks the goal complete with the agent's summary. It is a
// no-op error for sessions without a goal.
func (s *Store) Complete(ctx context.Context, sessionID, reason string) (Goal, error) {
	return s.setTerminal(ctx, sessionID, StatusComplete, reason)
}

// Block marks the goal blocked with the agent's reason. It is a no-op
// error for sessions without a goal.
func (s *Store) Block(ctx context.Context, sessionID, reason string) (Goal, error) {
	return s.setTerminal(ctx, sessionID, StatusBlocked, reason)
}

// Stall marks the goal stalled after the consecutive prod cap fired. It
// is a no-op error for sessions without a goal.
func (s *Store) Stall(ctx context.Context, sessionID string) (Goal, error) {
	return s.setTerminal(ctx, sessionID, StatusStalled, "")
}

func (s *Store) setTerminal(ctx context.Context, sessionID string, status Status, reason string) (Goal, error) {
	if s == nil || s.db == nil {
		return Goal{}, ErrNoGoal
	}
	existing, err := s.get(ctx, sessionID)
	if err != nil {
		return Goal{}, err
	}
	if existing == nil {
		return Goal{}, ErrNoGoal
	}
	col := ""
	switch status {
	case StatusComplete:
		col = "complete_reason"
	case StatusBlocked:
		col = "blocked_reason"
	}
	var res sql.Result
	if col == "" {
		res, err = s.db.ExecContext(ctx,
			`UPDATE goals SET status = ?, updated_at = ? WHERE session_id = ?`,
			status, time.Now().Unix(), sessionID)
	} else {
		res, err = s.db.ExecContext(ctx,
			`UPDATE goals SET status = ?, updated_at = ?, `+col+` = ? WHERE session_id = ?`,
			status, time.Now().Unix(), reason, sessionID)
	}
	if err != nil {
		return Goal{}, fmt.Errorf("failed to update goal status: %w", err)
	}
	if _, err = res.RowsAffected(); err != nil {
		return Goal{}, fmt.Errorf("failed to update goal status: %w", err)
	}
	return s.Get(ctx, sessionID)
}

// Resume reactivates a blocked or stalled goal and resets the prod
// counters, arming the check loop again. It errors for sessions without
// a goal; active goals are returned unchanged.
func (s *Store) Resume(ctx context.Context, sessionID string) (Goal, error) {
	if s == nil || s.db == nil {
		return Goal{}, ErrNoGoal
	}
	existing, err := s.get(ctx, sessionID)
	if err != nil {
		return Goal{}, err
	}
	if existing == nil {
		return Goal{}, ErrNoGoal
	}
	if existing.Status == StatusActive || existing.Status == StatusComplete {
		return *existing, nil
	}
	if _, err = s.db.ExecContext(ctx,
		`UPDATE goals SET status = ?, updated_at = ?,
		        blocked_reason = NULL, consecutive_prods = 0
		  WHERE session_id = ?`,
		StatusActive, time.Now().Unix(), sessionID); err != nil {
		return Goal{}, fmt.Errorf("failed to resume goal: %w", err)
	}
	return s.Get(ctx, sessionID)
}

// BumpProd records that a goal check was scheduled. It returns the
// updated goal; the counters let the scheduler enforce
// MaxConsecutiveProds.
func (s *Store) BumpProd(ctx context.Context, sessionID string) (Goal, error) {
	if s == nil || s.db == nil {
		return Goal{}, ErrNoGoal
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE goals SET consecutive_prods = consecutive_prods + 1,
		        total_prods = total_prods + 1, updated_at = ?
		  WHERE session_id = ?`,
		time.Now().Unix(), sessionID); err != nil {
		return Goal{}, fmt.Errorf("failed to bump goal prods: %w", err)
	}
	return s.Get(ctx, sessionID)
}

// ResetProd clears the consecutive prod counter. Fresh user prompts call
// it so a check issued after user input starts a new budget.
func (s *Store) ResetProd(ctx context.Context, sessionID string) error {
	if s == nil || s.db == nil {
		return nil
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE goals SET consecutive_prods = 0 WHERE session_id = ?`,
		sessionID); err != nil {
		return fmt.Errorf("failed to reset goal prods: %w", err)
	}
	return nil
}

// Clear deletes the goal for sessionID.
func (s *Store) Clear(ctx context.Context, sessionID string) error {
	if s == nil || s.db == nil {
		return nil
	}
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM goals WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("failed to clear goal: %w", err)
	}
	return nil
}
