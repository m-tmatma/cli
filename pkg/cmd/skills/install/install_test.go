package install

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cli/cli/v2/git"
	"github.com/cli/cli/v2/internal/ghrepo"
	"github.com/cli/cli/v2/internal/prompter"
	"github.com/cli/cli/v2/internal/skills/discovery"
	"github.com/cli/cli/v2/internal/skills/registry"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/httpmock"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/google/shlex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCmdInstall_Help(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	f := &cmdutil.Factory{
		IOStreams: ios,
		Prompter:  &prompter.PrompterMock{},
		GitClient: &git.Client{},
	}

	cmd := NewCmdInstall(f, func(opts *installOptions) error {
		return nil
	})

	assert.Equal(t, "install <repository> [<skill[@version]>]", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotEmpty(t, cmd.Example)
}

func TestNewCmdInstall_Alias(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	f := &cmdutil.Factory{IOStreams: ios, Prompter: &prompter.PrompterMock{}, GitClient: &git.Client{}}
	cmd := NewCmdInstall(f, func(_ *installOptions) error { return nil })
	assert.Contains(t, cmd.Aliases, "add")
}

func TestNewCmdInstall_Flags(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	f := &cmdutil.Factory{IOStreams: ios, Prompter: &prompter.PrompterMock{}, GitClient: &git.Client{}}
	cmd := NewCmdInstall(f, func(_ *installOptions) error { return nil })

	flags := []string{"agent", "scope", "pin", "all", "dir", "force"}
	for _, name := range flags {
		assert.NotNil(t, cmd.Flags().Lookup(name), "missing flag: --%s", name)
	}
}

func TestNewCmdInstall_MaxArgs(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	f := &cmdutil.Factory{IOStreams: ios, Prompter: &prompter.PrompterMock{}, GitClient: &git.Client{}}
	cmd := NewCmdInstall(f, func(_ *installOptions) error { return nil })

	cmd.SetArgs([]string{"a", "b", "c"})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestResolveRepoArg(t *testing.T) {
	tests := []struct {
		input   string
		owner   string
		repo    string
		wantErr bool
	}{
		{"github/awesome-copilot", "github", "awesome-copilot", false},
		{"owner/repo", "owner", "repo", false},
		{"a/b", "a", "b", false},
		{"https://github.com/owner/repo", "owner", "repo", false},
		{"https://github.com/owner/repo.git", "owner", "repo", false},
		{"invalid", "", "", true},
		{"", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			repo, _, err := resolveRepoArg(tt.input, false, nil)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.owner, repo.RepoOwner())
			assert.Equal(t, tt.repo, repo.RepoName())
		})
	}
}

func TestParseSkillFromOpts(t *testing.T) {
	tests := []struct {
		name      string
		skillName string
		pin       string
		wantName  string
		wantVer   string
	}{
		{
			name:      "name with version",
			skillName: "git-commit@v1.2.0",
			wantName:  "git-commit",
			wantVer:   "v1.2.0",
		},
		{
			name:      "name without version",
			skillName: "git-commit",
			wantName:  "git-commit",
			wantVer:   "",
		},
		{
			name:      "inline version takes precedence over pin",
			skillName: "git-commit@v1.0.0",
			pin:       "v2.0.0",
			wantName:  "git-commit",
			wantVer:   "v1.0.0",
		},
		{
			name:      "pin flag alone",
			skillName: "git-commit",
			pin:       "v3.0.0",
			wantName:  "git-commit",
			wantVer:   "v3.0.0",
		},
		{
			name:      "empty",
			skillName: "",
			wantName:  "",
			wantVer:   "",
		},
		{
			name:      "@ at start is not version",
			skillName: "@foo",
			wantName:  "@foo",
			wantVer:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &installOptions{SkillName: tt.skillName, Pin: tt.pin}
			parseSkillFromOpts(opts)
			assert.Equal(t, tt.wantName, opts.SkillName)
			assert.Equal(t, tt.wantVer, opts.version)
		})
	}
}

