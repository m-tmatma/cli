package shared

import (
	"errors"
	"testing"

	"github.com/cli/cli/v2/api"
	"github.com/cli/cli/v2/internal/gh"
	"github.com/cli/cli/v2/internal/ghrepo"
	"github.com/cli/cli/v2/internal/prompter"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type metadataFetcher struct {
	metadataResult *api.RepoMetadataResult
}

func (mf *metadataFetcher) RepoMetadataFetch(input api.RepoMetadataInput) (*api.RepoMetadataResult, error) {
	return mf.metadataResult, nil
}

func TestMetadataSurvey_selectAll(t *testing.T) {
	ios, _, stdout, stderr := iostreams.Test()

	repo := ghrepo.New("OWNER", "REPO")

	fetcher := &metadataFetcher{
		metadataResult: &api.RepoMetadataResult{
			AssignableUsers: []api.AssignableUser{
				api.NewAssignableUser("", "hubot", ""),
				api.NewAssignableUser("", "monalisa", ""),
			},
			Labels: []api.RepoLabel{
				{Name: "help wanted"},
				{Name: "good first issue"},
			},
			Projects: []api.RepoProject{
				{Name: "Huge Refactoring"},
				{Name: "The road to 1.0"},
			},
			Milestones: []api.RepoMilestone{
				{Title: "1.2 patch release"},
			},
		},
	}

	pm := prompter.NewMockPrompter(t)
	pm.RegisterMultiSelect("What would you like to add?",
		[]string{}, []string{"Reviewers", "Assignees", "Labels", "Projects", "Milestone"}, func(_ string, _, _ []string) ([]int, error) {
			return []int{0, 1, 2, 3, 4}, nil
		})
	pm.RegisterMultiSelect("Reviewers", []string{}, []string{"hubot", "monalisa"}, func(_ string, _, _ []string) ([]int, error) {
		return []int{1}, nil
	})
	pm.RegisterMultiSelect("Assignees", []string{}, []string{"hubot", "monalisa"}, func(_ string, _, _ []string) ([]int, error) {
		return []int{0}, nil
	})
	pm.RegisterMultiSelect("Labels", []string{}, []string{"help wanted", "good first issue"}, func(_ string, _, _ []string) ([]int, error) {
		return []int{1}, nil
	})
	pm.RegisterMultiSelect("Projects", []string{}, []string{"Huge Refactoring", "The road to 1.0"}, func(_ string, _, _ []string) ([]int, error) {
		return []int{1}, nil
	})
	pm.RegisterSelect("Milestone", []string{"(none)", "1.2 patch release"}, func(_, _ string, _ []string) (int, error) {
		return 0, nil
	})

	state := &IssueMetadataState{
		Assignees: []string{"hubot"},
		Type:      PRMetadata,
	}
	err := MetadataSurvey(pm, ios, repo, fetcher, state, gh.ProjectsV1Supported)
	assert.NoError(t, err)

	assert.Equal(t, "", stdout.String())
	assert.Equal(t, "", stderr.String())

	assert.Equal(t, []string{"hubot"}, state.Assignees)
	assert.Equal(t, []string{"monalisa"}, state.Reviewers)
	assert.Equal(t, []string{"good first issue"}, state.Labels)
	assert.Equal(t, []string{"The road to 1.0"}, state.ProjectTitles)
	assert.Equal(t, []string{}, state.Milestones)
}

func TestMetadataSurvey_keepExisting(t *testing.T) {
	ios, _, stdout, stderr := iostreams.Test()

	repo := ghrepo.New("OWNER", "REPO")

	fetcher := &metadataFetcher{
		metadataResult: &api.RepoMetadataResult{
			Labels: []api.RepoLabel{
				{Name: "help wanted"},
				{Name: "good first issue"},
			},
			Projects: []api.RepoProject{
				{Name: "Huge Refactoring"},
				{Name: "The road to 1.0"},
			},
		},
	}

	pm := prompter.NewMockPrompter(t)
	pm.RegisterMultiSelect("What would you like to add?", []string{}, []string{"Assignees", "Labels", "Projects", "Milestone"}, func(_ string, _, _ []string) ([]int, error) {
		return []int{1, 2}, nil
	})
	pm.RegisterMultiSelect("Labels", []string{}, []string{"help wanted", "good first issue"}, func(_ string, _, _ []string) ([]int, error) {
		return []int{1}, nil
	})
	pm.RegisterMultiSelect("Projects", []string{}, []string{"Huge Refactoring", "The road to 1.0"}, func(_ string, _, _ []string) ([]int, error) {
		return []int{1}, nil
	})

	state := &IssueMetadataState{
		Assignees: []string{"hubot"},
	}

	err := MetadataSurvey(pm, ios, repo, fetcher, state, gh.ProjectsV1Supported)
	assert.NoError(t, err)

	assert.Equal(t, "", stdout.String())
	assert.Equal(t, "", stderr.String())

	assert.Equal(t, []string{"hubot"}, state.Assignees)
	assert.Equal(t, []string{"good first issue"}, state.Labels)
	assert.Equal(t, []string{"The road to 1.0"}, state.ProjectTitles)
}

// TODO projectsV1Deprecation
// Remove this test and projectsV1MetadataFetcherSpy
func TestMetadataSurveyProjectV1Deprecation(t *testing.T) {
	t.Run("when projectsV1 is supported, requests projectsV1", func(t *testing.T) {
		ios, _, _, _ := iostreams.Test()
		repo := ghrepo.New("OWNER", "REPO")

		fetcher := &projectsV1MetadataFetcherSpy{}
		pm := prompter.NewMockPrompter(t)
		pm.RegisterMultiSelect("What would you like to add?", []string{}, []string{"Assignees", "Labels", "Projects", "Milestone"}, func(_ string, _, options []string) ([]int, error) {
			i, err := prompter.IndexFor(options, "Projects")
			require.NoError(t, err)
			return []int{i}, nil
		})
		pm.RegisterMultiSelect("Projects", []string{}, []string{"Huge Refactoring"}, func(_ string, _, _ []string) ([]int, error) {
			return []int{0}, nil
		})

		err := MetadataSurvey(pm, ios, repo, fetcher, &IssueMetadataState{}, gh.ProjectsV1Supported)
		require.ErrorContains(t, err, "expected test error")

		require.True(t, fetcher.projectsV1Requested, "expected projectsV1 to be requested")
	})

	t.Run("when projectsV1 is supported, does not request projectsV1", func(t *testing.T) {
		ios, _, _, _ := iostreams.Test()
		repo := ghrepo.New("OWNER", "REPO")

		fetcher := &projectsV1MetadataFetcherSpy{}
		pm := prompter.NewMockPrompter(t)
		pm.RegisterMultiSelect("What would you like to add?", []string{}, []string{"Assignees", "Labels", "Projects", "Milestone"}, func(_ string, _, options []string) ([]int, error) {
			i, err := prompter.IndexFor(options, "Projects")
			require.NoError(t, err)
			return []int{i}, nil
		})
		pm.RegisterMultiSelect("Projects", []string{}, []string{"Huge Refactoring"}, func(_ string, _, _ []string) ([]int, error) {
			return []int{0}, nil
		})

		err := MetadataSurvey(pm, ios, repo, fetcher, &IssueMetadataState{}, gh.ProjectsV1Unsupported)
		require.ErrorContains(t, err, "expected test error")

		require.False(t, fetcher.projectsV1Requested, "expected projectsV1 not to be requested")
	})
}

type projectsV1MetadataFetcherSpy struct {
	projectsV1Requested bool
}

func (mf *projectsV1MetadataFetcherSpy) RepoMetadataFetch(input api.RepoMetadataInput) (*api.RepoMetadataResult, error) {
	if input.ProjectsV1 {
		mf.projectsV1Requested = true
	}
	return nil, errors.New("expected test error")
}

func TestTitledEditSurvey_cleanupHint(t *testing.T) {
	var editorInitialText string
	editor := &testEditor{
		edit: func(s string) (string, error) {
			editorInitialText = s
			return `editedTitle
editedBody
------------------------ >8 ------------------------

Please Enter the title on the first line and the body on subsequent lines.
Lines below dotted lines will be ignored, and an empty title aborts the creation process.`, nil
		},
	}

	title, body, err := TitledEditSurvey(editor)("initialTitle", "initialBody")
	assert.NoError(t, err)

	assert.Equal(t, `initialTitle
initialBody
------------------------ >8 ------------------------

Please Enter the title on the first line and the body on subsequent lines.
Lines below dotted lines will be ignored, and an empty title aborts the creation process.`, editorInitialText)
	assert.Equal(t, "editedTitle", title)
	assert.Equal(t, "editedBody", body)
}

type testEditor struct {
	edit func(string) (string, error)
}

func (e testEditor) Edit(filename, text string) (string, error) {
	return e.edit(text)
}

func TestTitleSurvey(t *testing.T) {
	tests := []struct {
		name               string
		prompterMockInputs []string
		expectedTitle      string
		expectStderr       bool
	}{
		{
			name:               "title provided",
			prompterMockInputs: []string{"title"},
			expectedTitle:      "title",
		},
		{
			name:               "first input empty",
			prompterMockInputs: []string{"", "title"},
			expectedTitle:      "title",
			expectStderr:       true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			io, _, _, stderr := iostreams.Test()
			pm := prompter.NewMockPrompter(t)
			for _, input := range tt.prompterMockInputs {
				pm.RegisterInput("Title (required)", func(string, string) (string, error) {
					return input, nil
				})
			}

			state := &IssueMetadataState{}
			err := TitleSurvey(pm, io, state)

			assert.NoError(t, err)
			assert.Equal(t, tt.expectedTitle, state.Title)
			if tt.expectStderr {
				assert.Equal(t, "X Title cannot be blank\n", stderr.String())
			}
		})
	}
}
