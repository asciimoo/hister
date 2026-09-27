package qutebrowser

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/asciimoo/hister/server/document"

	"github.com/rs/zerolog/log"
)

const isolatedWorldName = "hister-companion"

type companion struct {
	options       normalizedOptions
	faviconClient *http.Client
	submitter     DocumentSubmitter
}

type targetInfo struct {
	TargetID string `json:"targetId"`
	Type     string `json:"type"`
	URL      string `json:"url"`
}

type pageState struct {
	targetID        string
	sessionID       string
	url             string
	timer           *time.Timer
	timerGeneration uint64
	timerDue        time.Time
	timerPriority   bool
	updateStarted   time.Time
	extracting      bool
	pending         bool
	lastFingerprint string
}

type extractionDue struct {
	sessionID  string
	generation uint64
}

type extractionResult struct {
	sessionID   string
	url         string
	fingerprint string
	statusCode  int
	unchanged   bool
	ignored     bool
	err         error
}

type monitor struct {
	companion   *companion
	client      *cdpClient
	bindingName string
	pages       map[string]*pageState
	targets     map[string]string
	extraction  chan extractionDue
	results     chan extractionResult
}

func newCompanion(opts normalizedOptions, submitter DocumentSubmitter) *companion {
	faviconClient := newHTTPClient(opts.requestTimeout)
	faviconClient.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) == 0 || !sameOrigin(request.URL, via[0].URL) {
			return http.ErrUseLastResponse
		}
		return nil
	}
	return &companion{
		options:       opts,
		faviconClient: faviconClient,
		submitter:     submitter,
	}
}

// Run watches qutebrowser pages until ctx is cancelled.
func Run(ctx context.Context, input Options, submitter DocumentSubmitter) error {
	if submitter == nil {
		return errors.New("document submitter is required")
	}
	opts, err := normalizeOptions(input)
	if err != nil {
		return err
	}
	return newCompanion(opts, submitter).run(ctx)
}

