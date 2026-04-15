package publish

import (
	"bytes"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/MakeNowJust/heredoc"
	"github.com/cli/cli/v2/api"
	"github.com/cli/cli/v2/git"
	"github.com/cli/cli/v2/internal/prompter"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/httpmock"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/google/shlex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestGitClient(t *testing.T, remoteURLs map[string]string) *git.Client {
	t.Helper()
	dir := t.TempDir()
	initGitRepo(t, dir, remoteURLs)
	return &git.Client{RepoDir: dir}
}

// initGitRepo initializes a git repo in the given directory and adds remotes.
// Use this when the git repo must live in the same directory as the skill files.
func initGitRepo(t *testing.T, dir string, remoteURLs map[string]string) {
	t.Helper()
	runGitInDir(t, dir, "init", "--initial-branch=main")
	runGitInDir(t, dir, "config", "user.email", "monalisa@github.com")
	runGitInDir(t, dir, "config", "user.name", "Monalisa Octocat")
	for name, url := range remoteURLs {
		runGitInDir(t, dir, "remote", "add", name, url)
	}
}

// stubAllSecureRemote registers the standard stubs for a fully-configured remote
// repo (topics, tags, rulesets, security) so publishRun skips all remote warnings.
func stubAllSecureRemote(reg *httpmock.Registry, owner, repo string) {
	reg.Register(
		httpmock.REST("GET", "repos/"+owner+"/"+repo+"/topics"),
		httpmock.JSONResponse(map[string]interface{}{
			"names": []string{"agent-skills"},
		}),
	)
	reg.Register(
		httpmock.REST("GET", "repos/"+owner+"/"+repo+"/tags"),
		httpmock.JSONResponse([]map[string]interface{}{
			{"name": "v1.0.0"},
		}),
	)
	reg.Register(
		httpmock.REST("GET", "repos/"+owner+"/"+repo+"/rulesets"),
		httpmock.JSONResponse([]map[string]interface{}{
			{"id": 1, "name": "tags", "target": "tag", "enforcement": "active"},
		}),
	)
	reg.Register(
		httpmock.REST("GET", "repos/"+owner+"/"+repo),
		httpmock.JSONResponse(map[string]interface{}{
			"security_and_analysis": map[string]interface{}{
				"secret_scanning":                 map[string]interface{}{"status": "enabled"},
				"secret_scanning_push_protection": map[string]interface{}{"status": "enabled"},
			},
		}),
	)
}

func TestNewCmdPublish(t *testing.T) {
	tests := []struct {
		name      string
		cli       string
		wantsErr  bool
		wantsOpts PublishOptions
	}{
		{
			name: "all flags",
			cli:  "./monalisa-skills --dry-run --fix --tag v1.0.0",
			wantsOpts: PublishOptions{
				Dir:    "./monalisa-skills",
				DryRun: true,
				Fix:    true,
				Tag:    "v1.0.0",
			},
		},
		{
			name: "directory only",
			cli:  "./octocat-repo",
			wantsOpts: PublishOptions{
				Dir: "./octocat-repo",
			},
		},
		{
			name:      "no args leaves dir empty",
			cli:       "",
			wantsOpts: PublishOptions{},
		},
		{
			name: "dry-run flag only",
			cli:  "--dry-run",
			wantsOpts: PublishOptions{
				DryRun: true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ios, _, _, _ := iostreams.Test()
			f := cmdutil.Factory{IOStreams: ios}

			var gotOpts *PublishOptions
			cmd := NewCmdPublish(&f, func(opts *PublishOptions) error {
				gotOpts = opts
				return nil
			})

			args, err := shlex.Split(tt.cli)
			require.NoError(t, err)
			cmd.SetArgs(args)
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})

			err = cmd.Execute()
			if tt.wantsErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, gotOpts)
			assert.Equal(t, tt.wantsOpts.Dir, gotOpts.Dir)
			assert.Equal(t, tt.wantsOpts.DryRun, gotOpts.DryRun)
			assert.Equal(t, tt.wantsOpts.Fix, gotOpts.Fix)
			assert.Equal(t, tt.wantsOpts.Tag, gotOpts.Tag)
		})
	}
}

func TestPublishRun_UnsupportedHost(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "test-skill", heredoc.Doc(`
		---
		name: test-skill
		description: A test skill
		---
		Body.
	`))

	ios, _, _, _ := iostreams.Test()
	initGitRepo(t, dir, map[string]string{"origin": "https://github.com/monalisa/skills-repo.git"})
	err := publishRun(&PublishOptions{
		IO:        ios,
		Dir:       dir,
		GitClient: &git.Client{},
		client:    api.NewClientFromHTTP(&http.Client{}),
		host:      "acme.ghes.com",
	})
	require.ErrorContains(t, err, "supports only github.com")
}

