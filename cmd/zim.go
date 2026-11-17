// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/asciimoo/hister/client"
	"github.com/asciimoo/hister/files"
	"github.com/asciimoo/hister/files/extract"
	"github.com/asciimoo/hister/server/document"

	"github.com/PuerkitoBio/goquery"
	"github.com/spf13/cobra"
	"github.com/stazelabs/gozim/zim"
)

var importZIMCmd = &cobra.Command{
	Use:   "zim ARCHIVE",
	Short: "Import HTML and text from a Kiwix ZIM archive",
	Long: `Import HTML and plain text from a local Kiwix ZIM archive.

Original page URLs are read from canonical, Open Graph, or Twitter metadata,
or absolute archive entry URLs. Relative URL metadata is resolved using the
page's HTML base or another known original URL. Use --base-url to supply a
fallback website base when archive paths match the original website paths.
Page URL metadata takes priority over this fallback.

Every document whose original URL cannot be identified is reported on stderr
and skipped. Relative article links are resolved through their target archive
entries. Links without an identifiable destination are disabled.

Images, scripts, other assets, and redirects are skipped. Previews do not
reproduce archive navigation or embedded assets. Each entry is limited by
indexer.max_file_size_mb, with a maximum of 32 MiB. Date filters use the archive
publication date. Rerun with --skip-existing to keep already imported URLs.

Examples:
  nogon import zim collection.zim --label reference
  nogon import zim collection.zim --base-url https://example.org/docs/`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		stats, err := runZIMImport(ctx, cmd, files.ExpandHome(args[0]))
		return finishImport(cmd, stats, err)
	},
}

type zimRecord struct {
	Path    string
	Title   string
	MIME    string
	Content string
}

type zimURLResolver struct {
	base      *url.URL
	pages     map[string]string
	redirects map[string]string
	prefix    string
}

func newZIMURLResolver(cmd *cobra.Command) (*zimURLResolver, error) {
	r := &zimURLResolver{pages: make(map[string]string), redirects: make(map[string]string)}
	base, _ := cmd.Flags().GetString("base-url")
	if base != "" {
		if !validZIMWebURL(base) {
			return nil, errors.New("--base-url must be an absolute HTTP or HTTPS URL without credentials")
		}
		r.base, _ = url.Parse(base)
		if r.base.RawQuery != "" || r.base.ForceQuery || r.base.Fragment != "" {
			return nil, errors.New("--base-url must not contain a query or fragment")
		}
	}
	return r, nil
}

func (r *zimURLResolver) resolve(entry zimRecord) string {
	fallback := ""
	if validZIMWebURL(entry.Path) {
		fallback = entry.Path
	} else if r.base != nil && safeZIMRelativePath(entry.Path) {
		fallback = zimURLAtBase(r.base, entry.Path)
	}
	if entry.MIME == "text/plain" {
		return fallback
	}
	html, err := goquery.NewDocumentFromReader(strings.NewReader(entry.Content))
	if err != nil {
		return fallback
	}
	var candidates []string
	html.Find("link[rel]").Each(func(_ int, node *goquery.Selection) {
		for rel := range strings.FieldsSeq(node.AttrOr("rel", "")) {
			if strings.EqualFold(rel, "canonical") {
				candidates = append(candidates, node.AttrOr("href", ""))
				break
			}
		}
	})
	for _, property := range []string{"og:url", "twitter:url"} {
		html.Find("meta[property],meta[name]").Each(func(_ int, node *goquery.Selection) {
			if strings.EqualFold(node.AttrOr("property", ""), property) || strings.EqualFold(node.AttrOr("name", ""), property) {
				candidates = append(candidates, node.AttrOr("content", ""))
			}
		})
	}
	// An absolute page URL can also supply the origin for a relative canonical.
	for _, candidate := range candidates {
		if absolute := resolveZIMWebURL(candidate, nil); absolute != "" {
			fallback = absolute
			break
		}
	}
	var base *url.URL
	if fallback != "" {
		base, _ = url.Parse(fallback)
	}
	if explicit := zimHTMLBase(html, base); explicit != nil {
		base = explicit
	}
	for _, candidate := range candidates {
		if absolute := resolveZIMWebURL(candidate, base); absolute != "" {
			return absolute
		}
	}
	return fallback
}

