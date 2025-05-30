package verifyasset

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/cli/cli/v2/pkg/cmd/attestation/auth"
	ghauth "github.com/cli/go-gh/v2/pkg/auth"

	"github.com/cli/cli/v2/internal/text"
	"github.com/cli/cli/v2/pkg/cmd/attestation/api"
	"github.com/cli/cli/v2/pkg/cmd/attestation/artifact"
	att_io "github.com/cli/cli/v2/pkg/cmd/attestation/io"
	"github.com/cli/cli/v2/pkg/cmd/attestation/verification"
	"github.com/cli/cli/v2/pkg/cmd/release/shared"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/spf13/cobra"
)

func NewCmdVerifyAsset(f *cmdutil.Factory, runF func(*shared.AttestOptions) error) *cobra.Command {
	opts := &shared.AttestOptions{}

	cmd := &cobra.Command{
		Use:    "verify-asset <tag> <file-path>",
		Short:  "Verify that a given asset originated from a specific GitHub Release.",
		Hidden: true,
		Args:   cobra.MaximumNArgs(2),
		PreRunE: func(cmd *cobra.Command, args []string) error {

			if len(args) == 2 {
				opts.TagName = args[0]
				opts.AssetFilePath = args[1]
			} else if len(args) == 1 {
				opts.AssetFilePath = args[0]
			} else {
				return cmdutil.FlagErrorf("you must specify an asset filepath")
			}

			httpClient, err := f.HttpClient()
			if err != nil {
				return err
			}
			baseRepo, err := f.BaseRepo()
			if err != nil {
				return err
			}
			logger := att_io.NewHandler(f.IOStreams)
			hostname, _ := ghauth.DefaultHost()

			err = auth.IsHostSupported(hostname)
			if err != nil {
				return err
			}

			*opts = shared.AttestOptions{
				TagName:       opts.TagName,
				AssetFilePath: opts.AssetFilePath,
				Repo:          baseRepo.RepoOwner() + "/" + baseRepo.RepoName(),
				APIClient:     api.NewLiveClient(httpClient, hostname, logger),
				Limit:         10,
				Owner:         baseRepo.RepoOwner(),
				PredicateType: shared.ReleasePredicateType,
				Logger:        logger,
				HttpClient:    httpClient,
				BaseRepo:      baseRepo,
				Hostname:      hostname,
			}

			// Check that the given flag combination is valid
			if err := opts.AreFlagsValid(); err != nil {
				return err
			}

			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			td, err := opts.APIClient.GetTrustDomain()
			if err != nil {
				opts.Logger.Println(opts.Logger.ColorScheme.Red("✗ Failed to get trust domain"))
				return err
			}

			opts.TrustedRoot = td

			ec, err := shared.NewEnforcementCriteria(opts)
			if err != nil {
				opts.Logger.Println(opts.Logger.ColorScheme.Red("✗ Failed to build policy information"))
				return err
			}

			opts.EC = ec

			opts.Clean()

			// Avoid creating a Sigstore verifier if the runF function is provided for testing purposes
			if runF != nil {
				return runF(opts)
			}

			return verifyAssetRun(opts)
		},
	}
	cmdutil.AddFormatFlags(cmd, &opts.Exporter)

	return cmd
}