func TestInstallRun_NonInteractive_NoRepo(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	ios.SetStdoutTTY(false)

	opts := &installOptions{
		IO:        ios,
		GitClient: &git.Client{RepoDir: t.TempDir()},
	}

	err := installRun(opts)
	assert.Error(t, err)
	assert.Equal(t, "must specify a repository to install from", err.Error())
}

func TestInstallRun_NonInteractive_NoSkill(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	ios.SetStdoutTTY(false)

	opts := &installOptions{IO: ios, repo: ghrepo.New("o", "r")}
	skills := []discovery.Skill{{Name: "test-skill", Path: "skills/test-skill"}}
	_, err := selectSkillsWithSelector(opts, skills, false, skillSelector{matchByName: matchSkillByName, sourceHint: "REPO"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must specify a skill name or use --all")
}

func TestSelectSkills_All(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	skills := []discovery.Skill{
		{Name: "a"},
		{Name: "b"},
	}
	opts := &installOptions{All: true, IO: ios, repo: ghrepo.New("o", "r")}
	got, err := selectSkillsWithSelector(opts, skills, false, skillSelector{matchByName: matchSkillByName, sourceHint: "REPO"})
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

func TestSelectSkills_ByName(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	skills := []discovery.Skill{
		{Name: "alpha"},
		{Name: "beta"},
	}
	opts := &installOptions{SkillName: "beta", IO: ios, repo: ghrepo.New("o", "r")}
	got, err := selectSkillsWithSelector(opts, skills, false, skillSelector{matchByName: matchSkillByName, sourceHint: "REPO"})
	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, "beta", got[0].Name)
}

func TestSelectSkills_NotFound(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	skills := []discovery.Skill{
		{Name: "alpha"},
	}
	opts := &installOptions{SkillName: "nonexistent", IO: ios, repo: ghrepo.New("o", "r")}
	_, err := selectSkillsWithSelector(opts, skills, false, skillSelector{matchByName: matchSkillByName, sourceHint: "REPO"})
	assert.Error(t, err)
}

func TestSkillSearchFunc_EmptyQuery(t *testing.T) {
	skills := []discovery.Skill{
		{Name: "alpha", Description: "first skill"},
		{Name: "beta", Description: "second skill"},
	}
	fn := skillSearchFunc(skills, 40)
	result := fn("")
	assert.Nil(t, result.Err)
	assert.Len(t, result.Keys, 2)
	assert.Equal(t, "alpha", result.Keys[0])
	assert.Equal(t, "beta", result.Keys[1])
	assert.Equal(t, 0, result.MoreResults)
}

func TestSkillSearchFunc_FilterByName(t *testing.T) {
	skills := []discovery.Skill{
		{Name: "git-commit"},
		{Name: "code-review"},
		{Name: "git-push"},
	}
	fn := skillSearchFunc(skills, 40)
	result := fn("git")
	assert.Nil(t, result.Err)
	assert.Len(t, result.Keys, 2)
	assert.Equal(t, "git-commit", result.Keys[0])
	assert.Equal(t, "git-push", result.Keys[1])
}

func TestSkillSearchFunc_FilterByDescription(t *testing.T) {
	skills := []discovery.Skill{
		{Name: "alpha", Description: "handles authentication"},
		{Name: "beta", Description: "builds docker images"},
	}
	fn := skillSearchFunc(skills, 40)
	result := fn("docker")
	assert.Nil(t, result.Err)
	assert.Len(t, result.Keys, 1)
	assert.Equal(t, "beta", result.Keys[0])
}

func TestSkillSearchFunc_CaseInsensitive(t *testing.T) {
	skills := []discovery.Skill{
		{Name: "Git-Commit"},
	}
	fn := skillSearchFunc(skills, 40)
	result := fn("GIT")
	assert.Nil(t, result.Err)
	assert.Len(t, result.Keys, 1)
}

func TestSkillSearchFunc_MoreResults(t *testing.T) {
	skills := make([]discovery.Skill, 50)
	for i := range skills {
		skills[i] = discovery.Skill{Name: fmt.Sprintf("skill-%d", i)}
	}
	fn := skillSearchFunc(skills, 40)
	result := fn("")
	assert.Equal(t, maxSearchResults, len(result.Keys))
	assert.Equal(t, 50-maxSearchResults, result.MoreResults)
}

func TestMatchSelectedSkills(t *testing.T) {
	skills := []discovery.Skill{
		{Name: "alpha"},
		{Name: "beta"},
		{Name: "gamma"},
	}
	got, err := matchSelectedSkills(skills, []string{"alpha", "gamma"})
	require.NoError(t, err)
	assert.Len(t, got, 2)
	assert.Equal(t, "alpha", got[0].Name)
	assert.Equal(t, "gamma", got[1].Name)
}

func TestMatchSelectedSkills_NoMatch(t *testing.T) {
	skills := []discovery.Skill{{Name: "alpha"}}
	_, err := matchSelectedSkills(skills, []string{"nonexistent"})
	assert.Error(t, err)
}

func TestResolveHosts_ByFlag(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	opts := &installOptions{Agent: "claude-code", IO: ios}
	hosts, err := resolveHosts(opts, false)
	require.NoError(t, err)
	assert.Len(t, hosts, 1)
	assert.Equal(t, "claude-code", hosts[0].ID)
}

func TestResolveHosts_InvalidFlag(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	opts := &installOptions{Agent: "nonexistent", IO: ios}
	_, err := resolveHosts(opts, false)
	assert.Error(t, err)
}

func TestResolveHosts_DefaultNonInteractive(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	opts := &installOptions{IO: ios}
	hosts, err := resolveHosts(opts, false)
	require.NoError(t, err)
	assert.Len(t, hosts, 1)
	assert.Equal(t, "github-copilot", hosts[0].ID)
}

func TestResolveHosts_MultiSelect(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	pm := &prompter.PrompterMock{
		MultiSelectFunc: func(_ string, _ []string, _ []string) ([]int, error) {
			return []int{0, 1}, nil
		},
	}
	opts := &installOptions{IO: ios, Prompter: pm}
	hosts, err := resolveHosts(opts, true)
	require.NoError(t, err)
	assert.Len(t, hosts, 2)
}

func TestResolveHosts_NoneSelected(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	pm := &prompter.PrompterMock{
		MultiSelectFunc: func(_ string, _ []string, _ []string) ([]int, error) {
			return []int{}, nil
		},
	}
	opts := &installOptions{IO: ios, Prompter: pm}
	_, err := resolveHosts(opts, true)
	assert.Error(t, err)
}

func TestTruncateDescription(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxWidth int
	}{
		{"short stays short", "A short description", 60},
		{"newlines collapsed", "Line one.\nLine two.\nLine three.", 60},
		{"excessive whitespace", "  lots   of   spaces  ", 60},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateDescription(tt.input, tt.maxWidth)
			assert.NotContains(t, got, "\n")
		})
	}

	long := "Execute git commit with conventional commit message analysis and intelligent staging"
	got := truncateDescription(long, 30)
	assert.LessOrEqual(t, len(got), 33) // allow room for ellipsis
}

