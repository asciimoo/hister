// SPDX-FileContributor: sathwick-p
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asciimoo/hister/internal/svc"
)

type fakeManager struct {
	platform                string
	path                    string
	installed               bool
	running                 bool
	foreign                 bool
	lastDef                 svc.Definition
	lastOpts                svc.InstallOptions
	installErr              error
	startErr                error
	stopErr                 error
	statusErr               error
	stayStoppedAfterInstall bool
}

func (f *fakeManager) Platform() string       { return f.platform }
func (f *fakeManager) DefinitionPath() string { return f.path }
func (f *fakeManager) Logs() []string         { return []string{"/tmp/hister.log"} }
func (f *fakeManager) LoginNote() string      { return "login note" }

func (f *fakeManager) Install(def svc.Definition, opts svc.InstallOptions) error {
	if f.installErr != nil {
		return f.installErr
	}
	f.lastDef = def
	f.lastOpts = opts
	f.installed = true
	if !opts.NoStart && !f.stayStoppedAfterInstall {
		f.running = true
	}
	return nil
}

func (f *fakeManager) Uninstall() error {
	if f.foreign {
		return svc.ErrForeign
	}
	f.installed = false
	f.running = false
	return nil
}

func (f *fakeManager) Start() error {
	if f.startErr != nil {
		return f.startErr
	}
	if f.foreign {
		return svc.ErrForeign
	}
	if !f.installed {
		return svc.ErrNotInstalled
	}
	f.running = true
	return nil
}

func (f *fakeManager) Stop() error {
	if f.stopErr != nil {
		return f.stopErr
	}
	if f.foreign {
		return svc.ErrForeign
	}
	f.running = false
	return nil
}

func (f *fakeManager) Restart() error {
	if err := f.Stop(); err != nil {
		return err
	}
	return f.Start()
}

func (f *fakeManager) Status() (svc.Status, error) {
	if f.statusErr != nil {
		return svc.Status{}, f.statusErr
	}
	st := svc.Status{Platform: f.platform, DefinitionPath: f.path, ExternallyManaged: f.foreign}
	switch {
	case f.running:
		st.State = svc.StateRunning
		st.PID = 11
	case f.installed || f.foreign:
		st.State = svc.StateStopped
	default:
		st.State = svc.StateNotInstalled
	}
	return st, nil
}

func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local/state"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local/share"))
	t.Setenv("HISTER_DATA_DIR", filepath.Join(home, "hister-data"))
	t.Setenv("HISTER_CONFIG", "")
	t.Setenv("HISTER_PORT", "")
	return home
}

func withFakeManager(t *testing.T, m svc.Manager) {
	t.Helper()
	orig := newServiceManager
	newServiceManager = func() (svc.Manager, error) { return m, nil }
	t.Cleanup(func() { newServiceManager = orig })
}

func executeArgs(t *testing.T, args ...string) error {
	t.Helper()
	resetRootState(t)
	rootCmd.SetArgs(args)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	return rootCmd.Execute()
}

func TestServiceHelpNoSubcommand(t *testing.T) {
	isolateHome(t)
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})
	if err := executeArgs(t, "service"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "install") {
		t.Fatalf("help output: %s", buf.String())
	}
}

func TestServiceInstallHelpHidesLogLevel(t *testing.T) {
	isolateHome(t)
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})
	if err := executeArgs(t, "service", "install", "--help"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "log-level") || strings.Contains(out, "--log-level") {
		t.Fatalf("install help should hide --log-level:\n%s", out)
	}
	if !strings.Contains(out, "--config") {
		t.Fatalf("install help should show --config:\n%s", out)
	}
}

func TestServiceStatusHelpHidesConfig(t *testing.T) {
	isolateHome(t)
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})
	if err := executeArgs(t, "service", "status", "--help"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "--config") {
		t.Fatalf("status help should hide --config:\n%s", out)
	}
	if strings.Contains(out, "log-level") || strings.Contains(out, "--log-level") {
		t.Fatalf("status help should hide --log-level:\n%s", out)
	}
}

func TestServiceUninstallHelpHidesConfig(t *testing.T) {
	isolateHome(t)
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})
	if err := executeArgs(t, "service", "uninstall", "--help"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "--config") {
		t.Fatalf("uninstall help should hide --config:\n%s", out)
	}
}

