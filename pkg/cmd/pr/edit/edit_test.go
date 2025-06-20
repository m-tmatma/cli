package edit

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/cli/cli/v2/api"
	fd "github.com/cli/cli/v2/internal/featuredetection"
	"github.com/cli/cli/v2/internal/ghrepo"
	shared "github.com/cli/cli/v2/pkg/cmd/pr/shared"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/httpmock"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/google/shlex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCmdEdit(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "my-body.md")
	err := os.WriteFile(tmpFile, []byte("a body from file"), 0600)
	require.NoError(t, err)

	tests := []struct {
		name     string
		input    string
		stdin    string
		output   EditOptions
		wantsErr bool
	}{
		{
			name:  "no argument",
			input: "",
			output: EditOptions{
				SelectorArg: "",
				Interactive: true,
			},
			wantsErr: false,
		},
		{
			name:     "two arguments",
			input:    "1 2",
			output:   EditOptions{},
			wantsErr: true,
		},
		{
			name:  "pull request number argument",
			input: "23",
			output: EditOptions{
				SelectorArg: "23",
				Interactive: true,
			},
			wantsErr: false,
		},
		{
			name:  "title flag",
			input: "23 --title test",
			output: EditOptions{
				SelectorArg: "23",
				Editable: shared.Editable{
					Title: shared.EditableString{
						Value:  "test",
						Edited: true,
					},
				},
			},
			wantsErr: false,
		},
		{
			name:  "body flag",
			input: "23 --body test",
			output: EditOptions{
				SelectorArg: "23",
				Editable: shared.Editable{
					Body: shared.EditableString{
						Value:  "test",
						Edited: true,
					},
				},
			},
			wantsErr: false,
		},
		{
			name:  "body from stdin",
			input: "23 --body-file -",
			stdin: "this is on standard input",
			output: EditOptions{
				SelectorArg: "23",
				Editable: shared.Editable{
					Body: shared.EditableString{
						Value:  "this is on standard input",
						Edited: true,
					},
				},
			},
			wantsErr: false,
		},
		{
			name:  "body from file",
			input: fmt.Sprintf("23 --body-file '%s'", tmpFile),
			output: EditOptions{
				SelectorArg: "23",
				Editable: shared.Editable{
					Body: shared.EditableString{
						Value:  "a body from file",
						Edited: true,
					},
				},
			},
			wantsErr: false,
		},
		{
			name:     "both body and body-file flags",
			input:    "23 --body foo --body-file bar",
			wantsErr: true,
		},
		{
			name:  "base flag",
			input: "23 --base base-branch-name",
			output: EditOptions{
				SelectorArg: "23",
				Editable: shared.Editable{
					Base: shared.EditableString{
						Value:  "base-branch-name",
						Edited: true,
					},
				},
			},
			wantsErr: false,
		},
		{
			name:  "add-reviewer flag",
			input: "23 --add-reviewer monalisa,owner/core",
			output: EditOptions{
				SelectorArg: "23",
				Editable: shared.Editable{
					Reviewers: shared.EditableSlice{
						Add:    []string{"monalisa", "owner/core"},
						Edited: true,
					},
				},
			},
			wantsErr: false,
		},
		{
			name:  "remove-reviewer flag",
			input: "23 --remove-reviewer monalisa,owner/core",
			output: EditOptions{
				SelectorArg: "23",
				Editable: shared.Editable{
					Reviewers: shared.EditableSlice{
						Remove: []string{"monalisa", "owner/core"},
						Edited: true,
					},
				},
			},
			wantsErr: false,
		},
		{
			name:  "add-assignee flag",
			input: "23 --add-assignee monalisa,hubot",
			output: EditOptions{
				SelectorArg: "23",
				Editable: shared.Editable{
					Assignees: shared.EditableAssignees{
						EditableSlice: shared.EditableSlice{
							Add:    []string{"monalisa", "hubot"},
							Edited: true,
						},
					},
				},
			},
			wantsErr: false,
		},
		{
			name:  "remove-assignee flag",
			input: "23 --remove-assignee monalisa,hubot",
			output: EditOptions{
				SelectorArg: "23",
				Editable: shared.Editable{
					Assignees: shared.EditableAssignees{
						EditableSlice: shared.EditableSlice{
							Remove: []string{"monalisa", "hubot"},
							Edited: true,
						},
					},
				},
			},
			wantsErr: false,
		},
		{
			name:  "add-label flag",
			input: "23 --add-label feature,TODO,bug",
			output: EditOptions{
				SelectorArg: "23",
				Editable: shared.Editable{
					Labels: shared.EditableSlice{
						Add:    []string{"feature", "TODO", "bug"},
						Edited: true,
					},
				},
			},
			wantsErr: false,
		},
		{
			name:  "remove-label flag",
			input: "23 --remove-label feature,TODO,bug",
			output: EditOptions{
				SelectorArg: "23",
				Editable: shared.Editable{
					Labels: shared.EditableSlice{
						Remove: []string{"feature", "TODO", "bug"},
						Edited: true,
					},
				},
			},
			wantsErr: false,
		},
		{
			name:  "add-project flag",
			input: "23 --add-project Cleanup,Roadmap",
			output: EditOptions{
				SelectorArg: "23",
				Editable: shared.Editable{
					Projects: shared.EditableProjects{
						EditableSlice: shared.EditableSlice{
							Add:    []string{"Cleanup", "Roadmap"},
							Edited: true,
						},
					},
				},
			},
			wantsErr: false,
		},
		{
			name:  "remove-project flag",
			input: "23 --remove-project Cleanup,Roadmap",
			output: EditOptions{
				SelectorArg: "23",
				Editable: shared.Editable{
					Projects: shared.EditableProjects{
						EditableSlice: shared.EditableSlice{
							Remove: []string{"Cleanup", "Roadmap"},
							Edited: true,
						},
					},
				},
			},
			wantsErr: false,
		},
		{
			name:  "milestone flag",
			input: "23 --milestone GA",
			output: EditOptions{
				SelectorArg: "23",
				Editable: shared.Editable{
					Milestone: shared.EditableString{
						Value:  "GA",
						Edited: true,
					},
				},
			},
			wantsErr: false,
		},
		{
			name:  "remove-milestone flag",
			input: "23 --remove-milestone",
			output: EditOptions{
				SelectorArg: "23",
				Editable: shared.Editable{
					Milestone: shared.EditableString{
						Value:  "",
						Edited: true,
					},
				},
			},
			wantsErr: false,
		},
		{
			name:     "both milestone and remove-milestone flags",
			input:    "23 --milestone foo --remove-milestone",
			wantsErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ios, stdin, _, _ := iostreams.Test()
			ios.SetStdoutTTY(true)
			ios.SetStdinTTY(true)
			ios.SetStderrTTY(true)

			if tt.stdin != "" {
				_, _ = stdin.WriteString(tt.stdin)
			}

			f := &cmdutil.Factory{
				IOStreams: ios,
			}

			argv, err := shlex.Split(tt.input)
			assert.NoError(t, err)

			var gotOpts *EditOptions
			cmd := NewCmdEdit(f, func(opts *EditOptions) error {
				gotOpts = opts
				return nil
			})
			cmd.Flags().BoolP("help", "x", false, "")

			cmd.SetArgs(argv)
			cmd.SetIn(&bytes.Buffer{})
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})

			_, err = cmd.ExecuteC()
			if tt.wantsErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.output.SelectorArg, gotOpts.SelectorArg)
			assert.Equal(t, tt.output.Interactive, gotOpts.Interactive)
			assert.Equal(t, tt.output.Editable, gotOpts.Editable)
		})
	}
}

