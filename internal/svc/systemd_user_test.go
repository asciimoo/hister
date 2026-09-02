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

func TestSystemdUserInstallWritesWants(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	started := false
	reloadCount := 0
	m, err := newSystemdUserManager(adaptRunner(func(name string, args []string) (string, string, error) {
		if name != "systemctl" {
			return "", "", fmt.Errorf("unexpected %s", name)
		}
		switch args[1] {
		case "daemon-reload":
			reloadCount++
			return "", "", nil
		case "start":
			started = true
			return "", "", nil
		case "stop":
			return "", "", nil
		case "show":
			if started {
				return "LoadState=loaded\nActiveState=active\nMainPID=7\n", "", nil
			}
			return "LoadState=not-found\nActiveState=inactive\nMainPID=0\n", "", nil
		default:
			return "", "", fmt.Errorf("unexpected %v", args)
		}
	}), home, filepath.Join(home, "runtime"), true)
	if err != nil {
		t.Fatal(err)
	}

	def := Definition{Binary: filepath.Join(home, "hister"), DataDir: filepath.Join(home, "data")}
	if err := os.WriteFile(def.Binary, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := m.Install(def, InstallOptions{}); err != nil {
		t.Fatal(err)
	}
	if reloadCount != 1 || !started {
		t.Fatalf("reloadCount=%d started=%v", reloadCount, started)
	}
	body, err := os.ReadFile(m.DefinitionPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), MarkerSystemd) {
		t.Fatal("missing marker")
	}
	if strings.Contains(string(body), "After=network.target") {
		t.Fatal("unexpected After=")
	}
	wants := filepath.Join(filepath.Dir(m.DefinitionPath()), "default.target.wants", "hister.service")
	target, err := os.Readlink(wants)
	if err != nil {
		t.Fatal(err)
	}
	if target != m.DefinitionPath() {
		t.Fatalf("wants -> %q", target)
	}

	if err := m.Uninstall(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.DefinitionPath()); !os.IsNotExist(err) {
		t.Fatal("unit still present")
	}
	if _, err := os.Lstat(wants); !os.IsNotExist(err) {
		t.Fatal("wants symlink still present")
	}
	if reloadCount != 2 {
		t.Fatalf("uninstall should daemon-reload, reloadCount=%d", reloadCount)
	}
}

func TestSystemdUserNoStartNoBus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	started := false
	m, err := newSystemdUserManager(adaptRunner(func(name string, args []string) (string, string, error) {
		if args[1] == "start" {
			started = true
		}
		return "LoadState=not-found\nActiveState=inactive\nMainPID=0\n", "", nil
	}), home, "", false)
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
	if started {
		t.Fatal("start must not run with --no-start")
	}
}

