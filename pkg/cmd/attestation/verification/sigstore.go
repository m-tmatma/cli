package verification

import (
	"bufio"
	"bytes"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/cli/cli/v2/pkg/cmd/attestation/api"
	"github.com/cli/cli/v2/pkg/cmd/attestation/io"
	o "github.com/cli/cli/v2/pkg/option"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

const (
	PublicGoodIssuerOrg = "sigstore.dev"
	GitHubIssuerOrg     = "GitHub, Inc."
)

// AttestationProcessingResult captures processing a given attestation's signature verification and policy evaluation
type AttestationProcessingResult struct {
	Attestation        *api.Attestation           `json:"attestation"`
	VerificationResult *verify.VerificationResult `json:"verificationResult"`
}

type SigstoreConfig struct {
	TrustedRoot  string
	Logger       *io.Handler
	NoPublicGood bool
	HttpClient   *http.Client
	// If tenancy mode is not used, trust domain is empty
	TrustDomain string
	// TUFMetadataDir
	TUFMetadataDir o.Option[string]
}

type SigstoreVerifier interface {
	Verify(attestations []*api.Attestation, policy verify.PolicyBuilder) ([]*AttestationProcessingResult, error)
}

type LiveSigstoreVerifier struct {
	Logger       *io.Handler
	NoPublicGood bool
	PublicGood   *verify.Verifier
	GitHub       *verify.Verifier
	Custom       map[string]*verify.Verifier
}

var ErrNoAttestationsVerified = errors.New("no attestations were verified")

// NewLiveSigstoreVerifier creates a new LiveSigstoreVerifier struct
// that is used to verify artifacts and attestations against the
// Public Good, GitHub, or a custom trusted root.
func NewLiveSigstoreVerifier(config SigstoreConfig) (*LiveSigstoreVerifier, error) {
	liveVerifier := &LiveSigstoreVerifier{
		Logger:       config.Logger,
		NoPublicGood: config.NoPublicGood,
	}
	// if a custom trusted root is set, configure custom verifiers
	if config.TrustedRoot != "" {
		customVerifiers, err := createCustomVerifiers(config.TrustedRoot, config.NoPublicGood)
		if err != nil {
			return nil, err
		}
		liveVerifier.Custom = customVerifiers
		return liveVerifier, nil
	}
	if !config.NoPublicGood {
		publicGoodVerifier, err := newPublicGoodVerifier(config.TUFMetadataDir, config.HttpClient)
		if err != nil {
			return nil, err
		}
		liveVerifier.PublicGood = publicGoodVerifier
	}
	github, err := newGitHubVerifier(config.TrustDomain, config.TUFMetadataDir, config.HttpClient)
	if err != nil {
		return nil, err
	}
	liveVerifier.GitHub = github

	return liveVerifier, nil
}

func createCustomVerifiers(trustedRoot string, noPublicGood bool) (map[string]*verify.Verifier, error) {
	customTrustRoots, err := os.ReadFile(trustedRoot)
	if err != nil {
		return nil, fmt.Errorf("unable to read file %s: %v", trustedRoot, err)
	}

	verifiers := make(map[string]*verify.Verifier)
	reader := bufio.NewReader(bytes.NewReader(customTrustRoots))
	var line []byte
	var readError error
	line, readError = reader.ReadBytes('\n')
	for readError == nil {
		// Load each trusted root
		trustedRoot, err := root.NewTrustedRootFromJSON(line)
		if err != nil {
			return nil, fmt.Errorf("failed to create custom verifier: %v", err)
		}

		// Compare bundle leafCert issuer with trusted root cert authority
		certAuthorities := trustedRoot.FulcioCertificateAuthorities()
		for _, certAuthority := range certAuthorities {
			fulcioCertAuthority, ok := certAuthority.(*root.FulcioCertificateAuthority)
			if !ok {
				return nil, fmt.Errorf("trusted root cert authority is not a FulcioCertificateAuthority")
			}
			lowestCert, err := getLowestCertInChain(fulcioCertAuthority)
			if err != nil {
				return nil, err
			}

			// if the custom trusted root issuer is not set, skip it
			if len(lowestCert.Issuer.Organization) == 0 {
				continue
			}
			issuer := lowestCert.Issuer.Organization[0]

			// Determine what policy to use with this trusted root.
			//
			// Note that we are *only* inferring the policy with the
			// issuer. We *must* use the trusted root provided.
			switch issuer {
			case PublicGoodIssuerOrg:
				if noPublicGood {
					return nil, fmt.Errorf("detected public good instance but requested verification without public good instance")
				}
				if _, ok := verifiers[PublicGoodIssuerOrg]; ok {
					// we have already created a public good verifier with this custom trusted root
					// so we skip it
					continue
				}
				publicGood, err := newPublicGoodVerifierWithTrustedRoot(trustedRoot)
				if err != nil {
					return nil, err
				}
				verifiers[PublicGoodIssuerOrg] = publicGood
			case GitHubIssuerOrg:
				if _, ok := verifiers[GitHubIssuerOrg]; ok {
					// we have already created a github verifier with this custom trusted root
					// so we skip it
					continue
				}
				github, err := newGitHubVerifierWithTrustedRoot(trustedRoot)
				if err != nil {
					return nil, err
				}
				verifiers[GitHubIssuerOrg] = github
			default:
				if _, ok := verifiers[issuer]; ok {
					// we have already created a custom verifier with this custom trusted root
					// so we skip it
					continue
				}
				// Make best guess at reasonable policy
				custom, err := newCustomVerifier(trustedRoot)
				if err != nil {
					return nil, err
				}
				verifiers[issuer] = custom
			}
		}
		line, readError = reader.ReadBytes('\n')
	}
	return verifiers, nil
}

func getBundleIssuer(b *bundle.Bundle) (string, error) {
	if !b.MinVersion("0.2") {
		return "", fmt.Errorf("unsupported bundle version: %s", b.MediaType)
	}
	verifyContent, err := b.VerificationContent()
	if err != nil {
		return "", fmt.Errorf("failed to get bundle verification content: %v", err)
	}
	leafCert := verifyContent.Certificate()
	if leafCert == nil {
		return "", fmt.Errorf("leaf cert not found")
	}
	if len(leafCert.Issuer.Organization) != 1 {
		return "", fmt.Errorf("expected the leaf certificate issuer to only have one organization")
	}
	return leafCert.Issuer.Organization[0], nil
}

func (v *LiveSigstoreVerifier) chooseVerifier(issuer string) (*verify.Verifier, error) {
	// if no custom trusted root is set, return either the Public Good or GitHub verifier
	// If the chosen verifier has not yet been created, create it as a LiveSigstoreVerifier field for use in future calls
	if v.Custom != nil {
		custom, ok := v.Custom[issuer]
		if !ok {
			return nil, fmt.Errorf("no custom verifier found for issuer \"%s\"", issuer)
		}
		return custom, nil
	}
	switch issuer {
	case PublicGoodIssuerOrg:
		if v.NoPublicGood {
			return nil, fmt.Errorf("detected public good instance but requested verification without public good instance")
		}
		return v.PublicGood, nil
	case GitHubIssuerOrg:
		return v.GitHub, nil
	default:
		return nil, fmt.Errorf("leaf certificate issuer is not recognized")
	}
}

func getLowestCertInChain(ca *root.FulcioCertificateAuthority) (*x509.Certificate, error) {
	if len(ca.Intermediates) > 0 {
		return ca.Intermediates[0], nil
	} else if ca.Root != nil {
		return ca.Root, nil
	}

	return nil, fmt.Errorf("certificate authority had no certificates")
}

func (v *LiveSigstoreVerifier) verify(attestation *api.Attestation, policy verify.PolicyBuilder) (*AttestationProcessingResult, error) {
	issuer, err := getBundleIssuer(attestation.Bundle)
	if err != nil {
		return nil, fmt.Errorf("failed to get bundle issuer: %v", err)
	}

	// determine which verifier should attempt verification against the bundle
	verifier, err := v.chooseVerifier(issuer)
	if err != nil {
		return nil, fmt.Errorf("failed to choose verifier based on provided bundle issuer: %v", err)
	}

	v.Logger.VerbosePrintf("Attempting verification against issuer \"%s\"\n", issuer)
	// attempt to verify the attestation
	result, err := verifier.Verify(attestation.Bundle, policy)
	// if verification fails, create the error and exit verification early
	if err != nil {
		v.Logger.VerbosePrint(v.Logger.ColorScheme.Redf(
			"Failed to verify against issuer \"%s\" \n\n", issuer,
		))

		return nil, fmt.Errorf("verifying with issuer \"%s\"", issuer)
	}

	// if verification is successful, add the result
	// to the AttestationProcessingResult entry
	v.Logger.VerbosePrint(v.Logger.ColorScheme.Greenf(
		"SUCCESS - attestation signature verified with \"%s\"\n", issuer,
	))

	return &AttestationProcessingResult{
		Attestation:        attestation,
		VerificationResult: result,
	}, nil
}

func (v *LiveSigstoreVerifier) Verify(attestations []*api.Attestation, policy verify.PolicyBuilder) ([]*AttestationProcessingResult, error) {
	if len(attestations) == 0 {
		return nil, ErrNoAttestationsVerified
	}

	results := make([]*AttestationProcessingResult, len(attestations))
	var verifyCount int
	var lastError error
	totalAttestations := len(attestations)
	for i, a := range attestations {
		v.Logger.VerbosePrintf("Verifying attestation %d/%d against the configured Sigstore trust roots\n", i+1, totalAttestations)

		apr, err := v.verify(a, policy)
		if err != nil {
			lastError = err
			// move onto the next attestation in the for loop if verification fails
			continue
		}
		// otherwise, add the result to the results slice and increment verifyCount
		results[verifyCount] = apr
		verifyCount++
	}

	if verifyCount == 0 {
		return nil, lastError
	}

	// truncate the results slice to only include verified attestations
	results = results[:verifyCount]

	return results, nil
}

func newCustomVerifier(trustedRoot *root.TrustedRoot) (*verify.Verifier, error) {
	// All we know about this trust root is its configuration so make some
	// educated guesses as to what the policy should be.
	verifierConfig := []verify.VerifierOption{}
	// This requires some independent corroboration of the signing certificate
	// (e.g. from Sigstore Fulcio) time, one of:
	// - a signed timestamp from a timestamp authority in the trusted root
	// - a transparency log entry (e.g. from Sigstore Rekor)
	verifierConfig = append(verifierConfig, verify.WithObserverTimestamps(1))

	// Infer verification options from contents of trusted root
	if len(trustedRoot.RekorLogs()) > 0 {
		verifierConfig = append(verifierConfig, verify.WithTransparencyLog(1))
	}

	gv, err := verify.NewVerifier(trustedRoot, verifierConfig...)
	if err != nil {
		return nil, fmt.Errorf("failed to create custom verifier: %v", err)
	}

	return gv, nil
}

func newGitHubVerifier(trustDomain string, tufMetadataDir o.Option[string], hc *http.Client) (*verify.Verifier, error) {
	var tr string

	opts := GitHubTUFOptions(tufMetadataDir, hc)
	client, err := tuf.New(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create TUF client: %v", err)
	}

	if trustDomain == "" {
		tr = "trusted_root.json"
	} else {
		tr = fmt.Sprintf("%s.trusted_root.json", trustDomain)
	}
	jsonBytes, err := client.GetTarget(tr)
	if err != nil {
		return nil, err
	}
	trustedRoot, err := root.NewTrustedRootFromJSON(jsonBytes)
	if err != nil {
		return nil, err
	}
	return newGitHubVerifierWithTrustedRoot(trustedRoot)
}

func newGitHubVerifierWithTrustedRoot(trustedRoot *root.TrustedRoot) (*verify.Verifier, error) {
	gv, err := verify.NewVerifier(trustedRoot, verify.WithSignedTimestamps(1))
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub verifier: %v", err)
	}

	return gv, nil
}

func newPublicGoodVerifier(tufMetadataDir o.Option[string], hc *http.Client) (*verify.Verifier, error) {
	opts := DefaultOptionsWithCacheSetting(tufMetadataDir, hc)
	client, err := tuf.New(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create TUF client: %v", err)
	}
	trustedRoot, err := root.GetTrustedRoot(client)
	if err != nil {
		return nil, fmt.Errorf("failed to get trusted root: %v", err)
	}

	return newPublicGoodVerifierWithTrustedRoot(trustedRoot)
}

func newPublicGoodVerifierWithTrustedRoot(trustedRoot *root.TrustedRoot) (*verify.Verifier, error) {
	sv, err := verify.NewVerifier(trustedRoot, verify.WithSignedCertificateTimestamps(1), verify.WithTransparencyLog(1), verify.WithObserverTimestamps(1))
	if err != nil {
		return nil, fmt.Errorf("failed to create Public Good verifier: %v", err)
	}

	return sv, nil
}
