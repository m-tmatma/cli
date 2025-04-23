package accessibility

import (
	"fmt"

	"github.com/MakeNowJust/heredoc"
	"github.com/cli/cli/v2/internal/browser"
	"github.com/cli/cli/v2/internal/text"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/spf13/cobra"
)

const (
	communityURL = "https://github.com/orgs/community/discussions/categories/accessibility"
)

type AccessibilityOptions struct {
	IO      *iostreams.IOStreams
	Browser browser.Browser
	Web     bool
}

func NewCmdAccessibility(f *cmdutil.Factory) *cobra.Command {
	opts := AccessibilityOptions{
		IO:      f.IOStreams,
		Browser: f.Browser,
	}

	cmd := &cobra.Command{
		Use:     "accessibility",
		Aliases: []string{"a11y"},
		Short:   "Learn about GitHub CLI accessibility experience",
		Long:    longDescription(opts.IO),
		Hidden:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.Web {
				if opts.IO.IsStdoutTTY() {
					fmt.Fprintf(opts.IO.ErrOut, "Opening %s in your browser.\n", text.DisplayURL(communityURL))
				}
				return opts.Browser.Browse(communityURL)
			}

			return cmd.Help()
		},
		Example: heredoc.Doc(`
			# Open the GitHub Community Accessibility discussions in your browser
			$ gh accessibility --web

			# Display color using customizable, 4-bit accessible colors
			$ gh config set accessible_colors enabled

			# Display issue and pull request labels using RGB hex color codes in terminals that support 24-bit truecolor
			$ gh config set color_labels enabled

			# Use input prompts without redrawing the screen
			$ gh config set accessible_prompter enabled

			# Disable motion-based spinners for progress indicators in favor of text
			$ gh config set spinner disabled
		`),
	}

	cmd.Flags().BoolVarP(&opts.Web, "web", "w", false, "Open the GitHub Community Accessibility discussions in the browser")
	cmdutil.DisableAuthCheck(cmd)

	return cmd
}

func longDescription(io *iostreams.IOStreams) string {
	cs := io.ColorScheme()
	title := cs.Bold("LEARN ABOUT GITHUB CLI ACCESSIBILITY EFFORTS")
	color := cs.Bold("CUSTOMIZABLE AND CONTRASTING COLORS")
	prompter := cs.Bold("NON-INTERACTIVE USER INPUT PROMPTING")
	spinner := cs.Bold("TEXT-BASED SPINNERS")

	return heredoc.Docf(`
		%[2]s

		As the home for all developers, we want every developer to feel welcome in our
		community and be empowered to contribute to the future of global software
		development with everything GitHub has to offer including the GitHub CLI.

		We invite you to join us in improving GitHub CLI accessibility by sharing your
		feedback and ideas in the GitHub Community Accessibility discussions:
		%[3]s


		%[4]s

		Color is a common approach to enhance user experiences, however users can find
		themselves with a worse experience due to insufficient contrast or
		customizability.

		To create an accessible experience, CLIs should use color palettes based on
		terminal background appearance and limit colors to 4-bit ANSI color palettes,
		which users can customize within terminal preferences.

		With this new experience, the GitHub CLI provides multiple options to address
		color usage:

		1. The GitHub CLI will use 4-bit color palette for increased color contrast based on
		   dark and light backgrounds including rendering markdown based on GitHub Primer.

		   To enable this experience, use one of the following methods:
		   - Run %[1]sgh config set accessible_colors enabled%[1]s
		   - Set %[1]sGH_ACCESSIBLE_COLORS=enabled%[1]s environment variable

		2. The GitHub CLI will display issue and pull request labels' custom RGB colors
		   in terminals with truecolor support.

		   To enable this experience, use one of the following methods:
		   - Run %[1]sgh config set color_labels enabled%[1]s
		   - Set %[1]sGH_COLOR_LABELS=enabled%[1]s environment variable


		%[5]s

		Interactive text user interfaces are an advanced approach to enhance user
		experiences, which manipulate the terminal cursor to redraw parts of the screen.
		However, this can be difficult for speech synthesizers or braille displays to
		accurately detect and read.

		To create an accessible experience, CLIs should give users the ability to disable
		this interactivity while providing a similar experience.

		With this new experience, the GitHub CLI will use non-interactive prompts for
		user input.

		To enable this experience, use one of the following methods:
		- Run %[1]sgh config set accessible_prompter enabled%[1]s
		- Set %[1]sGH_ACCESSIBLE_PROMPTER=enabled%[1]s environment variable


		%[6]s

		Motion-based spinners are a common approach to communicate activity, which
		manipulate the terminal cursor to create a spinning effect. However, this can be
		difficult for users with motion sensitivity as well as speech synthesizers.

		To create an accessible experience, CLIs should give users the ability to disable
		this interactivity while providing a similar experience.

		With this new experience, the GitHub CLI will use text-based progress indicators.

		To enable this experience, use one of the following methods:
		- Run %[1]sgh config set spinner disabled%[1]s
		- Set %[1]sGH_SPINNER_DISABLED=yes%[1]s environment variable
	`, "`", title, communityURL, color, prompter, spinner)
}
