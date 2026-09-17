// SPDX-License-Identifier: AGPL-3.0-or-later

package mastodon

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/asciimoo/hister/server/extractor/sdk"
	"github.com/asciimoo/hister/server/extractor/urlutil"
	"github.com/asciimoo/hister/server/sanitizer"

	"github.com/PuerkitoBio/goquery"
	"github.com/rs/zerolog/log"
)

type MastodonExtractor struct {
	sdk.ConfigSupport
}

func (e *MastodonExtractor) Name() string {
	return "Mastodon"
}

func (e *MastodonExtractor) Description() string {
	return "Extracts toots as individual documents from Mastodon websites."
}

func (e *MastodonExtractor) Capabilities() sdk.Capabilities {
	return sdk.Capabilities{Extract: true, Preview: true}
}

func (e *MastodonExtractor) Match(d *sdk.Document) bool {
	if strings.Contains(d.HTML, `"repository":"mastodon/mastodon"`) {
		return true
	}
	if d.Metadata != nil && d.Metadata["type"] == "toot" {
		return true
	}
	return false
}

func (e *MastodonExtractor) Extract(d *sdk.Document) sdk.ExtractResult {
	if d.Metadata != nil && d.Metadata["type"] == "toot" {
		return sdk.Extracted()
	}
	d.SkipIndexing = true
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(d.HTML))
	if err != nil {
		return sdk.ExtractFallback(nil)
	}
	statuses := doc.Find(".status, .detailed-status")
	if statuses.Length() == 0 {
		return sdk.Extracted()
	}

	pu, err := url.Parse(d.URL)
	if err != nil {
		return sdk.ExtractFallback(nil)
	}

	statuses.Each(func(_ int, s *goquery.Selection) {
		c := s.Find(".status__content")
		urlutil.RewriteURLs(c, pu)
		u, exists := s.Find(".status__relative-time, .detailed-status__datetime").First().Attr("href")
		if !exists {
			log.Debug().Msg("Failed to find URL for toot")
			return
		}
		h, err := c.Html()
		if err != nil {
			log.Debug().Msg("Failed to extract HTML for toot")
			return
		}
		// Keep the supplied permalink: status IDs are local to each instance,
		// and an account's domain may differ from its server's hostname.
		td := &sdk.Document{
			URL:    urlutil.ResolveURL(pu, u),
			Title:  "Mastodon toot: " + s.Find(".display-name").Text(),
			Text:   c.Text(),
			HTML:   h,
			UserID: d.UserID,
			Metadata: map[string]any{
				"type": "toot",
			},
		}
		d.ExtraDocuments = append(d.ExtraDocuments, td)
	})

	return sdk.Extracted()
}

func (e *MastodonExtractor) Preview(d *sdk.Document) sdk.PreviewResult {
	// TODO enhance the toot preview
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(d.HTML))
	if err != nil {
		return sdk.PreviewFallback(err)
	}

	var b strings.Builder

	// --- Title -----------------------------------------------------------
	if title := strings.TrimSpace(doc.Find("h1").First().Text()); title != "" {
		fmt.Fprintf(&b, "<h2>%s</h2>\n", title)
	}
	b.WriteString(d.HTML)

	// Always sanitize HTML before returning it to strip scripts, event
	// handlers, and other potentially unsafe markup.
	return sdk.Previewed(sdk.PreviewResponse{
		Content: sanitizer.SanitizeHTML(b.String()),
	})
}
