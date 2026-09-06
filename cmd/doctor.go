// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/asciimoo/hister/client"
	"github.com/asciimoo/hister/server/diagnostics"
	"github.com/asciimoo/hister/server/types"

	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:              "doctor",
	Short:            "Diagnose configuration, connectivity, authentication, and index compatibility",
	Long:             "Check local configuration and extractor dependencies, then contact the configured server to verify authentication, index metadata, and server extractor dependencies. Does not repair data, run extractors, or contact an embedding provider. Server diagnostics require admin access in multi user mode.",
	Args:             cobra.NoArgs,
	PersistentPreRun: skipRuntimeInitialization,
	RunE:             runDoctor,
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true
	writer, err := newRecordWriter(cmd.OutOrStdout(), commandOutputFormat(cmd), []string{"name", "status", "message"})
	if err != nil {
		return err
	}
	failed := false
	report := func(check types.DiagnosticCheck) error {
		failed = failed || check.Status == "error"
		return writer.Write(map[string]any{"name": check.Name, "status": check.Status, "message": check.Message}, func(out io.Writer) error {
			_, err := fmt.Fprintf(out, "%s %s: %s\n", check.Status, check.Name, check.Message)
			return err
		})
	}
	c, name, configErr := inspectConfig()
	if configErr != nil {
		if err := report(types.DiagnosticCheck{Name: "config", Status: "error", Message: configErr.Error()}); err != nil {
			return err
		}
	} else {
		if err := report(types.DiagnosticCheck{Name: "config", Status: "ok", Message: "Configuration valid: " + configSourceLabel(name)}); err != nil {
			return err
		}
		registry, err := diagnostics.Extractors(c)
		local := []types.DiagnosticCheck{}
		if err != nil {
			local = append(local, types.DiagnosticCheck{Name: "extractors", Status: "error", Message: "Invalid extractor configuration; run hister config validate"})
		} else {
			local = diagnostics.Dependencies(registry)
		}
		for _, check := range local {
			check.Name = "local." + check.Name
			if err := report(check); err != nil {
				return err
			}
		}
		if err := diagnoseServer(cmd.Context(), newClientForConfig(c), report); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	if failed {
		return errors.New("doctor found problems; see the reported checks")
	}
	return nil
}

func diagnoseServer(ctx context.Context, cl *client.Client, report func(types.DiagnosticCheck) error) error {
	serverConfig, err := cl.FetchConfigContext(ctx)
	if err != nil {
		return report(types.DiagnosticCheck{Name: "server.connection", Status: "error", Message: diagnosticRequestError(err)})
	}
	if serverConfig.AuthMode != "none" && serverConfig.AuthMode != "token" && serverConfig.AuthMode != "user" {
		return report(types.DiagnosticCheck{Name: "server.connection", Status: "error", Message: "Response is not a supported Hister configuration; check --server-url"})
	}
	if err := report(types.DiagnosticCheck{Name: "server.connection", Status: "ok", Message: "Hister server is reachable"}); err != nil {
		return err
	}
	auth := types.DiagnosticCheck{Name: "server.authentication", Status: "ok", Message: "Server authentication succeeded"}
	if serverConfig.AuthMode == "none" {
		auth.Message = "Server does not require authentication"
	} else if !serverConfig.Authenticated {
		auth.Status = "error"
		auth.Message = "Server requires a valid token; set --token or app.access_token"
	}
	if err := report(auth); err != nil || auth.Status == "error" {
		return err
	}
	if !serverConfig.DiagnosticsAvailable {
		return report(types.DiagnosticCheck{Name: "server.diagnostics", Status: "warning", Message: "Server does not support diagnostics; update the server to check its index and dependencies"})
	}
	checks, err := cl.FetchDiagnostics(ctx)
	if err != nil {
		check := types.DiagnosticCheck{Name: "server.diagnostics", Status: "error", Message: diagnosticRequestError(err)}
		var status *client.HTTPError
		if errors.As(err, &status) {
			switch status.StatusCode {
			case http.StatusNotFound:
				check.Status, check.Message = "warning", "Server does not support diagnostics; update the server to check its index and dependencies"
			case http.StatusForbidden:
				check.Message = "Server diagnostics require an admin token in multi user mode"
			}
		}
		return report(check)
	}
	if len(checks) == 0 {
		return report(types.DiagnosticCheck{Name: "server.diagnostics", Status: "error", Message: "Server returned no diagnostic checks"})
	}
	for _, check := range checks {
		if check.Status != "ok" && check.Status != "error" && check.Status != "warning" {
			return report(types.DiagnosticCheck{Name: "server.diagnostics", Status: "error", Message: "Server returned an invalid diagnostic status"})
		}
		check.Name = "server." + check.Name
		if err := report(check); err != nil {
			return err
		}
	}
	return nil
}

// HTTP errors and transport errors can contain response bodies and credentials
// embedded in URLs. Only report a controlled explanation.
func diagnosticRequestError(err error) string {
	var status *client.HTTPError
	if errors.As(err, &status) {
		if status.StatusCode == http.StatusUnauthorized {
			return "Authentication failed; set --token or app.access_token"
		}
		return fmt.Sprintf("Server returned HTTP %d; check --server-url and server logs", status.StatusCode)
	}
	if errors.Is(err, context.Canceled) {
		return "Request canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "Request timed out; check the server or increase --client-timeout"
	}
	return "Cannot read a Hister response; check --server-url, network access, TLS certificates, and server logs"
}
