package installer

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cli/cli/v2/api"
	"github.com/cli/cli/v2/internal/skills/discovery"
	"github.com/cli/cli/v2/pkg/httpmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallLocalSkill(t *testing.T) {
	tests := []struct {
		name   string
		skill  discovery.Skill
		setup  func(t *testing.T, srcDir string)
		verify func(t *testing.T, destDir string)
	}{
		{
			name:  "copies files",
			skill: discovery.Skill{Name: "code-review", Path: "skills/code-review"},
			setup: func(t *testing.T, srcDir string) {
				t.Helper()
				skillSrc := filepath.Join(srcDir, "skills", "code-review")
				require.NoError(t, os.MkdirAll(skillSrc, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(skillSrc, "SKILL.md"), []byte("# Code Review"), 0o644))
				require.NoError(t, os.WriteFile(filepath.Join(skillSrc, "prompt.txt"), []byte("review this PR"), 0o644))
			},
			verify: func(t *testing.T, destDir string) {
				t.Helper()
				content, err := os.ReadFile(filepath.Join(destDir, "code-review", "prompt.txt"))
				require.NoError(t, err)
				assert.Equal(t, "review this PR", string(content))

				_, err = os.Stat(filepath.Join(destDir, "code-review", "SKILL.md"))
				assert.NoError(t, err)
			},
		},
		{
			name:  "nested directories",
			skill: discovery.Skill{Name: "issue-triage", Path: "skills/issue-triage"},
			setup: func(t *testing.T, srcDir string) {
				t.Helper()
				deep := filepath.Join(srcDir, "skills", "issue-triage", "prompts", "templates")
				require.NoError(t, os.MkdirAll(deep, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(deep, "bug.txt"), []byte("triage bug"), 0o644))
				require.NoError(t, os.WriteFile(
					filepath.Join(srcDir, "skills", "issue-triage", "SKILL.md"), []byte("# Issue Triage"), 0o644))
			},
			verify: func(t *testing.T, destDir string) {
				t.Helper()
				content, err := os.ReadFile(filepath.Join(destDir, "issue-triage", "prompts", "templates", "bug.txt"))
				require.NoError(t, err)
				assert.Equal(t, "triage bug", string(content))
			},
		},
		{
			name:  "skips symlinks",
			skill: discovery.Skill{Name: "pr-summary", Path: "skills/pr-summary"},
			setup: func(t *testing.T, srcDir string) {
				t.Helper()
				skillSrc := filepath.Join(srcDir, "skills", "pr-summary")
				require.NoError(t, os.MkdirAll(skillSrc, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(skillSrc, "SKILL.md"), []byte("# PR Summary"), 0o644))
				require.NoError(t, os.WriteFile(filepath.Join(skillSrc, "prompt.txt"), []byte("summarize"), 0o644))
				os.Symlink(filepath.Join(skillSrc, "prompt.txt"), filepath.Join(skillSrc, "link.txt"))
			},
			verify: func(t *testing.T, destDir string) {
				t.Helper()
				_, err := os.Stat(filepath.Join(destDir, "pr-summary", "prompt.txt"))
				assert.NoError(t, err)
				_, err = os.Stat(filepath.Join(destDir, "pr-summary", "link.txt"))
				assert.True(t, os.IsNotExist(err))
			},
		},
		{
			name:  "injects metadata into SKILL.md",
			skill: discovery.Skill{Name: "copilot-helper", Path: "skills/copilot-helper"},
			setup: func(t *testing.T, srcDir string) {
				t.Helper()
				skillSrc := filepath.Join(srcDir, "skills", "copilot-helper")
				require.NoError(t, os.MkdirAll(skillSrc, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(skillSrc, "SKILL.md"), []byte("# Copilot Helper\nAssists with tasks"), 0o644))
			},
			verify: func(t *testing.T, destDir string) {
				t.Helper()
				content, err := os.ReadFile(filepath.Join(destDir, "copilot-helper", "SKILL.md"))
				require.NoError(t, err)
				assert.True(t, strings.Contains(string(content), "local-path"),
					"expected SKILL.md to contain local-path metadata, got: %s", string(content))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srcDir := t.TempDir()
			destDir := t.TempDir()
			tt.setup(t, srcDir)

			err := installLocalSkill(srcDir, tt.skill, destDir)
			require.NoError(t, err)
			tt.verify(t, destDir)
		})
	}
}

