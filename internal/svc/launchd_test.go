// SPDX-FileContributor: sathwick-p
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package svc

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLaunchdInstallStartStopUninstall(t *testing.T) {
	home := t.TempDir()
	loaded := false
	runner := func(name string, args []string) (string, string, error) {
		if name != "launchctl" {
			return "", "", fmt.Errorf("unexpected %s", name)
		}
		switch args[0] {
		case "bootstrap":
			loaded = true
			return "", "", nil
		case "bootout":
			if !loaded {
				return "", "Could not find service", errors.New("exit 1")
			}
			loaded = false
			return "", "", nil
		case "print":
			if !loaded {
				return "", "Could not find service", errors.New("exit 1")
			}
			return "state = running\npid = 99\n", "", nil
		case "kickstart":
			return "", "", nil
		default:
			return "", "", fmt.Errorf("unexpected %v", args)
		}
	}
	m, err := newLaunchdManager(adaptRunner(runner), home)
	if err != nil {
		t.Fatal(err)
	}

	st, err := m.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateNotInstalled || st.ExitCode() != ExitNotInstalled {
		t.Fatalf("status = %+v", st)
	}

	bin := filepath.Join(home, "bin", "hister")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	def := Definition{Binary: bin, DataDir: filepath.Join(home, "data")}
	if err := m.Install(def, InstallOptions{}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(m.DefinitionPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), MarkerPlist) {
		t.Fatal("missing marker")
	}
	st, err = m.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateRunning || st.PID != 99 {
		t.Fatalf("after install: %+v", st)
	}

	if err := m.Install(def, InstallOptions{}); err == nil {
		t.Fatal("second install should fail without --force")
	}

	if err := m.Stop(); err != nil {
		t.Fatal(err)
	}
	st, err = m.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateStopped || st.ExitCode() != ExitStopped {
		t.Fatalf("after stop: %+v", st)
	}

	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	if err := m.Restart(); err != nil {
		t.Fatal(err)
	}
	if err := m.Uninstall(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.DefinitionPath()); !os.IsNotExist(err) {
		t.Fatal("definition still on disk")
	}
	if err := m.Uninstall(); err != nil {
		t.Fatal(err)
	}
}

