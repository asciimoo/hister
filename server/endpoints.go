package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	iofs "io/fs"
	"mime"
	"net/http"
	"net/http/pprof"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/files"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/extractor"
	"github.com/asciimoo/hister/server/extractor/sdk"
	"github.com/asciimoo/hister/server/indexer"
	"github.com/asciimoo/hister/server/indexer/querybuilder"
	"github.com/asciimoo/hister/server/indexer/searchschema"
	"github.com/asciimoo/hister/server/model"
	"github.com/asciimoo/hister/server/timeline"
	"github.com/asciimoo/hister/server/types"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
	"github.com/sergi/go-diff/diffmatchpatch"
)

type historyItem struct {
	URL    string `json:"url"`
	Title  string `json:"title"`
	Query  string `json:"query"`
	Delete bool   `json:"delete"`
	Pin    *bool  `json:"pin"`
}

const (
	healthCheckPath                 = "/health"
	webUIDocumentCacheControl       = "no-store"
	webUIVersionCacheControl        = "no-cache"
	webUIAssetCacheControl          = "public, max-age=3600"
	webUIImmutableAssetCacheControl = "public, max-age=31536000, immutable"
)

var ws = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func registerEndpoints(cfg *config.Config, idx *indexer.Indexer) http.Handler {
	mux := http.NewServeMux()
	tokenAuth := cfg.App.AccessToken != ""
	userHandling := cfg.App.UserHandling

	for _, e := range Endpoints {
		h := e.Handler
		if e.CSRFRequired {
			h = withCSRF(h)
		}
		requiresAuth := endpointRequiresAuth(cfg, e)
		if tokenAuth && !userHandling && requiresAuth {
			h = withTokenAuth(h)
		} else if userHandling && requiresAuth {
			if e.AdminOnly {
				h = withAdminAuth(h)
			} else {
				h = withUserAuth(h)
			}
		}
		mux.HandleFunc(e.Pattern(), createHandler(cfg, idx, h))
	}
	if cfg.App.Profiler {
		registerDebugEndpoints(mux, cfg, idx)
	}
	// SPA catch-all: serve index.html for any path not matched above
	mux.HandleFunc("GET /static/", createHandler(cfg, idx, serveStatic))
	mux.HandleFunc("GET /favicon.ico", createHandler(cfg, idx, serveFavicon))
	mux.HandleFunc("GET /opensearch.xml", createHandler(cfg, idx, serveOpensearch))
	mux.HandleFunc("/", createHandler(cfg, idx, serveSPA))
	// If base_url contains a path prefix, require it for application routes.
	appHandler := withExtensionCORS(http.Handler(mux))
	basePrefix := cfg.BasePathPrefix()
	if basePrefix != "" {
		appHandler = withOptionalBasePathPrefix(basePrefix, appHandler)
	}
	serverMux := http.NewServeMux()
	serverMux.HandleFunc(healthCheckPath, serveHealth)
	serverMux.Handle("/", appHandler)
	return serverMux
}

func serveHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func registerDebugEndpoints(mux *http.ServeMux, cfg *config.Config, idx *indexer.Indexer) {
	register := func(pattern string, handler http.HandlerFunc) {
		h := endpointHandler(func(c *webContext) {
			handler(c.Response, c.Request)
		})
		if cfg.App.UserHandling {
			h = withAdminAuth(h)
		} else if cfg.App.AccessToken != "" {
			h = withTokenAuth(h)
		}
		mux.HandleFunc(pattern, createHandler(cfg, idx, h))
	}
	register("GET /debug/pprof", pprof.Index)
	register("GET /debug/pprof/", pprof.Index)
	register("GET /debug/pprof/cmdline", pprof.Cmdline)
	register("GET /debug/pprof/profile", pprof.Profile)
	register("GET /debug/pprof/symbol", pprof.Symbol)
	register("POST /debug/pprof/symbol", pprof.Symbol)
	register("GET /debug/pprof/trace", pprof.Trace)
}

func endpointRequiresAuth(cfg *config.Config, e *Endpoint) bool {
	if e.NoAuth {
		return false
	}
	if cfg.App.Public && e.Public {
		return false
	}
	return true
}

func withOptionalBasePathPrefix(prefix string, next http.Handler) http.Handler {
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix == "" || prefix == "/" {
		return next
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p != prefix && !strings.HasPrefix(p, prefix+"/") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = strings.TrimPrefix(p, prefix)
		if r2.URL.Path == "" {
			r2.URL.Path = "/"
		}
		r2.RequestURI = r2.URL.RequestURI()
		next.ServeHTTP(w, r2)
	})
}

func serveIndex(c *webContext) {
	c.Response.Header().Set("Cache-Control", webUIDocumentCacheControl)
	content, ok := staticTextFiles["index.html"]
	if !ok {
		serve500(c)
		return
	}
	c.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
	c.Response.Header().Set("Content-Security-Policy", fmt.Sprintf("script-src 'strict-dynamic' 'nonce-%s'", c.nonce))
	if _, err := c.Response.Write(bytes.ReplaceAll(content, []byte("<script>"), fmt.Appendf(nil, `<script nonce="%s">`, c.nonce))); err != nil {
		log.Warn().Err(err).Msg("failed to write index response")
	}
}

func webUICacheControl(requestPath string) string {
	assetPath := strings.TrimPrefix(requestPath, "/")
	assetPath = strings.TrimPrefix(assetPath, "static/")
	switch {
	case assetPath == "" || assetPath == "index.html":
		return webUIDocumentCacheControl
	case assetPath == "_app/version.json":
		return webUIVersionCacheControl
	case strings.HasPrefix(assetPath, "_app/immutable/"):
		return webUIImmutableAssetCacheControl
	default:
		return webUIAssetCacheControl
	}
}

func serveStaticContent(c *webContext, path string, content []byte) {
	ext := filepath.Ext(path)
	if mimeType := mime.TypeByExtension(ext); mimeType != "" {
		c.Response.Header().Set("Content-Type", mimeType)
	} else {
		c.Response.Header().Set("Content-Type", "application/octet-stream")
	}
	c.Response.Header().Set("Cache-Control", webUICacheControl(path))
	c.Response.WriteHeader(http.StatusOK)
	if _, err := c.Response.Write(content); err != nil {
		log.Warn().Err(err).Msg("failed to write static file response")
	}
}

// serveSPA serves the SPA index.html for any route not matching a static file.
func serveSPA(c *webContext) {
	path := strings.TrimPrefix(c.Request.URL.Path, "/")
	if path == "index.html" {
		serveIndex(c)
		return
	}
	if content, ok := staticTextFiles[path]; ok {
		serveStaticContent(c, path, content)
		return
	}
	// If the exact file exists in the embedded app FS, serve it directly
	if _, err := iofs.Stat(appSubFS, path); err == nil {
		// Read the file and serve it with proper MIME type
		content, err := iofs.ReadFile(appSubFS, path)
		if err != nil {
			serve500(c)
			return
		}
		serveStaticContent(c, path, content)
		return
	}

	// redirect to configured search engine if the query starts or ends with "!!"
	q := c.Request.URL.Query().Get("q")
	if strings.HasPrefix(q, "!!") || strings.HasSuffix(q, "!!") {
		if strings.HasPrefix(q, "!!") {
			q = q[2:]
		} else if strings.HasSuffix(q, "!!") {
			q = q[:len(q)-2]
		}
		c.Redirect(strings.Replace(c.Config.App.SearchURL, "{query}", strings.TrimSpace(q), 1))
		return
	}

	// redirect to configured search engine if query string exists but we have no matching results
	if q != "" && c.Config.App.RedirectOnNoResults {
		res, err := c.Indexer.Search(&indexer.Query{
			Text:   c.effectiveRules().ResolveAliases(q),
			UserID: c.UserID,
		})
		if err != nil {
			res = &indexer.Results{}
		}
		hr, err := model.GetURLsByQuery(c.UserID, q)
		if err == nil && len(hr) > 0 {
			res.History = hr
		}
		if err != nil {
			serve500(c)
			return
		}
		if len(res.Documents) == 0 && len(hr) == 0 {
			c.Redirect(strings.Replace(c.Config.App.SearchURL, "{query}", q, 1))
			return
		}
	}
	// Otherwise serve index.html for client-side routing
	serveIndex(c)
}

func serveLogin(c *webContext) {
	if c.Config.Server.OAuthOnly {
		http.Error(c.Response, "password login disabled, use OAuth", http.StatusForbidden)
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		serve500(c)
		return
	}
	user, err := model.AuthenticateUser(req.Username, req.Password)
	if err != nil {
		http.Error(c.Response, "invalid credentials", http.StatusUnauthorized)
		return
	}
	session, err := sessionStore.Get(c.Request, storeName)
	if err != nil {
		serve500(c)
		return
	}
	if err := sessionStore.Rotate(session); err != nil {
		serve500(c)
		return
	}
	sessionStore.authenticateUser(session, user.ID)
	if err := session.Save(c.Request, c.Response); err != nil {
		serve500(c)
		return
	}
	c.JSON(map[string]string{"username": user.Username})
}