func TestInstallSkill(t *testing.T) {
	tests := []struct {
		name   string
		skill  discovery.Skill
		stubs  func(*httpmock.Registry)
		verify func(t *testing.T, destDir string)
	}{
		{
			name:  "installs files from remote",
			skill: discovery.Skill{Name: "code-review", Path: "skills/code-review", TreeSHA: "tree123"},
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/trees/tree123"),
					httpmock.JSONResponse(map[string]interface{}{
						"sha": "tree123", "truncated": false,
						"tree": []map[string]interface{}{
							{"path": "SKILL.md", "type": "blob", "sha": "skill-sha", "size": 10},
							{"path": "prompt.txt", "type": "blob", "sha": "prompt-sha", "size": 5},
						},
					}))
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/blobs/skill-sha"),
					httpmock.JSONResponse(map[string]interface{}{
						"sha": "skill-sha", "encoding": "base64",
						"content": base64.StdEncoding.EncodeToString([]byte("# Code Review")),
					}))
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/blobs/prompt-sha"),
					httpmock.JSONResponse(map[string]interface{}{
						"sha": "prompt-sha", "encoding": "base64",
						"content": base64.StdEncoding.EncodeToString([]byte("review this PR")),
					}))
			},
			verify: func(t *testing.T, destDir string) {
				t.Helper()
				content, err := os.ReadFile(filepath.Join(destDir, "code-review", "prompt.txt"))
				require.NoError(t, err)
				assert.Equal(t, "review this PR", string(content))

				_, err = os.Stat(filepath.Join(destDir, "code-review", "SKILL.md"))
				assert.NoError(t, err)
			},
		},
		{
			name:  "injects metadata into SKILL.md",
			skill: discovery.Skill{Name: "pr-summary", Path: "skills/pr-summary", TreeSHA: "tree456"},
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/trees/tree456"),
					httpmock.JSONResponse(map[string]interface{}{
						"sha": "tree456", "truncated": false,
						"tree": []map[string]interface{}{
							{"path": "SKILL.md", "type": "blob", "sha": "md-sha", "size": 20},
						},
					}))
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/blobs/md-sha"),
					httpmock.JSONResponse(map[string]interface{}{
						"sha": "md-sha", "encoding": "base64",
						"content": base64.StdEncoding.EncodeToString([]byte("# PR Summary\nSummarize pull requests")),
					}))
			},
			verify: func(t *testing.T, destDir string) {
				t.Helper()
				content, err := os.ReadFile(filepath.Join(destDir, "pr-summary", "SKILL.md"))
				require.NoError(t, err)
				assert.Contains(t, string(content), "github-owner: monalisa")
				assert.Contains(t, string(content), "github-repo: octocat-skills")
			},
		},
		{
			name:  "skips path traversal from malicious tree",
			skill: discovery.Skill{Name: "code-review", Path: "skills/code-review", TreeSHA: "tree123"},
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/trees/tree123"),
					httpmock.JSONResponse(map[string]interface{}{
						"sha": "tree123", "truncated": false,
						"tree": []map[string]interface{}{
							{"path": "SKILL.md", "type": "blob", "sha": "safe-sha", "size": 10},
							{"path": "../../etc/passwd", "type": "blob", "sha": "evil-sha", "size": 100},
						},
					}))
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/blobs/safe-sha"),
					httpmock.JSONResponse(map[string]interface{}{
						"sha": "safe-sha", "encoding": "base64",
						"content": base64.StdEncoding.EncodeToString([]byte("# Safe Skill")),
					}))
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/blobs/evil-sha"),
					httpmock.JSONResponse(map[string]interface{}{
						"sha": "evil-sha", "encoding": "base64",
						"content": base64.StdEncoding.EncodeToString([]byte("malicious content")),
					}))
			},
			verify: func(t *testing.T, destDir string) {
				t.Helper()
				_, err := os.Stat(filepath.Join(destDir, "code-review", "SKILL.md"))
				assert.NoError(t, err)

				_, err = os.Stat(filepath.Join(destDir, "..", "etc", "passwd"))
				assert.True(t, os.IsNotExist(err), "traversal path should not be written")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			destDir := t.TempDir()
			reg := &httpmock.Registry{}
			defer reg.Verify(t)
			tt.stubs(reg)
			client := api.NewClientFromHTTP(&http.Client{Transport: reg})
			opts := &Options{
				Host:   "github.com",
				Owner:  "monalisa",
				Repo:   "octocat-skills",
				Ref:    "v1.0",
				SHA:    "commit123",
				Client: client,
			}

			err := installSkill(opts, tt.skill, destDir)
			require.NoError(t, err)
			tt.verify(t, destDir)
		})
	}
}