func resolveZIMWebURL(raw string, base *url.URL) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	if !validZIMWebURL(u.String()) {
		return ""
	}
	return u.String()
}

func zimHTMLBase(html *goquery.Document, pageURL *url.URL) *url.URL {
	raw := html.Find("base[href]").First().AttrOr("href", "")
	if absolute := resolveZIMWebURL(raw, pageURL); absolute != "" {
		base, _ := url.Parse(absolute)
		return base
	}
	return nil
}

func (r *zimURLResolver) lookup(path string) string {
	// Bound redirect traversal, including cycles and dangling targets.
	for range 64 {
		if page, ok := r.pages[path]; ok {
			return page
		}
		target, ok := r.redirects[path]
		if !ok {
			return ""
		}
		path = target
	}
	return ""
}

func (r *zimURLResolver) rewriteLinks(entry zimRecord, sourceURL string) (string, error) {
	html, err := goquery.NewDocumentFromReader(strings.NewReader(entry.Content))
	if err != nil {
		return "", err
	}
	pageURL, _ := url.Parse(sourceURL)
	webBase := zimHTMLBase(html, pageURL)
	archiveBase := &url.URL{Path: "/" + r.prefix + entry.Path}
	if raw := html.Find("base[href]").First().AttrOr("href", ""); raw != "" {
		if base, err := url.Parse(raw); err == nil && base.Scheme == "" && base.Host == "" {
			archiveBase = archiveBase.ResolveReference(base)
		}
	}
	html.Find("a[href],area[href]").Each(func(_ int, node *goquery.Selection) {
		raw := strings.TrimSpace(node.AttrOr("href", ""))
		if strings.HasPrefix(raw, "#") {
			return
		}
		ref, err := url.Parse(raw)
		if err != nil {
			node.RemoveAttr("href")
			return
		}
		if ref.Scheme != "" {
			return
		}
		resolved := ""
		if ref.Host != "" {
			base := webBase
			if base == nil {
				base = pageURL
			}
			resolved = resolveZIMWebURL(raw, base)
		} else {
			target := archiveBase.ResolveReference(ref)
			path := strings.TrimPrefix(target.Path, "/")
			if path, ok := strings.CutPrefix(path, r.prefix); ok {
				if target.RawQuery != "" || target.ForceQuery {
					path += "?" + target.RawQuery
				}
				resolved = r.lookup(path)
			}
			if resolved != "" && ref.Fragment != "" {
				u, _ := url.Parse(resolved)
				u.Fragment, u.RawFragment = ref.Fragment, ref.RawFragment
				resolved = u.String()
			} else if resolved == "" && webBase != nil {
				resolved = resolveZIMWebURL(raw, webBase)
			}
		}
		if resolved == "" {
			// Do not leave archive paths pointing into Nogon's own routes.
			node.RemoveAttr("href")
		} else {
			node.SetAttr("href", resolved)
		}
	})
	// Preserve the page identity after removing its original HTML base.
	html.Find("link[rel]").Each(func(_ int, node *goquery.Selection) {
		for rel := range strings.FieldsSeq(node.AttrOr("rel", "")) {
			if strings.EqualFold(rel, "canonical") {
				node.SetAttr("href", sourceURL)
				break
			}
		}
	})
	html.Find("meta[property],meta[name]").Each(func(_ int, node *goquery.Selection) {
		for _, property := range []string{"og:url", "twitter:url"} {
			if strings.EqualFold(node.AttrOr("property", ""), property) || strings.EqualFold(node.AttrOr("name", ""), property) {
				node.SetAttr("content", sourceURL)
				break
			}
		}
	})
	// An archive's base must not affect the Nogon preview.
	html.Find("base").Remove()
	return html.Html()
}

