package frontmatter

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantName string
		wantDesc string
		wantBody string
		wantErr  bool
	}{
		{
			name:     "valid frontmatter",
			content:  "---\nname: test-skill\ndescription: A test skill\n---\n# Body\n",
			wantName: "test-skill",
			wantDesc: "A test skill",
			wantBody: "# Body\n",
		},
		{
			name:     "no frontmatter",
			content:  "# Just a markdown file\n",
			wantBody: "# Just a markdown file\n",
		},
		{
			name:    "invalid YAML",
			content: "---\n: invalid yaml [[\n---\n",
			wantErr: true,
		},
		{
			name:     "no closing delimiter",
			content:  "---\nname: test\n",
			wantBody: "---\nname: test\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Parse(tt.content)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantName, result.Metadata.Name)
			assert.Equal(t, tt.wantDesc, result.Metadata.Description)
			assert.Equal(t, tt.wantBody, result.Body)
		})
	}
}

func TestInjectGitHubMetadata(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		owner          string
		repo           string
		ref            string
		sha            string
		treeSHA        string
		pinnedRef      string
		skillPath      string
		wantContains   []string
		wantNotContain []string
	}{
		{
			name:      "injects metadata without pin",
			content:   "---\nname: my-skill\ndescription: desc\n---\n# Body\n",
			owner:     "owner",
			repo:      "repo",
			ref:       "v1.0.0",
			sha:       "abc123",
			treeSHA:   "tree456",
			pinnedRef: "",
			skillPath: "skills/my-skill",
			wantContains: []string{
				"github-owner: owner",
				"github-repo: repo",
				"github-ref: v1.0.0",
				"github-sha: abc123",
				"github-tree-sha: tree456",
				"github-path: skills/my-skill",
				"# Body",
			},
			wantNotContain: []string{
				"github-pinned",
			},
		},
		{
			name:      "injects pinned ref",
			content:   "---\nname: my-skill\n---\n# Body\n",
			owner:     "owner",
			repo:      "repo",
			ref:       "v1.0.0",
			sha:       "abc",
			treeSHA:   "tree",
			pinnedRef: "v1.0.0",
			skillPath: "skills/my-skill",
			wantContains: []string{
				"github-pinned: v1.0.0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := InjectGitHubMetadata(tt.content, tt.owner, tt.repo, tt.ref, tt.sha, tt.treeSHA, tt.pinnedRef, tt.skillPath)
			require.NoError(t, err)
			for _, s := range tt.wantContains {
				assert.Contains(t, got, s)
			}
			for _, s := range tt.wantNotContain {
				assert.NotContains(t, got, s)
			}
		})
	}
}

func TestInjectLocalMetadata(t *testing.T) {
	content := "---\nname: my-skill\nmetadata:\n    github-owner: old\n    github-repo: old\n---\n# Body\n"
	got, err := InjectLocalMetadata(content, "/home/user/skills/my-skill")
	require.NoError(t, err)

	assert.Contains(t, got, "local-path: /home/user/skills/my-skill")
	assert.NotContains(t, got, "github-owner")
	assert.NotContains(t, got, "github-repo")
}

func TestSerialize(t *testing.T) {
	tests := []struct {
		name         string
		frontmatter  map[string]interface{}
		body         string
		wantPrefix   string
		wantSuffix   string
		wantContains []string
	}{
		{
			name:        "with body",
			frontmatter: map[string]interface{}{"name": "test"},
			body:        "# Body content",
			wantPrefix:  "---\n",
			wantContains: []string{
				"name: test",
				"# Body content",
			},
		},
		{
			name:        "empty body",
			frontmatter: map[string]interface{}{"name": "test"},
			body:        "",
			wantSuffix:  "---\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Serialize(tt.frontmatter, tt.body)
			require.NoError(t, err)
			if tt.wantPrefix != "" {
				assert.True(t, strings.HasPrefix(got, tt.wantPrefix))
			}
			if tt.wantSuffix != "" {
				assert.True(t, strings.HasSuffix(got, tt.wantSuffix))
			}
			for _, s := range tt.wantContains {
				assert.Contains(t, got, s)
			}
		})
	}
}
