// Package ytdlp provides an extractor for video pages using the yt-dlp tool.
package ytdlp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/extractor/sdk"
)

const (
	cacheTTL                 = 10 * time.Minute
	maxCacheSize             = 500
	maxThumbSize             = 5 << 20 // 5 MB
	defaultMaxConcurrentJobs = 2
)

// YtdlpExtractor extracts video metadata using yt-dlp.
type YtdlpExtractor struct {
	cfg      *sdk.Config
	cache    sync.Map // URL -> *cachedInfo
	jobSlots chan struct{}
}

func (e *YtdlpExtractor) Name() string {
	return "Ytdlp"
}

// Description returns a short summary of what this extractor does.
func (e *YtdlpExtractor) Description() string {
	return "Extracts video metadata (title, description, chapters, subtitles, thumbnail) from video hosting sites using the yt-dlp tool."
}

func (e *YtdlpExtractor) Capabilities() sdk.Capabilities {
	return sdk.Capabilities{Extract: true, Preview: true}
}

func (e *YtdlpExtractor) GetConfig() *sdk.Config {
	if e.cfg == nil {
		return &sdk.Config{
			Enable: false,
			Options: map[string]any{
				"binary":               "yt-dlp",
				"timeout":              15,
				"max_concurrent_jobs":  defaultMaxConcurrentJobs,
				"fetch_subtitles":      false,
				"sub_language":         "auto",
				"extra_domains":        [0]string{},
				"cookies_file":         "",
				"cookies_from_browser": "",
			},
		}
	}
	return e.cfg
}

// OptionMetadata is independent of applied configuration and credentials.
func (e *YtdlpExtractor) OptionMetadata() []sdk.OptionMetadata {
	zero, one, jobs, timeout := 0, 1, 1024, 86400
	text := config.SettingSchema{Type: "string", Control: "text", Collection: "none", Sensitivity: "none", MaxLength: 4096}
	secret := config.SettingSchema{Type: "string", Control: "secret", Collection: "none", Sensitivity: "secret", MaxLength: 4096}
	return []sdk.OptionMetadata{
		{Key: "timeout", Label: "Timeout (seconds)", Description: "Maximum time for a video metadata request.", Default: 15, Effect: "runtime", Schema: config.SettingSchema{Type: "integer", Control: "number", Collection: "none", Sensitivity: "none", Min: &one, Max: &timeout}},
		{Key: "max_concurrent_jobs", Label: "Concurrent video requests", Description: "Maximum simultaneous video requests. Zero allows unlimited requests.", Default: defaultMaxConcurrentJobs, Effect: "runtime", Schema: config.SettingSchema{Type: "integer", Control: "number", Collection: "none", Sensitivity: "none", Min: &zero, Max: &jobs}},
		{Key: "fetch_subtitles", Label: "Fetch subtitles", Description: "Include available subtitles when fetching videos.", Default: false, Effect: "runtime", Schema: config.SettingSchema{Type: "boolean", Control: "switch", Collection: "none", Sensitivity: "none"}},
		{Key: "sub_language", Label: "Subtitle language", Description: "Subtitle language requested from yt-dlp, such as en or auto.", Default: "auto", Effect: "runtime", Schema: config.SettingSchema{Type: "string", Control: "text", Collection: "none", Sensitivity: "none", MinLength: 1, MaxLength: 128}},
		{Key: "extra_domains", Label: "Additional video domains", Description: "Additional host names to process with yt-dlp. Subdomains are included.", Default: []string{}, Effect: "runtime", Schema: config.SettingSchema{Type: "list", Control: "list", Collection: "replace", Sensitivity: "none", MaxItems: 256, Items: &text}},
		{Key: "cookies_file", Label: "Cookies file", Description: "Path to a cookies file on the server used for video authentication.", Default: "", Effect: "runtime", Schema: secret},
		{Key: "cookies_from_browser", Label: "Browser cookies", Description: "Browser cookie source accepted by yt-dlp, such as firefox or chrome.", Default: "", Effect: "runtime", Schema: secret},
		{Key: "binary", Label: "Executable", Description: "yt-dlp executable selected in the configuration file.", Default: "yt-dlp", Effect: "runtime", Schema: secret, ReadOnlyReason: "Executable selections are file managed."},
		{Key: "extra_args", Label: "Extra arguments", Description: "Additional yt-dlp arguments selected in the configuration file.", Default: []string{}, Effect: "runtime", Schema: config.SettingSchema{Type: "list", Control: "list", Collection: "replace", Sensitivity: "mixed", Items: &secret}, ReadOnlyReason: "Arbitrary executable arguments are file managed."},
	}
}

