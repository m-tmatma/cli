package shared

import (
	"net/http"
	"net/url"
	"reflect"
	"testing"

	"github.com/cli/cli/v2/api"
	"github.com/cli/cli/v2/internal/gh"
	"github.com/cli/cli/v2/internal/ghrepo"
	"github.com/cli/cli/v2/pkg/httpmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_listURLWithQuery(t *testing.T) {
	trueBool := true
	falseBool := false

	type args struct {
		listURL string
		options FilterOptions
	}

	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "blank",
			args: args{
				listURL: "https://example.com/path?a=b",
				options: FilterOptions{
					Entity: "issue",
					State:  "open",
				},
			},
			want:    "https://example.com/path?a=b&q=state%3Aopen+type%3Aissue",
			wantErr: false,
		},
		{
			name: "draft",
			args: args{
				listURL: "https://example.com/path",
				options: FilterOptions{
					Entity: "pr",
					State:  "open",
					Draft:  &trueBool,
				},
			},
			want:    "https://example.com/path?q=draft%3Atrue+state%3Aopen+type%3Apr",
			wantErr: false,
		},
		{
			name: "non-draft",
			args: args{
				listURL: "https://example.com/path",
				options: FilterOptions{
					Entity: "pr",
					State:  "open",
					Draft:  &falseBool,
				},
			},
			want:    "https://example.com/path?q=draft%3Afalse+state%3Aopen+type%3Apr",
			wantErr: false,
		},
		{
			name: "all",
			args: args{
				listURL: "https://example.com/path",
				options: FilterOptions{
					Entity:     "issue",
					State:      "open",
					Assignee:   "bo",
					Author:     "ka",
					BaseBranch: "trunk",
					HeadBranch: "bug-fix",
					Mention:    "nu",
				},
			},
			want:    "https://example.com/path?q=assignee%3Abo+author%3Aka+base%3Atrunk+head%3Abug-fix+mentions%3Anu+state%3Aopen+type%3Aissue",
			wantErr: false,
		},
		{
			name: "spaces in values",
			args: args{
				listURL: "https://example.com/path",
				options: FilterOptions{
					Entity:    "pr",
					State:     "open",
					Labels:    []string{"docs", "help wanted"},
					Milestone: `Codename "What Was Missing"`,
				},
			},
			want:    "https://example.com/path?q=label%3A%22help+wanted%22+label%3Adocs+milestone%3A%22Codename+%5C%22What+Was+Missing%5C%22%22+state%3Aopen+type%3Apr",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ListURLWithQuery(tt.args.listURL, tt.args.options)
			if (err != nil) != tt.wantErr {
				t.Errorf("listURLWithQuery() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("listURLWithQuery() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMeReplacer_Replace(t *testing.T) {
	rtSuccess := &httpmock.Registry{}
	rtSuccess.Register(
		httpmock.GraphQL(`query UserCurrent\b`),
		httpmock.StringResponse(`
		{ "data": {
			"viewer": { "login": "ResolvedLogin" }
		} }
		`),
	)

	rtFailure := &httpmock.Registry{}
	rtFailure.Register(
		httpmock.GraphQL(`query UserCurrent\b`),
		httpmock.StatusStringResponse(500, `
		{ "data": {
			"viewer": { }
		} }
		`),
	)

	type args struct {
		logins []string
		client *api.Client
		repo   ghrepo.Interface
	}
	tests := []struct {
		name    string
		args    args
		verify  func(t httpmock.Testing)
		want    []string
		wantErr bool
	}{
		{
			name: "succeeds resolving the userlogin",
			args: args{
				client: api.NewClientFromHTTP(&http.Client{Transport: rtSuccess}),
				repo:   ghrepo.New("OWNER", "REPO"),
				logins: []string{"some", "@me", "other"},
			},
			verify: rtSuccess.Verify,
			want:   []string{"some", "ResolvedLogin", "other"},
		},
		{
			name: "fails resolving the userlogin",
			args: args{
				client: api.NewClientFromHTTP(&http.Client{Transport: rtFailure}),
				repo:   ghrepo.New("OWNER", "REPO"),
				logins: []string{"some", "@me", "other"},
			},
			verify:  rtFailure.Verify,
			want:    []string(nil),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			me := NewMeReplacer(tt.args.client, tt.args.repo.RepoHost())
			got, err := me.ReplaceSlice(tt.args.logins)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReplaceAtMeLogin() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ReplaceAtMeLogin() = %v, want %v", got, tt.want)
			}

			if tt.verify != nil {
				tt.verify(t)
			}
		})
	}
}

func TestCopilotReplacer_ReplaceSlice(t *testing.T) {
	type args struct {
		handles []string
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			name: "replaces @copilot with copilot-swe-agent",
			args: args{
				handles: []string{"monalisa", "@copilot", "hubot"},
			},
			want: []string{"monalisa", "copilot-swe-agent", "hubot"},
		},
		{
			name: "handles no @copilot mentions",
			args: args{
				handles: []string{"monalisa", "user", "hubot"},
			},
			want: []string{"monalisa", "user", "hubot"},
		},
		{
			name: "replaces multiple @copilot mentions",
			args: args{
				handles: []string{"@copilot", "user", "@copilot"},
			},
			want: []string{"copilot-swe-agent", "user", "copilot-swe-agent"},
		},
		{
			name: "handles @copilot case-insensitively",
			args: args{
				handles: []string{"@Copilot", "user", "@CoPiLoT"},
			},
			want: []string{"copilot-swe-agent", "user", "copilot-swe-agent"},
		},
		{
			name: "handles nil slice",
			args: args{
				handles: nil,
			},
			want: []string{},
		},
		{
			name: "handles empty slice",
			args: args{
				handles: []string{},
			},
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewCopilotReplacer()
			got := r.ReplaceSlice(tt.args.handles)
			require.Equal(t, tt.want, got)
		})
	}
}