func verifyAssetRun(opts *shared.AttestOptions) error {
	ctx := context.Background()

	if opts.SigstoreVerifier == nil {
		config := verification.SigstoreConfig{
			HttpClient:   opts.HttpClient,
			Logger:       opts.Logger,
			NoPublicGood: true,
			TrustDomain:  opts.TrustedRoot,
		}

		sigstoreVerifier, err := verification.NewLiveSigstoreVerifier(config)
		if err != nil {
			opts.Logger.Println(opts.Logger.ColorScheme.Red("✗ Failed to create Sigstore verifier"))
			return err
		}

		opts.SigstoreVerifier = sigstoreVerifier
	}

	if opts.TagName == "" {
		release, err := shared.FetchLatestRelease(ctx, opts.HttpClient, opts.BaseRepo)
		if err != nil {
			return err
		}
		opts.TagName = release.TagName
	}

	fileName := getFileName(opts.AssetFilePath)

	// calculate the digest of the file
	fileDigest, err := artifact.NewDigestedArtifact(nil, opts.AssetFilePath, "sha256")
	if err != nil {
		opts.Logger.Println(opts.Logger.ColorScheme.Red("✗ Failed to calculate file digest"))
		return err
	}

	opts.Logger.Printf("Loaded digest %s for %s\n", fileDigest.DigestWithAlg(), fileName)

	ref, err := shared.FetchRefSHA(ctx, opts.HttpClient, opts.BaseRepo, opts.TagName)
	if err != nil {
		return err
	}
	releaseRefDigest := artifact.NewDigestedArtifactForRelease(ref, "sha1")
	opts.Logger.Printf("Resolved %s to %s\n", opts.TagName, releaseRefDigest.DigestWithAlg())

	// Attestation fetching
	attestations, logMsg, err := shared.GetAttestations(opts, releaseRefDigest.DigestWithAlg())
	if err != nil {
		if errors.Is(err, api.ErrNoAttestationsFound) {
			opts.Logger.Printf(opts.Logger.ColorScheme.Red("✗ No attestations found for subject %s\n"), releaseRefDigest.DigestWithAlg())
			return err
		}
		opts.Logger.Println(opts.Logger.ColorScheme.Red(logMsg))
		return err
	}

	// Filter attestations by tag
	filteredAttestations, err := shared.FilterAttestationsByTag(attestations, opts.TagName)
	if err != nil {
		opts.Logger.Println(opts.Logger.ColorScheme.Red(err.Error()))
		return err
	}

	filteredAttestations, err = shared.FilterAttestationsByFileDigest(filteredAttestations, opts.Repo, opts.TagName, fileDigest.Digest())
	if err != nil {
		opts.Logger.Println(opts.Logger.ColorScheme.Red(err.Error()))
		return err
	}

	if len(filteredAttestations) == 0 {
		opts.Logger.Printf(opts.Logger.ColorScheme.Red("Release %s does not contain %s (%s)\n"), opts.TagName, opts.AssetFilePath, fileDigest.DigestWithAlg())
		return fmt.Errorf("release %s does not contain %s (%s)", opts.TagName, opts.AssetFilePath, fileDigest.DigestWithAlg())
	}

	opts.Logger.Printf("Loaded %s from GitHub API\n", text.Pluralize(len(filteredAttestations), "attestation"))

	// Verify attestations
	verified, errMsg, err := shared.VerifyAttestations(*releaseRefDigest, filteredAttestations, opts.SigstoreVerifier, opts.EC)

	if err != nil {
		opts.Logger.Println(opts.Logger.ColorScheme.Red(errMsg))
		opts.Logger.Printf(opts.Logger.ColorScheme.Red("Release %s does not contain %s (%s)\n"), opts.TagName, opts.AssetFilePath, fileDigest.DigestWithAlg())
		return err
	}

	// If an exporter is provided with the --json flag, write the results to the terminal in JSON format
	if opts.Exporter != nil {
		// print the results to the terminal as an array of JSON objects
		if err = opts.Exporter.Write(opts.Logger.IO, verified); err != nil {
			opts.Logger.Println(opts.Logger.ColorScheme.Red("✗ Failed to write JSON output"))
			return err
		}
		return nil
	}

	opts.Logger.Printf("The following %s matched the policy criteria\n\n", text.Pluralize(len(verified), "attestation"))
	opts.Logger.Println(opts.Logger.ColorScheme.Green("✓ Verification succeeded!\n"))
	opts.Logger.Printf("Attestation found matching release %s (%s)\n", opts.TagName, releaseRefDigest.DigestWithAlg())
	opts.Logger.Printf("%s is present in release %s\n", fileName, opts.TagName)

	return nil
}

func getFileName(filePath string) string {
	// Get the file name from the file path
	_, fileName := filepath.Split(filePath)
	return fileName
}
