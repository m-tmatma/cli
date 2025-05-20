package verify_asset

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	v1 "github.com/in-toto/attestation/go/v1"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/MakeNowJust/heredoc"
	"github.com/cli/cli/v2/internal/tableprinter"
	"github.com/cli/cli/v2/internal/text"
	"github.com/cli/cli/v2/pkg/cmd/attestation/api"
	"github.com/cli/cli/v2/pkg/cmd/attestation/artifact"
	att_io "github.com/cli/cli/v2/pkg/cmd/attestation/io"
	"github.com/cli/cli/v2/pkg/cmd/attestation/verification"
	"github.com/cli/cli/v2/pkg/cmd/release/attestation"
	"github.com/cli/cli/v2/pkg/cmd/release/shared"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/cli/cli/v2/pkg/markdown"
	ghauth "github.com/cli/go-gh/v2/pkg/auth"
	"github.com/spf13/cobra"
)

func NewCmdVerify(f *cmdutil.Factory, runF func(*attestation.VerifyOptions) error) *cobra.Command {
	opts := &attestation.VerifyOptions{
		IO:         f.IOStreams,
		HttpClient: f.HttpClient,
	}

	cmd := &cobra.Command{
		Use:   "verify-asset [<tag>]",
		Short: "Verify information about a release",
		Long: heredoc.Doc(`
			Verify information about a GitHub Release.

			Without an explicit tag name argument, the latest release in the project
			is shown.
		`),
		Args: cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			// Create a logger for use throughout the verify command
			// opts.Logger = io.NewHandler(f.IOStreams)

			// // set the artifact path
			// opts.ArtifactPath = args[0]

			// // Check that the given flag combination is valid
			// if err := opts.AreFlagsValid(); err != nil {
			// 	return err
			// }

			// // Clean file path options
			// opts.Clean()

			// if opts.TagName == "" {
			// 	return cmdutil.FlagErrorf("tag name is required")
			// }

			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// support `-R, --repo` override
			opts.BaseRepo = f.BaseRepo

			if len(args) > 0 {
				opts.TagName = args[0]
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
			return verifyRun(&option)
		},
	}

	cmdutil.AddJSONFlags(cmd, &opts.Exporter, shared.ReleaseFields)

	return cmd
}

func verifyRun(opts *attestation.AttestOptions) error {
	ctx := context.Background()
	logger := opts.Logger

	release, err := shared.FetchRelease(ctx, opts.HttpClient, opts.BaseRepo, opts.TagName)
	if err != nil {
		return err
	}

	sha, err := shared.FetchRefSHA(ctx, opts.HttpClient, opts.BaseRepo, opts.TagName)
	if err != nil {
		return err
	}

	artifact := artifact.NewDigestedArtifactForRelease(opts.TagName, sha, "sha1")

	// Attestation fetching
	attestations, logMsg, err := attestation.GetAttestations(opts, artifact.DigestWithAlg())
	if err != nil {
		if errors.Is(err, api.ErrNoAttestationsFound) {
			logger.Printf(logger.ColorScheme.Red("✗ No attestations found for subject %s\n"), artifact.DigestWithAlg())
			return err
		}
		logger.Println(logger.ColorScheme.Red(logMsg))
		return err
	}

	// Filter attestations by predicate PURL
	filteredAttestations := attestation.FilterAttestationsByPURL(attestations, opts.Repo, opts.TagName, logger)

	// Verify attestations
	verified, errMsg, err := attestation.VerifyAttestations(*artifact, filteredAttestations, opts.SigstoreVerifier, opts.EC)

	if err != nil {
		logger.Println(logger.ColorScheme.Red(errMsg))
		return err
	}

	logger.Printf("The following %s matched the policy criteria\n\n", text.Pluralize(len(verified), "attestation"))
	logger.Println(logger.ColorScheme.Green("✓ Verification succeeded!\n"))

	printVerifiedSubjects(verified, logger)

	opts.IO.DetectTerminalTheme()
	if err := opts.IO.StartPager(); err != nil {
		fmt.Fprintf(opts.IO.ErrOut, "error starting pager: %v\n", err)
	}
	defer opts.IO.StopPager()

	if opts.Exporter != nil {
		return opts.Exporter.Write(opts.IO, release)
	}

	if opts.IO.IsStdoutTTY() {
		return renderVerifyTTY(opts.IO, release)
	}
	return renderVerifyPlain(opts.IO.Out, release)
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
			logger.Printf("%s\n", s.String())
		}
	}
}