func Test_QueryHasStateClause(t *testing.T) {
	tests := []struct {
		searchQuery string
		hasState    bool
	}{
		{
			searchQuery: "is:closed is:merged",
			hasState:    true,
		},
		{
			searchQuery: "author:mislav",
			hasState:    false,
		},
		{
			searchQuery: "assignee:g14a mentions:vilmibm",
			hasState:    false,
		},
		{
			searchQuery: "merged:>2021-05-20",
			hasState:    true,
		},
		{
			searchQuery: "state:merged state:open",
			hasState:    true,
		},
		{
			searchQuery: "assignee:g14a is:closed",
			hasState:    true,
		},
		{
			searchQuery: "state:closed label:bug",
			hasState:    true,
		},
	}
	for _, tt := range tests {
		gotState := QueryHasStateClause(tt.searchQuery)
		assert.Equal(t, tt.hasState, gotState)
	}
}

func Test_WithPrAndIssueQueryParams(t *testing.T) {
	type args struct {
		baseURL string
		state   IssueMetadataState
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "blank",
			args: args{
				baseURL: "",
				state:   IssueMetadataState{},
			},
			want: "?body=",
		},
		{
			name: "no values",
			args: args{
				baseURL: "http://example.com/hey",
				state:   IssueMetadataState{},
			},
			want: "http://example.com/hey?body=",
		},
		{
			name: "title and body",
			args: args{
				baseURL: "http://example.com/hey",
				state: IssueMetadataState{
					Title: "my title",
					Body:  "my bodeh",
				},
			},
			want: "http://example.com/hey?body=my+bodeh&title=my+title",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := WithPrAndIssueQueryParams(nil, nil, tt.args.baseURL, tt.args.state, gh.ProjectsV1Supported)
			if (err != nil) != tt.wantErr {
				t.Errorf("WithPrAndIssueQueryParams() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("WithPrAndIssueQueryParams() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TODO projectsV1Deprecation
// Remove this test.
func TestWithPrAndIssueQueryParamsProjectsV1Deprecation(t *testing.T) {
	t.Run("when projectsV1 is supported, requests them", func(t *testing.T) {
		reg := &httpmock.Registry{}
		client := api.NewClientFromHTTP(&http.Client{
			Transport: reg,
		})

		repo, _ := ghrepo.FromFullName("OWNER/REPO")

		reg.Register(
			httpmock.GraphQL(`query RepositoryProjectList\b`),
			httpmock.StringResponse(`
		{ "data": { "repository": { "projects": {
			"nodes": [],
			"pageInfo": { "hasNextPage": false }
		} } } }
		`))
		reg.Register(
			httpmock.GraphQL(`query OrganizationProjectList\b`),
			httpmock.StringResponse(`
		{ "data": { "organization": { "projects": {
			"nodes": [
				{ "name": "Triage", "id": "TRIAGEID", "resourcePath": "/orgs/ORG/projects/1"  }
			],
			"pageInfo": { "hasNextPage": false }
		} } } }
		`))
		reg.Register(
			httpmock.GraphQL(`query RepositoryProjectV2List\b`),
			httpmock.StringResponse(`
		{ "data": { "repository": { "projectsV2": {
			"nodes": [],
			"pageInfo": { "hasNextPage": false }
		} } } }
		`))
		reg.Register(
			httpmock.GraphQL(`query OrganizationProjectV2List\b`),
			httpmock.StringResponse(`
		{ "data": { "organization": { "projectsV2": {
			"nodes": [],
			"pageInfo": { "hasNextPage": false }
		} } } }
		`))
		reg.Register(
			httpmock.GraphQL(`query UserProjectV2List\b`),
			httpmock.StringResponse(`
		{ "data": { "viewer": { "projectsV2": {
			"nodes": [],
			"pageInfo": { "hasNextPage": false }
		} } } }
		`))

		u, err := WithPrAndIssueQueryParams(
			client,
			repo,
			"http://example.com/hey",
			IssueMetadataState{
				ProjectTitles: []string{"Triage"},
			},
			gh.ProjectsV1Supported,
		)
		require.NoError(t, err)

		url, err := url.Parse(u)
		require.NoError(t, err)

		require.Equal(
			t,
			url.Query().Get("projects"),
			"ORG/1",
		)
	})

	t.Run("when projectsV1 is not supported, does not request them", func(t *testing.T) {
		reg := &httpmock.Registry{}
		client := api.NewClientFromHTTP(&http.Client{
			Transport: reg,
		})

		repo, _ := ghrepo.FromFullName("OWNER/REPO")

		reg.Exclude(
			t,
			httpmock.GraphQL(`query RepositoryProjectList\b`),
		)
		reg.Exclude(
			t,
			httpmock.GraphQL(`query OrganizationProjectList\b`),
		)

		reg.Register(
			httpmock.GraphQL(`query RepositoryProjectV2List\b`),
			httpmock.StringResponse(`
		{ "data": { "repository": { "projectsV2": {
			"nodes": [],
			"pageInfo": { "hasNextPage": false }
		} } } }
		`))
		reg.Register(
			httpmock.GraphQL(`query OrganizationProjectV2List\b`),
			httpmock.StringResponse(`
		{ "data": { "organization": { "projectsV2": {
			"nodes": [
				{ "title": "TriageV2", "id": "TRIAGEV2ID", "resourcePath": "/orgs/ORG/projects/2"  }
			],
			"pageInfo": { "hasNextPage": false }
		} } } }
		`))
		reg.Register(
			httpmock.GraphQL(`query UserProjectV2List\b`),
			httpmock.StringResponse(`
		{ "data": { "viewer": { "projectsV2": {
			"nodes": [],
			"pageInfo": { "hasNextPage": false }
		} } } }
		`))

		u, err := WithPrAndIssueQueryParams(
			client,
			repo,
			"http://example.com/hey",
			IssueMetadataState{
				ProjectTitles: []string{"TriageV2"},
			},
			gh.ProjectsV1Unsupported,
		)
		require.NoError(t, err)

		url, err := url.Parse(u)
		require.NoError(t, err)

		require.Equal(
			t,
			url.Query().Get("projects"),
			"ORG/2",
		)
	})
}
