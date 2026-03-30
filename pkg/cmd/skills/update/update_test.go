package update

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/cli/cli/v2/git"
	"github.com/cli/cli/v2/internal/config"
	"github.com/cli/cli/v2/internal/gh"
	"github.com/cli/cli/v2/internal/prompter"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/httpmock"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/google/shlex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCmdUpdate_Help(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	f := &cmdutil.Factory{
		IOStreams: ios,
		Prompter:  &prompter.PrompterMock{},
		GitClient: &git.Client{},
	}

	cmd := NewCmdUpdate(f, func(opts *updateOptions) error {
		return nil
	})

	assert.Equal(t, "update [<skill>...]", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotEmpty(t, cmd.Example)
}

func TestNewCmdUpdate_Flags(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	f := &cmdutil.Factory{IOStreams: ios, Prompter: &prompter.PrompterMock{}, GitClient: &git.Client{}}
	cmd := NewCmdUpdate(f, func(_ *updateOptions) error { return nil })

	flags := []string{"all", "force", "dry-run", "dir"}
	for _, name := range flags {
		assert.NotNil(t, cmd.Flags().Lookup(name), "missing flag: --%s", name)
	}
}

func TestNewCmdUpdate_ArgsPassedToOptions(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	f := &cmdutil.Factory{IOStreams: ios, Prompter: &prompter.PrompterMock{}, GitClient: &git.Client{}}

	var gotOpts *updateOptions
	cmd := NewCmdUpdate(f, func(opts *updateOptions) error {
		gotOpts = opts
		return nil
	})

	args, _ := shlex.Split("mcp-cli git-commit --all --force")
	cmd.SetArgs(args)
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)
	err := cmd.Execute()
	require.NoError(t, err)
	assert.Equal(t, []string{"mcp-cli", "git-commit"}, gotOpts.Skills)
	assert.True(t, gotOpts.All)
	assert.True(t, gotOpts.Force)
}

func TestScanInstalledSkills(t *testing.T) {
	dir := t.TempDir()

	skillDir := filepath.Join(dir, "git-commit")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	content := "---\nname: git-commit\ndescription: Git commit helper\nmetadata:\n  github-owner: github\n  github-repo: awesome-copilot\n  github-tree-sha: abc123\n  github-path: skills/git-commit\n---\nBody content\n"
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644))

	noMetaDir := filepath.Join(dir, "unknown-skill")
	require.NoError(t, os.MkdirAll(noMetaDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(noMetaDir, "SKILL.md"), []byte("---\nname: unknown-skill\n---\nNo metadata here\n"), 0o644))

	pinnedDir := filepath.Join(dir, "pinned-skill")
	require.NoError(t, os.MkdirAll(pinnedDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pinnedDir, "SKILL.md"), []byte("---\nname: pinned-skill\nmetadata:\n  github-owner: octo\n  github-repo: skills\n  github-tree-sha: def456\n  github-pinned: v1.0.0\n---\nPinned content\n"), 0o644))

	skills, err := scanInstalledSkills(dir, nil, "")
	require.NoError(t, err)
	assert.Len(t, skills, 3)

	byName := make(map[string]installedSkill)
	for _, s := range skills {
		byName[s.name] = s
	}

	gc := byName["git-commit"]
	assert.Equal(t, "github", gc.owner)
	assert.Equal(t, "awesome-copilot", gc.repo)
	assert.Equal(t, "abc123", gc.treeSHA)
	assert.Equal(t, "skills/git-commit", gc.sourcePath)
	assert.Empty(t, gc.pinned)

	us := byName["unknown-skill"]
	assert.Empty(t, us.owner)
	assert.Empty(t, us.repo)

	ps := byName["pinned-skill"]
	assert.Equal(t, "v1.0.0", ps.pinned)
}

func TestScanInstalledSkills_NonExistentDir(t *testing.T) {
	skills, err := scanInstalledSkills("/nonexistent/path", nil, "")
	require.NoError(t, err)
	assert.Nil(t, skills)
}

func TestScanInstalledSkills_CorruptedYAML(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "corrupt")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nnot: valid: yaml: [broken\n---\nbody\n"), 0o644))

	skills, err := scanInstalledSkills(dir, nil, "")
	require.NoError(t, err)
	assert.Len(t, skills, 0)
}

func TestPromptForSkillOrigin_Valid(t *testing.T) {
	pm := &prompter.PrompterMock{
		InputFunc: func(prompt string, defaultValue string) (string, error) {
			return "github/awesome-copilot", nil
		},
	}
	owner, repo, _, ok, err := promptForSkillOrigin(pm, "test-skill")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "github", owner)
	assert.Equal(t, "awesome-copilot", repo)
}

