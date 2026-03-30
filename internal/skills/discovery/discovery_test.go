package discovery

import (
	"net/http"
	"testing"

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

func TestDuplicatePluginSkills_DifferentAuthors(t *testing.T) {
	entries := []treeEntry{
		{Path: "plugins/monalisa/skills/code-review/SKILL.md", Type: "blob"},
		{Path: "plugins/hubot/skills/code-review/SKILL.md", Type: "blob"},
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

	require.Len(t, matches, 2)
	assert.Equal(t, "monalisa", matches[0].namespace)
	assert.Equal(t, "hubot", matches[1].namespace)
	assert.NotEqual(t,
		Skill{Name: matches[0].name, Namespace: matches[0].namespace}.InstallName(),
		Skill{Name: matches[1].name, Namespace: matches[1].namespace}.InstallName(),
	)
}

func TestValidateName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "empty", input: "", want: false},
		{name: "too long", input: string(make([]byte, 65)), want: false},
		{name: "max length", input: "a" + string(make([]byte, 63)), want: false}, // 64 'a's would be valid but []byte gives null bytes
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
