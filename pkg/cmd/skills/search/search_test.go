package search

import (
	"net/http"
	"testing"

	"github.com/cli/cli/v2/internal/config"
	"github.com/cli/cli/v2/internal/gh"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/httpmock"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCmdSearch(t *testing.T) {
	tests := []struct {
		name     string
		args     string
		wantOpts searchOptions
		wantErr  string
	}{
		{
			name:     "query argument",
			args:     "terraform",
			wantOpts: searchOptions{Query: "terraform", Page: 1, Limit: defaultLimit},
		},
		{
			name:     "with page flag",
			args:     "terraform --page 3",
			wantOpts: searchOptions{Query: "terraform", Page: 3, Limit: defaultLimit},
		},
		{
			name:     "with limit flag",
			args:     "terraform --limit 5",
			wantOpts: searchOptions{Query: "terraform", Page: 1, Limit: 5},
		},
		{
			name:     "with limit short flag",
			args:     "terraform -L 10",
			wantOpts: searchOptions{Query: "terraform", Page: 1, Limit: 10},
		},
		{
			name:     "with owner flag",
			args:     "terraform --owner hashicorp",
			wantOpts: searchOptions{Query: "terraform", Owner: "hashicorp", Page: 1, Limit: defaultLimit},
		},
		{
			name:    "no arguments",
			args:    "",
			wantErr: "cannot search: query argument required",
		},
		{
			name:    "invalid page",
			args:    "terraform --page 0",
			wantErr: "invalid page number: 0",
		},
		{
			name:    "query too short",
			args:    "a",
			wantErr: "search query must be at least 2 characters",
		},
		{
			name:    "query too short single char",
			args:    "x",
			wantErr: "search query must be at least 2 characters",
		},
		{
			name:    "invalid limit zero",
			args:    "terraform --limit 0",
			wantErr: "invalid limit: 0",
		},
		{
			name:    "invalid limit negative",
			args:    "terraform --limit -1",
			wantErr: "invalid limit: -1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &cmdutil.Factory{}
			var gotOpts *searchOptions
			cmd := NewCmdSearch(f, func(opts *searchOptions) error {
				gotOpts = opts
				return nil
			})

			argv := []string{}
			if tt.args != "" {
				for _, part := range splitOnSpaces(tt.args) {
					if part != "" {
						argv = append(argv, part)
					}
				}
			}
			cmd.SetArgs(argv)
			cmd.SetOut(&discardWriter{})
			cmd.SetErr(&discardWriter{})

			_, err := cmd.ExecuteC()
			if tt.wantErr != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantOpts.Query, gotOpts.Query)
			assert.Equal(t, tt.wantOpts.Owner, gotOpts.Owner)
			assert.Equal(t, tt.wantOpts.Page, gotOpts.Page)
			assert.Equal(t, tt.wantOpts.Limit, gotOpts.Limit)
		})
	}
}