func (e *YtdlpExtractor) SetConfig(c *sdk.Config) error {
	for k := range c.Options {
		switch k {
		case "binary", "timeout", "max_concurrent_jobs", "fetch_subtitles", "sub_language",
			"cookies_file", "cookies_from_browser", "extra_args", "extra_domains":
		default:
			return fmt.Errorf("unknown option %q", k)
		}
	}
	for _, option := range e.OptionMetadata() {
		value, present := c.Options[option.Key]
		if !present {
			continue
		}
		normalized, message := option.Schema.ValidateValue(value)
		if message != "" {
			return config.FieldError{Path: "extractors.ytdlp.options." + option.Key, Code: "invalid_value", Message: message}
		}
		if option.Key == "extra_domains" {
			for _, item := range normalized.([]any) {
				domain := item.(string)
				u, err := url.Parse("https://" + domain)
				if err != nil || domain == "" || u.Hostname() != domain || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(domain, "\\ \t\r\n") {
					return config.FieldError{Path: "extractors.ytdlp.options.extra_domains", Code: "invalid_value", Message: "Use host names without a scheme, port, or path."}
				}
			}
		}
	}
	maxJobs, err := maxConcurrentJobs(c.Options)
	if err != nil {
		return err
	}
	var jobSlots chan struct{}
	if maxJobs > 0 {
		jobSlots = make(chan struct{}, maxJobs)
	}
	e.cfg = c
	e.jobSlots = jobSlots
	return nil
}

func maxConcurrentJobs(options map[string]any) (int, error) {
	value, ok := options["max_concurrent_jobs"]
	if !ok {
		return defaultMaxConcurrentJobs, nil
	}
	var jobs int
	switch value := value.(type) {
	case int:
		jobs = value
	case float64:
		jobs = int(value)
		if float64(jobs) != value {
			return 0, errors.New("max_concurrent_jobs must be an integer")
		}
	default:
		return 0, errors.New("max_concurrent_jobs must be an integer")
	}
	if jobs < 0 {
		return 0, errors.New("max_concurrent_jobs must not be negative")
	}
	return jobs, nil
}

func (e *YtdlpExtractor) runJob(ctx context.Context, job func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	jobSlots := e.jobSlots
	if jobSlots == nil {
		return job()
	}
	select {
	case jobSlots <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-jobSlots }()
	return job()
}

func (e *YtdlpExtractor) binary() string {
	if b, ok := e.GetConfig().Options["binary"].(string); ok && b != "" {
		return b
	}
	return "yt-dlp"
}

func (e *YtdlpExtractor) timeout() time.Duration {
	cfg := e.GetConfig()
	switch v := cfg.Options["timeout"].(type) {
	case int:
		return time.Duration(v) * time.Second
	case float64:
		return time.Duration(v) * time.Second
	}
	return 15 * time.Second
}

func (e *YtdlpExtractor) fetchSubtitlesEnabled() bool {
	v, ok := e.GetConfig().Options["fetch_subtitles"].(bool)
	return ok && v
}

func (e *YtdlpExtractor) subLanguage() string {
	if s, ok := e.GetConfig().Options["sub_language"].(string); ok && s != "" {
		return s
	}
	return "en"
}

// cookieArgs returns yt-dlp CLI flags for cookie authentication based on config.
func (e *YtdlpExtractor) cookieArgs() []string {
	var args []string
	if f, ok := e.GetConfig().Options["cookies_file"].(string); ok && f != "" {
		args = append(args, "--cookies", f)
	}
	if b, ok := e.GetConfig().Options["cookies_from_browser"].(string); ok && b != "" {
		args = append(args, "--cookies-from-browser", b)
	}
	return args
}

func (e *YtdlpExtractor) Match(d *sdk.Document) bool {
	u, err := url.Parse(d.URL)
	if err != nil {
		return false
	}

	var extraDomains []string
	if l, ok := e.GetConfig().Options["extra_domains"].([]any); ok && len(l) > 0 {
		for _, v := range l {
			if s, ok := v.(string); ok {
				extraDomains = append(extraDomains, strings.ToLower(s))
			}
		}
	}

	host := strings.ToLower(u.Hostname())
	matched := false
	for _, domain := range append(knownDomains, extraDomains...) {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			matched = true
			break
		}
	}

	if !matched {
		for _, sub := range knownHostSubstrings {
			if strings.Contains(host, sub) {
				matched = true
				break
			}
		}
	}
	if !matched {
		return false
	}
	// Reject bare homepages — yt-dlp cannot extract anything from a site root
	// with no path or query parameters (e.g. "https://www.youtube.com/").
	path := strings.TrimRight(u.Path, "/")
	if path == "" && u.RawQuery == "" {
		return false
	}
	return true
}