func serveTokenLogin(c *webContext) {
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		serve500(c)
		return
	}
	if c.Config.App.AccessToken == "" || req.Token != c.Config.App.AccessToken {
		http.Error(c.Response, "invalid token", http.StatusUnauthorized)
		return
	}
	session, err := sessionStore.Get(c.Request, storeName)
	if err != nil {
		serve500(c)
		return
	}
	if err := sessionStore.Rotate(session); err != nil {
		serve500(c)
		return
	}
	sessionStore.authenticateToken(session, c.Config.App.AccessToken)
	if err := session.Save(c.Request, c.Response); err != nil {
		serve500(c)
		return
	}
	c.JSON(map[string]string{"status": "ok"})
}

func serveLogout(c *webContext) {
	session, err := sessionStore.Get(c.Request, storeName)
	if err != nil {
		serve500(c)
		return
	}
	session.Values = make(map[any]any)
	session.Options.MaxAge = -1
	if err := session.Save(c.Request, c.Response); err != nil {
		serve500(c)
		return
	}
	serve200(c)
}

func serveProfile(c *webContext) {
	if c.Config.App.UserHandling {
		resp := map[string]any{
			"user_id":  c.UserID,
			"username": c.Username,
			"is_admin": c.IsAdmin,
		}
		if c.IsAdmin {
			resp["version"] = Version
		}
		c.JSON(resp)
		return
	}
	serve200(c)
}

func serveGenerateToken(c *webContext) {
	token, err := model.RegenerateToken(c.UserID)
	if err != nil {
		serve500(c)
		return
	}
	c.JSON(map[string]string{"token": token})
}

// serveConfig returns app configuration as JSON and refreshes CSRF token.
func serveConfig(c *webContext) {
	type configResponse struct {
		BaseURL              string                    `json:"baseUrl"`
		BasePath             string                    `json:"basePath"`
		WsURL                string                    `json:"wsUrl"`
		Title                string                    `json:"title"`
		Subtitle             string                    `json:"subtitle"`
		ColorScheme          string                    `json:"colorScheme"`
		SearchURL            string                    `json:"searchUrl"`
		OpenResultsOnNewTab  bool                      `json:"openResultsOnNewTab"`
		Hotkeys              map[string]string         `json:"hotkeys"`
		AuthMode             string                    `json:"authMode"`
		Authenticated        bool                      `json:"authenticated"`
		DiagnosticsAvailable bool                      `json:"diagnosticsAvailable"`
		Public               bool                      `json:"public"`
		CanWrite             bool                      `json:"canWrite"`
		HistoryEnabled       bool                      `json:"historyEnabled"`
		Username             string                    `json:"username,omitempty"`
		UserID               uint                      `json:"userId,omitempty"`
		SemanticEnabled      bool                      `json:"semanticEnabled"`
		SemanticWeight       float64                   `json:"semanticWeight,omitempty"`
		SimilarityThreshold  float64                   `json:"similarityThreshold,omitempty"`
		OAuthProviders       []string                  `json:"oauthProviders,omitempty"`
		OAuthOnly            bool                      `json:"oauthOnly,omitempty"`
		DisablePreviews      bool                      `json:"disablePreviews,omitempty"`
		MaxBatchBodyBytes    int64                     `json:"maxBatchBodyBytes"`
		Search               searchschema.Capabilities `json:"search"`
	}
	authMode := "none"
	authenticated := true
	if c.Config.App.UserHandling {
		authMode = "user"
		authenticated = c.UserID > 0
	} else if c.Config.App.AccessToken != "" {
		authMode = "token"
		authenticated = c.Authenticated
	}
	hotkeys := c.Config.Hotkeys.Web
	if hotkeys == nil {
		hotkeys = make(map[string]string)
	}
	oauthProviders := make([]string, 0, len(c.Config.Server.OAuth))
	for name := range c.Config.Server.OAuth {
		oauthProviders = append(oauthProviders, name)
	}
	c.JSON(configResponse{
		BaseURL:              c.Config.BaseURL(""),
		BasePath:             c.Config.BasePathPrefix(),
		WsURL:                c.Config.WebSocketURL(),
		Title:                c.Config.App.Title,
		Subtitle:             c.Config.App.Subtitle,
		ColorScheme:          c.Config.App.ColorScheme,
		SearchURL:            c.Config.App.SearchURL,
		OpenResultsOnNewTab:  c.Config.App.OpenResultsOnNewTab,
		Hotkeys:              hotkeys,
		AuthMode:             authMode,
		Authenticated:        authenticated,
		DiagnosticsAvailable: true,
		Public:               c.Config.App.Public,
		CanWrite:             canWrite(c),
		HistoryEnabled:       historyEnabled(c),
		Username:             c.Username,
		UserID:               c.UserID,
		SemanticEnabled:      c.Indexer != nil && c.Indexer.SemanticSearchEnabled(),
		SemanticWeight:       c.Config.SemanticSearch.SemanticWeight,
		SimilarityThreshold:  c.Config.SemanticSearch.SimilarityThreshold,
		OAuthProviders:       oauthProviders,
		OAuthOnly:            c.Config.Server.OAuthOnly,
		DisablePreviews:      c.Config.App.DisablePreviews,
		MaxBatchBodyBytes:    c.Config.Server.MaxBatchBodyBytes(),
		Search:               searchschema.CapabilitiesDefinition(),
	})
}

// parseSearchQueryParams parses URL query parameters into an indexer.Query.
func parseSearchQueryParams(r *http.Request) (*indexer.Query, error) {
	urlParams := r.URL.Query()
	query := &indexer.Query{}
	if rawQuery := urlParams.Get("query"); rawQuery != "" {
		if err := json.Unmarshal([]byte(rawQuery), query); err != nil {
			return nil, err
		}
	}
	if q := urlParams.Get("q"); q != "" {
		query.Text = q
	}
	for param, field := range map[string]*int64{"date_from": &query.DateFrom, "date_to": &query.DateTo} {
		if v := urlParams.Get(param); v != "" {
			if t, err := time.Parse("2006-01-02", v); err == nil {
				ts := t.Unix()
				if param == "date_to" {
					// Include the entire end date by advancing to end of day (23:59:59)
					ts += 24*60*60 - 1
				}
				*field = ts
			}
		}
	}
	if urlParams.Get("include_html") == "1" {
		query.IncludeHTML = true
	}
	if pk := urlParams.Get("page_key"); pk != "" {
		query.PageKey = pk
	}
	if s := urlParams.Get("sort"); s != "" {
		query.Sort = s
	}
	if v := urlParams.Get("semantic"); v != "" {
		query.SemanticEnabled = v == "1" || v == "true"
	}
	if v := urlParams.Get("semantic_threshold"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			query.SemanticThreshold = f
		}
	}
	return query, nil
}

// serveSearchHTTP executes a single search and writes a JSON response.
func serveSearchHTTP(c *webContext, query *indexer.Query) {
	r, err := doSearch(c.Indexer, query, c.effectiveRules(), c.UserID, historyEnabled(c))
	if err != nil {
		if errors.Is(err, querybuilder.ErrInvalidRegexp) {
			http.Error(c.Response, err.Error(), http.StatusBadRequest)
			return
		}
		log.Error().Err(err).Msg("search error")
		serve500(c)
		return
	}
	jr, err := json.Marshal(r)
	if err != nil {
		serve500(c)
		return
	}
	c.Response.Header().Add("Content-Type", "application/json")
	if _, err := c.Response.Write(jr); err != nil {
		log.Warn().Err(err).Msg("failed to write search response")
	}
}

// serveSearchWebSocket upgrades the connection and serves searches over WebSocket.
func serveSearchWebSocket(c *webContext) {
	conn, err := ws.Upgrade(c.Response, c.Request, nil)
	if err != nil {
		log.Error().Err(err).Msg("failed to upgrade websocket request")
		return
	}
	defer func() {
		if err := conn.Close(); err != nil {
			log.Warn().Err(err).Msg("failed to close websocket connection")
		}
	}()
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Error().Err(err).Msg("failed to read websocket message")
			}
			break
		}
		var query *indexer.Query
		if err = json.Unmarshal(msg, &query); err != nil {
			log.Error().Err(err).Msg("failed to parse query")
			continue
		}
		// Semantic search is only available when the server has it enabled;
		// otherwise honour the client's per-request flag.
		if !c.Config.SemanticSearch.Enable {
			query.SemanticEnabled = false
		}
		res, err := doSearch(c.Indexer, query, c.effectiveRules(), c.UserID, historyEnabled(c))
		if err != nil {
			if errors.Is(err, querybuilder.ErrInvalidRegexp) {
				res = &indexer.Results{
					Query:     query,
					Documents: []*document.Document{},
					History:   []*model.URLCount{},
				}
			} else {
				log.Error().Err(err).Msg("search error")
				continue
			}
		}
		jr, err := json.Marshal(res)
		if err != nil {
			log.Error().Err(err).Msg("failed to marshal indexer results")
			continue
		}
		if err := conn.WriteMessage(websocket.TextMessage, jr); err != nil {
			log.Error().Err(err).Msg("failed to write websocket message")
			break
		}
	}
}