func TestPromptForSkillOrigin_Empty(t *testing.T) {
	pm := &prompter.PrompterMock{
		InputFunc: func(prompt string, defaultValue string) (string, error) {
			return "", nil
		},
	}
	_, _, _, ok, err := promptForSkillOrigin(pm, "test-skill")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestPromptForSkillOrigin_Invalid(t *testing.T) {
	pm := &prompter.PrompterMock{
		InputFunc: func(prompt string, defaultValue string) (string, error) {
			return "just-a-name", nil
		},
	}
	_, _, reason, ok, err := promptForSkillOrigin(pm, "test-skill")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Contains(t, reason, "invalid repository")
}

func TestUpdateRun_NoInstalledSkills(t *testing.T) {
	ios, _, _, stderr := iostreams.Test()
	ios.SetStdoutTTY(false)

	dir := t.TempDir()

	reg := &httpmock.Registry{}
	opts := &updateOptions{
		IO:     ios,
		Config: func() (gh.Config, error) { return config.NewBlankConfig(), nil },
		HttpClient: func() (*http.Client, error) {
			return &http.Client{Transport: reg}, nil
		},
		GitClient: &git.Client{RepoDir: dir},
		Dir:       dir,
	}

	defer reg.Verify(t)
	err := updateRun(opts)
	require.NoError(t, err)
	assert.Contains(t, stderr.String(), "No installed skills found.")
}

func TestUpdateRun_SpecificSkillNotInstalled(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	ios.SetStdoutTTY(false)

	dir := t.TempDir()
	skillDir := filepath.Join(dir, "existing-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: existing-skill\nmetadata:\n  github-owner: owner\n  github-repo: repo\n  github-tree-sha: abc\n---\n"), 0o644))

	reg := &httpmock.Registry{}
	opts := &updateOptions{
		IO:     ios,
		Config: func() (gh.Config, error) { return config.NewBlankConfig(), nil },
		HttpClient: func() (*http.Client, error) {
			return &http.Client{Transport: reg}, nil
		},
		GitClient: &git.Client{RepoDir: dir},
		Dir:       dir,
		Skills:    []string{"nonexistent"},
	}

	defer reg.Verify(t)
	err := updateRun(opts)
	assert.EqualError(t, err, "none of the specified skills are installed")
}

func TestUpdateRun_PinnedSkillsSkipped(t *testing.T) {
	ios, _, _, stderr := iostreams.Test()
	ios.SetStdoutTTY(true)
	ios.SetStderrTTY(true)

	dir := t.TempDir()
	skillDir := filepath.Join(dir, "pinned-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: pinned-skill\nmetadata:\n  github-owner: owner\n  github-repo: repo\n  github-tree-sha: abc123\n  github-pinned: v1.0.0\n---\n"), 0o644))

	reg := &httpmock.Registry{}
	opts := &updateOptions{
		IO:     ios,
		Config: func() (gh.Config, error) { return config.NewBlankConfig(), nil },
		HttpClient: func() (*http.Client, error) {
			return &http.Client{Transport: reg}, nil
		},
		Prompter:  &prompter.PrompterMock{},
		GitClient: &git.Client{RepoDir: dir},
		Dir:       dir,
	}

	defer reg.Verify(t)
	err := updateRun(opts)
	require.NoError(t, err)
	assert.Contains(t, stderr.String(), "pinned-skill is pinned to v1.0.0 (skipped)")
	assert.Contains(t, stderr.String(), "All skills are up to date.")
}

func TestUpdateRun_NoMetaSkipsNonInteractive(t *testing.T) {
	ios, _, _, stderr := iostreams.Test()
	ios.SetStdoutTTY(false)
	ios.SetStdinTTY(false)

	dir := t.TempDir()
	skillDir := filepath.Join(dir, "manual-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: manual-skill\n---\nNo metadata\n"), 0o644))

	reg := &httpmock.Registry{}
	opts := &updateOptions{
		IO:     ios,
		Config: func() (gh.Config, error) { return config.NewBlankConfig(), nil },
		HttpClient: func() (*http.Client, error) {
			return &http.Client{Transport: reg}, nil
		},
		GitClient: &git.Client{RepoDir: dir},
		Dir:       dir,
	}

	defer reg.Verify(t)
	err := updateRun(opts)
	require.NoError(t, err)
	assert.Contains(t, stderr.String(), "manual-skill has no GitHub metadata")
}

