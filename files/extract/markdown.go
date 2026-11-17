// SPDX-License-Identifier: AGPL-3.0-or-later

package extract

import (
	"context"
	"errors"
	"strings"

	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"

	"github.com/asciimoo/nogon/server/sanitizer"
)

type markdownFileType struct{}

func (markdownFileType) Match(path string) bool {
	return hasExtension(path, ".md", ".markdown")
}

func (markdownFileType) Prepare(_ context.Context, d *Result, mdData []byte) error {
	src := strings.TrimSpace(string(mdData))
	if src == "" {
		return errors.New("markdown file is empty")
	}
	d.HTML = renderMarkdown(mdData)
	d.Text = sanitizer.SanitizeText(d.HTML)
	d.Title = extractMarkdownTitle(src)
	d.Format = Markdown
	return nil
}

// extractMarkdownTitle returns the text of the first ATX H1 heading ("# ...").
// Returns an empty string if none is found.
func extractMarkdownTitle(src string) string {
	for _, line := range strings.SplitAfter(src, "\n") {
		t := strings.TrimRight(line, "\r\n")
		if after, ok := strings.CutPrefix(t, "# "); ok {
			return strings.TrimSpace(after)
		}
	}
	return ""
}

func renderMarkdown(src []byte) string {
	p := parser.NewWithExtensions(
		parser.CommonExtensions | parser.AutoHeadingIDs | parser.NoEmptyLineBeforeBlock,
	)
	opts := html.RendererOptions{Flags: html.CommonFlags | html.HrefTargetBlank}
	r := html.NewRenderer(opts)
	return string(markdown.ToHTML(src, p, r))
}
