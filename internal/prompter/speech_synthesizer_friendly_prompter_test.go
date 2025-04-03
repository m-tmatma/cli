//go:build !windows

package prompter_test

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Netflix/go-expect"
	"github.com/cli/cli/v2/internal/prompter"
	"github.com/creack/pty"
	"github.com/hinshun/vt10x"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpeechSynthesizerFriendlyPrompter(t *testing.T) {
	// Create a PTY and hook up a virtual terminal emulator
	ptm, pts, err := pty.Open()
	require.NoError(t, err)

	term := vt10x.New(vt10x.WithWriter(pts))

	// Create a console via Expect that allows scripting against the terminal
	consoleOpts := []expect.ConsoleOpt{
		expect.WithStdin(ptm),
		expect.WithStdout(term),
		expect.WithCloser(ptm, pts),
		failOnExpectError(t),
		failOnSendError(t),
		expect.WithDefaultTimeout(time.Second * 600),
	}

	console, err := expect.NewConsole(consoleOpts...)
	require.NoError(t, err)
	t.Cleanup(func() { testCloser(t, console) })

	// Using OS here because huh currently ignores configured iostreams
	// See https://github.com/charmbracelet/huh/issues/612
	stdIn := os.Stdin
	stdOut := os.Stdout
	stdErr := os.Stderr

	t.Cleanup(func() {
		os.Stdin = stdIn
		os.Stdout = stdOut
		os.Stderr = stdErr
	})

	os.Stdin = console.Tty()
	os.Stdout = console.Tty()
	os.Stderr = console.Tty()

	t.Setenv("GH_SPEECH_SYNTHESIZER_FRIENDLY_PROMPTER", "true")
	p := prompter.New("", nil, nil, nil)

	t.Run("Select", func(t *testing.T) {
		go func() {
			// Wait for prompt to appear
			_, err := console.ExpectString("Choose:")
			require.NoError(t, err)

			// Select option 1
			_, err = console.SendLine("1")
			require.NoError(t, err)
		}()

		selectValue, err := p.Select("Select a number", "", []string{"1", "2", "3"})
		require.NoError(t, err)

		assert.Equal(t, 0, selectValue)
	})

	t.Run("MultiSelect", func(t *testing.T) {
		go func() {
			// Wait for prompt to appear
			_, err := console.ExpectString("Select a number")
			require.NoError(t, err)

			// Select options 1 and 2
			_, err = console.SendLine("1")
			require.NoError(t, err)
			_, err = console.SendLine("2")
			require.NoError(t, err)

			// This confirms selections
			_, err = console.SendLine("0")
			require.NoError(t, err)
		}()

		multiSelectValue, err := p.MultiSelect("Select a number", []string{}, []string{"1", "2", "3"})
		require.NoError(t, err)

		assert.Equal(t, []int{0, 1}, multiSelectValue)
	})

	t.Run("Input", func(t *testing.T) {
		dummyText := "12345abcdefg"
		go func() {
			// Wait for prompt to appear
			_, err := console.ExpectString("Enter some characters")
			require.NoError(t, err)

			// Enter a number
			_, err = console.SendLine(dummyText)
			require.NoError(t, err)
		}()

		inputValue, err := p.Input("Enter some characters", "")
		require.NoError(t, err)

		assert.Equal(t, dummyText, inputValue)
	})

	t.Run("Password", func(t *testing.T) {
		dummyPassword := "12345abcdefg"
		go func() {
			// Wait for prompt to appear
			_, err := console.ExpectString("Enter password")
			require.NoError(t, err)

			// Enter a number
			_, err = console.SendLine(dummyPassword)
			require.NoError(t, err)
		}()

		passwordValue, err := p.Password("Enter password")
		require.NoError(t, err)
		require.Equal(t, dummyPassword, passwordValue)
	})

	t.Run("Confirm", func(t *testing.T) {
		go func() {
			// Wait for prompt to appear
			_, err := console.ExpectString("Are you sure")
			require.NoError(t, err)

			// Confirm
			_, err = console.SendLine("y")
			require.NoError(t, err)
		}()

		confirmValue, err := p.Confirm("Are you sure", false)
		require.NoError(t, err)
		require.Equal(t, true, confirmValue)
	})

	// TODO: Need one that enters invalid input
	// TODO: write tests for control-c
	t.Run("AuthToken", func(t *testing.T) {
		go func() {
			// Wait for prompt to appear
			_, err := console.ExpectString("Paste your authentication token:")
			require.NoError(t, err)

			// Enter a number
			_, err = console.SendLine("12345abcdefg")
			require.NoError(t, err)
		}()

		authValue, err := p.AuthToken()
		require.NoError(t, err)
		require.Equal(t, "12345abcdefg", authValue)
	})

	t.Run("ConfirmDeletion", func(t *testing.T) {
		requiredValue := "test"
		go func() {
			// Wait for prompt to appear
			_, err := console.ExpectString(fmt.Sprintf("Type %q to confirm deletion", requiredValue))
			require.NoError(t, err)

			// Confirm
			_, err = console.SendLine(requiredValue)
			require.NoError(t, err)
		}()

		// An err indicates that the confirmation text sent did not match
		err := p.ConfirmDeletion(requiredValue)
		require.NoError(t, err)
	})

	t.Run("InputHostname", func(t *testing.T) {
		hostname := "somethingdoesnotmatter.com"
		go func() {
			// Wait for prompt to appear
			_, err := console.ExpectString("Hostname:")
			require.NoError(t, err)

			// Enter the hostname
			_, err = console.SendLine(hostname)
			require.NoError(t, err)
		}()

		inputValue, err := p.InputHostname()
		require.NoError(t, err)
		require.Equal(t, hostname, inputValue)
	})

	t.Run("MarkdownEditor - blank allowed", func(t *testing.T) {
		go func() {
			// Wait for prompt to appear
			_, err := console.ExpectString("How to edit?")
			require.NoError(t, err)

			// Enter 2, to select "skip"
			_, err = console.SendLine("2")
			require.NoError(t, err)
		}()

		inputValue, err := p.MarkdownEditor("How to edit?", "", true)
		require.NoError(t, err)
		require.Equal(t, "", inputValue)
	})

	t.Run("MarkdownEditor - blank disallowed", func(t *testing.T) {
		go func() {
			// Wait for prompt to appear
			_, err := console.ExpectString("How to edit?")
			require.NoError(t, err)

			// Enter number 2 to select "skip". This shoudln't be allowed.
			_, err = console.SendLine("2")
			require.NoError(t, err)

			// Expect a notice to enter something valid since blank is disallowed.
			_, err = console.ExpectString("invalid input. please try again")
			require.NoError(t, err)

			// Send a 1 to select to open the editor.
			// Sending the input won't fail, so we expect no error here.
			// See below though, since we expect the editor to fail to open.
			_, err = console.SendLine("1")
			require.NoError(t, err)
		}()

		// However, here we do expect an error because the editor program
		// is intentionally empty and will fail.
		inputValue, err := p.MarkdownEditor("How to edit?", "", false)
		require.Error(t, err)
		require.Equal(t, "", inputValue)
	})
}