func TestSearchRun(t *testing.T) {
	const emptyCodeResponse = `{"total_count": 0, "incomplete_results": false, "items": []}`

	// stubKeywordSearch registers the HTTP stubs needed for a keyword search.
	// searchByKeyword fires up to 3 concurrent search/code requests (path,
	// owner, primary). Stubs are one-shot in httpmock, so we register one
	// per request.
	stubKeywordSearch := func(reg *httpmock.Registry, codeResponse string) {
		for range 3 {
			reg.Register(
				httpmock.REST("GET", "search/code"),
				httpmock.StringResponse(codeResponse),
			)
		}
	}

	tests := []struct {
		name       string
		opts       *searchOptions
		tty        bool
		httpStubs  func(*httpmock.Registry)
		wantStdout string
		wantStderr string
		wantErr    string
	}{
		{
			name: "displays results in non-TTY",
			tty:  false,
			opts: &searchOptions{Query: "terraform", Page: 1, Limit: defaultLimit},
			httpStubs: func(reg *httpmock.Registry) {
				stubKeywordSearch(reg, `{"total_count": 1, "incomplete_results": false, "items": [{"name": "SKILL.md", "path": "skills/terraform/SKILL.md", "repository": {"full_name": "github/awesome-skills"}}]}`)
			},
			wantStdout: "github/awesome-skills\tterraform\t\t0\n",
		},
		{
			name: "deduplicates results",
			tty:  false,
			opts: &searchOptions{Query: "terraform", Page: 1, Limit: defaultLimit},
			httpStubs: func(reg *httpmock.Registry) {
				stubKeywordSearch(reg, `{"total_count": 3, "incomplete_results": false, "items": [{"name": "SKILL.md", "path": "skills/terraform/SKILL.md", "repository": {"full_name": "github/awesome-skills"}}, {"name": "SKILL.md", "path": "skills/terraform/SKILL.md", "repository": {"full_name": "github/awesome-skills"}}, {"name": "SKILL.md", "path": "skills/terraform-aws/SKILL.md", "repository": {"full_name": "github/awesome-skills"}}]}`)
			},
			wantStdout: "github/awesome-skills\tterraform\t\t0\ngithub/awesome-skills\tterraform-aws\t\t0\n",
		},
		{
			name: "no results",
			tty:  true,
			opts: &searchOptions{Query: "nonexistent", Page: 1, Limit: defaultLimit},
			httpStubs: func(reg *httpmock.Registry) {
				stubKeywordSearch(reg, emptyCodeResponse)
			},
			wantErr: `no skills found matching "nonexistent"`,
		},
		{
			name: "nested skill path",
			tty:  false,
			opts: &searchOptions{Query: "my-skill", Page: 1, Limit: defaultLimit},
			httpStubs: func(reg *httpmock.Registry) {
				stubKeywordSearch(reg, `{"total_count": 1, "incomplete_results": false, "items": [{"name": "SKILL.md", "path": "skills/author/my-skill/SKILL.md", "repository": {"full_name": "org/repo"}}]}`)
			},
			wantStdout: "org/repo\tmy-skill\t\t0\n",
		},
		{
			name: "ranks name-matching results first",
			tty:  false,
			opts: &searchOptions{Query: "terraform", Page: 1, Limit: defaultLimit},
			httpStubs: func(reg *httpmock.Registry) {
				stubKeywordSearch(reg, `{"total_count": 3, "incomplete_results": false, "items": [
						{"name": "SKILL.md", "path": "skills/terraform-deploy/SKILL.md", "repository": {"full_name": "org/repo1"}},
						{"name": "SKILL.md", "path": "skills/terraform-plan/SKILL.md", "repository": {"full_name": "org/repo2"}},
						{"name": "SKILL.md", "path": "skills/terraform/SKILL.md", "repository": {"full_name": "org/repo3"}}
					]}`)
			},
			// exact name match "terraform" first, then partial matches alphabetically by score
			wantStdout: "org/repo3\tterraform\t\t0\norg/repo1\tterraform-deploy\t\t0\norg/repo2\tterraform-plan\t\t0\n",
		},
		{
			name: "caps total pages at 1000-result limit",
			tty:  false,
			opts: &searchOptions{Query: "terraform", Page: 1, Limit: defaultLimit},
			httpStubs: func(reg *httpmock.Registry) {
				stubKeywordSearch(reg, `{"total_count": 5000, "incomplete_results": false, "items": [{"name": "SKILL.md", "path": "skills/terraform/SKILL.md", "repository": {"full_name": "org/repo"}}]}`)
			},
			// In non-TTY mode, no header or pagination text is shown
			wantStdout: "org/repo\tterraform\t\t0\n",
		},
		{
			name: "page beyond available results",
			tty:  false,
			opts: &searchOptions{Query: "terraform", Page: 999, Limit: defaultLimit},
			httpStubs: func(reg *httpmock.Registry) {
				stubKeywordSearch(reg, `{"total_count": 1, "incomplete_results": false, "items": [{"name": "SKILL.md", "path": "skills/terraform/SKILL.md", "repository": {"full_name": "org/repo"}}]}`)
			},
			wantErr: `no skills found on page 999 for query "terraform"`,
		},
		{
			name: "json output with selected fields",
			tty:  false,
			opts: func() *searchOptions {
				exporter := cmdutil.NewJSONExporter()
				exporter.SetFields([]string{"repo", "skillName", "stars"})
				return &searchOptions{Query: "terraform", Page: 1, Limit: defaultLimit, Exporter: exporter}
			}(),
			httpStubs: func(reg *httpmock.Registry) {
				stubKeywordSearch(reg, `{"total_count": 1, "incomplete_results": false, "items": [{"name": "SKILL.md", "path": "skills/terraform/SKILL.md", "repository": {"full_name": "github/awesome-skills"}}]}`)
			},
			wantStdout: "[{\"repo\":\"github/awesome-skills\",\"skillName\":\"terraform\",\"stars\":0}]\n",
		},
		{
			name: "json output empty results",
			tty:  false,
			opts: func() *searchOptions {
				exporter := cmdutil.NewJSONExporter()
				exporter.SetFields([]string{"repo", "skillName"})
				return &searchOptions{Query: "nonexistent", Page: 1, Limit: defaultLimit, Exporter: exporter}
			}(),
			httpStubs: func(reg *httpmock.Registry) {
				stubKeywordSearch(reg, emptyCodeResponse)
			},
			wantStdout: "[]\n",
		},
		{
			name: "rate limit error returns friendly message",
			tty:  false,
			opts: &searchOptions{Query: "terraform", Page: 1, Limit: defaultLimit},
			httpStubs: func(reg *httpmock.Registry) {
				// All search/code calls return 403 with x-ratelimit-remaining: 0
				for range 3 {
					reg.Register(
						httpmock.REST("GET", "search/code"),
						httpmock.WithHeader(
							httpmock.StatusJSONResponse(403, map[string]string{"message": "API rate limit exceeded"}),
							"x-ratelimit-remaining", "0",
						),
					)
				}
			},
			wantErr: rateLimitErrorMessage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := &httpmock.Registry{}
			if tt.httpStubs != nil {
				tt.httpStubs(reg)
			}
			tt.opts.HttpClient = func() (*http.Client, error) {
				return &http.Client{Transport: reg}, nil
			}
			tt.opts.Config = func() (gh.Config, error) {
				return config.NewBlankConfig(), nil
			}

			ios, _, stdout, stderr := iostreams.Test()
			ios.SetStdoutTTY(tt.tty)
			ios.SetStderrTTY(tt.tty)
			tt.opts.IO = ios

			defer reg.Verify(t)
			err := searchRun(tt.opts)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantStdout, stdout.String())
			assert.Equal(t, tt.wantStderr, stderr.String())
		})
	}
}

