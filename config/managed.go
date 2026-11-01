// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// configComposition is an immutable snapshot of loaded inputs. Drafts compose
// from it without rereading external files or mutating the active configuration.
type configComposition struct {
	defaults      *Config
	authoritative map[string]any
	sources       map[string]ValueSource
	managed       map[string]any
	mainDigest    [32]byte
	managedDigest [32]byte
}

// ErrManagedRecovery means the confirmed file could not be durably restored.
var ErrManagedRecovery = errors.New("managed configuration recovery failed; inspect config.web.yml before restarting")

// ErrManagedConflict prevents replacing an unseen external selection.
var ErrManagedConflict = errors.New("configuration sources changed; reload and read settings again")

// ManagedWrite owns private staging files until Commit or Close. The runtime
// owner must serialize it with reloads and keep requests paused during Commit.
type ManagedWrite struct {
	candidate     *Config
	staged        string
	previous      string
	directory     *os.File
	digest        [32]byte
	syncDirectory func() error
	keepRecovery  bool
}

// StageManaged prepares a replacement without changing the confirmed file.
func (c *Config) StageManaged() (*ManagedWrite, error) {
	if c.composition == nil {
		return nil, errors.New("configuration was not loaded")
	}
	root := make(map[string]any)
	for path, value := range c.composition.managed {
		parts := strings.Split(path, ".")
		parent := root
		for _, part := range parts[:len(parts)-1] {
			child, ok := parent[part].(map[string]any)
			if !ok {
				child = make(map[string]any)
				parent[part] = child
			}
			parent = child
		}
		parent[parts[len(parts)-1]] = value
	}
	raw, err := yaml.Marshal(root)
	if err != nil {
		return nil, err
	}
	previous, exists, err := readManagedFile(c.FullPath("config.web.yml"))
	if err != nil {
		return nil, err
	}
	if managedFileDigest(previous, exists) != c.composition.managedDigest {
		return nil, ErrManagedConflict
	}
	directory, err := os.Open(c.App.Directory)
	if err != nil {
		return nil, err
	}
	w := &ManagedWrite{candidate: c, directory: directory, digest: sha256.Sum256(raw), syncDirectory: directory.Sync}
	w.staged, err = stageManagedFile(c.App.Directory, ".config.web-*", raw)
	if err == nil && exists {
		w.previous, err = stageManagedFile(c.App.Directory, ".config.web-recovery-*", previous)
	}
	if err != nil {
		_ = w.Close()
		return nil, err
	}
	return w, nil
}

func readManagedFile(path string) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, errors.New("managed configuration must be a regular file")
	}
	raw, err := os.ReadFile(path)
	return raw, true, err
}

func managedFileDigest(raw []byte, exists bool) [32]byte {
	if !exists {
		return [32]byte{}
	}
	return sha256.Sum256(raw)
}

