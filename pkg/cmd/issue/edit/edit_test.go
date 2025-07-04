package edit

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/MakeNowJust/heredoc"
	"github.com/cli/cli/v2/api"
	fd "github.com/cli/cli/v2/internal/featuredetection"
	"github.com/cli/cli/v2/internal/ghrepo"
	"github.com/cli/cli/v2/internal/run"
	prShared "github.com/cli/cli/v2/pkg/cmd/pr/shared"
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
		name             string
		input            string
		stdin            string
		output           EditOptions
		expectedBaseRepo ghrepo.Interface
		wantsErr         bool
	}{
		{
			name:     "no argument",
			input:    "",
			output:   EditOptions{},
			wantsErr: true,
		},
		{
			name:  "issue number argument",
			input: "23",
			output: EditOptions{
				IssueNumbers: []int{23},
				Interactive:  true,
			},
			wantsErr: false,
		},
		{
			name:  "title flag",
			input: "23 --title test",
			output: EditOptions{
				IssueNumbers: []int{23},
				Editable: prShared.Editable{
					Title: prShared.EditableString{
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
				IssueNumbers: []int{23},
				Editable: prShared.Editable{
					Body: prShared.EditableString{
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
				IssueNumbers: []int{23},
				Editable: prShared.Editable{
					Body: prShared.EditableString{
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
				IssueNumbers: []int{23},
				Editable: prShared.Editable{
					Body: prShared.EditableString{
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
			name:  "add-assignee flag",
			input: "23 --add-assignee monalisa,hubot",
			output: EditOptions{
				IssueNumbers: []int{23},
				Editable: prShared.Editable{
					Assignees: prShared.EditableAssignees{
						EditableSlice: prShared.EditableSlice{
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
				IssueNumbers: []int{23},
				Editable: prShared.Editable{
					Assignees: prShared.EditableAssignees{
						EditableSlice: prShared.EditableSlice{
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
				IssueNumbers: []int{23},
				Editable: prShared.Editable{
					Labels: prShared.EditableSlice{
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
				IssueNumbers: []int{23},
				Editable: prShared.Editable{
					Labels: prShared.EditableSlice{
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
				IssueNumbers: []int{23},
				Editable: prShared.Editable{
					Projects: prShared.EditableProjects{
						EditableSlice: prShared.EditableSlice{
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
				IssueNumbers: []int{23},
				Editable: prShared.Editable{
					Projects: prShared.EditableProjects{
						EditableSlice: prShared.EditableSlice{
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
				IssueNumbers: []int{23},
				Editable: prShared.Editable{
					Milestone: prShared.EditableString{
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
				IssueNumbers: []int{23},
				Editable: prShared.Editable{
					Milestone: prShared.EditableString{
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
		{
			name:  "add label to multiple issues",
			input: "23 34 --add-label bug",
			output: EditOptions{
				IssueNumbers: []int{23, 34},
				Editable: prShared.Editable{
					Labels: prShared.EditableSlice{
						Add:    []string{"bug"},
						Edited: true,
					},
				},
			},
			wantsErr: false,
		},
		{
			name: "argument is hash prefixed number",
			// Escaping is required here to avoid what I think is shellex treating it as a comment.
			input: "\\#23",
			output: EditOptions{
				IssueNumbers: []int{23},
				Interactive:  true,
			},
			wantsErr: false,
		},
		{
			name:  "argument is a URL",
			input: "https://github.com/cli/cli/issues/23",
			output: EditOptions{
				IssueNumbers: []int{23},
				Interactive:  true,
			},
			expectedBaseRepo: ghrepo.New("cli", "cli"),
			wantsErr:         false,
		},
		{
			name:     "URL arguments parse as different repos",
			input:    "https://github.com/cli/cli/issues/23 https://github.com/cli/go-gh/issues/23",
			wantsErr: true,
		},
		{
			name:     "interactive multiple issues",
			input:    "23 34",
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
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.output.IssueNumbers, gotOpts.IssueNumbers)
			assert.Equal(t, tt.output.Interactive, gotOpts.Interactive)
			assert.Equal(t, tt.output.Editable, gotOpts.Editable)
			if tt.expectedBaseRepo != nil {
				baseRepo, err := gotOpts.BaseRepo()
				require.NoError(t, err)
				require.True(
					t,
					ghrepo.IsSame(tt.expectedBaseRepo, baseRepo),
					"expected base repo %+v, got %+v", tt.expectedBaseRepo, baseRepo,
				)
			}
		})
	}
}

func Test_editRun(t *testing.T) {
	tests := []struct {
		name      string
		input     *EditOptions
		httpStubs func(*testing.T, *httpmock.Registry)
		stdout    string
		stderr    string
		wantErr   bool
	}{
		{
			name: "non-interactive",
			input: &EditOptions{
				IssueNumbers: []int{123},
				Interactive:  false,
				Editable: prShared.Editable{
					Title: prShared.EditableString{
						Value:  "new title",
						Edited: true,
					},
					Body: prShared.EditableString{
						Value:  "new body",
						Edited: true,
					},
					Assignees: prShared.EditableAssignees{
						EditableSlice: prShared.EditableSlice{
							Add:    []string{"monalisa", "hubot"},
							Remove: []string{"octocat"},
							Edited: true,
						},
					},
					Labels: prShared.EditableSlice{
						Add:    []string{"feature", "TODO", "bug"},
						Remove: []string{"docs"},
						Edited: true,
					},
					Projects: prShared.EditableProjects{
						EditableSlice: prShared.EditableSlice{
							Add:    []string{"Cleanup", "CleanupV2"},
							Remove: []string{"Roadmap", "RoadmapV2"},
							Edited: true,
						},
					},
					Milestone: prShared.EditableString{
						Value:  "GA",
						Edited: true,
					},
					Metadata: api.RepoMetadataResult{
						Labels: []api.RepoLabel{
							{Name: "docs", ID: "DOCSID"},
						},
					},
				},
				FetchOptions: prShared.FetchOptions,
			},
			httpStubs: func(t *testing.T, reg *httpmock.Registry) {
				mockIssueGet(t, reg)
				mockIssueProjectItemsGet(t, reg)
				mockRepoMetadata(t, reg)
				mockIssueUpdate(t, reg)
				mockIssueUpdateActorAssignees(t, reg)
				mockIssueUpdateLabels(t, reg)
				mockProjectV2ItemUpdate(t, reg)
			},
			stdout: "https://github.com/OWNER/REPO/issue/123\n",
		},
		{
			name: "non-interactive multiple issues",
			input: &EditOptions{
				IssueNumbers: []int{456, 123},
				Interactive:  false,
				Editable: prShared.Editable{
					Assignees: prShared.EditableAssignees{
						EditableSlice: prShared.EditableSlice{
							Add:    []string{"monalisa", "hubot"},
							Remove: []string{"octocat"},
							Edited: true,
						},
					},
					Labels: prShared.EditableSlice{
						Add:    []string{"feature", "TODO", "bug"},
						Remove: []string{"docs"},
						Edited: true,
					},
					Projects: prShared.EditableProjects{
						EditableSlice: prShared.EditableSlice{
							Add:    []string{"Cleanup", "CleanupV2"},
							Remove: []string{"Roadmap", "RoadmapV2"},
							Edited: true,
						},
					},
					Milestone: prShared.EditableString{
						Value:  "GA",
						Edited: true,
					},
				},
				FetchOptions: prShared.FetchOptions,
			},
			httpStubs: func(t *testing.T, reg *httpmock.Registry) {
				// Should only be one fetch of metadata.
				mockRepoMetadata(t, reg)
				// All other queries and mutations should be doubled.
				mockIssueNumberGet(t, reg, 123)
				mockIssueNumberGet(t, reg, 456)
				mockIssueProjectItemsGet(t, reg)
				mockIssueProjectItemsGet(t, reg)
				mockIssueUpdate(t, reg)
				mockIssueUpdate(t, reg)
				mockIssueUpdateActorAssignees(t, reg)
				mockIssueUpdateActorAssignees(t, reg)
				mockIssueUpdateLabels(t, reg)
				mockIssueUpdateLabels(t, reg)
				mockProjectV2ItemUpdate(t, reg)
				mockProjectV2ItemUpdate(t, reg)
			},
			stdout: heredoc.Doc(`
				https://github.com/OWNER/REPO/issue/123
				https://github.com/OWNER/REPO/issue/456
			`),
		},
		{
			name: "non-interactive multiple issues with fetch failures",
			input: &EditOptions{
				IssueNumbers: []int{123, 9999},
				Interactive:  false,
				Editable: prShared.Editable{
					Assignees: prShared.EditableAssignees{
						EditableSlice: prShared.EditableSlice{
							Add:    []string{"monalisa", "hubot"},
							Remove: []string{"octocat"},
							Edited: true,
						},
					},
					Labels: prShared.EditableSlice{
						Add:    []string{"feature", "TODO", "bug"},
						Remove: []string{"docs"},
						Edited: true,
					},
					Projects: prShared.EditableProjects{
						EditableSlice: prShared.EditableSlice{
							Add:    []string{"Cleanup", "CleanupV2"},
							Remove: []string{"Roadmap", "RoadmapV2"},
							Edited: true,
						},
					},
					Milestone: prShared.EditableString{
						Value:  "GA",
						Edited: true,
					},
				},
				FetchOptions: prShared.FetchOptions,
			},
			httpStubs: func(t *testing.T, reg *httpmock.Registry) {
				mockIssueNumberGet(t, reg, 123)
				reg.Register(
					httpmock.GraphQL(`query IssueByNumber\b`),
					httpmock.StringResponse(`
						{ "errors": [
							{
								"type": "NOT_FOUND",
								"message": "Could not resolve to an Issue with the number of 9999."
							}
						] }`),
				)
			},
			wantErr: true,
		},
		{
			name: "non-interactive multiple issues with update failures",
			input: &EditOptions{
				IssueNumbers: []int{123, 456},
				Interactive:  false,
				Editable: prShared.Editable{
					Assignees: prShared.EditableAssignees{
						EditableSlice: prShared.EditableSlice{
							Add:    []string{"monalisa", "hubot"},
							Remove: []string{"octocat"},
							Edited: true,
						},
					},
					Milestone: prShared.EditableString{
						Value:  "GA",
						Edited: true,
					},
				},
				FetchOptions: prShared.FetchOptions,
			},
			httpStubs: func(t *testing.T, reg *httpmock.Registry) {
				// Should only be one fetch of metadata.
				reg.Register(
					httpmock.GraphQL(`query RepositoryAssignableActors\b`),
					httpmock.StringResponse(`
					{ "data": { "repository": { "suggestedActors": {
						"nodes": [
							{ "login": "hubot", "id": "HUBOTID", "__typename": "Bot" },
							{ "login": "MonaLisa", "id": "MONAID", "__typename": "User" }
						],
						"pageInfo": { "hasNextPage": false, "endCursor": "Mg" }
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
				// All other queries should be doubled.
				mockIssueNumberGet(t, reg, 123)
				mockIssueNumberGet(t, reg, 456)
				// Updating 123 should succeed.
				reg.Register(
					httpmock.GraphQLMutationMatcher(`mutation ReplaceActorsForAssignable\b`, func(m map[string]interface{}) bool {
						return m["assignableId"] == "123"
					}),
					httpmock.GraphQLMutation(`
					{ "data": { "replaceActorsForAssignable": { "__typename": "" } } }`,
						func(inputs map[string]interface{}) {}),
				)
				reg.Register(
					httpmock.GraphQLMutationMatcher(`mutation IssueUpdate\b`, func(m map[string]interface{}) bool {
						return m["id"] == "123"
					}),
					httpmock.GraphQLMutation(`
							{ "data": { "updateIssue": { "__typename": "" } } }`,
						func(inputs map[string]interface{}) {}),
				)
				// Updating 456 should fail.
				reg.Register(
					httpmock.GraphQLMutationMatcher(`mutation ReplaceActorsForAssignable\b`, func(m map[string]interface{}) bool {
						return m["assignableId"] == "456"
					}),
					httpmock.GraphQLMutation(`
							{ "errors": [ { "message": "test error" } ] }`,
						func(inputs map[string]interface{}) {}),
				)
			},
			stdout: heredoc.Doc(`
				https://github.com/OWNER/REPO/issue/123
			`),
			stderr:  `failed to update https://github.com/OWNER/REPO/issue/456:.*test error`,
			wantErr: true,
		},
		{
			name: "interactive",
			input: &EditOptions{
				IssueNumbers: []int{123},
				Interactive:  true,
				FieldsToEditSurvey: func(p prShared.EditPrompter, eo *prShared.Editable) error {
					eo.Title.Edited = true
					eo.Body.Edited = true
					eo.Assignees.Edited = true
					eo.Labels.Edited = true
					eo.Projects.Edited = true
					eo.Milestone.Edited = true
					return nil
				},
				EditFieldsSurvey: func(p prShared.EditPrompter, eo *prShared.Editable, _ string) error {
					eo.Title.Value = "new title"
					eo.Body.Value = "new body"
					eo.Assignees.Value = []string{"monalisa", "hubot"}
					eo.Labels.Value = []string{"feature", "TODO", "bug"}
					eo.Labels.Add = []string{"feature", "TODO", "bug"}
					eo.Labels.Remove = []string{"docs"}
					eo.Projects.Value = []string{"Cleanup", "CleanupV2"}
					eo.Milestone.Value = "GA"
					return nil
				},
				FetchOptions:    prShared.FetchOptions,
				DetermineEditor: func() (string, error) { return "vim", nil },
			},
			httpStubs: func(t *testing.T, reg *httpmock.Registry) {
				mockIssueGet(t, reg)
				mockIssueProjectItemsGet(t, reg)
				mockRepoMetadata(t, reg)
				mockIssueUpdate(t, reg)
				mockIssueUpdateActorAssignees(t, reg)
				mockIssueUpdateLabels(t, reg)
				mockProjectV2ItemUpdate(t, reg)
			},
			stdout: "https://github.com/OWNER/REPO/issue/123\n",
		},
		{
			name: "interactive prompts with actor assignee display names when actors available",
			input: &EditOptions{
				IssueNumbers: []int{123},
				Interactive:  true,
				FieldsToEditSurvey: func(p prShared.EditPrompter, eo *prShared.Editable) error {
					eo.Assignees.Edited = true
					return nil
				},
				EditFieldsSurvey: func(p prShared.EditPrompter, eo *prShared.Editable, _ string) error {
					// Checking that the display name is being used in the prompt.
					require.Equal(t, []string{"hubot"}, eo.Assignees.Default)
					require.Equal(t, []string{"hubot"}, eo.Assignees.DefaultLogins)

					// Adding MonaLisa as PR assignee, should preserve hubot.
					eo.Assignees.Value = []string{"hubot", "MonaLisa (Mona Display Name)"}
					return nil
				},
				FetchOptions:    prShared.FetchOptions,
				DetermineEditor: func() (string, error) { return "vim", nil },
			},
			httpStubs: func(t *testing.T, reg *httpmock.Registry) {
				mockIsssueNumberGetWithAssignedActors(t, reg, 123)
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
				mockIssueUpdate(t, reg)
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
			stdout: "https://github.com/OWNER/REPO/issue/123\n",
		},
		{
			name: "interactive prompts with user assignee logins when actors unavailable",
			input: &EditOptions{
				IssueNumbers: []int{123},
				Interactive:  true,
				FieldsToEditSurvey: func(p prShared.EditPrompter, eo *prShared.Editable) error {
					eo.Assignees.Edited = true
					return nil
				},
				EditFieldsSurvey: func(p prShared.EditPrompter, eo *prShared.Editable, _ string) error {
					// Checking that only the login is used in the prompt (no display name)
					require.Equal(t, eo.Assignees.Default, []string{"hubot", "MonaLisa"})

					// Mocking a selection of only MonaLisa in the prompt.
					eo.Assignees.Value = []string{"MonaLisa"}
					return nil
				},
				FetchOptions:    prShared.FetchOptions,
				DetermineEditor: func() (string, error) { return "vim", nil },
				Detector:        &fd.DisabledDetectorMock{},
			},
			httpStubs: func(t *testing.T, reg *httpmock.Registry) {
				reg.Register(
					httpmock.GraphQL(`query IssueByNumber\b`),
					httpmock.StringResponse(fmt.Sprintf(`
                        { "data": { "repository": { "hasIssuesEnabled": true, "issue": {
							"id": "%[1]d",
							"number": %[1]d,
                            "url": "https://github.com/OWNER/REPO/issue/123",
                            "assignees": {
                                "nodes": [
                                    {
                                        "id": "HUBOTID",
                                        "login": "hubot",
										"name": ""
                                    },
                                    {
                                        "id": "MONAID",
                                        "login": "MonaLisa",
										"name": "Mona Display Name"
                                    }
                                ],
                                "totalCount": 2
                            }
                        } } } }`, 123)),
				)
				reg.Register(
					httpmock.GraphQL(`query RepositoryAssignableUsers\b`),
					httpmock.StringResponse(`
					{ "data": { "repository": { "assignableUsers": {
						"nodes": [
							{ "login": "hubot", "id": "HUBOTID", "name": "" },
							{ "login": "MonaLisa", "id": "MONAID", "name": "Mona Display Name" }
						],
						"pageInfo": { "hasNextPage": false }
					} } } }
					`))
				reg.Register(
					httpmock.GraphQL(`mutation IssueUpdate\b`),
					httpmock.GraphQLMutation(`
								{ "data": { "updateIssue": { "__typename": "" } } }`,
						func(inputs map[string]interface{}) {
							// Checking that we still assigned the expected ID.
							require.Contains(t, inputs["assigneeIds"], "MONAID")
						}),
				)
			},
			stdout: "https://github.com/OWNER/REPO/issue/123\n",
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
			tt.httpStubs(t, reg)

			httpClient := func() (*http.Client, error) { return &http.Client{Transport: reg}, nil }
			baseRepo := func() (ghrepo.Interface, error) { return ghrepo.New("OWNER", "REPO"), nil }

			tt.input.IO = ios
			tt.input.HttpClient = httpClient
			tt.input.BaseRepo = baseRepo

			err := editRun(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.stdout, stdout.String())
			// Use regex match since mock errors and service errors will differ.
			assert.Regexp(t, tt.stderr, stderr.String())
		})
	}
}

func mockIssueGet(_ *testing.T, reg *httpmock.Registry) {
	mockIssueNumberGet(nil, reg, 123)
}

func mockIssueNumberGet(_ *testing.T, reg *httpmock.Registry, number int) {
	reg.Register(
		httpmock.GraphQL(`query IssueByNumber\b`),
		httpmock.StringResponse(fmt.Sprintf(`
			{ "data": { "repository": { "hasIssuesEnabled": true, "issue": {
				"id": "%[1]d",
				"number": %[1]d,
				"url": "https://github.com/OWNER/REPO/issue/%[1]d",
				"labels": {
					"nodes": [
						{ "id": "DOCSID", "name": "docs" }
					], "totalCount": 1
				},
				"projectCards": {
					"nodes": [
						{ "project": { "name": "Roadmap" } }
					], "totalCount": 1
				}
			} } } }`, number)),
	)
}

func mockIsssueNumberGetWithAssignedActors(_ *testing.T, reg *httpmock.Registry, number int) {
	reg.Register(
		httpmock.GraphQL(`query IssueByNumber\b`),
		httpmock.StringResponse(fmt.Sprintf(`
			{ "data": { "repository": { "hasIssuesEnabled": true, "issue": {
				"id": "%[1]d",
				"number": %[1]d,
				"url": "https://github.com/OWNER/REPO/issue/%[1]d",
				"assignedActors": {
					"nodes": [
						{
							"id": "HUBOTID",
							"login": "hubot",
							"__typename": "Bot"
						}
					],
					"totalCount": 1
				}
			} } } }`, number)),
	)
}

func mockIssueProjectItemsGet(_ *testing.T, reg *httpmock.Registry) {
	reg.Register(
		httpmock.GraphQL(`query IssueProjectItems\b`),
		httpmock.StringResponse(`
			{ "data": { "repository": { "issue": {
				"projectItems": {
					"nodes": [
						{ "id": "ITEMID", "project": { "title": "RoadmapV2" } }
					]
				}
			} } } }`),
	)
}

func mockRepoMetadata(_ *testing.T, reg *httpmock.Registry) {
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
}

func mockIssueUpdate(t *testing.T, reg *httpmock.Registry) {
	reg.Register(
		httpmock.GraphQL(`mutation IssueUpdate\b`),
		httpmock.GraphQLMutation(`
				{ "data": { "updateIssue": { "__typename": "" } } }`,
			func(inputs map[string]interface{}) {}),
	)
}

func mockIssueUpdateActorAssignees(t *testing.T, reg *httpmock.Registry) {
	reg.Register(
		httpmock.GraphQL(`mutation ReplaceActorsForAssignable\b`),
		httpmock.GraphQLMutation(`
		{ "data": { "replaceActorsForAssignable": { "__typename": "" } } }`,
			func(inputs map[string]interface{}) {}),
	)
}

func mockIssueUpdateLabels(t *testing.T, reg *httpmock.Registry) {
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

func mockProjectV2ItemUpdate(t *testing.T, reg *httpmock.Registry) {
	reg.Register(
		httpmock.GraphQL(`mutation UpdateProjectV2Items\b`),
		httpmock.GraphQLMutation(`
		{ "data": { "add_000": { "item": { "id": "1" } }, "delete_001": { "item": { "id": "2" } } } }`,
			func(inputs map[string]interface{}) {}),
	)
}

func TestActorIsAssignable(t *testing.T) {
	t.Run("when actors are assignable, query includes assignedActors", func(t *testing.T) {
		ios, _, _, _ := iostreams.Test()

		reg := &httpmock.Registry{}
		reg.Register(
			httpmock.GraphQL(`assignedActors`),
			// Simulate a GraphQL error to early exit the test.
			httpmock.StatusStringResponse(500, ""),
		)

		_, cmdTeardown := run.Stub()
		defer cmdTeardown(t)

		// Ignore the error because we don't care.
		_ = editRun(&EditOptions{
			IO: ios,
			HttpClient: func() (*http.Client, error) {
				return &http.Client{Transport: reg}, nil
			},
			BaseRepo: func() (ghrepo.Interface, error) {
				return ghrepo.New("OWNER", "REPO"), nil
			},
			Detector:     &fd.EnabledDetectorMock{},
			IssueNumbers: []int{123},
			Editable: prShared.Editable{
				Assignees: prShared.EditableAssignees{
					EditableSlice: prShared.EditableSlice{
						Add:    []string{"monalisa", "octocat"},
						Edited: true,
					},
				},
			},
		})

		reg.Verify(t)
	})

	t.Run("when actors are not assignable, query includes assignees instead", func(t *testing.T) {
		ios, _, _, _ := iostreams.Test()

		reg := &httpmock.Registry{}
		// This test should NOT include assignedActors in the query
		reg.Exclude(t, httpmock.GraphQL(`assignedActors`))
		// It should include the regular assignees field
		reg.Register(
			httpmock.GraphQL(`assignees`),
			// Simulate a GraphQL error to early exit the test.
			httpmock.StatusStringResponse(500, ""),
		)

		_, cmdTeardown := run.Stub()
		defer cmdTeardown(t)

		// Ignore the error because we're not really interested in it.
		_ = editRun(&EditOptions{
			IO: ios,
			HttpClient: func() (*http.Client, error) {
				return &http.Client{Transport: reg}, nil
			},
			BaseRepo: func() (ghrepo.Interface, error) {
				return ghrepo.New("OWNER", "REPO"), nil
			},
			Detector:     &fd.DisabledDetectorMock{},
			IssueNumbers: []int{123},
			Editable: prShared.Editable{
				Assignees: prShared.EditableAssignees{
					EditableSlice: prShared.EditableSlice{
						Add:    []string{"monalisa", "octocat"},
						Edited: true,
					},
				},
			},
		})

		reg.Verify(t)
	})
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

		_, cmdTeardown := run.Stub()
		defer cmdTeardown(t)

		// Ignore the error because we have no way to really stub it without
		// fully stubbing a GQL error structure in the request body.
		_ = editRun(&EditOptions{
			IO: ios,
			HttpClient: func() (*http.Client, error) {
				return &http.Client{Transport: reg}, nil
			},
			BaseRepo: func() (ghrepo.Interface, error) {
				return ghrepo.New("OWNER", "REPO"), nil
			},
			Detector: &fd.EnabledDetectorMock{},

			IssueNumbers: []int{123},
			Editable: prShared.Editable{
				Projects: prShared.EditableProjects{
					EditableSlice: prShared.EditableSlice{
						Add:    []string{"Test Project"},
						Edited: true,
					},
				},
			},
		})

		// Verify that our request contained projectCards
		reg.Verify(t)
	})

	t.Run("when projects v1 is not supported, is not included in query", func(t *testing.T) {
		ios, _, _, _ := iostreams.Test()

		reg := &httpmock.Registry{}
		reg.Exclude(t, httpmock.GraphQL(`projectCards`))

		reg.Register(
			httpmock.GraphQL(`.*`),
			// Simulate a GraphQL error to early exit the test.
			httpmock.StatusStringResponse(500, ""),
		)

		_, cmdTeardown := run.Stub()
		defer cmdTeardown(t)

		// Ignore the error because we're not really interested in it.
		_ = editRun(&EditOptions{
			IO: ios,
			HttpClient: func() (*http.Client, error) {
				return &http.Client{Transport: reg}, nil
			},
			BaseRepo: func() (ghrepo.Interface, error) {
				return ghrepo.New("OWNER", "REPO"), nil
			},
			Detector: &fd.DisabledDetectorMock{},

			IssueNumbers: []int{123},
			Editable: prShared.Editable{
				Projects: prShared.EditableProjects{
					EditableSlice: prShared.EditableSlice{
						Add:    []string{"Test Project"},
						Edited: true,
					},
				},
			},
		})

		// Verify that our request contained projectCards
		reg.Verify(t)
	})
}