func safeZIMRelativePath(entryPath string) bool {
	if entryPath == "" || strings.HasPrefix(entryPath, "/") || strings.ContainsAny(entryPath, "\\\r\n\x00") {
		return false
	}
	for segment := range strings.SplitSeq(entryPath, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func zimURLAtBase(base *url.URL, entryPath string) string {
	result := *base
	// ZIM entry paths are identifiers, not relative URL references. Escaping
	// them as paths prevents a leading // from replacing the specified host.
	result.Path = strings.TrimRight(base.Path, "/") + "/" + strings.TrimLeft(entryPath, "/")
	result.RawPath = strings.TrimRight(base.EscapedPath(), "/") + "/" + (&url.URL{Path: strings.TrimLeft(entryPath, "/")}).EscapedPath()
	return result.String()
}

// Existing bulk client calls use their own contexts. Bind all requests from
// this import to its lifetime, including capability discovery and existence checks.
type zimImportTransport struct {
	ctx  context.Context
	base http.RoundTripper
}

func (t zimImportTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx, cancel := context.WithCancel(req.Context())
	stop := context.AfterFunc(t.ctx, cancel)
	cleanup := func() {
		stop()
		cancel()
	}
	if err := t.ctx.Err(); err != nil {
		cleanup()
		return nil, err
	}
	response, err := t.base.RoundTrip(req.Clone(ctx))
	if err != nil {
		cleanup()
		return response, err
	}
	response.Body = &zimResponseBody{response.Body, cleanup}
	return response, nil
}

type zimResponseBody struct {
	io.ReadCloser
	cleanup func()
}

func (b *zimResponseBody) Close() error {
	defer b.cleanup()
	return b.ReadCloser.Close()
}

func runZIMImport(ctx context.Context, cmd *cobra.Command, filename string) (serviceImportStats, error) {
	var stats serviceImportStats
	if err := ctx.Err(); err != nil {
		return stats, err
	}
	batchSize, _ := cmd.Flags().GetInt("batch-size")
	if batchSize < 1 || batchSize > maxImportBatchSize {
		return stats, fmt.Errorf("--batch-size must be between 1 and %d", maxImportBatchSize)
	}
	dates, err := parseDateRangeFlags(cmd)
	if err != nil {
		return stats, err
	}
	resolver, err := newZIMURLResolver(cmd)
	if err != nil {
		return stats, err
	}
	if err := validateZIMArchive(ctx, filename); err != nil {
		return stats, fmt.Errorf("invalid ZIM archive: %w", err)
	}
	archive, err := zim.OpenWithOptions(filename, zim.WithMmap(false), zim.WithCacheSize(1))
	if err != nil {
		return stats, fmt.Errorf("open ZIM archive: %w", err)
	}
	defer func() { _ = archive.Close() }()
	maxSize := min(cfg.Indexer.MaxFileSize<<20, int64(extract.MaxContentSize))
	if maxSize <= 0 {
		maxSize = 1 << 20
	}
	global, _ := cmd.Flags().GetBool("global")
	clientOpts := targetUserIDClientOptions(cmd, global)
	clientOpts = append(clientOpts, documentSubmissionClientOptions(cmd)...)
	httpClient := &http.Client{Timeout: 10 * time.Second, Transport: zimImportTransport{ctx, http.DefaultTransport}}
	if rootCmd.PersistentFlags().Changed("client-timeout") {
		seconds, _ := rootCmd.PersistentFlags().GetInt("client-timeout")
		httpClient.Timeout = time.Duration(seconds) * time.Second
	}
	clientOpts = append(clientOpts, client.WithHTTPClient(httpClient))
	skip, _ := cmd.Flags().GetBool("skip-existing")
	buffer, err := newServiceImportBuffer("kiwix", newClient(clientOpts...), nil, nil, serviceImportOptions{
		BatchSize: batchSize, SkipExisting: skip, StartDate: dates.From, EndDate: dates.To, Label: newDocumentLabelOverride(cmd),
	})
	if err != nil {
		return stats, err
	}
	err = importZIMArchive(ctx, cmd, archive, buffer, resolver, maxSize)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return buffer.stats, err
}

func importZIMArchive(ctx context.Context, cmd *cobra.Command, reader *zim.Archive, buffer *serviceImportBuffer, resolver *zimURLResolver, maxSize int64) error {
	metadata := make(map[string]string)
	for _, name := range []string{"Name", "Date"} {
		if err := ctx.Err(); err != nil {
			return err
		}
		entry, err := reader.EntryByPath("M/" + name)
		if errors.Is(err, zim.ErrNotFound) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read ZIM metadata %s: %w", name, err)
		}
		value, err := readZIMText(entry, 64<<10)
		if err != nil {
			return fmt.Errorf("read ZIM metadata %s: %w", name, err)
		}
		metadata[name] = value
	}
	collection := strings.TrimSpace(metadata["Name"])
	added := int64(0)
	if date, err := time.Parse("2006-01-02", metadata["Date"]); err == nil {
		added = date.Unix()
	}
	defer func() {
		if ctx.Err() == nil {
			buffer.Flush()
		}
	}()
	namespace := byte('C')
	if reader.MajorVersion() < 6 || reader.MinorVersion() == 0 {
		namespace = 'A'
		resolver.prefix = "A/"
	}
	reportError := func(path string, err error) error {
		buffer.stats.Errors++
		_, writeErr := fmt.Fprintf(cmd.ErrOrStderr(), "ZIM entry %q: %v\n", path, err)
		return writeErr
	}
	// Discover source URLs before submitting pages so forward links and
	// redirects can use their targets' URLs without guessing website paths.
	var articles []uint32
	for index := uint32(0); index < reader.EntryCount(); index++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		item, err := reader.EntryByIndex(index)
		if err != nil {
			return fmt.Errorf("read ZIM directory entry %d: %w", index, err)
		}
		if item.Namespace() != namespace {
			buffer.stats.Skipped++
			continue
		}
		if item.IsRedirect() {
			if target, err := item.RedirectTarget(); err == nil && target.Namespace() == namespace {
				resolver.redirects[item.Path()] = target.Path()
			}
			buffer.stats.Skipped++
			continue
		}
		mime, _, _ := strings.Cut(item.MIMEType(), ";")
		mime = strings.ToLower(strings.TrimSpace(mime))
		if mime != "text/html" && mime != "application/xhtml+xml" && mime != "text/plain" {
			buffer.stats.Skipped++
			continue
		}
		entry, err := readZIMArticle(item, maxSize)
		if err != nil {
			if err := reportError(item.Path(), err); err != nil {
				return err
			}
			continue
		}
		original := resolver.resolve(entry)
		if original == "" {
			buffer.stats.Skipped++
			if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "Warning: Skipping ZIM entry %q: original URL could not be identified. Supply --base-url if archive paths match the original website.\n", entry.Path); err != nil {
				return err
			}
			continue
		}
		resolver.pages[entry.Path] = original
		articles = append(articles, index)
	}
	for _, index := range articles {
		if err := ctx.Err(); err != nil {
			return err
		}
		item, err := reader.EntryByIndex(index)
		if err != nil {
			return fmt.Errorf("read ZIM directory entry %d: %w", index, err)
		}
		entry, err := readZIMArticle(item, maxSize)
		if err != nil {
			if err := reportError(item.Path(), err); err != nil {
				return err
			}
			continue
		}
		pageURL := resolver.pages[entry.Path]
		d := &document.Document{URL: pageURL, Title: entry.Title, Added: added, Metadata: map[string]any{"source": "kiwix"}}
		if collection != "" {
			d.Metadata["zim_collection"] = collection
		}
		if entry.MIME == "text/plain" {
			d.Text = entry.Content
		} else {
			d.HTML, err = resolver.rewriteLinks(entry, pageURL)
			if err != nil {
				if err := reportError(entry.Path, err); err != nil {
					return err
				}
				continue
			}
		}
		buffer.Add(ctx, d, nil)
	}
	return nil
}