func stageManagedFile(directory, pattern string, raw []byte) (string, error) {
	f, err := os.CreateTemp(directory, pattern)
	if err != nil {
		return "", err
	}
	_, writeErr := f.Write(raw)
	err = errors.Join(writeErr, f.Sync(), f.Close())
	if err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// Commit atomically replaces the file and synchronizes its directory. If that
// synchronization fails, restore the previous bytes before returning an error.
func (w *ManagedWrite) Commit() error {
	path := w.candidate.FullPath("config.web.yml")
	raw, exists, err := readManagedFile(path)
	if err != nil {
		return err
	}
	if managedFileDigest(raw, exists) != w.candidate.composition.managedDigest {
		return ErrManagedConflict
	}
	if err := os.Rename(w.staged, path); err != nil {
		return err
	}
	w.staged = ""
	if err := w.syncDirectory(); err != nil {
		var restoreErr error
		if w.previous != "" {
			restoreErr = os.Rename(w.previous, path)
			if restoreErr == nil {
				w.previous = ""
			}
		} else {
			restoreErr = os.Remove(path)
		}
		if restoreErr = errors.Join(restoreErr, w.syncDirectory()); restoreErr != nil {
			w.keepRecovery = true
			return errors.Join(ErrManagedRecovery, err, restoreErr)
		}
		return err
	}
	composition := *w.candidate.composition
	composition.managedDigest = w.digest
	w.candidate.composition = &composition
	return nil
}

// Close discards staging files, retaining recovery bytes if restoration failed.
func (w *ManagedWrite) Close() error {
	var err error
	paths := []string{w.staged}
	if !w.keepRecovery {
		paths = append(paths, w.previous)
	}
	for _, path := range paths {
		if path != "" {
			if removeErr := os.Remove(path); !errors.Is(removeErr, os.ErrNotExist) {
				err = errors.Join(err, removeErr)
			}
		}
	}
	return errors.Join(err, w.directory.Close())
}

// DiscardManagedStaging removes interrupted, unconfirmed writes on startup.
// Call only after acquiring exclusive runtime storage. Recovery copies are kept
// for manual repair and neither kind of file is an input to configuration loading.
func (c *Config) DiscardManagedStaging() error {
	entries, err := os.ReadDir(c.App.Directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		suffix, ok := strings.CutPrefix(entry.Name(), ".config.web-")
		if !ok || suffix == "" || strings.Trim(suffix, "0123456789") != "" || !entry.Type().IsRegular() {
			continue
		}
		if err := os.Remove(c.FullPath(entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// loadManaged adds sparse web selections only where no authoritative source
// selects the setting. Reading configuration never rewrites either file.
func (inputs *configInputs) loadManaged(directory string) error {
	name := filepath.Join(directory, "config.web.yml")
	info, err := os.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read managed configuration %s: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("managed configuration %s must be a regular file", name)
	}
	raw, err := os.ReadFile(name)
	if err != nil {
		return fmt.Errorf("read managed configuration %s: %w", name, err)
	}
	inputs.managedDigest = sha256.Sum256(raw)
	if err := validateManagedYAML(raw); err != nil {
		return fmt.Errorf("managed configuration %s: %w", name, err)
	}
	web, err := fileInputs(raw, name)
	if err != nil {
		return fmt.Errorf("managed configuration %s: %w", name, err)
	}
	selections := make(map[string]any, len(web.sources))
	for path := range web.sources {
		selections[path] = web.values.Get(path)
	}
	return inputs.applyManaged(selections, name)
}

func (inputs *configInputs) applyManaged(selections map[string]any, name string) error {
	inputs.managed = cloneInputValue(reflect.ValueOf(selections)).Interface().(map[string]any)
	for path, value := range inputs.managed {
		if _, locked := inputs.sources[path]; !locked {
			if err := inputs.apply(Override{Path: path, Value: value, Source: ValueSource{Kind: "web", Name: name}}); err != nil {
				return err
			}
		}
		source := inputs.sources[path]
		source.WebSelected = true
		inputs.sources[path] = source
	}
	return nil
}

func (c *Config) composeDraft(selections map[string]any) (*Config, error) {
	inputs, err := fileInputs(nil, "")
	if err != nil {
		return nil, err
	}
	inputs.defaults = c.composition.defaults
	if err := inputs.apply(Override{Value: c.composition.authoritative}); err != nil {
		return nil, err
	}
	inputs.sources = maps.Clone(c.composition.sources)
	if err := inputs.applyManaged(selections, filepath.Join(c.App.Directory, "config.web.yml")); err != nil {
		return nil, err
	}
	next, err := inputs.decode(true)
	if err != nil {
		return nil, err
	}
	next.fname = c.fname
	next.secretKey, next.TUI, next.tuiPath = c.secretKey, c.TUI, c.tuiPath
	next.Hotkeys.TUI = c.Hotkeys.TUI
	next.composition = &configComposition{defaults: c.composition.defaults, authoritative: c.composition.authoritative, sources: c.composition.sources, managed: inputs.managed, mainDigest: c.composition.mainDigest, managedDigest: c.composition.managedDigest}
	return next, next.normalize()
}

func validateManagedYAML(raw []byte) error {
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	var root yaml.Node
	if err := decoder.Decode(&root); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return errors.New("invalid YAML")
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("expected a single YAML mapping")
	}
	return validateManagedNode(root.Content[0], reflect.TypeFor[Config](), "")
}

// Validate the serialized shape before merging, including shadowed selections.
// Untyped extension options stay file managed until their owners supply metadata.
func validateManagedNode(node *yaml.Node, typ reflect.Type, path string) error {
	invalid := func() error { return fmt.Errorf("unsupported setting or value at %s", path) }
	switch path {
	case "app.directory", "app.log_file", "server.database", "server.proxy_auth_header", "indexer.pdf_parser.command", "indexer.pdf_parser.args":
		return fmt.Errorf("%s is file managed", path)
	}
	if strings.HasPrefix(path, "server.oauth.") {
		switch path[strings.LastIndexByte(path, '.')+1:] {
		case "identity_claim", "admin_groups", "groups_claim":
			return fmt.Errorf("%s is file managed", path)
		}
	}
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if node.Kind == yaml.AliasNode || node.Tag == "!!null" {
		return invalid()
	}
	switch typ.Kind() {
	case reflect.Struct, reflect.Map:
		if node.Kind != yaml.MappingNode {
			return invalid()
		}
		seen := make(map[string]bool)
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Tag != "!!str" || seen[key.Value] {
				return invalid()
			}
			seen[key.Value] = true
			childPath := key.Value
			if path != "" {
				childPath = path + "." + key.Value
			}
			var childType reflect.Type
			if typ.Kind() == reflect.Map {
				childType = typ.Elem()
				// Named structural entries cannot contain path delimiters.
				if sourceSettingPath(path) == "" && (strings.Contains(key.Value, ".") || key.Value == "" || key.Value != strings.ToLower(key.Value)) {
					return invalid()
				}
			} else {
				for field := range typ.Fields() {
					if tag := field.Tag.Get("yaml"); tag != "" && tag != "-" && tag == key.Value {
						childType = field.Type
						break
					}
				}
			}
			if childType == nil {
				return fmt.Errorf("unknown setting at %s", childPath)
			}
			if err := validateManagedNode(node.Content[i+1], childType, childPath); err != nil {
				return err
			}
		}
	case reflect.Slice:
		if node.Kind != yaml.SequenceNode {
			return invalid()
		}
		for _, item := range node.Content {
			if err := validateManagedNode(item, typ.Elem(), path); err != nil {
				return err
			}
		}
	case reflect.String:
		if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
			return invalid()
		}
	case reflect.Bool:
		if node.Kind != yaml.ScalarNode || node.Tag != "!!bool" {
			return invalid()
		}
	case reflect.Int, reflect.Int64:
		if node.Kind != yaml.ScalarNode || node.Tag != "!!int" {
			return invalid()
		}
	case reflect.Float64:
		if node.Kind != yaml.ScalarNode || (node.Tag != "!!float" && node.Tag != "!!int") {
			return invalid()
		}
	default:
		return invalid()
	}
	if node.Kind == yaml.ScalarNode {
		// Check representation limits before a weak decoder can coerce or wrap
		// numeric values, even when an authoritative selection shadows them.
		if err := node.Decode(reflect.New(typ).Interface()); err != nil {
			return invalid()
		}
	}
	return nil
}

// Main file maps historically inherit default members. Only collections
// selected by the web layer replace their defaults as a whole.
func (inputs *configInputs) clearWebDefaults(value reflect.Value, path string) {
	if value.Kind() == reflect.Map || value.Kind() == reflect.Slice {
		if inputs.sources[path].Kind == "web" {
			value.SetZero()
		}
		return
	}
	if value.Kind() != reflect.Struct {
		return
	}
	for field := range value.Type().Fields() {
		key := field.Tag.Get("mapstructure")
		if key == "" || key == "-" {
			continue
		}
		if path != "" {
			key = path + "." + key
		}
		inputs.clearWebDefaults(value.FieldByIndex(field.Index), key)
	}
}
