package shared

import (
	"fmt"

	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
	"github.com/sigstore/sigstore-go/pkg/verify"

	"github.com/cli/cli/v2/pkg/cmd/attestation/artifact"
	"github.com/cli/cli/v2/pkg/cmd/attestation/verification"
)

func expandToGitHubURL(tenant, ownerOrRepo string) string {
	if tenant == "" {
		return fmt.Sprintf("https://github.com/%s", ownerOrRepo)
	}
	return fmt.Sprintf("https://%s.ghe.com/%s", tenant, ownerOrRepo)
}

func NewEnforcementCriteria(opts *AttestOptions) (verification.EnforcementCriteria, error) {
	// initialize the enforcement criteria with the provided PredicateType and SAN
	c := verification.EnforcementCriteria{
		PredicateType: opts.PredicateType,
		// TODO: if the proxima is provided, the default uses the proxima-specific SAN
		SAN: "https://dotcom.releases.github.com",
	}

	// If the Repo option is provided, set the SourceRepositoryURI extension
	if opts.Repo != "" {
		c.Certificate.SourceRepositoryURI = expandToGitHubURL(opts.Tenant, opts.Repo)
	}

	// Set the SourceRepositoryOwnerURI extension using owner and tenant if provided
	c.Certificate.SourceRepositoryOwnerURI = expandToGitHubURL(opts.Tenant, opts.Owner)

	return c, nil
}

func buildCertificateIdentityOption(c verification.EnforcementCriteria) (verify.PolicyOption, error) {
	sanMatcher, err := verify.NewSANMatcher(c.SAN, c.SANRegex)
	if err != nil {
		return nil, err
	}

	// Accept any issuer, we will verify the issuer as part of the extension verification
	issuerMatcher, err := verify.NewIssuerMatcher("", ".*")
	if err != nil {
		return nil, err
	}

	extensions := certificate.Extensions{
		RunnerEnvironment: c.Certificate.RunnerEnvironment,
	}

	certId, err := verify.NewCertificateIdentity(sanMatcher, issuerMatcher, extensions)
	if err != nil {
		return nil, err
	}

	return verify.WithCertificateIdentity(certId), nil
}

func buildSigstoreVerifyPolicy(c verification.EnforcementCriteria, a artifact.DigestedArtifact) (verify.PolicyBuilder, error) {
	artifactDigestPolicyOption, err := verification.BuildDigestPolicyOption(a)
	if err != nil {
		return verify.PolicyBuilder{}, err
	}

	certIdOption, err := buildCertificateIdentityOption(c)
	if err != nil {
		return verify.PolicyBuilder{}, err
	}

	policy := verify.NewPolicy(artifactDigestPolicyOption, certIdOption)
	return policy, nil
}