func (c *companion) run(ctx context.Context) error {
	for {
		err := c.runConnection(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		log.Warn().
			Err(err).
			Dur("reconnect_delay", c.options.reconnectDelay).
			Msg("DevTools connection ended, reconnecting")
		timer := time.NewTimer(c.options.reconnectDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (c *companion) runConnection(ctx context.Context) error {
	client, err := dialCDP(ctx, c.options.devToolsURL, c.options.requestTimeout)
	if err != nil {
		return err
	}
	defer client.close()
	connectionCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	bindingSuffix := make([]byte, 8)
	if _, err := rand.Read(bindingSuffix); err != nil {
		return fmt.Errorf("create observer binding name: %w", err)
	}
	m := &monitor{
		companion:   c,
		client:      client,
		bindingName: "__histerCompanionChanged_" + hex.EncodeToString(bindingSuffix),
		pages:       make(map[string]*pageState),
		targets:     make(map[string]string),
		extraction:  make(chan extractionDue, 256),
		results:     make(chan extractionResult, 256),
	}
	defer m.stop()

	if err := m.initialize(connectionCtx); err != nil {
		return err
	}
	log.Info().
		Stringer("devtools_url", c.options.devToolsURL).
		Msg("Connected to qutebrowser DevTools")
	return m.eventLoop(connectionCtx)
}

func (m *monitor) initialize(ctx context.Context) error {
	if err := m.call(
		ctx,
		"Target.setDiscoverTargets",
		map[string]any{"discover": true},
		"",
		nil,
	); err != nil {
		return fmt.Errorf("enable target discovery: %w", err)
	}

	var targets struct {
		TargetInfos []targetInfo `json:"targetInfos"`
	}
	if err := m.call(ctx, "Target.getTargets", nil, "", &targets); err != nil {
		return fmt.Errorf("list browser targets: %w", err)
	}
	for _, target := range targets.TargetInfos {
		if err := m.attach(ctx, target); err != nil {
			log.Warn().Err(err).Str("url", target.URL).Msg("Cannot inspect browser target")
		}
	}
	return nil
}

func (m *monitor) eventLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-m.client.done:
			return m.client.connectionError()
		case <-m.client.events.ready:
			m.handleEvent(ctx, m.client.events.pop())
		case due := <-m.extraction:
			m.handleExtractionDue(ctx, due)
		case result := <-m.results:
			m.handleExtractionResult(result)
		}
	}
}

func (m *monitor) handleEvent(ctx context.Context, event rpcMessage) {
	log.Trace().Str("event", event.Method).Msg("Received DevTools event")
	switch event.Method {
	case "Target.targetCreated", "Target.targetInfoChanged":
		var params struct {
			TargetInfo targetInfo `json:"targetInfo"`
		}
		if !decodeEvent(event, &params) {
			return
		}
		sessionID := m.targets[params.TargetInfo.TargetID]
		if sessionID == "" {
			if err := m.attach(ctx, params.TargetInfo); err != nil {
				log.Warn().
					Err(err).
					Str("url", params.TargetInfo.URL).
					Msg("Cannot inspect browser target")
			}
			return
		}
		page := m.pages[sessionID]
		if page != nil {
			changedURL := page.url != params.TargetInfo.URL
			page.url = params.TargetInfo.URL
			if changedURL {
				m.scheduleInitial(page)
			} else {
				m.scheduleUpdate(page)
			}
		}
	case "Target.targetDestroyed":
		var params struct {
			TargetID string `json:"targetId"`
		}
		if decodeEvent(event, &params) {
			m.removeTarget(params.TargetID)
		}
	case "Target.targetCrashed":
		var params struct {
			TargetID string `json:"targetId"`
		}
		if decodeEvent(event, &params) {
			m.removeTarget(params.TargetID)
		}
	case "Target.detachedFromTarget":
		var params struct {
			SessionID string `json:"sessionId"`
		}
		if decodeEvent(event, &params) {
			m.removeSession(params.SessionID)
		}
	case "Page.frameNavigated":
		var params struct {
			Frame struct {
				ParentID string `json:"parentId"`
				URL      string `json:"url"`
			} `json:"frame"`
		}
		if decodeEvent(event, &params) && params.Frame.ParentID == "" {
			if page := m.pages[event.SessionID]; page != nil {
				page.url = params.Frame.URL
				m.scheduleInitial(page)
			}
		}
	case "Page.loadEventFired", "Page.navigatedWithinDocument":
		if page := m.pages[event.SessionID]; page != nil {
			m.scheduleInitial(page)
		}
	case "Page.lifecycleEvent":
		var params struct {
			Name string `json:"name"`
		}
		if decodeEvent(event, &params) {
			page := m.pages[event.SessionID]
			if page == nil {
				return
			}
			switch params.Name {
			case "DOMContentLoaded", "load":
				m.scheduleInitial(page)
			case "networkIdle":
				m.scheduleUpdate(page)
			}
		}
	case "Runtime.bindingCalled":
		var params struct {
			Name string `json:"name"`
		}
		if decodeEvent(event, &params) && params.Name == m.bindingName {
			if page := m.pages[event.SessionID]; page != nil {
				m.scheduleUpdate(page)
			}
		}
	}
}

func (m *monitor) attach(ctx context.Context, target targetInfo) error {
	if target.Type != "page" || target.TargetID == "" {
		return nil
	}
	if _, exists := m.targets[target.TargetID]; exists {
		return nil
	}

	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err := m.call(
		ctx,
		"Target.attachToTarget",
		map[string]any{"targetId": target.TargetID, "flatten": true},
		"",
		&attached,
	); err != nil {
		return err
	}
	if attached.SessionID == "" {
		return errors.New("devtools returned an empty page session ID")
	}

	page := &pageState{
		targetID:  target.TargetID,
		sessionID: attached.SessionID,
		url:       target.URL,
	}
	m.targets[target.TargetID] = attached.SessionID
	m.pages[attached.SessionID] = page

	if err := m.configurePage(ctx, page); err != nil {
		m.removeTarget(target.TargetID)
		return err
	}
	m.scheduleInitial(page)
	log.Debug().Str("url", target.URL).Msg("Watching browser page")
	return nil
}

func (m *monitor) configurePage(ctx context.Context, page *pageState) error {
	for _, method := range []string{"Page.enable", "Runtime.enable"} {
		if err := m.call(ctx, method, nil, page.sessionID, nil); err != nil {
			return fmt.Errorf("%s: %w", method, err)
		}
	}
	if err := m.call(
		ctx,
		"Runtime.addBinding",
		map[string]any{
			"name":                 m.bindingName,
			"executionContextName": isolatedWorldName,
		},
		page.sessionID,
		nil,
	); err != nil {
		return fmt.Errorf("install page change binding: %w", err)
	}

	source := mutationObserverSource(m.bindingName)
	if err := m.call(
		ctx,
		"Page.addScriptToEvaluateOnNewDocument",
		map[string]any{
			"source":    source,
			"worldName": isolatedWorldName,
		},
		page.sessionID,
		nil,
	); err != nil {
		return fmt.Errorf("install page observer for navigation: %w", err)
	}

	// Lifecycle events are useful but not required on older QtWebEngine builds.
	_ = m.call(
		ctx,
		"Page.setLifecycleEventsEnabled",
		map[string]any{"enabled": true},
		page.sessionID,
		nil,
	)
	// The new document hook applies only to later documents. Create an isolated
	// world in the current document so page scripts cannot alter the observer.
	executionContextID, err := m.isolatedExecutionContext(ctx, page.sessionID)
	if err != nil {
		return fmt.Errorf("create isolated page observer: %w", err)
	}
	if err := m.call(
		ctx,
		"Runtime.evaluate",
		map[string]any{
			"expression": source,
			"contextId":  executionContextID,
		},
		page.sessionID,
		nil,
	); err != nil {
		return fmt.Errorf("install current page observer: %w", err)
	}
	return nil
}

func (m *monitor) isolatedExecutionContext(ctx context.Context, sessionID string) (int64, error) {
	var frameTree struct {
		FrameTree struct {
			Frame struct {
				ID string `json:"id"`
			} `json:"frame"`
		} `json:"frameTree"`
	}
	if err := m.call(ctx, "Page.getFrameTree", nil, sessionID, &frameTree); err != nil {
		return 0, err
	}
	if frameTree.FrameTree.Frame.ID == "" {
		return 0, errors.New("devtools returned an empty main frame ID")
	}

	var isolatedWorld struct {
		ExecutionContextID int64 `json:"executionContextId"`
	}
	if err := m.call(
		ctx,
		"Page.createIsolatedWorld",
		map[string]any{
			"frameId":   frameTree.FrameTree.Frame.ID,
			"worldName": isolatedWorldName,
		},
		sessionID,
		&isolatedWorld,
	); err != nil {
		return 0, err
	}
	if isolatedWorld.ExecutionContextID == 0 {
		return 0, errors.New("devtools returned an empty execution context ID")
	}
	return isolatedWorld.ExecutionContextID, nil
}

func (m *monitor) call(
	parent context.Context,
	method string,
	params any,
	sessionID string,
	result any,
) error {
	ctx, cancel := context.WithTimeout(parent, m.companion.options.commandTimeout)
	defer cancel()
	return m.client.call(ctx, method, params, sessionID, result)
}

func (m *monitor) scheduleInitial(page *pageState) {
	page.updateStarted = time.Time{}
	m.scheduleAt(page, time.Now().Add(m.companion.options.initialDelay), true)
}

func (m *monitor) scheduleUpdate(page *pageState) {
	if page.timer != nil && page.timerPriority {
		return
	}
	now := time.Now()
	if page.updateStarted.IsZero() {
		page.updateStarted = now
	}
	due := now.Add(m.companion.options.debounce)
	if maxWait := m.companion.options.maxWait; maxWait > 0 {
		maxDue := page.updateStarted.Add(maxWait)
		if due.After(maxDue) {
			due = maxDue
		}
	}
	m.scheduleAt(page, due, false)
}

func (m *monitor) scheduleAfter(page *pageState, delay time.Duration) {
	m.scheduleAt(page, time.Now().Add(delay), false)
}

func (m *monitor) scheduleAt(page *pageState, due time.Time, priority bool) {
	if page.timer != nil {
		if page.timerPriority && !priority {
			return
		}
		if page.timerPriority == priority && due.Equal(page.timerDue) {
			return
		}
	}
	page.timerGeneration++
	generation := page.timerGeneration
	sessionID := page.sessionID
	if page.timer != nil {
		page.timer.Stop()
	}
	page.timerDue = due
	page.timerPriority = priority
	page.timer = time.AfterFunc(time.Until(due), func() {
		select {
		case m.extraction <- extractionDue{
			sessionID:  sessionID,
			generation: generation,
		}:
		case <-m.client.done:
		}
	})
}

func (m *monitor) handleExtractionDue(ctx context.Context, due extractionDue) {
	page := m.pages[due.sessionID]
	if page == nil || page.timerGeneration != due.generation {
		return
	}
	page.timer = nil
	page.timerDue = time.Time{}
	page.timerPriority = false
	page.updateStarted = time.Time{}
	if page.extracting {
		page.pending = true
		return
	}
	page.extracting = true
	lastFingerprint := page.lastFingerprint
	go func() {
		result := m.companion.extractAndSubmit(ctx, m, page.sessionID, lastFingerprint)
		select {
		case m.results <- result:
		case <-ctx.Done():
		}
	}()
}

func (m *monitor) handleExtractionResult(result extractionResult) {
	page := m.pages[result.sessionID]
	if page == nil {
		return
	}
	page.extracting = false
	if result.err == nil {
		page.lastFingerprint = result.fingerprint
	}

	switch {
	case result.err != nil:
		log.Error().Err(result.err).Str("url", result.url).Msg("Failed to index browser page")
	case result.ignored:
		log.Debug().Str("url", result.url).Msg("Ignored browser page")
	case result.unchanged:
		log.Debug().Str("url", result.url).Msg("Browser page content is unchanged")
	case result.statusCode == http.StatusNotAcceptable:
		log.Info().Str("url", result.url).Msg("Hister skip rules rejected browser page")
	case result.statusCode == http.StatusUnprocessableEntity:
		log.Info().Str("url", result.url).Msg("Hister sensitive content checks rejected browser page")
	default:
		log.Info().Str("url", result.url).Msg("Indexed browser page")
	}

	if page.pending {
		page.pending = false
		m.scheduleAt(page, time.Now(), true)
		return
	}
	if result.err != nil && m.companion.options.retryDelay > 0 {
		m.scheduleAfter(page, m.companion.options.retryDelay)
	}
}

func (m *monitor) removeTarget(targetID string) {
	sessionID := m.targets[targetID]
	delete(m.targets, targetID)
	if page := m.pages[sessionID]; page != nil {
		if page.timer != nil {
			page.timer.Stop()
		}
		delete(m.pages, sessionID)
	}
}

func (m *monitor) removeSession(sessionID string) {
	page := m.pages[sessionID]
	if page == nil {
		return
	}
	m.removeTarget(page.targetID)
}

func (m *monitor) stop() {
	for _, page := range m.pages {
		if page.timer != nil {
			page.timer.Stop()
		}
	}
}

func decodeEvent(event rpcMessage, target any) bool {
	if err := json.Unmarshal(event.Params, target); err != nil {
		log.Warn().Err(err).Str("event", event.Method).Msg("Cannot decode DevTools event")
		return false
	}
	return true
}

func mutationObserverSource(bindingName string) string {
	quotedName := strconv.Quote(bindingName)
	return `(() => {
		const bindingName = ` + quotedName + `;
		const observerKey = bindingName + "Observer";
		if (globalThis[observerKey]) {
			return;
		}
		let timer = 0;
		const signalChange = () => {
			if (timer) {
				return;
			}
			timer = setTimeout(() => {
				timer = 0;
			}, 1000);
			try {
				globalThis[bindingName]("changed");
			} catch (_) {}
		};
		if (typeof MutationObserver === "function") {
			const observer = new MutationObserver(signalChange);
			observer.observe(document, {
				attributes: true,
				characterData: true,
				childList: true,
				subtree: true
			});
			globalThis[observerKey] = observer;
		}
		addEventListener("load", signalChange, {once: true});
		addEventListener("hashchange", signalChange);
		addEventListener("popstate", signalChange);
		signalChange();
	})()`
}

const pageExtractionExpression = `(() => {
	const pageURL = window.location.href.replace(window.location.hash, "");
	let faviconURL = "";
	try {
		const faviconHref = document.querySelector("link[rel~='icon']")?.getAttribute("href");
		faviconURL = new URL(faviconHref || "/favicon.ico", pageURL).href;
	} catch (_) {
	}
	return {
		title: document.querySelector("title")?.innerText ?? document.title,
		text: document.body?.innerText ?? "",
		url: pageURL,
		html: document.documentElement?.innerHTML ?? "",
		faviconURL
	};
})()`

type pageData struct {
	Title      string `json:"title"`
	Text       string `json:"text"`
	URL        string `json:"url"`
	HTML       string `json:"html"`
	FaviconURL string `json:"faviconURL,omitempty"`
	Favicon    string `json:"favicon,omitempty"`
	Label      string `json:"label,omitempty"`
}

func (c *companion) extractAndSubmit(
	ctx context.Context,
	monitor *monitor,
	sessionID string,
	lastFingerprint string,
) extractionResult {
	result := extractionResult{sessionID: sessionID}

	var evaluated struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails json.RawMessage `json:"exceptionDetails"`
	}
	executionContextID, err := monitor.isolatedExecutionContext(ctx, sessionID)
	if err != nil {
		result.err = fmt.Errorf("open isolated page context: %w", err)
		return result
	}
	err = monitor.call(
		ctx,
		"Runtime.evaluate",
		map[string]any{
			"expression":    pageExtractionExpression,
			"returnByValue": true,
			"contextId":     executionContextID,
		},
		sessionID,
		&evaluated,
	)
	if err != nil {
		result.err = fmt.Errorf("extract rendered page: %w", err)
		return result
	}
	if len(evaluated.ExceptionDetails) != 0 {
		result.err = errors.New("extract rendered page: JavaScript evaluation failed")
		return result
	}

	var data pageData
	if err := json.Unmarshal(evaluated.Result.Value, &data); err != nil {
		result.err = fmt.Errorf("decode rendered page: %w", err)
		return result
	}
	result.url = data.URL
	result.fingerprint = pageFingerprint(data)

	pageURL, err := url.Parse(data.URL)
	if err != nil || (pageURL.Scheme != "http" && pageURL.Scheme != "https") || pageURL.Host == "" {
		result.ignored = true
		return result
	}
	if c.isHisterPage(pageURL) {
		result.ignored = true
		return result
	}
	if result.fingerprint == lastFingerprint {
		result.unchanged = true
		return result
	}

	data.Label = c.options.label
	if data.FaviconURL != "" {
		favicon, err := c.downloadFavicon(ctx, pageURL, data.FaviconURL)
		if err != nil {
			log.Debug().Err(err).Str("url", data.URL).Msg("Could not fetch browser page favicon")
		} else {
			data.Favicon = favicon
		}
	}
	data.FaviconURL = ""

	result.statusCode, result.err = c.submit(data)
	return result
}