func TestIsLocalPath(t *testing.T) {
	tests := []struct {
		arg  string
		want bool
	}{
		{".", true},
		{"./skills", true},
		{"../other", true},
		{"/tmp/skills", true},
		{"~/skills", true},
		{"github/awesome-copilot", false},
		{"owner/repo", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			got := isLocalPath(tt.arg)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsSkillPath(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"skills/test-skill", true},
		{"skills/author/skill", true},
		{"plugins/author/skills/skill", true},
		{"skills/author/skill/SKILL.md", true},
		{"git-commit", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isSkillPath(tt.name))
		})
	}
}

func TestRunLocalInstall_NonInteractive(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "test-local")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	content := "---\nname: test-local\ndescription: A local skill\n---\n# Test\n"
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644))

	targetDir := t.TempDir()
	ios, _, stdout, _ := iostreams.Test()
	ios.SetStdoutTTY(false)
	ios.SetColorEnabled(false)

	opts := &installOptions{
		IO:          ios,
		SkillSource: dir,
		localPath:   dir,
		All:         true,
		Force:       true,
		Agent:       "github-copilot",
		Scope:       "project",
		Dir:         targetDir,
		GitClient:   &git.Client{RepoDir: t.TempDir()},
	}

	err := installRun(opts)
	require.NoError(t, err)

	assert.Contains(t, stdout.String(), "Installed test-local")

	installed, err := os.ReadFile(filepath.Join(targetDir, "test-local", "SKILL.md"))
	require.NoError(t, err)
	assert.Contains(t, string(installed), "local-path")
}

