package preview

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"testing"

	"github.com/cli/cli/v2/internal/ghrepo"
	"github.com/cli/cli/v2/internal/prompter"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/httpmock"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/google/shlex"
	"github.com/stretchr/testify/assert"
)

func TestNewCmdPreview(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantRepo      string
		wantSkillName string
		wantErr       bool
	}{
		{
			name:          "repo and skill",
			input:         "github/awesome-copilot my-skill",
			wantRepo:      "github/awesome-copilot",
			wantSkillName: "my-skill",
		},
		{
			name:     "repo only",
			input:    "github/awesome-copilot",
			wantRepo: "github/awesome-copilot",
		},
		{
			name:    "no args",
			input:   "",
			wantErr: true,
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
			}

			var gotOpts *previewOptions
			cmd := NewCmdPreview(f, func(opts *previewOptions) error {
				gotOpts = opts
				return nil
			})

			args, _ := shlex.Split(tt.input)
			cmd.SetArgs(args)
			cmd.SetOut(&discardWriter{})
			cmd.SetErr(&discardWriter{})
			err := cmd.Execute()

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.wantRepo, gotOpts.RepoArg)
			assert.Equal(t, tt.wantSkillName, gotOpts.SkillName)
		})
	}
}

func TestNewCmdPreview_Alias(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	f := &cmdutil.Factory{IOStreams: ios, Prompter: &prompter.PrompterMock{}}
	cmd := NewCmdPreview(f, func(_ *previewOptions) error { return nil })
	assert.Contains(t, cmd.Aliases, "show")
}

func TestPreviewRun(t *testing.T) {
	skillContent := "---\nname: my-skill\ndescription: A test skill\n---\n# My Skill\n\nThis is the skill content."
	encodedContent := base64.StdEncoding.EncodeToString([]byte(skillContent))

	tests := []struct {
		name       string
		opts       *previewOptions
		tty        bool
		httpStubs  func(*httpmock.Registry)
		wantStdout string
		wantErr    string
	}{
		{
			name: "preview specific skill",
			tty:  true,
			opts: &previewOptions{
				repo:      ghrepo.New("github", "awesome-copilot"),
				SkillName: "my-skill",
			},
			httpStubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/github/awesome-copilot/releases/latest"),
					httpmock.StringResponse(`{"tag_name": "v1.0.0"}`),
				)
				reg.Register(
					httpmock.REST("GET", "repos/github/awesome-copilot/git/ref/tags/v1.0.0"),
					httpmock.StringResponse(`{"object": {"sha": "abc123", "type": "commit"}}`),
				)
				reg.Register(
					httpmock.REST("GET", "repos/github/awesome-copilot/git/trees/abc123"),
					httpmock.StringResponse(`{
						"sha": "abc123",
						"truncated": false,
						"tree": [
							{"path": "skills", "type": "tree", "sha": "tree1"},
							{"path": "skills/my-skill", "type": "tree", "sha": "treeSHA"},
							{"path": "skills/my-skill/SKILL.md", "type": "blob", "sha": "blob123"}
						]
					}`),
				)
				reg.Register(
					httpmock.REST("GET", "repos/github/awesome-copilot/git/trees/treeSHA"),
					httpmock.StringResponse(`{
						"tree": [
							{"path": "SKILL.md", "type": "blob", "sha": "blob123", "size": 50}
						]
					}`),
				)
				reg.Register(
					httpmock.REST("GET", "repos/github/awesome-copilot/git/blobs/blob123"),
					httpmock.StringResponse(`{"sha": "blob123", "content": "`+encodedContent+`", "encoding": "base64"}`),
				)
			},
			wantStdout: "My Skill",
		},
		{
			name: "preview with display name match",
			tty:  true,
			opts: &previewOptions{
				repo:      ghrepo.New("owner", "repo"),
				SkillName: "ns/my-skill",
			},
			httpStubs: func(reg *httpmock.Registry) {
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
					httpmock.StringResponse(`{
						"sha": "abc123",
						"truncated": false,
						"tree": [
							{"path": "skills", "type": "tree", "sha": "tree1"},
							{"path": "skills/ns", "type": "tree", "sha": "tree-ns"},
							{"path": "skills/ns/my-skill", "type": "tree", "sha": "treeSHA2"},
							{"path": "skills/ns/my-skill/SKILL.md", "type": "blob", "sha": "blob456"}
						]
					}`),
				)
				reg.Register(
					httpmock.REST("GET", "repos/owner/repo/git/trees/treeSHA2"),
					httpmock.StringResponse(`{
						"tree": [
							{"path": "SKILL.md", "type": "blob", "sha": "blob456", "size": 50}
						]
					}`),
				)
				reg.Register(
					httpmock.REST("GET", "repos/owner/repo/git/blobs/blob456"),
					httpmock.StringResponse(`{"sha": "blob456", "content": "`+encodedContent+`", "encoding": "base64"}`),
				)
			},
			wantStdout: "My Skill",
		},
		{
			name: "skill not found",
			tty:  true,
			opts: &previewOptions{
				repo:      ghrepo.New("owner", "repo"),
				SkillName: "nonexistent",
			},
			httpStubs: func(reg *httpmock.Registry) {
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
					httpmock.StringResponse(`{
						"sha": "abc123",
						"truncated": false,
						"tree": [
							{"path": "skills/my-skill", "type": "tree", "sha": "tree2"},
							{"path": "skills/my-skill/SKILL.md", "type": "blob", "sha": "blob123"}
						]
					}`),
				)
			},
			wantErr: `skill "nonexistent" not found in owner/repo`,
		},
		{
			name: "no skill name non-interactive errors",
			tty:  false,
			opts: &previewOptions{
				repo: ghrepo.New("owner", "repo"),
			},
			httpStubs: func(reg *httpmock.Registry) {
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
					httpmock.StringResponse(`{
						"sha": "abc123",
						"truncated": false,
						"tree": [
							{"path": "skills/my-skill", "type": "tree", "sha": "tree2"},
							{"path": "skills/my-skill/SKILL.md", "type": "blob", "sha": "blob123"}
						]
					}`),
				)
			},
			wantErr: "must specify a skill name when not running interactively",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := &httpmock.Registry{}
			defer reg.Verify(t)
			if tt.httpStubs != nil {
				tt.httpStubs(reg)
			}
			tt.opts.HttpClient = func() (*http.Client, error) {
				return &http.Client{Transport: reg}, nil
			}

			ios, _, stdout, _ := iostreams.Test()
			ios.SetStdoutTTY(tt.tty)
			ios.SetStdinTTY(tt.tty)
			tt.opts.IO = ios

			tt.opts.Prompter = &prompter.PrompterMock{}

			err := previewRun(tt.opts)

			if tt.wantErr != "" {
				assert.EqualError(t, err, tt.wantErr)
				return
			}

			assert.NoError(t, err)
			if tt.wantStdout != "" {
				assert.Contains(t, stdout.String(), tt.wantStdout)
			}
		})
	}
}

