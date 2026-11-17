// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/asciimoo/nogon/config"
	"github.com/asciimoo/nogon/server/crawler"
)

const (
	sitemapNamespace  = "http://www.sitemaps.org/schemas/sitemap/0.9"
	maxSitemapBytes   = 50 << 20
	maxSitemapEntries = 50_000
	maxSitemapFiles   = 50_000
)

// sitemapReader collects page URLs from XML sitemaps and sitemap indexes.
// Sitemap requests always use HTTP, including when pages use a browser backend.
type sitemapReader struct {
	client *crawler.HTTPClient
	robots *crawler.RobotsCache
	delay  time.Duration
}

func newSitemapReader(cfg *config.CrawlerConfig, robots *crawler.RobotsCache) (*sitemapReader, error) {
	client, err := crawler.NewHTTPClient(cfg)
	if err != nil {
		return nil, err
	}
	return &sitemapReader{client: client, robots: robots, delay: time.Duration(cfg.Delay) * time.Second}, nil
}

// Close releases idle connections used for sitemap requests.
func (r *sitemapReader) Close() {
	r.client.CloseIdleConnections()
}

// Read accepts plain or gzip compressed XML. References in sitemap indexes must
// be absolute HTTP or HTTPS URLs, including when the input is a local file.
func (r *sitemapReader) Read(ctx context.Context, input io.Reader) ([]string, error) {
	contents, err := parseSitemap(input)
	if err != nil {
		return nil, err
	}
	return r.collect(ctx, contents)
}

// Fetch downloads a sitemap and any sitemaps referenced by its index.
func (r *sitemapReader) Fetch(ctx context.Context, rawURL string) ([]string, error) {
	location, err := sitemapURL(rawURL)
	if err != nil {
		return nil, err
	}
	return r.collect(ctx, sitemapContents{index: true, locations: []string{location}})
}

type sitemapContents struct {
	index     bool
	locations []string
}

func (r *sitemapReader) collect(ctx context.Context, root sitemapContents) ([]string, error) {
	var urls, pending []string
	seenURLs := make(map[string]bool)
	seenSitemaps := make(map[string]bool)
	add := func(contents sitemapContents) error {
		for _, location := range contents.locations {
			if contents.index {
				if !seenSitemaps[location] {
					if len(seenSitemaps) >= maxSitemapFiles {
						return fmt.Errorf("sitemap import exceeds %d sitemap files", maxSitemapFiles)
					}
					seenSitemaps[location] = true
					pending = append(pending, location)
				}
			} else if !seenURLs[location] {
				seenURLs[location] = true
				urls = append(urls, location)
			}
		}
		return nil
	}
	if err := add(root); err != nil {
		return nil, err
	}
	for i := 0; i < len(pending); i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if i > 0 && r.delay > 0 {
			timer := time.NewTimer(r.delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		contents, err := r.fetch(ctx, pending[i])
		if err != nil {
			return nil, fmt.Errorf("read sitemap %q: %w", pending[i], err)
		}
		if err := add(contents); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("sitemap contains no page URLs")
	}
	return urls, nil
}

func (r *sitemapReader) fetch(ctx context.Context, rawURL string) (_ sitemapContents, err error) {
	if r.robots != nil && !r.robots.Allowed(ctx, rawURL) {
		return sitemapContents{}, fmt.Errorf("disallowed by robots.txt")
	}
	resp, err := r.client.Get(ctx, rawURL)
	if err != nil {
		return sitemapContents{}, err
	}
	defer func() {
		if closeErr := resp.Body.Close(); err == nil {
			err = closeErr
		}
	}()
	if resp.StatusCode != http.StatusOK {
		return sitemapContents{}, fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}
	return parseSitemap(resp.Body)
}

func parseSitemap(input io.Reader) (_ sitemapContents, resultErr error) {
	reader := bufio.NewReader(input)
	var source io.Reader = reader
	if magic, _ := reader.Peek(2); bytes.Equal(magic, []byte{0x1f, 0x8b}) {
		compressed, err := gzip.NewReader(reader)
		if err != nil {
			return sitemapContents{}, fmt.Errorf("read gzip sitemap: %w", err)
		}
		defer func() {
			if err := compressed.Close(); resultErr == nil {
				resultErr = err
			}
		}()
		source = compressed
	}
	data, err := io.ReadAll(io.LimitReader(source, maxSitemapBytes+1))
	if err != nil {
		return sitemapContents{}, fmt.Errorf("read sitemap: %w", err)
	}
	if len(data) > maxSitemapBytes {
		return sitemapContents{}, fmt.Errorf("sitemap exceeds the 50 MiB uncompressed size limit")
	}

	decoder := xml.NewDecoder(bytes.NewReader(data))
	var contents sitemapContents
	var root xml.Name
	closed := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			if !closed {
				return sitemapContents{}, fmt.Errorf("missing sitemap root element")
			}
			return contents, nil
		}
		if err != nil {
			return sitemapContents{}, fmt.Errorf("parse sitemap XML: %w", err)
		}
		switch element := token.(type) {
		case xml.StartElement:
			if closed {
				return sitemapContents{}, fmt.Errorf("unexpected element after sitemap root")
			}
			if root.Local == "" {
				root = element.Name
				if (root.Local != "urlset" && root.Local != "sitemapindex") || (root.Space != "" && root.Space != sitemapNamespace) {
					return sitemapContents{}, fmt.Errorf("expected sitemap urlset or sitemapindex root, got %q", root.Local)
				}
				contents.index = root.Local == "sitemapindex"
				continue
			}
			entryName := "url"
			if contents.index {
				entryName = "sitemap"
			}
			if element.Name.Local != entryName || element.Name.Space != root.Space {
				if err := decoder.Skip(); err != nil {
					return sitemapContents{}, err
				}
				continue
			}
			location, err := decodeSitemapLocation(decoder, element)
			if err != nil {
				return sitemapContents{}, err
			}
			if len(contents.locations) >= maxSitemapEntries {
				return sitemapContents{}, fmt.Errorf("sitemap exceeds %d entries", maxSitemapEntries)
			}
			contents.locations = append(contents.locations, location)
		case xml.EndElement:
			closed = true
		case xml.CharData:
			if strings.TrimSpace(string(element)) != "" {
				return sitemapContents{}, fmt.Errorf("unexpected text outside sitemap entry")
			}
		}
	}
}

func decodeSitemapLocation(decoder *xml.Decoder, element xml.StartElement) (string, error) {
	var entry struct {
		Locations []struct {
			XMLName xml.Name
			Value   string `xml:",chardata"`
		} `xml:"loc"`
	}
	if err := decoder.DecodeElement(&entry, &element); err != nil {
		return "", fmt.Errorf("parse sitemap entry: %w", err)
	}
	var locations []string
	for _, location := range entry.Locations {
		if location.XMLName.Space == element.Name.Space {
			locations = append(locations, location.Value)
		}
	}
	if len(locations) != 1 {
		return "", fmt.Errorf("sitemap %s entry must contain exactly one loc", element.Name.Local)
	}
	return sitemapURL(locations[0])
}

func sitemapURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || strings.ContainsFunc(raw, unicode.IsSpace) {
		return "", fmt.Errorf("invalid sitemap URL %q: expected an absolute HTTP or HTTPS URL", raw)
	}
	u.Fragment = ""
	u.RawFragment = ""
	return u.String(), nil
}
