package verify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	v1 "github.com/in-toto/attestation/go/v1"

	"github.com/cli/cli/v2/pkg/cmd/attestation/verification"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/cli/cli/v2/pkg/cmd/attestation/artifact"
	att_io "github.com/cli/cli/v2/pkg/cmd/attestation/io"

	"github.com/MakeNowJust/heredoc"
	"github.com/cli/cli/v2/internal/ghrepo"
	"github.com/cli/cli/v2/internal/tableprinter"
	"github.com/cli/cli/v2/internal/text"
	"github.com/cli/cli/v2/pkg/cmd/attestation/api"
	"github.com/cli/cli/v2/pkg/cmd/release/shared"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/cli/cli/v2/pkg/markdown"
	ghauth "github.com/cli/go-gh/v2/pkg/auth"
	"github.com/spf13/cobra"
)

type VerifyOptions struct {
	HttpClient func() (*http.Client, error)
	IO         *iostreams.IOStreams
	BaseRepo   func() (ghrepo.Interface, error)
	Exporter   cmdutil.Exporter

	TagName string
}

func NewCmdVerify(f *cmdutil.Factory, runF func(*VerifyOptions) error) *cobra.Command {
	opts := &VerifyOptions{
		IO:         f.IOStreams,
		HttpClient: f.HttpClient,
	}

	cmd := &cobra.Command{
		Use:   "verify [<tag>]",
		Short: "Verify information about a release",
		Long: heredoc.Doc(`
			Verify information about a GitHub Release.

			Without an explicit tag name argument, the latest release in the project
			is shown.
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// support `-R, --repo` override
			opts.BaseRepo = f.BaseRepo

			if len(args) > 0 {
				opts.TagName = args[0]
			}

			if runF != nil {
				return runF(opts)
			}
			return verifyRun(opts)
		},
	}

	cmdutil.AddJSONFlags(cmd, &opts.Exporter, shared.ReleaseFields)

	return cmd
}

func verifyRun(opts *VerifyOptions) error {
	httpClient, err := opts.HttpClient()
	if err != nil {
		return err
	}

	baseRepo, err := opts.BaseRepo()
	if err != nil {
		return err
	}

	ctx := context.Background()
	var release *shared.Release

	if opts.TagName == "" {
		return cmdutil.FlagErrorf("tag name is required")
	} else {
		release, err = shared.FetchRelease(ctx, httpClient, baseRepo, opts.TagName)
		if err != nil {
			return err
		}
	}

	sha, err := shared.FetchRefSHA(ctx, httpClient, baseRepo, opts.TagName)
	if err != nil {
		return err
	}
	artifact := artifact.NewDigestedArtifactForRelease(opts.TagName, sha, "sha1")

	sha = "sha1:" + sha

	// Resolved v1.0.0 to sha1:824acc86dd86a745b3014bd5353b844959f3591e
	fmt.Println("Resolved", opts.TagName, "to "+sha)

	// Fetch Attestation
	PredicateType := "https://in-toto.io/attestation/release/v0.1"
	limit := 10

	Hostname, _ := ghauth.DefaultHost()

	logger := att_io.NewHandler(opts.IO)

	repo := baseRepo.RepoOwner() + "/" + baseRepo.RepoName()
	attestOption := &Options{
		Repo:          repo,
		APIClient:     api.NewLiveClient(httpClient, Hostname, logger),
		Limit:         limit,
		Owner:         baseRepo.RepoOwner(),
		PredicateType: PredicateType,
	}
	attestations, logMsg, err := getAttestations(attestOption, sha)

	if err != nil {
		if ok := errors.Is(err, api.ErrNoAttestationsFound); ok {
			logger.Printf(logger.ColorScheme.Red("✗ No attestations found for subject %s\n"), sha)
			return err
		}
		// Print the message signifying failure fetching attestations
		logger.Println(logger.ColorScheme.Red(logMsg))
		return err
	}
	// Print the message signifying success fetching attestations
	logger.Println(logMsg)

	td, err := attestOption.APIClient.GetTrustDomain()
	if err != nil {
		logger.Println(logger.ColorScheme.Red("✗ Failed to get trust domain"))
		return err
	}

	// print information about the policy that will be enforced against attestations
	// logger.Println("\nThe following policy criteria will be enforced:")
	ec, err := newEnforcementCriteria(attestOption)
	ec.SANRegex = "https://dotcom.releases.github.com"

	if err != nil {
		logger.Println(logger.ColorScheme.Red("✗ Failed to build policy information"))
		return err
	}
	// logger.Println(ec.BuildPolicyInformation())

	config := verification.SigstoreConfig{
		TrustedRoot:  "",
		Logger:       logger,
		NoPublicGood: true,
	}

	config.TrustDomain = td

	sigstoreVerifier, err := verification.NewLiveSigstoreVerifier(config)
	if err != nil {
		logger.Println(logger.ColorScheme.Red("✗ Failed to create Sigstore verifier"))
		return err
	}

	var filteredAttestations []*api.Attestation

	for _, att := range attestations {
		statement := att.Bundle.Bundle.GetDsseEnvelope().Payload

		var statementData v1.Statement
		err = protojson.Unmarshal([]byte(statement), &statementData)

		if err != nil {
			logger.Println(logger.ColorScheme.Red("✗ Failed to unmarshal statement"))
			return err
		}
		expectedPURL := "pkg:github/" + attestOption.Repo + "@" + opts.TagName
		purlValue := statementData.Predicate.GetFields()["purl"]
		var purl string
		if purlValue != nil {
			purl = purlValue.GetStringValue()
		}

		// fmt.Print("purlValue: ", expectedPURL, "\n")
		// fmt.Print("purl: ", purl, "\n")
		if purl == expectedPURL {
			filteredAttestations = append(filteredAttestations, att)
		}
	}

	verified, errMsg, err := verifyAttestations(*artifact, filteredAttestations, sigstoreVerifier, ec)
	if err != nil {
		logger.Println(logger.ColorScheme.Red(errMsg))
		return err
	}

	logger.Printf("The following %s matched the policy criteria\n\n", text.Pluralize(len(verified), "attestation"))

	logger.Println(logger.ColorScheme.Green("✓ Verification succeeded!\n"))

	// Print verified attestations
	for _, att := range verified {

		// • {"_type":"https://in-toto.io/Statement/v1", "subject":[{"name":"pkg:github/bdehamer/delme@v2.0.0", "digest":{"sha1":"c5e17a62e06a1d201570249c61fae531e9244e1b"}}, {"name":"bdehamer-attest-demo-attestation-6498970.sigstore.1.json", "digest":{"sha256":"b41c3c570a2f60272cb387a58f3e574c6f9da913f6281204b67a223e6ae56176"}}], "predicateType":"https://in-toto.io/attestation/release/v0.1", "predicate":{"ownerId":"398027", "purl":"pkg:github/bdehamer/delme@v2.0.0", "releaseId":"217656813", "repository":"bdehamer/delme", "repositoryId":"905988044", "tag":"v2.0.0"}}
		statement := att.Attestation.Bundle.GetDsseEnvelope().Payload

		// cast statement to {"_type":"https://in-toto.io/Statement/v1", "subject":[{"name":"pkg:github/bdehamer/delme@v2.0.0", "digest":{"sha1":"c5e17a62e06a1d201570249c61fae531e9244e1b"}}, {"name":"bdehamer-attest-demo-attestation-6498970.sigstore.1.json", "digest":{"sha256":"b41c3c570a2f60272cb387a58f3e574c6f9da913f6281204b67a223e6ae56176"}}], "predicateType":"https://in-toto.io/attestation/release/v0.1", "predicate":{"ownerId":"398027", "purl":"pkg:github/bdehamer/delme@v2.0.0", "releaseId":"217656813", "repository":"bdehamer/delme", "repositoryId":"905988044", "tag":"v2.0.0"}}

		var statementData v1.Statement
		err = protojson.Unmarshal([]byte(statement), &statementData)
		if err != nil {
			logger.Println(logger.ColorScheme.Red("✗ Failed to unmarshal statement"))
			return err
		}

		subjects := statementData.Subject

		for _, s := range subjects {
			// // Print the subject name and digest
			// logger.Printf("• %s\n", s.Name)
			// for k, v := range s.Digest {
			// 	// Print the digest algorithm and value
			// 	logger.Printf("  - %s: %s\n", k, v)
			// }

			// Print the whole subject
			logger.Printf("%s\n", s.String())
		}

		// logger.Printf("• %s\n", att.Attestation.Bundle.GetDsseEnvelope().Payload)

	}

	// Verify attestations

	opts.IO.DetectTerminalTheme()
	if err := opts.IO.StartPager(); err != nil {
		fmt.Fprintf(opts.IO.ErrOut, "error starting pager: %v\n", err)
	}
	defer opts.IO.StopPager()

	if opts.Exporter != nil {
		return opts.Exporter.Write(opts.IO, release)
	}

	if opts.IO.IsStdoutTTY() {
		if err := renderVerifyTTY(opts.IO, release); err != nil {
			return err
		}
	} else {
		if err := renderVerifyPlain(opts.IO.Out, release); err != nil {
			return err
		}
	}

	return nil
}

func renderVerifyTTY(io *iostreams.IOStreams, release *shared.Release) error {
	cs := io.ColorScheme()
	w := io.Out

	// fmt.Fprintf(w, "%s\n", cs.Bold(release.TagName))
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
