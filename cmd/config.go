// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/diagnostics"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// Configuration commands override the root initialization hook to avoid creating
// directories, secret keys, rules, TUI configuration, or log files.
func skipRuntimeInitialization(_ *cobra.Command, _ []string) {}

var configCmd = &cobra.Command{
	Use:              "config",
	Short:            "Create, inspect, and validate configuration",
	PersistentPreRun: skipRuntimeInitialization,
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the selected configuration file path",
	Long:  "Print the absolute path of the selected main configuration file, or (defaults) when no file is present. The file does not need to contain valid YAML.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		filename, explicit := inspectionConfigSource()
		name, err := config.ResolvePath(filename, explicit)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), configSourceLabel(name))
		return err
	},
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print effective configuration with credentials redacted",
	Long:  "Print effective main configuration as YAML after applying defaults, environment variables, and global flags. Credentials, header and cookie values, URL credentials and query values, database connection strings, and extractor extra arguments are redacted. Separate rules and TUI configuration are not included.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		c, _, err := inspectConfig()
		if err != nil {
			return err
		}
		values, err := c.Redacted()
		if err != nil {
			return err
		}
		encoder := yaml.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent(2)
		if err := encoder.Encode(values); err != nil {
			return err
		}
		return encoder.Close()
	},
}

var configValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate configuration without creating runtime files",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		c, name, err := inspectConfig()
		if err != nil {
			return err
		}
		if _, err := diagnostics.Extractors(c); err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Configuration valid: %s\n", configSourceLabel(name))
		return err
	},
}

func inspectionConfigSource() (string, bool) {
	if rootCmd.PersistentFlags().Changed("config") {
		return cfgFile, true
	}
	if name := os.Getenv("HISTER_CONFIG"); name != "" {
		return name, true
	}
	return cfgFile, false
}

func configSourceLabel(name string) string {
	if name == "" {
		return "(defaults)"
	}
	return name
}

func inspectConfig() (*config.Config, string, error) {
	filename, explicit := inspectionConfigSource()
	c, name, err := config.LoadForInspection(filename, explicit)
	if err != nil {
		return nil, name, err
	}
	flags := rootCmd.PersistentFlags()
	for flag, target := range map[string]*string{
		"log-level":  &c.App.LogLevel,
		"search-url": &c.App.SearchURL,
		"server-url": &c.Server.BaseURL,
		"token":      &c.App.AccessToken,
	} {
		if flags.Changed(flag) {
			*target, _ = flags.GetString(flag)
		}
	}
	return c, name, c.Validate()
}

var configCreateCmd = &cobra.Command{
	Use:   "create [FILENAME]",
	Short: "Create default configuration file",
	Long:  "Write the default configuration to FILENAME, or print it to standard output when no filename is given.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runConfigCreate,
}

var createConfigCmd = &cobra.Command{
	Use:              "create-config [FILENAME]",
	Short:            "Deprecated alias for config create",
	Long:             "Deprecated: use hister config create [FILENAME]. Write default configuration to FILENAME, or print it to standard output when no filename is given.",
	Hidden:           true,
	Args:             cobra.MaximumNArgs(1),
	PersistentPreRun: skipRuntimeInitialization,
	// Cobra's Deprecated field writes through the output writer, which can
	// contaminate redirected YAML. Keep the notice on stderr explicitly.
	PreRun: func(cmd *cobra.Command, _ []string) {
		cmd.PrintErrln("Command create-config is deprecated; use hister config create instead.")
	},
	RunE: runConfigCreate,
}

func runConfigCreate(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	content, err := yaml.Marshal(config.CreateDefaultConfig())
	if err != nil {
		return err
	}
	if len(args) == 0 {
		_, err := cmd.OutOrStdout().Write(content)
		return err
	}
	filename := args[0]
	if err := writeDefaultConfigFile(filename, content); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("config file %q already exists", filename)
		}
		return fmt.Errorf("create config file: %w", err)
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), cliSuccessStyle.Render("✓")+" Config file created: "+cliInfoStyle.Render(filename))
	return err
}

func writeDefaultConfigFile(filename string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(content)
	return errors.Join(writeErr, f.Close())
}
