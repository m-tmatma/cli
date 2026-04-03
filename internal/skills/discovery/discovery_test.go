package discovery

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MakeNowJust/heredoc"
	"github.com/cli/cli/v2/api"
	"github.com/cli/cli/v2/pkg/httpmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallName(t *testing.T) {
	tests := []struct {
		name     string
		skill    Skill
		wantName string
	}{
		{
			name:     "plain skill",
			skill:    Skill{Name: "code-review"},
			wantName: "code-review",
		},
		{
			name:     "namespaced skill",
			skill:    Skill{Name: "issue-triage", Namespace: "monalisa"},
			wantName: "monalisa/issue-triage",
		},
		{
			name:     "plugin skill with namespace",
			skill:    Skill{Name: "pr-summary", Namespace: "hubot", Convention: "plugins"},
			wantName: "hubot/pr-summary",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantName, tt.skill.InstallName())
		})
	}
}

func TestMatchSkillConventions(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		wantNil        bool
		wantName       string
		wantNamespace  string
		wantConvention string
	}{
		{
			name:           "plugin namespace",
			path:           "plugins/hubot/skills/pr-summary/SKILL.md",
			wantName:       "pr-summary",
			wantNamespace:  "hubot",
			wantConvention: "plugins",
		},
		{
			name:           "namespaced skill",
			path:           "skills/monalisa/issue-triage/SKILL.md",
			wantName:       "issue-triage",
			wantNamespace:  "monalisa",
			wantConvention: "skills-namespaced",
		},
		{
			name:           "regular skill",
			path:           "skills/code-review/SKILL.md",
			wantName:       "code-review",
			wantConvention: "skills",
		},
		{
			name:    "non-SKILL.md file",
			path:    "skills/code-review/README.md",
			wantNil: true,
		},
		{
			name:           "plugin skill from different author",
			path:           "plugins/monalisa/skills/code-review/SKILL.md",
			wantName:       "code-review",
			wantNamespace:  "monalisa",
			wantConvention: "plugins",
		},
		{
			name:           "root convention single-skill repo",
			path:           "code-review/SKILL.md",
			wantName:       "code-review",
			wantConvention: "root",
		},
		{
			name:    "root convention excludes skills dir",
			path:    "skills/SKILL.md",
			wantNil: true,
		},
		{
			name:    "root convention excludes dot-prefixed",
			path:    ".hidden/SKILL.md",
			wantNil: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := matchSkillConventions(treeEntry{Path: tt.path, Type: "blob"})
			if tt.wantNil {
				assert.Nil(t, m)
				return
			}
			require.NotNil(t, m)
			assert.Equal(t, tt.wantName, m.name)
			assert.Equal(t, tt.wantNamespace, m.namespace)
			assert.Equal(t, tt.wantConvention, m.convention)
		})
	}
}

func TestValidateName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "empty", input: "", want: false},
		{name: "too long", input: strings.Repeat("a", 65), want: false},
		{name: "max length is valid", input: strings.Repeat("a", 64), want: true},
		{name: "contains slash", input: "foo/bar", want: false},
		{name: "contains dotdot", input: "foo..bar", want: false},
		{name: "starts with dot", input: ".hidden", want: false},
		{name: "simple name", input: "code-review", want: true},
		{name: "with dots and underscores", input: "octocat_helper.v2", want: true},
		{name: "uppercase allowed", input: "Octocat", want: true},
		{name: "single char", input: "a", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, validateName(tt.input))
		})
	}
}

func TestIsSpecCompliant(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "empty", input: "", want: false},
		{name: "consecutive hyphens", input: "code--review", want: false},
		{name: "uppercase rejected", input: "Octocat", want: false},
		{name: "starts with hyphen", input: "-octocat", want: false},
		{name: "ends with hyphen", input: "octocat-", want: false},
		{name: "valid lowercase with hyphens", input: "issue-triage", want: true},
		{name: "valid single char", input: "a", want: true},
		{name: "valid with numbers", input: "copilot4", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsSpecCompliant(tt.input))
		})
	}
}

