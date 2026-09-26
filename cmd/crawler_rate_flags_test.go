package cmd

import (
	"testing"

	"github.com/spf13/cobra"

	"github.com/asciimoo/hister/config"
)

func newRateTestCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "index"}
	cmd.Flags().Float64("per-host-rps", 0, "")
	cmd.Flags().Int("delay", 0, "")
	return cmd
}

// TestApplyCrawlerRateFlags pins that the CLI writes the rate value the crawler
// actually reads. --delay used to be assigned to the deprecated
// CrawlerConfig.Delay field after the config loader's delay-to-rate conversion
// had already run, so the flag silently did nothing.
func TestApplyCrawlerRateFlags(t *testing.T) {
	tests := []struct {
		name      string
		configRPS float64
		flags     map[string]string
		want      float64
	}{
		{
			name:      "no flags leaves the configured rate alone",
			configRPS: 4,
			want:      4,
		},
		{
			name:      "per-host-rps overrides config",
			configRPS: 4,
			flags:     map[string]string{"per-host-rps": "0.5"},
			want:      0.5,
		},
		{
			name:      "deprecated delay is converted to a rate",
			configRPS: 4,
			flags:     map[string]string{"delay": "2"},
			want:      0.5,
		},
		{
			name:      "delay of one second is one request per second",
			flags:     map[string]string{"delay": "1"},
			want:      1,
		},
		{
			// "0 = no delay" cannot be expressed as a rate, so it carries no
			// instruction and must not quietly remove the configured limit.
			name:      "delay of zero leaves the configured rate alone",
			configRPS: 4,
			flags:     map[string]string{"delay": "0"},
			want:      4,
		},
		{
			name:      "per-host-rps wins over delay",
			configRPS: 4,
			flags:     map[string]string{"delay": "10", "per-host-rps": "2"},
			want:      2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newRateTestCommand()
			for name, value := range tt.flags {
				if err := cmd.Flags().Set(name, value); err != nil {
					t.Fatalf("set --%s: %v", name, err)
				}
			}

			rate := config.CrawlerRate{PerHostRPS: tt.configRPS}
			applyCrawlerRateFlags(cmd, &rate)

			if rate.PerHostRPS != tt.want {
				t.Errorf("PerHostRPS = %v, want %v", rate.PerHostRPS, tt.want)
			}
		})
	}
}

// TestIndexCommandExposesRateFlags guards the wiring: the helper above is only
// reachable if the flags are actually registered on the command.
func TestIndexCommandExposesRateFlags(t *testing.T) {
	for _, name := range []string{"per-host-rps", "delay"} {
		if indexCmd.Flags().Lookup(name) == nil {
			t.Errorf("index command has no --%s flag", name)
		}
	}
	if f := indexCmd.Flags().Lookup("delay"); f != nil && f.Deprecated == "" {
		t.Error("--delay should be marked deprecated in favor of --per-host-rps")
	}
}
