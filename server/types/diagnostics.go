// SPDX-License-Identifier: AGPL-3.0-or-later

package types

// DiagnosticCheck describes one inspection result without configuration values.
type DiagnosticCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}
