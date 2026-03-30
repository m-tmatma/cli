package hosts

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindByID(t *testing.T) {
	host, err := FindByID("github-copilot")
	require.NoError(t, err)
	assert.Equal(t, "GitHub Copilot", host.Name)
	assert.Equal(t, ".github/skills", host.ProjectDir)
}

func TestFindByID_Invalid(t *testing.T) {
	_, err := FindByID("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown host")
}

func TestValidHostIDs(t *testing.T) {
	ids := ValidHostIDs()
	assert.Contains(t, ids, "github-copilot")
	assert.Contains(t, ids, "claude-code")
	assert.Contains(t, ids, "cursor")
}

func TestHostNames(t *testing.T) {
	names := HostNames()
	assert.Contains(t, names, "GitHub Copilot")
	assert.Contains(t, names, "Claude Code")
}

func TestInstallDir_Project(t *testing.T) {
	host, _ := FindByID("github-copilot")
	dir, err := host.InstallDir(ScopeProject, "/tmp/myrepo", "/home/user")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/tmp/myrepo", ".github", "skills"), dir)
}

func TestInstallDir_User(t *testing.T) {
	host, _ := FindByID("github-copilot")
	dir, err := host.InstallDir(ScopeUser, "/tmp/myrepo", "/home/user")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/home/user", ".copilot", "skills"), dir)
}

func TestInstallDir_NoGitRoot(t *testing.T) {
	host, _ := FindByID("github-copilot")
	_, err := host.InstallDir(ScopeProject, "", "/home/user")
	assert.Error(t, err)
}

func TestRepoNameFromRemote(t *testing.T) {
	tests := []struct {
		remote string
		want   string
	}{
		{"https://github.com/owner/repo.git", "owner/repo"},
		{"https://github.com/owner/repo", "owner/repo"},
		{"git@github.com:owner/repo.git", "owner/repo"},
		{"git@github.com:owner/repo", "owner/repo"},
		{"ssh://git@github.com/owner/repo.git", "owner/repo"},
		{"ssh://git@github.com/owner/repo", "owner/repo"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.remote, func(t *testing.T) {
			assert.Equal(t, tt.want, RepoNameFromRemote(tt.remote))
		})
	}
}

func TestUniqueProjectDirs(t *testing.T) {
	dirs := UniqueProjectDirs()

	// Should contain all known project dirs
	assert.Contains(t, dirs, ".github/skills")
	assert.Contains(t, dirs, ".claude/skills")
	assert.Contains(t, dirs, ".cursor/skills")
	assert.Contains(t, dirs, ".agents/skills")
	assert.Contains(t, dirs, ".agent/skills")

	// Should deduplicate — gemini and antigravity share .agent/skills
	seen := map[string]int{}
	for _, d := range dirs {
		seen[d]++
	}
	for dir, count := range seen {
		assert.Equalf(t, 1, count, "directory %q appears %d times, expected 1", dir, count)
	}
}

func TestScopeLabels(t *testing.T) {
	t.Run("without repo name", func(t *testing.T) {
		labels := ScopeLabels("")
		require.Len(t, labels, 2)
		assert.Contains(t, labels[0], "Project")
		assert.Contains(t, labels[0], "recommended")
		assert.Contains(t, labels[1], "Global")
	})

	t.Run("with repo name", func(t *testing.T) {
		labels := ScopeLabels("owner/repo")
		require.Len(t, labels, 2)
		assert.Contains(t, labels[0], "owner/repo")
		assert.Contains(t, labels[0], "recommended")
		assert.Contains(t, labels[1], "Global")
	})
}
