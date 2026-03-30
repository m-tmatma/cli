package registry

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindByID(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		wantName string
		wantErr  string
	}{
		{name: "github-copilot", id: "github-copilot", wantName: "GitHub Copilot"},
		{name: "claude-code", id: "claude-code", wantName: "Claude Code"},
		{name: "cursor", id: "cursor", wantName: "Cursor"},
		{name: "codex", id: "codex", wantName: "Codex"},
		{name: "gemini", id: "gemini", wantName: "Gemini CLI"},
		{name: "antigravity", id: "antigravity", wantName: "Antigravity"},
		{name: "unknown agent", id: "nonexistent", wantErr: "unknown agent"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, err := FindByID(tt.id)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantName, host.Name)
		})
	}
}

func TestInstallDir(t *testing.T) {
	host, err := FindByID("github-copilot")
	require.NoError(t, err)

	tests := []struct {
		name    string
		scope   Scope
		gitRoot string
		homeDir string
		wantDir string
		wantErr bool
	}{
		{
			name:    "project scope",
			scope:   ScopeProject,
			gitRoot: "/tmp/monalisa-repo",
			homeDir: "/home/monalisa",
			wantDir: filepath.Join("/tmp/monalisa-repo", ".github", "skills"),
		},
		{
			name:    "user scope",
			scope:   ScopeUser,
			gitRoot: "/tmp/monalisa-repo",
			homeDir: "/home/monalisa",
			wantDir: filepath.Join("/home/monalisa", ".copilot", "skills"),
		},
		{
			name:    "project scope without git root",
			scope:   ScopeProject,
			gitRoot: "",
			homeDir: "/home/monalisa",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, err := host.InstallDir(tt.scope, tt.gitRoot, tt.homeDir)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantDir, dir)
		})
	}
}

func TestRepoNameFromRemote(t *testing.T) {
	tests := []struct {
		remote string
		want   string
	}{
		{"https://github.com/monalisa/octocat-skills.git", "monalisa/octocat-skills"},
		{"https://github.com/monalisa/octocat-skills", "monalisa/octocat-skills"},
		{"git@github.com:monalisa/octocat-skills.git", "monalisa/octocat-skills"},
		{"git@github.com:monalisa/octocat-skills", "monalisa/octocat-skills"},
		{"ssh://git@github.com/monalisa/octocat-skills.git", "monalisa/octocat-skills"},
		{"ssh://git@github.com/monalisa/octocat-skills", "monalisa/octocat-skills"},
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
	require.NotEmpty(t, dirs)

	// Should deduplicate — e.g. gemini and antigravity share .agent/skills
	seen := map[string]int{}
	for _, d := range dirs {
		seen[d]++
	}
	for dir, count := range seen {
		assert.Equalf(t, 1, count, "directory %q appears %d times, expected 1", dir, count)
	}
}

func TestScopeLabels(t *testing.T) {
	tests := []struct {
		name       string
		repoName   string
		wantFirst  []string
		wantSecond []string
	}{
		{
			name:       "without repo name",
			repoName:   "",
			wantFirst:  []string{"Project", "recommended"},
			wantSecond: []string{"Global"},
		},
		{
			name:       "with repo name",
			repoName:   "monalisa/octocat-skills",
			wantFirst:  []string{"monalisa/octocat-skills", "recommended"},
			wantSecond: []string{"Global"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			labels := ScopeLabels(tt.repoName)
			require.Len(t, labels, 2)
			for _, s := range tt.wantFirst {
				assert.Contains(t, labels[0], s)
			}
			for _, s := range tt.wantSecond {
				assert.Contains(t, labels[1], s)
			}
		})
	}
}
