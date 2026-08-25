package document

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/asciimoo/hister/files"

	"github.com/rs/zerolog/log"
)

type Document struct {
	DocumentID string         `json:"id,omitempty"`
	URL        string         `json:"url"`
	Domain     string         `json:"domain"`
	HTML       string         `json:"html"`
	HTMLKey    string         `json:"html_key"`
	Title      string         `json:"title"`
	Text       string         `json:"text"`
	Favicon    string         `json:"favicon"`
	FaviconKey string         `json:"favicon_key"`
	Score      float64        `json:"score"`
	Added      int64          `json:"added"`
	Updated    int64          `json:"updated"`
	Type       DocType        `json:"type"`
	Language   string         `json:"language"`
	UserID     uint           `json:"user_id"`
	Label      string         `json:"label"`
	AddCount   uint           `json:"add_count"`
	Metadata   map[string]any `json:"metadata"`
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"last_modified,omitempty"`
	// ExtraDocuments can be populated by extractors to create new documents during the extraction
	ExtraDocuments []*Document `json:"-"`
	// SkipIndexing can be set by extractors to mark the document to exclude from indexing. Useful when populating ExtraDocuments
	SkipIndexing       bool `json:"-"`
	SkipSensitiveCheck bool `json:"skip_sensitive_check"`
	Processed          bool `json:"processed"`
	faviconURL         string
}

var (
	ErrSensitiveContent = errors.New("document contains sensitive data")
	sensitiveContentRe  *regexp.Regexp
)

// ErrReadFile is the sentinel error for file read failures.
var ErrReadFile = errors.New("cannot read file")

// ReadFileError wraps a file read failure with a message.
type ReadFileError struct {
	Msg string
}

func (e *ReadFileError) Unwrap() error {
	return ErrReadFile
}

func (e *ReadFileError) Error() string {
	return fmt.Sprintf("%s: %s", ErrReadFile.Error(), e.Msg)
}

// SetSensitiveContentPattern sets the regexp used to detect sensitive content.
// Process uses this pattern when no pattern is supplied explicitly.
func SetSensitiveContentPattern(re *regexp.Regexp) {
	sensitiveContentRe = re
}

func (d *Document) DownloadFavicon(userAgent string) error {
	if d.faviconURL == "" {
		d.faviconURL = fullURL(d.URL, "/favicon.ico")
	}
	if strings.HasPrefix(d.faviconURL, "data:") {
		d.Favicon = d.faviconURL
		return nil
	}
	cli := &http.Client{
		Timeout: 10 * time.Second,
	}
	req, err := http.NewRequest("GET", d.faviconURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := cli.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Warn().Err(cerr).Msg("failed to close favicon response body")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("invalid status code (%d)", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	d.Favicon = fmt.Sprintf("data:%s;base64,%s", resp.Header.Get("Content-Type"), base64.StdEncoding.EncodeToString(data))
	return nil
}

func (d *Document) Process(ld LanguageDetector, extractFn func(*Document) error) error {
	return d.ProcessWithSensitivePattern(ld, extractFn, sensitiveContentRe)
}

// ProcessWithSensitivePattern processes the document using the provided
// sensitive content pattern. It allows callers with instance scoped
// configuration to avoid relying on the package default pattern.
func (d *Document) ProcessWithSensitivePattern(ld LanguageDetector, extractFn func(*Document) error, sensitivePattern *regexp.Regexp) error {
	var contextExtractFn func(context.Context, *Document) error
	if extractFn != nil {
		contextExtractFn = func(_ context.Context, document *Document) error {
			return extractFn(document)
		}
	}
	return d.ProcessWithSensitivePatternContext(context.Background(), ld, contextExtractFn, sensitivePattern)
}

// ProcessContext processes the document and propagates cancellation to its
// extractor.
func (d *Document) ProcessContext(ctx context.Context, ld LanguageDetector, extractFn func(context.Context, *Document) error) error {
	return d.ProcessWithSensitivePatternContext(ctx, ld, extractFn, sensitiveContentRe)
}

// ProcessWithSensitivePatternContext processes the document with caller
// cancellation and an explicit sensitive content pattern.
func (d *Document) ProcessWithSensitivePatternContext(ctx context.Context, ld LanguageDetector, extractFn func(context.Context, *Document) error, sensitivePattern *regexp.Regexp) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.Processed {
		return nil
	}
	if ld == nil {
		ld = NewNullLanguageDetector()
	}
	if !d.SkipSensitiveCheck && sensitivePattern != nil && sensitivePattern.MatchString(d.HTML) {
		log.Debug().Msg("Sensitive content detected")
		return ErrSensitiveContent
	}
	if d.URL == "" {
		return errors.New("missing URL")
	}
	pu, err := url.Parse(d.URL)
	if err != nil {
		return err
	}
	if d.Type == RemoteFile {
		return d.processRemoteFile(pu, ld, sensitivePattern)
	}
	if pu.Scheme == "file" && d.HTML == "" {
		return d.processFile(ld, sensitivePattern)
	}
	if pu.Scheme != "file" && (pu.Scheme == "" || pu.Host == "") {
		return errors.New("invalid URL: missing scheme/host")
	}
	if pu.Scheme == "file" {
		d.Domain = "local"
	} else {
		d.normalizeWebURL(pu)
		d.Domain = pu.Hostname()
	}
	now := time.Now().Unix()
	if d.Added == 0 {
		d.Added = now
	}
	d.Updated = now
	d.Type = Web
	if d.HTML != "" {
		if err := extractFn(ctx, d); err != nil {
			return err
		}
	}
	d.finalizeDocument(ld)
	return nil
}

func (d *Document) processRemoteFile(pu *url.URL, ld LanguageDetector, sensitivePattern *regexp.Regexp) error {
	if pu.Scheme != "remote-file" || pu.Host == "" {
		return errors.New("invalid remote file URL: expected remote-file scheme and source host")
	}
	if !d.SkipSensitiveCheck && sensitivePattern != nil && sensitivePattern.MatchString(d.Text) {
		return ErrSensitiveContent
	}

	d.Domain = pu.Hostname()
	if d.Title == "" {
		base := path.Base(pu.Path)
		parent := path.Base(path.Dir(pu.Path))
		if parent == "." || parent == "/" {
			d.Title = base
		} else {
			d.Title = parent + "/" + base
		}
	}
	now := time.Now().Unix()
	if d.Added == 0 {
		d.Added = now
	}
	if d.Updated == 0 {
		d.Updated = now
	}
	d.finalizeDocument(ld)
	return nil
}

// normalizeWebURL strips the URL fragment and removes UTM tracking parameters.
func (d *Document) normalizeWebURL(pu *url.URL) {
	if pu.Fragment != "" {
		pu.Fragment = ""
		d.URL = pu.String()
	}
	q := pu.Query()
	changed := false
	for k := range q {
		if k == "utm" || strings.HasPrefix(k, "utm_") {
			changed = true
			q.Del(k)
		}
	}
	if changed {
		pu.RawQuery = q.Encode()
		d.URL = pu.String()
	}
}

func (d *Document) processFile(ld LanguageDetector, sensitivePattern *regexp.Regexp) error {
	osPath := files.FileURLToPath(d.URL)
	if d.Text == "" {
		content, err := os.ReadFile(osPath)
		if err != nil {
			return &ReadFileError{
				Msg: err.Error(),
			}
		}
		if !utf8.Valid(content) {
			return errors.New("binary file")
		}
		d.Text = string(content)
	}
	if !d.SkipSensitiveCheck && sensitivePattern != nil && sensitivePattern.MatchString(d.Text) {
		return ErrSensitiveContent
	}
	d.Type = Local
	d.Domain = "local"
	if d.Title == "" {
		base := filepath.Base(osPath)
		parent := filepath.Base(filepath.Dir(osPath))
		if parent == "." || parent == "/" {
			d.Title = base
		} else {
			d.Title = parent + "/" + base
		}
	}
	now := time.Now().Unix()
	if d.Added == 0 {
		d.Added = now
	}
	if d.Updated == 0 {
		d.Updated = now
	}
	d.finalizeDocument(ld)
	return nil
}

// finalizeDocument sets the document language and marks it as processed.
func (d *Document) finalizeDocument(ld LanguageDetector) {
	d.Language = ld.DetectLanguage(d.Text)
	d.Processed = true
}

// SetSkipSensitiveCheck controls whether sensitive content checks are skipped
// during processing (e.g. during reindex with skipSensitiveChecks=true).
func (d *Document) SetSkipSensitiveCheck(v bool) {
	d.SkipSensitiveCheck = v
}

// IsProcessed reports whether the document has already been processed.
func (d *Document) IsProcessed() bool {
	return d.Processed
}

// SetFaviconURL sets the favicon URL discovered during extraction.
func (d *Document) SetFaviconURL(u string) {
	d.faviconURL = u
}

// CopyFaviconFrom copies favicon data or a discovered favicon URL when the
// destination document does not already have a favicon source.
func (d *Document) CopyFaviconFrom(source *Document) {
	if source == nil || d.Favicon != "" || d.faviconURL != "" {
		return
	}
	d.Favicon = source.Favicon
	d.faviconURL = source.faviconURL
}

func (d *Document) ID() string {
	return GetDocID(d.UserID, d.URL)
}

func (d *Document) AddMetadata(k string, v any) {
	if d.Metadata == nil {
		d.Metadata = map[string]any{
			k: v,
		}
	} else {
		d.Metadata[k] = v
	}
}

// GetPreviewMeta returns the document's normalized metadata (merged
// readability + JSON-LD output) for the preview UI. Returns nil when
// there is nothing to surface.
func (d *Document) GetPreviewMeta() map[string]any {
	meta := map[string]any{}
	for _, k := range []string{
		"type", "headline", "description", "author",
		"published", "modified", "image", "site_name", "language",
	} {
		if v, ok := d.Metadata[k].(string); ok && v != "" {
			meta[k] = v
		}
	}
	if raw, ok := d.Metadata["jsonld"].(string); ok && raw != "" {
		var nodes []map[string]any
		if err := json.Unmarshal([]byte(raw), &nodes); err == nil && len(nodes) > 0 {
			meta["jsonld"] = nodes
		}
	}
	if raw, ok := d.Metadata["videos"].(string); ok && raw != "" {
		var vids []map[string]any
		if err := json.Unmarshal([]byte(raw), &vids); err == nil && len(vids) > 0 {
			meta["videos"] = vids
		}
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
}

func GetDocID(uid uint, url string) string {
	if uid != 0 {
		return fmt.Sprintf("%d:%s", uid, url)
	}
	return url
}

func fullURL(base, u string) string {
	if strings.HasPrefix(u, "data:") {
		return u
	}
	pu, err := url.Parse(u)
	if err != nil {
		return ""
	}
	pb, err := url.Parse(base)
	if err != nil {
		return ""
	}
	return pb.ResolveReference(pu).String()
}
