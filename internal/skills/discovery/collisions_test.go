package discovery

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFindNameCollisions(t *testing.T) {
	tests := []struct {
		name   string
		skills []Skill
		want   []NameCollision
	}{
		{
			name: "no collisions",
			skills: []Skill{
				{Name: "code-review", Path: "skills/code-review"},
				{Name: "issue-triage", Path: "skills/issue-triage"},
			},
			want: nil,
		},
		{
			name: "single collision",
			skills: []Skill{
				{Name: "pr-summary", Path: "skills/pr-summary"},
				{Name: "pr-summary", Path: "skills/monalisa/pr-summary"},
			},
			want: []NameCollision{
				{Name: "pr-summary", DisplayNames: []string{"pr-summary", "pr-summary"}},
			},
		},
		{
			name: "collisions sorted by name",
			skills: []Skill{
				{Name: "octocat-lint", Path: "skills/octocat-lint"},
				{Name: "octocat-lint", Path: "skills/hubot/octocat-lint"},
				{Name: "code-review", Path: "skills/code-review"},
				{Name: "code-review", Path: "skills/monalisa/code-review"},
			},
			want: []NameCollision{
				{Name: "code-review", DisplayNames: []string{"code-review", "code-review"}},
				{Name: "octocat-lint", DisplayNames: []string{"octocat-lint", "octocat-lint"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FindNameCollisions(tt.skills)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFormatCollisions(t *testing.T) {
	collisions := []NameCollision{
		{Name: "pr-summary", DisplayNames: []string{"skills/pr-summary", "plugins/hubot/pr-summary"}},
		{Name: "code-review", DisplayNames: []string{"skills/code-review", "skills/monalisa/code-review"}},
	}
	got := FormatCollisions(collisions)
	assert.Equal(t, "pr-summary: skills/pr-summary, plugins/hubot/pr-summary\n  code-review: skills/code-review, skills/monalisa/code-review", got)
}
