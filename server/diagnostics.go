// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"github.com/asciimoo/hister/server/diagnostics"
	"github.com/asciimoo/hister/server/types"
)

func serveDiagnostics(c *webContext) {
	checks := diagnostics.Index(c.Config, c.Indexer)
	r, err := diagnostics.Extractors(c.Config)
	if err != nil {
		checks = append(checks, types.DiagnosticCheck{Name: "extractors", Status: "error", Message: "Invalid extractor configuration; run hister config validate on the server"})
	} else {
		checks = append(checks, diagnostics.Dependencies(r)...)
	}
	c.JSON(checks)
}
