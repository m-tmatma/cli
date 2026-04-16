package root

import (
	"fmt"
	"io"
	"testing"

	"github.com/cli/cli/v2/internal/ghrepo"
	"github.com/cli/cli/v2/internal/prompter"
	"github.com/cli/cli/v2/pkg/extensions"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOfficialExtensionRun_NonTTY(t *testing.T) {
	ios, _, _, stderr := iostreams.Test()
	// non-TTY by default

	ext := &extensions.OfficialExtension{Name: "stack", Owner: "github", Repo: "gh-stack"}
	em := &extensions.ExtensionManagerMock{}
	p := &prompter.PrompterMock{}

	err := officialExtensionRun(ios, p, em, ext, nil)
	require.NoError(t, err)

	assert.Contains(t, stderr.String(), "gh stack")
	assert.Contains(t, stderr.String(), "gh extension install github/gh-stack")
}

func TestOfficialExtensionRun_TTY_Confirmed(t *testing.T) {
	ios, _, _, stderr := iostreams.Test()
	ios.SetStdinTTY(true)
	ios.SetStdoutTTY(true)
	ios.SetStderrTTY(true)

	ext := &extensions.OfficialExtension{Name: "stack", Owner: "github", Repo: "gh-stack"}
	var installedRepo ghrepo.Interface
	var dispatchedArgs []string
	em := &extensions.ExtensionManagerMock{
		InstallFunc: func(repo ghrepo.Interface, pin string) error {
			installedRepo = repo
			return nil
		},
		DispatchFunc: func(args []string, stdin io.Reader, stdout, stderr io.Writer) (bool, error) {
			dispatchedArgs = args
			return true, nil
		},
	}
	p := &prompter.PrompterMock{
		ConfirmFunc: func(_ string, _ bool) (bool, error) {
			return true, nil
		},
	}

	err := officialExtensionRun(ios, p, em, ext, []string{"--help"})
	require.NoError(t, err)

	require.NotNil(t, installedRepo)
	assert.Equal(t, "github", installedRepo.RepoOwner())
	assert.Equal(t, "gh-stack", installedRepo.RepoName())
	assert.Equal(t, "github.com", installedRepo.RepoHost())
	assert.Contains(t, stderr.String(), "Successfully installed github/gh-stack")
	assert.Equal(t, []string{"stack", "--help"}, dispatchedArgs)
}

func TestOfficialExtensionRun_TTY_Declined(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	ios.SetStdinTTY(true)
	ios.SetStdoutTTY(true)
	ios.SetStderrTTY(true)

	ext := &extensions.OfficialExtension{Name: "stack", Owner: "github", Repo: "gh-stack"}
	em := &extensions.ExtensionManagerMock{}
	p := &prompter.PrompterMock{
		ConfirmFunc: func(_ string, _ bool) (bool, error) {
			return false, nil
		},
	}

	err := officialExtensionRun(ios, p, em, ext, nil)
	require.NoError(t, err)

	assert.Empty(t, em.InstallCalls())
}

func TestOfficialExtensionRun_TTY_PromptError(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	ios.SetStdinTTY(true)
	ios.SetStdoutTTY(true)
	ios.SetStderrTTY(true)

	ext := &extensions.OfficialExtension{Name: "stack", Owner: "github", Repo: "gh-stack"}
	em := &extensions.ExtensionManagerMock{}
	p := &prompter.PrompterMock{
		ConfirmFunc: func(_ string, _ bool) (bool, error) {
			return false, fmt.Errorf("prompt interrupted")
		},
	}

	err := officialExtensionRun(ios, p, em, ext, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompt interrupted")
}

func TestOfficialExtensionRun_TTY_InstallError(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	ios.SetStdinTTY(true)
	ios.SetStdoutTTY(true)
	ios.SetStderrTTY(true)

	ext := &extensions.OfficialExtension{Name: "stack", Owner: "github", Repo: "gh-stack"}
	em := &extensions.ExtensionManagerMock{
		InstallFunc: func(_ ghrepo.Interface, _ string) error {
			return fmt.Errorf("network error")
		},
	}
	p := &prompter.PrompterMock{
		ConfirmFunc: func(_ string, _ bool) (bool, error) {
			return true, nil
		},
	}

	err := officialExtensionRun(ios, p, em, ext, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "network error")
}

func TestOfficialExtensionRun_TTY_DispatchError(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	ios.SetStdinTTY(true)
	ios.SetStdoutTTY(true)
	ios.SetStderrTTY(true)

	ext := &extensions.OfficialExtension{Name: "stack", Owner: "github", Repo: "gh-stack"}
	em := &extensions.ExtensionManagerMock{
		InstallFunc: func(_ ghrepo.Interface, _ string) error {
			return nil
		},
		DispatchFunc: func(_ []string, _ io.Reader, _, _ io.Writer) (bool, error) {
			return false, fmt.Errorf("dispatch failed")
		},
	}
	p := &prompter.PrompterMock{
		ConfirmFunc: func(_ string, _ bool) (bool, error) {
			return true, nil
		},
	}

	err := officialExtensionRun(ios, p, em, ext, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dispatch failed")
}

func TestNewCmdOfficialExtension_Properties(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	ext := &extensions.OfficialExtension{Name: "stack", Owner: "github", Repo: "gh-stack"}
	em := &extensions.ExtensionManagerMock{}
	p := &prompter.PrompterMock{}

	cmd := NewCmdOfficialExtension(ios, p, em, ext)

	assert.Equal(t, "stack", cmd.Use)
	assert.True(t, cmd.Hidden)
	assert.Equal(t, "extension", cmd.GroupID)
	assert.True(t, cmd.DisableFlagParsing)
	assert.Equal(t, "true", cmd.Annotations["skipAuthCheck"])
}
