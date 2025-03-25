package api

import (
	"fmt"

	"github.com/cli/cli/v2/pkg/cmd/attestation/test/data"
)

type MockClient struct {
	OnGetByRepoAndDigest  func(params FetchParams) ([]*Attestation, error)
	OnGetByOwnerAndDigest func(params FetchParams) ([]*Attestation, error)
	OnGetTrustDomain      func() (string, error)
}

func (m MockClient) GetByRepoAndDigest(params FetchParams) ([]*Attestation, error) {
	return m.OnGetByRepoAndDigest(params)
}

func (m MockClient) GetByOwnerAndDigest(params FetchParams) ([]*Attestation, error) {
	return m.OnGetByOwnerAndDigest(params)
}

func (m MockClient) GetTrustDomain() (string, error) {
	return m.OnGetTrustDomain()
}

func makeTestAttestation() Attestation {
	return Attestation{Bundle: data.SigstoreBundle(nil), BundleURL: "https://example.com"}
}

func OnGetByRepoAndDigestSuccess(params FetchParams) ([]*Attestation, error) {
	att1 := makeTestAttestation()
	att2 := makeTestAttestation()
	return []*Attestation{&att1, &att2}, nil
}

func OnGetByRepoAndDigestFailure(params FetchParams) ([]*Attestation, error) {
	return nil, fmt.Errorf("failed to fetch by repo and digest")
}

func OnGetByOwnerAndDigestSuccess(params FetchParams) ([]*Attestation, error) {
	att1 := makeTestAttestation()
	att2 := makeTestAttestation()
	return []*Attestation{&att1, &att2}, nil
}

func OnGetByOwnerAndDigestFailure(params FetchParams) ([]*Attestation, error) {
	return nil, fmt.Errorf("failed to fetch by owner and digest")
}

func NewTestClient() *MockClient {
	return &MockClient{
		OnGetByRepoAndDigest:  OnGetByRepoAndDigestSuccess,
		OnGetByOwnerAndDigest: OnGetByOwnerAndDigestSuccess,
	}
}

func NewFailTestClient() *MockClient {
	return &MockClient{
		OnGetByRepoAndDigest:  OnGetByRepoAndDigestFailure,
		OnGetByOwnerAndDigest: OnGetByOwnerAndDigestFailure,
	}
}
