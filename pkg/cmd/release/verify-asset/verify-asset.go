package verifyasset

import (
	"context"
	"errors"
	"path/filepath"

	ghauth "github.com/cli/go-gh/v2/pkg/auth"

	"github.com/cli/cli/v2/internal/text"
	"github.com/cli/cli/v2/pkg/cmd/attestation/api"
	"github.com/cli/cli/v2/pkg/cmd/attestation/artifact"
	att_io "github.com/cli/cli/v2/pkg/cmd/attestation/io"
	"github.com/cli/cli/v2/pkg/cmd/attestation/verification"
	"github.com/cli/cli/v2/pkg/cmd/release/attestation"
	"github.com/cli/cli/v2/pkg/cmd/release/shared"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/spf13/cobra"
)

func NewCmdVerifyAsset(f *cmdutil.Factory, runF func(*attestation.VerifyAssetOptions) error) *cobra.Command {
	opts := &attestation.VerifyAssetOptions{
		IO:         f.IOStreams,
		HttpClient: f.HttpClient,
	}

	cmd := &cobra.Command{
		Use:   "verify-asset <tag> <file-path>",
		Short: "Verify that a given asset originated from a specific GitHub Release.",
		Args:  cobra.ExactArgs(2),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// support `-R, --repo` override
			opts.BaseRepo = f.BaseRepo

			if len(args) > 0 {
				opts.TagName = args[0]
			}
			if len(args) > 1 {
				opts.FilePath = args[1]
			}

			if runF != nil {
				return runF(opts)
			}

			httpClient, err := opts.HttpClient()
			if err != nil {
				return err
			}

			baseRepo, err := opts.BaseRepo()
			if err != nil {
				return err
			}

			logger := att_io.NewHandler(opts.IO)
			hostname, _ := ghauth.DefaultHost()
			option := attestation.AttestOptions{
				Repo:          baseRepo.RepoOwner() + "/" + baseRepo.RepoName(),
				APIClient:     api.NewLiveClient(httpClient, hostname, logger),
				Limit:         10,
				Owner:         baseRepo.RepoOwner(),
				PredicateType: "https://in-toto.io/attestation/release/v0.1",
				Logger:        logger,
			}

			option.HttpClient = httpClient
			option.BaseRepo = baseRepo
			option.IO = opts.IO
			option.TagName = opts.TagName
			option.Exporter = opts.Exporter
			option.FilePath = opts.FilePath

			td, err := option.APIClient.GetTrustDomain()
			if err != nil {
				logger.Println(logger.ColorScheme.Red("✗ Failed to get trust domain"))
				return err
			}

			ec, err := attestation.NewEnforcementCriteria(&option, logger)
			if err != nil {
				logger.Println(logger.ColorScheme.Red("✗ Failed to build policy information"))
				return err
			}

			config := verification.SigstoreConfig{
				TrustedRoot:  "",
				Logger:       logger,
				NoPublicGood: true,
				TrustDomain:  td,
			}
			sigstoreVerifier, err := verification.NewLiveSigstoreVerifier(config)
			if err != nil {
				logger.Println(logger.ColorScheme.Red("✗ Failed to create Sigstore verifier"))
				return err
			}

			option.SigstoreVerifier = sigstoreVerifier
			option.EC = ec

			// output ec
			return verifyAssetRun(&option)
		},
	}

	cmdutil.AddJSONFlags(cmd, &opts.Exporter, shared.ReleaseFields)
	return cmd
}

func verifyAssetRun(opts *attestation.AttestOptions) error {
	ctx := context.Background()
	fileName := getFileName(opts.FilePath)

	// calculate the digest of the file
	fileDigest, err := artifact.NewDigestedArtifact(nil, opts.FilePath, "sha256")
	if err != nil {
		opts.Logger.Println(opts.Logger.ColorScheme.Red("✗ Failed to calculate file digest"))
		return err
	}

	opts.Logger.Printf("Loaded digest %s for %s\n", fileDigest.DigestWithAlg(), fileName)

	sha, err := shared.FetchRefSHA(ctx, opts.HttpClient, opts.BaseRepo, opts.TagName)
	if err != nil {
		return err
	}
	releaseArtifact := artifact.NewDigestedArtifactForRelease(opts.TagName, sha, "sha1")
	opts.Logger.Printf("Resolved %s to %s\n", opts.TagName, releaseArtifact.DigestWithAlg())

	// Attestation fetching
	attestations, logMsg, err := attestation.GetAttestations(opts, releaseArtifact.DigestWithAlg())
	if err != nil {
		if errors.Is(err, api.ErrNoAttestationsFound) {
			opts.Logger.Printf(opts.Logger.ColorScheme.Red("✗ No attestations found for subject %s\n"), releaseArtifact.DigestWithAlg())
			return err
		}
		opts.Logger.Println(opts.Logger.ColorScheme.Red(logMsg))
		return err
	}

	// Filter attestations by predicate PURL
	filteredAttestations := attestation.FilterAttestationsByPURL(attestations, opts.Repo, opts.TagName, opts.Logger)
	filteredAttestations = attestation.FilterAttestationsByFileDigest(filteredAttestations, opts.Repo, opts.TagName, fileDigest.Digest(), opts.Logger)

	opts.Logger.Printf("Loaded %s from GitHub API\n", text.Pluralize(len(filteredAttestations), "attestation"))

	// Verify attestations
	verified, errMsg, err := attestation.VerifyAttestations(*releaseArtifact, filteredAttestations, opts.SigstoreVerifier, opts.EC)

	if err != nil {
		opts.Logger.Println(opts.Logger.ColorScheme.Red(errMsg))

		opts.Logger.Println(opts.Logger.ColorScheme.Red("✗ Verification failed"))

		// Release v1.0.0 does not contain bin-linux.tgz (sha256:0c2524c2b002fda89f8b766c7d3dd8e6ac1de183556728a83182c6137f19643d)

		opts.Logger.Printf(opts.Logger.ColorScheme.Red("Release %s does not contain %s (%s)\n"), opts.TagName, opts.FilePath, fileDigest.DigestWithAlg())
		return err
	}

	opts.Logger.Printf("The following %s matched the policy criteria\n\n", text.Pluralize(len(verified), "attestation"))
	opts.Logger.Println(opts.Logger.ColorScheme.Green("✓ Verification succeeded!\n"))

	opts.Logger.Printf("Attestation found matching release %s (%s)\n", opts.TagName, releaseArtifact.DigestWithAlg())

	// bin-linux.tgz is present in release v1.0.0
	opts.Logger.Printf("%s is present in release %s\n", fileName, opts.TagName)

	return nil
}

func getFileName(filePath string) string {
	// Get the file name from the file path
	_, fileName := filepath.Split(filePath)
	return fileName
}
