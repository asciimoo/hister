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

func withResolveHooks(t *testing.T, arg string, exe func() (string, error)) {
	t.Helper()
	oldArg0, oldExecutable := arg0, executable
	t.Cleanup(func() {
		arg0 = oldArg0
		executable = oldExecutable
	})
	arg0 = func() string { return arg }
	executable = exe
}

func TestUnstableBinaryReason(t *testing.T) {
	tests := []struct {
		path    string
		wantErr bool
	}{
		{path: "/opt/homebrew/bin/hister", wantErr: false},
		{path: "/usr/local/bin/hister", wantErr: false},
		{path: "/opt/homebrew/Cellar/hister/0.18.0/bin/hister", wantErr: true},
		{path: "/nix/store/abc-hister-0.18.0/bin/hister", wantErr: true},
	}
	for _, tt := range tests {
		got := unstableBinaryReason(tt.path)
		if tt.wantErr && got == "" {
			t.Fatalf("%s: expected unstable", tt.path)
		}
		if !tt.wantErr && got != "" {
			t.Fatalf("%s: unexpected %q", tt.path, got)
		}
	}
}

func TestResolveBinaryBareNameUsesLookPath(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "hister")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	origArg0, origLook, origExec := arg0, lookPath, executable
	t.Cleanup(func() {
		arg0, lookPath, executable = origArg0, origLook, origExec
	})
	arg0 = func() string { return "hister" }
	lookPath = func(string) (string, error) { return bin, nil }
	executable = func() (string, error) { return bin, nil }

	got, err := ResolveBinary()
	if err != nil {
		t.Fatal(err)
	}
	if got != bin {
		t.Fatalf("ResolveBinary() = %q, want PATH alias %q", got, bin)
	}
}

func TestResolveBinaryRefusesCellar(t *testing.T) {
	origArg0 := arg0
	t.Cleanup(func() { arg0 = origArg0 })
	arg0 = func() string { return "/opt/homebrew/Cellar/hister/0.18.0/bin/hister" }
	_, err := ResolveBinary()
	if err == nil {
		t.Fatal("expected Cellar path to be refused")
	}
}

func TestResolveBinaryRefusesNixStore(t *testing.T) {
	origArg0 := arg0
	t.Cleanup(func() { arg0 = origArg0 })
	arg0 = func() string { return "/nix/store/hash-hister-0.18.0/bin/hister" }
	_, err := ResolveBinary()
	if err == nil {
		t.Fatal("expected nix store path to be refused")
	}
}

func TestResolveBinaryPrefersPATHAliasOverCellar(t *testing.T) {
	dir := t.TempDir()
	cellarDir := filepath.Join(dir, "Cellar", "hister", "1.0", "bin")
	if err := os.MkdirAll(cellarDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cellar := filepath.Join(cellarDir, "hister")
	if err := os.WriteFile(cellar, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	pathDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(pathDir, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(pathDir, "hister")
	if err := os.Symlink(cellar, alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", pathDir)

	origArg0, origExec := arg0, executable
	t.Cleanup(func() {
		arg0 = origArg0
		executable = origExec
	})
	arg0 = func() string { return cellar }
	executable = func() (string, error) { return cellar, nil }

	got, err := ResolveBinary()
	if err != nil {
		t.Fatal(err)
	}
	if got != alias {
		t.Fatalf("ResolveBinary() = %q, want PATH alias %q", got, alias)
	}
}

func TestResolveBinaryKeepsAbsoluteSymlinkPath(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "Cellar", "hister", "1.0", "bin")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	realBin := filepath.Join(target, "hister")
	if err := os.WriteFile(realBin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "bin", "hister")
	if err := os.MkdirAll(filepath.Dir(alias), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realBin, alias); err != nil {
		t.Fatal(err)
	}

	origArg0, origExec := arg0, executable
	t.Cleanup(func() {
		arg0 = origArg0
		executable = origExec
	})
	arg0 = func() string { return alias }
	executable = func() (string, error) { return realBin, nil }

	got, err := ResolveBinary()
	if err != nil {
		t.Fatal(err)
	}
	if got != alias {
		t.Fatalf("stored %q, want unresolved symlink %q", got, alias)
	}
}

func TestResolveBinaryRejectsExecutableLookupFailure(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "hister")
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	withResolveHooks(t, bin, func() (string, error) {
		return "", errors.New("executable unavailable")
	})

	_, err := ResolveBinary()
	if err == nil || !strings.Contains(err.Error(), "locate running hister executable") {
		t.Fatalf("ResolveBinary() error = %v", err)
	}
}

func TestResolveBinaryRejectsUninspectableExecutable(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "hister")
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	running := filepath.Join(dir, "missing-hister")
	withResolveHooks(t, bin, func() (string, error) {
		return running, nil
	})

	_, err := ResolveBinary()
	if err == nil || !strings.Contains(err.Error(), "inspect running hister executable") {
		t.Fatalf("ResolveBinary() error = %v", err)
	}
}

func TestResolveBinaryRejectsDifferentExecutable(t *testing.T) {
	dir := t.TempDir()
	selected := filepath.Join(dir, "selected")
	running := filepath.Join(dir, "running")
	for _, path := range []string{selected, running} {
		if err := os.WriteFile(path, []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	withResolveHooks(t, selected, func() (string, error) { return running, nil })

	_, err := ResolveBinary()
	if err == nil || !strings.Contains(err.Error(), "not this hister executable") {
		t.Fatalf("ResolveBinary() error = %v", err)
	}
}

func TestResolveBinaryRejectsMissingPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	withResolveHooks(t, missing, func() (string, error) { return missing, nil })

	_, err := ResolveBinary()
	if err == nil || !strings.Contains(err.Error(), "binary") {
		t.Fatalf("ResolveBinary() error = %v", err)
	}
}

func TestResolveBinaryRejectsDirectory(t *testing.T) {
	dir := t.TempDir()
	withResolveHooks(t, dir, func() (string, error) { return dir, nil })

	_, err := ResolveBinary()
	if err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("ResolveBinary() error = %v", err)
	}
}

func TestResolveBinaryRejectsNonExecutableFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix executable bits do not apply on Windows")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "hister")
	if err := os.WriteFile(bin, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	withResolveHooks(t, bin, func() (string, error) { return bin, nil })

	_, err := ResolveBinary()
	if err == nil || !strings.Contains(err.Error(), "is not executable") {
		t.Fatalf("ResolveBinary() error = %v", err)
	}
}

func TestResolveBinaryRejectsBrokenSymlink(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "hister")
	if err := os.Symlink(filepath.Join(dir, "missing"), link); err != nil {
		t.Fatal(err)
	}
	withResolveHooks(t, link, func() (string, error) { return link, nil })

	_, err := ResolveBinary()
	if err == nil || !strings.Contains(err.Error(), "binary") {
		t.Fatalf("ResolveBinary() error = %v", err)
	}
}
