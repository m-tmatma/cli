package attestation

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/cli/cli/v2/internal/gh"
	"github.com/cli/cli/v2/internal/ghinstance"
	"github.com/cli/cli/v2/internal/ghrepo"
	"github.com/cli/cli/v2/pkg/cmd/attestation/api"
	"github.com/cli/cli/v2/pkg/cmd/attestation/io"
	"github.com/cli/cli/v2/pkg/cmd/attestation/verification"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/iostreams"
)

const ReleasePredicateType = "https://in-toto.io/attestation/release/v0.1"

type AttestOptions struct {
	Config           func() (gh.Config, error)
	HttpClient       *http.Client
	IO               *iostreams.IOStreams
	BaseRepo         ghrepo.Interface
	Exporter         cmdutil.Exporter
	TagName          string
	TrustedRoot      string
	Limit            int
	Owner            string
	PredicateType    string
	Repo             string
	APIClient        api.Client
	Logger           *io.Handler
	SigstoreVerifier verification.SigstoreVerifier
	Hostname         string
	EC               verification.EnforcementCriteria
	// Tenant is only set when tenancy is used
	Tenant        string
	AssetFilePath string
}

// Clean cleans the file path option values
func (opts *AttestOptions) Clean() {
	if opts.AssetFilePath != "" {
		opts.AssetFilePath = filepath.Clean(opts.AssetFilePath)
	}
}

// AreFlagsValid checks that the provided flag combination is valid
// and returns an error otherwise
func (opts *AttestOptions) AreFlagsValid() error {
	// If provided, check that the Repo option is in the expected format <OWNER>/<REPO>
	if opts.Repo != "" && !isProvidedRepoValid(opts.Repo) {
		return fmt.Errorf("invalid value provided for repo: %s", opts.Repo)
	}

	// Check that limit is between 1 and 1000
	if opts.Limit < 1 || opts.Limit > 1000 {
		return fmt.Errorf("limit %d not allowed, must be between 1 and 1000", opts.Limit)
	}

	if opts.Hostname != "" {
		if err := ghinstance.HostnameValidator(opts.Hostname); err != nil {
			return fmt.Errorf("error parsing hostname: %w", err)
		}
	}

	return nil
}

func isProvidedRepoValid(repo string) bool {
	// we expect a provided repository argument be in the format <OWNER>/<REPO>
	splitRepo := strings.Split(repo, "/")
	return len(splitRepo) == 2
}
