package prompter

import (
	"fmt"
	"slices"

	"github.com/charmbracelet/huh"
	"github.com/cli/cli/v2/internal/ghinstance"
	"github.com/cli/cli/v2/pkg/surveyext"
	ghPrompter "github.com/cli/go-gh/v2/pkg/prompter"
)

type huhPrompter struct {
	stdin     ghPrompter.FileReader
	stdout    ghPrompter.FileWriter
	stderr    ghPrompter.FileWriter
	editorCmd string
}

func (p *huhPrompter) newForm(groups ...*huh.Group) *huh.Form {
	return huh.NewForm(groups...).
		WithTheme(huh.ThemeBase16()).
		WithInput(p.stdin).
		WithOutput(p.stdout)
}

func (p *huhPrompter) Select(prompt, defaultValue string, options []string) (int, error) {
	var result int

	if !slices.Contains(options, defaultValue) {
		defaultValue = ""
	}

	formOptions := make([]huh.Option[int], len(options))
	for i, o := range options {
		if defaultValue == o {
			result = i
		}
		formOptions[i] = huh.NewOption(o, i)
	}

	err := p.newForm(
		huh.NewGroup(
			huh.NewSelect[int]().
				Title(prompt).
				Value(&result).
				Options(formOptions...),
		),
	).Run()

	return result, err
}

func (p *huhPrompter) MultiSelect(prompt string, defaults []string, options []string) ([]int, error) {
	var result []int

	defaults = slices.DeleteFunc(defaults, func(s string) bool {
		return !slices.Contains(options, s)
	})

	formOptions := make([]huh.Option[int], len(options))
	for i, o := range options {
		if slices.Contains(defaults, o) {
			result = append(result, i)
		}
		formOptions[i] = huh.NewOption(o, i)
	}

	err := p.newForm(
		huh.NewGroup(
			huh.NewMultiSelect[int]().
				Title(prompt).
				Value(&result).
				Limit(len(options)).
				Options(formOptions...),
		),
	).Run()

	if err != nil {
		return nil, err
	}
	return result, nil
}

func (p *huhPrompter) MultiSelectWithSearch(prompt, searchPrompt string, defaultValues, persistentValues []string, searchFunc func(string) MultiSelectSearchResult) ([]string, error) {
	selectedValues := make([]string, len(defaultValues))
	copy(selectedValues, defaultValues)

	optionKeyLabels := make(map[string]string)
	for _, k := range selectedValues {
		optionKeyLabels[k] = k
	}

	buildOptions := func(query string) []huh.Option[string] {
		result := searchFunc(query)
		if result.Err != nil {
			return nil
		}
		for i, k := range result.Keys {
			optionKeyLabels[k] = result.Labels[i]
		}

		var formOptions []huh.Option[string]
		seen := make(map[string]bool)

		// 1. Currently selected values (persisted across searches).
		for _, k := range selectedValues {
			if seen[k] {
				continue
			}
			seen[k] = true
			l := optionKeyLabels[k]
			if l == "" {
				l = k
			}
			formOptions = append(formOptions, huh.NewOption(l, k))
		}

		// 2. Search results.
		for i, k := range result.Keys {
			if seen[k] {
				continue
			}
			seen[k] = true
			l := result.Labels[i]
			if l == "" {
				l = k
			}
			formOptions = append(formOptions, huh.NewOption(l, k))
		}

		// 3. Persistent options.
		for _, k := range persistentValues {
			if seen[k] {
				continue
			}
			seen[k] = true
			l := optionKeyLabels[k]
			if l == "" {
				l = k
			}
			formOptions = append(formOptions, huh.NewOption(l, k))
		}

		if len(formOptions) == 0 {
			formOptions = append(formOptions, huh.NewOption("No results", ""))
		}

		return formOptions
	}

	var searchQuery string

	err := p.newForm(
		huh.NewGroup(
			huh.NewInput().
				Title(searchPrompt).
				Value(&searchQuery),
			huh.NewMultiSelect[string]().
				Title(prompt).
				Options(buildOptions("")...).
				OptionsFunc(func() []huh.Option[string] {
					return buildOptions(searchQuery)
				}, &searchQuery).
				Value(&selectedValues).
				Limit(0),
		),
	).Run()
	if err != nil {
		return nil, err
	}

	return selectedValues, nil
}

func (p *huhPrompter) Input(prompt, defaultValue string) (string, error) {
	result := defaultValue

	err := p.newForm(
		huh.NewGroup(
			huh.NewInput().
				Title(prompt).
				Value(&result),
		),
	).Run()

	return result, err
}

func (p *huhPrompter) Password(prompt string) (string, error) {
	var result string

	err := p.newForm(
		huh.NewGroup(
			huh.NewInput().
				EchoMode(huh.EchoModePassword).
				Title(prompt).
				Value(&result),
		),
	).Run()

	if err != nil {
		return "", err
	}
	return result, nil
}

func (p *huhPrompter) Confirm(prompt string, defaultValue bool) (bool, error) {
	result := defaultValue

	err := p.newForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(prompt).
				Value(&result),
		),
	).Run()

	if err != nil {
		return false, err
	}
	return result, nil
}

func (p *huhPrompter) AuthToken() (string, error) {
	var result string

	err := p.newForm(
		huh.NewGroup(
			huh.NewInput().
				EchoMode(huh.EchoModePassword).
				Title("Paste your authentication token:").
				Validate(func(input string) error {
					if input == "" {
						return fmt.Errorf("token is required")
					}
					return nil
				}).
				Value(&result),
		),
	).Run()

	return result, err
}

func (p *huhPrompter) ConfirmDeletion(requiredValue string) error {
	return p.newForm(
		huh.NewGroup(
			huh.NewInput().
				Title(fmt.Sprintf("Type %q to confirm deletion", requiredValue)).
				Validate(func(input string) error {
					if input != requiredValue {
						return fmt.Errorf("You entered: %q", input)
					}
					return nil
				}),
		),
	).Run()
}

func (p *huhPrompter) InputHostname() (string, error) {
	var result string

	err := p.newForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Hostname:").
				Validate(ghinstance.HostnameValidator).
				Value(&result),
		),
	).Run()

	if err != nil {
		return "", err
	}
	return result, nil
}

func (p *huhPrompter) MarkdownEditor(prompt, defaultValue string, blankAllowed bool) (string, error) {
	var result string
	skipOption := "skip"
	launchOption := "launch"
	options := []huh.Option[string]{
		huh.NewOption(fmt.Sprintf("Launch %s", surveyext.EditorName(p.editorCmd)), launchOption),
	}
	if blankAllowed {
		options = append(options, huh.NewOption("Skip", skipOption))
	}

	err := p.newForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(prompt).
				Options(options...).
				Value(&result),
		),
	).Run()

	if err != nil {
		return "", err
	}

	if result == skipOption {
		return "", nil
	}

	text, err := surveyext.Edit(p.editorCmd, "*.md", defaultValue, p.stdin, p.stdout, p.stderr)
	if err != nil {
		return "", err
	}

	return text, nil
}
