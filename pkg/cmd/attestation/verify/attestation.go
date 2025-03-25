package verify

import (
	"fmt"

	"github.com/cli/cli/v2/internal/text"
	"github.com/cli/cli/v2/pkg/cmd/attestation/api"
	"github.com/cli/cli/v2/pkg/cmd/attestation/artifact"
	"github.com/cli/cli/v2/pkg/cmd/attestation/verification"
)

func filterByPredicateType(predicateType string, attestations []*api.Attestation) ([]*api.Attestation, string, error) {
	// Apply predicate type filter to returned attestations
	filteredAttestations := verification.FilterAttestations(predicateType, attestations)
	if len(filteredAttestations) == 0 {
		msg := fmt.Sprintf("✗ No attestations found with predicate type: %s\n", predicateType)
		return nil, msg, fmt.Errorf("no matching predicate found")
	}
	return filteredAttestations, "", nil
}

func getAttestations(o *Options, a artifact.DigestedArtifact) ([]*api.Attestation, string, error) {
	if o.FetchAttestationsFromGitHubAPI() {
		params := api.FetchParams{
			Digest:        a.DigestWithAlg(),
			Limit:         o.Limit,
			Owner:         o.Owner,
			PredicateType: o.PredicateType,
			Repo:          o.Repo,
		}

		attestations, err := verification.GetRemoteAttestations(o.APIClient, params)
		if err != nil {
			msg := "✗ Loading attestations from GitHub API failed"
			return nil, msg, err
		}
		pluralAttestation := text.Pluralize(len(attestations), "attestation")
		msg := fmt.Sprintf("Loaded %s from GitHub API", pluralAttestation)
		return attestations, msg, nil
	}

	var attestations []*api.Attestation
	var err error
	var errMsg string
	if o.BundlePath != "" {
		attestations, err = verification.GetLocalAttestations(o.BundlePath)
		if err != nil {
			errMsg = fmt.Sprintf("✗ Loading attestations from %s failed", a.URL)
		}
	} else if o.UseBundleFromRegistry {
		attestations, err = verification.GetOCIAttestations(o.OCIClient, a)
		if err != nil {
			errMsg = "✗ Loading attestations from OCI registry failed"
		}
	}

	if err != nil {
		return nil, errMsg, err
	}

	filtered, errMsg, err := filterByPredicateType(o.PredicateType, attestations)
	if err != nil {
		return nil, errMsg, err
	}

	pluralAttestation := text.Pluralize(len(filtered), "attestation")
	msg := fmt.Sprintf("Loaded %s from %s", pluralAttestation, o.BundlePath)
	return filtered, msg, nil
}

func verifyAttestations(art artifact.DigestedArtifact, att []*api.Attestation, sgVerifier verification.SigstoreVerifier, ec verification.EnforcementCriteria) ([]*verification.AttestationProcessingResult, string, error) {
	sgPolicy, err := buildSigstoreVerifyPolicy(ec, art)
	if err != nil {
		logMsg := "✗ Failed to build Sigstore verification policy"
		return nil, logMsg, err
	}

	sigstoreVerified, err := sgVerifier.Verify(att, sgPolicy)
	if err != nil {
		logMsg := "✗ Sigstore verification failed"
		return nil, logMsg, err
	}

	// Verify extensions
	certExtVerified, err := verification.VerifyCertExtensions(sigstoreVerified, ec)
	if err != nil {
		logMsg := "✗ Policy verification failed"
		return nil, logMsg, err
	}

	return certExtVerified, "", nil
}
