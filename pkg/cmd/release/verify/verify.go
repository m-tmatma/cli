package verify

import (
	"context"
	"errors"
	"fmt"

	v1 "github.com/in-toto/attestation/go/v1"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/cli/cli/v2/internal/text"
	"github.com/cli/cli/v2/pkg/cmd/attestation/api"
	"github.com/cli/cli/v2/pkg/cmd/attestation/artifact"
	"github.com/cli/cli/v2/pkg/cmd/attestation/auth"
	att_io "github.com/cli/cli/v2/pkg/cmd/attestation/io"
	"github.com/cli/cli/v2/pkg/cmd/attestation/verification"
	"github.com/cli/cli/v2/pkg/cmd/release/shared"

	"github.com/cli/cli/v2/pkg/cmdutil"
	ghauth "github.com/cli/go-gh/v2/pkg/auth"
	"github.com/spf13/cobra"
)

func NewCmdVerify(f *cmdutil.Factory, runF func(*shared.AttestOptions) error) *cobra.Command {
	opts := &shared.AttestOptions{}

	cmd := &cobra.Command{
		Use:    "verify [<tag>]",
		Short:  "Verify the attestation for a GitHub Release.",
		Hidden: true,
		Args:   cobra.MaximumNArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				opts.TagName = args[0]
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
				opts.Logger.Println(opts.Logger.ColorScheme.Red("X Failed to get trust domain"))
				return err
			}
			opts.TrustedRoot = td

			ec, err := shared.NewEnforcementCriteria(opts)
			if err != nil {
				opts.Logger.Println(opts.Logger.ColorScheme.Red("X Failed to build policy information"))
				return err
			}
			opts.EC = ec

			// Avoid creating a Sigstore verifier if the runF function is provided for testing purposes
			if runF != nil {
				return runF(opts)
			}
			return verifyRun(opts)
		},
	}
	cmdutil.AddFormatFlags(cmd, &opts.Exporter)

	return cmd
}

func verifyRun(opts *shared.AttestOptions) error {
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
			opts.Logger.Println(opts.Logger.ColorScheme.Red("X Failed to create Sigstore verifier"))
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
			opts.Logger.Printf(opts.Logger.ColorScheme.Red("X No attestations found for subject %s\n"), releaseRefDigest.DigestWithAlg())
			return err
		}
		opts.Logger.Println(opts.Logger.ColorScheme.Red(logMsg))
		return err
	}

	// Filter attestations by predicate tag
	filteredAttestations, err := shared.FilterAttestationsByTag(attestations, opts.TagName)
	if err != nil {
		opts.Logger.Println(opts.Logger.ColorScheme.Red(err.Error()))
		return err
	}

	if len(filteredAttestations) == 0 {
		opts.Logger.Printf(opts.Logger.ColorScheme.Red("X No attestations found for release %s in %s\n"), opts.TagName, opts.Repo)
		return fmt.Errorf("no attestations found for release %s in %s", opts.TagName, opts.Repo)
	}

	opts.Logger.Printf("Loaded %s from GitHub API\n", text.Pluralize(len(filteredAttestations), "attestation"))

	// Verify attestations
	verified, errMsg, err := shared.VerifyAttestations(*releaseRefDigest, filteredAttestations, opts.SigstoreVerifier, opts.EC)

	if err != nil {
		opts.Logger.Println(opts.Logger.ColorScheme.Red(errMsg))
		opts.Logger.Printf(opts.Logger.ColorScheme.Red("X Failed to find an attestation for release %s in %s\n"), opts.TagName, opts.Repo)
		return err
	}

	// If an exporter is provided with the --json flag, write the results to the terminal in JSON format
	if opts.Exporter != nil {
		// print the results to the terminal as an array of JSON objects
		if err = opts.Exporter.Write(opts.Logger.IO, verified); err != nil {
			opts.Logger.Println(opts.Logger.ColorScheme.Red("X Failed to write JSON output"))
			return err
		}
		return nil
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
			logger.Println(logger.ColorScheme.Red("X Failed to unmarshal statement"))
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