func TestRunLocalInstall_SingleSkillDir(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: direct-skill\ndescription: Direct\n---\n# Direct\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644))

	targetDir := t.TempDir()
	ios, _, stdout, _ := iostreams.Test()
	ios.SetStdoutTTY(false)
	ios.SetColorEnabled(false)

	opts := &installOptions{
		IO:          ios,
		SkillSource: dir,
		localPath:   dir,
		All:         true,
		Force:       true,
		Agent:       "github-copilot",
		Scope:       "project",
		Dir:         targetDir,
		GitClient:   &git.Client{RepoDir: t.TempDir()},
	}

	err := installRun(opts)
	require.NoError(t, err)

	assert.Contains(t, stdout.String(), "Installed direct-skill")
}

func TestCollisionError(t *testing.T) {
	t.Run("no collisions", func(t *testing.T) {
		skills := []discovery.Skill{
			{Name: "a"},
			{Name: "b"},
		}
		assert.NoError(t, collisionError(skills, "REPO"))
	})

	t.Run("no collisions with different namespaces", func(t *testing.T) {
		skills := []discovery.Skill{
			{Name: "xlsx-pro", Namespace: "author1"},
			{Name: "xlsx-pro", Namespace: "author2"},
		}
		assert.NoError(t, collisionError(skills, "REPO"))
	})

	t.Run("has collisions same name no namespace", func(t *testing.T) {
		skills := []discovery.Skill{
			{Name: "xlsx-pro", Convention: "skills"},
			{Name: "xlsx-pro", Convention: "root"},
		}
		err := collisionError(skills, "REPO")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "conflicting names")
		assert.Contains(t, err.Error(), "gh skills install REPO")
	})

	t.Run("local source hint", func(t *testing.T) {
		skills := []discovery.Skill{
			{Name: "xlsx-pro", Convention: "skills"},
			{Name: "xlsx-pro", Convention: "root"},
		}
		err := collisionError(skills, "PATH")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "conflicting names")
		assert.Contains(t, err.Error(), "gh skills install PATH")
	})
}

func TestMatchSkillByName_Ambiguous(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	skills := []discovery.Skill{
		{Name: "xlsx-pro", Namespace: "alice"},
		{Name: "xlsx-pro", Namespace: "bob"},
	}
	opts := &installOptions{SkillName: "xlsx-pro", IO: ios, repo: ghrepo.New("o", "r")}
	_, err := matchSkillByName(opts, skills)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ambiguous")
}

func TestMatchSkillByName_NamespacedExact(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	skills := []discovery.Skill{
		{Name: "xlsx-pro", Namespace: "alice"},
		{Name: "xlsx-pro", Namespace: "bob"},
	}
	opts := &installOptions{SkillName: "bob/xlsx-pro", IO: ios, repo: ghrepo.New("o", "r")}
	got, err := matchSkillByName(opts, skills)
	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, "bob", got[0].Namespace)
}

func TestFriendlyDir(t *testing.T) {
	// Test home directory path
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	got := friendlyDir(filepath.Join(home, ".github", "skills"))
	assert.True(t, strings.HasPrefix(got, "~"), "expected ~ prefix, got %q", got)
}

func TestResolveScope_ExplicitFlag(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	opts := &installOptions{
		IO:           ios,
		Scope:        "user",
		ScopeChanged: true,
		GitClient:    &git.Client{RepoDir: t.TempDir()},
	}
	scope, err := resolveScope(opts, true)
	require.NoError(t, err)
	assert.Equal(t, "user", string(scope))
}

func TestResolveScope_DirBypasses(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	opts := &installOptions{
		IO:        ios,
		Dir:       "/tmp/custom",
		Scope:     "project",
		GitClient: &git.Client{RepoDir: t.TempDir()},
	}
	scope, err := resolveScope(opts, true)
	require.NoError(t, err)
	assert.Equal(t, "project", string(scope))
}

