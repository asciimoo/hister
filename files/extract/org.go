// SPDX-License-Identifier: AGPL-3.0-or-later

package extract

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/niklasfasching/go-org/org"

	"github.com/asciimoo/nogon/server/sanitizer"
)

type orgFileType struct{}

func (orgFileType) Match(path string) bool {
	return hasExtension(path, ".org")
}

func (orgFileType) Prepare(_ context.Context, d *Result, orgData []byte) error {
	src := strings.TrimSpace(string(orgData))
	if src == "" {
		return errors.New("org file empty")
	}
	html, title, err := renderOrg(orgData)
	if err != nil {
		return fmt.Errorf("rendering org: %w", err)
	}
	d.HTML = html
	d.Text = sanitizer.SanitizeText(d.HTML)
	d.Title = title
	d.Format = Org
	return nil
}

func renderOrg(src []byte) (string, string, error) {
	parser := org.New().Silent()
	var readErr error
	parser.ReadFile = func(string) ([]byte, error) {
		readErr = ErrExternalFileReference
		return nil, readErr
	}
	doc := parser.Parse(bytes.NewReader(src), "")

	html, err := doc.Write(org.NewHTMLWriter())
	// Includes are resolved during rendering, while setup files are read during
	// parsing. The library logs read errors and continues, so check both phases.
	if readErr != nil {
		return "", "", readErr
	}
	if err != nil {
		return "", "", err
	}
	title := doc.Get("TITLE")
	return html, title, nil
}
