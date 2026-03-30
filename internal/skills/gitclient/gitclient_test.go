package gitclient

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

type mockResolver struct {
	root string
	err  error
}

func (m *mockResolver) ToplevelDir() (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.root, nil
}

func TestResolveGitRoot(t *testing.T) {
	t.Run("returns root on success", func(t *testing.T) {
		got := ResolveGitRoot(&mockResolver{root: "/my/repo"})
		assert.Equal(t, "/my/repo", got)
	})

	t.Run("falls back to cwd on error", func(t *testing.T) {
		got := ResolveGitRoot(&mockResolver{err: fmt.Errorf("not a git repo")})
		assert.NotEmpty(t, got) // falls back to cwd
	})

	t.Run("nil resolver falls back to cwd", func(t *testing.T) {
		got := ResolveGitRoot(nil)
		assert.NotEmpty(t, got) // falls back to cwd
	})
}

func TestResolveHomeDir(t *testing.T) {
	got := ResolveHomeDir()
	assert.NotEmpty(t, got)
}

func TestTruncateSHA(t *testing.T) {
	assert.Equal(t, "abcdef12", TruncateSHA("abcdef1234567890"))
	assert.Equal(t, "short", TruncateSHA("short"))
	assert.Equal(t, "12345678", TruncateSHA("12345678"))
	assert.Equal(t, "", TruncateSHA(""))
}