func TestResolveRef(t *testing.T) {
	tests := []struct {
		name    string
		version string
		stubs   func(*httpmock.Registry)
		wantRef string
		wantSHA string
		wantErr string
	}{
		{
			name:    "explicit version resolves lightweight tag",
			version: "v1.0",
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/ref/tags/v1.0"),
					httpmock.JSONResponse(map[string]interface{}{
						"object": map[string]interface{}{"sha": "abc123", "type": "commit"},
					}))
			},
			wantRef: "v1.0",
			wantSHA: "abc123",
		},
		{
			name:    "explicit version resolves annotated tag",
			version: "v2.0",
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/ref/tags/v2.0"),
					httpmock.JSONResponse(map[string]interface{}{
						"object": map[string]interface{}{"sha": "tag-obj-sha", "type": "tag"},
					}))
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/tags/tag-obj-sha"),
					httpmock.JSONResponse(map[string]interface{}{
						"object": map[string]interface{}{"sha": "real-commit-sha"},
					}))
			},
			wantRef: "v2.0",
			wantSHA: "real-commit-sha",
		},
		{
			name:    "explicit version falls back to commit SHA",
			version: "deadbeef",
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/ref/tags/deadbeef"),
					httpmock.StatusStringResponse(404, "not found"))
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/commits/deadbeef"),
					httpmock.JSONResponse(map[string]interface{}{"sha": "deadbeef"}))
			},
			wantRef: "deadbeef",
			wantSHA: "deadbeef",
		},
		{
			name:    "explicit version not found anywhere",
			version: "nonexistent",
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/ref/tags/nonexistent"),
					httpmock.StatusStringResponse(404, "not found"))
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/commits/nonexistent"),
					httpmock.StatusStringResponse(404, "not found"))
			},
			wantErr: `ref "nonexistent" not found as tag or commit in monalisa/octocat-skills`,
		},
		{
			name: "no version uses latest release",
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/releases/latest"),
					httpmock.JSONResponse(map[string]interface{}{"tag_name": "v3.0"}))
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/ref/tags/v3.0"),
					httpmock.JSONResponse(map[string]interface{}{
						"object": map[string]interface{}{"sha": "release-sha", "type": "commit"},
					}))
			},
			wantRef: "v3.0",
			wantSHA: "release-sha",
		},
		{
			name: "no version falls back to default branch when no releases",
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/releases/latest"),
					httpmock.StatusStringResponse(404, "not found"))
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills"),
					httpmock.JSONResponse(map[string]interface{}{"default_branch": "main"}))
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/ref/heads/main"),
					httpmock.JSONResponse(map[string]interface{}{
						"object": map[string]interface{}{"sha": "branch-sha"},
					}))
			},
			wantRef: "main",
			wantSHA: "branch-sha",
		},
		{
			name:    "annotated tag dereference failure",
			version: "v4.0",
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/ref/tags/v4.0"),
					httpmock.JSONResponse(map[string]interface{}{
						"object": map[string]interface{}{"sha": "tag-obj-sha", "type": "tag"},
					}))
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/tags/tag-obj-sha"),
					httpmock.StatusStringResponse(500, "server error"))
			},
			wantErr: "could not dereference annotated tag",
		},
		{
			name: "empty tag_name in latest release falls back to default branch",
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/releases/latest"),
					httpmock.JSONResponse(map[string]interface{}{"tag_name": ""}))
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills"),
					httpmock.JSONResponse(map[string]interface{}{"default_branch": "main"}))
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/ref/heads/main"),
					httpmock.JSONResponse(map[string]interface{}{
						"object": map[string]interface{}{"sha": "fallback-sha"},
					}))
			},
			wantRef: "main",
			wantSHA: "fallback-sha",
		},
		{
			name: "empty default_branch falls back to main",
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/releases/latest"),
					httpmock.StatusStringResponse(404, "not found"))
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills"),
					httpmock.JSONResponse(map[string]interface{}{"default_branch": ""}))
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/ref/heads/main"),
					httpmock.JSONResponse(map[string]interface{}{
						"object": map[string]interface{}{"sha": "main-sha"},
					}))
			},
			wantRef: "main",
			wantSHA: "main-sha",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := &httpmock.Registry{}
			defer reg.Verify(t)
			tt.stubs(reg)
			client := api.NewClientFromHTTP(&http.Client{Transport: reg})

			ref, err := ResolveRef(client, "github.com", "monalisa", "octocat-skills", tt.version)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantRef, ref.Ref)
			assert.Equal(t, tt.wantSHA, ref.SHA)
		})
	}
}

