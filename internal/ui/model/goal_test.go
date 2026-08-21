package model

import (
	"testing"

	"github.com/charmbracelet/crush/internal/goal"
	"github.com/stretchr/testify/require"
)

func TestGoalInfoUsesGreenTextOnlyWhileActive(t *testing.T) {
	t.Parallel()

	u := newTestUI()
	u.goal = goal.Goal{SessionID: "s1", Text: "Ship the release", Status: goal.StatusActive}
	active := u.goalInfo(40, false)
	require.Contains(t, active, u.com.Styles.Sidebar.GoalActiveText.Render("Ship the release"))

	u.goal.Status = goal.StatusComplete
	complete := u.goalInfo(40, false)
	require.Contains(t, complete, u.com.Styles.Resource.AdditionalText.Render("Ship the release"))
	require.NotContains(t, complete, u.com.Styles.Sidebar.GoalActiveText.Render("Ship the release"))
}
