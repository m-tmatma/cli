package cmdutil

import (
	"net/http"

	"github.com/cli/cli/v2/context"
	"github.com/cli/cli/v2/git"
	"github.com/cli/cli/v2/internal/browser"
	"github.com/cli/cli/v2/internal/gh"
	"github.com/cli/cli/v2/internal/ghrepo"
	"github.com/cli/cli/v2/internal/prompter"
	"github.com/cli/cli/v2/pkg/extensions"
	"github.com/cli/cli/v2/pkg/iostreams"
)

type Factory struct {
	AppVersion     string
	ExecutablePath string
	InvokingAgent  string

	Browser          browser.Browser
	ExtensionManager extensions.ExtensionManager
	GitClient        *git.Client
	IOStreams        *iostreams.IOStreams
	Prompter         prompter.Prompter

	BaseRepo func() (ghrepo.Interface, error)
	Branch   func() (string, error)
	Cfg      gh.Config
	// TODO: Config should be removed in favour of cfg being passed to the right place,
	// but this is going to be very invasive and shouldn't be done as part of a feature change.
	Config     func() (gh.Config, error)
	HttpClient func() (*http.Client, error)
	// PlainHttpClient is a special HTTP client that does not automatically set
	// auth and other headers. This is meant to be used in situations where the
	// client needs to specify the headers itself (e.g. during login).
	PlainHttpClient func() (*http.Client, error)
	Remotes         func() (context.Remotes, error)
}
