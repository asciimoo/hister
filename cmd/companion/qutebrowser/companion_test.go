package qutebrowser

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/asciimoo/nogon/client"
)

func TestNormalizeOptionsRejectsRemoteDevTools(t *testing.T) {
	input := DefaultOptions()
	input.DevToolsURL = "http://192.0.2.10:9222"
	_, err := normalizeOptions(input)
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("normalizeOptions error = %v, want loopback validation error", err)
	}
}

func TestBrowserWebSocketURLKeepsConfiguredHost(t *testing.T) {
	devToolsURL := mustURL(t, "http://127.0.0.1:9222")
	got, err := normalizeWebSocketURL(
		"ws://0.0.0.0:9222/devtools/browser/id",
		devToolsURL,
	)
	if err != nil {
		t.Fatal(err)
	}
	parsed := mustURL(t, got)
	if parsed.Host != devToolsURL.Host {
		t.Fatalf("WebSocket host = %q, want %q", parsed.Host, devToolsURL.Host)
	}
	if parsed.Path != "/devtools/browser/id" {
		t.Fatalf("WebSocket path = %q, want discovery path", parsed.Path)
	}
}

func TestHisterPageDetectionRespectsBasePath(t *testing.T) {
	input := DefaultOptions()
	input.HisterURL = "https://example.com/hister/"
	opts, err := normalizeOptions(input)
	if err != nil {
		t.Fatal(err)
	}
	app := newCompanion(opts, &recordingSubmitter{})
	tests := []struct {
		rawURL string
		want   bool
	}{
		{"https://example.com/hister/", true},
		{"https://example.com/hister/history", true},
		{"https://example.com/hister-other", false},
		{"https://other.example/hister/", false},
	}
	for _, test := range tests {
		if got := app.isHisterPage(mustURL(t, test.rawURL)); got != test.want {
			t.Errorf("isHisterPage(%q) = %t, want %t", test.rawURL, got, test.want)
		}
	}
}

func TestPageFingerprintTracksURLAndHTML(t *testing.T) {
	base := pageFingerprint(pageData{URL: "https://example.com", HTML: "<body>a</body>"})
	same := pageFingerprint(pageData{URL: "https://example.com", HTML: "<body>a</body>"})
	changedURL := pageFingerprint(pageData{URL: "https://example.org", HTML: "<body>a</body>"})
	changedHTML := pageFingerprint(pageData{URL: "https://example.com", HTML: "<body>b</body>"})
	if base != same {
		t.Fatal("equal pages produced different fingerprints")
	}
	if base == changedURL || base == changedHTML {
		t.Fatal("changed page produced the same fingerprint")
	}
}

func TestSameOriginUsesEffectivePorts(t *testing.T) {
	if !sameOrigin(mustURL(t, "https://example.com/a"), mustURL(t, "https://example.com:443/b")) {
		t.Fatal("default HTTPS port should equal explicit port 443")
	}
	if sameOrigin(mustURL(t, "https://example.com"), mustURL(t, "http://example.com")) {
		t.Fatal("different schemes should not have the same origin")
	}
}

