//go:build windows

package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsLocalPath_Windows(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		want bool
	}{
		// Backslash-relative paths that only exist on Windows.
		{`dot-backslash prefix`, `.\skills`, true},
		{`dotdot-backslash prefix`, `..\other`, true},
		{`drive-absolute path`, `C:\Users\me\skills`, true},
		{`drive-relative path`, `D:\projects`, true},
		{`UNC path`, `\\server\share\skills`, true},

		// Forward-slash forms should still work on Windows.
		{`dot-slash prefix`, `./skills`, true},
		{`dotdot-slash prefix`, `../other`, true},
		{`current dir`, `.`, true},
		{`absolute unix-style`, `/tmp/skills`, true},
		{`tilde prefix`, `~/skills`, true},

		// owner/repo should never be treated as local.
		{`owner-repo`, `github/awesome-copilot`, false},
		{`simple name`, `awesome-copilot`, false},
		{`empty string`, ``, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isLocalPath(tt.arg)
			assert.Equal(t, tt.want, got, "isLocalPath(%q)", tt.arg)
		})
	}
}

func TestIsLocalPath_WindowsExistingDir(t *testing.T) {
	// A directory that exists on disk should be detected as local even when
	// its name looks like owner/repo (the os.Stat safety-net).
	dir := t.TempDir()
	nested := filepath.Join(dir, "owner", "repo")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	// Use a relative path that happens to contain a backslash separator.
	rel, err := filepath.Rel(".", nested)
	if err != nil {
		// If we can't compute a relative path, just use the absolute one.
		rel = nested
	}
	assert.True(t, isLocalPath(rel), "existing dir should be detected as local: %s", rel)
}
