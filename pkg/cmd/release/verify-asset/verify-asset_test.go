package verifyasset

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/cli/cli/v2/pkg/cmd/attestation/api"
	"github.com/cli/cli/v2/pkg/cmd/attestation/io"
	"github.com/cli/cli/v2/pkg/cmd/attestation/test"
	"github.com/cli/cli/v2/pkg/cmd/attestation/verification"
	"github.com/cli/cli/v2/pkg/cmd/release/attestation"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cli/cli/v2/internal/ghrepo"

	"github.com/cli/cli/v2/pkg/cmd/release/shared"
	"github.com/cli/cli/v2/pkg/httpmock"
)

func TestNewCmdVerifyAsset_Args(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantTag  string
		wantFile string
		wantErr  string
	}{
		{
			name:     "valid args",
			args:     []string{"v1.2.3", "../../attestation/test/data/github_release_artifact.zip"},
			wantTag:  "v1.2.3",
			wantFile: "../../attestation/test/data/github_release_artifact.zip",
		},
		{
			name: "valid flag with no tag",

			args:     []string{"../../attestation/test/data/github_release_artifact.zip"},
			wantFile: "../../attestation/test/data/github_release_artifact.zip",
		},
		{
			name:    "no args",
			args:    []string{},
			wantErr: "you must specify an asset filepath",
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
			cmd := NewCmdVerifyAsset(f, func(o *attestation.AttestOptions) error {
				opts = o
				return nil
			})
			cmd.SetArgs(tt.args)
			cmd.SetIn(&bytes.Buffer{})
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			_, err := cmd.ExecuteC()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantTag, opts.TagName)
				assert.Equal(t, tt.wantFile, opts.AssetFilePath)
			}
		})
	}
}

func Test_verifyAssetRun_Success(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	tagName := "v5"

	fakeHTTP := &httpmock.Registry{}
	defer fakeHTTP.Verify(t)
	fakeSHA := "1234567890abcdef1234567890abcdef12345678"
	shared.StubFetchRefSHA(t, fakeHTTP, "owner", "repo", tagName, fakeSHA)

	baseRepo, err := ghrepo.FromFullName("owner/repo")
	require.NoError(t, err)

	opts := &attestation.AttestOptions{
		TagName:          tagName,
		AssetFilePath:    test.NormalizeRelativePath("../../attestation/test/data/github_release_artifact.zip"),
		Repo:             "owner/repo",
		Owner:            "owner",
		Limit:            10,
		Logger:           io.NewHandler(ios),
		APIClient:        api.NewTestClient(),
		SigstoreVerifier: verification.NewMockSigstoreVerifier(t),
		PredicateType:    attestation.ReleasePredicateType,
		HttpClient:       &http.Client{Transport: fakeHTTP},
		BaseRepo:         baseRepo,
	}

	ec, err := attestation.NewEnforcementCriteria(opts)
	require.NoError(t, err)
	opts.EC = ec
	opts.Clean()
	err = verifyAssetRun(opts)
	require.NoError(t, err)
}

func Test_verifyAssetRun_Failed_With_Wrong_tag(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	tagName := "v1"

	fakeHTTP := &httpmock.Registry{}
	defer fakeHTTP.Verify(t)
	fakeSHA := "1234567890abcdef1234567890abcdef12345678"
	shared.StubFetchRefSHA(t, fakeHTTP, "owner", "repo", tagName, fakeSHA)

	baseRepo, err := ghrepo.FromFullName("owner/repo")
	require.NoError(t, err)

	opts := &attestation.AttestOptions{
		TagName:          tagName,
		AssetFilePath:    test.NormalizeRelativePath("../../attestation/test/data/github_release_artifact.zip"),
		Repo:             "owner/repo",
		Owner:            "owner",
		Limit:            10,
		Logger:           io.NewHandler(ios),
		APIClient:        api.NewTestClient(),
		SigstoreVerifier: verification.NewMockSigstoreVerifier(t),
		PredicateType:    attestation.ReleasePredicateType,
		HttpClient:       &http.Client{Transport: fakeHTTP},
		BaseRepo:         baseRepo,
	}

	ec, err := attestation.NewEnforcementCriteria(opts)
	require.NoError(t, err)
	opts.EC = ec

	err = verifyAssetRun(opts)
	require.Error(t, err, "no attestations found for github_release_artifact.zip in release v1")
}

func Test_verifyAssetRun_Failed_With_Invalid_Artifact(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	tagName := "v5"

	fakeHTTP := &httpmock.Registry{}
	defer fakeHTTP.Verify(t)
	fakeSHA := "1234567890abcdef1234567890abcdef12345678"
	shared.StubFetchRefSHA(t, fakeHTTP, "owner", "repo", tagName, fakeSHA)

	baseRepo, err := ghrepo.FromFullName("owner/repo")
	require.NoError(t, err)

	opts := &attestation.AttestOptions{
		TagName:          tagName,
		AssetFilePath:    test.NormalizeRelativePath("../../attestation/test/data/github_release_artifact.zip"),
		Repo:             "owner/repo",
		Owner:            "owner",
		Limit:            10,
		Logger:           io.NewHandler(ios),
		APIClient:        api.NewTestClient(),
		SigstoreVerifier: verification.NewMockSigstoreVerifier(t),
		PredicateType:    attestation.ReleasePredicateType,
		HttpClient:       &http.Client{Transport: fakeHTTP},
		BaseRepo:         baseRepo,
	}

	err = verifyAssetRun(opts)
	require.Error(t, err, "no attestations found for github_release_artifact_invalid.zip in release v1.2.3")
}

func Test_verifyAssetRun_NoAttestation(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	opts := &attestation.AttestOptions{
		TagName:          "v1.2.3",
		AssetFilePath:    "artifact.tgz",
		Repo:             "owner/repo",
		Limit:            10,
		Logger:           io.NewHandler(ios),
		IO:               ios,
		APIClient:        api.NewTestClient(),
		SigstoreVerifier: verification.NewMockSigstoreVerifier(t),
		PredicateType:    attestation.ReleasePredicateType,

		EC: verification.EnforcementCriteria{},
	}

	err := verifyAssetRun(opts)
	require.Error(t, err, "failed to get open local artifact: open artifact.tgz: no such file or director")
}

func Test_getFileName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"foo/bar/baz.txt", "baz.txt"},
		{"baz.txt", "baz.txt"},
		{"/tmp/foo.tar.gz", "foo.tar.gz"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := getFileName(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