func TestUpdateRun_AllUpToDate(t *testing.T) {
	ios, _, _, stderr := iostreams.Test()
	ios.SetStdoutTTY(false)

	dir := t.TempDir()
	skillDir := filepath.Join(dir, "my-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: my-skill\nmetadata:\n  github-owner: octo\n  github-repo: skills\n  github-tree-sha: abc123def456\n  github-path: skills/my-skill\n---\n"), 0o644))

	reg := &httpmock.Registry{}
	reg.Register(
		httpmock.REST("GET", "repos/octo/skills/releases/latest"),
		httpmock.StringResponse(`{"tag_name": "v1.0.0"}`),
	)
	reg.Register(
		httpmock.REST("GET", "repos/octo/skills/git/ref/tags/v1.0.0"),
		httpmock.StringResponse(`{"object": {"sha": "commitsha123", "type": "commit"}}`),
	)
	reg.Register(
		httpmock.REST("GET", fmt.Sprintf("repos/octo/skills/git/trees/commitsha123")),
		httpmock.StringResponse(`{"sha": "commitsha123", "tree": [{"path": "skills/my-skill/SKILL.md", "type": "blob", "sha": "blobsha1"}, {"path": "skills/my-skill", "type": "tree", "sha": "abc123def456"}, {"path": "skills", "type": "tree", "sha": "treeshaX"}], "truncated": false}`),
	)

	opts := &updateOptions{
		IO:     ios,
		Config: func() (gh.Config, error) { return config.NewBlankConfig(), nil },
		HttpClient: func() (*http.Client, error) {
			return &http.Client{Transport: reg}, nil
		},
		GitClient: &git.Client{RepoDir: dir},
		Dir:       dir,
	}

	defer reg.Verify(t)
	err := updateRun(opts)
	require.NoError(t, err)
	assert.Contains(t, stderr.String(), "All skills are up to date.")
}

func TestUpdateRun_DryRun(t *testing.T) {
	ios, _, stdout, stderr := iostreams.Test()
	ios.SetStdoutTTY(true)
	ios.SetStderrTTY(true)

	dir := t.TempDir()
	skillDir := filepath.Join(dir, "my-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: my-skill\nmetadata:\n  github-owner: octo\n  github-repo: skills\n  github-tree-sha: oldsha123\n  github-path: skills/my-skill\n---\n"), 0o644))

	reg := &httpmock.Registry{}
	reg.Register(
		httpmock.REST("GET", "repos/octo/skills/releases/latest"),
		httpmock.StringResponse(`{"tag_name": "v2.0.0"}`),
	)
	reg.Register(
		httpmock.REST("GET", "repos/octo/skills/git/ref/tags/v2.0.0"),
		httpmock.StringResponse(`{"object": {"sha": "newcommit456", "type": "commit"}}`),
	)
	reg.Register(
		httpmock.REST("GET", "repos/octo/skills/git/trees/newcommit456"),
		httpmock.StringResponse(`{"sha": "newcommit456", "tree": [{"path": "skills/my-skill/SKILL.md", "type": "blob", "sha": "blobsha2"}, {"path": "skills/my-skill", "type": "tree", "sha": "newsha456"}, {"path": "skills", "type": "tree", "sha": "treeshaY"}], "truncated": false}`),
	)

	opts := &updateOptions{
		IO:     ios,
		Config: func() (gh.Config, error) { return config.NewBlankConfig(), nil },
		HttpClient: func() (*http.Client, error) {
			return &http.Client{Transport: reg}, nil
		},
		Prompter:  &prompter.PrompterMock{},
		GitClient: &git.Client{RepoDir: dir},
		Dir:       dir,
		DryRun:    true,
	}

	defer reg.Verify(t)
	err := updateRun(opts)
	require.NoError(t, err)
	assert.Contains(t, stderr.String(), "1 update(s) available:")
	assert.Contains(t, stdout.String(), "my-skill")
	assert.Contains(t, stdout.String(), "octo/skills")
}

func TestUpdateRun_NonInteractiveNoAll(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	ios.SetStdoutTTY(false)
	ios.SetStdinTTY(false)

	dir := t.TempDir()
	skillDir := filepath.Join(dir, "my-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: my-skill\nmetadata:\n  github-owner: octo\n  github-repo: skills\n  github-tree-sha: oldsha123\n  github-path: skills/my-skill\n---\n"), 0o644))

	reg := &httpmock.Registry{}
	reg.Register(
		httpmock.REST("GET", "repos/octo/skills/releases/latest"),
		httpmock.StringResponse(`{"tag_name": "v2.0.0"}`),
	)
	reg.Register(
		httpmock.REST("GET", "repos/octo/skills/git/ref/tags/v2.0.0"),
		httpmock.StringResponse(`{"object": {"sha": "newcommit456", "type": "commit"}}`),
	)
	reg.Register(
		httpmock.REST("GET", "repos/octo/skills/git/trees/newcommit456"),
		httpmock.StringResponse(`{"sha": "newcommit456", "tree": [{"path": "skills/my-skill/SKILL.md", "type": "blob", "sha": "blobsha2"}, {"path": "skills/my-skill", "type": "tree", "sha": "newsha456"}, {"path": "skills", "type": "tree", "sha": "treeshaY"}], "truncated": false}`),
	)

	opts := &updateOptions{
		IO:     ios,
		Config: func() (gh.Config, error) { return config.NewBlankConfig(), nil },
		HttpClient: func() (*http.Client, error) {
			return &http.Client{Transport: reg}, nil
		},
		GitClient: &git.Client{RepoDir: dir},
		Dir:       dir,
	}

	defer reg.Verify(t)
	err := updateRun(opts)
	assert.EqualError(t, err, "updates available; re-run with --all to apply, or run interactively to confirm")
}
