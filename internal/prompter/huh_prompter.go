package prompter

import (
	"fmt"
	"slices"
	"sync"

	"charm.land/huh/v2"
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
		WithTheme(huh.ThemeFunc(huh.ThemeBase16)).
		WithInput(p.stdin).
		WithOutput(p.stdout)
}

func (p *huhPrompter) buildSelectForm(prompt, defaultValue string, options []string) (*huh.Form, *int) {
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

	form := p.newForm(
		huh.NewGroup(
			huh.NewSelect[int]().
				Title(prompt).
				Value(&result).
				Options(formOptions...),
		),
	)
	return form, &result
}

func (p *huhPrompter) Select(prompt, defaultValue string, options []string) (int, error) {
	form, result := p.buildSelectForm(prompt, defaultValue, options)
	err := form.Run()
	return *result, err
}

func (p *huhPrompter) buildMultiSelectForm(prompt string, defaults []string, options []string) (*huh.Form, *[]int) {
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

	form := p.newForm(
		huh.NewGroup(
			huh.NewMultiSelect[int]().
				Title(prompt).
				Value(&result).
				Limit(len(options)).
				Options(formOptions...),
		),
	)
	return form, &result
}

func (p *huhPrompter) MultiSelect(prompt string, defaults []string, options []string) ([]int, error) {
	form, result := p.buildMultiSelectForm(prompt, defaults, options)
	err := form.Run()
	if err != nil {
		return nil, err
	}
	return *result, nil
}

// searchOptionsBinding is used as the OptionsFunc binding for MultiSelectWithSearch.
// By including both the search query and selected values, the binding hash changes
// whenever either changes. This prevents huh's internal Eval cache from serving
// stale option sets that would overwrite the user's current selections.
type searchOptionsBinding struct {
	Query    *string
	Selected *[]string
}

// syncAccessor is a thread-safe huh.Accessor implementation.
// huh calls OptionsFunc from a goroutine while the main event loop
// writes field values via Set(). This accessor synchronizes both
// paths through the same mutex.
type syncAccessor[T any] struct {
	mu    *sync.Mutex
	value T
}

func (a *syncAccessor[T]) Get() T {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.value
}

func (a *syncAccessor[T]) Set(value T) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.value = value
}

func (p *huhPrompter) buildMultiSelectWithSearchForm(prompt, searchPrompt string, defaultValues, persistentValues []string, searchFunc func(string) MultiSelectSearchResult) (*huh.Form, *syncAccessor[[]string]) {
	var mu sync.Mutex

	queryAccessor := &syncAccessor[string]{mu: &mu}
	selectAccessor := &syncAccessor[[]string]{mu: &mu, value: slices.Clone(defaultValues)}

	optionKeyLabels := make(map[string]string)
	for _, k := range defaultValues {
		optionKeyLabels[k] = k
	}

	// Cache searchFunc results locally keyed by query string.
	// This avoids redundant calls when the OptionsFunc binding hash changes
	// due to selection changes (not query changes).
	searchCacheValid := false
	var cachedSearchQuery string
	var cachedSearchResult MultiSelectSearchResult

	buildOptions := func() []huh.Option[string] {
		mu.Lock()
		query := queryAccessor.value
		needsFetch := !searchCacheValid || query != cachedSearchQuery
		mu.Unlock()

		if needsFetch {
			result := searchFunc(query)
			mu.Lock()
			cachedSearchResult = result
			cachedSearchQuery = query
			searchCacheValid = true
			mu.Unlock()
		}

		mu.Lock()
		defer mu.Unlock()

		selectedValues := selectAccessor.value
		result := cachedSearchResult

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
			formOptions = append(formOptions, huh.NewOption(l, k).Selected(true))
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

	binding := &searchOptionsBinding{
		Query:    &queryAccessor.value,
		Selected: &selectAccessor.value,
	}

	form := p.newForm(
		huh.NewGroup(
			huh.NewInput().
				Title(searchPrompt).
				Accessor(queryAccessor),
			huh.NewMultiSelect[string]().
				Title(prompt).
				Options(buildOptions()...).
				OptionsFunc(func() []huh.Option[string] {
					return buildOptions()
				}, binding).
				Accessor(selectAccessor).
				Limit(0),
		),
	)
	return form, selectAccessor
}

func (p *huhPrompter) MultiSelectWithSearch(prompt, searchPrompt string, defaultValues, persistentValues []string, searchFunc func(string) MultiSelectSearchResult) ([]string, error) {
	form, accessor := p.buildMultiSelectWithSearchForm(prompt, searchPrompt, defaultValues, persistentValues, searchFunc)
	err := form.Run()
	if err != nil {
		return nil, err
	}
	return accessor.Get(), nil
}

func (p *huhPrompter) buildInputForm(prompt, defaultValue string) (*huh.Form, *string) {
	result := defaultValue
	form := p.newForm(
		huh.NewGroup(
			huh.NewInput().
				Title(prompt).
				Value(&result),
		),
	)
	return form, &result
}

func (p *huhPrompter) Input(prompt, defaultValue string) (string, error) {
	form, result := p.buildInputForm(prompt, defaultValue)
	err := form.Run()
	return *result, err
}

func (p *huhPrompter) buildPasswordForm(prompt string) (*huh.Form, *string) {
	var result string
	form := p.newForm(
		huh.NewGroup(
			huh.NewInput().
				EchoMode(huh.EchoModePassword).
				Title(prompt).
				Value(&result),
		),
	)
	return form, &result
}

func (p *huhPrompter) Password(prompt string) (string, error) {
	form, result := p.buildPasswordForm(prompt)
	err := form.Run()
	if err != nil {
		return "", err
	}
	return *result, nil
}

func (p *huhPrompter) buildConfirmForm(prompt string, defaultValue bool) (*huh.Form, *bool) {
	result := defaultValue
	form := p.newForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(prompt).
				Value(&result),
		),
	)
	return form, &result
}

