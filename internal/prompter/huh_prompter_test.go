package prompter

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helpers adapted from huh's own test suite (huh_test.go).

func batchUpdate(m huh.Model, cmd tea.Cmd) huh.Model {
	if cmd == nil {
		return m
	}
	msg := cmd()
	m, cmd = m.Update(msg)
	if cmd == nil {
		return m
	}
	msg = cmd()
	m, _ = m.Update(msg)
	return m
}

func codeKeypress(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: r})
}

func keypress(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{
		Text:        string(r),
		Code:        r,
		ShiftedCode: r,
	})
}

func typeText(m huh.Model, s string) huh.Model {
	for _, r := range s {
		m, _ = m.Update(keypress(r))
	}
	return m
}

func viewStripped(m huh.Model) string {
	return ansi.Strip(m.View())
}

func shiftTabKeypress() tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyTab, Mod: tea.ModShift})
}

func newTestHuhPrompter() *huhPrompter {
	return &huhPrompter{}
}

// doAllUpdates processes all batched commands from the form, including async
// OptionsFunc evaluations. Adapted from huh's own test suite. Uses iterative
// rounds with a depth limit to prevent infinite loops from cascading binding updates.
func doAllUpdates(f *huh.Form, cmd tea.Cmd) {
	for range 3 {
		if cmd == nil {
			return
		}
		cmds := expandBatch(cmd)
		var next []tea.Cmd
		for _, c := range cmds {
			if c == nil {
				continue
			}
			_, result := f.Update(c())
			if result != nil {
				next = append(next, result)
			}
		}
		if len(next) == 0 {
			return
		}
		cmd = tea.Batch(next...)
	}
}

// expandBatch flattens nested tea.BatchMsg into a flat slice of commands.
func expandBatch(cmd tea.Cmd) []tea.Cmd {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var all []tea.Cmd
		for _, sub := range batch {
			all = append(all, expandBatch(sub)...)
		}
		return all
	}
	return []tea.Cmd{func() tea.Msg { return msg }}
}

func TestHuhPrompterInput(t *testing.T) {
	tests := []struct {
		name         string
		defaultValue string
		input        string
		wantResult   string
	}{
		{
			name:       "basic input",
			input:      "hello",
			wantResult: "hello",
		},
		{
			name:         "default value returned when no input",
			defaultValue: "default",
			wantResult:   "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestHuhPrompter()
			f, result := p.buildInputForm("Name:", tt.defaultValue)
			f.Update(f.Init())

			var m huh.Model = f
			if tt.input != "" {
				m = typeText(m, tt.input)
			}
			batchUpdate(m.Update(codeKeypress(tea.KeyEnter)))

			require.Equal(t, tt.wantResult, *result)
		})
	}
}

func TestHuhPrompterSelect(t *testing.T) {
	tests := []struct {
		name         string
		options      []string
		defaultValue string
		keys         []tea.KeyPressMsg // keypresses before Enter
		wantIndex    int
	}{
		{
			name:      "selects first option by default",
			options:   []string{"a", "b", "c"},
			wantIndex: 0,
		},
		{
			name:         "respects default value",
			options:      []string{"a", "b", "c"},
			defaultValue: "b",
			wantIndex:    1,
		},
		{
			name:         "invalid default selects first",
			options:      []string{"a", "b", "c"},
			defaultValue: "z",
			wantIndex:    0,
		},
		{
			name:      "navigate down one",
			options:   []string{"a", "b", "c"},
			keys:      []tea.KeyPressMsg{keypress('j')},
			wantIndex: 1,
		},
		{
			name:      "navigate down two",
			options:   []string{"a", "b", "c"},
			keys:      []tea.KeyPressMsg{keypress('j'), keypress('j')},
			wantIndex: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestHuhPrompter()
			f, result := p.buildSelectForm("Pick:", tt.defaultValue, tt.options)
			f.Update(f.Init())

			var m huh.Model = f
			for _, k := range tt.keys {
				m = batchUpdate(m.Update(k))
			}
			batchUpdate(m.Update(codeKeypress(tea.KeyEnter)))

			require.Equal(t, tt.wantIndex, *result)
		})
	}
}