// getInfo returns cached videoInfo or fetches it via yt-dlp.
func (e *YtdlpExtractor) getInfo(ctx context.Context, videoURL string) (*videoInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cached, ok := e.cache.Load(videoURL); ok {
		ci := cached.(*cachedInfo)
		if time.Since(ci.fetchedAt) < cacheTTL {
			return ci.info, nil
		}
		e.cache.Delete(videoURL)
	}

	info, err := e.fetchInfo(ctx, videoURL)
	if err != nil {
		return nil, err
	}

	e.pruneCache()
	e.cache.Store(videoURL, &cachedInfo{info: info, fetchedAt: time.Now()})
	return info, nil
}

// pruneCache evicts expired entries and, if still over maxCacheSize, evicts the oldest.
func (e *YtdlpExtractor) pruneCache() {
	var count int
	var oldestKey string
	var oldestTime time.Time

	e.cache.Range(func(key, value any) bool {
		ci := value.(*cachedInfo)
		if time.Since(ci.fetchedAt) >= cacheTTL {
			e.cache.Delete(key)
			return true
		}
		count++
		if oldestKey == "" || ci.fetchedAt.Before(oldestTime) {
			oldestKey = key.(string)
			oldestTime = ci.fetchedAt
		}
		return true
	})

	if count >= maxCacheSize && oldestKey != "" {
		e.cache.Delete(oldestKey)
	}
}

func (e *YtdlpExtractor) fetchInfo(ctx context.Context, videoURL string) (*videoInfo, error) {
	args := []string{
		"--dump-json",
		"--no-download",
		"--no-playlist",
		"--no-warnings",
	}
	args = append(args, e.cookieArgs()...)
	if extra, ok := e.GetConfig().Options["extra_args"].([]any); ok {
		for _, a := range extra {
			if s, ok := a.(string); ok {
				args = append(args, s)
			}
		}
	}
	args = append(args, videoURL)

	var out []byte
	var stderr bytes.Buffer
	err := e.runJob(ctx, func() error {
		ctx, cancel := context.WithTimeout(ctx, e.timeout())
		defer cancel()

		// #nosec G204 -- binary path and args are admin-configured, not user input.
		cmd := exec.CommandContext(ctx, e.binary(), args...)
		cmd.Stderr = &stderr
		log.Debug().Str("args", strings.Join(cmd.Args, " ")).Msg("yt-dlp executing")
		var err error
		out, err = cmd.Output()
		return err
	})
	log.Debug().Str("stdout", strings.TrimSpace(string(out))).Str("stderr", strings.TrimSpace(stderr.String())).Msg("yt-dlp output")
	if err != nil {
		if s := stderr.String(); s != "" {
			return nil, fmt.Errorf("yt-dlp failed: %w: %s", err, strings.TrimSpace(s))
		}
		return nil, fmt.Errorf("yt-dlp failed: %w", err)
	}
	var info videoInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return nil, fmt.Errorf("failed to parse yt-dlp output: %w", err)
	}
	return &info, nil
}

// downloadThumbnail fetches the thumbnail image and returns it as a base64 data URI.
func (e *YtdlpExtractor) downloadThumbnail(ctx context.Context, thumbnailURL string) string {
	if thumbnailURL == "" {
		return ""
	}

	ctx, cancel := context.WithTimeout(ctx, e.timeout())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, thumbnailURL, nil)
	if err != nil {
		return ""
	}
	cli := &http.Client{Timeout: e.timeout()}
	resp, err := cli.Do(req)
	if err != nil {
		return ""
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Warn().Err(cerr).Msg("failed to close thumbnail response body")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxThumbSize+1))
	if err != nil || len(data) > maxThumbSize {
		return ""
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}
	return fmt.Sprintf("data:%s;base64,%s", contentType, base64.StdEncoding.EncodeToString(data))
}

func (e *YtdlpExtractor) Extract(d *sdk.Document) sdk.ExtractResult {
	return e.ExtractContext(context.Background(), d)
}