func TestFetchBlob(t *testing.T) {
	tests := []struct {
		name    string
		stubs   func(*httpmock.Registry)
		wantErr string
		want    string
	}{
		{
			name: "decodes base64 content",
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/blobs/abc"),
					httpmock.JSONResponse(map[string]interface{}{
						"sha": "abc", "encoding": "base64", "content": "SGVsbG8gV29ybGQ=",
					}))
			},
			want: "Hello World",
		},
		{
			name: "rejects non-base64 encoding",
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/blobs/abc"),
					httpmock.JSONResponse(map[string]interface{}{
						"sha": "abc", "encoding": "utf-8", "content": "raw",
					}))
			},
			wantErr: "unexpected blob encoding: utf-8",
		},
		{
			name: "API error",
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/blobs/abc"),
					httpmock.StatusStringResponse(500, "server error"))
			},
			wantErr: "could not fetch blob",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := &httpmock.Registry{}
			defer reg.Verify(t)
			tt.stubs(reg)
			client := api.NewClientFromHTTP(&http.Client{Transport: reg})

			got, err := FetchBlob(client, "github.com", "monalisa", "octocat-skills", "abc")
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDiscoverSkills(t *testing.T) {
	tests := []struct {
		name       string
		stubs      func(*httpmock.Registry)
		wantSkills []string
		wantErr    string
	}{
		{
			name: "discovers skills from tree",
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/trees/abc123"),
					httpmock.JSONResponse(map[string]interface{}{
						"sha": "abc123", "truncated": false,
						"tree": []map[string]interface{}{
							{"path": "skills/code-review", "type": "tree", "sha": "tree-sha-1"},
							{"path": "skills/code-review/SKILL.md", "type": "blob", "sha": "blob-1"},
							{"path": "skills/issue-triage", "type": "tree", "sha": "tree-sha-2"},
							{"path": "skills/issue-triage/SKILL.md", "type": "blob", "sha": "blob-2"},
							{"path": "README.md", "type": "blob", "sha": "readme"},
						},
					}))
			},
			wantSkills: []string{"code-review", "issue-triage"},
		},
		{
			name: "truncated tree returns error",
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/trees/abc123"),
					httpmock.JSONResponse(map[string]interface{}{
						"sha": "abc123", "truncated": true, "tree": []map[string]interface{}{},
					}))
			},
			wantErr: "too large",
		},
		{
			name: "no skills found",
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/trees/abc123"),
					httpmock.JSONResponse(map[string]interface{}{
						"sha": "abc123", "truncated": false,
						"tree": []map[string]interface{}{
							{"path": "README.md", "type": "blob", "sha": "readme"},
						},
					}))
			},
			wantErr: "no skills found",
		},
		{
			name: "API error",
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/trees/abc123"),
					httpmock.StatusStringResponse(500, "server error"))
			},
			wantErr: "could not fetch repository tree",
		},
		{
			name: "deduplicates skills from same directory",
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/trees/abc123"),
					httpmock.JSONResponse(map[string]interface{}{
						"sha": "abc123", "truncated": false,
						"tree": []map[string]interface{}{
							{"path": "skills/code-review", "type": "tree", "sha": "tree-sha"},
							{"path": "skills/code-review/SKILL.md", "type": "blob", "sha": "blob-1"},
							{"path": "skills/code-review/SKILL.md", "type": "blob", "sha": "blob-2"},
						},
					}))
			},
			wantSkills: []string{"code-review"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := &httpmock.Registry{}
			defer reg.Verify(t)
			tt.stubs(reg)
			client := api.NewClientFromHTTP(&http.Client{Transport: reg})

			skills, err := DiscoverSkills(client, "github.com", "monalisa", "octocat-skills", "abc123")
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			var names []string
			for _, s := range skills {
				names = append(names, s.Name)
			}
			assert.Equal(t, tt.wantSkills, names)
		})
	}
}