// failOnExpectError adds an observer that will fail the test in a standardised way
// if any expectation on the command output fails, without requiring an explicit
// assertion.
//
// Use WithRelaxedIO to disable this behaviour.
func failOnExpectError(t testing.TB) expect.ConsoleOpt {
	t.Helper()
	return expect.WithExpectObserver(
		func(matchers []expect.Matcher, buf string, err error) {
			t.Helper()

			if err == nil {
				return
			}

			if len(matchers) == 0 {
				t.Fatalf("Error occurred while matching %q: %s\n", buf, err)
			}

			var criteria []string
			for _, matcher := range matchers {
				criteria = append(criteria, fmt.Sprintf("%q", matcher.Criteria()))
			}
			t.Fatalf("Failed to find [%s] in %q: %s\n", strings.Join(criteria, ", "), buf, err)
		},
	)
}

// failOnSendError adds an observer that will fail the test in a standardised way
// if any sending of input fails, without requiring an explicit assertion.
//
// Use WithRelaxedIO to disable this behaviour.
func failOnSendError(t testing.TB) expect.ConsoleOpt {
	t.Helper()
	return expect.WithSendObserver(
		func(msg string, n int, err error) {
			t.Helper()

			if err != nil {
				t.Fatalf("Failed to send %q: %s\n", msg, err)
			}
			if len(msg) != n {
				t.Fatalf("Only sent %d of %d bytes for %q\n", n, len(msg), msg)
			}
		},
	)
}

// testCloser is a helper to fail the test if a Closer fails to close.
func testCloser(t testing.TB, closer io.Closer) {
	t.Helper()
	if err := closer.Close(); err != nil {
		t.Errorf("Close failed: %s", err)
	}
}