func TestSubmitBuildsHisterDocument(t *testing.T) {
	opts, err := normalizeOptions(DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	submitter := &recordingSubmitter{}
	app := newCompanion(opts, submitter)

	statusCode, err := app.submit(pageData{
		URL:     "https://page.example/article",
		Title:   "Article",
		Text:    "Text",
		HTML:    "<body>Text</body>",
		Favicon: "data:image/png;base64,AA==",
		Label:   "qutebrowser",
	})
	if err != nil {
		t.Fatal(err)
	}
	if statusCode != http.StatusCreated {
		t.Fatalf("status = %d, want %d", statusCode, http.StatusCreated)
	}
	if submitter.document == nil {
		t.Fatal("submitter did not receive a document")
	}
	if submitter.document.URL != "https://page.example/article" ||
		submitter.document.Title != "Article" ||
		submitter.document.Text != "Text" ||
		submitter.document.HTML != "<body>Text</body>" ||
		submitter.document.Favicon != "data:image/png;base64,AA==" ||
		submitter.document.Label != "qutebrowser" {
		t.Fatalf("submitted document = %#v, want all extracted fields", submitter.document)
	}
}

func TestSubmitTreatsPolicyRejectionsAsCompleted(t *testing.T) {
	for _, statusCode := range []int{
		http.StatusNotAcceptable,
		http.StatusUnprocessableEntity,
	} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			opts, err := normalizeOptions(DefaultOptions())
			if err != nil {
				t.Fatal(err)
			}
			app := newCompanion(opts, &recordingSubmitter{
				err: testHTTPError{statusCode: statusCode},
			})

			got, err := app.submit(pageData{
				URL: "https://page.example/",
			})
			if err != nil {
				t.Fatalf("submit error = %v, want completed rejection", err)
			}
			if got != statusCode {
				t.Fatalf("status = %d, want %d", got, statusCode)
			}
		})
	}
}

func TestSubmitPropagatesUnexpectedHTTPFailure(t *testing.T) {
	opts, err := normalizeOptions(DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	submitErr := testHTTPError{statusCode: http.StatusInternalServerError}
	app := newCompanion(opts, &recordingSubmitter{err: submitErr})

	statusCode, err := app.submit(pageData{URL: "https://page.example/"})
	if !errors.Is(err, submitErr) {
		t.Fatalf("submit error = %v, want %v", err, submitErr)
	}
	if statusCode != http.StatusInternalServerError {
		t.Fatalf(
			"status = %d, want %d",
			statusCode,
			http.StatusInternalServerError,
		)
	}
}

func TestScheduleAtReplacesLaterDeadline(t *testing.T) {
	opts, err := normalizeOptions(DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	m := &monitor{
		companion:  newCompanion(opts, &recordingSubmitter{}),
		extraction: make(chan extractionDue, 1),
	}
	page := &pageState{sessionID: "page"}
	t.Cleanup(func() {
		if page.timer != nil {
			page.timer.Stop()
		}
	})

	firstDue := time.Now().Add(time.Hour)
	secondDue := firstDue.Add(time.Hour)
	m.scheduleAt(page, firstDue, false)
	firstGeneration := page.timerGeneration
	m.scheduleAt(page, secondDue, false)

	if !page.timerDue.Equal(secondDue) {
		t.Fatalf("timer due = %v, want %v", page.timerDue, secondDue)
	}
	if page.timerGeneration != firstGeneration+1 {
		t.Fatalf(
			"timer generation = %d, want %d",
			page.timerGeneration,
			firstGeneration+1,
		)
	}
}

func TestDefaultPageTiming(t *testing.T) {
	defaults := DefaultOptions()
	if defaults.InitialDelay != time.Second {
		t.Fatalf("initial delay = %v, want %v", defaults.InitialDelay, time.Second)
	}
	if defaults.Debounce != 10*time.Second {
		t.Fatalf("debounce = %v, want %v", defaults.Debounce, 10*time.Second)
	}
	if defaults.MaxWait != 30*time.Second {
		t.Fatalf("maximum wait = %v, want %v", defaults.MaxWait, 30*time.Second)
	}
}

func TestScheduleUpdateDoesNotPostponeInitialPage(t *testing.T) {
	input := DefaultOptions()
	input.InitialDelay = time.Hour
	input.Debounce = 2 * time.Hour
	opts, err := normalizeOptions(input)
	if err != nil {
		t.Fatal(err)
	}
	m := &monitor{
		companion:  newCompanion(opts, &recordingSubmitter{}),
		extraction: make(chan extractionDue, 1),
	}
	page := &pageState{sessionID: "page"}
	t.Cleanup(func() {
		if page.timer != nil {
			page.timer.Stop()
		}
	})

	m.scheduleInitial(page)
	initialDue := page.timerDue
	initialGeneration := page.timerGeneration
	m.scheduleUpdate(page)

	if !page.timerPriority {
		t.Fatal("initial page timer lost its priority")
	}
	if !page.timerDue.Equal(initialDue) {
		t.Fatalf("timer due = %v, want initial deadline %v", page.timerDue, initialDue)
	}
	if page.timerGeneration != initialGeneration {
		t.Fatalf(
			"timer generation = %d, want unchanged generation %d",
			page.timerGeneration,
			initialGeneration,
		)
	}
}

func TestPendingUpdateRunsImmediatelyAfterExtraction(t *testing.T) {
	opts, err := normalizeOptions(DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	page := &pageState{
		sessionID:  "page",
		extracting: true,
		pending:    true,
	}
	m := &monitor{
		companion:  newCompanion(opts, &recordingSubmitter{}),
		client:     &cdpClient{done: make(chan struct{})},
		pages:      map[string]*pageState{page.sessionID: page},
		extraction: make(chan extractionDue, 1),
	}
	t.Cleanup(func() {
		if page.timer != nil {
			page.timer.Stop()
		}
	})

	started := time.Now()
	m.handleExtractionResult(extractionResult{
		sessionID:   page.sessionID,
		url:         "https://example.com/",
		fingerprint: "fingerprint",
		unchanged:   true,
	})

	if page.pending {
		t.Fatal("pending update was not consumed")
	}
	if !page.timerPriority {
		t.Fatal("pending update timer can be postponed by later DOM changes")
	}
	if page.timerDue.After(started.Add(time.Second)) {
		t.Fatalf("pending update due = %v, want immediate extraction", page.timerDue)
	}
}

func TestDownloadFavicon(t *testing.T) {
	opts, err := normalizeOptions(DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	app := newCompanion(opts, &recordingSubmitter{})
	app.faviconClient = &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.String() != "https://example.com/favicon.png" {
				t.Fatalf("favicon request URL = %q", request.URL)
			}
			return &http.Response{
				Status:     "200 OK",
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"image/png"}},
				Body:       io.NopCloser(strings.NewReader("icon")),
				Request:    request,
			}, nil
		}),
	}

	got, err := app.downloadFavicon(
		context.Background(),
		mustURL(t, "https://example.com/article"),
		"https://example.com/favicon.png",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got != "data:image/png;base64,aWNvbg==" {
		t.Fatalf("favicon = %q, want encoded image data", got)
	}
}

