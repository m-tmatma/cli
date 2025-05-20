package verify

import (
	"context"
	"errors"

	v1 "github.com/in-toto/attestation/go/v1"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/cli/cli/v2/internal/text"
	"github.com/cli/cli/v2/pkg/cmd/attestation/api"
	"github.com/cli/cli/v2/pkg/cmd/attestation/artifact"
	att_io "github.com/cli/cli/v2/pkg/cmd/attestation/io"
	"github.com/cli/cli/v2/pkg/cmd/attestation/verification"
	"github.com/cli/cli/v2/pkg/cmd/release/attestation"
	"github.com/cli/cli/v2/pkg/cmd/release/shared"

	"github.com/cli/cli/v2/pkg/cmdutil"
	ghauth "github.com/cli/go-gh/v2/pkg/auth"
	"github.com/spf13/cobra"
)

func NewCmdVerify(f *cmdutil.Factory, runF func(*attestation.AttestOptions) error) *cobra.Command {
	opts := &attestation.AttestOptions{}

	cmd := &cobra.Command{
		Use:   "verify [<tag>]",
		Short: "Verify the attestation for a GitHub Release.",
		Args:  cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return cmdutil.FlagErrorf("You must specify a tag")
			}

			opts.TagName = args[0]

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

			*opts = attestation.AttestOptions{
				TagName:       opts.TagName,
				Repo:          baseRepo.RepoOwner() + "/" + baseRepo.RepoName(),
				APIClient:     api.NewLiveClient(httpClient, hostname, logger),
				Limit:         10,
				Owner:         baseRepo.RepoOwner(),
				PredicateType: attestation.ReleasePredicateType,
				Logger:        logger,
				HttpClient:    httpClient,
				BaseRepo:      baseRepo,
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if runF != nil {
				return runF(opts)
			}

			td, err := opts.APIClient.GetTrustDomain()
			if err != nil {
				opts.Logger.Println(opts.Logger.ColorScheme.Red("✗ Failed to get trust domain"))
				return err
			}

			ec, err := attestation.NewEnforcementCriteria(opts, opts.Logger)
			if err != nil {
				opts.Logger.Println(opts.Logger.ColorScheme.Red("✗ Failed to build policy information"))
				return err
			}

			config := verification.SigstoreConfig{
				Logger:       opts.Logger,
				NoPublicGood: true,
				TrustDomain:  td,
			}

			sigstoreVerifier, err := verification.NewLiveSigstoreVerifier(config)
			if err != nil {
				opts.Logger.Println(opts.Logger.ColorScheme.Red("✗ Failed to create Sigstore verifier"))
				return err
			}

			opts.SigstoreVerifier = sigstoreVerifier
			opts.EC = ec

			return verifyRun(opts)
		},
	}
	return cmd
}

func verifyRun(opts *attestation.AttestOptions) error {
	ctx := context.Background()

	ref, err := shared.FetchRefSHA(ctx, opts.HttpClient, opts.BaseRepo, opts.TagName)
	if err != nil {
		return err
	}

	releaseRefDigest := artifact.NewDigestedArtifactForRelease(ref, "sha1")
	opts.Logger.Printf("Resolved %s to %s\n", opts.TagName, releaseRefDigest.DigestWithAlg())

	// Attestation fetching
	attestations, logMsg, err := attestation.GetAttestations(opts, releaseRefDigest.DigestWithAlg())
	if err != nil {
		if errors.Is(err, api.ErrNoAttestationsFound) {
			opts.Logger.Printf(opts.Logger.ColorScheme.Red("✗ No attestations found for subject %s\n"), releaseRefDigest.DigestWithAlg())
			return err
		}
		opts.Logger.Println(opts.Logger.ColorScheme.Red(logMsg))
		return err
	}

	// Filter attestations by predicate tag
	filteredAttestations, err := attestation.FilterAttestationsByTag(attestations, opts.TagName)
	if err != nil {
		opts.Logger.Println(opts.Logger.ColorScheme.Red(err.Error()))
		return err
	}

	opts.Logger.Printf("Loaded %s from GitHub API\n", text.Pluralize(len(filteredAttestations), "attestation"))

	// Verify attestations
	verified, errMsg, err := attestation.VerifyAttestations(*releaseRefDigest, filteredAttestations, opts.SigstoreVerifier, opts.EC)

	if err != nil {
		opts.Logger.Println(opts.Logger.ColorScheme.Red(errMsg))
		opts.Logger.Printf(opts.Logger.ColorScheme.Red("✗ Failed to find an attestation for release %s in %s\n"), opts.TagName, opts.Repo)
		return err
	}

	opts.Logger.Printf("The following %s matched the policy criteria\n\n", text.Pluralize(len(verified), "attestation"))
	opts.Logger.Println(opts.Logger.ColorScheme.Green("✓ Verification succeeded!\n"))

	opts.Logger.Printf("Attestation found matching release %s (%s)\n", opts.TagName, releaseRefDigest.Digest())
	printVerifiedSubjects(verified, opts.Logger)

	return nil
}

func printVerifiedSubjects(verified []*verification.AttestationProcessingResult, logger *att_io.Handler) {
	for _, att := range verified {
		statement := att.Attestation.Bundle.GetDsseEnvelope().Payload
		var statementData v1.Statement
		err := protojson.Unmarshal([]byte(statement), &statementData)
		if err != nil {
			logger.Println(logger.ColorScheme.Red("✗ Failed to unmarshal statement"))
			continue
		}
		for _, s := range statementData.Subject {
			name := s.Name
			digest := s.Digest

			if name != "" {
				digestStr := ""
				for key, value := range digest {
					digestStr += key + ":" + value
				}
				logger.Println("  " + name + "          " + digestStr)
			}
		}
	}
}