func TestLaunchdForeignRefusesMutations(t *testing.T) {
	home := t.TempDir()
	m, err := newLaunchdManager(adaptRunner(func(string, []string) (string, string, error) {
		return "state = running\npid = 1\n", "", nil
	}), home)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(m.DefinitionPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	nix := filepath.Join(home, "nix-unit")
	if err := os.WriteFile(nix, []byte("foreign"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(nix, m.DefinitionPath()); err != nil {
		t.Fatal(err)
	}

	def := Definition{Binary: filepath.Join(home, "hister"), DataDir: filepath.Join(home, "data")}
	if err := os.WriteFile(def.Binary, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := m.Install(def, InstallOptions{Force: true}); !errors.Is(err, ErrForeign) {
		t.Fatalf("force install foreign: %v", err)
	}
	if err := m.Start(); !errors.Is(err, ErrForeign) {
		t.Fatalf("start foreign: %v", err)
	}
	if err := m.Stop(); !errors.Is(err, ErrForeign) {
		t.Fatalf("stop foreign: %v", err)
	}
	if err := m.Uninstall(); !errors.Is(err, ErrForeign) {
		t.Fatalf("uninstall foreign: %v", err)
	}
	st, err := m.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !st.ExternallyManaged || st.State != StateRunning {
		t.Fatalf("status %+v", st)
	}
}

func TestLaunchdNoStart(t *testing.T) {
	home := t.TempDir()
	bootstrapped := false
	m, err := newLaunchdManager(adaptRunner(func(name string, args []string) (string, string, error) {
		if args[0] == "bootstrap" {
			bootstrapped = true
		}
		if args[0] == "print" {
			return "", "Could not find service", errors.New("exit 1")
		}
		return "", "", nil
	}), home)
	if err != nil {
		t.Fatal(err)
	}
	def := Definition{Binary: filepath.Join(home, "hister"), DataDir: filepath.Join(home, "data")}
	if err := os.WriteFile(def.Binary, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := m.Install(def, InstallOptions{NoStart: true}); err != nil {
		t.Fatal(err)
	}
	if bootstrapped {
		t.Fatal("bootstrap should not run with --no-start")
	}
	st, err := m.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateStopped {
		t.Fatalf("status %+v", st)
	}
}

func TestLaunchdForceStopFailureLeavesFile(t *testing.T) {
	home := t.TempDir()
	m, err := newLaunchdManager(adaptRunner(func(name string, args []string) (string, string, error) {
		switch args[0] {
		case "print":
			return "state = running\npid = 8\n", "", nil
		case "bootout":
			return "", "busy", errors.New("exit 1")
		default:
			return "", "", nil
		}
	}), home)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(m.DefinitionPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n" + MarkerPlist + "\nold\n")
	if err := os.WriteFile(m.DefinitionPath(), original, 0o644); err != nil {
		t.Fatal(err)
	}
	def := Definition{Binary: filepath.Join(home, "hister"), DataDir: filepath.Join(home, "data")}
	if err := os.WriteFile(def.Binary, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := m.Install(def, InstallOptions{Force: true}); err == nil {
		t.Fatal("expected stop failure")
	}
	got, err := os.ReadFile(m.DefinitionPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("file overwritten: %q", got)
	}
}

func TestLaunchdStartKickstartWhenAlreadyLoaded(t *testing.T) {
	home := t.TempDir()
	kickstarted := false
	bootstrapped := false
	m, err := newLaunchdManager(adaptRunner(func(name string, args []string) (string, string, error) {
		switch args[0] {
		case "bootstrap":
			bootstrapped = true
			return "", "service already loaded", errors.New("exit 5")
		case "kickstart":
			kickstarted = true
			return "", "", nil
		case "print":
			return "state = running\npid = 3\n", "", nil
		default:
			return "", "", fmt.Errorf("unexpected %v", args)
		}
	}), home)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(m.DefinitionPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.DefinitionPath(), []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n"+MarkerPlist+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	if !kickstarted {
		t.Fatal("expected kickstart when print reports loaded")
	}
	if bootstrapped {
		t.Fatal("bootstrap must not run when the job is already loaded")
	}
}

func TestLaunchdOrphanLoadedIsForeign(t *testing.T) {
	home := t.TempDir()
	bootout := false
	m, err := newLaunchdManager(adaptRunner(func(name string, args []string) (string, string, error) {
		switch args[0] {
		case "print":
			return "state = waiting\npid = 0\n", "", nil
		case "bootout":
			bootout = true
			return "", "", nil
		case "bootstrap":
			return "", "", fmt.Errorf("must not bootstrap")
		default:
			return "", "", fmt.Errorf("unexpected %v", args)
		}
	}), home)
	if err != nil {
		t.Fatal(err)
	}
	st, err := m.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !st.ExternallyManaged || st.State != StateStopped {
		t.Fatalf("status %+v", st)
	}
	def := Definition{Binary: filepath.Join(home, "hister"), DataDir: filepath.Join(home, "data")}
	if err := os.WriteFile(def.Binary, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := m.Install(def, InstallOptions{Force: true}); !errors.Is(err, ErrForeign) {
		t.Fatalf("install orphan: %v", err)
	}
	if err := m.Start(); !errors.Is(err, ErrForeign) {
		t.Fatalf("start orphan: %v", err)
	}
	if err := m.Uninstall(); !errors.Is(err, ErrForeign) {
		t.Fatalf("uninstall orphan: %v", err)
	}
	if bootout {
		t.Fatal("must not bootout an orphan loaded job")
	}
}

func TestLaunchdUninstallQueryFailure(t *testing.T) {
	home := t.TempDir()
	m, err := newLaunchdManager(adaptRunner(func(string, []string) (string, string, error) {
		return "", "launchd busy", errors.New("exit 1")
	}), home)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Uninstall(); err == nil {
		t.Fatal("expected query failure")
	}
}

func TestLaunchdStatusMissingLaunchctlIsFailure(t *testing.T) {
	home := t.TempDir()
	m, err := newLaunchdManager(adaptRunner(func(string, []string) (string, string, error) {
		return "", "", exec.ErrNotFound
	}), home)
	if err != nil {
		t.Fatal(err)
	}
	st, err := m.Status()
	if err == nil {
		t.Fatalf("status succeeded: %+v", st)
	}
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("status err=%v, want ErrNotFound", err)
	}
}

func adaptRunner(fn func(name string, args []string) (string, string, error)) CommandRunner {
	return func(name string, args ...string) (string, string, error) {
		return fn(name, args)
	}
}
