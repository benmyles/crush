package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/stretchr/testify/require"
)

func TestExplicitSkillNames(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"prompt-tweaks"}, explicitSkillNames("use $prompt-tweaks to update this"))
	require.Equal(t, []string{"one", "two"}, explicitSkillNames("$one and $two and $one"))
	require.Empty(t, explicitSkillNames(`ignore \$prompt-tweaks and $MORPH_API_KEY`))
}

func TestAttachExplicitSkillContents(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	skillPath := filepath.Join(dir, "prompt-tweaks", skills.SkillFileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(skillPath), 0o755))
	require.NoError(t, os.WriteFile(skillPath, []byte(`---
name: prompt-tweaks
description: Tune prompts.
---
# Prompt Tweaks
`), 0o644))
	skill, err := skills.Parse(skillPath)
	require.NoError(t, err)

	c := &coordinator{
		activeSkills: []*skills.Skill{skill},
		skillTracker: skills.NewTracker([]*skills.Skill{skill}),
	}

	attachments := c.attachExplicitSkillContents("use $prompt-tweaks to update this", nil)

	require.Len(t, attachments, 1)
	require.Equal(t, skillPath, attachments[0].FilePath)
	require.Equal(t, "text/markdown", attachments[0].MimeType)
	require.Contains(t, string(attachments[0].Content), `<attached_skill name="prompt-tweaks"`)
	require.Contains(t, string(attachments[0].Content), "# Prompt Tweaks")
	require.True(t, c.skillTracker.IsLoaded("prompt-tweaks"))
}

func TestDiscoverSkillsResolvesRelativeSkillPaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	skillPath := filepath.Join(dir, "skills", "prompt-tweaks", skills.SkillFileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(skillPath), 0o755))
	require.NoError(t, os.WriteFile(skillPath, []byte(`---
name: prompt-tweaks
description: Tune prompts.
---
# Prompt Tweaks
`), 0o644))
	store := config.NewTestStoreWithWorkingDir(&config.Config{
		Options: &config.Options{
			SkillsPaths: []string{"skills"},
		},
	}, dir)

	_, activeSkills := discoverSkills(store)
	skill := findSkillForTest(activeSkills, "prompt-tweaks")

	require.NotNil(t, skill)
	require.Equal(t, skillPath, skill.SkillFilePath)
}

func findSkillForTest(skillList []*skills.Skill, name string) *skills.Skill {
	for _, skill := range skillList {
		if skill.Name == name {
			return skill
		}
	}
	return nil
}
