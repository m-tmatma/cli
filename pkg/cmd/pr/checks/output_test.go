package checks

import (
	"testing"

	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrintSummary(t *testing.T) {
	tests := []struct {
		name   string
		counts checkCounts
		want   string
	}{
		{
			name:   "no checks",
			counts: checkCounts{},
			want:   "\n\n",
		},
		{
			name:   "only cancelled checks",
			counts: checkCounts{Canceled: 2},
			want:   "Some checks were cancelled\n2 cancelled, 0 failing, 0 successful, 0 skipped, and 0 pending checks\n\n",
		},
		{
			name:   "cancelled and passing checks",
			counts: checkCounts{Canceled: 1, Passed: 2},
			want:   "Some checks were cancelled\n1 cancelled, 0 failing, 2 successful, 0 skipped, and 0 pending checks\n\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ios, _, stdout, _ := iostreams.Test()
			ios.SetStdoutTTY(true)

			printSummary(ios, tt.counts)

			require.Equal(t, tt.want, stdout.String())
		})
	}

	// Regression guard: a check set containing only cancelled checks must still
	// produce a summary. Before the fix, the guard in printSummary omitted
	// counts.Canceled, so a cancelled-only result printed an empty summary.
	t.Run("cancelled-only is not silently empty", func(t *testing.T) {
		ios, _, stdout, _ := iostreams.Test()
		ios.SetStdoutTTY(true)

		printSummary(ios, checkCounts{Canceled: 1})

		assert.Contains(t, stdout.String(), "Some checks were cancelled")
	})
}