func TestCheckOverwrite_NoExisting(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	targetDir := t.TempDir()
	skills := []discovery.Skill{{Name: "new-skill"}}
	host := &registry.AgentHost{ID: "test", ProjectDir: "skills"}
	opts := &installOptions{IO: ios, Dir: targetDir}

	got, err := checkOverwrite(opts, skills, host, registry.ScopeProject, "/tmp", "/home", false)
	require.NoError(t, err)
	assert.Len(t, got, 1)
}

func TestCheckOverwrite_ExistingWithForce(t *testing.T) {
	targetDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(targetDir, "existing-skill"), 0o755))

	ios, _, _, _ := iostreams.Test()
	skills := []discovery.Skill{{Name: "existing-skill"}}
	host := &registry.AgentHost{ID: "test", ProjectDir: "skills"}
	opts := &installOptions{IO: ios, Dir: targetDir, Force: true}

	got, err := checkOverwrite(opts, skills, host, registry.ScopeProject, "/tmp", "/home", false)
	require.NoError(t, err)
	assert.Len(t, got, 1)
}

func TestCheckOverwrite_ExistingNonInteractive(t *testing.T) {
	targetDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(targetDir, "existing-skill"), 0o755))

	ios, _, _, _ := iostreams.Test()
	skills := []discovery.Skill{{Name: "existing-skill"}}
	host := &registry.AgentHost{ID: "test", ProjectDir: "skills"}
	opts := &installOptions{IO: ios, Dir: targetDir}

	_, err := checkOverwrite(opts, skills, host, registry.ScopeProject, "/tmp", "/home", false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already installed")
}

func TestNewCmdInstall(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantOpts installOptions
		wantErr  bool
	}{
		{
			name:     "repo argument only",
			input:    "owner/repo",
			wantOpts: installOptions{SkillSource: "owner/repo", Scope: "project"},
		},
		{
			name:     "repo and skill",
			input:    "owner/repo my-skill",
			wantOpts: installOptions{SkillSource: "owner/repo", SkillName: "my-skill", Scope: "project"},
		},
		{
			name:     "with all flags",
			input:    "owner/repo my-skill --agent github-copilot --scope user --pin v1.0.0 --force",
			wantOpts: installOptions{SkillSource: "owner/repo", SkillName: "my-skill", Agent: "github-copilot", Scope: "user", Pin: "v1.0.0", Force: true},
		},
		{
			name:     "all flag",
			input:    "owner/repo --all",
			wantOpts: installOptions{SkillSource: "owner/repo", All: true, Scope: "project"},
		},
		{
			name:     "dir flag",
			input:    "owner/repo my-skill --dir /tmp/skills",
			wantOpts: installOptions{SkillSource: "owner/repo", SkillName: "my-skill", Dir: "/tmp/skills", Scope: "project"},
		},
		{
			name:    "too many args",
			input:   "a b c",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ios, _, _, _ := iostreams.Test()
			f := &cmdutil.Factory{
				IOStreams: ios,
				Prompter:  &prompter.PrompterMock{},
				GitClient: &git.Client{},
			}

			var gotOpts *installOptions
			cmd := NewCmdInstall(f, func(opts *installOptions) error {
				gotOpts = opts
				return nil
			})

			args, err := shlex.Split(tt.input)
			require.NoError(t, err)
			cmd.SetArgs(args)
			cmd.SetIn(&strings.Reader{})
			cmd.SetOut(&strings.Builder{})
			cmd.SetErr(&strings.Builder{})

			_, err = cmd.ExecuteC()
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantOpts.SkillSource, gotOpts.SkillSource)
			assert.Equal(t, tt.wantOpts.SkillName, gotOpts.SkillName)
			assert.Equal(t, tt.wantOpts.Agent, gotOpts.Agent)
			assert.Equal(t, tt.wantOpts.Scope, gotOpts.Scope)
			assert.Equal(t, tt.wantOpts.Pin, gotOpts.Pin)
			assert.Equal(t, tt.wantOpts.Dir, gotOpts.Dir)
			assert.Equal(t, tt.wantOpts.All, gotOpts.All)
			assert.Equal(t, tt.wantOpts.Force, gotOpts.Force)
		})
	}
}