func TestDiscoverSkillByPath(t *testing.T) {
	tests := []struct {
		name      string
		skillPath string
		stubs     func(*httpmock.Registry)
		wantName  string
		wantNS    string
		wantErr   string
	}{
		{
			name:      "discovers skill by path",
			skillPath: "skills/code-review",
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/contents/skills"),
					httpmock.JSONResponse([]map[string]interface{}{
						{"name": "code-review", "path": "skills/code-review", "sha": "tree-sha", "type": "dir"},
					}))
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/trees/tree-sha"),
					httpmock.JSONResponse(map[string]interface{}{
						"sha": "tree-sha", "truncated": false,
						"tree": []map[string]interface{}{
							{"path": "SKILL.md", "type": "blob", "sha": "blob-sha"},
						},
					}))
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/blobs/blob-sha"),
					httpmock.JSONResponse(map[string]interface{}{
						"sha": "blob-sha", "encoding": "base64", "content": "IyBTa2lsbA==",
					}))
			},
			wantName: "code-review",
		},
		{
			name:      "namespaced path sets namespace",
			skillPath: "skills/monalisa/issue-triage",
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/contents/skills/monalisa"),
					httpmock.JSONResponse([]map[string]interface{}{
						{"name": "issue-triage", "path": "skills/monalisa/issue-triage", "sha": "tree-sha", "type": "dir"},
					}))
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/trees/tree-sha"),
					httpmock.JSONResponse(map[string]interface{}{
						"sha": "tree-sha", "truncated": false,
						"tree": []map[string]interface{}{
							{"path": "SKILL.md", "type": "blob", "sha": "blob-sha"},
						},
					}))
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/blobs/blob-sha"),
					httpmock.JSONResponse(map[string]interface{}{
						"sha": "blob-sha", "encoding": "base64", "content": "IyBTa2lsbA==",
					}))
			},
			wantName: "issue-triage",
			wantNS:   "monalisa",
		},
		{
			name:      "strips trailing SKILL.md from path",
			skillPath: "skills/code-review/SKILL.md",
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/contents/skills"),
					httpmock.JSONResponse([]map[string]interface{}{
						{"name": "code-review", "path": "skills/code-review", "sha": "tree-sha", "type": "dir"},
					}))
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/trees/tree-sha"),
					httpmock.JSONResponse(map[string]interface{}{
						"sha": "tree-sha", "truncated": false,
						"tree": []map[string]interface{}{
							{"path": "SKILL.md", "type": "blob", "sha": "blob-sha"},
						},
					}))
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/blobs/blob-sha"),
					httpmock.JSONResponse(map[string]interface{}{
						"sha": "blob-sha", "encoding": "base64", "content": "IyBTa2lsbA==",
					}))
			},
			wantName: "code-review",
		},
		{
			name:      "invalid skill name",
			skillPath: "skills/.hidden-skill",
			wantErr:   "invalid skill name",
		},
		{
			name:      "skill directory not found",
			skillPath: "skills/nonexistent",
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/contents/skills"),
					httpmock.JSONResponse([]map[string]interface{}{
						{"name": "other-skill", "path": "skills/other-skill", "sha": "tree-sha", "type": "dir"},
					}))
			},
			wantErr: "skill directory",
		},
		{
			name:      "no SKILL.md in directory",
			skillPath: "skills/code-review",
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/contents/skills"),
					httpmock.JSONResponse([]map[string]interface{}{
						{"name": "code-review", "path": "skills/code-review", "sha": "tree-sha", "type": "dir"},
					}))
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/trees/tree-sha"),
					httpmock.JSONResponse(map[string]interface{}{
						"sha": "tree-sha", "truncated": false,
						"tree": []map[string]interface{}{
							{"path": "README.md", "type": "blob", "sha": "readme"},
						},
					}))
			},
			wantErr: "no SKILL.md found",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := &httpmock.Registry{}
			defer reg.Verify(t)
			if tt.stubs != nil {
				tt.stubs(reg)
			}
			client := api.NewClientFromHTTP(&http.Client{Transport: reg})

			skill, err := DiscoverSkillByPath(client, "github.com", "monalisa", "octocat-skills", "abc123", tt.skillPath)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantName, skill.Name)
			assert.Equal(t, tt.wantNS, skill.Namespace)
		})
	}
}

