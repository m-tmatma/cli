package prompter

import (
	"testing"

	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/stretchr/testify/require"
)

func TestNewReturnsAccessiblePrompter(t *testing.T) {
	editorCmd := "nothing"
	ios, _, _, _ := iostreams.Test()
	stdin := ios.In
	stdout := ios.Out
	stderr := ios.ErrOut

	t.Run("returns SpeechSynthesizerFriendlyPrompter when GH_SCREENREADER_FRIENDLY is set to true", func(t *testing.T) {
		t.Setenv("GH_SCREENREADER_FRIENDLY", "true")

		p := New(editorCmd, stdin, stdout, stderr)

		require.IsType(t, &speechSynthesizerFriendlyPrompter{}, p, "expected SpeechSynthesizerFriendlyPrompter to be returned")
		require.Equal(t, p.(*speechSynthesizerFriendlyPrompter).IsAccessible(), true, "expected SpeechSynthesizerFriendlyPrompter to be accessible")
	})

	t.Run("returns SpeechSynthesizerFriendlyPrompter when GH_SCREENREADER_FRIENDLY is set to 1", func(t *testing.T) {
		t.Setenv("GH_SCREENREADER_FRIENDLY", "1")

		p := New(editorCmd, stdin, stdout, stderr)

		require.IsType(t, &speechSynthesizerFriendlyPrompter{}, p, "expected SpeechSynthesizerFriendlyPrompter to be returned")
		require.Equal(t, p.(*speechSynthesizerFriendlyPrompter).IsAccessible(), true, "expected SpeechSynthesizerFriendlyPrompter to be accessible")
	})

	t.Run("returns surveyPrompter when GH_SCREENREADER_FRIENDLY is set to false", func(t *testing.T) {
		t.Setenv("GH_SCREENREADER_FRIENDLY", "false")

		p := New(editorCmd, stdin, stdout, stderr)

		require.IsType(t, &surveyPrompter{}, p, "expected surveyPrompter to be returned")
	})

	t.Run("returns surveyPrompter when GH_SCREENREADER_FRIENDLY is set to 0", func(t *testing.T) {
		t.Setenv("GH_SCREENREADER_FRIENDLY", "0")

		p := New(editorCmd, stdin, stdout, stderr)

		require.IsType(t, &surveyPrompter{}, p, "expected surveyPrompter to be returned")
	})

	t.Run("returns surveyPrompter when GH_SCREENREADER_FRIENDLY is unset", func(t *testing.T) {
		t.Setenv("GH_SCREENREADER_FRIENDLY", "")

		p := New(editorCmd, stdin, stdout, stderr)

		require.IsType(t, &surveyPrompter{}, p, "expected surveyPrompter to be returned")
	})
}
