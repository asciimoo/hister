package ytdlp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/extractor/sdk"
)

// sampleJSONTemplate is based on real yt-dlp --dump-json output from
// https://www.youtube.com/watch?v=jNQXAC9IVRw ("Me at the zoo").
// %s placeholders: 1=thumbnail URL, 2=subtitle URL.
const sampleJSONTemplate = `{
  "id": "jNQXAC9IVRw",
  "title": "Me at the zoo",
  "description": "The first video on YouTube.\n\n00:00 Intro\n00:05 The cool thing\n00:17 End",
  "uploader": "jawed",
  "channel": "jawed",
  "duration": 19,
  "view_count": 387196767,
  "like_count": 18694393,
  "upload_date": "20050424",
  "thumbnail": "%s/thumb.jpg",
  "webpage_url": "https://www.youtube.com/watch?v=jNQXAC9IVRw",
  "categories": ["Film & Animation"],
  "tags": ["me at the zoo", "jawed karim", "first youtube video"],
  "chapters": [
    {"start_time": 0.0, "title": "Intro", "end_time": 5.0},
    {"start_time": 5.0, "title": "The cool thing", "end_time": 17.0},
    {"start_time": 17.0, "title": "End", "end_time": 19}
  ],
  "subtitles": {
    "en": [
      {"ext": "vtt", "url": "%s/subs.vtt", "name": "English"}
    ]
  },
  "language": "en",
  "automatic_captions": {},
  "playlist_title": "First Videos",
  "playlist_index": 1,
  "playlist_count": 10
}`

// sampleJSONTemplateFr is like sampleJSONTemplate but for a French-language video.
// The "language" field is set to "fr" and automatic_captions include a French track
// so that downloadSubtitleForLang can find available subtitles.
// %s placeholders: 1=thumbnail URL.
const sampleJSONTemplateFr = `{
  "id": "testFrVideo",
  "title": "French Video",
  "description": "A video in French.",
  "uploader": "testuser",
  "channel": "testuser",
  "duration": 60,
  "view_count": 1000,
  "like_count": 50,
  "upload_date": "20240101",
  "thumbnail": "%s/thumb.jpg",
  "webpage_url": "https://www.youtube.com/watch?v=testFrVideo",
  "categories": [],
  "tags": [],
  "chapters": [],
  "language": "fr",
  "subtitles": {},
  "automatic_captions": {
    "fr": [{"ext": "json3", "url": "https://example.com/subs?lang=fr", "name": "French"}]
  }
}`

// sampleJSONTemplateNoLang is like sampleJSONTemplateFr but without a "language" field,
// simulating videos where yt-dlp cannot detect the original language.
// %s placeholder: thumbnail URL.
const sampleJSONTemplateNoLang = `{
  "id": "testNoLang",
  "title": "Unknown Language Video",
  "description": "No language metadata.",
  "uploader": "testuser",
  "channel": "testuser",
  "duration": 60,
  "view_count": 100,
  "like_count": 5,
  "upload_date": "20240101",
  "thumbnail": "%s/thumb.jpg",
  "webpage_url": "https://www.youtube.com/watch?v=testNoLang",
  "categories": [],
  "tags": [],
  "chapters": [],
  "subtitles": {},
  "automatic_captions": {}
}`

const sampleVTT = `WEBVTT
Kind: captions
Language: en

00:00:00.000 --> 00:00:05.000
All right, so here we are in front of the <c>elephants</c>

00:00:05.000 --> 00:00:10.000
The cool thing about these guys is that they have really

00:00:10.000 --> 00:00:17.000
really really long trunks
`

const fakeBinaryTimeoutSeconds = 30