func Test_editRun(t *testing.T) {
	tests := []struct {
		name      string
		input     *EditOptions
		httpStubs func(*httpmock.Registry)
		stdout    string
		stderr    string
	}{
		{
			name: "non-interactive",
			input: &EditOptions{
				Detector:    &fd.EnabledDetectorMock{},
				SelectorArg: "123",
				Finder: shared.NewMockFinder("123", &api.PullRequest{
					URL:                "https://github.com/OWNER/REPO/pull/123",
					AssignedActorsUsed: true,
				}, ghrepo.New("OWNER", "REPO")),
				Interactive: false,
				Editable: shared.Editable{
					Title: shared.EditableString{
						Value:  "new title",
						Edited: true,
					},
					Body: shared.EditableString{
						Value:  "new body",
						Edited: true,
					},
					Base: shared.EditableString{
						Value:  "base-branch-name",
						Edited: true,
					},
					Reviewers: shared.EditableSlice{
						Add:    []string{"OWNER/core", "OWNER/external", "monalisa", "hubot"},
						Remove: []string{"dependabot"},
						Edited: true,
					},
					Assignees: shared.EditableAssignees{
						EditableSlice: shared.EditableSlice{
							Add:    []string{"monalisa", "hubot"},
							Remove: []string{"octocat"},
							Edited: true,
						},
					},
					Labels: shared.EditableSlice{
						Add:    []string{"feature", "TODO", "bug"},
						Remove: []string{"docs"},
						Edited: true,
					},
					Projects: shared.EditableProjects{
						EditableSlice: shared.EditableSlice{
							Add:    []string{"Cleanup", "CleanupV2"},
							Remove: []string{"Roadmap", "RoadmapV2"},
							Edited: true,
						},
					},
					Milestone: shared.EditableString{
						Value:  "GA",
						Edited: true,
					},
				},
				Fetcher: testFetcher{},
			},
			httpStubs: func(reg *httpmock.Registry) {
				mockRepoMetadata(reg, false)
				mockPullRequestUpdate(reg)
				mockPullRequestUpdateActorAssignees(reg)
				mockPullRequestReviewersUpdate(reg)
				mockPullRequestUpdateLabels(reg)
				mockProjectV2ItemUpdate(reg)
			},
			stdout: "https://github.com/OWNER/REPO/pull/123\n",
		},
		{
			name: "non-interactive skip reviewers",
			input: &EditOptions{
				Detector:    &fd.EnabledDetectorMock{},
				SelectorArg: "123",
				Finder: shared.NewMockFinder("123", &api.PullRequest{
					URL:                "https://github.com/OWNER/REPO/pull/123",
					AssignedActorsUsed: true,
				}, ghrepo.New("OWNER", "REPO")),
				Interactive: false,
				Editable: shared.Editable{
					Title: shared.EditableString{
						Value:  "new title",
						Edited: true,
					},
					Body: shared.EditableString{
						Value:  "new body",
						Edited: true,
					},
					Base: shared.EditableString{
						Value:  "base-branch-name",
						Edited: true,
					},
					Assignees: shared.EditableAssignees{
						EditableSlice: shared.EditableSlice{
							Add:    []string{"monalisa", "hubot"},
							Remove: []string{"octocat"},
							Edited: true,
						},
					},
					Labels: shared.EditableSlice{
						Add:    []string{"feature", "TODO", "bug"},
						Remove: []string{"docs"},
						Edited: true,
					},
					Projects: shared.EditableProjects{
						EditableSlice: shared.EditableSlice{
							Add:    []string{"Cleanup", "CleanupV2"},
							Remove: []string{"Roadmap", "RoadmapV2"},
							Edited: true,
						},
					},
					Milestone: shared.EditableString{
						Value:  "GA",
						Edited: true,
					},
				},
				Fetcher: testFetcher{},
			},
			httpStubs: func(reg *httpmock.Registry) {
				mockRepoMetadata(reg, true)
				mockPullRequestUpdate(reg)
				mockPullRequestUpdateActorAssignees(reg)
				mockPullRequestUpdateLabels(reg)
				mockProjectV2ItemUpdate(reg)
			},
			stdout: "https://github.com/OWNER/REPO/pull/123\n",
		},
		{
			name: "non-interactive remove all reviewers",
			input: &EditOptions{
				Detector:    &fd.EnabledDetectorMock{},
				SelectorArg: "123",
				Finder: shared.NewMockFinder("123", &api.PullRequest{
					URL:                "https://github.com/OWNER/REPO/pull/123",
					AssignedActorsUsed: true,
				}, ghrepo.New("OWNER", "REPO")),
				Interactive: false,
				Editable: shared.Editable{
					Title: shared.EditableString{
						Value:  "new title",
						Edited: true,
					},
					Body: shared.EditableString{
						Value:  "new body",
						Edited: true,
					},
					Base: shared.EditableString{
						Value:  "base-branch-name",
						Edited: true,
					},
					Reviewers: shared.EditableSlice{
						Remove: []string{"OWNER/core", "OWNER/external", "monalisa", "hubot", "dependabot"},
						Edited: true,
					},
					Assignees: shared.EditableAssignees{
						EditableSlice: shared.EditableSlice{
							Add:    []string{"monalisa", "hubot"},
							Remove: []string{"octocat"},
							Edited: true,
						},
					},
					Labels: shared.EditableSlice{
						Add:    []string{"feature", "TODO", "bug"},
						Remove: []string{"docs"},
						Edited: true,
					},
					Projects: shared.EditableProjects{
						EditableSlice: shared.EditableSlice{
							Add:    []string{"Cleanup", "CleanupV2"},
							Remove: []string{"Roadmap", "RoadmapV2"},
							Edited: true,
						},
					},
					Milestone: shared.EditableString{
						Value:  "GA",
						Edited: true,
					},
				},
				Fetcher: testFetcher{},
			},
			httpStubs: func(reg *httpmock.Registry) {
				mockRepoMetadata(reg, false)
				mockPullRequestUpdate(reg)
				mockPullRequestReviewersUpdate(reg)
				mockPullRequestUpdateLabels(reg)
				mockPullRequestUpdateActorAssignees(reg)
				mockProjectV2ItemUpdate(reg)
			},
			stdout: "https://github.com/OWNER/REPO/pull/123\n",
		},
		{
			name: "interactive",
			input: &EditOptions{
				Detector:    &fd.EnabledDetectorMock{},
				SelectorArg: "123",
				Finder: shared.NewMockFinder("123", &api.PullRequest{
					URL:                "https://github.com/OWNER/REPO/pull/123",
					AssignedActorsUsed: true,
				}, ghrepo.New("OWNER", "REPO")),
				Interactive: true,
				Surveyor: testSurveyor{
					fieldsToEdit: func(e *shared.Editable) error {
						e.Title.Edited = true
						e.Body.Edited = true
						e.Reviewers.Edited = true
						e.Assignees.Edited = true
						e.Labels.Edited = true
						e.Projects.Edited = true
						e.Milestone.Edited = true
						return nil
					},
					editFields: func(e *shared.Editable, _ string) error {
						e.Title.Value = "new title"
						e.Body.Value = "new body"
						e.Reviewers.Value = []string{"monalisa", "hubot", "OWNER/core", "OWNER/external"}
						e.Assignees.Value = []string{"monalisa", "hubot"}
						e.Labels.Value = []string{"feature", "TODO", "bug"}
						e.Labels.Add = []string{"feature", "TODO", "bug"}
						e.Labels.Remove = []string{"docs"}
						e.Projects.Value = []string{"Cleanup", "CleanupV2"}
						e.Milestone.Value = "GA"
						return nil
					},
				},
				Fetcher:         testFetcher{},
				EditorRetriever: testEditorRetriever{},
			},
			httpStubs: func(reg *httpmock.Registry) {
				mockRepoMetadata(reg, false)
				mockPullRequestUpdate(reg)
				mockPullRequestUpdateActorAssignees(reg)
				mockPullRequestReviewersUpdate(reg)
				mockPullRequestUpdateLabels(reg)
				mockProjectV2ItemUpdate(reg)
			},
			stdout: "https://github.com/OWNER/REPO/pull/123\n",
		},
		{
			name: "interactive skip reviewers",
			input: &EditOptions{
				Detector:    &fd.EnabledDetectorMock{},
				SelectorArg: "123",
				Finder: shared.NewMockFinder("123", &api.PullRequest{
					URL:                "https://github.com/OWNER/REPO/pull/123",
					AssignedActorsUsed: true,
				}, ghrepo.New("OWNER", "REPO")),
				Interactive: true,
				Surveyor: testSurveyor{
					fieldsToEdit: func(e *shared.Editable) error {
						e.Title.Edited = true
						e.Body.Edited = true
						e.Assignees.Edited = true
						e.Labels.Edited = true
						e.Projects.Edited = true
						e.Milestone.Edited = true
						return nil
					},
					editFields: func(e *shared.Editable, _ string) error {
						e.Title.Value = "new title"
						e.Body.Value = "new body"
						e.Assignees.Value = []string{"monalisa", "hubot"}
						e.Labels.Value = []string{"feature", "TODO", "bug"}
						e.Labels.Add = []string{"feature", "TODO", "bug"}
						e.Labels.Remove = []string{"docs"}
						e.Projects.Value = []string{"Cleanup", "CleanupV2"}
						e.Milestone.Value = "GA"
						return nil
					},
				},
				Fetcher:         testFetcher{},
				EditorRetriever: testEditorRetriever{},
			},
			httpStubs: func(reg *httpmock.Registry) {
				mockRepoMetadata(reg, true)
				mockPullRequestUpdate(reg)
				mockPullRequestUpdateActorAssignees(reg)
				mockPullRequestUpdateLabels(reg)
				mockProjectV2ItemUpdate(reg)
			},
			stdout: "https://github.com/OWNER/REPO/pull/123\n",
		},
		{
			name: "interactive remove all reviewers",
			input: &EditOptions{
				Detector:    &fd.EnabledDetectorMock{},
				SelectorArg: "123",
				Finder: shared.NewMockFinder("123", &api.PullRequest{
					URL:                "https://github.com/OWNER/REPO/pull/123",
					AssignedActorsUsed: true,
				}, ghrepo.New("OWNER", "REPO")),
				Interactive: true,
				Surveyor: testSurveyor{
					fieldsToEdit: func(e *shared.Editable) error {
						e.Title.Edited = true
						e.Body.Edited = true
						e.Reviewers.Edited = true
						e.Assignees.Edited = true
						e.Labels.Edited = true
						e.Projects.Edited = true
						e.Milestone.Edited = true
						return nil
					},
					editFields: func(e *shared.Editable, _ string) error {
						e.Title.Value = "new title"
						e.Body.Value = "new body"
						e.Reviewers.Remove = []string{"monalisa", "hubot", "OWNER/core", "OWNER/external", "dependabot"}
						e.Assignees.Value = []string{"monalisa", "hubot"}
						e.Labels.Value = []string{"feature", "TODO", "bug"}
						e.Labels.Add = []string{"feature", "TODO", "bug"}
						e.Labels.Remove = []string{"docs"}
						e.Projects.Value = []string{"Cleanup", "CleanupV2"}
						e.Milestone.Value = "GA"
						return nil
					},
				},
				Fetcher:         testFetcher{},
				EditorRetriever: testEditorRetriever{},
			},
			httpStubs: func(reg *httpmock.Registry) {
				mockRepoMetadata(reg, false)
				mockPullRequestUpdate(reg)
				mockPullRequestReviewersUpdate(reg)
				mockPullRequestUpdateActorAssignees(reg)
				mockPullRequestUpdateLabels(reg)
				mockProjectV2ItemUpdate(reg)
			},
			stdout: "https://github.com/OWNER/REPO/pull/123\n",
		},
		{
			name: "interactive prompts with actor assignee display names when actors available",
			input: &EditOptions{
				Detector:    &fd.EnabledDetectorMock{},
				SelectorArg: "123",
				Finder: shared.NewMockFinder("123", &api.PullRequest{
					URL:                "https://github.com/OWNER/REPO/pull/123",
					AssignedActorsUsed: true,
					AssignedActors: api.AssignedActors{
						Nodes: []api.Actor{
							{
								ID:       "HUBOTID",
								Login:    "hubot",
								TypeName: "Bot",
							},
						},
						TotalCount: 1,
					},
				}, ghrepo.New("OWNER", "REPO")),
				Interactive: true,
				Surveyor: testSurveyor{
					fieldsToEdit: func(e *shared.Editable) error {
						e.Assignees.Edited = true
						return nil
					},
					editFields: func(e *shared.Editable, _ string) error {
						// Checking that the display name is being used in the prompt.
						require.Equal(t, []string{"hubot"}, e.Assignees.Default)
						require.Equal(t, []string{"hubot"}, e.Assignees.DefaultLogins)

						// Adding MonaLisa as PR assignee, should preserve hubot.
						e.Assignees.Value = []string{"hubot", "MonaLisa (Mona Display Name)"}
						return nil
					},
				},
				Fetcher:         testFetcher{},
				EditorRetriever: testEditorRetriever{},
			},
			httpStubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.GraphQL(`query RepositoryAssignableActors\b`),
					httpmock.StringResponse(`
					{ "data": { "repository": { "suggestedActors": {
						"nodes": [
							{ "login": "hubot", "id": "HUBOTID", "__typename": "Bot" },
							{ "login": "MonaLisa", "id": "MONAID", "name": "Mona Display Name", "__typename": "User" }
						],
						"pageInfo": { "hasNextPage": false }
					} } } }
					`))
				mockPullRequestUpdate(reg)
				reg.Register(
					httpmock.GraphQL(`mutation ReplaceActorsForAssignable\b`),
					httpmock.GraphQLMutation(`
					{ "data": { "replaceActorsForAssignable": { "__typename": "" } } }`,
						func(inputs map[string]interface{}) {
							// Checking that despite the display name being returned
							// from the EditFieldsSurvey, the ID is still
							// used in the mutation.
							require.Subset(t, inputs["actorIds"], []string{"MONAID", "HUBOTID"})
						}),
				)
			},
			stdout: "https://github.com/OWNER/REPO/pull/123\n",
		},
		{
			name: "Legacy assignee users are fetched and updated on unsupported GitHub Hosts",
			input: &EditOptions{
				Detector:    &fd.DisabledDetectorMock{},
				SelectorArg: "123",
				Finder: shared.NewMockFinder("123", &api.PullRequest{
					URL: "https://github.com/OWNER/REPO/pull/123",
				}, ghrepo.New("OWNER", "REPO")),
				Interactive: false,
				Editable: shared.Editable{
					Assignees: shared.EditableAssignees{
						EditableSlice: shared.EditableSlice{
							Add:    []string{"monalisa", "hubot"},
							Remove: []string{"octocat"},
							Edited: true,
						},
					},
				},
				Fetcher: testFetcher{},
			},
			httpStubs: func(reg *httpmock.Registry) {
				// Notice there is no call to mockReplaceActorsForAssignable()
				// and no GraphQL call to RepositoryAssignableActors below.
				reg.Register(
					httpmock.GraphQL(`query RepositoryAssignableUsers\b`),
					httpmock.StringResponse(`
					{ "data": { "repository": { "assignableUsers": {
						"nodes": [
							{ "login": "hubot", "id": "HUBOTID" },
							{ "login": "MonaLisa", "id": "MONAID" }
						],
						"pageInfo": { "hasNextPage": false }
					} } } }
					`))
				mockPullRequestUpdate(reg)
			},
			stdout: "https://github.com/OWNER/REPO/pull/123\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ios, _, stdout, stderr := iostreams.Test()
			ios.SetStdoutTTY(true)
			ios.SetStdinTTY(true)
			ios.SetStderrTTY(true)

			reg := &httpmock.Registry{}
			defer reg.Verify(t)
			tt.httpStubs(reg)

			httpClient := func() (*http.Client, error) { return &http.Client{Transport: reg}, nil }
			baseRepo := func() (ghrepo.Interface, error) { return ghrepo.New("OWNER", "REPO"), nil }

			tt.input.IO = ios
			tt.input.HttpClient = httpClient
			tt.input.BaseRepo = baseRepo

			err := editRun(tt.input)
			assert.NoError(t, err)
			assert.Equal(t, tt.stdout, stdout.String())
			assert.Equal(t, tt.stderr, stderr.String())
		})
	}
}

