package attestation

import (
	"errors"
	"fmt"

	"github.com/cli/cli/v2/internal/text"
	"github.com/cli/cli/v2/pkg/cmd/attestation/api"
	"github.com/cli/cli/v2/pkg/cmd/attestation/artifact"
	"github.com/cli/cli/v2/pkg/cmd/attestation/verification"

	att_io "github.com/cli/cli/v2/pkg/cmd/attestation/io"
	v1 "github.com/in-toto/attestation/go/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

func GetAttestations(o *AttestOptions, sha string) ([]*api.Attestation, string, error) {
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

func VerifyAttestations(art artifact.DigestedArtifact, att []*api.Attestation, sgVerifier verification.SigstoreVerifier, ec verification.EnforcementCriteria) ([]*verification.AttestationProcessingResult, string, error) {
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

func FilterAttestationsByPURL(attestations []*api.Attestation, repo, tagName string, logger *att_io.Handler) []*api.Attestation {
	var filtered []*api.Attestation
	expectedPURL := "pkg:github/" + repo + "@" + tagName
	for _, att := range attestations {
		statement := att.Bundle.Bundle.GetDsseEnvelope().Payload
		var statementData v1.Statement
		err := protojson.Unmarshal([]byte(statement), &statementData)
		if err != nil {
			logger.Println(logger.ColorScheme.Red("✗ Failed to unmarshal statement"))
			continue
		}
		purlValue := statementData.Predicate.GetFields()["purl"]
		var purl string
		if purlValue != nil {
			purl = purlValue.GetStringValue()
		}
		if purl == expectedPURL {
			filtered = append(filtered, att)
		}
	}
	return filtered
}

func FilterAttestationsByFileDigest(attestations []*api.Attestation, repo, tagName, fileDigest string, logger *att_io.Handler) []*api.Attestation {
	var filtered []*api.Attestation
	for _, att := range attestations {
		statement := att.Bundle.Bundle.GetDsseEnvelope().Payload
		var statementData v1.Statement
		err := protojson.Unmarshal([]byte(statement), &statementData)

		if err != nil {
			logger.Println(logger.ColorScheme.Red("✗ Failed to unmarshal statement"))
			continue
		}
		subjects := statementData.Subject
		for _, subject := range subjects {
			digestMap := subject.GetDigest()
			alg := "sha256"

			digest := digestMap[alg]
			if digest == fileDigest {
				filtered = append(filtered, att)
			}
		}

	}
	return filtered
}