// Use the test executable as the subprocess fixture on every platform.
func TestMain(m *testing.M) {
	if os.Getenv("HISTER_TEST_YTDLP_HELPER") != "1" {
		os.Exit(m.Run())
	}
	if err := runFakeYtdlp(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func runFakeYtdlp() error {
	args := os.Args[1:]
	if slices.Contains(args, "--dump-json") {
		if counter := os.Getenv("HISTER_TEST_YTDLP_COUNTER"); counter != "" {
			raw, err := os.ReadFile(counter)
			if err != nil {
				return err
			}
			count, err := strconv.Atoi(strings.TrimSpace(string(raw)))
			if err != nil {
				return err
			}
			if err := os.WriteFile(counter, []byte(strconv.Itoa(count+1)), 0o600); err != nil {
				return err
			}
		}
		_, err := fmt.Fprintln(os.Stdout, os.Getenv("HISTER_TEST_YTDLP_JSON"))
		return err
	}
	if slices.Contains(args, "--write-sub") || slices.Contains(args, "--write-auto-sub") {
		output, language := "", "en"
		for i := 0; i+1 < len(args); i++ {
			switch args[i] {
			case "-o":
				output = args[i+1]
			case "--sub-lang":
				language = args[i+1]
			}
		}
		if output != "" {
			return os.WriteFile(output+"."+language+".vtt", []byte(sampleVTT), 0o600)
		}
	}
	return nil
}

func writeFakeBinary(t *testing.T, jsonContent string) string {
	t.Helper()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HISTER_TEST_YTDLP_HELPER", "1")
	t.Setenv("HISTER_TEST_YTDLP_JSON", jsonContent)
	t.Setenv("HISTER_TEST_YTDLP_COUNTER", "")
	return bin
}

// startTestServer returns an httptest.Server that serves fake thumbnails and subtitles.
func startTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/subs.vtt":
			w.Header().Set("Content-Type", "text/vtt")
			_, _ = fmt.Fprint(w, sampleVTT)
		case "/thumb.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte{0xFF, 0xD8, 0xFF, 0xD9})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newTestExtractor creates an extractor with a fake binary and test HTTP server.
func newTestExtractor(t *testing.T, fetchSubs bool) (*YtdlpExtractor, *httptest.Server) {
	t.Helper()
	srv := startTestServer(t)
	jsonContent := fmt.Sprintf(sampleJSONTemplate, srv.URL, srv.URL)

	e := &YtdlpExtractor{}
	if err := e.SetConfig(&config.Extractor{
		Enable: true,
		Options: map[string]any{
			"binary":          writeFakeBinary(t, jsonContent),
			"timeout":         fakeBinaryTimeoutSeconds,
			"fetch_subtitles": fetchSubs,
			"sub_language":    "en",
		},
	}); err != nil {
		t.Fatal(err)
	}
	return e, srv
}

// writeFakeBinaryCounter creates a fake binary that counts invocations of --dump-json calls.
func writeFakeBinaryCounter(t *testing.T, jsonContent, counterFile string) string {
	t.Helper()
	bin := writeFakeBinary(t, jsonContent)
	t.Setenv("HISTER_TEST_YTDLP_COUNTER", counterFile)
	return bin
}

func TestMatch(t *testing.T) {
	e := &YtdlpExtractor{}
	tests := []struct {
		url  string
		want bool
	}{
		{"https://www.youtube.com/watch?v=abc123", true},
		{"https://youtu.be/abc123", true},
		{"https://vimeo.com/123456", true},
		{"https://www.dailymotion.com/video/x7tgad0", true},
		{"https://www.twitch.tv/videos/123456", true},
		{"https://soundcloud.com/artist/track", true},
		{"https://artist.bandcamp.com/track/song", true},
		{"https://www.tiktok.com/@user/video/123", true},
		{"https://www.ted.com/talks/some_talk", true},
		{"https://archive.org/details/something", false},
		{"https://videos.peertube.example.com/w/abc", true},
		{"https://www.youtube.com/", false},
		{"https://www.youtube.com", false},
		{"https://youtube.com/", false},
		{"https://vimeo.com/", false},
		{"https://tiktok.com", false},
		{"https://example.com/page", false},
		{"https://stackoverflow.com/questions/123", false},
		{"https://example.com/path/youtube.com/fake", false},
		{"https://nebula.tv/videos/some-video", true},
	}

	if err := e.SetConfig(&config.Extractor{
		Enable: true,
		Options: map[string]any{
			"extra_domains": []any{"nebula.tv"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	for _, tt := range tests {
		d := &document.Document{URL: tt.url}
		if got := e.Match(d); got != tt.want {
			t.Errorf("Match(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

func TestExtract(t *testing.T) {
	e, _ := newTestExtractor(t, false)
	d := &document.Document{URL: "https://www.youtube.com/watch?v=jNQXAC9IVRw"}

	state, err := e.Extract(d).Unpack()
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if state != sdk.ExtractorSuccess {
		t.Fatalf("Extract returned state %v, want ExtractorSuccess", state)
	}

	// Title.
	if d.Title != "Me at the zoo" {
		t.Errorf("Title = %q, want %q", d.Title, "Me at the zoo")
	}

	// Text content.
	for _, want := range []string{
		"The first video on YouTube.",
		"Uploader: jawed",
		"me at the zoo",
		"Film & Animation",
		"Chapters:",
		"0:05 The cool thing",
		"Playlist: First Videos (1/10)",
	} {
		if !strings.Contains(d.Text, want) {
			t.Errorf("Text missing %q", want)
		}
	}
	if strings.Contains(d.Text, "Transcript") {
		t.Error("Text should not contain transcript when fetch_subtitles is false")
	}

	// Metadata.
	thumb, ok := d.Metadata["thumbnail"].(string)
	if !ok || !strings.HasPrefix(thumb, "data:image/jpeg;base64,") {
		t.Error("metadata missing cached thumbnail data URI")
	}
	if dur, _ := d.Metadata["duration"].(float64); dur != 19 {
		t.Errorf("metadata duration = %v, want 19", d.Metadata["duration"])
	}
	if date, _ := d.Metadata["upload_date"].(string); date != "2005-04-24" {
		t.Errorf("metadata upload_date = %v, want 2005-04-24", d.Metadata["upload_date"])
	}
	if up, _ := d.Metadata["uploader"].(string); up != "jawed" {
		t.Errorf("metadata uploader = %v, want jawed", d.Metadata["uploader"])
	}
	if views, _ := d.Metadata["view_count"].(int64); views != 387196767 {
		t.Errorf("metadata view_count = %v, want 387196767", d.Metadata["view_count"])
	}
}

func TestExtractWithSubtitles(t *testing.T) {
	e, _ := newTestExtractor(t, true)
	d := &document.Document{URL: "https://www.youtube.com/watch?v=jNQXAC9IVRw"}

	if _, err := e.Extract(d).Unpack(); err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if !strings.Contains(d.Text, "Transcript:") {
		t.Error("Text missing transcript section")
	}
	if !strings.Contains(d.Text, "elephants") {
		t.Error("Text missing subtitle content 'elephants'")
	}
}

func TestPreview(t *testing.T) {
	e, _ := newTestExtractor(t, false)
	d := &document.Document{URL: "https://www.youtube.com/watch?v=jNQXAC9IVRw"}

	resp, state, err := e.Preview(d).Unpack()
	if err != nil {
		t.Fatalf("Preview returned error: %v", err)
	}
	if state != sdk.ExtractorSuccess {
		t.Fatalf("Preview returned state %v, want ExtractorSuccess", state)
	}
	if resp.Template != "video" {
		t.Errorf("Template = %q, want %q", resp.Template, "video")
	}

	var preview videoPreviewData
	if err := json.Unmarshal([]byte(resp.Content), &preview); err != nil {
		t.Fatalf("Preview content is not valid JSON: %v", err)
	}

	// Core fields.
	if preview.Title != "Me at the zoo" {
		t.Errorf("Title = %q, want %q", preview.Title, "Me at the zoo")
	}
	if preview.Uploader != "jawed" {
		t.Errorf("Uploader = %q, want %q", preview.Uploader, "jawed")
	}
	if preview.DurationFormatted != "0:19" {
		t.Errorf("DurationFormatted = %q, want %q", preview.DurationFormatted, "0:19")
	}
	if preview.UploadDate != "2005-04-24" {
		t.Errorf("UploadDate = %q, want %q", preview.UploadDate, "2005-04-24")
	}
	if preview.ViewCount != 387196767 {
		t.Errorf("ViewCount = %d, want 387196767", preview.ViewCount)
	}
	if preview.WebpageURL != "https://www.youtube.com/watch?v=jNQXAC9IVRw" {
		t.Errorf("WebpageURL = %q", preview.WebpageURL)
	}
	if !strings.HasPrefix(preview.Thumbnail, "data:image/jpeg;base64,") {
		t.Error("Thumbnail missing data URI")
	}

	// Chapters.
	if len(preview.Chapters) != 3 {
		t.Fatalf("expected 3 chapters, got %d", len(preview.Chapters))
	}
	if preview.Chapters[0].Title != "Intro" || preview.Chapters[0].StartTime != "0:00" {
		t.Errorf("chapter[0] = %+v, want Intro@0:00", preview.Chapters[0])
	}
	if preview.Chapters[1].Title != "The cool thing" || preview.Chapters[1].StartTime != "0:05" {
		t.Errorf("chapter[1] = %+v, want 'The cool thing'@0:05", preview.Chapters[1])
	}

	// Playlist.
	if preview.Playlist == nil {
		t.Fatal("expected playlist info, got nil")
	}
	if preview.Playlist.Title != "First Videos" || preview.Playlist.Index != 1 || preview.Playlist.Count != 10 {
		t.Errorf("playlist = %+v, want First Videos 1/10", preview.Playlist)
	}
}

func TestPreviewWithSubtitles(t *testing.T) {
	e, _ := newTestExtractor(t, true)
	d := &document.Document{URL: "https://www.youtube.com/watch?v=jNQXAC9IVRw"}

	resp, _, err := e.Preview(d).Unpack()
	if err != nil {
		t.Fatalf("Preview returned error: %v", err)
	}

	var preview videoPreviewData
	if err := json.Unmarshal([]byte(resp.Content), &preview); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !strings.Contains(preview.Transcript, "elephants") {
		t.Errorf("transcript missing 'elephants', got: %q", preview.Transcript)
	}
}

func TestDownloadThumbnailRejectsOversizedResponse(t *testing.T) {
	thumbnail := strings.Repeat("x", maxThumbSize+1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = fmt.Fprint(w, thumbnail)
	}))
	t.Cleanup(srv.Close)

	if got := (&YtdlpExtractor{}).downloadThumbnail(t.Context(), srv.URL); got != "" {
		t.Fatalf("downloadThumbnail() returned %d bytes for an oversized response", len(got))
	}
}

// newTestExtractorFr creates an extractor configured with a French-language video
// JSON and the given sub_language setting.
func newTestExtractorFr(t *testing.T, subLang string) *YtdlpExtractor {
	t.Helper()
	srv := startTestServer(t)
	jsonContent := fmt.Sprintf(sampleJSONTemplateFr, srv.URL)

	e := &YtdlpExtractor{}
	if err := e.SetConfig(&config.Extractor{
		Enable: true,
		Options: map[string]any{
			"binary":          writeFakeBinary(t, jsonContent),
			"timeout":         fakeBinaryTimeoutSeconds,
			"fetch_subtitles": true,
			"sub_language":    subLang,
		},
	}); err != nil {
		t.Fatal(err)
	}
	return e
}

// newTestExtractorNoLang creates an extractor configured with a video JSON that has
// no "language" field and the given sub_language setting.
func newTestExtractorNoLang(t *testing.T, subLang string) *YtdlpExtractor {
	t.Helper()
	srv := startTestServer(t)
	jsonContent := fmt.Sprintf(sampleJSONTemplateNoLang, srv.URL)

	e := &YtdlpExtractor{}
	if err := e.SetConfig(&config.Extractor{
		Enable: true,
		Options: map[string]any{
			"binary":          writeFakeBinary(t, jsonContent),
			"timeout":         fakeBinaryTimeoutSeconds,
			"fetch_subtitles": true,
			"sub_language":    subLang,
		},
	}); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestExtractWithSubtitlesAuto(t *testing.T) {
	e := newTestExtractorFr(t, "auto")
	d := &document.Document{URL: "https://www.youtube.com/watch?v=testFrVideo"}

	if _, err := e.Extract(d).Unpack(); err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if !strings.Contains(d.Text, "Transcript:") {
		t.Error("Text missing transcript section with auto mode")
	}
	if !strings.Contains(d.Text, "elephants") {
		t.Error("Text missing subtitle content with auto mode")
	}
}

func TestExtractWithSubtitlesMultiLangMatch(t *testing.T) {
	// "fr,en" should download subtitles because the video's original language is "fr".
	e := newTestExtractorFr(t, "fr,en")
	d := &document.Document{URL: "https://www.youtube.com/watch?v=testFrVideo"}

	if _, err := e.Extract(d).Unpack(); err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if !strings.Contains(d.Text, "elephants") {
		t.Error("Text missing subtitle content when original language matches multi-lang list")
	}
}

func TestExtractWithSubtitlesAutoNoLanguage(t *testing.T) {
	// When the video has no "language" field, auto mode should skip subtitles.
	e := newTestExtractorNoLang(t, "auto")
	d := &document.Document{URL: "https://www.youtube.com/watch?v=testNoLang"}
	if _, err := e.Extract(d).Unpack(); err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if strings.Contains(d.Text, "Transcript:") {
		t.Error("Text should not contain transcript when video has no language metadata")
	}
}

func TestExtractWithSubtitlesMultiLangNoMatch(t *testing.T) {
	// "de,es" should NOT download subtitles because the video's original language is "fr".
	e := newTestExtractorFr(t, "de,es")
	d := &document.Document{URL: "https://www.youtube.com/watch?v=testFrVideo"}

	if _, err := e.Extract(d).Unpack(); err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if strings.Contains(d.Text, "Transcript:") {
		t.Error("Text should not contain transcript when original language is not in list")
	}
}

func TestCaching(t *testing.T) {
	dir := t.TempDir()
	counterFile := filepath.Join(dir, "count")
	if err := os.WriteFile(counterFile, []byte("0"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := startTestServer(t)
	jsonContent := fmt.Sprintf(sampleJSONTemplate, srv.URL, srv.URL)

	e := &YtdlpExtractor{}
	if err := e.SetConfig(&config.Extractor{
		Enable: true,
		Options: map[string]any{
			"binary":          writeFakeBinaryCounter(t, jsonContent, counterFile),
			"timeout":         fakeBinaryTimeoutSeconds,
			"fetch_subtitles": false,
			"sub_language":    "en",
		},
	}); err != nil {
		t.Fatal(err)
	}

	url := "https://www.youtube.com/watch?v=test_cache"

	d1 := &document.Document{URL: url}
	if _, err := e.Extract(d1).Unpack(); err != nil {
		t.Fatalf("first Extract failed: %v", err)
	}

	// Second call should use cache.
	d2 := &document.Document{URL: url}
	if _, _, err := e.Preview(d2).Unpack(); err != nil {
		t.Fatalf("Preview failed: %v", err)
	}

	countBytes, err := os.ReadFile(counterFile)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.TrimSpace(string(countBytes)); count != "1" {
		t.Errorf("yt-dlp was called %s times, want 1 (caching broken)", count)
	}
}

func TestExtractFailure(t *testing.T) {
	e := &YtdlpExtractor{}
	if err := e.SetConfig(&config.Extractor{
		Enable: true,
		Options: map[string]any{
			"binary":          "/nonexistent/yt-dlp",
			"timeout":         2,
			"fetch_subtitles": false,
			"sub_language":    "en",
		},
	}); err != nil {
		t.Fatal(err)
	}

	d := &document.Document{URL: "https://www.youtube.com/watch?v=test"}
	state, err := e.Extract(d).Unpack()
	if err == nil {
		t.Fatal("expected error from missing binary")
	}
	if state != sdk.ExtractorFallback {
		t.Errorf("expected ExtractorFallback on failure, got %v", state)
	}
}

func TestConfig(t *testing.T) {
	e := &YtdlpExtractor{}

	if e.Name() != "Ytdlp" {
		t.Errorf("Name() = %q, want %q", e.Name(), "Ytdlp")
	}

	cfg := e.GetConfig()
	if cfg.Enable {
		t.Error("expected default config to have Enable=false")
	}
	if cfg.Options["binary"] != "yt-dlp" {
		t.Errorf("default binary = %v, want yt-dlp", cfg.Options["binary"])
	}
	if cfg.Options["max_concurrent_jobs"] != defaultMaxConcurrentJobs {
		t.Errorf("default max_concurrent_jobs = %v, want %d", cfg.Options["max_concurrent_jobs"], defaultMaxConcurrentJobs)
	}
	err := e.SetConfig(&config.Extractor{
		Enable: true,
		Options: map[string]any{
			"binary":              "/usr/local/bin/yt-dlp",
			"timeout":             30,
			"max_concurrent_jobs": 3,
			"fetch_subtitles":     true,
			"sub_language":        "de",
		},
	})
	if err != nil {
		t.Errorf("SetConfig with valid options failed: %v", err)
	}

	err = e.SetConfig(&config.Extractor{
		Enable:  true,
		Options: map[string]any{"unknown": "value"},
	})
	if err == nil {
		t.Error("SetConfig with unknown option should return error")
	}
}

func TestConcurrentJobLimit(t *testing.T) {
	e := &YtdlpExtractor{}
	if err := e.SetConfig(&config.Extractor{
		Enable:  true,
		Options: map[string]any{"max_concurrent_jobs": 2},
	}); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{}, 3)
	release := make(chan struct{})
	done := make(chan struct{}, 3)
	t.Cleanup(func() { close(release) })

	for range 3 {
		go func() {
			if err := e.runJob(context.Background(), func() error {
				started <- struct{}{}
				<-release
				return nil
			}); err != nil {
				t.Errorf("runJob() error = %v", err)
			}
			done <- struct{}{}
		}()
	}

	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("job did not start")
		}
	}
	select {
	case <-started:
		t.Fatal("third job started before a slot was released")
	case <-time.After(100 * time.Millisecond):
	}

	release <- struct{}{}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("queued job did not start after a slot was released")
	}

	release <- struct{}{}
	release <- struct{}{}
	for range 3 {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("job did not finish")
		}
	}
}

func TestRunJobCancelsWhileWaitingForSlot(t *testing.T) {
	e := &YtdlpExtractor{}
	if err := e.SetConfig(&config.Extractor{
		Enable:  true,
		Options: map[string]any{"max_concurrent_jobs": 1},
	}); err != nil {
		t.Fatal(err)
	}

	e.jobSlots <- struct{}{}
	defer func() { <-e.jobSlots }()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	called := false
	go func() {
		done <- e.runJob(ctx, func() error {
			called = true
			return nil
		})
	}()
	select {
	case err := <-done:
		t.Fatalf("runJob() returned before cancellation: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("runJob() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runJob() did not return after cancellation")
	}
	if called {
		t.Fatal("canceled queued job ran")
	}
}

func TestExtractRejectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := (&YtdlpExtractor{}).ExtractContext(ctx, &document.Document{URL: "https://www.youtube.com/watch?v=test"})
	if result.Decision() != sdk.ExtractorAbort {
		t.Fatalf("Extract decision = %v, want ExtractorAbort", result.Decision())
	}
	if !errors.Is(result.Err(), context.Canceled) {
		t.Fatalf("Extract error = %v, want context.Canceled", result.Err())
	}
}

func TestMaxConcurrentJobsValidation(t *testing.T) {
	for _, value := range []any{-1, 1.5, "2"} {
		e := &YtdlpExtractor{}
		err := e.SetConfig(&config.Extractor{
			Enable:  true,
			Options: map[string]any{"max_concurrent_jobs": value},
		})
		if err == nil {
			t.Errorf("SetConfig() accepted max_concurrent_jobs=%v", value)
		}
	}
}