func mockRepoMetadata(reg *httpmock.Registry, skipReviewers bool) {
	reg.Register(
		httpmock.GraphQL(`query RepositoryAssignableActors\b`),
		httpmock.StringResponse(`
			{ "data": { "repository": { "suggestedActors": {
				"nodes": [
					{ "login": "hubot", "id": "HUBOTID", "__typename": "Bot" },
					{ "login": "MonaLisa", "id": "MONAID", "name": "Mona Display Name", "__typename": "User" }
				],
				"pageInfo": { "hasNextPage": false }
			} } } }
			`))
	reg.Register(
		httpmock.GraphQL(`query RepositoryLabelList\b`),
		httpmock.StringResponse(`
		{ "data": { "repository": { "labels": {
			"nodes": [
				{ "name": "feature", "id": "FEATUREID" },
				{ "name": "TODO", "id": "TODOID" },
				{ "name": "bug", "id": "BUGID" },
				{ "name": "docs", "id": "DOCSID" }
			],
			"pageInfo": { "hasNextPage": false }
		} } } }
		`))
	reg.Register(
		httpmock.GraphQL(`query RepositoryMilestoneList\b`),
		httpmock.StringResponse(`
		{ "data": { "repository": { "milestones": {
			"nodes": [
				{ "title": "GA", "id": "GAID" },
				{ "title": "Big One.oh", "id": "BIGONEID" }
			],
			"pageInfo": { "hasNextPage": false }
		} } } }
		`))
	reg.Register(
		httpmock.GraphQL(`query RepositoryProjectList\b`),
		httpmock.StringResponse(`
		{ "data": { "repository": { "projects": {
			"nodes": [
				{ "name": "Cleanup", "id": "CLEANUPID" },
				{ "name": "Roadmap", "id": "ROADMAPID" }
			],
			"pageInfo": { "hasNextPage": false }
		} } } }
		`))
	reg.Register(
		httpmock.GraphQL(`query OrganizationProjectList\b`),
		httpmock.StringResponse(`
		{ "data": { "organization": { "projects": {
			"nodes": [
				{ "name": "Triage", "id": "TRIAGEID" }
			],
			"pageInfo": { "hasNextPage": false }
		} } } }
		`))
	reg.Register(
		httpmock.GraphQL(`query RepositoryProjectV2List\b`),
		httpmock.StringResponse(`
		{ "data": { "repository": { "projectsV2": {
			"nodes": [
				{ "title": "CleanupV2", "id": "CLEANUPV2ID" },
				{ "title": "RoadmapV2", "id": "ROADMAPV2ID" }
			],
			"pageInfo": { "hasNextPage": false }
		} } } }
		`))
	reg.Register(
		httpmock.GraphQL(`query OrganizationProjectV2List\b`),
		httpmock.StringResponse(`
		{ "data": { "organization": { "projectsV2": {
			"nodes": [
				{ "title": "TriageV2", "id": "TRIAGEV2ID" }
			],
			"pageInfo": { "hasNextPage": false }
		} } } }
		`))
	reg.Register(
		httpmock.GraphQL(`query UserProjectV2List\b`),
		httpmock.StringResponse(`
		{ "data": { "viewer": { "projectsV2": {
			"nodes": [
				{ "title": "MonalisaV2", "id": "MONALISAV2ID" }
			],
			"pageInfo": { "hasNextPage": false }
		} } } }
		`))
	if !skipReviewers {
		reg.Register(
			httpmock.GraphQL(`query OrganizationTeamList\b`),
			httpmock.StringResponse(`
      { "data": { "organization": { "teams": {
        "nodes": [
          { "slug": "external", "id": "EXTERNALID" },
          { "slug": "core", "id": "COREID" }
        ],
        "pageInfo": { "hasNextPage": false }
      } } } }
		`))
		reg.Register(
			httpmock.GraphQL(`query UserCurrent\b`),
			httpmock.StringResponse(`
		  { "data": { "viewer": { "login": "monalisa" } } }
		`))
	}
}