func pageFingerprint(data pageData) string {
	hash := sha256.New()
	_, _ = io.WriteString(hash, data.URL)
	_, _ = hash.Write([]byte{0})
	_, _ = io.WriteString(hash, data.HTML)
	return hex.EncodeToString(hash.Sum(nil))
}

func (c *companion) isHisterPage(pageURL *url.URL) bool {
	if !sameOrigin(pageURL, c.options.histerURL) {
		return false
	}
	basePath := strings.TrimSuffix(c.options.histerURL.EscapedPath(), "/")
	pagePath := pageURL.EscapedPath()
	return pagePath == basePath || strings.HasPrefix(pagePath, basePath+"/")
}

func (c *companion) submit(data pageData) (int, error) {
	err := c.submitter.AddDocumentJSON(&document.Document{
		URL:     data.URL,
		Title:   data.Title,
		Text:    data.Text,
		HTML:    data.HTML,
		Favicon: data.Favicon,
		Label:   data.Label,
	})
	if err == nil {
		return http.StatusCreated, nil
	}

	var statusError interface {
		HTTPStatusCode() int
	}
	if !errors.As(err, &statusError) {
		return 0, err
	}
	statusCode := statusError.HTTPStatusCode()
	if statusCode == http.StatusNotAcceptable ||
		statusCode == http.StatusUnprocessableEntity {
		return statusCode, nil
	}
	return statusCode, err
}

