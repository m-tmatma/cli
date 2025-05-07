//go:build integration

package inspect

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/cli/cli/v2/pkg/cmd/attestation/verification"
	"github.com/cli/cli/v2/pkg/cmdutil"

	//"github.com/cli/cli/v2/pkg/httpmock"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/stretchr/testify/assert"
)

func TestNewInspectCmd(t *testing.T) {
	testIO, _, _, _ := iostreams.Test()
	f := &cmdutil.Factory{
		IOStreams: testIO,
		HttpClient: func() (*http.Client, error) {
			//reg := &httpmock.Registry{}
			client := http.DefaultClient
			//httpmock.ReplaceTripper(client, reg)
			return client, nil
		},
	}

	testcases := []struct {
		name          string
		cli           string
		wants         Options
		wantsErr      bool
		wantsExporter bool
	}{
		{
			name: "Prints output in JSON format",
			cli:  fmt.Sprintf("%s --format json", bundlePath),
			wants: Options{
				BundlePath:       bundlePath,
				SigstoreVerifier: verification.NewMockSigstoreVerifier(t),
			},
			wantsExporter: true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			var opts *Options
			cmd := NewInspectCmd(f, func(o *Options) error {
				opts = o
				return nil
			})

			argv := strings.Split(tc.cli, " ")
			cmd.SetArgs(argv)
			cmd.SetIn(&bytes.Buffer{})
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			_, err := cmd.ExecuteC()
			if tc.wantsErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)

			assert.Equal(t, tc.wants.BundlePath, opts.BundlePath)
			assert.NotNil(t, opts.Logger)
			assert.Equal(t, tc.wantsExporter, opts.exporter != nil)
		})
	}
}