func TestHuhPrompterMultiSelect(t *testing.T) {
	tests := []struct {
		name       string
		options    []string
		defaults   []string
		keys       []tea.KeyPressMsg
		wantResult []int
	}{
		{
			name:       "no defaults and no toggles returns empty",
			options:    []string{"a", "b", "c"},
			wantResult: []int{},
		},
		{
			name:       "defaults are pre-selected",
			options:    []string{"a", "b", "c"},
			defaults:   []string{"a", "c"},
			wantResult: []int{0, 2},
		},
		{
			name:    "toggle first option",
			options: []string{"a", "b", "c"},
			keys:    []tea.KeyPressMsg{keypress('x')},
			wantResult: []int{0},
		},
		{
			name:    "toggle multiple options",
			options: []string{"a", "b", "c"},
			keys: []tea.KeyPressMsg{
				keypress('x'),          // toggle a
				keypress('j'),          // move to b
				keypress('j'),          // move to c
				keypress('x'),          // toggle c
			},
			wantResult: []int{0, 2},
		},
		{
			name:     "invalid defaults are excluded",
			options:  []string{"a", "b"},
			defaults: []string{"z"},
			wantResult: []int{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestHuhPrompter()
			f, result := p.buildMultiSelectForm("Pick:", tt.defaults, tt.options)
			f.Update(f.Init())

			var m huh.Model = f
			for _, k := range tt.keys {
				m = batchUpdate(m.Update(k))
			}
			batchUpdate(m.Update(codeKeypress(tea.KeyEnter)))

			require.Equal(t, tt.wantResult, *result)
		})
	}
}

func TestHuhPrompterConfirm(t *testing.T) {
	tests := []struct {
		name         string
		defaultValue bool
		keys         []tea.KeyPressMsg
		wantResult   bool
	}{
		{
			name:         "default false submitted as-is",
			defaultValue: false,
			wantResult:   false,
		},
		{
			name:         "default true submitted as-is",
			defaultValue: true,
			wantResult:   true,
		},
		{
			name:         "toggle from false to true with left arrow",
			defaultValue: false,
			keys:         []tea.KeyPressMsg{codeKeypress(tea.KeyLeft)},
			wantResult:   true,
		},
		{
			name:         "toggle from true to false with right arrow",
			defaultValue: true,
			keys:         []tea.KeyPressMsg{codeKeypress(tea.KeyRight)},
			wantResult:   false,
		},
		{
			name:         "accept with y key",
			defaultValue: false,
			keys:         []tea.KeyPressMsg{keypress('y')},
			wantResult:   true,
		},
		{
			name:         "reject with n key",
			defaultValue: true,
			keys:         []tea.KeyPressMsg{keypress('n')},
			wantResult:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestHuhPrompter()
			f, result := p.buildConfirmForm("Sure?", tt.defaultValue)
			f.Update(f.Init())

			var m huh.Model = f
			for _, k := range tt.keys {
				m = batchUpdate(m.Update(k))
			}
			batchUpdate(m.Update(codeKeypress(tea.KeyEnter)))

			require.Equal(t, tt.wantResult, *result)
		})
	}
}

func TestHuhPrompterPassword(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantResult string
	}{
		{
			name:       "basic password",
			input:      "s3cret",
			wantResult: "s3cret",
		},
		{
			name:       "empty password",
			wantResult: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestHuhPrompter()
			f, result := p.buildPasswordForm("Password:")
			f.Update(f.Init())

			var m huh.Model = f
			if tt.input != "" {
				m = typeText(m, tt.input)
			}
			batchUpdate(m.Update(codeKeypress(tea.KeyEnter)))

			require.Equal(t, tt.wantResult, *result)
		})
	}
}

func TestHuhPrompterMarkdownEditor(t *testing.T) {
	tests := []struct {
		name         string
		blankAllowed bool
		keys         []tea.KeyPressMsg
		wantResult   string
	}{
		{
			name:         "selects launch by default",
			blankAllowed: true,
			wantResult:   "launch",
		},
		{
			name:         "navigate to skip",
			blankAllowed: true,
			keys:         []tea.KeyPressMsg{keypress('j')},
			wantResult:   "skip",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestHuhPrompter()
			f, result := p.buildMarkdownEditorForm("Body:", tt.blankAllowed)
			f.Update(f.Init())

			var m huh.Model = f
			for _, k := range tt.keys {
				m = batchUpdate(m.Update(k))
			}
			batchUpdate(m.Update(codeKeypress(tea.KeyEnter)))

			require.Equal(t, tt.wantResult, *result)
		})
	}
}