func TestServiceStatusSkipBrokenConfig(t *testing.T) {
	home := isolateHome(t)
	cfgPath := filepath.Join(home, ".config", "hister", "config.yml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(":\n  - not yaml"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeManager{platform: "fake", path: filepath.Join(home, "svc"), installed: true}
	withFakeManager(t, fake)
	err := executeArgs(t, "service", "status")
	if ProcessExitCode(err) != svc.ExitStopped {
		t.Fatalf("status err=%v code=%d", err, ProcessExitCode(err))
	}
}

func TestServiceInstallMissingConfigDoesNotCreateData(t *testing.T) {
	home := isolateHome(t)
	missing := filepath.Join(home, "nope.yml")
	dataDir := os.Getenv("HISTER_DATA_DIR")
	err := executeArgs(t, "--config", missing, "service", "install")
	if err == nil {
		t.Fatal("expected missing config error")
	}
	if _, statErr := os.Stat(dataDir); !os.IsNotExist(statErr) {
		t.Fatalf("data dir created: %v", statErr)
	}
}

func TestServiceInstallPinsSourcePath(t *testing.T) {
	home := isolateHome(t)
	t.Chdir(home)
	if err := os.WriteFile("mine.yml", []byte("app:\n  title: fromfile\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeManager{platform: "fake", path: filepath.Join(home, "svc")}
	withFakeManager(t, fake)
	if err := executeArgs(t, "--config", "./mine.yml", "service", "install"); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs("mine.yml")
	if err != nil {
		t.Fatal(err)
	}
	if fake.lastDef.ConfigPath != want {
		t.Fatalf("config path = %q, want %q", fake.lastDef.ConfigPath, want)
	}
	if fake.lastDef.DataDir == "" || !filepath.IsAbs(fake.lastDef.DataDir) {
		t.Fatalf("data dir %q", fake.lastDef.DataDir)
	}
}

func TestServiceInstallNoStart(t *testing.T) {
	home := isolateHome(t)
	fake := &fakeManager{platform: "fake", path: filepath.Join(home, "svc")}
	withFakeManager(t, fake)
	if err := executeArgs(t, "service", "install", "--no-start"); err != nil {
		t.Fatal(err)
	}
	if !fake.lastOpts.NoStart || fake.running {
		t.Fatalf("opts=%+v running=%v", fake.lastOpts, fake.running)
	}
}

func TestServiceStatusCodes(t *testing.T) {
	home := isolateHome(t)
	fake := &fakeManager{platform: "fake", path: filepath.Join(home, "svc")}
	withFakeManager(t, fake)

	err := executeArgs(t, "service", "status")
	if ProcessExitCode(err) != svc.ExitNotInstalled {
		t.Fatalf("not installed code=%d err=%v", ProcessExitCode(err), err)
	}

	fake.installed = true
	err = executeArgs(t, "service", "status")
	if ProcessExitCode(err) != svc.ExitStopped {
		t.Fatalf("stopped code=%d err=%v", ProcessExitCode(err), err)
	}

	fake.running = true
	if err := executeArgs(t, "service", "status"); err != nil {
		t.Fatal(err)
	}
}

func TestServiceStatusQueryFailure(t *testing.T) {
	isolateHome(t)
	fake := &fakeManager{statusErr: errors.New("bus down")}
	withFakeManager(t, fake)
	err := executeArgs(t, "service", "status")
	if ProcessExitCode(err) != 1 {
		t.Fatalf("query failure code=%d err=%v", ProcessExitCode(err), err)
	}
}

func TestServiceStopStartRestart(t *testing.T) {
	home := isolateHome(t)
	fake := &fakeManager{platform: "fake", path: filepath.Join(home, "svc"), installed: true, running: true}
	withFakeManager(t, fake)
	if err := executeArgs(t, "service", "stop"); err != nil {
		t.Fatal(err)
	}
	if fake.running {
		t.Fatal("still running")
	}
	if err := executeArgs(t, "service", "start"); err != nil {
		t.Fatal(err)
	}
	if err := executeArgs(t, "service", "restart"); err != nil {
		t.Fatal(err)
	}
}

func TestServiceUninstallKeepsDataDir(t *testing.T) {
	home := isolateHome(t)
	data := os.Getenv("HISTER_DATA_DIR")
	if err := os.MkdirAll(data, 0o750); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(data, "keep-me")
	if err := os.WriteFile(marker, []byte("idx"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeManager{platform: "fake", path: filepath.Join(home, "svc"), installed: true, running: true}
	withFakeManager(t, fake)
	if err := executeArgs(t, "service", "uninstall"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("data was deleted")
	}
}

func TestServiceInstallRefusesUnpersistedAccessToken(t *testing.T) {
	home := isolateHome(t)
	t.Setenv("HISTER__APP__ACCESS_TOKEN", "super-secret-token-value")
	fake := &fakeManager{platform: "fake", path: filepath.Join(home, "svc")}
	withFakeManager(t, fake)
	err := executeArgs(t, "service", "install")
	if err == nil {
		t.Fatal("expected refuse")
	}
	if !strings.Contains(err.Error(), "HISTER__APP__ACCESS_TOKEN") {
		t.Fatalf("error = %v", err)
	}
	if fake.installed {
		t.Fatal("must not write a service definition")
	}
}

func TestServiceInstallAllowsPinnedDataDir(t *testing.T) {
	home := isolateHome(t)
	fake := &fakeManager{platform: "fake", path: filepath.Join(home, "svc")}
	withFakeManager(t, fake)
	if err := executeArgs(t, "service", "install"); err != nil {
		t.Fatal(err)
	}
	if !fake.installed {
		t.Fatal("expected install")
	}
}

func TestServiceInstallRefusesHisterPort(t *testing.T) {
	home := isolateHome(t)
	t.Setenv("HISTER_PORT", "9999")
	fake := &fakeManager{platform: "fake", path: filepath.Join(home, "svc")}
	withFakeManager(t, fake)
	err := executeArgs(t, "service", "install")
	if err == nil || !strings.Contains(err.Error(), "HISTER_PORT") {
		t.Fatalf("error = %v", err)
	}
}

func TestServiceUninstallQueryFailureDoesNotClaimRemoval(t *testing.T) {
	home := isolateHome(t)
	fake := &fakeManager{platform: "fake", path: filepath.Join(home, "svc"), installed: true, statusErr: errors.New("bus down")}
	withFakeManager(t, fake)
	out := captureStdout(t, func() {
		err := executeArgs(t, "service", "uninstall")
		if ProcessExitCode(err) != 1 {
			t.Fatalf("err=%v code=%d", err, ProcessExitCode(err))
		}
	})
	if strings.Contains(out, "Removed the Hister service definition.") {
		t.Fatalf("claimed removal: %s", out)
	}
	if !fake.installed {
		t.Fatal("query failure must not uninstall")
	}
}

func TestServiceInstallFailsWhenProcessNotRunning(t *testing.T) {
	home := isolateHome(t)
	fake := &fakeManager{platform: "fake", path: filepath.Join(home, "svc"), stayStoppedAfterInstall: true}
	withFakeManager(t, fake)
	err := executeArgs(t, "service", "install")
	if err == nil {
		t.Fatal("expected install error when process did not start")
	}
	if !strings.Contains(err.Error(), "process is not running") {
		t.Fatalf("error = %v", err)
	}
	if ProcessExitCode(err) != 1 {
		t.Fatalf("code=%d", ProcessExitCode(err))
	}
}

func TestServiceInstallHISTERConfigMissingDoesNotFallback(t *testing.T) {
	home := isolateHome(t)
	discovered := filepath.Join(home, ".config", "hister", "config.yml")
	if err := os.MkdirAll(filepath.Dir(discovered), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(discovered, []byte("app:\n  title: discovered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HISTER_CONFIG", filepath.Join(home, "missing.yml"))
	fake := &fakeManager{platform: "fake", path: filepath.Join(home, "svc")}
	withFakeManager(t, fake)
	err := executeArgs(t, "service", "install")
	if err == nil {
		t.Fatal("expected missing HISTER_CONFIG to fail install")
	}
	if fake.installed {
		t.Fatal("must not install using a fallback config")
	}
}

func TestServiceInstallPinsHISTERConfig(t *testing.T) {
	home := isolateHome(t)
	path := filepath.Join(home, "env.yml")
	if err := os.WriteFile(path, []byte("app:\n  title: fromenv\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HISTER_CONFIG", path)
	fake := &fakeManager{platform: "fake", path: filepath.Join(home, "svc")}
	withFakeManager(t, fake)
	if err := executeArgs(t, "service", "install"); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	if fake.lastDef.ConfigPath != want {
		t.Fatalf("config path = %q, want HISTER_CONFIG %q", fake.lastDef.ConfigPath, want)
	}
}

func TestServiceUninstallAlreadyAbsent(t *testing.T) {
	home := isolateHome(t)
	fake := &fakeManager{platform: "fake", path: filepath.Join(home, "svc")}
	withFakeManager(t, fake)
	out := captureStdout(t, func() {
		if err := executeArgs(t, "service", "uninstall"); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "Hister service is not installed.") {
		t.Fatalf("output: %s", out)
	}
	if strings.Contains(out, "Removed the Hister service definition.") {
		t.Fatalf("absent uninstall should not claim removal: %s", out)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()
	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	fn()
	_ = w.Close()
	return <-done
}