func mockPullRequestUpdate(reg *httpmock.Registry) {
	reg.Register(
		httpmock.GraphQL(`mutation PullRequestUpdate\b`),
		httpmock.StringResponse(`{}`))
}

func mockPullRequestUpdateActorAssignees(reg *httpmock.Registry) {
	reg.Register(
		httpmock.GraphQL(`mutation ReplaceActorsForAssignable\b`),
		httpmock.GraphQLMutation(`
		{ "data": { "replaceActorsForAssignable": { "__typename": "" } } }`,
			func(inputs map[string]interface{}) {}),
	)
}

func mockPullRequestReviewersUpdate(reg *httpmock.Registry) {
	reg.Register(
		httpmock.GraphQL(`mutation PullRequestUpdateRequestReviews\b`),
		httpmock.StringResponse(`{}`))
}

func mockPullRequestUpdateLabels(reg *httpmock.Registry) {
	reg.Register(
		httpmock.GraphQL(`mutation LabelAdd\b`),
		httpmock.GraphQLMutation(`
		{ "data": { "addLabelsToLabelable": { "__typename": "" } } }`,
			func(inputs map[string]interface{}) {}),
	)
	reg.Register(
		httpmock.GraphQL(`mutation LabelRemove\b`),
		httpmock.GraphQLMutation(`
		{ "data": { "removeLabelsFromLabelable": { "__typename": "" } } }`,
			func(inputs map[string]interface{}) {}),
	)
}