func readZIMArticle(item zim.Entry, maxSize int64) (zimRecord, error) {
	mime, _, _ := strings.Cut(item.MIMEType(), ";")
	entry := zimRecord{Path: item.Path(), Title: item.Title(), MIME: strings.ToLower(strings.TrimSpace(mime))}
	var err error
	entry.Content, err = readZIMText(item, maxSize)
	if err == nil && strings.TrimSpace(entry.Content) == "" {
		err = errors.New("entry contains no text or HTML")
	}
	return entry, err
}

func readZIMText(entry zim.Entry, maxSize int64) (string, error) {
	if entry.IsRedirect() {
		return "", errors.New("expected content, got a redirect")
	}
	data, err := entry.ReadContent()
	if err != nil {
		return "", err
	}
	if int64(len(data)) > maxSize {
		return "", fmt.Errorf("entry exceeds size limit of %d bytes (indexer.max_file_size_mb for articles)", maxSize)
	}
	if !utf8.Valid(data) {
		return "", errors.New("entry is not valid UTF-8")
	}
	return string(data), nil
}

// The reader assumes valid header and directory offsets. Check their bounds
// before opening an archive so truncated or corrupt files return errors.
func validateZIMArchive(ctx context.Context, filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("ZIM input must be a regular file")
	}
	var header [80]byte
	if _, err := io.ReadFull(f, header[:]); err != nil {
		return err
	}
	le := binary.LittleEndian
	if le.Uint32(header[:4]) != 0x044d495a {
		return errors.New("invalid ZIM signature")
	}
	size := uint64(info.Size())
	validSpan := func(offset, length uint64) bool {
		return offset >= uint64(len(header)) && offset <= size && length <= size-offset
	}
	entries := uint64(le.Uint32(header[24:28]))
	clusters := uint64(le.Uint32(header[28:32]))
	urlPos := le.Uint64(header[32:40])
	titlePos := le.Uint64(header[40:48])
	clusterPos := le.Uint64(header[48:56])
	mimePos := le.Uint64(header[56:64])
	checksumPos := le.Uint64(header[72:80])
	if !validSpan(mimePos, 1) || !validSpan(checksumPos, 16) ||
		!validSpan(urlPos, entries*8) || !validSpan(clusterPos, clusters*8) ||
		(titlePos != ^uint64(0) && !validSpan(titlePos, entries*4)) {
		return errors.New("ZIM header points outside the file; split archives must be combined first")
	}
	for _, table := range []struct {
		name       string
		pos, count uint64
	}{{"directory", urlPos, entries}, {"cluster", clusterPos, clusters}} {
		input := bufio.NewReader(io.NewSectionReader(f, int64(table.pos), int64(table.count*8)))
		var previous uint64
		for index := uint64(0); index < table.count; index++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			var data [8]byte
			if _, err := io.ReadFull(input, data[:]); err != nil {
				return err
			}
			offset := le.Uint64(data[:])
			if !validSpan(offset, 16) || offset >= checksumPos {
				return fmt.Errorf("ZIM %s pointer %d is outside the file", table.name, index)
			}
			if table.name == "cluster" && index > 0 && offset <= previous {
				return errors.New("ZIM cluster pointers are not increasing")
			}
			previous = offset
		}
	}
	return nil
}

func validZIMWebURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil
}

func addZIMImportFlags(cmd *cobra.Command) {
	addCommonImportFlags(cmd)
	cmd.Flags().String("base-url", "", "Fallback original website base URL for archive entry paths")
	cmd.Flags().Bool("allow-sensitive", false, "Skip sensitive content checks")
}

func init() {
	addZIMImportFlags(importZIMCmd)
}