func (c *companion) downloadFavicon(
	ctx context.Context,
	pageURL *url.URL,
	rawFaviconURL string,
) (_ string, err error) {
	if strings.HasPrefix(rawFaviconURL, "data:") {
		maxEncodedSize := c.options.maxFaviconBytes*4/3 + 1024
		if int64(len(rawFaviconURL)) > maxEncodedSize {
			return "", errors.New("data URI favicon is too large")
		}
		return rawFaviconURL, nil
	}

	faviconURL, err := url.Parse(rawFaviconURL)
	if err != nil {
		return "", fmt.Errorf("invalid favicon URL: %w", err)
	}
	if !sameOrigin(faviconURL, pageURL) {
		return "", errors.New("favicon URL has a different origin")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, faviconURL.String(), nil)
	if err != nil {
		return "", fmt.Errorf("create favicon request: %w", err)
	}
	request.Header.Set("User-Agent", c.options.userAgent)
	response, err := c.faviconClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("fetch favicon: %w", err)
	}
	defer closeResponseBody(response, &err)
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch favicon: HTTP %s", response.Status)
	}
	if response.ContentLength > c.options.maxFaviconBytes {
		return "", errors.New("favicon is too large")
	}

	limited := io.LimitReader(response.Body, c.options.maxFaviconBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("read favicon: %w", err)
	}
	if int64(len(data)) > c.options.maxFaviconBytes {
		return "", errors.New("favicon is too large")
	}

	contentType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || contentType == "" {
		contentType = http.DetectContentType(data)
	}
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) &&
		strings.EqualFold(a.Hostname(), b.Hostname()) &&
		effectivePort(a) == effectivePort(b)
}

func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	if u.Scheme == "https" || u.Scheme == "wss" {
		return "443"
	}
	return "80"
}
