// SPDX-FileContributor: sathwick-p
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/asciimoo/hister/internal/svc"
)

var newServiceManager = svc.New

const serviceStartCheckDelay = 2 * time.Second

var serviceCmd = &cobra.Command{
	Use:   "service",
	Short: "Install and control a background Hister server",
	Long:  "Install a user-level background service that runs `hister listen`, or start, stop, and query that service.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return cmd.Help()
	},
}

var serviceInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install and start a user-level Hister service",
	Args:  cobra.NoArgs,
	RunE:  runServiceInstall,
}

var serviceUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Stop the service and remove the Hister-managed definition",
	Args:  cobra.NoArgs,
	RunE:  runServiceUninstall,
}

var serviceStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the installed Hister service",
	Args:  cobra.NoArgs,
	RunE:  runServiceStart,
}

var serviceStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the installed Hister service",
	Args:  cobra.NoArgs,
	RunE:  runServiceStop,
}

var serviceRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the installed Hister service",
	Args:  cobra.NoArgs,
	RunE:  runServiceRestart,
}

var serviceStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show whether the Hister service is running",
	Args:  cobra.NoArgs,
	RunE:  runServiceStatus,
}

func registerServiceCommand() {
	serviceInstallCmd.Flags().Bool("force", false, "replace an existing Hister-managed service definition")
	serviceInstallCmd.Flags().Bool("no-start", false, "write the service definition without starting it")

	annotateSkipConfig := func(cmd *cobra.Command) {
		if cmd.Annotations == nil {
			cmd.Annotations = make(map[string]string)
		}
		cmd.Annotations[skipConfigInitAnnotation] = "true"
	}
	annotateSkipConfig(serviceCmd)
	annotateSkipConfig(serviceUninstallCmd)
	annotateSkipConfig(serviceStartCmd)
	annotateSkipConfig(serviceStopCmd)
	annotateSkipConfig(serviceRestartCmd)
	annotateSkipConfig(serviceStatusCmd)

	serviceCmd.AddCommand(
		serviceInstallCmd,
		serviceUninstallCmd,
		serviceStartCmd,
		serviceStopCmd,
		serviceRestartCmd,
		serviceStatusCmd,
	)
	rootCmd.AddCommand(serviceCmd)
}

func configureServiceScopes() {
	setCommandScope(serviceCmd, executionScopeLocal)
	setCommandScope(serviceInstallCmd, executionScopeLocal)
	setCommandScope(serviceUninstallCmd, executionScopeLocal)
	setCommandScope(serviceStartCmd, executionScopeLocal)
	setCommandScope(serviceStopCmd, executionScopeLocal)
	setCommandScope(serviceRestartCmd, executionScopeLocal)
	setCommandScope(serviceStatusCmd, executionScopeLocal)
	serviceInstallCmd.Annotations[applicableFlagsAnnotation] = "config"
	for _, cmd := range []*cobra.Command{
		serviceCmd, serviceUninstallCmd, serviceStartCmd,
		serviceStopCmd, serviceRestartCmd, serviceStatusCmd,
	} {
		cmd.Annotations[applicableFlagsAnnotation] = ""
	}
	configureScopeGroups(serviceCmd)
}

func runServiceInstall(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true
	force, _ := cmd.Flags().GetBool("force")
	noStart, _ := cmd.Flags().GetBool("no-start")

	if err := refuseUnpersistedInstallEnv(); err != nil {
		return err
	}

	bin, err := svc.ResolveBinary()
	if err != nil {
		return err
	}
	dataDir, err := filepath.Abs(cfg.App.Directory)
	if err != nil {
		return fmt.Errorf("data directory: %w", err)
	}
	def := svc.Definition{
		Binary:  bin,
		DataDir: dataDir,
	}
	if path, ok := cfg.SourcePath(); ok {
		def.ConfigPath = path
	}

	m, err := newServiceManager()
	if err != nil {
		return err
	}
	if err := m.Install(def, svc.InstallOptions{Force: force, NoStart: noStart}); err != nil {
		return err
	}

	printInstallSummary(m, def, noStart)
	if !noStart {
		time.Sleep(serviceStartCheckDelay)
	}
	st, err := m.Status()
	if err != nil {
		if noStart {
			log.Warn().Err(err).Msg("Installed, but could not query service status")
			return nil
		}
		return fmt.Errorf("installed service definition at %s, but could not query status: %w", m.DefinitionPath(), err)
	}
	cliPrintf("  Status:      %s\n", st.String())
	if !noStart && st.State != svc.StateRunning {
		return fmt.Errorf("installed service definition at %s, but the process is not running", m.DefinitionPath())
	}
	return nil
}

