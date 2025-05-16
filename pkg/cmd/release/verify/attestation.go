package verify

import (
	"errors"
	"fmt"

	"github.com/cli/cli/v2/internal/text"
	"github.com/cli/cli/v2/pkg/cmd/attestation/api"
	"github.com/cli/cli/v2/pkg/cmd/attestation/artifact"
	"github.com/cli/cli/v2/pkg/cmd/attestation/verification"
)

func getAttestations(o *Options, sha string) ([]*api.Attestation, string, error) {
	if o.APIClient == nil {
		errMsg := "✗ No APIClient provided"
		return nil, errMsg, errors.New(errMsg)
	}

	params := api.FetchParams{
		Digest:        sha,
		Limit:         o.Limit,
		Owner:         o.Owner,
		PredicateType: o.PredicateType,
		Repo:          o.Repo,
	}

	attestations, err := o.APIClient.GetByDigest(params)
	if err != nil {
		msg := "✗ Loading attestations from GitHub API failed"
		return nil, msg, err
	}
	pluralAttestation := text.Pluralize(len(attestations), "attestation")
	msg := fmt.Sprintf("Loaded %s from GitHub API", pluralAttestation)
	return attestations, msg, nil
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
	// certExtVerified, err := verification.VerifyCertExtensions(sigstoreVerified, ec)
	// if err != nil {
	// 	logMsg := "✗ Policy verification failed"
	// 	return nil, logMsg, err
	// }

	return sigstoreVerified, "", nil
}
