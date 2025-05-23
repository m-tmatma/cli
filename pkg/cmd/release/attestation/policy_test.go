package attestation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewEnforcementCriteria(t *testing.T) {
	t.Run("check SAN", func(t *testing.T) {
		opts := &AttestOptions{
			Owner:         "foo",
			Repo:          "foo/bar",
			PredicateType: "https://in-toto.io/attestation/release/v0.1",
		}

		c, err := NewEnforcementCriteria(opts)
		require.NoError(t, err)
		require.Equal(t, "https://dotcom.releases.github.com", c.SAN)
		require.Equal(t, "https://in-toto.io/attestation/release/v0.1", c.PredicateType)
	})

	t.Run("sets Extensions.SourceRepositoryURI using opts.Repo and opts.Tenant", func(t *testing.T) {
		opts := &AttestOptions{
			Owner:  "foo",
			Repo:   "foo/bar",
			Tenant: "baz",
		}

		c, err := NewEnforcementCriteria(opts)
		require.NoError(t, err)
		require.Equal(t, "https://baz.ghe.com/foo/bar", c.Certificate.SourceRepositoryURI)
	})

	t.Run("sets Extensions.SourceRepositoryURI using opts.Repo", func(t *testing.T) {
		opts := &AttestOptions{
			Owner: "foo",
			Repo:  "foo/bar",
		}

		c, err := NewEnforcementCriteria(opts)
		require.NoError(t, err)
		require.Equal(t, "https://github.com/foo/bar", c.Certificate.SourceRepositoryURI)
	})

	t.Run("sets Extensions.SourceRepositoryOwnerURI using opts.Owner and opts.Tenant", func(t *testing.T) {
		opts := &AttestOptions{

			Owner:  "foo",
			Repo:   "foo/bar",
			Tenant: "baz",
		}

		c, err := NewEnforcementCriteria(opts)
		require.NoError(t, err)
		require.Equal(t, "https://baz.ghe.com/foo", c.Certificate.SourceRepositoryOwnerURI)
	})

	t.Run("sets Extensions.SourceRepositoryOwnerURI using opts.Owner", func(t *testing.T) {
		opts := &AttestOptions{

			Owner: "foo",
			Repo:  "foo/bar",
		}

		c, err := NewEnforcementCriteria(opts)
		require.NoError(t, err)
		require.Equal(t, "https://github.com/foo", c.Certificate.SourceRepositoryOwnerURI)
	})

}