func runServiceUninstall(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true
	m, err := newServiceManager()
	if err != nil {
		return err
	}
	st, err := m.Status()
	if err != nil {
		return err
	}
	if err := m.Uninstall(); err != nil {
		return err
	}
	if st.State == svc.StateNotInstalled {
		cliPrintln("Hister service is not installed.")
		return nil
	}
	cliPrintln(cliSuccessStyle.Render("✓") + " Removed the Hister service definition.")
	cliPrintln("Indexed data was not deleted.")
	return nil
}

func runServiceStart(cmd *cobra.Command, _ []string) error {
	return runServiceControl(cmd, func(m svc.Manager) error { return m.Start() }, "Started the Hister service.")
}

func runServiceStop(cmd *cobra.Command, _ []string) error {
	return runServiceControl(cmd, func(m svc.Manager) error { return m.Stop() }, "Stopped the Hister service.")
}

func runServiceRestart(cmd *cobra.Command, _ []string) error {
	return runServiceControl(cmd, func(m svc.Manager) error { return m.Restart() }, "Restarted the Hister service.")
}

func runServiceControl(cmd *cobra.Command, op func(svc.Manager) error, okMsg string) error {
	cmd.SilenceUsage = true
	m, err := newServiceManager()
	if err != nil {
		return err
	}
	if err := op(m); err != nil {
		return err
	}
	cliPrintln(cliSuccessStyle.Render("✓") + " " + okMsg)
	return nil
}

func runServiceStatus(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	m, err := newServiceManager()
	if err != nil {
		cmd.SilenceErrors = false
		return err
	}
	st, err := m.Status()
	if err != nil {
		cmd.SilenceErrors = false
		return err
	}
	printServiceStatus(st)
	code := st.ExitCode()
	if code == svc.ExitRunning {
		return nil
	}
	return exitError(code, st.String())
}

func printInstallSummary(m svc.Manager, def svc.Definition, noStart bool) {
	cliPrintln(cliSuccessStyle.Render("✓") + " Installed a " + m.Platform() + " user service.")
	cliPrintf("  Definition:  %s\n", m.DefinitionPath())
	cliPrintf("  Binary:      %s\n", def.Binary)
	if def.ConfigPath != "" {
		cliPrintf("  Config:      %s\n", def.ConfigPath)
	} else {
		cliPrintln("  Config:      built-in defaults (no --config)")
	}
	cliPrintf("  Data:        %s\n", def.DataDir)
	for i, logPath := range m.Logs() {
		if i == 0 {
			cliPrintf("  Logs:        %s\n", logPath)
		} else {
			cliPrintf("              %s\n", logPath)
		}
	}
	if noStart {
		cliPrintln("  The service was not started (--no-start).")
	}
	if note := m.LoginNote(); note != "" {
		cliPrintln(note)
	}
}

func printServiceStatus(st svc.Status) {
	cliPrintf("Hister service (%s)\n", st.Platform)
	if st.DefinitionPath != "" {
		cliPrintf("  Definition:  %s\n", st.DefinitionPath)
	}
	cliPrintf("  Status:      %s\n", st.String())
	if st.ExternallyManaged {
		cliPrintln("  Note:        managed outside `hister service`; change it through its original configuration or package manager")
	}
}

func refuseUnpersistedInstallEnv() error {
	names := unpersistedInstallEnvNames()
	if len(names) == 0 {
		return nil
	}
	return fmt.Errorf("refusing to install while unpersisted environment overrides are set: %s", strings.Join(names, ", "))
}

func unpersistedInstallEnvNames() []string {
	var names []string
	seen := map[string]struct{}{}
	add := func(name string) {
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	pinnedConfig := false
	if cfg != nil {
		_, pinnedConfig = cfg.SourcePath()
	}
	for _, env := range os.Environ() {
		name, val, _ := strings.Cut(env, "=")
		if val == "" {
			continue
		}
		switch name {
		case "HISTER_DATA_DIR", "HISTER__APP__DIRECTORY":
			continue
		case "HISTER_CONFIG":
			abs, absErr := filepath.Abs(val)
			if absErr == nil && pinnedConfig {
				if src, ok := cfg.SourcePath(); ok && src == abs {
					continue
				}
			}
			add(name)
		case "HISTER_PORT":
			add(name)
		default:
			if strings.HasPrefix(name, "HISTER__") {
				add(name)
			}
		}
	}
	slices.Sort(names)
	return names
}
