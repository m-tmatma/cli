package discovery

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInstallName(t *testing.T) {
	tests := []struct {
		name     string
		skill    Skill
		wantName string
	}{
		{
			name:     "plain skill",
			skill:    Skill{Name: "git-commit"},
			wantName: "git-commit",
		},
		{
			name:     "namespaced skill",
			skill:    Skill{Name: "xlsx-pro", Namespace: "alice"},
			wantName: "alice/xlsx-pro",
		},
		{
			name:     "plugin skill with namespace",
			skill:    Skill{Name: "code-review", Namespace: "bob", Convention: "plugins"},
			wantName: "bob/code-review",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantName, tt.skill.InstallName())
		})
	}
}

func TestMatchSkillConventions_PluginNamespace(t *testing.T) {
	entry := treeEntry{
		Path: "plugins/bob/skills/code-review/SKILL.md",
		Type: "blob",
	}
	m := matchSkillConventions(entry)
	assert.NotNil(t, m)
	assert.Equal(t, "code-review", m.name)
	assert.Equal(t, "bob", m.namespace)
	assert.Equal(t, "plugins", m.convention)
}

func TestMatchSkillConventions_NamespacedSkill(t *testing.T) {
	entry := treeEntry{
		Path: "skills/alice/xlsx-pro/SKILL.md",
		Type: "blob",
	}
	m := matchSkillConventions(entry)
	assert.NotNil(t, m)
	assert.Equal(t, "xlsx-pro", m.name)
	assert.Equal(t, "alice", m.namespace)
	assert.Equal(t, "skills-namespaced", m.convention)
}

func TestMatchSkillConventions_RegularSkill(t *testing.T) {
	entry := treeEntry{
		Path: "skills/git-commit/SKILL.md",
		Type: "blob",
	}
	m := matchSkillConventions(entry)
	assert.NotNil(t, m)
	assert.Equal(t, "git-commit", m.name)
	assert.Equal(t, "", m.namespace)
	assert.Equal(t, "skills", m.convention)
}

func TestDuplicatePluginSkills_DifferentAuthors(t *testing.T) {
	// Simulates a repo with the same skill name under two different plugin authors.
	// Previously this caused a collision error; now each gets a distinct namespace.
	entries := []treeEntry{
		{Path: "plugins/author1/skills/azure-diag/SKILL.md", Type: "blob"},
		{Path: "plugins/author2/skills/azure-diag/SKILL.md", Type: "blob"},
	}

	seen := make(map[string]bool)
	var matches []skillMatch
	for _, e := range entries {
		m := matchSkillConventions(e)
		if m == nil || seen[m.skillDir] {
			continue
		}
		seen[m.skillDir] = true
		matches = append(matches, *m)
	}

	assert.Len(t, matches, 2)
	assert.Equal(t, "author1", matches[0].namespace)
	assert.Equal(t, "author2", matches[1].namespace)

	// Build skills and verify they have different InstallNames
	var skills []Skill
	for _, m := range matches {
		skills = append(skills, Skill{
			Name:       m.name,
			Namespace:  m.namespace,
			Convention: m.convention,
		})
	}
	assert.Equal(t, "author1/azure-diag", skills[0].InstallName())
	assert.Equal(t, "author2/azure-diag", skills[1].InstallName())
	assert.NotEqual(t, skills[0].InstallName(), skills[1].InstallName())
}
