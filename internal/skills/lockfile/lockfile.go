package lockfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	// lockVersion must match Vercel's CURRENT_LOCK_VERSION for interop.
	lockVersion = 3
	agentsDir   = ".agents"
	lockFile    = ".skill-lock.json"
)

// entry represents a single installed skill in the lock file.
type entry struct {
	Source          string `json:"source"`
	SourceType      string `json:"sourceType"`
	SourceURL       string `json:"sourceUrl"`
	SkillPath       string `json:"skillPath,omitempty"`
	SkillFolderHash string `json:"skillFolderHash"`
	InstalledAt     string `json:"installedAt"`
	UpdatedAt       string `json:"updatedAt"`
	PinnedRef       string `json:"pinnedRef,omitempty"`
}

// file is the top-level structure of .skill-lock.json.
type file struct {
	Version   int              `json:"version"`
	Skills    map[string]entry `json:"skills"`
	Dismissed map[string]bool  `json:"dismissed,omitempty"`
}

// lockfilePath returns the absolute path to the lock file.
func lockfilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, agentsDir, lockFile), nil
}

// read loads the lock file, returning an empty file if it doesn't exist
// or if it's an incompatible version.
func read() (*file, error) {
	lockPath, err := lockfilePath()
	if err != nil {
		return newFile(), nil //nolint:nilerr // graceful: no home dir means fresh state
	}

	data, err := os.ReadFile(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			return newFile(), nil
		}
		return nil, fmt.Errorf("could not read lock file: %w", err)
	}

	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return newFile(), nil //nolint:nilerr // graceful: corrupt file means fresh state
	}

	if f.Version != lockVersion || f.Skills == nil {
		return newFile(), nil
	}

	return &f, nil
}

// write persists the lock file to disk.
func write(f *file) error {
	lockPath, err := lockfilePath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(lockPath, data, 0o644)
}

// RecordInstall adds or updates a skill entry in the lock file.
// It uses a file-based lock to prevent concurrent read-modify-write races
// when multiple install processes run simultaneously.
func RecordInstall(skillName, owner, repo, skillPath, treeSHA, pinnedRef string) error {
	unlock := acquireLock()
	defer unlock()

	f, err := read()
	if err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)

	existing, exists := f.Skills[skillName]
	installedAt := now
	if exists {
		installedAt = existing.InstalledAt
	}

	f.Skills[skillName] = entry{
		Source:          owner + "/" + repo,
		SourceType:      "github",
		SourceURL:       "https://github.com/" + owner + "/" + repo + ".git",
		SkillPath:       skillPath,
		SkillFolderHash: treeSHA,
		InstalledAt:     installedAt,
		UpdatedAt:       now,
		PinnedRef:       pinnedRef,
	}

	return write(f)
}

func newFile() *file {
	return &file{
		Version: lockVersion,
		Skills:  make(map[string]entry),
	}
}

// acquireLock creates an exclusive lock file to serialize concurrent access.
// Returns an unlock function. If locking fails after retries, it proceeds
// unlocked rather than blocking the user indefinitely.
func acquireLock() (unlock func()) {
	lockPath, pathErr := lockfilePath()
	if pathErr != nil {
		return func() {}
	}
	lkPath := lockPath + ".lk"

	// Ensure the parent directory exists (fresh machine may lack ~/.agents).
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return func() {}
	}

	for i := 0; i < 30; i++ {
		f, createErr := os.OpenFile(lkPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if createErr == nil {
			f.Close()
			return func() { os.Remove(lkPath) }
		}
		// Only retry when the lock file already exists (concurrent process).
		// For other errors (permission denied, invalid path, etc.) give up immediately.
		if !os.IsExist(createErr) {
			return func() {}
		}
		// Break stale locks older than 30s (e.g. from a crashed process).
		if info, statErr := os.Stat(lkPath); statErr == nil && time.Since(info.ModTime()) > 30*time.Second {
			os.Remove(lkPath)
			continue
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Best-effort: proceed without lock.
	return func() {}
}