func TestHuhPrompterMultiSelectWithSearch(t *testing.T) {
	staticSearchFunc := func(query string) MultiSelectSearchResult {
		if query == "" {
			return MultiSelectSearchResult{
				Keys:   []string{"result-a", "result-b"},
				Labels: []string{"Result A", "Result B"},
			}
		}
		return MultiSelectSearchResult{
			Keys:   []string{"search-1", "search-2"},
			Labels: []string{"Search 1", "Search 2"},
		}
	}

	tests := []struct {
		name       string
		defaults   []string
		persistent []string
		keys       []tea.KeyPressMsg
		wantResult []string
	}{
		{
			name: "defaults are pre-selected and returned on immediate submit",
			defaults: []string{"result-a"},
			keys: []tea.KeyPressMsg{
				// Tab past the search input to the multi-select, then submit.
				codeKeypress(tea.KeyTab),
				codeKeypress(tea.KeyEnter),
			},
			wantResult: []string{"result-a"},
		},
		{
			name: "toggle an option from search results",
			keys: []tea.KeyPressMsg{
				codeKeypress(tea.KeyTab),   // advance to multi-select
				keypress('x'),             // toggle first option (result-a)
				codeKeypress(tea.KeyEnter), // submit
			},
			wantResult: []string{"result-a"},
		},
		{
			name: "toggle multiple options",
			keys: []tea.KeyPressMsg{
				codeKeypress(tea.KeyTab),   // advance to multi-select
				keypress('x'),             // toggle result-a
				keypress('j'),             // move to result-b
				keypress('x'),             // toggle result-b
				codeKeypress(tea.KeyEnter), // submit
			},
			wantResult: []string{"result-a", "result-b"},
		},
		{
			name: "no selection returns empty",
			keys: []tea.KeyPressMsg{
				codeKeypress(tea.KeyTab),
				codeKeypress(tea.KeyEnter),
			},
			wantResult: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestHuhPrompter()
			f, result := p.buildMultiSelectWithSearchForm(
				"Select", "Search", tt.defaults, tt.persistent, staticSearchFunc,
			)
			doAllUpdates(f, f.Init())

			for _, k := range tt.keys {
				_, cmd := f.Update(k)
				doAllUpdates(f, cmd)
			}

			assert.Equal(t, tt.wantResult, *result)
		})
	}
}

func TestHuhPrompterMultiSelectWithSearchPersistence(t *testing.T) {
	callCount := 0
	staticSearchFunc := func(query string) MultiSelectSearchResult {
		callCount++
		if query == "" {
			return MultiSelectSearchResult{
				Keys:   []string{"result-a", "result-b"},
				Labels: []string{"Result A", "Result B"},
			}
		}
		return MultiSelectSearchResult{
			Keys:   []string{"search-1", "search-2"},
			Labels: []string{"Search 1", "Search 2"},
		}
	}

	t.Run("selections persist after changing search query", func(t *testing.T) {
		p := newTestHuhPrompter()
		f, result := p.buildMultiSelectWithSearchForm(
			"Select", "Search", nil, nil, staticSearchFunc,
		)
		doAllUpdates(f, f.Init())

		steps := []tea.KeyPressMsg{
			// Tab to multi-select, toggle result-a.
			codeKeypress(tea.KeyTab),
			keypress('x'),
			// Shift+Tab back to search input, type "foo".
			shiftTabKeypress(),
			keypress('f'), keypress('o'), keypress('o'),
			// Tab back to multi-select — result-a should still be selected.
			codeKeypress(tea.KeyTab),
			// Submit.
			codeKeypress(tea.KeyEnter),
		}

		for _, k := range steps {
			_, cmd := f.Update(k)
			doAllUpdates(f, cmd)
		}

		assert.Equal(t, []string{"result-a"}, *result)
	})
}