func serveSearch(c *webContext) {
	origin := c.Request.Header.Get("Origin")
	// `?format=json` (or `Accept: application/json`) opts the caller into a
	// pure JSON response and skips the same-host Origin check, so CLI tools
	// and ad-hoc HTTP clients can hit the search endpoint without spoofing
	// a hister:// Origin header.
	jsonFormat := c.Request.URL.Query().Get("format") == "json" ||
		c.Request.Header.Get("Accept") == "application/json"
	if !jsonFormat && !c.Config.IsSameHost(origin) {
		serve500(c)
		log.Info().Str("Origin", origin).Msg("Invalid origin")
		return
	}
	query, err := parseSearchQueryParams(c.Request)
	if err != nil {
		c.Response.WriteHeader(http.StatusBadRequest)
		return
	}
	if jsonFormat && query.Text == "" {
		c.Response.WriteHeader(http.StatusBadRequest)
		_, _ = c.Response.Write([]byte(`{"error":"text query required for format=json"}`))
		return
	}
	if query.Text != "" {
		serveSearchHTTP(c, query)
		return
	}
	serveSearchWebSocket(c)
}

func doSearch(idx *indexer.Indexer, query *indexer.Query, rules *config.Rules, userID uint, includeHistory bool) (*indexer.Results, error) {
	start := time.Now()
	oq := query.Text
	res, err := searchIndex(idx, query, rules, userID)
	if err != nil {
		return nil, err
	}
	if includeHistory {
		hr, err := model.GetURLsByQuery(userID, oq)
		if err == nil && len(hr) > 0 {
			res.History = hr
			priorityByURL := make(map[string]*model.URLCount, len(hr))
			for _, h := range hr {
				priorityByURL[h.URL] = h
			}
			filtered := res.Documents[:0]
			for _, d := range res.Documents {
				if h, ok := priorityByURL[d.URL]; ok {
					if h.Text == "" {
						h.Text = d.Text
					}
					h.DocID = d.DocumentID
					continue
				}
				filtered = append(filtered, d)
			}
			res.Documents = filtered
		}
		if oq != "" {
			res.QuerySuggestion = model.GetQuerySuggestion(userID, oq)
		}
	}
	res.SearchDuration = formatSearchDuration(time.Since(start))
	return res, nil
}

func searchIndex(idx *indexer.Indexer, query *indexer.Query, rules *config.Rules, userID uint) (*indexer.Results, error) {
	if rules != nil {
		query.Text = rules.ResolveAliases(query.Text)
	}
	query.UserID = userID
	if rules != nil && rules.Priority != nil {
		query.PriorityPatterns = rules.Priority.ReStrs
	}
	return idx.Search(query)
}

func formatSearchDuration(duration time.Duration) string {
	seconds := duration.Round(10 * time.Millisecond).Seconds()
	return strconv.FormatFloat(seconds, 'f', -1, 64) + " seconds"
}

// applyPatchReverse reconstructs an older document by inverting the stored
// patch and applying the result to the current content. The diff-match-patch
// Patch struct has an unexported diffs field, so we work at the text level:
// swap the position fields in each @@ header and swap every leading '-'/'+'
// marker on diff lines. PatchApply uses fuzzy matching so position drift
// across multiple sequential applications is handled automatically.
// On any error it returns the content unchanged.
func applyPatchReverse(patchText, content string) string {
	if patchText == "" {
		return content
	}
	lines := strings.Split(patchText, "\n")
	reversed := make([]string, 0, len(lines))
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "@@ "):
			// "@@ -a,b +c,d @@" → "@@ -c,d +a,b @@"
			parts := strings.SplitN(line, " ", 4)
			if len(parts) == 4 && strings.HasPrefix(parts[1], "-") && strings.HasPrefix(parts[2], "+") {
				reversed = append(reversed, "@@ -"+parts[2][1:]+" +"+parts[1][1:]+" @@")
			} else {
				reversed = append(reversed, line)
			}
		case strings.HasPrefix(line, "+"):
			reversed = append(reversed, "-"+line[1:])
		case strings.HasPrefix(line, "-"):
			reversed = append(reversed, "+"+line[1:])
		default:
			reversed = append(reversed, line)
		}
	}
	dmp := diffmatchpatch.New()
	patches, err := dmp.PatchFromText(strings.Join(reversed, "\n"))
	if err != nil {
		return content
	}
	result, _ := dmp.PatchApply(patches, content)
	return result
}

// computeDocumentDiff returns diff match patch strings for the HTML and text
// fields independently. Either value may be empty when its content is
// identical between the two versions.
func computeDocumentDiff(old, new *document.Document) (htmlDiff, textDiff string) {
	dmp := diffmatchpatch.New()
	makePatch := func(oldContent, newContent string) string {
		if oldContent == newContent {
			return ""
		}
		diffs := dmp.DiffMain(oldContent, newContent, true)
		diffs = dmp.DiffCleanupSemantic(diffs)
		return dmp.PatchToText(dmp.PatchMake(oldContent, diffs))
	}
	htmlDiff = makePatch(old.HTML, new.HTML)
	textDiff = makePatch(old.Text, new.Text)
	return
}

// serveVersions returns all stored version diffs for a given URL and the
// authenticated user (or user 0 when user handling is disabled).
func serveVersions(c *webContext) {
	u := c.Request.URL.Query().Get("url")
	if u == "" {
		http.Error(c.Response, "url parameter is required", http.StatusBadRequest)
		return
	}
	doc := requestedDocument(c, u)
	if doc == nil {
		http.Error(c.Response, "document not found", http.StatusNotFound)
		return
	}
	versions, err := model.GetDocumentVersions(u, doc.UserID)
	if err != nil {
		log.Error().Err(err).Str("url", u).Msg("failed to get document versions")
		serve500(c)
		return
	}
	c.JSON(versions)
}

// shouldSkipSubmission checks URL skip rules unless the document carries an
// explicit override. Other document validation and authorization still apply.
func shouldSkipSubmission(c *webContext, d *document.Document) bool {
	return !d.IgnoreSkipRules() && c.effectiveRules().IsSkip(d.URL)
}

