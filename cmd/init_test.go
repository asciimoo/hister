// SPDX-FileContributor: sathwick-p
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestRequireExplicitInstallConfigMissingFile(t *testing.T) {
	resetRootState(t)
	missing := filepath.Join(t.TempDir(), "nope.yml")
	cfgFile = missing
	rootCmd.PersistentFlags().Lookup("config").Changed = true

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local/state"))
	t.Setenv("HISTER_DATA_DIR", filepath.Join(home, "data"))

	err := pinInstallConfigPath()
	if err == nil {
		t.Fatal("expected error for missing --config file")
	}
	if _, statErr := os.Stat(filepath.Join(home, "data")); !os.IsNotExist(statErr) {
		t.Fatalf("data directory was created before install config validation failed: %v", statErr)
	}
	if _, err := os.Stat(filepath.Join(home, "data", ".secret_key")); !os.IsNotExist(err) {
		t.Fatal("secret key was created for a missing install --config")
	}
}

func TestRequireExplicitInstallConfigUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read mode 000 files")
	}
	resetRootState(t)
	path := filepath.Join(t.TempDir(), "secret.yml")
	if err := os.WriteFile(path, []byte("app:\n  title: x\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	cfgFile = path
	rootCmd.PersistentFlags().Lookup("config").Changed = true
	if err := pinInstallConfigPath(); err == nil {
		t.Fatal("expected error for unreadable --config file")
	}
}

func TestRequireExplicitInstallConfigDirectory(t *testing.T) {
	resetRootState(t)
	dir := t.TempDir()
	cfgFile = dir
	rootCmd.PersistentFlags().Lookup("config").Changed = true
	if err := pinInstallConfigPath(); err == nil {
		t.Fatal("expected directory config path to be rejected")
	} else if !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("directory config error = %v", err)
	}
}

func TestRequireExplicitInstallConfigAbsolute(t *testing.T) {
	resetRootState(t)
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("mine.yml", []byte("app:\n  title: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgFile = "./mine.yml"
	rootCmd.PersistentFlags().Lookup("config").Changed = true
	if err := pinInstallConfigPath(); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs("mine.yml")
	if err != nil {
		t.Fatal(err)
	}
	if cfgFile != want {
		t.Fatalf("cfgFile = %q, want %q", cfgFile, want)
	}
}

func TestRequireExplicitInstallConfigUnchangedSkips(t *testing.T) {
	resetRootState(t)
	cfgFile = filepath.Join(t.TempDir(), "missing.yml")
	if err := pinInstallConfigPath(); err != nil {
		t.Fatalf("unchanged --config: %v", err)
	}
}

func TestPinInstallConfigFromHISTERConfig(t *testing.T) {
	resetRootState(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "env.yml")
	if err := os.WriteFile(path, []byte("app:\n  title: env\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HISTER_CONFIG", path)
	if err := pinInstallConfigPath(); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfgFile != want {
		t.Fatalf("cfgFile = %q, want %q", cfgFile, want)
	}
	if !installConfigPinned {
		t.Fatal("expected installConfigPinned")
	}
}

func TestPinInstallConfigHISTERConfigMissing(t *testing.T) {
	resetRootState(t)
	missing := filepath.Join(t.TempDir(), "nope.yml")
	t.Setenv("HISTER_CONFIG", missing)
	if err := pinInstallConfigPath(); err == nil {
		t.Fatal("expected error for missing HISTER_CONFIG")
	}
}

func TestSkipConfigInitAnnotation(t *testing.T) {
	cmd := serviceStatusCmd
	if !skipConfigInit(cmd) {
		t.Fatal("status command should skip config init")
	}
	if skipConfigInit(serviceInstallCmd) {
		t.Fatal("install command must load config")
	}
}

func resetRootState(t *testing.T) {
	t.Helper()
	prevFile := cfgFile
	prevCfg := cfg
	prevPinned := installConfigPinned
	t.Cleanup(func() {
		cfgFile = prevFile
		cfg = prevCfg
		installConfigPinned = prevPinned
		resetCommandTree(rootCmd)
		rootCmd.SetArgs(nil)
	})
	cfgFile = "config.yml"
	cfg = nil
	installConfigPinned = false
	resetCommandTree(rootCmd)
	rootCmd.SetArgs(nil)
}

func resetCommandTree(cmd *cobra.Command) {
	cmd.SilenceErrors = false
	cmd.SilenceUsage = false
	resetFlagSet(cmd.PersistentFlags())
	resetFlagSet(cmd.Flags())
	for _, child := range cmd.Commands() {
		resetCommandTree(child)
	}
}

func resetFlagSet(fs *pflag.FlagSet) {
	if fs == nil {
		return
	}
	fs.VisitAll(func(f *pflag.Flag) {
		f.Changed = false
		_ = f.Value.Set(f.DefValue)
	})
}