func TestPublishRun(t *testing.T) {
	tests := []struct {
		name       string
		isTTY      bool
		setup      func(t *testing.T, dir string)
		stubs      func(*httpmock.Registry)
		opts       func(ios *iostreams.IOStreams, dir string, reg *httpmock.Registry) *PublishOptions
		verify     func(t *testing.T, dir string)
		wantErr    string
		wantStdout string
		wantStderr string
	}{
		{
			name:  "no skills directory",
			setup: func(_ *testing.T, _ string) {},
			opts: func(ios *iostreams.IOStreams, dir string, _ *httpmock.Registry) *PublishOptions {
				t.Helper()
				return &PublishOptions{IO: ios, Dir: dir}
			},
			wantErr: "no skills/ directory",
		},
		{
			name: "missing SKILL.md",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(dir, "skills", "empty-skill"), 0o755))
			},
			opts: func(ios *iostreams.IOStreams, dir string, _ *httpmock.Registry) *PublishOptions {
				t.Helper()
				return &PublishOptions{IO: ios, Dir: dir}
			},
			wantErr:    "validation failed",
			wantStdout: "missing SKILL.md",
		},
		{
			name: "missing name in frontmatter",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeSkill(t, dir, "git-commit", heredoc.Doc(`
					---
					description: A skill for writing good git commits
					---
					Body text.
				`))
			},
			opts: func(ios *iostreams.IOStreams, dir string, _ *httpmock.Registry) *PublishOptions {
				t.Helper()
				return &PublishOptions{IO: ios, Dir: dir}
			},
			wantErr:    "validation failed",
			wantStdout: "missing required field: name",
		},
		{
			name: "name does not match directory",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeSkill(t, dir, "git-commit", heredoc.Doc(`
					---
					name: wrong-name
					description: A skill
					---
					Body.
				`))
			},
			opts: func(ios *iostreams.IOStreams, dir string, _ *httpmock.Registry) *PublishOptions {
				t.Helper()
				return &PublishOptions{IO: ios, Dir: dir}
			},
			wantErr:    "validation failed",
			wantStdout: "does not match directory name",
		},
		{
			name: "non-spec-compliant name",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeSkill(t, dir, "My_Skill", heredoc.Doc(`
					---
					name: My_Skill
					description: A skill with non-compliant name
					---
					Body.
				`))
			},
			opts: func(ios *iostreams.IOStreams, dir string, _ *httpmock.Registry) *PublishOptions {
				t.Helper()
				return &PublishOptions{IO: ios, Dir: dir}
			},
			wantErr:    "validation failed",
			wantStdout: "naming convention",
		},
		{
			name:  "valid skill dry-run passes validation",
			isTTY: true,
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeSkill(t, dir, "good-skill", heredoc.Doc(`
					---
					name: good-skill
					description: A good skill
					license: MIT
					---
					Body.
				`))
			},
			stubs: func(reg *httpmock.Registry) {
				stubAllSecureRemote(reg, "monalisa", "skills-repo")
			},
			opts: func(ios *iostreams.IOStreams, dir string, reg *httpmock.Registry) *PublishOptions {
				t.Helper()
				initGitRepo(t, dir, map[string]string{
					"origin": "https://github.com/monalisa/skills-repo.git",
				})
				return &PublishOptions{
					IO:        ios,
					Dir:       dir,
					DryRun:    true,
					GitClient: &git.Client{},
					client:    api.NewClientFromHTTP(&http.Client{Transport: reg}),
					host:      "github.com",
				}
			},
			wantStdout: "1 skill(s) validated successfully",
			wantStderr: "Dry run complete",
		},
		{
			name:  "valid skill with --tag publishes release",
			isTTY: true,
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeSkill(t, dir, "git-commit", heredoc.Doc(`
					---
					name: git-commit
					description: A skill for writing good git commits
					allowed-tools: git
					license: MIT
					---
					You are a git commit expert.
				`))
			},
			stubs: func(reg *httpmock.Registry) {
				stubAllSecureRemote(reg, "monalisa", "skills-repo")
				// topic already present, so no PUT needed
				// immutable releases check
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/skills-repo/immutable-releases"),
					httpmock.JSONResponse(map[string]interface{}{"enabled": true}),
				)
				// default branch for branch comparison
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/skills-repo"),
					httpmock.JSONResponse(map[string]interface{}{"default_branch": "main"}),
				)
				// create release
				reg.Register(
					httpmock.REST("POST", "repos/monalisa/skills-repo/releases"),
					httpmock.JSONResponse(map[string]interface{}{
						"html_url": "https://github.com/monalisa/skills-repo/releases/tag/v1.0.1",
					}),
				)
			},
			opts: func(ios *iostreams.IOStreams, dir string, reg *httpmock.Registry) *PublishOptions {
				t.Helper()
				initGitRepo(t, dir, map[string]string{
					"origin": "https://github.com/monalisa/skills-repo.git",
				})
				return &PublishOptions{
					IO:  ios,
					Dir: dir,
					Tag: "v1.0.1",
					Prompter: &prompter.PrompterMock{
						ConfirmFunc: func(msg string, def bool) (bool, error) { return true, nil },
					},
					GitClient: &git.Client{},
					client:    api.NewClientFromHTTP(&http.Client{Transport: reg}),
					host:      "github.com",
				}
			},
			wantStdout: "Published v1.0.1",
		},
		{
			name: "strip metadata with --fix",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeSkill(t, dir, "test-skill", heredoc.Doc(`
					---
					name: test-skill
					description: A test skill
					metadata:
					    github-owner: someone
					    github-repo: something
					    github-ref: v1.0.0
					    github-sha: abc123
					    github-tree-sha: def456
					---
					Body.
				`))
			},
			opts: func(ios *iostreams.IOStreams, dir string, _ *httpmock.Registry) *PublishOptions {
				t.Helper()
				return &PublishOptions{IO: ios, Dir: dir, Fix: true}
			},
			wantStdout: "stripped install metadata",
			verify: func(t *testing.T, dir string) {
				t.Helper()
				fixed, err := os.ReadFile(filepath.Join(dir, "skills", "test-skill", "SKILL.md"))
				require.NoError(t, err)
				fixedStr := string(fixed)
				assert.NotContains(t, fixedStr, "github-owner")
				assert.NotContains(t, fixedStr, "github-sha")
				assert.NotContains(t, fixedStr, "metadata:")
			},
		},
		{
			name: "metadata without --fix errors with hint",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeSkill(t, dir, "test-skill", heredoc.Doc(`
					---
					name: test-skill
					description: A test skill
					metadata:
					    github-owner: someone
					    github-sha: abc123
					---
					Body.
				`))
			},
			opts: func(ios *iostreams.IOStreams, dir string, _ *httpmock.Registry) *PublishOptions {
				t.Helper()
				return &PublishOptions{IO: ios, Dir: dir, Fix: false}
			},
			wantErr:    "validation failed",
			wantStdout: "--fix",
		},
		{
			name: "missing license warning",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeSkill(t, dir, "no-license", heredoc.Doc(`
					---
					name: no-license
					description: A skill without license
					---
					Body.
				`))
			},
			opts: func(ios *iostreams.IOStreams, dir string, _ *httpmock.Registry) *PublishOptions {
				t.Helper()
				return &PublishOptions{IO: ios, Dir: dir}
			},
			wantStdout: "license",
		},
		{
			name: "allowed-tools array error",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeSkill(t, dir, "bad-tools", heredoc.Doc(`
					---
					name: bad-tools
					description: A skill with array allowed-tools
					allowed-tools:
					  - git
					  - curl
					---
					Body.
				`))
			},
			opts: func(ios *iostreams.IOStreams, dir string, _ *httpmock.Registry) *PublishOptions {
				t.Helper()
				return &PublishOptions{IO: ios, Dir: dir}
			},
			wantErr:    "validation failed",
			wantStdout: "allowed-tools must be a string",
		},
		{
			name: "security warnings when features disabled",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeSkill(t, dir, "my-skill", heredoc.Doc(`
					---
					name: my-skill
					description: A skill
					license: MIT
					---
					Body.
				`))
			},
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/octocat/secure-repo/topics"),
					httpmock.JSONResponse(map[string]interface{}{
						"names": []string{"agent-skills"},
					}),
				)
				reg.Register(
					httpmock.REST("GET", "repos/octocat/secure-repo/tags"),
					httpmock.JSONResponse([]interface{}{}),
				)
				reg.Register(
					httpmock.REST("GET", "repos/octocat/secure-repo/rulesets"),
					httpmock.JSONResponse([]map[string]interface{}{
						{"id": 1, "name": "branch-only", "target": "branch", "enforcement": "active"},
					}),
				)
				reg.Register(
					httpmock.REST("GET", "repos/octocat/secure-repo"),
					httpmock.JSONResponse(map[string]interface{}{
						"security_and_analysis": map[string]interface{}{
							"secret_scanning":                 map[string]interface{}{"status": "disabled"},
							"secret_scanning_push_protection": map[string]interface{}{"status": "disabled"},
						},
					}),
				)
			},
			opts: func(ios *iostreams.IOStreams, dir string, reg *httpmock.Registry) *PublishOptions {
				t.Helper()
				initGitRepo(t, dir, map[string]string{
					"origin": "https://github.com/octocat/secure-repo.git",
				})
				return &PublishOptions{
					IO:        ios,
					Dir:       dir,
					GitClient: &git.Client{},
					client:    api.NewClientFromHTTP(&http.Client{Transport: reg}),
					host:      "github.com",
				}
			},
			wantStdout: "secret scanning is not enabled",
		},
		{
			name: "tag protection warning",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeSkill(t, dir, "my-skill", heredoc.Doc(`
					---
					name: my-skill
					description: A skill
					license: MIT
					---
					Body.
				`))
			},
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/octocat/tag-repo/topics"),
					httpmock.JSONResponse(map[string]interface{}{
						"names": []string{"agent-skills"},
					}),
				)
				reg.Register(
					httpmock.REST("GET", "repos/octocat/tag-repo/tags"),
					httpmock.JSONResponse([]interface{}{}),
				)
				reg.Register(
					httpmock.REST("GET", "repos/octocat/tag-repo/rulesets"),
					httpmock.JSONResponse([]interface{}{}),
				)
				reg.Register(
					httpmock.REST("GET", "repos/octocat/tag-repo"),
					httpmock.JSONResponse(map[string]interface{}{
						"security_and_analysis": map[string]interface{}{
							"secret_scanning":                 map[string]interface{}{"status": "enabled"},
							"secret_scanning_push_protection": map[string]interface{}{"status": "enabled"},
						},
					}),
				)
			},
			opts: func(ios *iostreams.IOStreams, dir string, reg *httpmock.Registry) *PublishOptions {
				t.Helper()
				initGitRepo(t, dir, map[string]string{
					"origin": "https://github.com/octocat/tag-repo.git",
				})
				return &PublishOptions{
					IO:        ios,
					Dir:       dir,
					GitClient: &git.Client{},
					client:    api.NewClientFromHTTP(&http.Client{Transport: reg}),
					host:      "github.com",
				}
			},
			wantStdout: "tag protection",
		},
		{
			name:  "code files trigger code scanning info",
			isTTY: true,
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeSkill(t, dir, "code-skill", heredoc.Doc(`
					---
					name: code-skill
					description: A skill with code
					license: MIT
					---
					Body.
				`))
				scriptDir := filepath.Join(dir, "skills", "code-skill", "scripts")
				require.NoError(t, os.MkdirAll(scriptDir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(scriptDir, "helper.sh"), []byte("#!/bin/bash"), 0o644))
			},
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/octocat/code-repo/topics"),
					httpmock.JSONResponse(map[string]interface{}{
						"names": []string{"agent-skills"},
					}),
				)
				reg.Register(
					httpmock.REST("GET", "repos/octocat/code-repo/tags"),
					httpmock.JSONResponse([]interface{}{}),
				)
				reg.Register(
					httpmock.REST("GET", "repos/octocat/code-repo/rulesets"),
					httpmock.JSONResponse([]map[string]interface{}{
						{"id": 1, "name": "tags", "target": "tag", "enforcement": "active"},
					}),
				)
				reg.Register(
					httpmock.REST("GET", "repos/octocat/code-repo"),
					httpmock.JSONResponse(map[string]interface{}{
						"security_and_analysis": map[string]interface{}{
							"secret_scanning":                 map[string]interface{}{"status": "enabled"},
							"secret_scanning_push_protection": map[string]interface{}{"status": "enabled"},
						},
					}),
				)
				reg.Register(
					httpmock.REST("GET", "repos/octocat/code-repo/code-scanning/alerts"),
					httpmock.StatusStringResponse(404, "not found"),
				)
			},
			opts: func(ios *iostreams.IOStreams, dir string, reg *httpmock.Registry) *PublishOptions {
				t.Helper()
				initGitRepo(t, dir, map[string]string{
					"origin": "https://github.com/octocat/code-repo.git",
				})
				return &PublishOptions{
					IO:        ios,
					Dir:       dir,
					DryRun:    true,
					GitClient: &git.Client{},
					client:    api.NewClientFromHTTP(&http.Client{Transport: reg}),
					host:      "github.com",
				}
			},
			wantStderr: "code scanning",
		},
		{
			name:  "manifest files trigger dependabot info",
			isTTY: true,
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeSkill(t, dir, "dep-skill", heredoc.Doc(`
					---
					name: dep-skill
					description: A skill with manifests
					license: MIT
					---
					Body.
				`))
				require.NoError(t, os.WriteFile(
					filepath.Join(dir, "skills", "dep-skill", "package.json"),
					[]byte("{}"), 0o644,
				))
			},
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/octocat/dep-repo/topics"),
					httpmock.JSONResponse(map[string]interface{}{
						"names": []string{"agent-skills"},
					}),
				)
				reg.Register(
					httpmock.REST("GET", "repos/octocat/dep-repo/tags"),
					httpmock.JSONResponse([]interface{}{}),
				)
				reg.Register(
					httpmock.REST("GET", "repos/octocat/dep-repo/rulesets"),
					httpmock.JSONResponse([]map[string]interface{}{
						{"id": 1, "name": "tags", "target": "tag", "enforcement": "active"},
					}),
				)
				reg.Register(
					httpmock.REST("GET", "repos/octocat/dep-repo"),
					httpmock.JSONResponse(map[string]interface{}{
						"security_and_analysis": map[string]interface{}{
							"secret_scanning":                 map[string]interface{}{"status": "enabled"},
							"secret_scanning_push_protection": map[string]interface{}{"status": "enabled"},
						},
					}),
				)
				reg.Register(
					httpmock.REST("GET", "repos/octocat/dep-repo/vulnerability-alerts"),
					httpmock.StatusStringResponse(404, "not found"),
				)
			},
			opts: func(ios *iostreams.IOStreams, dir string, reg *httpmock.Registry) *PublishOptions {
				t.Helper()
				initGitRepo(t, dir, map[string]string{
					"origin": "https://github.com/octocat/dep-repo.git",
				})
				return &PublishOptions{
					IO:        ios,
					Dir:       dir,
					DryRun:    true,
					GitClient: &git.Client{},
					client:    api.NewClientFromHTTP(&http.Client{Transport: reg}),
					host:      "github.com",
				}
			},
			wantStderr: "Dependabot",
		},
		{
			name:  "installed skill dirs not gitignored warns",
			isTTY: true,
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeSkill(t, dir, "my-skill", heredoc.Doc(`
					---
					name: my-skill
					description: A skill
					license: MIT
					---
					Body.
				`))
			},
			opts: func(ios *iostreams.IOStreams, dir string, _ *httpmock.Registry) *PublishOptions {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(dir, ".agents", "skills", "installed"), 0o755))
				runGitInDir(t, dir, "init", "--initial-branch=main")
				runGitInDir(t, dir, "config", "user.email", "monalisa@github.com")
				runGitInDir(t, dir, "config", "user.name", "Monalisa Octocat")

				return &PublishOptions{
					IO:        ios,
					Dir:       dir,
					GitClient: &git.Client{RepoDir: dir},
				}
			},
			wantStdout: ".gitignore",
		},
		{
			name: "installed skill dirs gitignored no warning",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeSkill(t, dir, "my-skill", heredoc.Doc(`
					---
					name: my-skill
					description: A skill
					license: MIT
					---
					Body.
				`))
				require.NoError(t, os.MkdirAll(filepath.Join(dir, ".agents", "skills", "installed"), 0o755))

				runGitInDir(t, dir, "init", "--initial-branch=main")
				runGitInDir(t, dir, "config", "user.email", "monalisa@github.com")
				runGitInDir(t, dir, "config", "user.name", "Monalisa Octocat")
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".agents/skills\n"), 0o644))
				runGitInDir(t, dir, "add", ".gitignore")
				runGitInDir(t, dir, "commit", "-m", "init")
			},
			opts: func(ios *iostreams.IOStreams, dir string, _ *httpmock.Registry) *PublishOptions {
				t.Helper()
				return &PublishOptions{
					IO:        ios,
					Dir:       dir,
					GitClient: &git.Client{RepoDir: dir},
				}
			},
			wantStdout: "no git remote",
			verify: func(t *testing.T, dir string) {
				t.Helper()
				// The key assertion: .gitignored dirs should NOT produce a warning
			},
		},
		{
			name: "installed skill dirs git error warns about unverified status",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeSkill(t, dir, "my-skill", heredoc.Doc(`
					---
					name: my-skill
					description: A skill
					license: MIT
					---
					Body.
				`))
				// Create install dir but do NOT init git so check-ignore will fail
				require.NoError(t, os.MkdirAll(filepath.Join(dir, ".agents", "skills", "installed"), 0o755))
			},
			opts: func(ios *iostreams.IOStreams, dir string, _ *httpmock.Registry) *PublishOptions {
				t.Helper()
				return &PublishOptions{
					IO:        ios,
					Dir:       dir,
					GitClient: &git.Client{RepoDir: dir},
				}
			},
			wantStdout: "may contain installed skills that are not gitignored",
		},
		{
			name: "no GitHub remote warns",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeSkill(t, dir, "my-skill", heredoc.Doc(`
					---
					name: my-skill
					description: A skill
					license: MIT
					---
					Body.
				`))
				runGitInDir(t, dir, "init", "--initial-branch=main")
				runGitInDir(t, dir, "config", "user.email", "monalisa@github.com")
				runGitInDir(t, dir, "config", "user.name", "Monalisa Octocat")
				runGitInDir(t, dir, "remote", "add", "origin", "https://gitlab.com/hubot/bar.git")
			},
			opts: func(ios *iostreams.IOStreams, dir string, _ *httpmock.Registry) *PublishOptions {
				t.Helper()
				return &PublishOptions{
					IO:        ios,
					Dir:       dir,
					GitClient: &git.Client{RepoDir: dir},
				}
			},
			wantStdout: "not a GitHub repository",
		},
		{
			name:  "fallback remote detection uses non-origin GitHub remote",
			isTTY: true,
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeSkill(t, dir, "my-skill", heredoc.Doc(`
					---
					name: my-skill
					description: A skill
					license: MIT
					---
					Body.
				`))
			},
			stubs: func(reg *httpmock.Registry) {
				stubAllSecureRemote(reg, "octocat", "repo")
			},
			opts: func(ios *iostreams.IOStreams, dir string, reg *httpmock.Registry) *PublishOptions {
				t.Helper()
				initGitRepo(t, dir, map[string]string{
					"origin":   "https://gitlab.com/hubot/bar.git",
					"upstream": "git@github.com:octocat/repo.git",
				})
				return &PublishOptions{
					IO:        ios,
					Dir:       dir,
					DryRun:    true,
					GitClient: &git.Client{},
					client:    api.NewClientFromHTTP(&http.Client{Transport: reg}),
					host:      "github.com",
				}
			},
			wantStderr: "octocat/repo",
		},
		{
			name:  "publish adds missing topic via --tag",
			isTTY: true,
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeSkill(t, dir, "my-skill", heredoc.Doc(`
					---
					name: my-skill
					description: A skill
					license: MIT
					---
					Body.
				`))
			},
			stubs: func(reg *httpmock.Registry) {
				// topic missing
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/skills-repo/topics"),
					httpmock.JSONResponse(map[string]interface{}{
						"names": []string{"golang"},
					}),
				)
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/skills-repo/tags"),
					httpmock.JSONResponse([]interface{}{}),
				)
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/skills-repo/rulesets"),
					httpmock.JSONResponse([]map[string]interface{}{
						{"id": 1, "name": "tags", "target": "tag", "enforcement": "active"},
					}),
				)
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/skills-repo"),
					httpmock.JSONResponse(map[string]interface{}{
						"security_and_analysis": map[string]interface{}{
							"secret_scanning":                 map[string]interface{}{"status": "enabled"},
							"secret_scanning_push_protection": map[string]interface{}{"status": "enabled"},
						},
					}),
				)
				// addAgentSkillsTopic fetches topics again then PUTs
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/skills-repo/topics"),
					httpmock.JSONResponse(map[string]interface{}{
						"names": []string{"golang"},
					}),
				)
				reg.Register(
					httpmock.REST("PUT", "repos/monalisa/skills-repo/topics"),
					httpmock.JSONResponse(map[string]interface{}{}),
				)
				// immutable releases
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/skills-repo/immutable-releases"),
					httpmock.JSONResponse(map[string]interface{}{"enabled": true}),
				)
				// default branch
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/skills-repo"),
					httpmock.JSONResponse(map[string]interface{}{"default_branch": "main"}),
				)
				// create release
				reg.Register(
					httpmock.REST("POST", "repos/monalisa/skills-repo/releases"),
					httpmock.JSONResponse(map[string]interface{}{
						"html_url": "https://github.com/monalisa/skills-repo/releases/tag/v1.0.0",
					}),
				)
			},
			opts: func(ios *iostreams.IOStreams, dir string, reg *httpmock.Registry) *PublishOptions {
				t.Helper()
				initGitRepo(t, dir, map[string]string{
					"origin": "https://github.com/monalisa/skills-repo.git",
				})
				return &PublishOptions{
					IO:  ios,
					Dir: dir,
					Tag: "v1.0.0",
					Prompter: &prompter.PrompterMock{
						ConfirmFunc: func(msg string, def bool) (bool, error) { return true, nil },
					},
					GitClient: &git.Client{},
					client:    api.NewClientFromHTTP(&http.Client{Transport: reg}),
					host:      "github.com",
				}
			},
			wantStdout: "Added \"agent-skills\" topic",
		},
		{
			name: "tag suggestion uses existing tags",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeSkill(t, dir, "my-skill", heredoc.Doc(`
					---
					name: my-skill
					description: A skill
					license: MIT
					---
					Body.
				`))
			},
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/skills-repo/topics"),
					httpmock.JSONResponse(map[string]interface{}{
						"names": []string{"agent-skills"},
					}),
				)
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/skills-repo/tags"),
					httpmock.JSONResponse([]map[string]interface{}{
						{"name": "v2.3.4"},
					}),
				)
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/skills-repo/rulesets"),
					httpmock.JSONResponse([]map[string]interface{}{
						{"id": 1, "name": "tags", "target": "tag", "enforcement": "active"},
					}),
				)
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/skills-repo"),
					httpmock.JSONResponse(map[string]interface{}{
						"security_and_analysis": map[string]interface{}{
							"secret_scanning":                 map[string]interface{}{"status": "enabled"},
							"secret_scanning_push_protection": map[string]interface{}{"status": "enabled"},
						},
					}),
				)
				// immutable releases
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/skills-repo/immutable-releases"),
					httpmock.JSONResponse(map[string]interface{}{"enabled": true}),
				)
				// default branch
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/skills-repo"),
					httpmock.JSONResponse(map[string]interface{}{"default_branch": "main"}),
				)
				// create release with the suggested v2.3.5 tag
				reg.Register(
					httpmock.REST("POST", "repos/monalisa/skills-repo/releases"),
					httpmock.JSONResponse(map[string]interface{}{
						"html_url": "https://github.com/monalisa/skills-repo/releases/tag/v2.3.5",
					}),
				)
			},
			opts: func(ios *iostreams.IOStreams, dir string, reg *httpmock.Registry) *PublishOptions {
				t.Helper()
				initGitRepo(t, dir, map[string]string{
					"origin": "https://github.com/monalisa/skills-repo.git",
				})
				return &PublishOptions{
					IO:        ios,
					Dir:       dir,
					Tag:       "v2.3.5",
					GitClient: &git.Client{},
					client:    api.NewClientFromHTTP(&http.Client{Transport: reg}),
					host:      "github.com",
				}
			},
			wantStdout: "Published v2.3.5",
		},
		{
			name: "duplicate tag errors",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeSkill(t, dir, "my-skill", heredoc.Doc(`
					---
					name: my-skill
					description: A skill
					license: MIT
					---
					Body.
				`))
			},
			stubs: func(reg *httpmock.Registry) {
				stubAllSecureRemote(reg, "monalisa", "skills-repo")
			},
			opts: func(ios *iostreams.IOStreams, dir string, reg *httpmock.Registry) *PublishOptions {
				t.Helper()
				initGitRepo(t, dir, map[string]string{
					"origin": "https://github.com/monalisa/skills-repo.git",
				})
				return &PublishOptions{
					IO:        ios,
					Dir:       dir,
					Tag:       "v1.0.0", // same as stubAllSecureRemote's existing tag
					GitClient: &git.Client{},
					client:    api.NewClientFromHTTP(&http.Client{Transport: reg}),
					host:      "github.com",
				}
			},
			wantErr: "tag v1.0.0 already exists",
		},
		{
			name: "valid skill non-tty plain output",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeSkill(t, dir, "git-commit", heredoc.Doc(`
					---
					name: git-commit
					description: A skill for writing good git commits
					allowed-tools: git
					license: MIT
					---
					You are a git commit expert.
				`))
			},
			stubs: func(reg *httpmock.Registry) {
				stubAllSecureRemote(reg, "monalisa", "skills-repo")
			},
			opts: func(ios *iostreams.IOStreams, dir string, reg *httpmock.Registry) *PublishOptions {
				t.Helper()
				initGitRepo(t, dir, map[string]string{
					"origin": "https://github.com/monalisa/skills-repo.git",
				})
				return &PublishOptions{
					IO:        ios,
					Dir:       dir,
					GitClient: &git.Client{},
					client:    api.NewClientFromHTTP(&http.Client{Transport: reg}),
					host:      "github.com",
				}
			},
			wantStdout: "ok",
		},
		{
			name: "no remote and non-tty shows validation passed message",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeSkill(t, dir, "my-skill", heredoc.Doc(`
					---
					name: my-skill
					description: A skill
					license: MIT
					---
					Body.
				`))
			},
			opts: func(ios *iostreams.IOStreams, dir string, _ *httpmock.Registry) *PublishOptions {
				t.Helper()
				return &PublishOptions{
					IO:  ios,
					Dir: dir,
				}
			},
			wantStdout: "ok",
		},
		{
			name:  "interactive publish with topic and semver tag",
			isTTY: true,
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeSkill(t, dir, "my-skill", heredoc.Doc(`
					---
					name: my-skill
					description: A skill
					license: MIT
					---
					Body.
				`))
			},
			stubs: func(reg *httpmock.Registry) {
				// No topic yet, first GET for diagnostic check
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/skills-repo/topics"),
					httpmock.JSONResponse(map[string]interface{}{"names": []string{}}),
				)
				// Second GET inside addAgentSkillsTopic
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/skills-repo/topics"),
					httpmock.JSONResponse(map[string]interface{}{"names": []string{}}),
				)
				// Add topic
				reg.Register(
					httpmock.REST("PUT", "repos/monalisa/skills-repo/topics"),
					httpmock.StringResponse("{}"),
				)
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/skills-repo/tags"),
					httpmock.JSONResponse([]map[string]interface{}{}),
				)
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/skills-repo/rulesets"),
					httpmock.JSONResponse([]map[string]interface{}{}),
				)
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/skills-repo"),
					httpmock.JSONResponse(map[string]interface{}{
						"default_branch": "main",
						"security_and_analysis": map[string]interface{}{
							"secret_scanning":                 map[string]interface{}{"status": "enabled"},
							"secret_scanning_push_protection": map[string]interface{}{"status": "enabled"},
						},
					}),
				)
				// Immutable releases already enabled
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/skills-repo/immutable-releases"),
					httpmock.JSONResponse(map[string]interface{}{"enabled": true}),
				)
				// Create release
				reg.Register(
					httpmock.REST("POST", "repos/monalisa/skills-repo/releases"),
					httpmock.JSONResponse(map[string]interface{}{
						"html_url": "https://github.com/monalisa/skills-repo/releases/tag/v1.0.0",
					}),
				)
			},
			opts: func(ios *iostreams.IOStreams, dir string, reg *httpmock.Registry) *PublishOptions {
				t.Helper()
				confirmCall := 0
				initGitRepo(t, dir, map[string]string{
					"origin": "https://github.com/monalisa/skills-repo.git",
				})
				return &PublishOptions{
					IO:  ios,
					Dir: dir,
					Prompter: &prompter.PrompterMock{
						ConfirmFunc: func(msg string, def bool) (bool, error) {
							confirmCall++
							return true, nil // accept topic + final confirm
						},
						SelectFunc: func(msg string, def string, opts []string) (int, error) {
							return 0, nil // semver strategy
						},
						InputFunc: func(msg string, def string) (string, error) {
							return "v1.0.0", nil // accept suggested tag
						},
					},
					GitClient: &git.Client{},
					client:    api.NewClientFromHTTP(&http.Client{Transport: reg}),
					host:      "github.com",
				}
			},
			wantStdout: "Published v1.0.0",
		},
		{
			name:  "interactive publish with custom tag",
			isTTY: true,
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeSkill(t, dir, "my-skill", heredoc.Doc(`
					---
					name: my-skill
					description: A skill
					license: MIT
					---
					Body.
				`))
			},
			stubs: func(reg *httpmock.Registry) {
				stubAllSecureRemote(reg, "monalisa", "skills-repo")
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/skills-repo/immutable-releases"),
					httpmock.JSONResponse(map[string]interface{}{"enabled": true}),
				)
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/skills-repo"),
					httpmock.JSONResponse(map[string]interface{}{"default_branch": "main"}),
				)
				reg.Register(
					httpmock.REST("POST", "repos/monalisa/skills-repo/releases"),
					httpmock.JSONResponse(map[string]interface{}{
						"html_url": "https://github.com/monalisa/skills-repo/releases/tag/beta-1",
					}),
				)
			},
			opts: func(ios *iostreams.IOStreams, dir string, reg *httpmock.Registry) *PublishOptions {
				t.Helper()
				initGitRepo(t, dir, map[string]string{
					"origin": "https://github.com/monalisa/skills-repo.git",
				})
				return &PublishOptions{
					IO:  ios,
					Dir: dir,
					Prompter: &prompter.PrompterMock{
						ConfirmFunc: func(msg string, def bool) (bool, error) {
							return true, nil
						},
						SelectFunc: func(msg string, def string, opts []string) (int, error) {
							return 1, nil // custom tag strategy
						},
						InputFunc: func(msg string, def string) (string, error) {
							return "beta-1", nil
						},
					},
					GitClient: &git.Client{},
					client:    api.NewClientFromHTTP(&http.Client{Transport: reg}),
					host:      "github.com",
				}
			},
			wantStdout: "Published beta-1",
		},
		{
			name:  "interactive publish declined at final confirm",
			isTTY: true,
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeSkill(t, dir, "my-skill", heredoc.Doc(`
					---
					name: my-skill
					description: A skill
					license: MIT
					---
					Body.
				`))
			},
			stubs: func(reg *httpmock.Registry) {
				stubAllSecureRemote(reg, "monalisa", "skills-repo")
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/skills-repo/immutable-releases"),
					httpmock.JSONResponse(map[string]interface{}{"enabled": true}),
				)
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/skills-repo"),
					httpmock.JSONResponse(map[string]interface{}{"default_branch": "main"}),
				)
			},
			opts: func(ios *iostreams.IOStreams, dir string, reg *httpmock.Registry) *PublishOptions {
				t.Helper()
				confirmCall := 0
				initGitRepo(t, dir, map[string]string{
					"origin": "https://github.com/monalisa/skills-repo.git",
				})
				return &PublishOptions{
					IO:  ios,
					Dir: dir,
					Prompter: &prompter.PrompterMock{
						ConfirmFunc: func(msg string, def bool) (bool, error) {
							confirmCall++
							if confirmCall >= 1 {
								return false, nil // decline final confirm
							}
							return true, nil
						},
						SelectFunc: func(msg string, def string, opts []string) (int, error) {
							return 0, nil
						},
						InputFunc: func(msg string, def string) (string, error) {
							return "v1.0.1", nil
						},
					},
					GitClient: &git.Client{},
					client:    api.NewClientFromHTTP(&http.Client{Transport: reg}),
					host:      "github.com",
				}
			},
			wantErr:    "CancelError",
			wantStderr: "Publish cancelled",
		},
		{
			name:  "interactive immutable releases prompt",
			isTTY: true,
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeSkill(t, dir, "my-skill", heredoc.Doc(`
					---
					name: my-skill
					description: A skill
					license: MIT
					---
					Body.
				`))
			},
			stubs: func(reg *httpmock.Registry) {
				stubAllSecureRemote(reg, "monalisa", "skills-repo")
				// Immutable releases NOT enabled
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/skills-repo/immutable-releases"),
					httpmock.JSONResponse(map[string]interface{}{"enabled": false}),
				)
				// Enable immutable releases
				reg.Register(
					httpmock.REST("PATCH", "repos/monalisa/skills-repo/immutable-releases"),
					httpmock.StringResponse("{}"),
				)
				reg.Register(
					httpmock.REST("GET", "repos/monalisa/skills-repo"),
					httpmock.JSONResponse(map[string]interface{}{"default_branch": "main"}),
				)
				reg.Register(
					httpmock.REST("POST", "repos/monalisa/skills-repo/releases"),
					httpmock.JSONResponse(map[string]interface{}{
						"html_url": "https://github.com/monalisa/skills-repo/releases/tag/v1.0.1",
					}),
				)
			},
			opts: func(ios *iostreams.IOStreams, dir string, reg *httpmock.Registry) *PublishOptions {
				t.Helper()
				initGitRepo(t, dir, map[string]string{
					"origin": "https://github.com/monalisa/skills-repo.git",
				})
				return &PublishOptions{
					IO:  ios,
					Dir: dir,
					Prompter: &prompter.PrompterMock{
						ConfirmFunc: func(msg string, def bool) (bool, error) {
							return true, nil // accept all confirms (immutable + final)
						},
						SelectFunc: func(msg string, def string, opts []string) (int, error) {
							return 0, nil
						},
						InputFunc: func(msg string, def string) (string, error) {
							return "v1.0.1", nil
						},
					},
					GitClient: &git.Client{},
					client:    api.NewClientFromHTTP(&http.Client{Transport: reg}),
					host:      "github.com",
				}
			},
			wantStdout: "Enabled immutable releases",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setup(t, dir)

			ios, _, stdout, stderr := iostreams.Test()
			ios.SetStdoutTTY(tt.isTTY)
			ios.SetStdinTTY(tt.isTTY)
			ios.SetStderrTTY(tt.isTTY)

			reg := &httpmock.Registry{}
			defer reg.Verify(t)
			if tt.stubs != nil {
				tt.stubs(reg)
			}

			opts := tt.opts(ios, dir, reg)
			err := publishRun(opts)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			if tt.wantStdout != "" {
				assert.Contains(t, stdout.String(), tt.wantStdout)
			}
			if tt.wantStderr != "" {
				assert.Contains(t, stderr.String(), tt.wantStderr)
			}
			if tt.verify != nil {
				tt.verify(t, dir)
			}
		})
	}
}