func TestPreviewRun_Interactive(t *testing.T) {
	skillContent := "# Selected Skill\n\nContent here."
	encodedContent := base64.StdEncoding.EncodeToString([]byte(skillContent))

	reg := &httpmock.Registry{}
	defer reg.Verify(t)
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
		httpmock.StringResponse(`{
			"sha": "abc123",
			"truncated": false,
			"tree": [
				{"path": "skills/alpha", "type": "tree", "sha": "tree-a"},
				{"path": "skills/alpha/SKILL.md", "type": "blob", "sha": "blob-a"},
				{"path": "skills/beta", "type": "tree", "sha": "tree-b"},
				{"path": "skills/beta/SKILL.md", "type": "blob", "sha": "blob-b"}
			]
		}`),
	)
	reg.Register(
		httpmock.REST("GET", "repos/owner/repo/git/trees/tree-b"),
		httpmock.StringResponse(`{
			"tree": [
				{"path": "SKILL.md", "type": "blob", "sha": "blob-b", "size": 40}
			]
		}`),
	)
	reg.Register(
		httpmock.REST("GET", "repos/owner/repo/git/blobs/blob-b"),
		httpmock.StringResponse(`{"sha": "blob-b", "content": "`+encodedContent+`", "encoding": "base64"}`),
	)

	ios, _, stdout, _ := iostreams.Test()
	ios.SetStdoutTTY(true)
	ios.SetStdinTTY(true)

	pm := &prompter.PrompterMock{
		SelectFunc: func(prompt string, defaultValue string, options []string) (int, error) {
			assert.Equal(t, "Select a skill to preview:", prompt)
			assert.Equal(t, []string{"alpha", "beta"}, options)
			return 1, nil // select "beta"
		},
	}

	opts := &previewOptions{
		IO:         ios,
		HttpClient: func() (*http.Client, error) { return &http.Client{Transport: reg}, nil },
		Prompter:   pm,
		repo:       ghrepo.New("owner", "repo"),
	}

	err := previewRun(opts)
	assert.NoError(t, err)
	assert.Contains(t, stdout.String(), "Selected Skill")
}

