// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/diagnostics"
	"github.com/asciimoo/hister/server/document"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

const configPathAnnotation = "hister_config_path"

func bindConfigFlag(flags *pflag.FlagSet, name, path string) {
	flag := flags.Lookup(name)
	if flag.Annotations == nil {
		flag.Annotations = make(map[string][]string)
	}
	flag.Annotations[configPathAnnotation] = []string{path}
}

func configFlagOverrides(flagSets ...*pflag.FlagSet) []config.Override {
	var overrides []config.Override
	seen := make(map[*pflag.Flag]bool)
	values := viper.New()
	for _, flags := range flagSets {
		flags.VisitAll(func(flag *pflag.Flag) {
			paths := flag.Annotations[configPathAnnotation]
			if !flag.Changed || len(paths) != 1 || seen[flag] {
				return
			}
			seen[flag] = true
			// VisitAll supplies a nonnil flag, the only requirement of BindPFlag.
			_ = values.BindPFlag(flag.Name, flag)
			overrides = append(overrides, config.Override{
				Path: paths[0], Value: values.Get(flag.Name),
				Source: config.ValueSource{Kind: "flag", Name: "--" + flag.Name},
			})
		})
	}
	return overrides
}

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
	Long:  "Print the absolute path of the selected main configuration file, or (defaults) when no file is present. Without an explicit selection, search ./config.yml, then Nogon locations, then legacy Hister locations. The file does not need to contain valid YAML.",
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
	Long:  "Print effective main configuration as YAML after applying defaults, environment variables, and global flags. Credentials, header and cookie values, URL credentials and query values, database connection strings, and external command arguments are redacted. Separate rules and TUI configuration are not included.",
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
		if unsupported := document.UnsupportedLanguages(c.Indexer.Languages); len(unsupported) > 0 {
			return fmt.Errorf("unsupported language(s) in indexer.languages: %s", strings.Join(unsupported, ", "))
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
	if name, ok := os.LookupEnv("NOGON_CONFIG"); ok {
		return name, true
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
	c, name, err := config.LoadForInspection(filename, explicit, globalConfigOverrides()...)
	if err != nil {
		return nil, name, err
	}
	return c, name, c.Validate()
}

func globalConfigOverrides() []config.Override {
	return configFlagOverrides(rootCmd.PersistentFlags())
}

var configCreateCmd = &cobra.Command{
	Use:   "create [FILENAME]",
	Short: "Create a commented default configuration reference",
	Long:  "Write all default settings as commented YAML to FILENAME, or print them to standard output when no filename is given. Uncomment a section and selected settings to activate them, leaving unrelated settings commented. Use --full to write active YAML values for all defaults. Existing files are never overwritten.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runConfigCreate,
}

func runConfigCreate(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	content, err := yaml.Marshal(config.CreateDefaultConfig())
	if err != nil {
		return err
	}
	if full, _ := cmd.Flags().GetBool("full"); !full {
		var reference strings.Builder
		reference.WriteString("# Commented settings are examples and do not override web configuration.\n")
		reference.WriteString("# Uncomment a section and selected settings to manage them in this file.\n")
		for line := range strings.SplitAfterSeq(string(content), "\n") {
			if line != "" {
				reference.WriteString("# ")
				reference.WriteString(line)
			}
		}
		content = []byte(reference.String())
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