func TestDownloadFaviconRejectsDifferentOrigin(t *testing.T) {
	opts, err := normalizeOptions(DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	app := newCompanion(opts, &recordingSubmitter{})

	_, err = app.downloadFavicon(
		context.Background(),
		mustURL(t, "https://page.example/article"),
		"https://cdn.example/favicon.png",
	)
	if err == nil || !strings.Contains(err.Error(), "different origin") {
		t.Fatalf("downloadFavicon error = %v, want origin validation error", err)
	}
}

func TestRunRejectsMissingSubmitter(t *testing.T) {
	err := Run(context.Background(), DefaultOptions(), nil)
	if err == nil || !strings.Contains(err.Error(), "submitter") {
		t.Fatalf("Run error = %v, want missing submitter error", err)
	}
}

func mustURL(t *testing.T, rawURL string) *url.URL {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

type recordingSubmitter struct {
	document *client.Document
	err      error
}

func (s *recordingSubmitter) AddDocumentJSON(doc *client.Document) error {
	s.document = doc
	return s.err
}

type testHTTPError struct {
	statusCode int
}

func (e testHTTPError) Error() string {
	return http.StatusText(e.statusCode)
}

func (e testHTTPError) HTTPStatusCode() int {
	return e.statusCode
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestDefaultUserAgent(t *testing.T) {
	if got := DefaultOptions().UserAgent; got != "Nogon qutebrowser companion" {
		t.Fatalf("user agent = %q", got)
	}
}
