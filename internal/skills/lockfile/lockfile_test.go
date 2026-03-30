package lockfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupHome redirects HOME to a temp dir and returns the expected lockfile path.
func setupHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return filepath.Join(home, agentsDir, lockFile)
}

func TestRecordInstall(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T) // optional pre-existing state
		skill     string
		owner     string
		repo      string
		skillPath string
		treeSHA   string
		pinnedRef string
		verify    func(t *testing.T, lockPath string)
	}{
		{
			name:      "fresh install creates lockfile",
			skill:     "code-review",
			owner:     "monalisa",
			repo:      "octocat-skills",
			skillPath: "skills/code-review/SKILL.md",
			treeSHA:   "abc123",
			verify: func(t *testing.T, lockPath string) {
				t.Helper()
				f := readLockfile(t, lockPath)
				require.Contains(t, f.Skills, "code-review")
				e := f.Skills["code-review"]
				assert.Equal(t, "monalisa/octocat-skills", e.Source)
				assert.Equal(t, "github", e.SourceType)
				assert.Equal(t, "https://github.com/monalisa/octocat-skills.git", e.SourceURL)
				assert.Equal(t, "skills/code-review/SKILL.md", e.SkillPath)
				assert.Equal(t, "abc123", e.SkillFolderHash)
				assert.NotEmpty(t, e.InstalledAt)
				assert.NotEmpty(t, e.UpdatedAt)
				assert.Empty(t, e.PinnedRef)
			},
		},
		{
			name:      "install with pinned ref",
			skill:     "pr-summary",
			owner:     "hubot",
			repo:      "skills-repo",
			skillPath: "skills/pr-summary/SKILL.md",
			treeSHA:   "def456",
			pinnedRef: "v1.0.0",
			verify: func(t *testing.T, lockPath string) {
				t.Helper()
				f := readLockfile(t, lockPath)
				assert.Equal(t, "v1.0.0", f.Skills["pr-summary"].PinnedRef)
			},
		},
		{
			name: "update preserves InstalledAt and updates treeSHA",
			setup: func(t *testing.T) {
				t.Helper()
				require.NoError(t, RecordInstall("code-review", "monalisa", "octocat-skills", "skills/code-review/SKILL.md", "old-sha", ""))
			},
			skill:     "code-review",
			owner:     "monalisa",
			repo:      "octocat-skills",
			skillPath: "skills/code-review/SKILL.md",
			treeSHA:   "new-sha",
			verify: func(t *testing.T, lockPath string) {
				t.Helper()
				f := readLockfile(t, lockPath)
				e := f.Skills["code-review"]
				assert.Equal(t, "new-sha", e.SkillFolderHash, "treeSHA should be updated")
				// InstalledAt should be preserved (not empty proves it wasn't clobbered)
				assert.NotEmpty(t, e.InstalledAt, "InstalledAt should be preserved from first install")
			},
		},
		{
			name: "multiple skills coexist",
			setup: func(t *testing.T) {
				t.Helper()
				require.NoError(t, RecordInstall("code-review", "monalisa", "octocat-skills", "skills/code-review/SKILL.md", "sha1", ""))
			},
			skill:     "issue-triage",
			owner:     "monalisa",
			repo:      "octocat-skills",
			skillPath: "skills/issue-triage/SKILL.md",
			treeSHA:   "sha2",
			verify: func(t *testing.T, lockPath string) {
				t.Helper()
				f := readLockfile(t, lockPath)
				assert.Contains(t, f.Skills, "code-review")
				assert.Contains(t, f.Skills, "issue-triage")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lockPath := setupHome(t)
			if tt.setup != nil {
				tt.setup(t)
			}

			err := RecordInstall(tt.skill, tt.owner, tt.repo, tt.skillPath, tt.treeSHA, tt.pinnedRef)
			require.NoError(t, err)
			tt.verify(t, lockPath)
		})
	}
}

func TestRead(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T, lockPath string)
		wantSkill bool
	}{
		{
			name:  "missing file returns fresh state",
			setup: func(t *testing.T, lockPath string) {},
		},
		{
			name: "corrupt JSON returns fresh state",
			setup: func(t *testing.T, lockPath string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Dir(lockPath), 0o755))
				require.NoError(t, os.WriteFile(lockPath, []byte("{invalid json"), 0o644))
			},
		},
		{
			name: "wrong version returns fresh state",
			setup: func(t *testing.T, lockPath string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Dir(lockPath), 0o755))
				data, _ := json.Marshal(file{Version: 999, Skills: map[string]entry{"x": {}}})
				require.NoError(t, os.WriteFile(lockPath, data, 0o644))
			},
		},
		{
			name: "valid lockfile",
			setup: func(t *testing.T, lockPath string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Dir(lockPath), 0o755))
				f := &file{
					Version: lockVersion,
					Skills: map[string]entry{
						"code-review": {Source: "monalisa/octocat-skills", SourceType: "github"},
					},
				}
				data, err := json.MarshalIndent(f, "", "  ")
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(lockPath, data, 0o644))
			},
			wantSkill: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lockPath := setupHome(t)
			tt.setup(t, lockPath)

			loaded, err := read()
			require.NoError(t, err)
			assert.Equal(t, lockVersion, loaded.Version)

			if tt.wantSkill {
				assert.Contains(t, loaded.Skills, "code-review")
			} else {
				assert.Empty(t, loaded.Skills)
			}
		})
	}
}

// readLockfile is a test helper that reads and parses the lockfile from disk.
func readLockfile(t *testing.T, path string) *file {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err, "lockfile should exist at %s", path)
	var f file
	require.NoError(t, json.Unmarshal(data, &f))
	return &f
}
