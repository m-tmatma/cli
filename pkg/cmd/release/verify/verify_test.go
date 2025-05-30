package verify

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/cli/cli/v2/internal/ghrepo"
	"github.com/cli/cli/v2/pkg/cmd/attestation/api"
	"github.com/cli/cli/v2/pkg/cmd/attestation/io"
	"github.com/cli/cli/v2/pkg/cmd/attestation/verification"
	"github.com/cli/cli/v2/pkg/cmd/release/attestation"
	"github.com/cli/cli/v2/pkg/cmd/release/shared"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/httpmock"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCmdVerify_Args(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantTag string
		wantErr string
	}{
		{
			name:    "valid tag arg",
			args:    []string{"v1.2.3"},
			wantTag: "v1.2.3",
		},
		{
			name:    "no tag arg",
			args:    []string{},
			wantTag: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testIO, _, _, _ := iostreams.Test()
			var testReg httpmock.Registry
			var metaResp = api.MetaResponse{
				Domains: api.Domain{
					ArtifactAttestations: api.ArtifactAttestations{},
				},
			}
			testReg.Register(httpmock.REST(http.MethodGet, "meta"),
				httpmock.StatusJSONResponse(200, &metaResp))

			f := &cmdutil.Factory{
				IOStreams: testIO,
				HttpClient: func() (*http.Client, error) {
					reg := &testReg
					client := &http.Client{}
					httpmock.ReplaceTripper(client, reg)
					return client, nil
				},
				BaseRepo: func() (ghrepo.Interface, error) {
					return ghrepo.FromFullName("owner/repo")
				},
			}

			var opts *attestation.AttestOptions
			cmd := NewCmdVerify(f, func(o *attestation.AttestOptions) error {
				opts = o
				return nil
			})
			cmd.SetArgs(tt.args)
			cmd.SetIn(&bytes.Buffer{})
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			_, err := cmd.ExecuteC()
			require.NoError(t, err)
			assert.Equal(t, tt.wantTag, opts.TagName)
		})
	}
}

func Test_verifyRun_Success(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	tagName := "v6"

	fakeHTTP := &httpmock.Registry{}
	defer fakeHTTP.Verify(t)
	fakeSHA := "1234567890abcdef1234567890abcdef12345678"
	shared.StubFetchRefSHA(t, fakeHTTP, "owner", "repo", tagName, fakeSHA)

	baseRepo, err := ghrepo.FromFullName("owner/repo")
	require.NoError(t, err)

	opts := &attestation.AttestOptions{
		TagName:          tagName,
		Repo:             "owner/repo",
		Owner:            "owner",
		Limit:            10,
		Logger:           io.NewHandler(ios),
		APIClient:        api.NewTestClient(),
		SigstoreVerifier: verification.NewMockSigstoreVerifier(t),
		HttpClient:       &http.Client{Transport: fakeHTTP},
		BaseRepo:         baseRepo,
		PredicateType:    attestation.ReleasePredicateType,
	}

	ec, err := attestation.NewEnforcementCriteria(opts)
	require.NoError(t, err)
	opts.EC = ec

	err = verifyRun(opts)
	require.NoError(t, err)
}

func Test_verifyRun_Failed_With_Invalid_Tag(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	tagName := "v1.2.3"

	fakeHTTP := &httpmock.Registry{}
	defer fakeHTTP.Verify(t)
	fakeSHA := "1234567890abcdef1234567890abcdef12345678"
	shared.StubFetchRefSHA(t, fakeHTTP, "owner", "repo", tagName, fakeSHA)

	baseRepo, err := ghrepo.FromFullName("owner/repo")
	require.NoError(t, err)

	opts := &attestation.AttestOptions{
		TagName:          tagName,
		Repo:             "owner/repo",
		Owner:            "owner",
		Limit:            10,
		Logger:           io.NewHandler(ios),
		APIClient:        api.NewFailTestClient(),
		SigstoreVerifier: verification.NewMockSigstoreVerifier(t),
		PredicateType:    attestation.ReleasePredicateType,

		HttpClient: &http.Client{Transport: fakeHTTP},
		BaseRepo:   baseRepo,
	}

	ec, err := attestation.NewEnforcementCriteria(opts)
	require.NoError(t, err)
	opts.EC = ec

	err = verifyRun(opts)
	require.Error(t, err, "failed to fetch attestations from owner/repo")
}

func Test_verifyRun_Failed_NoAttestation(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	tagName := "v1.2.3"

	fakeHTTP := &httpmock.Registry{}
	defer fakeHTTP.Verify(t)
	fakeSHA := "1234567890abcdef1234567890abcdef12345678"
	shared.StubFetchRefSHA(t, fakeHTTP, "owner", "repo", tagName, fakeSHA)

	baseRepo, err := ghrepo.FromFullName("owner/repo")
	require.NoError(t, err)

	opts := &attestation.AttestOptions{
		TagName:          tagName,
		Repo:             "owner/repo",
		Owner:            "owner",
		Limit:            10,
		Logger:           io.NewHandler(ios),
		APIClient:        api.NewFailTestClient(),
		SigstoreVerifier: verification.NewMockSigstoreVerifier(t),
		HttpClient:       &http.Client{Transport: fakeHTTP},
		BaseRepo:         baseRepo,
		PredicateType:    attestation.ReleasePredicateType,
	}

	ec, err := attestation.NewEnforcementCriteria(opts)
	require.NoError(t, err)
	opts.EC = ec

	err = verifyRun(opts)
	require.Error(t, err, "failed to fetch attestations from owner/repo")
}