func (p *huhPrompter) Confirm(prompt string, defaultValue bool) (bool, error) {
	form, result := p.buildConfirmForm(prompt, defaultValue)
	err := form.Run()
	if err != nil {
		return false, err
	}
	return *result, nil
}

func (p *huhPrompter) buildAuthTokenForm() (*huh.Form, *string) {
	var result string
	form := p.newForm(
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
	)
	return form, &result
}

func (p *huhPrompter) AuthToken() (string, error) {
	form, result := p.buildAuthTokenForm()
	err := form.Run()
	return *result, err
}

func (p *huhPrompter) buildConfirmDeletionForm(requiredValue string) *huh.Form {
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
	)
}

func (p *huhPrompter) ConfirmDeletion(requiredValue string) error {
	return p.buildConfirmDeletionForm(requiredValue).Run()
}

func (p *huhPrompter) buildInputHostnameForm() (*huh.Form, *string) {
	var result string
	form := p.newForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Hostname:").
				Validate(ghinstance.HostnameValidator).
				Value(&result),
		),
	)
	return form, &result
}

func (p *huhPrompter) InputHostname() (string, error) {
	form, result := p.buildInputHostnameForm()
	err := form.Run()
	if err != nil {
		return "", err
	}
	return *result, nil
}

func (p *huhPrompter) buildMarkdownEditorForm(prompt string, blankAllowed bool) (*huh.Form, *string) {
	var result string
	skipOption := "skip"
	launchOption := "launch"
	options := []huh.Option[string]{
		huh.NewOption(fmt.Sprintf("Launch %s", surveyext.EditorName(p.editorCmd)), launchOption),
	}
	if blankAllowed {
		options = append(options, huh.NewOption("Skip", skipOption))
	}

	form := p.newForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(prompt).
				Options(options...).
				Value(&result),
		),
	)
	return form, &result
}

func (p *huhPrompter) MarkdownEditor(prompt, defaultValue string, blankAllowed bool) (string, error) {
	form, result := p.buildMarkdownEditorForm(prompt, blankAllowed)
	err := form.Run()
	if err != nil {
		return "", err
	}

	if *result == "skip" {
		return "", nil
	}

	text, err := surveyext.Edit(p.editorCmd, "*.md", defaultValue, p.stdin, p.stdout, p.stderr)
	if err != nil {
		return "", err
	}

	return text, nil
}