func TestDiscoverLocalSkills(t *testing.T) {
	tests := []struct {
		name       string
		createDir  bool
		setup      func(t *testing.T, dir string)
		wantSkills []string
		wantErr    string
	}{
		{
			name:      "discovers skills in skills/ directory",
			createDir: true,
			setup: func(t *testing.T, dir string) {
				t.Helper()
				for _, name := range []string{"code-review", "issue-triage"} {
					skillDir := filepath.Join(dir, "skills", name)
					require.NoError(t, os.MkdirAll(skillDir, 0o755))
					require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# "+name), 0o644))
				}
			},
			wantSkills: []string{"code-review", "issue-triage"},
		},
		{
			name:      "single skill at root",
			createDir: true,
			setup: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(heredoc.Doc(`
				---
				name: root-skill
				---
				# Root
			`)), 0o644))
			},
			wantSkills: []string{"root-skill"},
		},
		{
			name:      "no skills found",
			createDir: true,
			setup: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Not a skill"), 0o644))
			},
			wantErr: "no skills found",
		},
		{
			name:    "nonexistent directory",
			setup:   func(t *testing.T, dir string) {},
			wantErr: "could not access",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "repo")
			if tt.createDir {
				require.NoError(t, os.MkdirAll(dir, 0o755))
			}
			tt.setup(t, dir)

			skills, err := DiscoverLocalSkills(dir)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			var names []string
			for _, s := range skills {
				names = append(names, s.Name)
			}
			assert.ElementsMatch(t, tt.wantSkills, names)
		})
	}
}

func TestMatchesSkillPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		wantName string
	}{
		{name: "skills convention", path: "skills/code-review/SKILL.md", wantName: "code-review"},
		{name: "namespaced convention", path: "skills/monalisa/issue-triage/SKILL.md", wantName: "issue-triage"},
		{name: "plugins convention", path: "plugins/hubot/skills/pr-summary/SKILL.md", wantName: "pr-summary"},
		{name: "non-skill file", path: "README.md", wantName: ""},
		{name: "non-SKILL.md in skill dir", path: "skills/code-review/prompt.txt", wantName: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantName, MatchesSkillPath(tt.path))
		})
	}
}

func TestDiscoverSkillFiles(t *testing.T) {
	tests := []struct {
		name      string
		stubs     func(*httpmock.Registry)
		wantPaths []string
		wantErr   string
	}{
		{
			name: "returns files with skill path prefix",
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/trees/tree123"),
					httpmock.JSONResponse(map[string]interface{}{
						"sha": "tree123", "truncated": false,
						"tree": []map[string]interface{}{
							{"path": "SKILL.md", "type": "blob", "sha": "sha1", "size": 10},
							{"path": "scripts/setup.sh", "type": "blob", "sha": "sha2", "size": 50},
							{"path": "scripts", "type": "tree", "sha": "treesub"},
						},
					}))
			},
			wantPaths: []string{"skills/code-review/SKILL.md", "skills/code-review/scripts/setup.sh"},
		},
		{
			name: "truncated tree falls back to walkTree",
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/trees/tree123"),
					httpmock.JSONResponse(map[string]interface{}{
						"sha": "tree123", "truncated": true, "tree": []map[string]interface{}{},
					}))
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/trees/tree123"),
					httpmock.JSONResponse(map[string]interface{}{
						"sha": "tree123",
						"tree": []map[string]interface{}{
							{"path": "SKILL.md", "type": "blob", "sha": "sha1", "size": 10},
						},
					}))
			},
			wantPaths: []string{"skills/code-review/SKILL.md"},
		},
		{
			name: "API error",
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/trees/tree123"),
					httpmock.StatusStringResponse(500, "server error"))
			},
			wantErr: "could not fetch skill tree",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := &httpmock.Registry{}
			defer reg.Verify(t)
			tt.stubs(reg)
			client := api.NewClientFromHTTP(&http.Client{Transport: reg})

			files, err := DiscoverSkillFiles(client, "github.com", "monalisa", "octocat-skills", "tree123", "skills/code-review")
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			var paths []string
			for _, f := range files {
				paths = append(paths, f.Path)
			}
			assert.Equal(t, tt.wantPaths, paths)
		})
	}
}