func serveAdd(c *webContext) {
	m := c.Request.Method
	if m == http.MethodGet {
		serve200(c)
		return
	}
	if m != http.MethodPost {
		serve500(c)
		return
	}
	d := &document.Document{}
	if strings.Contains(c.Request.Header.Get("Content-Type"), "json") {
		maxBodyBytes := c.Config.Server.MaxBatchBodyBytes()
		c.Request.Body = http.MaxBytesReader(c.Response, c.Request.Body, maxBodyBytes)
		if err := json.NewDecoder(c.Request.Body).Decode(d); err != nil {
			if maxBytesErr, ok := errors.AsType[*http.MaxBytesError](err); ok {
				http.Error(c.Response, fmt.Sprintf("request body exceeds the %d MiB limit", maxBytesErr.Limit>>20), http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(c.Response, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
	} else {
		if err := c.Request.ParseForm(); err != nil {
			serve500(c)
			return
		}
		f := c.Request.PostForm
		d.URL = f.Get("url")
		d.Title = f.Get("title")
		d.Text = f.Get("text")
	}
	if err := validateAddDocument(d); err != nil {
		http.Error(c.Response, err.Error(), http.StatusBadRequest)
		return
	}
	if !shouldSkipSubmission(c, d) && !c.Config.IsSameHost(d.URL) {
		d.UserID = submittedDocumentUserID(c)
		rules := c.effectiveRules()
		var existingDoc *document.Document
		if d.Type != document.RemoteFile && rules.IsVersioning(d.URL) {
			existingDoc = c.Indexer.GetByURLAndUser(d.URL, d.UserID)
		}
		err := c.Indexer.AddContext(c.Request.Context(), d)
		if err != nil {
			if errors.Is(err, document.ErrSensitiveContent) {
				log.Warn().Str("URL", d.URL).Msg("rejected document: sensitive content")
				http.Error(c.Response, document.ErrSensitiveContent.Error(), http.StatusUnprocessableEntity)
				return
			}
			log.Error().Err(err).Str("URL", d.URL).Msg("failed to create index")
			serve500(c)
			return
		}
		if existingDoc != nil {
			newDoc := c.Indexer.GetByURLAndUser(d.URL, d.UserID)
			if newDoc != nil {
				htmlDiff, textDiff := computeDocumentDiff(existingDoc, newDoc)
				if htmlDiff != "" || textDiff != "" {
					if err := model.SaveDocumentVersion(newDoc.URL, newDoc.UserID, htmlDiff, textDiff); err != nil {
						log.Warn().Err(err).Str("url", newDoc.URL).Msg("failed to save document version")
					}
				}
			}
		}
		c.Response.WriteHeader(http.StatusCreated)
	} else {
		log.Debug().Str("url", d.URL).Msg("skip indexing")
		c.Response.WriteHeader(http.StatusNotAcceptable)
	}
}

func validateAddDocument(d *document.Document) error {
	parsedURL, err := url.Parse(d.URL)
	if err != nil {
		return fmt.Errorf("invalid document URL: %w", err)
	}
	if d.Type != document.RemoteFile {
		if strings.EqualFold(parsedURL.Scheme, "remote-file") {
			return errors.New("remote-file URLs require the remote document type")
		}
		return nil
	}
	if parsedURL.Scheme != "remote-file" || parsedURL.Hostname() == "" {
		return errors.New("remote file URL must use the remote-file scheme and include a source host")
	}
	if parsedURL.User != nil || parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return errors.New("remote file URL must not contain user information, a query, or a fragment")
	}
	if parsedURL.Path == "" || parsedURL.Path == "/" || !strings.HasPrefix(parsedURL.Path, "/") {
		return errors.New("remote file URL must contain an absolute file path")
	}
	if d.Text == "" && d.HTML == "" {
		return errors.New("remote file document must contain extracted text or HTML")
	}

	// All remote snapshot fields that depend on processing or server storage are
	// derived again. This also prevents a submitted Processed value from
	// bypassing URL and sensitive content checks.
	d.DocumentID = ""
	d.Domain = ""
	d.HTMLKey = ""
	d.Favicon = ""
	d.FaviconKey = ""
	d.Score = 0
	d.Language = ""
	d.UserID = 0
	d.AddCount = 0
	d.Processed = false
	d.ExtraDocuments = nil
	d.SkipIndexing = false
	return nil
}

func serveAddPDF(c *webContext) {
	if c.Request.Method != http.MethodPost {
		serve500(c)
		return
	}

	var req struct {
		Document *document.Document `json:"document"`
		PDF      string             `json:"pdf"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		http.Error(c.Response, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Document == nil {
		http.Error(c.Response, "missing document field", http.StatusBadRequest)
		return
	}
	if req.PDF == "" {
		http.Error(c.Response, "missing pdf field", http.StatusBadRequest)
		return
	}
	pdfData, err := base64.StdEncoding.DecodeString(req.PDF)
	if err != nil {
		http.Error(c.Response, "pdf must be base64-encoded: "+err.Error(), http.StatusBadRequest)
		return
	}

	d := req.Document
	if shouldSkipSubmission(c, d) || c.Config.IsSameHost(d.URL) {
		log.Debug().Str("url", d.URL).Msg("skip indexing pdf")
		c.Response.WriteHeader(http.StatusNotAcceptable)
		return
	}

	d.UserID = submittedDocumentUserID(c)

	if err := c.Indexer.AddPDF(d, pdfData); err != nil {
		if errors.Is(err, document.ErrSensitiveContent) {
			log.Warn().Str("URL", d.URL).Msg("rejected pdf document: sensitive content")
			http.Error(c.Response, document.ErrSensitiveContent.Error(), http.StatusUnprocessableEntity)
			return
		}
		log.Error().Err(err).Str("URL", d.URL).Msg("failed to index pdf")
		serve500(c)
		return
	}

	log.Debug().Str("URL", d.URL).Msg("pdf added to index")
	c.Response.WriteHeader(http.StatusCreated)
}

func serveUpdateLabel(c *webContext) {
	var req struct {
		URL   string `json:"url"`
		Label string `json:"label"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		http.Error(c.Response, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.URL == "" {
		http.Error(c.Response, "missing url", http.StatusBadRequest)
		return
	}
	doc := c.Indexer.GetByURLAndUser(req.URL, c.UserID)
	if doc == nil {
		http.Error(c.Response, "document not found", http.StatusNotFound)
		return
	}
	doc.Label = req.Label
	if err := c.Indexer.Save(doc); err != nil {
		log.Error().Err(err).Str("url", req.URL).Msg("failed to save label")
		serve500(c)
		return
	}
	c.JSON(map[string]any{"ok": true})
}

func serveUpdateDocuments(c *webContext) {
	var req types.UpdateDocumentsRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		http.Error(c.Response, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Changes.UserID != nil {
		if !c.Config.App.UserHandling {
			http.Error(c.Response, "changing document ownership requires user handling", http.StatusBadRequest)
			return
		}
		if !c.IsAdmin {
			http.Error(c.Response, "admin permission required to change document ownership", http.StatusForbidden)
			return
		}
		if *req.Changes.UserID != 0 {
			if _, err := model.GetUserByID(*req.Changes.UserID); err != nil {
				http.Error(c.Response, "target user not found", http.StatusBadRequest)
				return
			}
		}
	}
	var sourceUserID *uint
	if c.Config.App.UserHandling && !c.IsAdmin {
		sourceUserID = &c.UserID
	}
	result, err := c.Indexer.UpdateByQuery(req.Query, sourceUserID, req.Changes, req.DryRun)
	if err != nil {
		switch {
		case errors.Is(err, indexer.ErrEmptyFilter),
			errors.Is(err, indexer.ErrEmptyUpdate),
			errors.Is(err, indexer.ErrInvalidLanguage):
			http.Error(c.Response, err.Error(), http.StatusBadRequest)
		case errors.Is(err, indexer.ErrFileURLNotAllowed):
			http.Error(c.Response, err.Error(), http.StatusConflict)
		default:
			log.Error().Err(err).Msg("document update failed")
			serve500(c)
		}
		return
	}
	if !req.DryRun {
		for _, change := range result.OwnershipChanges {
			if err := model.MoveDocumentVersions(change.URL, change.FromUserID, change.ToUserID); err != nil {
				log.Warn().Err(err).Str("url", change.URL).Msg("failed to move document versions to the new owner")
			}
		}
	}
	c.JSON(result)
}

func serveHistory(c *webContext) {
	if !historyEnabled(c) {
		c.Response.WriteHeader(http.StatusNotFound)
		return
	}
	rssFormat := c.Request.URL.Query().Get("format") == "rss"
	filter := strings.TrimSpace(c.Request.URL.Query().Get("filter"))
	dateFrom, dateTo, err := parseHistoryDateRange(c.Request)
	if err != nil {
		http.Error(c.Response, err.Error(), http.StatusBadRequest)
		return
	}
	if c.Request.URL.Query().Get("opened") == "true" {
		var lastID uint
		if v := c.Request.URL.Query().Get("last_id"); v != "" {
			if parsed, err := strconv.ParseUint(v, 10, 64); err == nil {
				lastID = uint(parsed)
			}
		}
		var lastUpdatedAt time.Time
		if value := c.Request.URL.Query().Get("last_updated_at"); value != "" {
			lastUpdatedAt, err = time.Parse(time.RFC3339Nano, value)
			if err != nil {
				http.Error(c.Response, "invalid last_updated_at", http.StatusBadRequest)
				return
			}
		}
		items, err := model.GetLatestHistoryItemsFilteredByDate(c.UserID, 100, lastID, lastUpdatedAt, filter, dateFrom, dateTo)
		if err != nil {
			serve500(c)
			return
		}
		type openedItem struct {
			ID       uint   `json:"id"`
			URL      string `json:"url"`
			Title    string `json:"title"`
			Query    string `json:"query"`
			Added    int64  `json:"added"`
			AddCount uint   `json:"add_count"`
		}
		type openedResponse struct {
			Documents     []*openedItem `json:"documents"`
			LastID        uint          `json:"last_id"`
			LastUpdatedAt string        `json:"last_updated_at,omitempty"`
		}
		docs := make([]*openedItem, 0, len(items))
		for _, item := range items {
			docs = append(docs, &openedItem{
				ID:       item.ID,
				URL:      item.URL,
				Title:    item.Title,
				Query:    item.Query,
				Added:    item.UpdatedAt.Unix(),
				AddCount: c.Indexer.GetAddCountByURLAndUser(item.URL, c.UserID),
			})
		}
		var nextLastID uint
		var nextLastUpdatedAt string
		if len(docs) > 0 {
			nextLastID = docs[len(docs)-1].ID
			nextLastUpdatedAt = items[len(items)-1].UpdatedAt.Format(time.RFC3339Nano)
		}
		if rssFormat {
			rssItems := make([]rssItem, 0, len(docs))
			for _, d := range docs {
				title := d.Title
				if title == "" {
					title = d.URL
				}
				rssItems = append(rssItems, rssItem{
					Title:   title,
					Link:    d.URL,
					GUID:    rssGUID{Value: d.URL, IsPermaLink: "true"},
					PubDate: time.Unix(d.Added, 0).UTC().Format(time.RFC1123Z),
				})
			}
			serveRSS(c, "Hister - visited pages", c.Config.BaseURL("/"), rssItems)
			return
		}
		c.JSON(&openedResponse{Documents: docs, LastID: nextLastID, LastUpdatedAt: nextLastUpdatedAt})
		return
	}
	ds := c.Indexer.GetLatestDocumentsFilteredByDate(100, c.Request.URL.Query().Get("last"), c.UserID, filter, dateFrom, dateTo)
	if rssFormat {
		var rssItems []rssItem
		if ds != nil {
			rssItems = make([]rssItem, 0, len(ds.Documents))
			for _, d := range ds.Documents {
				title := d.Title
				if title == "" {
					title = d.URL
				}
				rssItems = append(rssItems, rssItem{
					Title:   title,
					Link:    d.URL,
					GUID:    rssGUID{Value: d.URL, IsPermaLink: "true"},
					PubDate: time.Unix(d.Updated, 0).UTC().Format(time.RFC1123Z),
				})
			}
		}
		serveRSS(c, "Hister - indexed pages", c.Config.BaseURL("/"), rssItems)
		return
	}
	c.JSON(ds)
}

func parseHistoryDateRange(r *http.Request) (int64, int64, error) {
	var dateFrom, dateTo int64
	for name, target := range map[string]*int64{"date_from": &dateFrom, "date_to": &dateTo} {
		value := r.URL.Query().Get(name)
		if value == "" {
			continue
		}
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed < 0 {
			return 0, 0, fmt.Errorf("invalid %s", name)
		}
		*target = parsed
	}
	if dateFrom != 0 && dateTo != 0 && dateFrom >= dateTo {
		return 0, 0, errors.New("date_from must be earlier than date_to")
	}
	return dateFrom, dateTo, nil
}

func addTimelineTimestamps(result interface{ AddTimestamp(int64) }, timestamps []time.Time) {
	for _, timestamp := range timestamps {
		result.AddTimestamp(timestamp.Unix())
	}
}

func serveHistoryTimeline(c *webContext) {
	if !historyEnabled(c) {
		c.Response.WriteHeader(http.StatusNotFound)
		return
	}
	loc := time.Local
	if timezone := c.Request.URL.Query().Get("timezone"); timezone != "" {
		var err error
		loc, err = time.LoadLocation(timezone)
		if err != nil {
			http.Error(c.Response, "invalid timezone", http.StatusBadRequest)
			return
		}
	}
	filter := strings.TrimSpace(c.Request.URL.Query().Get("filter"))
	dateFrom, dateTo, err := parseHistoryDateRange(c.Request)
	if err != nil || (dateFrom == 0) != (dateTo == 0) {
		if err == nil {
			err = errors.New("date_from and date_to must be provided together")
		}
		http.Error(c.Response, err.Error(), http.StatusBadRequest)
		return
	}
	if dateFrom != 0 && dateTo-dateFrom > int64(32*24*time.Hour/time.Second) {
		http.Error(c.Response, "daily drilldown range cannot exceed 32 days", http.StatusBadRequest)
		return
	}
	if c.Request.URL.Query().Get("opened") == "true" {
		timestamps, err := model.GetHistoryItemTimestampsFilteredByDate(c.UserID, filter, dateFrom, dateTo)
		if err != nil {
			serve500(c)
			return
		}
		if dateFrom != 0 {
			result := timeline.NewDays(dateFrom, dateTo, loc)
			addTimelineTimestamps(result, timestamps)
			c.JSON(result)
			return
		}
		var oldest int64
		for _, timestamp := range timestamps {
			unix := timestamp.Unix()
			if oldest == 0 || unix < oldest {
				oldest = unix
			}
		}
		result := timeline.New(time.Now(), loc, oldest)
		addTimelineTimestamps(result, timestamps)
		c.JSON(result)
		return
	}
	if dateFrom != 0 {
		result, err := c.Indexer.GetHistoryTimelineDays(c.UserID, filter, loc, dateFrom, dateTo)
		if err != nil {
			serve500(c)
			return
		}
		c.JSON(result)
		return
	}
	result, err := c.Indexer.GetHistoryTimeline(c.UserID, filter, loc)
	if err != nil {
		serve500(c)
		return
	}
	c.JSON(result)
}

type rssGUID struct {
	Value       string `xml:",chardata"`
	IsPermaLink string `xml:"isPermaLink,attr,omitempty"`
}

type rssItem struct {
	Title   string  `xml:"title"`
	Link    string  `xml:"link"`
	GUID    rssGUID `xml:"guid"`
	PubDate string  `xml:"pubDate,omitempty"`
}

type rssChannel struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	Description string    `xml:"description"`
	Items       []rssItem `xml:"item"`
}

type rssFeed struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Channel rssChannel `xml:"channel"`
}

func serveRSS(c *webContext, title, link string, items []rssItem) {
	feed := rssFeed{
		Version: "2.0",
		Channel: rssChannel{
			Title:       title,
			Link:        link,
			Description: title,
			Items:       items,
		},
	}
	c.Response.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	if _, err := c.Response.Write([]byte(xml.Header)); err != nil {
		log.Warn().Err(err).Msg("failed to write RSS header")
		return
	}
	if err := xml.NewEncoder(c.Response).Encode(feed); err != nil {
		log.Warn().Err(err).Msg("failed to encode RSS feed")
	}
}

func serveSaveHistory(c *webContext) {
	if !historyEnabled(c) {
		c.Response.WriteHeader(http.StatusNoContent)
		return
	}
	h := &historyItem{}
	err := json.NewDecoder(c.Request.Body).Decode(h)
	if err != nil {
		serve500(c)
		return
	}
	if h.Delete {
		if err := model.DeleteHistoryItem(c.UserID, h.Query, h.URL); err != nil {
			serve500(c)
		}
		return
	}
	if h.Pin != nil {
		if err := model.SetHistoryPinned(c.UserID, strings.TrimSpace(h.Query), strings.TrimSpace(h.URL), strings.TrimSpace(h.Title), *h.Pin); err != nil {
			log.Error().Err(err).Msg("failed to update pin state")
			serve500(c)
		}
		return
	}
	err = model.UpdateHistory(c.UserID, strings.TrimSpace(h.Query), strings.TrimSpace(h.URL), strings.TrimSpace(h.Title))
	if err != nil {
		log.Error().Err(err).Msg("failed to update history")
		serve500(c)
		return
	}
}

// validatePatterns checks that each string in patterns is a valid Go regexp.
// Returns an error naming the first invalid pattern.
func validatePatterns(patterns []string) error {
	for _, p := range patterns {
		if _, err := regexp.Compile(p); err != nil {
			return fmt.Errorf("invalid pattern %q: %w", p, err)
		}
	}
	return nil
}

func serveRules(c *webContext) {
	m := c.Request.Method
	rules := c.effectiveRules()
	if m == http.MethodGet {
		type rulesResponse struct {
			Skip       []string          `json:"skip"`
			Priority   []string          `json:"priority"`
			Versioning []string          `json:"versioning"`
			Aliases    map[string]string `json:"aliases"`
		}
		skip := rules.Skip.ReStrs
		if skip == nil {
			skip = []string{}
		}
		priority := rules.Priority.ReStrs
		if priority == nil {
			priority = []string{}
		}
		versioning := rules.Versioning.ReStrs
		if versioning == nil {
			versioning = []string{}
		}
		aliases := map[string]string(rules.Aliases)
		if aliases == nil {
			aliases = make(map[string]string)
		}
		c.JSON(rulesResponse{Skip: skip, Priority: priority, Versioning: versioning, Aliases: aliases})
		return
	}
	if m != http.MethodPost {
		serve500(c)
		return
	}
	err := c.Request.ParseForm()
	if err != nil {
		serve500(c)
		return
	}
	f := c.Request.PostForm
	type ruleUpdate struct {
		patterns []string
		target   **config.Rule
	}
	updates := make([]ruleUpdate, 0, 3)
	for _, field := range []struct {
		label  string
		target **config.Rule
	}{
		{label: "skip", target: &rules.Skip},
		{label: "priority", target: &rules.Priority},
		{label: "versioning", target: &rules.Versioning},
	} {
		if !f.Has(field.label) {
			continue
		}
		patterns := uniqueStrings(strings.Fields(f.Get(field.label)))
		if err := validatePatterns(patterns); err != nil {
			http.Error(c.Response, fmt.Sprintf("%s: %s", field.label, err.Error()), http.StatusBadRequest)
			return
		}
		updates = append(updates, ruleUpdate{patterns: patterns, target: field.target})
	}
	for _, update := range updates {
		if *update.target == nil {
			*update.target = &config.Rule{ReStrs: make([]string, 0)}
		}
		(*update.target).ReStrs = update.patterns
	}
	if err := rules.Compile(); err != nil {
		log.Error().Err(err).Msg("failed to compile rules")
		serve500(c)
		return
	}
	if c.Config.App.UserHandling {
		if err := model.SaveUserRules(c.UserID, rules); err != nil {
			log.Error().Err(err).Msg("failed to save user rules")
			serve500(c)
			return
		}
		c.userRules = rules
	} else {
		if err := c.Config.SaveRules(); err != nil {
			log.Error().Err(err).Msg("failed to save rules")
			serve500(c)
			return
		}
	}
	serve200(c)
}

func serveGetFacets(c *webContext) {
	params := c.Request.URL.Query()
	q := &indexer.Query{
		Text:       params.Get("q"),
		Facets:     true,
		FacetsOnly: true,
		FacetSizes: make(map[string]int),
	}
	for param, field := range map[string]*int64{"date_from": &q.DateFrom, "date_to": &q.DateTo} {
		if v := params.Get(param); v != "" {
			if t, err := strconv.ParseInt(v, 10, 64); err == nil {
				*field = t
			}
		}
	}
	for key, vals := range params {
		if name, ok := strings.CutPrefix(key, "size_"); ok && len(vals) > 0 {
			_, configured := searchschema.Facet(name)
			if n, err := strconv.Atoi(vals[0]); configured && err == nil && n > 0 {
				q.FacetSizes[name] = n
			}
		}
	}
	res, err := doSearch(c.Indexer, q, c.effectiveRules(), c.UserID, historyEnabled(c))
	if err != nil || res.Facets == nil {
		c.JSON(map[string]any{})
		return
	}
	c.JSON(res.Facets)
}

func serveGet(c *webContext) {
	u := c.Request.URL.Query().Get("url")
	doc := requestedDocument(c, u)
	if doc == nil {
		http.Error(c.Response, "document not found", http.StatusNotFound)
		return
	}
	// We skip generating the body on HEAD requests, since those only check the status.
	// Note that we want to return the same status as a GET request, so **no faillible processing**
	// is to be made inside of this block!
	if c.Request.Method != "HEAD" {
		c.JSON(doc)
	}
}

// requestedDocument resolves an optional document ID. Multiuser mode enforces
// owner isolation. URL only requests retain the legacy behavior of preferring
// the caller's document over a global document.
func requestedDocument(c *webContext, u string) *document.Document {
	id := c.Request.URL.Query().Get("document_id")
	if id == "" {
		return c.Indexer.GetByURLAndUser(u, c.UserID)
	}
	doc := c.Indexer.GetByDocID(id)
	if doc == nil || doc.URL != u {
		return nil
	}
	if c.Config.App.UserHandling && doc.UserID != 0 && doc.UserID != c.UserID {
		return nil
	}
	return doc
}

func servePreview(c *webContext) {
	u := c.Request.URL.Query().Get("url")
	extractorName := c.Request.URL.Query().Get("extractor")
	versionIDStr := c.Request.URL.Query().Get("version")
	doc := requestedDocument(c, u)
	if doc == nil {
		http.Error(c.Response, "document not found", http.StatusNotFound)
		return
	}
	// If a specific version is requested, reconstruct the older document content
	// by applying the stored diffs in reverse order.
	var viewingVersionID uint
	var viewingVersionCreatedAt time.Time
	if versionIDStr != "" {
		if versionID, err := strconv.ParseUint(versionIDStr, 10, 64); err == nil {
			if versions, err := model.GetDocumentVersionsUntil(u, doc.UserID, uint(versionID)); err == nil && len(versions) > 0 {
				docCopy := *doc
				for _, v := range versions {
					docCopy.HTML = applyPatchReverse(v.HTMLDiff, docCopy.HTML)
					docCopy.Text = applyPatchReverse(v.TextDiff, docCopy.Text)
				}
				doc = &docCopy
				// versions is ordered newest first; the last element is the target version.
				target := versions[len(versions)-1]
				viewingVersionID = target.ID
				viewingVersionCreatedAt = target.CreatedAt
			}
		}
	}
	var resp sdk.PreviewResponse
	var err error
	if doc.HTML == "" {
		resp = sdk.PreviewResponse{Content: doc.Text}
	} else {
		resp, err = extractor.PreviewContext(c.Request.Context(), doc, extractorName)
		if err != nil {
			if errors.Is(err, extractor.ErrNoExtractor) {
				http.Error(c.Response, err.Error(), http.StatusBadRequest)
				return
			}
			log.Warn().Err(err).Str("url", u).Msg("failed to generate preview")
			serve500(c)
			return
		}
	}
	metadata := doc.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	payload := types.DocumentPreviewResponse{
		Title:    doc.Title,
		Content:  resp.Content,
		Template: resp.Template,
		Added:    doc.Added,
		Updated:  doc.Updated,
		Details: types.PreviewDocumentDetails{
			Type:     doc.Type.String(),
			Language: doc.Language,
			Label:    doc.Label,
			Visits:   doc.AddCount,
			UserID:   doc.UserID,
			Metadata: metadata,
		},
	}
	if viewingVersionID > 0 {
		payload.VersionID = viewingVersionID
		payload.VersionCreatedAt = &viewingVersionCreatedAt
	}
	if versionCount, err := model.CountDocumentVersions(u, doc.UserID); err == nil && versionCount > 0 {
		payload.VersionCount = versionCount
	}
	if meta := doc.GetPreviewMeta(); meta != nil {
		payload.Meta = meta
	}
	c.JSON(payload)
}

func serveFile(c *webContext) {
	id := c.Request.URL.Query().Get("id")
	if id == "" {
		http.Error(c.Response, "missing id parameter", http.StatusBadRequest)
		return
	}
	doc := c.Indexer.GetByDocID(id)
	filePath, dir, ok := authorizedFilePath(c, id, doc)
	if !ok {
		http.Error(c.Response, "file not found", http.StatusNotFound)
		return
	}

	// Resolve symlinks to prevent a symlink inside a configured directory
	// from serving files outside it
	resolvedPath, err := filepath.EvalSymlinks(filePath)
	if err != nil {
		http.Error(c.Response, "file not found", http.StatusNotFound)
		return
	}

	// Verify the resolved file remains within its configured directory.
	resolvedDir, err := filepath.EvalSymlinks(filepath.Clean(files.ExpandHome(dir.Path)))
	if err != nil || !files.HasPathPrefix(resolvedPath, resolvedDir) {
		http.Error(c.Response, "file not found", http.StatusNotFound)
		return
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		http.Error(c.Response, "file not found", http.StatusNotFound)
		return
	}

	ext := filepath.Ext(filePath)
	mimeType := mime.TypeByExtension(ext)
	if mimeType == "" {
		mimeType = "text/plain; charset=utf-8"
	}
	c.Response.Header().Set("Content-Type", mimeType)
	if _, err := c.Response.Write(content); err != nil {
		log.Warn().Err(err).Msg("failed to write file response")
	}
}

func authorizedFilePath(c *webContext, id string, doc *document.Document) (string, *config.Directory, bool) {
	if doc == nil || doc.Type != document.Local || document.GetDocID(doc.UserID, doc.URL) != id {
		return "", nil, false
	}
	if doc.UserID != 0 && doc.UserID != c.UserID {
		return "", nil, false
	}
	pu, err := url.Parse(doc.URL)
	if err != nil || !strings.EqualFold(pu.Scheme, "file") {
		return "", nil, false
	}
	filePath := filepath.Clean(files.FileURLToPath(doc.URL))
	if !filepath.IsAbs(filePath) {
		return "", nil, false
	}
	dir := files.FindMatchingDir(c.Config.Indexer.Directories, filePath)
	if !files.DirectoryMatchesPath(dir, filePath) {
		return "", nil, false
	}
	ownerID, err := files.FindDirUser(c.Config.Indexer.Directories, filePath)
	if err != nil || ownerID != doc.UserID {
		return "", nil, false
	}
	return filePath, dir, true
}

func serveAPI(c *webContext) {
	type endpointInfo struct {
		Name         string             `json:"name"`
		Path         string             `json:"path"`
		Method       string             `json:"method"`
		CSRFRequired bool               `json:"csrf_required"`
		Public       bool               `json:"public"`
		RequiresAuth bool               `json:"requires_auth"`
		Mutates      bool               `json:"mutates"`
		Description  string             `json:"description"`
		Args         []*EndpointArg     `json:"args,omitempty"`
		JSONSchema   []*JSONSchemaField `json:"json_schema,omitempty"`
	}
	var result []endpointInfo
	for _, e := range Endpoints {
		result = append(result, endpointInfo{
			Name:         e.Name,
			Path:         e.Path,
			Method:       e.Method,
			CSRFRequired: e.CSRFRequired,
			Public:       e.Public,
			RequiresAuth: endpointRequiresAuth(c.Config, e),
			Mutates:      endpointMutates(e),
			Description:  e.Description,
			Args:         e.Args,
			JSONSchema:   e.JSONSchema,
		})
	}
	c.JSON(result)
}

func endpointMutates(e *Endpoint) bool {
	if e.Method != http.MethodGet && e.Method != http.MethodHead {
		return true
	}
	return e.Path == "/api/add"
}

func serveStats(c *webContext) {
	var hs []*model.HistoryItem
	if historyEnabled(c) {
		hs, _ = model.GetLatestHistoryItems(c.UserID, 5, 0)
	}
	var docCount, fileCount uint64
	if c.Config.App.UserHandling {
		docCount = c.Indexer.TotalByUser(c.UserID)
		fileCount = c.Indexer.TotalFilesByUser(c.UserID)
	} else {
		docCount = c.Indexer.Total()
		fileCount = c.Indexer.TotalFiles()
	}
	rules := c.effectiveRules()
	resp := map[string]any{
		"doc_count":       docCount,
		"file_count":      fileCount,
		"rule_count":      rules.Count(),
		"alias_count":     len(rules.Aliases),
		"recent_searches": hs,
	}
	if c.Config.App.Public && !c.Authenticated {
		resp["rule_count"] = 0
		resp["alias_count"] = 0
		delete(resp, "recent_searches")
	}
	c.JSON(resp)
}

func serveExtractors(c *webContext) {
	u := c.Request.URL.Query().Get("url")
	if u != "" {
		doc := requestedDocument(c, u)
		if doc == nil {
			http.Error(c.Response, "document not found", http.StatusNotFound)
			return
		}
		c.JSON(extractor.ListMatchingPreview(doc))
		return
	}
	infos := extractor.List()
	if !c.Config.App.DisplayExtractorConfig {
		for i := range infos {
			infos[i].Options = nil
		}
	}
	c.JSON(infos)
}

func serveOpensearch(c *webContext) {
	baseURL := strings.TrimSuffix(c.Config.BaseURL("/"), "/")
	xml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<OpenSearchDescription xmlns="http://a9.com/-/spec/opensearch/1.1/">
  <ShortName>Hister</ShortName>
  <Description>Search your history with Hister</Description>
  <Url type="text/html" template="%s/?q={searchTerms}"/>
  <Url type="application/x-suggestions+json" template="%s/suggest?q={searchTerms}"/>
</OpenSearchDescription>`, baseURL, baseURL)
	c.Response.Header().Set("Content-Type", "application/xml")
	if _, err := c.Response.Write([]byte(xml)); err != nil {
		log.Warn().Err(err).Msg("failed to write opensearch response")
	}
}

const suggestLimit = 10

func serveSuggest(c *webContext) {
	// Sec-Fetch-Site is set by browsers and forbidden to JS, so a cross-site
	// fetch() can't spoof it. Browser address-bar flows either omit the header
	// (Firefox) or send "none" (Chrome); reject anything explicitly cross-site.
	switch c.Request.Header.Get("Sec-Fetch-Site") {
	case "", "none", "same-origin", "same-site":
	default:
		c.Response.WriteHeader(http.StatusForbidden)
		return
	}
	q := c.Request.URL.Query().Get("q")
	suggestions := []string{}
	descriptions := []string{}
	suggestionURLs := []string{}
	if q != "" {
		res, err := doSearch(c.Indexer, &indexer.Query{
			Text:  q,
			Limit: suggestLimit,
		}, c.effectiveRules(), c.UserID, historyEnabled(c))
		if err != nil {
			log.Warn().Err(err).Msg("suggest search failed")
		}
		if res != nil {
			for _, h := range res.History {
				if len(suggestions) >= suggestLimit {
					break
				}
				title := strings.TrimSpace(h.Title)
				if title == "" {
					title = h.URL
				}
				suggestions = append(suggestions, title)
				descriptions = append(descriptions, h.URL)
				suggestionURLs = append(suggestionURLs, historySuggestionURL(c.Config, h))
			}
			for _, d := range res.Documents {
				if len(suggestions) >= suggestLimit {
					break
				}
				title := strings.TrimSpace(d.Title)
				if title == "" {
					title = d.URL
				}
				suggestions = append(suggestions, title)
				descriptions = append(descriptions, d.URL)
				suggestionURLs = append(suggestionURLs, suggestionURL(c.Config, d))
			}
		}
	}
	jr, err := json.Marshal([]any{q, suggestions, descriptions, suggestionURLs})
	if err != nil {
		log.Warn().Err(err).Msg("failed to marshal suggest response")
		return
	}
	c.Response.Header().Set("Content-Type", "application/x-suggestions+json")
	if _, err := c.Response.Write(jr); err != nil {
		log.Warn().Err(err).Msg("failed to write suggest response")
	}
}

func suggestionURL(cfg *config.Config, d *document.Document) string {
	switch d.Type {
	case document.Local:
		if d.DocumentID != "" {
			return cfg.BaseURL("/api/file?id=" + url.QueryEscape(d.DocumentID))
		}
		return previewSuggestionURL(cfg, d.URL, d.Title)
	case document.RemoteFile:
		return previewSuggestionURL(cfg, d.URL, d.Title)
	default:
		return d.URL
	}
}

func historySuggestionURL(cfg *config.Config, h *model.URLCount) string {
	d := &document.Document{
		DocumentID: h.DocID,
		URL:        h.URL,
		Title:      h.Title,
	}
	if parsed, err := url.Parse(h.URL); err == nil {
		switch strings.ToLower(parsed.Scheme) {
		case "file":
			d.Type = document.Local
		case "remote-file":
			d.Type = document.RemoteFile
		}
	}
	return suggestionURL(cfg, d)
}

func previewSuggestionURL(cfg *config.Config, documentURL, title string) string {
	values := url.Values{"id": {documentURL}}
	if title = strings.TrimSpace(title); title != "" {
		values.Set("title", title)
	}
	return cfg.BaseURL("/preview?" + values.Encode())
}

func serveAddAlias(c *webContext) {
	err := c.Request.ParseForm()
	if err != nil {
		serve500(c)
		return
	}
	f := c.Request.PostForm
	keyword, value := f.Get("alias-keyword"), f.Get("alias-value")
	if keyword == "" || value == "" {
		serve200(c)
		return
	}
	rules := c.effectiveRules()
	rules.Aliases[keyword] = value
	if c.Config.App.UserHandling {
		if err := model.SaveUserRules(c.UserID, rules); err != nil {
			log.Error().Err(err).Msg("failed to save user rules")
			serve500(c)
			return
		}
		c.userRules = rules
	} else {
		if err := c.Config.SaveRules(); err != nil {
			log.Error().Err(err).Msg("failed to save rules")
			serve500(c)
			return
		}
	}
	serve200(c)
}

func serveDeleteAlias(c *webContext) {
	err := c.Request.ParseForm()
	if err != nil {
		serve500(c)
		return
	}
	a := c.Request.PostForm.Get("alias")
	rules := c.effectiveRules()
	if _, ok := rules.Aliases[a]; !ok {
		serve500(c)
		return
	}
	delete(rules.Aliases, a)
	if c.Config.App.UserHandling {
		if err := model.SaveUserRules(c.UserID, rules); err != nil {
			log.Error().Err(err).Msg("failed to save user rules")
			serve500(c)
			return
		}
		c.userRules = rules
	} else {
		if err := c.Config.SaveRules(); err != nil {
			log.Error().Err(err).Msg("failed to save rules")
			serve500(c)
			return
		}
	}
	serve200(c)
}

func serveDelete(c *webContext) {
	var req struct {
		Query  string `json:"query"`
		DryRun bool   `json:"dry_run"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		http.Error(c.Response, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	// Non-admin users may only delete their own documents.
	var userID *uint
	if c.Config.App.UserHandling && !c.IsAdmin {
		userID = &c.UserID
	}
	var count int
	var err error
	if req.DryRun {
		count, err = c.Indexer.CountByQuery(req.Query, userID)
	} else {
		count, err = c.Indexer.DeleteByQuery(req.Query, userID, func(url string, uid uint) {
			if err := model.DeleteHistoryURL(uid, url); err != nil {
				log.Warn().Err(err).Str("url", url).Msg("failed to delete history for deleted document")
			}
		})
	}
	if err != nil {
		if errors.Is(err, indexer.ErrEmptyFilter) || errors.Is(err, querybuilder.ErrInvalidRegexp) {
			http.Error(c.Response, err.Error(), http.StatusBadRequest)
			return
		}
		log.Error().Err(err).Msg("delete failed")
		serve500(c)
		return
	}
	if req.DryRun {
		c.JSON(map[string]any{"matched": count})
		return
	}
	c.JSON(map[string]any{"deleted": count})
}

type batchOp struct {
	Op string `json:"op"`
	document.Document
}

type batchOpResult struct {
	Status   int                `json:"status"`
	Error    string             `json:"error,omitempty"`
	Document *document.Document `json:"document,omitempty"`
}

type batchRequest struct {
	Ops []batchOp `json:"ops"`
}

type batchResponse struct {
	Results    []batchOpResult `json:"results,omitempty"`
	Error      string          `json:"error,omitempty"`
	Code       string          `json:"code,omitempty"`
	LimitBytes int64           `json:"limit_bytes,omitempty"`
}

const (
	maxBatchOps = 100
	batchOpAdd  = "add"
	batchOpDel  = "delete"
	batchOpGet  = "get"
)

// TODO handle data dir updates
func serveBatch(c *webContext) {
	maxBodyBytes := c.Config.Server.MaxBatchBodyBytes()
	c.Request.Body = http.MaxBytesReader(c.Response, c.Request.Body, maxBodyBytes)
	var req batchRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		if maxBytesErr, ok := errors.AsType[*http.MaxBytesError](err); ok {
			c.JSONStatus(http.StatusRequestEntityTooLarge, batchResponse{
				Error:      fmt.Sprintf("request body exceeds the %d MiB limit", maxBytesErr.Limit>>20),
				Code:       "request_too_large",
				LimitBytes: maxBytesErr.Limit,
			})
			return
		}
		c.JSONStatus(http.StatusBadRequest, batchResponse{Error: "invalid JSON"})
		return
	}

	if len(req.Ops) == 0 {
		c.JSONStatus(http.StatusBadRequest, batchResponse{Error: "empty batch"})
		return
	}

	if len(req.Ops) > maxBatchOps {
		c.JSONStatus(http.StatusBadRequest, batchResponse{Error: "too many operations (max 100)"})
		return
	}

	batch := c.Indexer.NewMultiBatch()
	uid := targetUserID(c)
	results := make([]batchOpResult, len(req.Ops))
	for i, op := range req.Ops {
		switch op.Op {
		case batchOpAdd:
			if op.URL == "" {
				results[i] = batchOpResult{Status: http.StatusBadRequest, Error: "missing url"}
				continue
			}
			d := &op.Document
			d.UserID = uid
			if shouldSkipSubmission(c, d) || strings.HasPrefix(d.URL, c.Config.BaseURL("/")) {
				results[i] = batchOpResult{Status: http.StatusNotAcceptable, Error: "url skipped by rules"}
				continue
			}
			if err := batch.AddContext(c.Request.Context(), d); err != nil {
				if errors.Is(err, document.ErrSensitiveContent) {
					log.Warn().Str("URL", op.URL).Msg("rejected document: sensitive content")
					results[i] = batchOpResult{Status: http.StatusUnprocessableEntity, Error: document.ErrSensitiveContent.Error()}
				} else {
					log.Error().Err(err).Str("URL", op.URL).Msg("batch add error")
					results[i] = batchOpResult{Status: http.StatusInternalServerError, Error: "internal error"}
				}
			} else {
				results[i] = batchOpResult{Status: http.StatusCreated}
			}
		case batchOpDel:
			if op.URL == "" {
				results[i] = batchOpResult{Status: http.StatusBadRequest, Error: "missing url"}
				continue
			}
			id := document.GetDocID(uid, op.URL)
			if d := c.Indexer.GetByDocID(id); d != nil {
				if err := model.DeleteHistoryURL(d.UserID, d.URL); err != nil {
					log.Warn().Err(err).Str("url", d.URL).Msg("failed to delete history for batch deleted document")
				}
			}
			batch.Delete(id)
			results[i] = batchOpResult{Status: http.StatusOK}
		case batchOpGet:
			if op.URL == "" {
				results[i] = batchOpResult{Status: http.StatusBadRequest, Error: "missing url"}
				continue
			}
			d := c.Indexer.GetByURLAndUser(op.URL, uid)
			if d == nil {
				results[i] = batchOpResult{Status: http.StatusNotFound, Error: "document not found"}
			} else {
				results[i] = batchOpResult{Status: http.StatusOK, Document: d}
			}
		default:
			results[i] = batchOpResult{Status: http.StatusBadRequest, Error: fmt.Sprintf("unknown op: %q", op.Op)}
		}
	}

	if err := batch.Save(); err != nil {
		log.Error().Err(err).Msg("batch save error")
		c.JSONStatus(http.StatusInternalServerError, batchResponse{Error: "internal error"})
		return
	}

	log.Debug().Int("ops", len(req.Ops)).Msg("batch request processed")
	c.JSON(batchResponse{Results: results})
}

type reindexRequest struct {
	SkipSensitive   bool `json:"skipSensitive"`
	DetectLanguages bool `json:"detectLanguages"`
}

func serveReindex(c *webContext) {
	var req reindexRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		serve500(c)
		return
	}
	if err := c.Indexer.ReindexContext(c.Request.Context(), c.Config.Rules, req.SkipSensitive, req.DetectLanguages, c.Config.Indexer.KeepStopwords, c.Config.Indexer.Directories); err != nil {
		log.Error().Err(err).Msg("reindex failed")
		serve500(c)
		return
	}
	serve200(c)
}

func serveCleanup(c *webContext) {
	result, err := c.Indexer.Cleanup(c.Config.Indexer.Directories)
	if err != nil {
		log.Error().Err(err).Msg("cleanup failed")
		serve500(c)
		return
	}
	c.JSON(result)
}

func serveFavicon(c *webContext) {
	i, err := iofs.ReadFile(appSubFS, "favicon.ico")
	if err != nil {
		serve500(c)
		return
	}
	c.Response.Header().Add("Content-Type", "image/vnd.microsoft.icon")
	c.Response.Header().Set("Cache-Control", webUICacheControl("favicon.ico"))
	if _, err := c.Response.Write(i); err != nil {
		log.Warn().Err(err).Msg("failed to write favicon response")
	}
}

const storedFaviconCacheControl = "public, max-age=604800, immutable"

func serveStoredFavicon(c *webContext) {
	key := c.Request.URL.Query().Get("key")
	dataURI, err := c.Indexer.ReadFavicon(key)
	if err != nil {
		http.Error(c.Response, "favicon not found", http.StatusNotFound)
		return
	}
	contentType, data, err := decodeFaviconDataURI(string(dataURI))
	if err != nil {
		http.Error(c.Response, "invalid favicon data", http.StatusInternalServerError)
		return
	}
	c.Response.Header().Set("Content-Type", contentType)
	c.Response.Header().Set("Cache-Control", storedFaviconCacheControl)
	c.Response.Header().Set("ETag", `"`+key+`"`)
	if _, err := c.Response.Write(data); err != nil {
		log.Warn().Err(err).Str("key", key).Msg("failed to write stored favicon response")
	}
}

func decodeFaviconDataURI(dataURI string) (string, []byte, error) {
	if !strings.HasPrefix(dataURI, "data:") {
		return http.DetectContentType([]byte(dataURI)), []byte(dataURI), nil
	}
	meta, payload, ok := strings.Cut(dataURI, ",")
	if !ok {
		return "", nil, errors.New("invalid data URI")
	}
	contentType := strings.TrimPrefix(meta, "data:")
	base64Encoded := false
	if mediaType, params, found := strings.Cut(contentType, ";"); found {
		contentType = mediaType
		for param := range strings.SplitSeq(params, ";") {
			if strings.EqualFold(param, "base64") {
				base64Encoded = true
				break
			}
		}
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if !base64Encoded {
		data := []byte(payload)
		return contentType, data, nil
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return "", nil, err
	}
	return contentType, data, nil
}

func serveStatic(c *webContext) {
	assetPath := strings.TrimPrefix(c.Request.URL.Path, "/static/")
	if _, err := iofs.Stat(appSubFS, assetPath); err == nil {
		c.Response.Header().Set("Cache-Control", webUICacheControl(assetPath))
	}
	staticFileServer.ServeHTTP(c.Response, c.Request)
}

func serve200(c *webContext) {
	c.Response.WriteHeader(http.StatusOK)
}

// uniqueStrings returns a copy of ss with duplicate entries removed,
// preserving the first occurrence of each value.
func uniqueStrings(ss []string) []string {
	seen := make(map[string]struct{}, len(ss))
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}

func serve403(c *webContext) {
	c.Response.WriteHeader(http.StatusForbidden)
}

func serve500(c *webContext) {
	http.Error(c.Response, "Internal Server Error", http.StatusInternalServerError)
}

func (c *webContext) JSON(o any) {
	c.Response.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(c.Response).Encode(o); err != nil {
		log.Error().Err(err).Msg("failed to encode JSON response")
	}
}

func (c *webContext) JSONStatus(status int, o any) {
	c.Response.Header().Set("Content-Type", "application/json")
	c.Response.WriteHeader(status)
	if err := json.NewEncoder(c.Response).Encode(o); err != nil {
		log.Error().Err(err).Msg("failed to encode JSON response")
	}
}

func (c *webContext) Redirect(u string) {
	http.Redirect(c.Response, c.Request, u, http.StatusFound)
}