func TestInstallRun_RemoteInstall(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	skillContent := "---\nname: test-skill\ndescription: A test\n---\n# Test\n"
	encodedContent := base64.StdEncoding.EncodeToString([]byte(skillContent))

	reg := &httpmock.Registry{}
	reg.Register(
		httpmock.REST("GET", "repos/owner/repo/releases/latest"),
		httpmock.StringResponse(`{"tag_name": "v1.0.0"}`),
	)
	reg.Register(
		httpmock.REST("GET", "repos/owner/repo/git/ref/tags/v1.0.0"),
		httpmock.StringResponse(`{"object": {"sha": "abc123", "type": "commit"}}`),
	)
	reg.Register(
		httpmock.REST("GET", "repos/owner/repo/git/trees/abc123"),
		httpmock.StringResponse(`{"sha": "abc123", "tree": [{"path": "skills/test-skill", "type": "tree", "sha": "treeSHA"}, {"path": "skills/test-skill/SKILL.md", "type": "blob", "sha": "blobSHA"}]}`),
	)
	reg.Register(
		httpmock.REST("GET", "repos/owner/repo/git/trees/treeSHA"),
		httpmock.StringResponse(`{"tree": [{"path": "SKILL.md", "type": "blob", "sha": "blobSHA", "size": 50}]}`),
	)
	reg.Register(
		httpmock.REST("GET", "repos/owner/repo/git/blobs/blobSHA"),
		httpmock.StringResponse(fmt.Sprintf(`{"sha": "blobSHA", "content": "%s", "encoding": "base64"}`, encodedContent)),
	)

	targetDir := t.TempDir()
	ios, _, stdout, _ := iostreams.Test()
	ios.SetStdoutTTY(true)
	ios.SetColorEnabled(false)

	opts := &installOptions{
		IO: ios,
		HttpClient: func() (*http.Client, error) {
			return &http.Client{Transport: reg}, nil
		},
		GitClient:   &git.Client{RepoDir: t.TempDir()},
		SkillSource: "owner/repo",
		SkillName:   "test-skill",
		Agent:       "github-copilot",
		Scope:       "project",
		Dir:         targetDir,
	}

	defer reg.Verify(t)
	err := installRun(opts)
	require.NoError(t, err)

	assert.Contains(t, stdout.String(), "Installed test-skill")

	installed, readErr := os.ReadFile(filepath.Join(targetDir, "test-skill", "SKILL.md"))
	require.NoError(t, readErr)
	assert.Contains(t, string(installed), "github-owner: owner")
	assert.Contains(t, string(installed), "github-repo: repo")
}