func TestPreviewRun_ShowsFileTree(t *testing.T) {
	skillContent := "---\nname: my-skill\ndescription: test\n---\n# My Skill\nBody."
	encodedContent := base64.StdEncoding.EncodeToString([]byte(skillContent))

	scriptContent := "#!/bin/bash\necho hello"
	encodedScript := base64.StdEncoding.EncodeToString([]byte(scriptContent))

	makeReg := func() *httpmock.Registry {
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
			httpmock.StringResponse(`{
				"sha": "abc123",
				"truncated": false,
				"tree": [
					{"path": "skills/my-skill", "type": "tree", "sha": "treeSHA"},
					{"path": "skills/my-skill/SKILL.md", "type": "blob", "sha": "blobSKILL"},
					{"path": "skills/my-skill/scripts", "type": "tree", "sha": "treeScripts"},
					{"path": "skills/my-skill/scripts/run.sh", "type": "blob", "sha": "blobScript"}
				]
			}`),
		)
		reg.Register(
			httpmock.REST("GET", "repos/owner/repo/git/trees/treeSHA"),
			httpmock.StringResponse(`{
				"tree": [
					{"path": "SKILL.md", "type": "blob", "sha": "blobSKILL", "size": 50},
					{"path": "scripts", "type": "tree", "sha": "treeScripts"},
					{"path": "scripts/run.sh", "type": "blob", "sha": "blobScript", "size": 20}
				]
			}`),
		)
		reg.Register(
			httpmock.REST("GET", "repos/owner/repo/git/blobs/blobSKILL"),
			httpmock.StringResponse(`{"sha": "blobSKILL", "content": "`+encodedContent+`", "encoding": "base64"}`),
		)
		reg.Register(
			httpmock.REST("GET", "repos/owner/repo/git/blobs/blobScript"),
			httpmock.StringResponse(`{"sha": "blobScript", "content": "`+encodedScript+`", "encoding": "base64"}`),
		)
		return reg
	}

	t.Run("interactive file picker", func(t *testing.T) {
		reg := makeReg()
		defer reg.Verify(t)
		ios, _, stdout, _ := iostreams.Test()
		ios.SetStdoutTTY(true)
		ios.SetStdinTTY(true)
		ios.SetColorEnabled(false)

		selectCalls := 0
		pm := &prompter.PrompterMock{
			SelectFunc: func(prompt string, defaultValue string, options []string) (int, error) {
				selectCalls++
				if selectCalls == 1 {
					// Options: ["SKILL.md", "scripts/run.sh"]
					assert.Equal(t, "SKILL.md", options[0])
					assert.Equal(t, "scripts/run.sh", options[1])
					// Select "scripts/run.sh"
					return 1, nil
				}
				// Simulate Esc/Ctrl-C to exit
				return 0, fmt.Errorf("user cancelled")
			},
		}

		opts := &previewOptions{
			IO:         ios,
			HttpClient: func() (*http.Client, error) { return &http.Client{Transport: reg}, nil },
			Prompter:   pm,
			repo:       ghrepo.New("owner", "repo"),
			SkillName:  "my-skill",
		}

		err := previewRun(opts)
		assert.NoError(t, err)

		out := stdout.String()
		assert.Contains(t, out, "echo hello")
		assert.Equal(t, 2, selectCalls)
	})

	t.Run("non-interactive dumps all files", func(t *testing.T) {
		reg := makeReg()
		defer reg.Verify(t)
		ios, _, stdout, _ := iostreams.Test()
		ios.SetStdoutTTY(false)
		ios.SetStdinTTY(false)
		ios.SetColorEnabled(false)

		opts := &previewOptions{
			IO:         ios,
			HttpClient: func() (*http.Client, error) { return &http.Client{Transport: reg}, nil },
			Prompter:   &prompter.PrompterMock{},
			repo:       ghrepo.New("owner", "repo"),
			SkillName:  "my-skill",
		}

		err := previewRun(opts)
		assert.NoError(t, err)

		out := stdout.String()
		assert.Contains(t, out, "my-skill/")
		assert.Contains(t, out, "My Skill")
		assert.Contains(t, out, "scripts/run.sh")
		assert.Contains(t, out, "echo hello")
	})
}

// discardWriter is a no-op writer for suppressing cobra output in tests.
type discardWriter struct{}

func (d *discardWriter) Write(p []byte) (int, error) { return len(p), nil }