func TestSystemdUserStartWithoutBus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	m, err := newSystemdUserManager(adaptRunner(func(string, []string) (string, string, error) {
		return "", "bus down", errors.New("exit 1")
	}), home, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(m.DefinitionPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.DefinitionPath(), []byte(MarkerSystemd+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = m.Start()
	if err == nil || !strings.Contains(err.Error(), "loginctl enable-linger") {
		t.Fatalf("start without bus: %v", err)
	}
}

func TestSystemdHandWrittenUnitIsForeign(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	m, err := newSystemdUserManager(adaptRunner(func(string, []string) (string, string, error) {
		return "LoadState=loaded\nActiveState=inactive\nMainPID=0\n", "", nil
	}), home, filepath.Join(home, "run"), true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(m.DefinitionPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.DefinitionPath(), []byte("[Service]\nExecStart=/bin/true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	def := Definition{Binary: filepath.Join(home, "hister"), DataDir: filepath.Join(home, "data")}
	if err := os.WriteFile(def.Binary, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := m.Install(def, InstallOptions{Force: true}); !errors.Is(err, ErrForeign) {
		t.Fatalf("force on hand-written unit: %v", err)
	}
	wants := filepath.Join(filepath.Dir(m.DefinitionPath()), "default.target.wants")
	if _, err := os.Stat(wants); !os.IsNotExist(err) {
		t.Fatal("refusing a foreign unit must not create default.target.wants")
	}
}

func TestSystemdOrphanLoadedIsForeign(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	stopped := false
	m, err := newSystemdUserManager(adaptRunner(func(name string, args []string) (string, string, error) {
		if len(args) > 1 && args[1] == "stop" {
			stopped = true
		}
		return "LoadState=loaded\nActiveState=active\nMainPID=9\n", "", nil
	}), home, filepath.Join(home, "run"), true)
	if err != nil {
		t.Fatal(err)
	}
	st, err := m.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !st.ExternallyManaged || st.State != StateRunning {
		t.Fatalf("status %+v", st)
	}
	def := Definition{Binary: filepath.Join(home, "hister"), DataDir: filepath.Join(home, "data")}
	if err := os.WriteFile(def.Binary, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := m.Install(def, InstallOptions{Force: true}); !errors.Is(err, ErrForeign) {
		t.Fatalf("install orphan: %v", err)
	}
	if err := m.Uninstall(); !errors.Is(err, ErrForeign) {
		t.Fatalf("uninstall orphan: %v", err)
	}
	if stopped {
		t.Fatal("must not stop an orphan loaded unit")
	}
}

func TestParseSystemdShowLoadedStopped(t *testing.T) {
	q, err := parseSystemdShow("LoadState=loaded\nActiveState=inactive\nMainPID=0\n")
	if err != nil {
		t.Fatal(err)
	}
	if !q.loaded || q.running {
		t.Fatalf("query %+v", q)
	}
}

func TestParseSystemdShowFailed(t *testing.T) {
	q, err := parseSystemdShow("LoadState=loaded\nActiveState=failed\nMainPID=0\n")
	if err != nil {
		t.Fatal(err)
	}
	if !q.loaded || q.running || !q.failed {
		t.Fatalf("query %+v", q)
	}
	st := Status{State: StateStopped, Failed: true}
	if st.String() != "installed, failed" {
		t.Fatalf("status string %q", st.String())
	}
	if st.ExitCode() != ExitStopped {
		t.Fatalf("failed exit %d", st.ExitCode())
	}
}

func TestSystemdRelativeXDGConfigHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "relative-config")
	_, err := newSystemdUserManager(adaptRunner(func(string, []string) (string, string, error) {
		return "", "", nil
	}), home, filepath.Join(home, "run"), true)
	if err == nil || !strings.Contains(err.Error(), "XDG_CONFIG_HOME") {
		t.Fatalf("relative XDG_CONFIG_HOME: %v", err)
	}
}

func TestParseSystemdShowNotFound(t *testing.T) {
	q, err := parseSystemdShow("LoadState=not-found\nActiveState=inactive\nMainPID=0\n")
	if err != nil {
		t.Fatal(err)
	}
	if q.loaded || q.running || q.pid != 0 {
		t.Fatalf("query %+v", q)
	}
}

func TestSystemdStatusMissingSystemctlIsFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	m, err := newSystemdUserManager(adaptRunner(func(string, []string) (string, string, error) {
		return "", "", exec.ErrNotFound
	}), home, filepath.Join(home, "run"), true)
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

func TestSystemdForeignWantsRefusesInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	m, err := newSystemdUserManager(adaptRunner(func(string, []string) (string, string, error) {
		return "LoadState=not-found\nActiveState=inactive\nMainPID=0\n", "", nil
	}), home, filepath.Join(home, "run"), true)
	if err != nil {
		t.Fatal(err)
	}
	wants := filepath.Join(filepath.Dir(m.DefinitionPath()), "default.target.wants", "hister.service")
	if err := os.MkdirAll(filepath.Dir(wants), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wants, []byte("not a symlink"), 0o644); err != nil {
		t.Fatal(err)
	}
	def := Definition{Binary: filepath.Join(home, "hister"), DataDir: filepath.Join(home, "data")}
	if err := os.WriteFile(def.Binary, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := m.Install(def, InstallOptions{Force: true}); !errors.Is(err, ErrForeign) {
		t.Fatalf("install with foreign wants: %v", err)
	}
	if _, err := os.Stat(m.DefinitionPath()); !os.IsNotExist(err) {
		t.Fatal("unit must not be written when wants is foreign")
	}
}

func TestSystemdForeignWantsRefusesUninstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	stopped := false
	m, err := newSystemdUserManager(adaptRunner(func(name string, args []string) (string, string, error) {
		if len(args) > 1 && args[1] == "stop" {
			stopped = true
		}
		return "LoadState=loaded\nActiveState=inactive\nMainPID=0\n", "", nil
	}), home, filepath.Join(home, "run"), true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(m.DefinitionPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.DefinitionPath(), []byte(MarkerSystemd+"\n[Service]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wants := filepath.Join(filepath.Dir(m.DefinitionPath()), "default.target.wants", "hister.service")
	if err := os.MkdirAll(filepath.Dir(wants), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wants, []byte("not a symlink"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.Uninstall(); !errors.Is(err, ErrForeign) {
		t.Fatalf("uninstall with foreign wants: %v", err)
	}
	if stopped {
		t.Fatal("must not stop when wants is foreign")
	}
	if _, err := os.Stat(m.DefinitionPath()); err != nil {
		t.Fatal("ours unit must remain")
	}
}