func TestListSkillFiles(t *testing.T) {
	tests := []struct {
		name      string
		stubs     func(*httpmock.Registry)
		wantPaths []string
		wantErr   string
	}{
		{
			name: "returns relative paths",
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/trees/tree123"),
					httpmock.JSONResponse(map[string]interface{}{
						"sha": "tree123", "truncated": false,
						"tree": []map[string]interface{}{
							{"path": "SKILL.md", "type": "blob", "sha": "sha1", "size": 10},
							{"path": "prompt.txt", "type": "blob", "sha": "sha2", "size": 20},
						},
					}))
			},
			wantPaths: []string{"SKILL.md", "prompt.txt"},
		},
		{
			name: "truncated tree falls back to walkTree with nested subtree",
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/trees/tree123"),
					httpmock.JSONResponse(map[string]interface{}{
						"sha": "tree123", "truncated": true, "tree": []map[string]interface{}{},
					}))
				// walkTree fetches the top-level tree non-recursively
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/trees/tree123"),
					httpmock.JSONResponse(map[string]interface{}{
						"sha": "tree123",
						"tree": []map[string]interface{}{
							{"path": "SKILL.md", "type": "blob", "sha": "sha1", "size": 10},
							{"path": "scripts", "type": "tree", "sha": "subtree1"},
						},
					}))
				// walkTree recurses into the "scripts" subtree
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/trees/subtree1"),
					httpmock.JSONResponse(map[string]interface{}{
						"sha": "subtree1",
						"tree": []map[string]interface{}{
							{"path": "setup.sh", "type": "blob", "sha": "sha2", "size": 50},
						},
					}))
			},
			wantPaths: []string{"SKILL.md", "scripts/setup.sh"},
		},
		{
			name: "API error",
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/trees/tree123"),
					httpmock.StatusStringResponse(500, "server error"))
			},
			wantErr: "could not fetch skill tree",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := &httpmock.Registry{}
			defer reg.Verify(t)
			tt.stubs(reg)
			client := api.NewClientFromHTTP(&http.Client{Transport: reg})

			files, err := ListSkillFiles(client, "github.com", "monalisa", "octocat-skills", "tree123")
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			var paths []string
			for _, f := range files {
				paths = append(paths, f.Path)
			}
			assert.Equal(t, tt.wantPaths, paths)
		})
	}
}

func TestFetchDescriptionsConcurrent(t *testing.T) {
	tests := []struct {
		name      string
		skills    []Skill
		stubs     func(*httpmock.Registry)
		wantDescs []string
	}{
		{
			name: "fetches descriptions for skills without one",
			skills: []Skill{
				{Name: "code-review", BlobSHA: "blob1"},
				{Name: "issue-triage", Description: "already set"},
			},
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/octocat-skills/git/blobs/blob1"),
					httpmock.JSONResponse(map[string]interface{}{
						"sha": "blob1", "encoding": "base64",
						"content": "LS0tCm5hbWU6IGNvZGUtcmV2aWV3CmRlc2NyaXB0aW9uOiBSZXZpZXdzIFBScwotLS0KIyBUZXN0",
					}))
			},
			wantDescs: []string{"Reviews PRs", "already set"},
		},
		{
			name: "no-op when all descriptions set",
			skills: []Skill{
				{Name: "code-review", Description: "set"},
			},
			stubs:     func(reg *httpmock.Registry) {},
			wantDescs: []string{"set"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := &httpmock.Registry{}
			defer reg.Verify(t)
			tt.stubs(reg)
			client := api.NewClientFromHTTP(&http.Client{Transport: reg})

			FetchDescriptionsConcurrent(client, "github.com", "monalisa", "octocat-skills", tt.skills, nil)
			var descs []string
			for _, s := range tt.skills {
				descs = append(descs, s.Description)
			}
			assert.Equal(t, tt.wantDescs, descs)
		})
	}
}
