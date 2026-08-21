package model

import (
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/status"
	"github.com/stretchr/testify/require"
)

func TestStatusInfoRendersStandup(t *testing.T) {
	t.Parallel()

	u := newTestUI()
	u.statusUpdate = status.Update{
		SessionID: "sess-1",
		Done:      "Added the caching layer",
		Doing:     "Writing migration tests",
		Next:      "Run the full suite",
		Blockers:  "CI is down",
		UpdatedAt: time.Now().Add(-5 * time.Minute).Unix(),
	}

	out := u.statusInfo(60, true)
	require.Contains(t, out, "Status")
	require.Contains(t, out, "Added the caching layer")
	require.Contains(t, out, "Writing migration tests")
	require.Contains(t, out, "Run the full suite")
	require.Contains(t, out, "CI is down")
	require.Contains(t, out, "5m ago")
}

func TestStatusInfoEmptyState(t *testing.T) {
	t.Parallel()

	u := newTestUI()
	out := u.statusInfo(60, true)
	require.Contains(t, out, "Status")
	require.Contains(t, out, "None.")
}

func TestStatusInfoWithoutBlockers(t *testing.T) {
	t.Parallel()

	u := newTestUI()
	u.statusUpdate = status.Update{
		SessionID: "sess-1",
		Done:      "a",
		Doing:     "b",
		Next:      "c",
		UpdatedAt: time.Now().Unix(),
	}
	out := u.statusInfo(60, true)
	require.NotContains(t, out, "⛔")
}

func TestFormatSince(t *testing.T) {
	t.Parallel()
	now := time.Now()

	require.Equal(t, "just now", formatSince(now.Add(-10*time.Second).Unix()))
	require.Equal(t, "3m ago", formatSince(now.Add(-3*time.Minute).Unix()))
	require.Equal(t, "5h ago", formatSince(now.Add(-5*time.Hour).Unix()))
	require.Equal(t, "2d ago", formatSince(now.Add(-2*24*time.Hour).Unix()))
}