func renderVerifyTTY(io *iostreams.IOStreams, release *shared.Release) error {
	cs := io.ColorScheme()
	w := io.Out

	fmt.Fprintf(w, "%s\n", cs.Bold(release.TagName))
	if release.IsDraft {
		fmt.Fprintf(w, "%s • ", cs.Red("Draft"))
	} else if release.IsPrerelease {
		fmt.Fprintf(w, "%s • ", cs.Yellow("Pre-release"))
	}
	if release.IsDraft {
		fmt.Fprintln(w, cs.Mutedf("%s created this %s", release.Author.Login, text.FuzzyAgo(time.Now(), release.CreatedAt)))
	} else {
		fmt.Fprintln(w, cs.Mutedf("%s released this %s", release.Author.Login, text.FuzzyAgo(time.Now(), *release.PublishedAt)))
	}

	renderedDescription, err := markdown.Render(release.Body,
		markdown.WithTheme(io.TerminalTheme()),
		markdown.WithWrap(io.TerminalWidth()))
	if err != nil {
		return err
	}
	fmt.Fprintln(w, renderedDescription)

	if len(release.Assets) > 0 {
		fmt.Fprintln(w, cs.Bold("Assets"))
		//nolint:staticcheck // SA1019: Showing NAME|SIZE headers adds nothing to table.
		table := tableprinter.New(io, tableprinter.NoHeader)
		for _, a := range release.Assets {
			table.AddField(a.Name)
			table.AddField(humanFileSize(a.Size))
			table.EndRow()
		}
		err := table.Render()
		if err != nil {
			return err
		}
		fmt.Fprint(w, "\n")
	}

	fmt.Fprintln(w, cs.Mutedf("View on GitHub: %s", release.URL))
	return nil
}

func renderVerifyPlain(w io.Writer, release *shared.Release) error {
	fmt.Fprintf(w, "title:\t%s\n", release.Name)
	fmt.Fprintf(w, "tag:\t%s\n", release.TagName)
	fmt.Fprintf(w, "draft:\t%v\n", release.IsDraft)
	fmt.Fprintf(w, "prerelease:\t%v\n", release.IsPrerelease)
	fmt.Fprintf(w, "author:\t%s\n", release.Author.Login)
	fmt.Fprintf(w, "created:\t%s\n", release.CreatedAt.Format(time.RFC3339))
	if !release.IsDraft {
		fmt.Fprintf(w, "published:\t%s\n", release.PublishedAt.Format(time.RFC3339))
	}
	fmt.Fprintf(w, "url:\t%s\n", release.URL)
	for _, a := range release.Assets {
		fmt.Fprintf(w, "asset:\t%s\n", a.Name)
	}
	fmt.Fprint(w, "--\n")
	fmt.Fprint(w, release.Body)
	if !strings.HasSuffix(release.Body, "\n") {
		fmt.Fprintf(w, "\n")
	}
	return nil
}

func humanFileSize(s int64) string {
	if s < 1024 {
		return fmt.Sprintf("%d B", s)
	}

	kb := float64(s) / 1024
	if kb < 1024 {
		return fmt.Sprintf("%s KiB", floatToString(kb, 2))
	}

	mb := kb / 1024
	if mb < 1024 {
		return fmt.Sprintf("%s MiB", floatToString(mb, 2))
	}

	gb := mb / 1024
	return fmt.Sprintf("%s GiB", floatToString(gb, 2))
}

// render float to fixed precision using truncation instead of rounding
func floatToString(f float64, p uint8) string {
	fs := fmt.Sprintf("%#f%0*s", f, p, "")
	idx := strings.IndexRune(fs, '.')
	return fs[:idx+int(p)+1]
}