func TestPrintFileTree(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "my-skill")
	require.NoError(t, os.MkdirAll(filepath.Join(skillDir, "scripts"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# test"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "scripts", "run.sh"), []byte("#!/bin/bash"), 0o644))

	ios, _, stdout, _ := iostreams.Test()
	cs := ios.ColorScheme()

	printFileTree(stdout, cs, dir, []string{"my-skill"})

	out := stdout.String()
	assert.Contains(t, out, "my-skill/")
	assert.Contains(t, out, "SKILL.md")
	assert.Contains(t, out, "scripts/")
	assert.Contains(t, out, "run.sh")
}

func TestPrintFileTree_Empty(t *testing.T) {
	ios, _, stdout, _ := iostreams.Test()
	cs := ios.ColorScheme()

	printFileTree(stdout, cs, t.TempDir(), nil)
	assert.Empty(t, stdout.String())
}

func TestPrintTreeDir_Unreadable(t *testing.T) {
	ios, _, stdout, _ := iostreams.Test()
	cs := ios.ColorScheme()

	printTreeDir(stdout, cs, filepath.Join(t.TempDir(), "nonexistent"), "  ")
	assert.Contains(t, stdout.String(), "(could not read directory)")
}

func TestPrintReviewHint_Remote(t *testing.T) {
	ios, _, _, stderr := iostreams.Test()
	cs := ios.ColorScheme()

	printReviewHint(stderr, cs, "owner/repo", []string{"my-skill", "other-skill"})

	out := stderr.String()
	assert.Contains(t, out, "prompt injections or malicious scripts")
	assert.Contains(t, out, "gh skills preview owner/repo my-skill")
	assert.Contains(t, out, "gh skills preview owner/repo other-skill")
}

func TestPrintReviewHint_Local(t *testing.T) {
	ios, _, _, stderr := iostreams.Test()
	cs := ios.ColorScheme()

	printReviewHint(stderr, cs, "", []string{"my-skill"})

	out := stderr.String()
	assert.Contains(t, out, "prompt injections or malicious scripts")
	assert.Contains(t, out, "Review the installed files before use.")
	assert.NotContains(t, out, "gh skills preview")
}

func TestPrintReviewHint_Empty(t *testing.T) {
	ios, _, _, stderr := iostreams.Test()
	cs := ios.ColorScheme()

	printReviewHint(stderr, cs, "owner/repo", nil)
	assert.Empty(t, stderr.String())
}

func TestSelectSkills_AllWithNamespacedSkills(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	skills := []discovery.Skill{
		{Name: "xlsx-pro", Namespace: "alice", Convention: "skills-namespaced"},
		{Name: "xlsx-pro", Namespace: "bob", Convention: "skills-namespaced"},
		{Name: "other-skill", Convention: "skills"},
	}
	opts := &installOptions{All: true, IO: ios, repo: ghrepo.New("o", "r")}
	got, err := selectSkillsWithSelector(opts, skills, false, skillSelector{matchByName: matchSkillByName, sourceHint: "REPO"})
	require.NoError(t, err)
	assert.Len(t, got, 3)
}

func TestRunLocalInstall_NamespacedSkills(t *testing.T) {
	dir := t.TempDir()

	// Create two skills with the same name under different namespaces
	for _, ns := range []string{"alice", "bob"} {
		skillDir := filepath.Join(dir, "skills", ns, "xlsx-pro")
		require.NoError(t, os.MkdirAll(skillDir, 0o755))
		content := fmt.Sprintf("---\nname: xlsx-pro\ndescription: %s xlsx-pro\n---\n# Test\n", ns)
		require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644))
	}

	targetDir := t.TempDir()
	ios, _, stdout, _ := iostreams.Test()
	ios.SetStdoutTTY(false)
	ios.SetColorEnabled(false)

	opts := &installOptions{
		IO:          ios,
		SkillSource: dir,
		localPath:   dir,
		All:         true,
		Force:       true,
		Agent:       "github-copilot",
		Scope:       "project",
		Dir:         targetDir,
		GitClient:   &git.Client{RepoDir: t.TempDir()},
	}

	err := installRun(opts)
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "Installed alice/xlsx-pro")
	assert.Contains(t, out, "Installed bob/xlsx-pro")

	// Both should be installed in separate directories
	_, err = os.Stat(filepath.Join(targetDir, "alice", "xlsx-pro", "SKILL.md"))
	assert.NoError(t, err, "alice/xlsx-pro should be installed")
	_, err = os.Stat(filepath.Join(targetDir, "bob", "xlsx-pro", "SKILL.md"))
	assert.NoError(t, err, "bob/xlsx-pro should be installed")
}

func TestCheckOverwrite_NamespacedSkill(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	targetDir := t.TempDir()

	// Pre-create a namespaced skill directory
	require.NoError(t, os.MkdirAll(filepath.Join(targetDir, "alice", "xlsx-pro"), 0o755))

	skills := []discovery.Skill{
		{Name: "xlsx-pro", Namespace: "alice"},
		{Name: "xlsx-pro", Namespace: "bob"},
	}
	host := &registry.AgentHost{ID: "test", ProjectDir: "skills"}
	opts := &installOptions{IO: ios, Dir: targetDir, Force: true}

	got, err := checkOverwrite(opts, skills, host, registry.ScopeProject, "/tmp", "/home", false)
	require.NoError(t, err)
	assert.Len(t, got, 2, "both skills should be installable (force mode)")
}
