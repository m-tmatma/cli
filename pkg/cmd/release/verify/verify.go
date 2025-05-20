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

			opts.Repo = baseRepo.RepoOwner() + "/" + baseRepo.RepoName()
			opts.APIClient = api.NewLiveClient(httpClient, hostname, logger)
			opts.Limit = 10
			opts.Owner = baseRepo.RepoOwner()
			opts.PredicateType = "https://in-toto.io/attestation/release/v0.1"
			opts.Logger = logger

			opts.HttpClient = httpClient
			opts.BaseRepo = baseRepo

			opts.HttpClient = httpClient

			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if runF != nil {
				return runF(opts)
			}
			//

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
				TrustedRoot:  "",
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

			// output ec
			return verifyRun(opts)
		},
	}

	cmdutil.AddJSONFlags(cmd, &opts.Exporter, shared.ReleaseFields)

	return cmd
}

func verifyRun(opts *attestation.AttestOptions) error {
	ctx := context.Background()

	sha, err := shared.FetchRefSHA(ctx, opts.HttpClient, opts.BaseRepo, opts.TagName)
	if err != nil {
		return err
	}

	artifact := artifact.NewDigestedArtifactForRelease(opts.TagName, sha, "sha1")
	opts.Logger.Printf("Resolved %s to %s\n", opts.TagName, artifact.DigestWithAlg())

	// Attestation fetching
	attestations, logMsg, err := attestation.GetAttestations(opts, artifact.DigestWithAlg())
	if err != nil {
		if errors.Is(err, api.ErrNoAttestationsFound) {
			opts.Logger.Printf(opts.Logger.ColorScheme.Red("✗ No attestations found for subject %s\n"), artifact.DigestWithAlg())
			return err
		}
		opts.Logger.Println(opts.Logger.ColorScheme.Red(logMsg))
		return err
	}

	// Filter attestations by predicate PURL
	filteredAttestations := attestation.FilterAttestationsByPURL(attestations, opts.Repo, opts.TagName, opts.Logger)

	opts.Logger.Printf("Loaded %s from GitHub API\n", text.Pluralize(len(filteredAttestations), "attestation"))

	// Verify attestations
	verified, errMsg, err := attestation.VerifyAttestations(*artifact, filteredAttestations, opts.SigstoreVerifier, opts.EC)

	if err != nil {
		opts.Logger.Println(opts.Logger.ColorScheme.Red(errMsg))

		opts.Logger.Println(opts.Logger.ColorScheme.Red("✗ Verification failed"))

		opts.Logger.Printf(opts.Logger.ColorScheme.Red("✗ Failed to find an attestation for release %s in %s\n"), opts.TagName, opts.Repo)
		return err
	}

	opts.Logger.Printf("The following %s matched the policy criteria\n\n", text.Pluralize(len(verified), "attestation"))
	opts.Logger.Println(opts.Logger.ColorScheme.Green("✓ Verification succeeded!\n"))

	opts.Logger.Printf("Attestation found matching release %s (%s)\n", opts.TagName, artifact.Digest())
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
				// digest is map[string]string and i want to be key:value
				// so i need to iterate over the map and print key:value
				digestStr := ""
				for key, value := range digest {
					digestStr += key + ":" + value
				}
				// output should like this
				//   bin-linux.tgz          sha256:0c2524c2b002fda89f8b766c7d3dd8e6ac1de183556728a83182c6137f19643d
				logger.Println("  " + name + "          " + digestStr)
			}
		}
	}
}
