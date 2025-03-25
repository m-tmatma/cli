package api

import (
	"fmt"

	"github.com/cli/cli/v2/pkg/cmd/attestation/test/data"
)

func makeTestAttestation() Attestation {
	return Attestation{Bundle: data.SigstoreBundle(nil), BundleURL: "https://example.com"}
}

type MockClient struct {
	OnGetByDigest    func(params FetchParams) ([]*Attestation, error)
	OnGetTrustDomain func() (string, error)
}

func (m MockClient) GetByDigest(params FetchParams) ([]*Attestation, error) {
	return m.OnGetByDigest(params)
}

func (m MockClient) GetTrustDomain() (string, error) {
	return m.OnGetTrustDomain()
}

func OnGetByDigestSuccess(params FetchParams) ([]*Attestation, error) {
	att1 := makeTestAttestation()
	att2 := makeTestAttestation()
	return []*Attestation{&att1, &att2}, nil
}

func OnGetByDigestFailure(params FetchParams) ([]*Attestation, error) {
	if params.Repo != "" {
		return nil, fmt.Errorf("failed to fetch by repo and digest")
	}
	return nil, fmt.Errorf("failed to fetch by owner and digest")
}

func NewTestClient() *MockClient {
	return &MockClient{
		OnGetByDigest: OnGetByDigestSuccess,
	}
}

func NewFailTestClient() *MockClient {
	return &MockClient{
		OnGetByDigest: OnGetByDigestFailure,
	}
}
