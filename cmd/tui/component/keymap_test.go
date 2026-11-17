package component

import (
	"testing"

	"github.com/asciimoo/nogon/config"

	tea "charm.land/bubbletea/v2"
)

func TestActionDefinitionsCoverConfigActions(t *testing.T) {
	seen := make(map[config.Action]bool, len(actionDefinitions))
	for _, definition := range actionDefinitions {
		if definition.label == "" || seen[definition.action] {
			t.Fatalf("invalid or duplicate action definition: %#v", definition)
		}
		seen[definition.action] = true
	}
	for action := range config.ValidTUIActions {
		if !seen[action] {
			t.Errorf("valid TUI action %q has no keymap definition", action)
		}
	}
	for action := range seen {
		if !config.ValidTUIActions[action] {
			t.Errorf("keymap definition %q is not a valid TUI action", action)
		}
	}
}

func keyPress(text string) tea.KeyPressMsg {
	runes := []rune(text)
	return tea.KeyPressMsg(tea.Key{Text: text, Code: runes[0]})
}

func TestKeyMapResolvesAndRebuildsBindings(t *testing.T) {
	keys := NewKeyMap(map[string]string{
		"j": string(config.ActionScrollDown),
		"y": string(config.ActionCopyResult),
	})

	if got := keys.Action(keyPress("j")); got != config.ActionScrollDown {
		t.Fatalf("Action(j) = %q, want %q", got, config.ActionScrollDown)
	}

	keys.Rebuild(map[string]string{"x": string(config.ActionCopyResult)})
	if got := keys.Action(keyPress("y")); got != "" {
		t.Fatalf("old binding still active after rebuild: %q", got)
	}
	if got := keys.Action(keyPress("x")); got != config.ActionCopyResult {
		t.Fatalf("Action(x) = %q, want %q", got, config.ActionCopyResult)
	}
	if got := keys.BestKey(config.ActionCopyResult); got != "x" {
		t.Fatalf("BestKey(copy_result) = %q, want x", got)
	}
}

func TestShortHelpIsContextual(t *testing.T) {
	keys := NewKeyMap(config.DefaultTUIHotkeys)
	addHints := keys.For(ContextAdd).ShortHints()

	want := map[config.Action]bool{
		config.ActionToggleFocus: true,
		config.ActionOpenResult:  true,
		config.ActionToggleHelp:  true,
	}
	if len(addHints) != len(want) {
		t.Fatalf("add hint count = %d, want %d", len(addHints), len(want))
	}
	for _, hint := range addHints {
		if !want[hint.Action] {
			t.Fatalf("unexpected add hint %q", hint.Action)
		}
	}
}

func TestSearchHelpMatchesKeyboardOwnership(t *testing.T) {
	keys := NewKeyMap(config.DefaultTUIHotkeys)
	inputHints := keys.For(ContextSearchInput).ShortHints()
	inputActions := make(map[config.Action]bool, len(inputHints))
	for _, hint := range inputHints {
		inputActions[hint.Action] = true
	}
	for _, unavailable := range []config.Action{
		config.ActionOpenResult,
		config.ActionCopyResult,
		config.ActionTogglePreview,
		config.ActionDeleteResult,
	} {
		if inputActions[unavailable] {
			t.Errorf("typing help advertises unavailable action %q", unavailable)
		}
	}
	if len(inputHints) == 0 || inputHints[0].Binding.Help().Key != "↓/⇥" {
		t.Fatalf("typing help does not expose result focus: %#v", inputHints)
	}

	resultActions := map[config.Action]bool{}
	for _, hint := range keys.For(ContextSearchResults).ShortHints() {
		resultActions[hint.Action] = true
	}
	for _, available := range []config.Action{
		config.ActionToggleFocus,
		config.ActionOpenResult,
		config.ActionCopyResult,
		config.ActionTogglePreview,
	} {
		if !resultActions[available] {
			t.Errorf("result help omits available action %q", available)
		}
	}
}

func TestSearchFocusHintUsesConfiguredBindings(t *testing.T) {
	keys := NewKeyMap(map[string]string{
		"ctrl+n": string(config.ActionScrollDown),
		"f2":     string(config.ActionToggleFocus),
	})
	hints := keys.For(ContextSearchInput).ShortHints()
	if len(hints) != 1 {
		t.Fatalf("input hint count = %d, want 1", len(hints))
	}
	want := FormatKey("ctrl+n") + "/" + FormatKey("f2")
	if got := hints[0].Binding.Help().Key; got != want {
		t.Fatalf("focus hint = %q, want %q", got, want)
	}

	keys.Rebuild(map[string]string{"ctrl+n": string(config.ActionScrollDown)})
	hints = keys.For(ContextSearchInput).ShortHints()
	if len(hints) != 1 || hints[0].Action != config.ActionScrollDown {
		t.Fatalf("navigation-only focus hint = %#v", hints)
	}

	keys.Rebuild(map[string]string{"j": string(config.ActionScrollDown)})
	if hints := keys.For(ContextSearchInput).ShortHints(); len(hints) != 0 {
		t.Fatalf("typing-only focus hints = %#v, want none", hints)
	}
}
