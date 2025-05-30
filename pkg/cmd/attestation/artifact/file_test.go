package artifact

import (
	"testing"

	"github.com/cli/cli/v2/pkg/cmd/attestation/test"
	"github.com/stretchr/testify/require"
)

func Test_digestLocalFileArtifact_withRealZip(t *testing.T) {
	// Path to the test artifact
	artifactPath := test.NormalizeRelativePath("../../attestation/test/data/github_release_artifact.zip")

	// Calculate expected digest using the same algorithm as the function under test
	expectedDigest := "f7165848f9f5ddc578d7adbd1f566a394169385c73bd88bf60df7e759db8e08d"

	// Call the function under test
	artifact, err := digestLocalFileArtifact(artifactPath, "sha256")
	require.NoError(t, err)
	require.Equal(t, "file://"+artifactPath, artifact.URL)
	require.Equal(t, expectedDigest, artifact.digest)
	require.Equal(t, "sha256", artifact.digestAlg)
}
