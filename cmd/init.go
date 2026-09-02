// SPDX-FileContributor: sathwick-p
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/asciimoo/hister/config"
)

const skipConfigInitAnnotation = "hister_skip_config_init"

func persistentPreRun(cmd *cobra.Command, _ []string) error {
	if skipConfigInit(cmd) {
		return nil
	}
	if isServiceInstall(cmd) {
		if err := pinInstallConfigPath(); err != nil {
			return printPreRunError(cmd, err)
		}
	}
	if err := initialize(); err != nil {
		return printPreRunError(cmd, err)
	}
	if isServiceInstall(cmd) {
		if err := verifyLoadedInstallConfig(); err != nil {
			return printPreRunError(cmd, err)
		}
	}
	return nil
}

func printPreRunError(cmd *cobra.Command, err error) error {
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cliPrintln(cliErrorStyle.Render("Error!") + " " + err.Error())
	return err
}

func skipConfigInit(cmd *cobra.Command) bool {
	return cmd.Annotations[skipConfigInitAnnotation] == "true"
}

func isServiceInstall(cmd *cobra.Command) bool {
	return cmd.Name() == "install" && cmd.Parent() != nil && cmd.Parent().Name() == "service"
}

var installConfigPinned bool

func pinInstallConfigPath() error {
	path := cfgFile
	if !rootCmd.PersistentFlags().Changed("config") {
		path = os.Getenv("HISTER_CONFIG")
		if path == "" {
			return nil
		}
	}
	if path == "" {
		return errors.New("config path is empty")
	}
	abs, err := readableRegularFile(path)
	if err != nil {
		return err
	}
	cfgFile = abs
	installConfigPinned = true
	return nil
}

func verifyLoadedInstallConfig() error {
	if !installConfigPinned {
		return nil
	}
	got, ok := cfg.SourcePath()
	if !ok || got != cfgFile {
		return fmt.Errorf("config file %q was not loaded", cfgFile)
	}
	return nil
}

func readableRegularFile(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("config path %q: %w", path, err)
	}
	st, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("config file %q does not exist", abs)
		}
		return "", fmt.Errorf("config file %q: %w", abs, err)
	}
	if !st.Mode().IsRegular() {
		return "", fmt.Errorf("config file %q is not a regular file", abs)
	}
	f, err := os.Open(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("config file %q does not exist", abs)
		}
		return "", fmt.Errorf("config file %q: %w", abs, err)
	}
	closeErr := f.Close()
	if closeErr != nil {
		return "", fmt.Errorf("config file %q: %w", abs, closeErr)
	}
	return abs, nil
}

func initialize() error {
	if ll := os.Getenv("HISTER__APP__LOG_LEVEL"); ll != "debug" {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}
	if err := initConfig(); err != nil {
		return err
	}
	if cfg.Crawler.UserAgent != "" {
		UserAgent = cfg.Crawler.UserAgent
	}
	initLog()
	log.Debug().Str("filename", cfg.Filename()).Msg("Config initialization complete")
	log.Debug().Msg("Logging initialization complete")
	return nil
}

func initConfig() error {
	var err error

	if !installConfigPinned && !rootCmd.PersistentFlags().Changed("config") {
		if envConfig := os.Getenv("HISTER_CONFIG"); envConfig != "" {
			cfgFile = envConfig
		}
	}

	cfg, err = config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("failed to initialize config: %w", err)
	}

	if v, _ := rootCmd.PersistentFlags().GetString("log-level"); v != "" && (rootCmd.Flags().Changed("log-level") || cfg.App.LogLevel == "") {
		cfg.App.LogLevel = v
	}
	if v, _ := rootCmd.PersistentFlags().GetString("search-url"); v != "" && (rootCmd.Flags().Changed("search-url") || cfg.App.SearchURL == "") {
		cfg.App.SearchURL = v
	}
	if v, _ := rootCmd.PersistentFlags().GetString("server-url"); v != "" && rootCmd.Flags().Changed("server-url") {
		if err := cfg.UpdateBaseURL(v); err != nil {
			return fmt.Errorf("failed to initialize config: %w", err)
		}
	}
	if v, _ := rootCmd.PersistentFlags().GetString("token"); rootCmd.PersistentFlags().Changed("token") {
		cfg.App.AccessToken = v
	}
	if err := cfg.ValidatePublicMode(); err != nil {
		return fmt.Errorf("failed to initialize config: %w", err)
	}
	return nil
}