// ExtractContext extracts video metadata and cancels blocking work when ctx
// is canceled.
func (e *YtdlpExtractor) ExtractContext(ctx context.Context, d *sdk.Document) sdk.ExtractResult {
	info, err := e.getInfo(ctx, d.URL)
	if err != nil {
		if ctx.Err() != nil {
			return sdk.AbortExtraction(ctx.Err())
		}
		return sdk.ExtractFallback(err)
	}

	if info.Title != "" {
		d.Title = info.Title
	}

	var text strings.Builder
	if info.Description != "" {
		text.WriteString(info.Description)
	}
	if info.Uploader != "" {
		text.WriteString("\n\nUploader: ")
		text.WriteString(info.Uploader)
	}
	if len(info.Tags) > 0 {
		text.WriteString("\nTags: ")
		text.WriteString(strings.Join(info.Tags, ", "))
	}
	if len(info.Categories) > 0 {
		text.WriteString("\nCategories: ")
		text.WriteString(strings.Join(info.Categories, ", "))
	}
	if len(info.Chapters) > 0 {
		text.WriteString("\n\nChapters:\n")
		for _, ch := range info.Chapters {
			fmt.Fprintf(&text, "  %s %s\n", formatDuration(ch.StartTime), ch.Title)
		}
	}
	if info.PlaylistTitle != "" {
		text.WriteString("\nPlaylist: ")
		text.WriteString(info.PlaylistTitle)
		if info.PlaylistIndex != nil && info.PlaylistCount != nil {
			fmt.Fprintf(&text, " (%d/%d)", *info.PlaylistIndex, *info.PlaylistCount)
		}
	}

	if e.fetchSubtitlesEnabled() {
		if transcript := e.fetchSubtitleText(ctx, info); transcript != "" {
			text.WriteString("\n\nTranscript:\n")
			text.WriteString(transcript)
		}
	}

	d.Text = strings.TrimSpace(text.String())

	// Store structured metadata.
	if d.Metadata == nil {
		d.Metadata = make(map[string]any)
	}
	if thumb := e.downloadThumbnail(ctx, info.Thumbnail); thumb != "" {
		d.Metadata["thumbnail"] = thumb
	}
	if info.Duration > 0 {
		d.Metadata["duration"] = info.Duration
	}
	if info.UploadDate != "" {
		d.Metadata["upload_date"] = formatDate(info.UploadDate)
	}
	if info.Uploader != "" {
		d.Metadata["uploader"] = info.Uploader
	}
	if info.ViewCount > 0 {
		d.Metadata["view_count"] = info.ViewCount
	}
	if err := ctx.Err(); err != nil {
		return sdk.AbortExtraction(err)
	}

	return sdk.Extracted()
}

func (e *YtdlpExtractor) Preview(d *sdk.Document) sdk.PreviewResult {
	return e.PreviewContext(context.Background(), d)
}

// PreviewContext renders video metadata and cancels blocking work when ctx is
// canceled.
func (e *YtdlpExtractor) PreviewContext(ctx context.Context, d *sdk.Document) sdk.PreviewResult {
	info, err := e.getInfo(ctx, d.URL)
	if err != nil {
		if ctx.Err() != nil {
			return sdk.AbortPreview(ctx.Err())
		}
		return sdk.PreviewFallback(err)
	}

	// Use cached thumbnail from metadata, or download it.
	thumbDataURI := ""
	if d.Metadata != nil {
		if t, ok := d.Metadata["thumbnail"].(string); ok {
			thumbDataURI = t
		}
	}
	if thumbDataURI == "" {
		thumbDataURI = e.downloadThumbnail(ctx, info.Thumbnail)
	}

	// Build structured preview data for the frontend.
	preview := videoPreviewData{
		Title:             info.Title,
		Uploader:          info.Uploader,
		Duration:          info.Duration,
		DurationFormatted: formatDuration(info.Duration),
		UploadDate:        formatDate(info.UploadDate),
		ViewCount:         info.ViewCount,
		LikeCount:         info.LikeCount,
		Categories:        info.Categories,
		Tags:              info.Tags,
		Description:       info.Description,
		Thumbnail:         thumbDataURI,
		WebpageURL:        info.WebpageURL,
	}

	if len(info.Chapters) > 0 {
		preview.Chapters = make([]chapterPreview, len(info.Chapters))
		for i, ch := range info.Chapters {
			preview.Chapters[i] = chapterPreview{
				Title:     ch.Title,
				StartTime: formatDuration(ch.StartTime),
			}
		}
	}

	if info.PlaylistTitle != "" {
		preview.Playlist = &playlistPreview{
			Title: info.PlaylistTitle,
		}
		if info.PlaylistIndex != nil {
			preview.Playlist.Index = *info.PlaylistIndex
		}
		if info.PlaylistCount != nil {
			preview.Playlist.Count = *info.PlaylistCount
		}
	}

	if e.fetchSubtitlesEnabled() {
		preview.Transcript = e.fetchSubtitleText(ctx, info)
	}
	if err := ctx.Err(); err != nil {
		return sdk.AbortPreview(err)
	}

	data, err := json.Marshal(preview)
	if err != nil {
		return sdk.PreviewFallback(err)
	}

	return sdk.Previewed(sdk.PreviewResponse{
		Content:  string(data),
		Template: "video",
	})
}