func mockProjectV2ItemUpdate(reg *httpmock.Registry) {
	reg.Register(
		httpmock.GraphQL(`mutation UpdateProjectV2Items\b`),
		httpmock.GraphQLMutation(`
		{ "data": { "add_000": { "item": { "id": "1" } }, "delete_001": { "item": { "id": "2" } } } }`,
			func(inputs map[string]interface{}) {}),
	)
}

type testFetcher struct{}

func (f testFetcher) EditableOptionsFetch(client *api.Client, repo ghrepo.Interface, opts *shared.Editable) error {
	return shared.FetchOptions(client, repo, opts)
}

type testSurveyor struct {
	fieldsToEdit func(e *shared.Editable) error
	editFields   func(e *shared.Editable, editorCmd string) error
}

func (s testSurveyor) FieldsToEdit(e *shared.Editable) error {
	return s.fieldsToEdit(e)
}

func (s testSurveyor) EditFields(e *shared.Editable, editorCmd string) error {
	return s.editFields(e, editorCmd)
}

type testEditorRetriever struct{}

func (t testEditorRetriever) Retrieve() (string, error) {
	return "vim", nil
}

// TODO projectsV1Deprecation
// Remove this test.
func TestProjectsV1Deprecation(t *testing.T) {
	t.Run("when projects v1 is supported, is included in query", func(t *testing.T) {
		ios, _, _, _ := iostreams.Test()

		reg := &httpmock.Registry{}
		reg.Register(
			httpmock.GraphQL(`projectCards`),
			// Simulate a GraphQL error to early exit the test.
			httpmock.StatusStringResponse(500, ""),
		)

		f := &cmdutil.Factory{
			IOStreams: ios,
			HttpClient: func() (*http.Client, error) {
				return &http.Client{Transport: reg}, nil
			},
		}

		// Ignore the error because we have no way to really stub it without
		// fully stubbing a GQL error structure in the request body.
		_ = editRun(&EditOptions{
			IO: ios,
			HttpClient: func() (*http.Client, error) {
				return &http.Client{Transport: reg}, nil
			},
			Detector: &fd.EnabledDetectorMock{},

			Finder: shared.NewFinder(f),

			SelectorArg: "https://github.com/cli/cli/pull/123",
		})

		// Verify that our request contained projectCards
		reg.Verify(t)
	})

	t.Run("when projects v1 is not supported, is not included in query", func(t *testing.T) {
		ios, _, _, _ := iostreams.Test()

		reg := &httpmock.Registry{}
		reg.Exclude(t, httpmock.GraphQL(`projectCards`))

		f := &cmdutil.Factory{
			IOStreams: ios,
			HttpClient: func() (*http.Client, error) {
				return &http.Client{Transport: reg}, nil
			},
		}

		// Ignore the error because we have no way to really stub it without
		// fully stubbing a GQL error structure in the request body.
		_ = editRun(&EditOptions{
			IO: ios,
			HttpClient: func() (*http.Client, error) {
				return &http.Client{Transport: reg}, nil
			},
			Detector: &fd.DisabledDetectorMock{},

			Finder: shared.NewFinder(f),

			SelectorArg: "https://github.com/cli/cli/pull/123",
		})

		// Verify that our request did not contain projectCards
		reg.Verify(t)
	})
}