func stubTreeAndBlob(reg *httpmock.Registry, treeSHA string) {
	reg.Register(
		httpmock.REST("GET", fmt.Sprintf("repos/monalisa/octocat-skills/git/trees/%s", treeSHA)),
		httpmock.JSONResponse(map[string]interface{}{
			"sha": treeSHA, "truncated": false,
			"tree": []map[string]interface{}{
				{"path": "SKILL.md", "type": "blob", "sha": treeSHA + "-blob", "size": 10},
			},
		}))
	reg.Register(
		httpmock.REST("GET", fmt.Sprintf("repos/monalisa/octocat-skills/git/blobs/%s-blob", treeSHA)),
		httpmock.JSONResponse(map[string]interface{}{
			"sha": treeSHA + "-blob", "encoding": "base64",
			"content": base64.StdEncoding.EncodeToString([]byte("# Skill")),
		}))
}

func TestInstall(t *testing.T) {
	tests := []struct {
		name          string
		skills        []discovery.Skill
		stubs         func(*httpmock.Registry)
		wantInstalled []string
		wantErr       string
	}{
		{
			name: "single skill",
			skills: []discovery.Skill{
				{Name: "code-review", Path: "skills/code-review", TreeSHA: "tree-cr"},
			},
			stubs:         func(reg *httpmock.Registry) { stubTreeAndBlob(reg, "tree-cr") },
			wantInstalled: []string{"code-review"},
		},
		{
			name: "multiple skills concurrently",
			skills: []discovery.Skill{
				{Name: "code-review", Path: "skills/code-review", TreeSHA: "tree-cr"},
				{Name: "issue-triage", Path: "skills/issue-triage", TreeSHA: "tree-it"},
			},
			stubs: func(reg *httpmock.Registry) {
				stubTreeAndBlob(reg, "tree-cr")
				stubTreeAndBlob(reg, "tree-it")
			},
			wantInstalled: []string{"code-review", "issue-triage"},
		},
		{
			name:    "no dir or agent host",
			skills:  []discovery.Skill{{Name: "code-review"}},
			stubs:   func(reg *httpmock.Registry) {},
			wantErr: "either Dir or AgentHost must be specified",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())

			destDir := t.TempDir()
			reg := &httpmock.Registry{}
			defer reg.Verify(t)
			tt.stubs(reg)
			client := api.NewClientFromHTTP(&http.Client{Transport: reg})

			opts := &Options{
				Host:   "github.com",
				Owner:  "monalisa",
				Repo:   "octocat-skills",
				Ref:    "v1.0",
				SHA:    "commit123",
				Client: client,
				Skills: tt.skills,
				Dir:    destDir,
			}
			if tt.wantErr != "" {
				opts.Dir = ""
			}

			result, err := Install(opts)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.wantInstalled, result.Installed)
			assert.Equal(t, destDir, result.Dir)

			homeDir, _ := os.UserHomeDir()
			lockPath := filepath.Join(homeDir, ".agents", ".skill-lock.json")
			lockData, err := os.ReadFile(lockPath)
			require.NoError(t, err, "lockfile should have been written")
			for _, name := range tt.wantInstalled {
				assert.Contains(t, string(lockData), name)
			}
		})
	}
}