func TestDetectGitHubRemote_UsesDir(t *testing.T) {
	// Create two separate git repos: "cwd-repo" simulates the working directory
	// and "target-repo" simulates the directory argument passed to publish.
	cwdRepo := t.TempDir()
	initGitRepo(t, cwdRepo, map[string]string{
		"origin": "https://github.com/monalisa/cwd-repo.git",
	})

	targetRepo := t.TempDir()
	initGitRepo(t, targetRepo, map[string]string{
		"origin": "https://github.com/monalisa/target-repo.git",
	})

	// gitClient points at cwd-repo (simulating factory-provided client)
	gitClient := &git.Client{RepoDir: cwdRepo}

	// detectGitHubRemote should use targetRepo's remotes, not cwdRepo's
	repo, err := detectGitHubRemote(gitClient, targetRepo)
	require.NoError(t, err)
	require.NotNil(t, repo)
	assert.Equal(t, "monalisa", repo.RepoOwner())
	assert.Equal(t, "target-repo", repo.RepoName())
}

func TestPublishRun_DirArgUsesTargetRemote(t *testing.T) {
	// Regression test: when a directory argument is provided, remote detection
	// must use that directory's git remotes, not the factory client's directory.
	//
	// Scenario:
	//   1. User is in cwd-repo (has remote → monalisa/cwd-repo)
	//   2. User runs: gh skill publish /path/to/target-repo
	//   3. target-repo has remote → monalisa/target-repo
	//   4. API calls must go to target-repo, NOT cwd-repo

	cwdRepo := t.TempDir()
	initGitRepo(t, cwdRepo, map[string]string{
		"origin": "https://github.com/monalisa/cwd-repo.git",
	})

	targetRepo := t.TempDir()
	initGitRepo(t, targetRepo, map[string]string{
		"origin": "https://github.com/monalisa/target-repo.git",
	})

	writeSkill(t, targetRepo, "my-skill", heredoc.Doc(`
		---
		name: my-skill
		description: A test skill
		license: MIT
		---
		Body text.
	`))

	ios, _, stdout, _ := iostreams.Test()
	ios.SetStdoutTTY(true)
	ios.SetStdinTTY(true)
	ios.SetStderrTTY(true)

	reg := &httpmock.Registry{}
	defer reg.Verify(t)

	// Stub API calls for target-repo (the correct repo).
	// If the bug is present, these stubs won't be called because the code
	// would try to hit cwd-repo endpoints instead, and reg.Verify would fail.
	stubAllSecureRemote(reg, "monalisa", "target-repo")

	err := publishRun(&PublishOptions{
		IO:        ios,
		Dir:       targetRepo,
		DryRun:    true,
		GitClient: &git.Client{RepoDir: cwdRepo},
		client:    api.NewClientFromHTTP(&http.Client{Transport: reg}),
		host:      "github.com",
	})

	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "1 skill(s) validated successfully")
}

// writeSkill creates skills/<name>/SKILL.md with the given content.
func writeSkill(t *testing.T, dir, name, content string) {
	t.Helper()
	skillDir := filepath.Join(dir, "skills", name)
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644))
}

// runGitInDir runs a git command in the given directory with isolation env vars.
func runGitInDir(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+dir)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}
