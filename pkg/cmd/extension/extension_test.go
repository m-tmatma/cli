package extension

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUpdateAvailable_IsLocal(t *testing.T) {
	e := &Extension{
		kind: LocalKind,
	}

	assert.False(t, e.UpdateAvailable())
}

func TestUpdateAvailable_NoCurrentVersion(t *testing.T) {
	e := &Extension{
		kind: LocalKind,
	}

	assert.False(t, e.UpdateAvailable())
}

func TestUpdateAvailable_NoLatestVersion(t *testing.T) {
	e := &Extension{
		kind:           BinaryKind,
		currentVersion: "1.0.0",
	}

	assert.False(t, e.UpdateAvailable())
}

func TestUpdateAvailable_CurrentVersionIsLatestVersion(t *testing.T) {
	e := &Extension{
		kind:           BinaryKind,
		currentVersion: "1.0.0",
		latestVersion:  "1.0.0",
	}

	assert.False(t, e.UpdateAvailable())
}

func TestUpdateAvailable(t *testing.T) {
	e := &Extension{
		kind:           BinaryKind,
		currentVersion: "1.0.0",
		latestVersion:  "1.1.0",
	}

	assert.True(t, e.UpdateAvailable())
}

func TestOwnerLocalExtension(t *testing.T) {
	tempDir := t.TempDir()
	extPath := filepath.Join(tempDir, "extensions", "gh-local", "gh-local")
	assert.NoError(t, stubLocalExtension(tempDir, extPath))
	e := &Extension{
		kind: LocalKind,
		path: extPath,
	}

	assert.Equal(t, "", e.Owner())
}

func TestOwnerBinaryExtension(t *testing.T) {
	tempDir := t.TempDir()
	extName := "gh-bin-ext"
	extDir := filepath.Join(tempDir, "extensions", extName)
	extPath := filepath.Join(extDir, extName)
	bm := binManifest{
		Owner: "owner",
		Name:  "gh-bin-ext",
		Host:  "example.com",
		Tag:   "v1.0.1",
	}
	assert.NoError(t, stubBinaryExtension(extDir, bm))
	e := &Extension{
		kind: BinaryKind,
		path: extPath,
	}

	assert.Equal(t, "owner", e.Owner())
}

func TestOwnerGitExtension(t *testing.T) {
	gc := &mockGitClient{}
	gc.On("Config", "remote.origin.url").Return("git@github.com:owner/repo.git", nil).Once()
	e := &Extension{
		kind:      GitKind,
		gitClient: gc,
	}

	assert.Equal(t, "owner", e.Owner())
}

func TestOwnerCached(t *testing.T) {
	e := &Extension{
		owner: "cli",
	}

	assert.Equal(t, "cli", e.Owner())
}

func TestIsPinnedBinaryExtensionUnpinned(t *testing.T) {
	tempDir := t.TempDir()
	extName := "gh-bin-ext"
	extDir := filepath.Join(tempDir, "extensions", extName)
	extPath := filepath.Join(extDir, extName)
	bm := binManifest{
		Name: "gh-bin-ext",
	}
	assert.NoError(t, stubBinaryExtension(extDir, bm))
	e := &Extension{
		kind: BinaryKind,
		path: extPath,
	}

	assert.False(t, e.IsPinned())
}

func TestIsPinnedBinaryExtensionPinned(t *testing.T) {
	tempDir := t.TempDir()
	extName := "gh-bin-ext"
	extDir := filepath.Join(tempDir, "extensions", extName)
	extPath := filepath.Join(extDir, extName)
	bm := binManifest{
		Name:     "gh-bin-ext",
		IsPinned: true,
	}
	assert.NoError(t, stubBinaryExtension(extDir, bm))
	e := &Extension{
		kind: BinaryKind,
		path: extPath,
	}

	assert.True(t, e.IsPinned())
}

func TestIsPinnedGitExtensionUnpinned(t *testing.T) {
	tempDir := t.TempDir()
	extPath := filepath.Join(tempDir, "extensions", "gh-local", "gh-local")
	assert.NoError(t, stubExtension(extPath))

	gc := &mockGitClient{}
	gc.On("CommandOutput", []string{"rev-parse", "HEAD"}).Return("abcd1234", nil)
	e := &Extension{
		kind:      GitKind,
		gitClient: gc,
		path:      extPath,
	}

	assert.False(t, e.IsPinned())
	gc.AssertExpectations(t)
}

func TestIsPinnedGitExtensionPinned(t *testing.T) {
	tempDir := t.TempDir()
	extPath := filepath.Join(tempDir, "extensions", "gh-local", "gh-local")
	assert.NoError(t, stubPinnedExtension(extPath, "abcd1234"))

	gc := &mockGitClient{}
	gc.On("CommandOutput", []string{"rev-parse", "HEAD"}).Return("abcd1234", nil)
	e := &Extension{
		kind:      GitKind,
		gitClient: gc,
		path:      extPath,
	}

	assert.True(t, e.IsPinned())
	gc.AssertExpectations(t)
}

func TestIsPinnedLocalExtension(t *testing.T) {
	tempDir := t.TempDir()
	extPath := filepath.Join(tempDir, "extensions", "gh-local", "gh-local")
	assert.NoError(t, stubLocalExtension(tempDir, extPath))
	e := &Extension{
		kind: LocalKind,
		path: extPath,
	}

	assert.False(t, e.IsPinned())
}
