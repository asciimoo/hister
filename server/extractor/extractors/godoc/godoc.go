// Package godoc provides an extractor for pkg.go.dev documentation pages.
package godoc

import (
	"bytes"
	"errors"
	stdhtml "html"
	"io"
	"net/url"
	"slices"
	"strings"

	"golang.org/x/net/html"

	"github.com/asciimoo/nogon/server/extractor/sdk"
	"github.com/asciimoo/nogon/server/extractor/urlutil"
	"github.com/asciimoo/nogon/server/sanitizer"
)

const pkgGoDevPrefix = "https://pkg.go.dev/"

// GoDocExtractor extracts content from pkg.go.dev documentation pages.
type GoDocExtractor struct {
	sdk.ConfigSupport
}

// Name returns the extractor's identifier.
func (e *GoDocExtractor) Name() string {
	return "GoDoc"
}

// Description returns a short summary of what this extractor does.
func (e *GoDocExtractor) Description() string {
	return "Extracts and renders Go package documentation from pkg.go.dev pages."
}

func (e *GoDocExtractor) Capabilities() sdk.Capabilities {
	return sdk.Capabilities{Preview: true}
}

// Match returns true for any pkg.go.dev URL that has a non-empty path beyond
// the host (i.e. at least one character after https://pkg.go.dev/).
func (e *GoDocExtractor) Match(d *sdk.Document) bool {
	return strings.HasPrefix(d.URL, pkgGoDevPrefix) && len(d.URL) > len(pkgGoDevPrefix)
}

// Extract does not provide a custom extractor
func (e *GoDocExtractor) Extract(d *sdk.Document) sdk.ExtractResult {
	return sdk.ExtractFallback(nil)
}

// Preview returns the sanitized HTML of the documentation base
// element with all relative links and image sources rewritten
// to absolute URLs.
func (e *GoDocExtractor) Preview(d *sdk.Document) sdk.PreviewResult {
	base, err := url.Parse(d.URL)
	if err != nil {
		return sdk.PreviewFallback(err)
	}
	content, err := extractArticle(d.HTML, true, base)
	if err != nil {
		return sdk.PreviewFallback(err)
	}
	return sdk.Previewed(sdk.PreviewResponse{Content: sanitizer.SanitizeHTML(content)})
}

func extractArticle(rawHTML string, renderHTML bool, base *url.URL) (string, error) {
	z := html.NewTokenizer(strings.NewReader(rawHTML))
	depth := 0
	inArticle := false
	var buf bytes.Buffer

	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			if errors.Is(z.Err(), io.EOF) {
				return buf.String(), nil
			}
			return "", z.Err()

		case html.StartTagToken, html.SelfClosingTagToken:
			tn, hasAttr := z.TagName()
			tag := string(tn)
			attrs := collectAttrs(z)

			if !inArticle {
				if tag == "div" && hasAttr && hasClass(attrs["class"], "Documentation-content") {
					inArticle = true
					depth = 1
					if renderHTML {
						writeTag(&buf, tag, attrs, false, base)
					}
				}
			} else {
				if tt == html.StartTagToken {
					depth++
				}
				if renderHTML {
					writeTag(&buf, tag, attrs, tt == html.SelfClosingTagToken, base)
				}
			}

		case html.EndTagToken:
			if inArticle {
				depth--
				if depth == 0 {
					if renderHTML {
						buf.WriteString("</div>")
					}
					return buf.String(), nil
				}
				if renderHTML {
					buf.Write(z.Raw())
				}
			}

		case html.TextToken:
			if inArticle {
				if renderHTML {
					buf.Write(z.Raw())
				} else {
					buf.Write(z.Text())
				}
			}

		default:
			if inArticle && renderHTML {
				buf.Write(z.Raw())
			}
		}
	}
}

// writeTag writes a reconstructed start or self-closing tag to buf, rewriting
// href and src attribute values to absolute URLs using base.
func writeTag(buf *bytes.Buffer, tag string, attrs map[string]string, selfClosing bool, base *url.URL) {
	buf.WriteByte('<')
	buf.WriteString(tag)
	for k, v := range attrs {
		if base != nil && (k == "href" || k == "src") {
			v = urlutil.ResolveURL(base, v)
		}
		buf.WriteByte(' ')
		buf.WriteString(k)
		buf.WriteString(`="`)
		buf.WriteString(escapeAttr(v))
		buf.WriteByte('"')
	}
	if selfClosing {
		buf.WriteString("/>")
	} else {
		buf.WriteByte('>')
	}
}

// escapeAttr escapes special characters in HTML attribute values using the
// standard library's html.EscapeString.
func escapeAttr(s string) string {
	return stdhtml.EscapeString(s)
}

func collectAttrs(z *html.Tokenizer) map[string]string {
	attrs := make(map[string]string)
	for {
		key, val, more := z.TagAttr()
		if len(key) > 0 {
			attrs[string(key)] = string(val)
		}
		if !more {
			break
		}
	}
	return attrs
}

func hasClass(classAttr, target string) bool {
	return slices.Contains(strings.Fields(classAttr), target)
}
