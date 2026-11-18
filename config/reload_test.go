package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNogonEnvironmentReloadPreservesManagedSettings(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("LOCALAPPDATA", dir)
	t.Setenv("NOGON_DATA_DIR", dir)
	t.Setenv("NOGON_PORT", "6655")
	const managed = "app:\n  title: Saved title\n  subtitle: Saved subtitle\n"
	managedPath := filepath.Join(dir, "config.web.yml")
	if err := os.WriteFile(managedPath, []byte(managed), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HISTER__APP__TITLE", "Legacy environment")
	t.Setenv("NOGON__APP__TITLE", "Nogon environment")
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if c.App.Title != "Nogon environment" || c.App.Subtitle != "Saved subtitle" || c.Server.Address != "127.0.0.1:6655" {
		t.Fatalf("startup lost environment or managed settings: %+v", c.App)
	}
	if source := c.Source("app.title"); source.Name != "NOGON__APP__TITLE" || !source.Locked || !source.WebSelected {
		t.Fatalf("incorrect startup source: %+v", source)
	}
	if !c.SourcesUnchanged() {
		t.Fatal("unchanged environment immediately invalidated loaded settings")
	}
	t.Setenv("NOGON__APP__TITLE", "")
	t.Setenv("NOGON_PORT", "7788")
	if c.SourcesUnchanged() {
		t.Fatal("changed Nogon environment did not invalidate loaded settings")
	}
	pending, err := c.PendingConfiguration()
	if err != nil || pending == nil || pending.App.Title != "" || pending.Server.Address != "127.0.0.1:7788" {
		t.Fatalf("pending configuration ignored Nogon environment: %+v, %v", pending, err)
	}
	next, err := c.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if next.App.Title != "" || next.App.Subtitle != "Saved subtitle" || next.Server.Address != "127.0.0.1:7788" {
		t.Fatal("reload lost empty environment selection or managed settings")
	}
	if next.Source("app.title").Name != "NOGON__APP__TITLE" || next.Source("server.address").Name != "NOGON_PORT" {
		t.Fatal("reload lost environment source names")
	}
	if c.App.Title != "Nogon environment" || c.Server.Address != "127.0.0.1:6655" {
		t.Fatal("reload mutated the active configuration")
	}
	if !bytes.Equal(next.secretKey, c.secretKey) || next.Rules == nil {
		t.Fatal("reload lost runtime state")
	}
	if raw, err := os.ReadFile(managedPath); err != nil || string(raw) != managed {
		t.Fatalf("environment overrides rewrote managed settings: %q, %v", raw, err)
	}
}

func TestNogonReloadRetainsDiscoveredLegacyInstance(t *testing.T) {
	dir := isolateRenamePaths(t)
	dataRoot := filepath.Join(dir, "state")
	switch runtime.GOOS {
	case "darwin":
		dataRoot = filepath.Join(dir, "Library/Application Support")
	case "windows":
		dataRoot = filepath.Join(dir, "config")
	}
	legacy := filepath.Join(dataRoot, "hister")
	filename := filepath.Join(dir, "config", "hister", "config.yml")
	writeRenameFile(t, filename, "# main configuration\n")
	managed := filepath.Join(legacy, "config.web.yml")
	writeRenameFile(t, managed, "app:\n  title: Saved title\n")
	writeRenameFile(t, filepath.Join(filepath.Dir(filename), "tui.yaml"), "dark_theme: saved-theme\n")
	name, err := ResolvePath("", false)
	if err != nil || name != filename {
		t.Fatalf("legacy discovery: %q, %v", name, err)
	}
	c, err := Load(name)
	if err != nil {
		t.Fatal(err)
	}
	if !c.SourcesUnchanged() {
		t.Fatal("runtime initialization changed the selected default data directory")
	}
	writeRenameFile(t, filepath.Join(dir, "config", "nogon", "config.yml"), "app:\n  title: Unrelated new file\n")
	writeRenameFile(t, managed, "app:\n  title: Updated saved title\n")
	next, err := c.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if next.Filename() != filename || next.App.Directory != legacy || next.App.Title != "Updated saved title" || next.Source("app.directory").Kind != "default" {
		t.Fatal("reload rediscovered a main file or lost the legacy data directory")
	}
	if !bytes.Equal(c.secretKey, next.secretKey) || next.TUI.DarkTheme != "saved-theme" {
		t.Fatal("reload lost the original session key or TUI preferences")
	}
	draft, fields := next.PrepareDraft([]SettingEdit{{Path: "app.title", Action: "reset"}})
	if len(fields) != 0 || draft.App.Directory != legacy {
		t.Fatalf("web draft lost the resolved default directory: %v", fields)
	}
	writeRenameFile(t, filepath.Join(dataRoot, "nogon", "db.sqlite3"), "another instance")
	if _, err := next.Reload(); err == nil || !strings.Contains(err.Error(), "app.directory") {
		t.Fatalf("reload accepted ambiguous data directories: %v", err)
	}
	if next.App.Directory != legacy || next.App.Title != "Updated saved title" {
		t.Fatal("failed reload mutated the active configuration")
	}
	t.Setenv("NOGON_DATA_DIR", legacy)
	selected, err := next.Reload()
	if err != nil || selected.App.Directory != legacy || selected.Source("app.directory").Name != "NOGON_DATA_DIR" {
		t.Fatalf("explicit choice did not resolve the conflict: %v", err)
	}
}

func TestManagedPersistenceRoundTrip(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, longPath := range []bool{false, true} {
			t.Run(fmt.Sprintf("existing_%t_long_path_%t", existing, longPath), func(t *testing.T) {
				dir := t.TempDir()
				if longPath {
					for range 4 {
						dir = filepath.Join(dir, strings.Repeat("settings", 10))
					}
					if err := os.MkdirAll(dir, 0o700); err != nil {
						t.Fatal(err)
					}
				}
				t.Setenv("HISTER_DATA_DIR", dir)
				main := filepath.Join(dir, "config.yml")
				if err := os.WriteFile(main, []byte("# reference\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(dir, "config.web.yml")
				if existing {
					if err := os.WriteFile(path, []byte("app:\n  title: Confirmed\n  subtitle: Retained\n"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				cfg, _, err := LoadForInspection(main, true)
				if err != nil {
					t.Fatal(err)
				}
				next, fields := cfg.PrepareDraft([]SettingEdit{{Path: "app.title", Action: "set", Value: json.RawMessage(`"Saved"`)}})
				if len(fields) != 0 {
					t.Fatal(fields)
				}
				staged, err := next.StageManaged()
				if err != nil {
					t.Fatal(err)
				}
				err = staged.Commit()
				closeErr := staged.Close()
				if err != nil || closeErr != nil {
					t.Fatalf("save managed settings: %v, cleanup: %v", err, closeErr)
				}
				restarted, _, err := LoadForInspection(main, true)
				if err != nil || restarted.App.Title != "Saved" || restarted.App.Subtitle != cfg.App.Subtitle {
					t.Fatalf("restart did not retain the saved settings: %v", err)
				}
				if !next.SourcesUnchanged() {
					t.Fatal("successful save immediately conflicts with the managed file")
				}
				staging, err := filepath.Glob(filepath.Join(dir, ".config.web-*"))
				if err != nil || len(staging) != 0 {
					t.Fatalf("successful save retained temporary files: %v %v", staging, err)
				}
			})
		}
	}
}

func TestManagedPersistenceDurabilityFailures(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, recoveryFails := range []bool{false, true} {
			t.Run(fmt.Sprintf("existing_%t_recovery_failure_%t", existing, recoveryFails), func(t *testing.T) {
				dir := t.TempDir()
				t.Setenv("HISTER_DATA_DIR", dir)
				main := filepath.Join(dir, "config.yml")
				if err := os.WriteFile(main, []byte("# only reference values\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(dir, "config.web.yml")
				const previous = "# preserve confirmed bytes on failure\napp:\n  title: Confirmed\n"
				if existing {
					if err := os.WriteFile(path, []byte(previous), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				cfg, _, err := LoadForInspection(main, true)
				if err != nil {
					t.Fatal(err)
				}
				next, fields := cfg.PrepareDraft([]SettingEdit{{Path: "app.title", Action: "set", Value: json.RawMessage(`"Candidate"`)}})
				if len(fields) != 0 {
					t.Fatal(fields)
				}
				staged, err := next.StageManaged()
				if err != nil {
					t.Fatal(err)
				}
				// Inject a disk durability failure at the filesystem boundary.
				syncDirectory := staged.syncDirectory
				calls := 0
				staged.syncDirectory = func() error {
					calls++
					if calls == 1 || recoveryFails {
						return errors.New("disk sync failed")
					}
					return syncDirectory()
				}
				err = staged.Commit()
				if err == nil || errors.Is(err, ErrManagedRecovery) != recoveryFails {
					t.Fatalf("incorrect durability result: %v", err)
				}
				raw, err := os.ReadFile(path)
				if existing && (err != nil || string(raw) != previous) {
					t.Fatalf("previous bytes not restored: %q %v", raw, err)
				}
				if !existing && !os.IsNotExist(err) {
					t.Fatalf("unconfirmed file survived: %v", err)
				}
				if err := staged.Close(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestManagedCommitRejectsExternalReplacement(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HISTER_DATA_DIR", dir)
	main := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(main, []byte("# reference\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := LoadForInspection(main, true)
	if err != nil {
		t.Fatal(err)
	}
	next, fields := cfg.PrepareDraft([]SettingEdit{{Path: "app.title", Action: "set", Value: json.RawMessage(`"Candidate"`)}})
	if len(fields) != 0 {
		t.Fatal(fields)
	}
	staged, err := next.StageManaged()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.web.yml")
	const external = "app:\n  title: External\n"
	if err := os.WriteFile(path, []byte(external), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := staged.Commit(); err == nil {
		t.Error("commit overwrote an unseen external configuration")
	}
	if err := staged.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != external {
		t.Fatalf("external configuration changed: %q %v", raw, err)
	}
}

func TestManagedPersistenceRetainsRecoveryCopyOnRestoreFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Chmod does not enforce directory write permissions on Windows")
	}
	dir := t.TempDir()
	t.Setenv("HISTER_DATA_DIR", dir)
	main := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(main, []byte("# reference\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const previous = "app:\n  title: Confirmed\n"
	if err := os.WriteFile(filepath.Join(dir, "config.web.yml"), []byte(previous), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := LoadForInspection(main, true)
	if err != nil {
		t.Fatal(err)
	}
	next, fields := cfg.PrepareDraft([]SettingEdit{{Path: "app.title", Action: "set", Value: json.RawMessage(`"Candidate"`)}})
	if len(fields) > 0 {
		t.Fatal(fields)
	}
	staged, err := next.StageManaged()
	if err != nil {
		t.Fatal(err)
	}
	staged.syncDirectory = func() error {
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		return errors.New("disk failure after replacement")
	}
	err = staged.Commit()
	if chmodErr := os.Chmod(dir, 0o700); chmodErr != nil {
		t.Fatal(chmodErr)
	}
	if !errors.Is(err, ErrManagedRecovery) {
		t.Fatalf("restore failure not reported: %v", err)
	}
	if err := staged.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cfg.DiscardManagedStaging(); err != nil {
		t.Fatal(err)
	}
	recovery, err := filepath.Glob(filepath.Join(dir, ".config.web-recovery-*"))
	if err != nil || len(recovery) != 1 {
		t.Fatalf("confirmed configuration recovery copy was lost: %v %v", recovery, err)
	}
	raw, err := os.ReadFile(recovery[0])
	if err != nil || string(raw) != previous {
		t.Fatalf("incorrect recovery bytes: %q %v", raw, err)
	}
}

func TestManagedConfigurationSourcesAndReset(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HISTER_DATA_DIR", dir)
	filename := filepath.Join(dir, "config.yml")
	managed := filepath.Join(dir, "config.web.yml")
	const web = "app:\n  title: Web title\n"
	var running *Config
	for _, tc := range []struct{ main, web, title, source string }{
		{"app:\n  title: Nogon\n", web, "Nogon", "file"},
		{"# no authoritative title\n", web, "Web title", "web"},
		{"# no authoritative title\n", "{}\n", "Nogon", "default"},
	} {
		if err := os.WriteFile(filename, []byte(tc.main), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(managed, []byte(tc.web), 0o600); err != nil {
			t.Fatal(err)
		}
		restarted, err := Load(filename)
		if err != nil {
			t.Fatal(err)
		}
		if running == nil {
			running = restarted
		}
		reloaded, err := running.Reload()
		if err != nil {
			t.Fatal(err)
		}
		inspected, _, err := LoadForInspection(filename, true)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range []*Config{restarted, reloaded, inspected} {
			source := c.Source("app.title")
			if c.App.Title != tc.title || source.Kind != tc.source {
				t.Fatalf("title=%q, source=%+v", c.App.Title, source)
			}
			if tc.source == "file" && !strings.Contains(source.Reason, "web selection") {
				t.Fatalf("shadowed web selection is not identified: %+v", source)
			}
		}
		for name, want := range map[string]string{filename: tc.main, managed: tc.web} {
			got, err := os.ReadFile(name)
			if err != nil || string(got) != want {
				t.Fatalf("loading changed %s: %v", name, err)
			}
		}
		running = reloaded
	}
}

func TestManagedConfigurationReloadWithoutMainFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HISTER_DATA_DIR", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("LOCALAPPDATA", dir)
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(dir, "config.web.yml")
	if err := os.WriteFile(managed, []byte("app:\n  title: Saved\ncrawler:\n  headers: {x-test: original}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	next, err := c.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if next.App.Title != "Saved" || next.Filename() != "*Default Config*" || !bytes.Equal(next.SecretKey(), c.SecretKey()) || next.TUI != c.TUI {
		t.Fatal("reload lost selections or runtime state")
	}
	next.Crawler.Headers["x-test"] = "mutated"
	restarted, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if restarted.App.Title != "Saved" || restarted.Crawler.Headers["x-test"] != "original" || c.App.Title != "Nogon" {
		t.Fatal("candidates share state or managed selections were not retained")
	}
	if err := os.WriteFile(managed, []byte("app: [invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := next.Reload(); err == nil {
		t.Fatal("reload accepted an invalid managed file")
	}
	if next.App.Title != "Saved" {
		t.Fatal("failed reload mutated active configuration")
	}
	if err := os.Remove(managed); err != nil {
		t.Fatal(err)
	}
	reset, err := next.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if reset.App.Title != "Nogon" || reset.Source("app.title").Kind != "default" {
		t.Fatal("removing managed file did not restore inherited values")
	}
}

func TestManagedConfigurationReloadSelectsNewDirectory(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "config.yml")
	first, second := filepath.Join(dir, "first"), filepath.Join(dir, "second")
	for directory, title := range map[string]string{first: "First", second: "Second"} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "config.web.yml"), fmt.Appendf(nil, "app:\n  title: %s\n", title), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filename, fmt.Appendf(nil, "app:\n  directory: %q\n", first), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(filename)
	if err != nil {
		t.Fatal(err)
	}
	if c.App.Title != "First" {
		t.Fatal("startup selected wrong managed file")
	}
	if err := os.WriteFile(filename, fmt.Appendf(nil, "app:\n  directory: %q\n", second), 0o600); err != nil {
		t.Fatal(err)
	}
	next, err := c.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if next.App.Title != "Second" || next.App.Directory != second {
		t.Fatal("reload did not select new directory's managed file")
	}
	entries, err := os.ReadDir(second)
	if err != nil || len(entries) != 1 {
		t.Fatal("candidate reload created runtime files in new directory")
	}
	got, err := os.ReadFile(filepath.Join(first, "config.web.yml"))
	if err != nil || string(got) != "app:\n  title: First\n" {
		t.Fatal("reload moved or rewrote previous managed file")
	}
}

func TestReloadCommentedConfiguration(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "config.yml")
	t.Setenv("HISTER_DATA_DIR", filepath.Join(dir, "data"))
	const reference = "# app:\n#   title: Example title\n# server:\n#   address: 127.0.0.1:5544\n"
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(reference)
	c, err := Load(filename)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ content, address string }{
		{reference, DefaultServerAddress},
		{"# app:\n#   title: Example title\nserver:\n  address: 127.0.0.1:5544\n", "127.0.0.1:5544"},
		{reference, DefaultServerAddress},
	} {
		write(tc.content)
		c, err = c.Reload()
		if err != nil {
			t.Fatal(err)
		}
		if c.App.Title != CreateDefaultConfig().App.Title || c.Server.Address != tc.address {
			t.Fatalf("title = %q, address = %q; reload must only apply uncommented values", c.App.Title, c.Server.Address)
		}
		content, err := os.ReadFile(filename)
		if err != nil || string(content) != tc.content {
			t.Fatalf("reload changed main configuration: %q, err=%v", content, err)
		}
	}
}

func TestReloadPreservesRuntimeStateAndAppliesOverrides(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "config.yml")
	write := func(title string) {
		t.Helper()
		content := fmt.Sprintf("app:\n  directory: %q\n  title: %s\n", dir, title)
		if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("Original")
	c, err := Load(filename)
	if err != nil {
		t.Fatal(err)
	}
	write("Reloaded")
	t.Setenv("HISTER__APP__SUBTITLE", "From server environment")
	if err := os.WriteFile(filepath.Join(dir, "tui.yaml"), []byte("dark_theme: changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, secretKeyFilename), []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	next, err := c.Reload(Override{Path: "app.search_url", Value: "https://startup.example/?q={query}", Source: ValueSource{Kind: "flag", Name: "--search-url"}})
	if err != nil {
		t.Fatal(err)
	}
	if next.App.Title != "Reloaded" || next.App.Subtitle != "From server environment" || next.App.SearchURL != "https://startup.example/?q={query}" {
		t.Fatalf("incorrect reload: %+v", next.App)
	}
	if c.App.Title != "Original" || next.TUI != c.TUI || !bytes.Equal(next.SecretKey(), c.SecretKey()) {
		t.Fatal("reload mutated original configuration or replaced startup runtime state")
	}
}

func TestReloadRequiresOriginalFile(t *testing.T) {
	dir := t.TempDir()
	c := CreateDefaultConfig()
	c.fname = filepath.Join(dir, "missing.yml")
	c.App.Directory = dir
	if _, err := c.Reload(); err == nil {
		t.Fatal("reload silently fell back from the original config file")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed reload created files: %v, %v", entries, err)
	}
}

func TestReloadWithoutMainConfiguration(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HISTER_DATA_DIR", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("LOCALAPPDATA", dir)
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	// A file appearing after startup must not silently become authoritative.
	for _, name := range []string{"hister", "nogon"} {
		writeRenameFile(t, filepath.Join(dir, name, "config.yml"), "app:\n  title: Unexpected file\n")
	}
	t.Setenv("HISTER__APP__SUBTITLE", "Updated environment")
	next, err := c.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if next.Filename() != "*Default Config*" || next.App.Title != "Nogon" || next.App.Subtitle != "Updated environment" {
		t.Fatalf("reload did not retain defaults and environment without a selected file: %q, %+v", next.Filename(), next.App)
	}
}

func TestReloadRefreshesSourcesAndDetachesCandidates(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HISTER_DATA_DIR", filepath.Join(dir, "data"))
	filename := filepath.Join(dir, "config.yml")
	const original = "app:\n  title: Original\ncrawler:\n  headers:\n    x-test: original\n"
	if err := os.WriteFile(filename, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(filename)
	if err != nil {
		t.Fatal(err)
	}
	if c.Source("app.title").Kind != "file" {
		t.Fatal("startup did not capture the selected file source")
	}
	const updated = "# app:\n#   title: Original\ncrawler:\n  headers:\n    x-test: updated\n"
	if err := os.WriteFile(filename, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
	next, err := c.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if next.App.Title != "Nogon" || next.Source("app.title").Locked || next.Source("crawler.headers").Kind != "file" {
		t.Fatal("reload retained a removed file selection")
	}
	next.Crawler.Headers["x-test"] = "candidate"
	snapshot, err := c.Reload(Override{Path: "app.title", Value: "Candidate", Source: ValueSource{Kind: "flag", Name: "--title"}})
	if err != nil || snapshot.App.Title != "Candidate" || snapshot.Source("app.title").Kind != "flag" {
		t.Fatalf("candidate override failed: %v", err)
	}
	if c.Crawler.Headers["x-test"] != "original" || c.App.Title != "Original" || c.Source("app.title").Kind != "file" {
		t.Fatal("changing a candidate or snapshot mutated the active configuration")
	}
	t.Setenv("HISTER__APP__TITLE", "Environment")
	next, err = next.Reload()
	if err != nil || next.App.Title != "Environment" || next.Source("app.title").Kind != "environment" {
		t.Fatalf("reload did not capture updated environment sources: %v", err)
	}
	data, err := os.ReadFile(filename)
	if err != nil || string(data) != updated {
		t.Fatalf("reload rewrote the main file: %v", err)
	}
}

func TestReloadKeepsSelectedRelativeFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HISTER_DATA_DIR", filepath.Join(dir, "data"))
	const content = "app:\n  title: Selected file\n"
	if err := os.WriteFile("config.yml", []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load("config.yml")
	if err != nil {
		t.Fatal(err)
	}
	if c.Filename() != filepath.Join(dir, "config.yml") {
		t.Fatalf("main file identity was resolved relative to data storage: %q", c.Filename())
	}
	t.Chdir(t.TempDir())
	next, err := c.Reload()
	if err != nil || next.App.Title != "Selected file" {
		t.Fatalf("reload lost the selected file after changing directory: %v", err)
	}
}
