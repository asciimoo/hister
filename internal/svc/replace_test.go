// SPDX-FileContributor: sathwick-p
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package svc

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReplaceStopFailureDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hister.service")
	original := []byte("old\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	err := replaceDefinition(path, []byte("new\n"), nil, replaceHooks{
		stopFirst: true,
		stop:      func() error { return errors.New("stop failed") },
	})
	if err == nil {
		t.Fatal("expected stop error")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(original) {
		t.Fatalf("file changed to %q", got)
	}
}

func TestReplaceReloadFailureRestores(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hister.service")
	wants := filepath.Join(dir, "default.target.wants", "hister.service")
	original := []byte("old\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(wants), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, wants); err != nil {
		t.Fatal(err)
	}
	err := replaceDefinition(path, []byte("new\n"), []extraArtifact{{path: wants, symlink: path}}, replaceHooks{
		reload: func() error { return errors.New("reload failed") },
	})
	if err == nil {
		t.Fatal("expected reload error")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(original) {
		t.Fatalf("definition not restored: %q", got)
	}
	target, err := os.Readlink(wants)
	if err != nil {
		t.Fatal(err)
	}
	if target != path {
		t.Fatalf("wants target = %q", target)
	}
}

func TestReplaceStartFailureKeepsNewDefinition(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hister.service")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := replaceDefinition(path, []byte("new\n"), nil, replaceHooks{
		startWanted: true,
		start:       func() error { return errors.New("port busy") },
	})
	if err == nil {
		t.Fatal("expected start error")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "new\n" {
		t.Fatalf("definition = %q, want new file kept", got)
	}
	if !strings.Contains(err.Error(), "failed to start") {
		t.Fatalf("error should report start failure: %v", err)
	}
}

func TestReplaceRollbackReloadsBeforeStart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hister.service")
	wants := filepath.Join(dir, "default.target.wants", "hister.service")
	original := []byte("old\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(wants), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, wants); err != nil {
		t.Fatal(err)
	}

	var order []string
	reloadCount := 0
	err := replaceDefinition(path, []byte("new\n"), []extraArtifact{{path: wants, symlink: path}}, replaceHooks{
		stopFirst:  true,
		restoreRun: true,
		stop: func() error {
			order = append(order, "stop")
			return nil
		},
		reload: func() error {
			reloadCount++
			order = append(order, "reload")
			if reloadCount == 1 {
				return errors.New("reload failed")
			}
			return nil
		},
		start: func() error {
			order = append(order, "start")
			return nil
		},
	})
	if err == nil {
		t.Fatal("expected reload error")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(original) {
		t.Fatalf("definition not restored: %q", got)
	}
	wantOrder := []string{"stop", "reload", "reload", "start"}
	if strings.Join(order, ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("order = %v, want %v", order, wantOrder)
	}
}

func TestReplaceRollbackDoesNotStartWhenReloadFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hister.service")
	original := []byte("old\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	started := false
	err := replaceDefinition(path, []byte("new\n"), nil, replaceHooks{
		restoreRun: true,
		reload:     func() error { return errors.New("reload failed") },
		start: func() error {
			started = true
			return nil
		},
	})
	if err == nil {
		t.Fatal("expected reload error")
	}
	if started {
		t.Fatal("must not restore-start after rollback reload failure")
	}
	if !strings.Contains(err.Error(), "reload failed") {
		t.Fatalf("joined error missing reload failure: %v", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(original) {
		t.Fatalf("definition not restored: %q", got)
	}
}

func TestReplaceRollbackDoesNotStartWhenRestoreFails(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("relies on unix directory permissions")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "hister.service")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	started := false
	err := replaceDefinition(path, []byte("new\n"), nil, replaceHooks{
		restoreRun: true,
		reload: func() error {
			if err := os.Chmod(dir, 0o555); err != nil {
				t.Fatal(err)
			}
			return errors.New("reload failed")
		},
		start: func() error {
			started = true
			return nil
		},
	})
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if err == nil {
		t.Fatal("expected rollback error")
	}
	if started {
		t.Fatal("must not restore-start after definition restore failure")
	}
	if !strings.Contains(err.Error(), "restore service definition") {
		t.Fatalf("joined error missing restore failure: %v", err)
	}
}

func TestReplaceRollbackDoesNotStartWhenEnableLinkRestoreFails(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("relies on unix directory permissions")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "hister.service")
	wantsDir := filepath.Join(dir, "wants")
	wants := filepath.Join(wantsDir, "hister.service")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(wantsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, wants); err != nil {
		t.Fatal(err)
	}
	started := false
	err := replaceDefinition(path, []byte("new\n"), []extraArtifact{{path: wants, symlink: path}}, replaceHooks{
		restoreRun: true,
		reload: func() error {
			if err := os.Chmod(wantsDir, 0o555); err != nil {
				t.Fatal(err)
			}
			return errors.New("reload failed")
		},
		start: func() error {
			started = true
			return nil
		},
	})
	t.Cleanup(func() { _ = os.Chmod(wantsDir, 0o755) })
	if err == nil {
		t.Fatal("expected rollback error")
	}
	if started {
		t.Fatal("must not restore-start after enable-link restore failure")
	}
	if !strings.Contains(err.Error(), "restore "+wants) {
		t.Fatalf("joined error missing enable-link restore failure: %v", err)
	}
}
