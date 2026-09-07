// SPDX-License-Identifier: AGPL-3.0-or-later

package diagnostics

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/extractor"
	"github.com/asciimoo/hister/server/indexer"
	"github.com/asciimoo/hister/server/types"
)

// Extractors validates a fresh registry so inspection cannot alter the running
// extractor chain. Unknown extractor names are errors rather than ignored keys.
func Extractors(c *config.Config) (*extractor.Registry, error) {
	r, err := extractor.NewRegistry(extractor.DefaultExtractors()...)
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool)
	for _, candidate := range r.Extractors() {
		known[strings.ToLower(candidate.Name())] = true
	}
	for name, entry := range c.Extractors {
		if !known[name] {
			return nil, fmt.Errorf("unknown extractor %q; use a lower case extractor name", name)
		}
		if entry == nil {
			return nil, fmt.Errorf("extractor %q requires a configuration mapping", name)
		}
	}
	if err := r.Init(c.Extractors); err != nil {
		return nil, err
	}
	return r, nil
}

// Dependencies checks executable availability without running an extractor.
func Dependencies(r *extractor.Registry) []types.DiagnosticCheck {
	checks := []types.DiagnosticCheck{}
	for _, e := range r.List() {
		if !e.Enabled || e.Name != "Ytdlp" {
			continue
		}
		check := types.DiagnosticCheck{Name: "extractor.ytdlp", Status: "ok", Message: "Configured yt-dlp executable is available"}
		binary, _ := e.Options["binary"].(string)
		if _, err := exec.LookPath(binary); err != nil {
			check.Status = "error"
			check.Message = "Install yt-dlp or set extractors.ytdlp.options.binary to an executable path"
		}
		checks = append(checks, check)
	}
	if len(checks) == 0 {
		checks = append(checks, types.DiagnosticCheck{Name: "extractors", Status: "ok", Message: "Enabled extractors require no external executables"})
	}
	return checks
}

type indexMetadataReader interface {
	GetMetadata() (int, string, error)
	GetEmbeddingFingerprint() (string, error)
}

// Index compares stored metadata against the configuration and binary of the
// server that owns the index. It never opens or updates an index itself.
func Index(c *config.Config, idx indexMetadataReader) []types.DiagnosticCheck {
	version, analyzer, err := idx.GetMetadata()
	if err != nil {
		return []types.DiagnosticCheck{{Name: "index.metadata", Status: "error", Message: "Cannot read consistent index metadata; inspect server logs and rebuild with hister reindex"}}
	}
	checks := []types.DiagnosticCheck{
		{Name: "index.version", Status: "ok", Message: fmt.Sprintf("Index version %d matches the server", version)},
		{Name: "index.analyzer", Status: "ok", Message: "Analyzer settings match the stored index"},
	}
	if version != indexer.Version {
		checks[0].Status = "error"
		checks[0].Message = fmt.Sprintf("Index version %d differs from server version %d; run hister reindex", version, indexer.Version)
	}
	if analyzer != indexer.AnalyzerFingerprint(c.Indexer.DetectLanguages, c.Indexer.KeepStopwords) {
		checks[1].Status = "error"
		checks[1].Message = "Analyzer settings differ or metadata is missing; run hister reindex"
	}
	if c.SemanticSearch.Enable {
		check := types.DiagnosticCheck{Name: "index.embeddings", Status: "ok", Message: "Embedding settings match the stored index"}
		fingerprint, err := idx.GetEmbeddingFingerprint()
		if err != nil || fingerprint != c.SemanticSearch.EmbeddingFingerprint() {
			check.Status = "error"
			check.Message = "Embedding settings differ or metadata is missing; run hister reindex"
		}
		checks = append(checks, check)
	}
	return checks
}
