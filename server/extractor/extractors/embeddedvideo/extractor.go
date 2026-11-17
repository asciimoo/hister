// SPDX-License-Identifier: AGPL-3.0-or-later

// Package embeddedvideo extracts embedded video URLs from HTML documents.
package embeddedvideo

import (
	"bytes"
	"encoding/json"
	"strings"

	"golang.org/x/net/html"

	"github.com/asciimoo/nogon/server/extractor/sdk"
)

// videoEntry holds a single embedded video found in a document.
type videoEntry struct {
	URL  string `json:"url"`
	Type string `json:"type"`           // "iframe", "video", "embed", "object"
	Mime string `json:"mime,omitempty"` // MIME type when available
}

// knownVideoHosts contains full https:// URL prefixes for known video hosting
// services. Prefix matching prevents a malicious URL like
// https://evil.com/youtube.com/embed/ from being accepted.
var knownVideoHosts = []string{
	"https://youtube.com/embed/",
	"https://www.youtube.com/embed/",
	"https://youtube.com/v/",
	"https://www.youtube.com/v/",
	"https://youtu.be/",
	"https://player.vimeo.com/video/",
	"https://vimeo.com/video/",
	"https://www.dailymotion.com/embed/",
	"https://bitchute.com/embed/",
	"https://www.bitchute.com/embed/",
	"https://rumble.com/embed/",
	"https://player.twitch.tv/",
	"https://www.facebook.com/plugins/video",
	"https://www.instagram.com/p/",
	"https://www.tiktok.com/embed/",
	"https://ok.ru/videoembed/",
	"https://rutube.ru/play/embed/",
	"https://www.ted.com/talks/",
	"https://fast.wistia.com/embed/",
	"https://cdn.jwplayer.com/players/",
	"https://players.brightcove.net/",
	"https://www.metacafe.com/embed/",
	"https://streamable.com/e/",
	"https://odysee.com/$/embed/",
}

// htmlQuickCheck holds the byte strings used for a cheap pre-scan. If none
// of these appear in the raw HTML there is nothing for this extractor to do.
var htmlQuickCheck = []string{"<iframe", "<video", "<embed", "<object"}

// EmbeddedVideoExtractor scans HTML for embedded video elements and stores
// their URLs in d.Metadata["videos"]. Its enrichment capability keeps body
// extraction independent from this metadata pass.
type EmbeddedVideoExtractor struct {
	sdk.ConfigSupport
}

func (e *EmbeddedVideoExtractor) Name() string {
	return "EmbeddedVideo"
}

func (e *EmbeddedVideoExtractor) Description() string {
	return "Scans HTML for embedded video tags (iframe, video, embed, object) and stores discovered video URLs in document metadata."
}

func (e *EmbeddedVideoExtractor) Capabilities() sdk.Capabilities {
	return sdk.Capabilities{Enrich: true}
}

// Match returns true only when the raw HTML plausibly contains a video
// element, avoiding the tokenizer overhead on pages without any embeds.
func (e *EmbeddedVideoExtractor) Match(d *sdk.Document) bool {
	if len(d.HTML) == 0 {
		return false
	}
	lower := strings.ToLower(d.HTML)
	for _, needle := range htmlQuickCheck {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

// Extract scans d.HTML for video embedding elements and appends any
// discovered videos to d.Metadata["videos"].
func (e *EmbeddedVideoExtractor) Extract(d *sdk.Document) sdk.ExtractResult {
	videos := extractVideos(d.HTML)
	if len(videos) > 0 {
		raw, err := json.Marshal(videos)
		if err != nil {
			return sdk.ExtractFallback(err)
		}
		d.AddMetadata("videos", string(raw))
		return sdk.Extracted()
	}
	return sdk.ExtractFallback(nil)
}

// Preview does not provide a custom rendering; let the chain continue.
func (e *EmbeddedVideoExtractor) Preview(d *sdk.Document) sdk.PreviewResult {
	return sdk.PreviewFallback(nil)
}

// extractVideos tokenizes raw HTML and returns all embedded video entries
// found in iframe/video/source/embed/object elements, preserving the original
// embedding type of each one.
func extractVideos(rawHTML string) []videoEntry {
	var videos []videoEntry
	seen := make(map[string]struct{})

	add := func(e videoEntry) {
		e.URL = strings.TrimSpace(e.URL)
		if e.URL == "" {
			return
		}
		if _, dup := seen[e.URL]; dup {
			return
		}
		seen[e.URL] = struct{}{}
		videos = append(videos, e)
	}

	z := html.NewTokenizer(bytes.NewReader([]byte(rawHTML)))
	inVideo := false // true while inside a <video> element
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return videos
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			attrs := readAttrs(z, hasAttr)
			tag := string(name)
			switch tag {
			case "video":
				inVideo = true
				if src := attrs["src"]; src != "" {
					add(videoEntry{URL: src, Type: "video", Mime: attrs["type"]})
				}
			case "source":
				if inVideo {
					if src := attrs["src"]; src != "" {
						add(videoEntry{URL: src, Type: "video", Mime: attrs["type"]})
					}
				}
			case "iframe":
				if src := attrs["src"]; src != "" && isVideoEmbedURL(src) {
					add(videoEntry{URL: src, Type: "iframe"})
				}
			case "embed":
				if src := attrs["src"]; src != "" && isVideoEmbedURL(src) {
					add(videoEntry{URL: src, Type: "embed", Mime: attrs["type"]})
				}
			case "object":
				if data := attrs["data"]; data != "" && isVideoEmbedURL(data) {
					add(videoEntry{URL: data, Type: "object", Mime: attrs["type"]})
				}
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			if string(name) == "video" {
				inVideo = false
			}
		}
	}
}

// readAttrs reads all attributes from the current token into a lowercase-keyed
// map. First occurrence of each key wins (mirrors browser behaviour).
func readAttrs(z *html.Tokenizer, hasAttr bool) map[string]string {
	attrs := make(map[string]string)
	for hasAttr {
		var k, v []byte
		k, v, hasAttr = z.TagAttr()
		key := strings.ToLower(string(k))
		if _, exists := attrs[key]; !exists {
			attrs[key] = string(v)
		}
	}
	return attrs
}

// isVideoEmbedURL returns true when the URL matches a known video
// hosting / embed service by full https:// prefix.
func isVideoEmbedURL(u string) bool {
	lower := strings.ToLower(u)
	for _, prefix := range knownVideoHosts {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}
