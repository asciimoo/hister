// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package component contains reusable, state-independent TUI building blocks.
package component

import (
	"runtime"
	"slices"
	"strings"

	"github.com/asciimoo/hister/config"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// KeyContext selects the concise, task-specific bindings shown in the footer.
type KeyContext uint8

const (
	ContextSearchInput KeyContext = iota
	ContextSearchInputEmpty
	ContextSearchSuggestion
	ContextSearchResults
	ContextHistory
	ContextRules
	ContextAdd
	ContextDetails
)

// Hint ties a visible key binding back to the application action it invokes.
// Keeping this association lets keyboard help and clickable footer hints share
// one source of truth.
type Hint struct {
	Action  config.Action
	Binding key.Binding
}

// KeyMap adapts Nogon's user-configurable action map to the Bubbles key and
// help abstractions. Copies are cheap and can carry a view-specific context.
type KeyMap struct {
	bindings map[config.Action]key.Binding
	context  KeyContext
}

type actionDefinition struct {
	action config.Action
	label  string
}

// actionDefinitions is the ordered catalog used for matching and default help
// text. Keeping both properties together prevents a supported action from
// silently disappearing from one side of the keymap adapter.
var actionDefinitions = []actionDefinition{
	{config.ActionQuit, "quit"},
	{config.ActionToggleHelp, "help"},
	{config.ActionToggleFocus, "change focus"},
	{config.ActionScrollUp, "move up"},
	{config.ActionScrollDown, "move down"},
	{config.ActionOpenResult, "open"},
	{config.ActionCopyResult, "copy URL"},
	{config.ActionTogglePreview, "details"},
	{config.ActionEditLabel, "label"},
	{config.ActionDeleteResult, "delete"},
	{config.ActionToggleTheme, "theme"},
	{config.ActionToggleSettings, "keybindings"},
	{config.ActionToggleSort, "sort"},
	{config.ActionToggleSemantic, "semantic search"},
	{config.ActionTabSearch, "search tab"},
	{config.ActionTabHistory, "history tab"},
	{config.ActionTabRules, "rules tab"},
	{config.ActionTabAdd, "add tab"},
}

func actionLabel(action config.Action) string {
	for _, definition := range actionDefinitions {
		if definition.action == action {
			return definition.label
		}
	}
	return string(action)
}

// NewKeyMap builds Bubbles bindings from the persisted key → action mapping.
func NewKeyMap(hotkeys map[string]string) KeyMap {
	k := KeyMap{}
	k.Rebuild(hotkeys)
	return k
}

// Rebuild applies edited keybindings without requiring an application restart.
func (k *KeyMap) Rebuild(hotkeys map[string]string) {
	grouped := make(map[config.Action][]string)
	for name, action := range hotkeys {
		grouped[config.Action(action)] = append(grouped[config.Action(action)], name)
	}
	k.bindings = make(map[config.Action]key.Binding, len(grouped))
	for action, keys := range grouped {
		slices.Sort(keys)
		formatted := make([]string, 0, len(keys))
		for _, name := range keys {
			formatted = append(formatted, FormatKey(name))
		}
		k.bindings[action] = key.NewBinding(
			key.WithKeys(keys...),
			key.WithHelp(strings.Join(formatted, "/"), actionLabel(action)),
		)
	}
}

// For returns a contextual copy suitable for Bubbles' help.Model.
func (k KeyMap) For(context KeyContext) KeyMap {
	k.context = context
	return k
}

// Action resolves a key press through Bubbles bindings.
func (k KeyMap) Action(msg tea.KeyPressMsg) config.Action {
	for _, definition := range actionDefinitions {
		if binding, ok := k.bindings[definition.action]; ok && key.Matches(msg, binding) {
			return definition.action
		}
	}
	return ""
}

// Binding returns the configured binding for an action.
func (k KeyMap) Binding(action config.Action) (key.Binding, bool) {
	binding, ok := k.bindings[action]
	return binding, ok
}

// BestKey returns the most compact display label for an action's configured
// Bubbles binding. Overlays use this instead of scanning persisted config a
// second time, so edited bindings update every help surface immediately.
func (k KeyMap) BestKey(action config.Action) string {
	return k.bestKey(action, func(string) bool { return true })
}

func (k KeyMap) bestKey(action config.Action, usable func(string) bool) string {
	binding, ok := k.bindings[action]
	if !ok {
		return ""
	}
	best := ""
	for _, name := range binding.Keys() {
		if !usable(name) {
			continue
		}
		formatted := FormatKey(name)
		if best == "" || ansi.StringWidth(formatted) < ansi.StringWidth(best) {
			best = formatted
		}
	}
	return best
}

func (k KeyMap) searchInputKey(action config.Action) string {
	return k.bestKey(action, func(name string) bool { return len([]rune(name)) != 1 })
}

func (k KeyMap) preferredSearchInputKey(action config.Action, preferred string) string {
	binding, ok := k.bindings[action]
	if ok && slices.Contains(binding.Keys(), preferred) {
		return FormatKey(preferred)
	}
	return k.searchInputKey(action)
}

func (k KeyMap) focusResultsKeys() string {
	keys := []string{
		k.searchInputKey(config.ActionScrollDown),
		k.preferredSearchInputKey(config.ActionToggleFocus, "tab"),
	}
	return strings.Join(slices.DeleteFunc(keys, func(key string) bool { return key == "" }), "/")
}

func (k KeyMap) hint(action config.Action, keyLabel, label string) (Hint, bool) {
	binding, ok := k.bindings[action]
	if !ok {
		return Hint{}, false
	}
	help := binding.Help()
	if keyLabel != "" {
		help.Key = keyLabel
	}
	if label != "" {
		help.Desc = label
		binding.SetHelp(help.Key, help.Desc)
	}
	return Hint{Action: action, Binding: binding}, true
}

// ShortHints returns the actions that matter in the current workspace.
func (k KeyMap) ShortHints() []Hint {
	type entry struct {
		action config.Action
		key    string
		label  string
	}
	var entries []entry
	switch k.context {
	case ContextDetails:
		entries = []entry{
			{action: config.ActionToggleFocus, label: "results/preview"},
			{action: config.ActionScrollDown, label: "navigate/scroll"},
			{action: config.ActionOpenResult, label: "open"},
			{action: config.ActionCopyResult, label: "copy"},
			{action: config.ActionEditLabel, label: "label"},
			{action: config.ActionTogglePreview, label: "close preview"},
			{action: config.ActionToggleHelp, label: "help"},
		}
	case ContextHistory:
		entries = []entry{
			{action: config.ActionScrollDown, label: "navigate"},
			{action: config.ActionOpenResult, label: "open"},
			{action: config.ActionCopyResult, label: "copy"},
			{action: config.ActionDeleteResult, label: "delete"},
			{action: config.ActionToggleFocus, label: "back"},
			{action: config.ActionToggleHelp, label: "help"},
		}
	case ContextRules:
		entries = []entry{
			{action: config.ActionToggleFocus, label: "form/list"},
			{action: config.ActionScrollDown, label: "navigate"},
			{action: config.ActionOpenResult, label: "add/edit"},
			{action: config.ActionDeleteResult, label: "delete"},
			{action: config.ActionToggleHelp, label: "help"},
		}
	case ContextAdd:
		entries = []entry{
			{action: config.ActionToggleFocus, label: "next/back"},
			{action: config.ActionOpenResult, label: "continue/submit"},
			{action: config.ActionToggleHelp, label: "help"},
		}
	case ContextSearchInput:
		focusAction := config.Action("")
		if k.searchInputKey(config.ActionToggleFocus) != "" {
			focusAction = config.ActionToggleFocus
		} else if k.searchInputKey(config.ActionScrollDown) != "" {
			focusAction = config.ActionScrollDown
		}
		entries = []entry{
			{action: focusAction, key: k.focusResultsKeys(), label: "focus results"},
			{action: config.ActionToggleSort, label: "sort"},
			{action: config.ActionToggleSemantic, label: "semantic"},
			{action: config.ActionToggleHelp, label: "help"},
			{action: config.ActionQuit, label: "quit"},
		}
	case ContextSearchSuggestion:
		entries = []entry{
			{action: config.ActionOpenResult, label: "try suggestion"},
			{action: config.ActionToggleHelp, label: "help"},
			{action: config.ActionQuit, label: "quit"},
		}
	case ContextSearchInputEmpty:
		entries = []entry{
			{action: config.ActionTabHistory, label: "history"},
			{action: config.ActionToggleTheme, label: "appearance"},
			{action: config.ActionToggleHelp, label: "help"},
			{action: config.ActionQuit, label: "quit"},
		}
	default: // ContextSearchResults
		entries = []entry{
			{action: config.ActionToggleFocus, label: "search"},
			{action: config.ActionScrollDown, label: "navigate"},
			{action: config.ActionOpenResult, label: "open"},
			{action: config.ActionCopyResult, label: "copy"},
			{action: config.ActionTogglePreview, label: "preview"},
			{action: config.ActionDeleteResult, label: "delete"},
			{action: config.ActionToggleHelp, label: "help"},
		}
	}

	hints := make([]Hint, 0, len(entries))
	for _, entry := range entries {
		if hint, ok := k.hint(entry.action, entry.key, entry.label); ok {
			hints = append(hints, hint)
		}
	}
	return hints
}

// ShortHelp implements help.KeyMap.
func (k KeyMap) ShortHelp() []key.Binding {
	hints := k.ShortHints()
	bindings := make([]key.Binding, 0, len(hints))
	for _, hint := range hints {
		bindings = append(bindings, hint.Binding)
	}
	return bindings
}

// FullHelp implements help.KeyMap.
func (k KeyMap) FullHelp() [][]key.Binding {
	groups := [][]config.Action{
		{
			config.ActionScrollUp,
			config.ActionScrollDown,
			config.ActionToggleFocus,
			config.ActionOpenResult,
			config.ActionCopyResult,
			config.ActionTogglePreview,
			config.ActionEditLabel,
			config.ActionDeleteResult,
		},
		{
			config.ActionToggleSort,
			config.ActionToggleSemantic,
			config.ActionToggleTheme,
			config.ActionToggleSettings,
			config.ActionToggleHelp,
			config.ActionQuit,
		},
		{
			config.ActionTabSearch,
			config.ActionTabHistory,
			config.ActionTabRules,
			config.ActionTabAdd,
		},
	}
	result := make([][]key.Binding, 0, len(groups))
	for _, actions := range groups {
		bindings := make([]key.Binding, 0, len(actions))
		for _, action := range actions {
			if binding, ok := k.bindings[action]; ok {
				bindings = append(bindings, binding)
			}
		}
		result = append(result, bindings)
	}
	return result
}

// FormatKey turns terminal key names into compact platform-aware labels.
func FormatKey(name string) string {
	switch name {
	case "up":
		return "↑"
	case "down":
		return "↓"
	case "left":
		return "←"
	case "right":
		return "→"
	case "enter":
		return "↵"
	case "esc":
		return "⎋"
	case "tab":
		return "⇥"
	case "shift+tab":
		return "⇤"
	case "":
		return ""
	}
	if runtime.GOOS == "darwin" {
		name = strings.ReplaceAll(name, "ctrl+", "⌃")
		name = strings.ReplaceAll(name, "alt+", "⌥")
		name = strings.ReplaceAll(name, "shift+", "⇧")
	} else {
		name = strings.ReplaceAll(name, "ctrl+", "^")
		name = strings.ReplaceAll(name, "alt+", "M-")
	}
	return name
}
