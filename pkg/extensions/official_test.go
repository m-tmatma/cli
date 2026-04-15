package extensions

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindOfficialExtension(t *testing.T) {
	tests := []struct {
		name        string
		commandName string
		wantNil     bool
		wantRepo    string
	}{
		{name: "found", commandName: "stack", wantNil: false, wantRepo: "gh-stack"},
		{name: "not found", commandName: "xyzzy", wantNil: true},
		{name: "empty", commandName: "", wantNil: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ext := FindOfficialExtension(tt.commandName)
			if tt.wantNil {
				assert.Nil(t, ext)
			} else {
				require.NotNil(t, ext)
				assert.Equal(t, tt.wantRepo, ext.Repo)
			}
		})
	}
}

func TestOfficialExtension_Repository(t *testing.T) {
	ext := &OfficialExtension{Name: "stack", Owner: "github", Repo: "gh-stack"}
	repo := ext.Repository()
	assert.Equal(t, "github", repo.RepoOwner())
	assert.Equal(t, "gh-stack", repo.RepoName())
	assert.Equal(t, "github.com", repo.RepoHost())
}