func TestDeduplicateResults(t *testing.T) {
	items := []codeSearchItem{
		{Path: "skills/terraform/SKILL.md", Repository: codeSearchRepository{FullName: "org/repo"}},
		{Path: "skills/terraform/SKILL.md", Repository: codeSearchRepository{FullName: "org/repo"}},
		{Path: "skills/docker/SKILL.md", Repository: codeSearchRepository{FullName: "org/repo"}},
		{Path: "skills/terraform/SKILL.md", Repository: codeSearchRepository{FullName: "other/repo"}},
	}

	results := deduplicateResults(items)

	assert.Equal(t, 3, len(results))
	assert.Equal(t, "org/repo", results[0].Repo)
	assert.Equal(t, "org", results[0].Owner)
	assert.Equal(t, "repo", results[0].RepoName)
	assert.Equal(t, "terraform", results[0].SkillName)
	assert.Equal(t, "docker", results[1].SkillName)
	assert.Equal(t, "other/repo", results[2].Repo)
	assert.Equal(t, "other", results[2].Owner)
	assert.Equal(t, "terraform", results[2].SkillName)
}

func TestExtractSkillName(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"skills/terraform/SKILL.md", "terraform"},
		{"skills/author/my-skill/SKILL.md", "my-skill"},
		{"SKILL.md", ""},
		{"skills/docker/SKILL.md", "docker"},
		// Root-level convention
		{"my-skill/SKILL.md", "my-skill"},
		// Plugins convention
		{"plugins/openai/skills/chat/SKILL.md", "chat"},
		// Non-matching paths should be filtered out
		{"random/nested/deep/SKILL.md", ""},
		{".hidden/SKILL.md", ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := extractSkillName(tt.path)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFilterByRelevance(t *testing.T) {
	skills := []skillResult{
		{Repo: "org/repo1", Owner: "org", RepoName: "repo1", SkillName: "terraform"},
		{Repo: "org/repo2", Owner: "org", RepoName: "repo2", SkillName: "docker"},
		{Repo: "terraform-corp/tools", Owner: "terraform-corp", RepoName: "tools", SkillName: "linter"},
		{Repo: "acme/terraform-tools", Owner: "acme", RepoName: "terraform-tools", SkillName: "validator"},
		{Repo: "x/y", Owner: "x", RepoName: "y", SkillName: "unrelated", Description: "terraform integration"},
		{Repo: "x/z", Owner: "x", RepoName: "z", SkillName: "noise"},
	}

	filtered := filterByRelevance(skills, "terraform")

	// Should keep: name match (terraform), owner match (terraform-corp),
	// repo name match (terraform-tools), description match (terraform integration).
	// Should drop: docker, noise.
	assert.Equal(t, 4, len(filtered))
	assert.Equal(t, "terraform", filtered[0].SkillName)
	assert.Equal(t, "linter", filtered[1].SkillName)
	assert.Equal(t, "validator", filtered[2].SkillName)
	assert.Equal(t, "unrelated", filtered[3].SkillName)
}

func TestRankByRelevance(t *testing.T) {
	skills := []skillResult{
		{Repo: "org/repo1", Owner: "org", SkillName: "devops"},
		{Repo: "org/repo2", Owner: "org", SkillName: "terraform-plan"},
		{Repo: "org/repo3", Owner: "org", SkillName: "docker", Description: "Manages terraform docker containers"},
		{Repo: "org/repo4", Owner: "org", SkillName: "terraform"},
	}

	rankByRelevance(skills, "terraform")

	// Exact name match scores highest (10 000), then partial name (1 000),
	// then description match (100), then body-only (0).
	assert.Equal(t, "terraform", skills[0].SkillName)
	assert.Equal(t, "terraform-plan", skills[1].SkillName)
	assert.Equal(t, "docker", skills[2].SkillName)
	assert.Equal(t, "devops", skills[3].SkillName)
}

func TestRankByRelevanceStarsTiebreak(t *testing.T) {
	skills := []skillResult{
		{Repo: "small/repo", Owner: "small", SkillName: "terraform", Stars: 10},
		{Repo: "big/repo", Owner: "big", SkillName: "terraform", Stars: 5000},
	}

	rankByRelevance(skills, "terraform")

	// Both have exact name match; big/repo wins on stars tiebreak
	assert.Equal(t, "big/repo", skills[0].Repo)
	assert.Equal(t, "small/repo", skills[1].Repo)
}

func TestFormatStars(t *testing.T) {
	assert.Equal(t, "0", formatStars(0))
	assert.Equal(t, "42", formatStars(42))
	assert.Equal(t, "999", formatStars(999))
	assert.Equal(t, "1.0k", formatStars(1000))
	assert.Equal(t, "1.7k", formatStars(1700))
	assert.Equal(t, "12.5k", formatStars(12500))
}

func splitOnSpaces(s string) []string {
	var parts []string
	current := ""
	for _, c := range s {
		if c == ' ' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

type discardWriter struct{}

func (d *discardWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}
