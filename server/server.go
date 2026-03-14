package server

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	iofs "io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/files"
	"github.com/asciimoo/hister/server/indexer"
	"github.com/asciimoo/hister/server/model"
	"github.com/asciimoo/hister/server/static"

	readability "codeberg.org/readeck/go-readability/v2"
	"github.com/gorilla/sessions"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

var (
	appSubFS         iofs.FS
	staticFileServer http.Handler
	sessionStore     *sessions.CookieStore
	errCSRFMismatch  = errors.New("CSRF token mismatch")
	storeName        = "hister"
	tokName          = "csrf_token"
	staticTextFiles  map[string][]byte
)

type historyItem struct {
	URL    string `json:"url"`
	Title  string `json:"title"`
	Query  string `json:"query"`
	Delete bool   `json:"delete"`
}

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

func (lrw *loggingResponseWriter) Header() http.Header {
	return lrw.ResponseWriter.Header()
}

func (lrw *loggingResponseWriter) Write(d []byte) (int, error) {
	return lrw.ResponseWriter.Write(d)
}

func (lrw *loggingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := lrw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("hijacking not supported")
	}
	return hj.Hijack()
}

var ws = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type webContext struct {
	Request  *http.Request
	Response http.ResponseWriter
	Config   *config.Config
	nonce    string
	csrf     string
}

func init() {
	sub, err := iofs.Sub(static.FS, "app")
	if err != nil {
		panic(err)
	}
	staticTextFiles = make(map[string][]byte)
	appSubFS = sub
	staticFileServer = http.StripPrefix("/static/", http.FileServerFS(appSubFS))
}

func parseStaticFiles(baseDir string) error {
	files, err := static.FS.ReadDir("app")
	if err != nil {
		return err
	}
	return recParseStaticFiles(files, "app", baseDir)
	//cspHashes := make([]string, 0, len(staticTextFiles))
	//for n, c := range staticTextFiles {
	//	if strings.HasSuffix(n, ".js") {
	//		h := sha256.New()
	//		h.Write(c)
	//		s := h.Sum(nil)
	//		cspHashes = append(cspHashes, fmt.Sprintf("'sha256-%s'", base64.StdEncoding.EncodeToString(s)))
	//	}
	//}
	//cspValues = fmt.Sprintf("script-src 'strict-dynamic' %s", strings.Join(cspHashes, " "))
}

func recParseStaticFiles(entries []iofs.DirEntry, dir, baseDir string) error {
	for _, e := range entries {
		if e.IsDir() {
			subDir := path.Join(dir, e.Name())
			sd, err := static.FS.ReadDir(subDir)
			if err != nil {
				return err
			}
			if err := recParseStaticFiles(sd, subDir, baseDir); err != nil {
				return err
			}
			continue
		}
		fn := e.Name()
		if strings.HasSuffix(fn, ".html") || strings.HasSuffix(fn, ".js") || strings.HasSuffix(fn, ".css") {
			p := path.Join(dir, fn)
			c, err := static.FS.ReadFile(p)
			if err != nil {
				return err
			}
			k := strings.TrimPrefix(p, "app/")
			staticTextFiles[k] = bytes.ReplaceAll(c, []byte("/magic-string-that-we-replace-runtime-in-the-app"), []byte(baseDir))
		}
	}
	return nil
}

func Listen(cfg *config.Config) {
	sessionStore = sessions.NewCookieStore(cfg.SecretKey()[:32])
	sessionStore.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   60 * 60 * 24 * 365,
		HttpOnly: true,
	}

	// This is an ugly hack required to set the base path dynamically in svelte files.
	// Svelte only supports build time specification of the base path and it accepts
	// only absolute paths: https://github.com/sveltejs/kit/issues/9569#issuecomment-3202269382
	//
	// Related issues for more details:
	//  - https://codeberg.org/asciimoo/hister/issues/7
	//  - https://github.com/asciimoo/hister/issues/147
	if err := parseStaticFiles(cfg.BasePathPrefix()); err != nil {
		panic(err)
	}

	handler := registerEndpoints(cfg)
	handler = withLogging(handler)

	log.Info().Str("Address", cfg.Server.Address).Str("URL", cfg.BaseURL("/")).Msg("Starting webserver")
	err := http.ListenAndServe(cfg.Server.Address, handler)
	if err != nil {
		log.Error().Err(err).Msg("Webserver failed to listen on " + cfg.Server.Address)
	}
}

func registerEndpoints(cfg *config.Config) http.Handler {
	mux := http.NewServeMux()
	auth := cfg.App.AccessToken != ""

	for _, e := range Endpoints {
		log.Debug().Str("Endpoint", e.Pattern()).Msg("Registering endpoint")
		h := e.Handler
		if e.CSRFRequired {
			h = withCSRF(h)
		}
		if auth {
			h = withAuth(h)
		}
		mux.HandleFunc(e.Pattern(), createHandler(cfg, h))
	}
	// SPA catch-all: serve index.html for any path not matched above
	mux.HandleFunc("GET /static/", createHandler(cfg, serveStatic))
	mux.HandleFunc("GET /favicon.ico", createHandler(cfg, serveFavicon))
	mux.HandleFunc("GET /opensearch.xml", createHandler(cfg, serveOpensearch))
	mux.HandleFunc("/", createHandler(cfg, serveSPA))
	// If base_url contains a non-root path prefix (e.g. https://x.com/subfolder),
	// accept requests both with and without that prefix.
	basePrefix := cfg.BasePathPrefix()
	if basePrefix != "" {
		return withOptionalBasePathPrefix(basePrefix, mux)
	}
	return mux
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

func createHandler(cfg *config.Config, h func(*webContext)) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		c := &webContext{
			Request:  r,
			Response: w,
			Config:   cfg,
			nonce:    rand.Text(),
		}
		h(c)
	}
}

func withAuth(handler endpointHandler) endpointHandler {
	return func(c *webContext) {
		session, err := sessionStore.Get(c.Request, storeName)
		if err != nil {
			serve403(c)
			return
		}
		if t, ok := session.Values["access_token"].(string); ok && t == c.Config.App.AccessToken {
			handler(c)
			return
		}
		if c.Request.Header.Get("X-Access-Token") != c.Config.App.AccessToken {
			serve403(c)
			return
		}
		session.Values["access_token"] = c.Config.App.AccessToken
		err = session.Save(c.Request, c.Response)
		if err != nil {
			serve500(c)
			return
		}
		handler(c)
	}
}

func withCSRF(handler endpointHandler) endpointHandler {
	return func(c *webContext) {
		// Allow requests coming from the command line
		if c.Request.Header.Get("Origin") == "hister://" {
			handler(c)
			return
		}
		// Allow requests coming from the same site
		if c.Request.Header.Get("Sec-Fetch-Site") == "same-origin" {
			handler(c)
			return
		}
		// Allow add/batch requests from the addons
		p := strings.TrimPrefix(c.Request.URL.Path, c.Config.BasePathPrefix())
		if p == "/add" || p == "/api/add" || p == "/api/batch" {
			if strings.HasPrefix(c.Request.Header.Get("Origin"), "moz-extension://") {
				handler(c)
				return
			}
			if c.Request.Header.Get("Origin") == "chrome-extension://cciilamhchpmbdnniabclekddabkifhb" {
				handler(c)
				return
			}
		}

		session, err := sessionStore.Get(c.Request, storeName)
		if err != nil {
			http.Error(c.Response, err.Error(), http.StatusInternalServerError)
			return
		}
		method := c.Request.Method
		origin := c.Request.Header.Get("Origin")
		safeRequest := c.Config.IsSameHost(origin) || origin == "same-origin"
		if method != http.MethodGet && method != http.MethodHead && !safeRequest {
			sToken, ok := session.Values[tokName].(string)
			if !ok {
				http.Error(c.Response, errCSRFMismatch.Error(), http.StatusInternalServerError)
				return
			}
			token := c.Request.PostFormValue(tokName)
			if token == "" {
				token = c.Request.Header.Get("X-CSRF-Token")
			}
			if token != sToken {
				http.Error(c.Response, errCSRFMismatch.Error(), http.StatusInternalServerError)
				return
			}
		}
		tok := rand.Text()
		session.Values[tokName] = tok
		err = session.Save(c.Request, c.Response)
		if err != nil {
			http.Error(c.Response, err.Error(), http.StatusInternalServerError)
			return
		}
		c.csrf = tok
		c.Response.Header().Add("X-CSRF-Token", tok)
		handler(c)
	}
}

func withLogging(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lrw := &loggingResponseWriter{w, http.StatusOK}
		h.ServeHTTP(lrw, r)
		log.Info().Str("Method", r.Method).Int("Status", lrw.statusCode).Dur("LoadTimeMS", time.Since(start)).Str("URL", r.RequestURI).Msg("WEB")
	})
}

func serveIndex(c *webContext) {
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

// serveSPA serves the SPA index.html for any route not matching a static file.
func serveSPA(c *webContext) {
	path := strings.TrimPrefix(c.Request.URL.Path, "/")
	if path == "index.html" {
		serveIndex(c)
		return
	}
	if content, ok := staticTextFiles[path]; ok {
		ext := filepath.Ext(path)
		if mimeType := mime.TypeByExtension(ext); mimeType != "" {
			c.Response.Header().Set("Content-Type", mimeType)
		} else {
			// Default to application/octet-stream if we can't detect the type
			c.Response.Header().Set("Content-Type", "application/octet-stream")
		}
		c.Response.WriteHeader(http.StatusOK)
		if _, err := c.Response.Write(content); err != nil {
			log.Warn().Err(err).Msg("failed to write static text response")
		}
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
		// Detect and set proper MIME type
		ext := filepath.Ext(path)
		if mimeType := mime.TypeByExtension(ext); mimeType != "" {
			c.Response.Header().Set("Content-Type", mimeType)
		} else {
			// Default to application/octet-stream if we can't detect the type
			c.Response.Header().Set("Content-Type", "application/octet-stream")
		}
		c.Response.WriteHeader(http.StatusOK)
		if _, err := c.Response.Write(content); err != nil {
			log.Warn().Err(err).Msg("failed to write static file response")
		}
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
	if q != "" {
		res, err := indexer.Search(c.Config, &indexer.Query{
			Text: c.Config.Rules.ResolveAliases(q),
		})
		if err != nil {
			res = &indexer.Results{}
		}
		hr, err := model.GetURLsByQuery(q)
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

// serveConfig returns app configuration as JSON and refreshes CSRF token.
func serveConfig(c *webContext) {
	type configResponse struct {
		BaseURL             string            `json:"baseUrl"`
		BasePath            string            `json:"basePath"`
		WsURL               string            `json:"wsUrl"`
		SearchURL           string            `json:"searchUrl"`
		OpenResultsOnNewTab bool              `json:"openResultsOnNewTab"`
		Hotkeys             map[string]string `json:"hotkeys"`
	}
	hotkeys := c.Config.Hotkeys.Web
	if hotkeys == nil {
		hotkeys = make(map[string]string)
	}
	c.JSON(configResponse{
		BaseURL:             c.Config.BaseURL(""),
		BasePath:            c.Config.BasePathPrefix(),
		WsURL:               c.Config.WebSocketURL(),
		SearchURL:           c.Config.App.SearchURL,
		OpenResultsOnNewTab: c.Config.App.OpenResultsOnNewTab,
		Hotkeys:             hotkeys,
	})
}

func serveSearch(c *webContext) {
	origin := c.Request.Header.Get("Origin")
	if !c.Config.IsSameHost(origin) {
		serve500(c)
		log.Info().Str("Origin", origin).Msg("Invalid origin")
		return
	}
	q := c.Request.URL.Query().Get("q")
	if q != "" {
		query := &indexer.Query{Text: q}
		for param, field := range map[string]*int64{"date_from": &query.DateFrom, "date_to": &query.DateTo} {
			if v := c.Request.URL.Query().Get(param); v != "" {
				if t, err := time.Parse("2006-01-02", v); err == nil {
					*field = t.Unix()
				}
			}
		}
		r, err := doSearch(query, c.Config)
		if err != nil {
			fmt.Println(err)
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
		return
	}
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
		_, q, err := conn.ReadMessage()
		if err != nil {
			log.Error().Err(err).Msg("failed to read websocket message")
			break
		}
		var query *indexer.Query
		err = json.Unmarshal(q, &query)
		if err != nil {
			log.Error().Err(err).Msg("failed to parse query")
			continue
		}
		res, err := doSearch(query, c.Config)
		if err != nil {
			log.Error().Err(err).Msg("search error")
			continue
		}
		jr, err := json.Marshal(res)
		if err != nil {
			log.Error().Err(err).Msg("failed to marshal indexer results")
		}
		if err := conn.WriteMessage(websocket.TextMessage, jr); err != nil {
			log.Error().Err(err).Msg("failed to write websocket message")
			break
		}
	}
}

func doSearch(query *indexer.Query, cfg *config.Config) (*indexer.Results, error) {
	start := time.Now()
	oq := query.Text
	query.Text = cfg.Rules.ResolveAliases(query.Text)
	res, err := indexer.Search(cfg, query)
	if err != nil {
		log.Error().Err(err).Msg("failed to get indexer results")
	}
	if res == nil {
		res = &indexer.Results{}
	}
	hr, err := model.GetURLsByQuery(oq)
	if err == nil && len(hr) > 0 {
		res.History = hr
	}
	if oq != "" {
		res.QuerySuggestion = model.GetQuerySuggestion(oq)
	}
	if len(cfg.Indexer.Directories) > 0 {
		for _, doc := range res.Documents {
			if cfp, cut := strings.CutPrefix(doc.URL, "file://"); cut {
				doc.URL = cfg.BaseURL("/api/file?path=") + url.QueryEscape(cfp)
			}
		}
	}
	duration := float32(time.Since(start).Milliseconds()) / 1000.
	res.SearchDuration = fmt.Sprintf("%.3f seconds", duration)
	return res, nil
}

func serveBatch(c *webContext) {
	type batchRequest struct {
		Documents []*indexer.Document `json:"documents"`
		History   []historyItem       `json:"history"`
	}
	type batchResponse struct {
		Indexed int      `json:"indexed"`
		Skipped int      `json:"skipped"`
		Errors  []string `json:"errors,omitempty"`
	}

	var req batchRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		http.Error(c.Response, "invalid JSON body", http.StatusBadRequest)
		return
	}

	if len(req.Documents)+len(req.History) == 0 {
		c.JSON(batchResponse{})
		return
	}
	if len(req.Documents)+len(req.History) > 100 {
		http.Error(c.Response, "batch size exceeds 100 items", http.StatusBadRequest)
		return
	}

	var resp batchResponse

	// Filter and index documents
	if len(req.Documents) > 0 {
		docs := make([]*indexer.Document, 0, len(req.Documents))
		for _, d := range req.Documents {
			if d.URL == "" {
				resp.Skipped++
				resp.Errors = append(resp.Errors, "document missing URL")
				continue
			}
			if c.Config.Rules.IsSkip(d.URL) || strings.HasPrefix(d.URL, c.Config.BaseURL("/")) {
				resp.Skipped++
				continue
			}
			docs = append(docs, d)
		}
		if len(docs) > 0 {
			indexed, skipped, errs := indexer.AddBatch(docs)
			resp.Indexed += indexed
			resp.Skipped += skipped
			for _, e := range errs {
				resp.Errors = append(resp.Errors, e.Error())
			}
		}
	}

	// Process history items
	for _, h := range req.History {
		if h.Delete {
			if err := model.DeleteHistoryItem(h.Query, h.URL); err != nil {
				resp.Errors = append(resp.Errors, err.Error())
			}
			continue
		}
		if err := model.UpdateHistory(strings.TrimSpace(h.Query), strings.TrimSpace(h.URL), strings.TrimSpace(h.Title)); err != nil {
			resp.Errors = append(resp.Errors, err.Error())
		}
	}

	c.JSON(resp)
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
	d := &indexer.Document{}
	jsonData := false
	if strings.Contains(c.Request.Header.Get("Content-Type"), "json") {
		jsonData = true
		err := json.NewDecoder(c.Request.Body).Decode(d)
		if err != nil {
			serve500(c)
			return
		}
	} else {
		err := c.Request.ParseForm()
		if err != nil {
			serve500(c)
			return
		}
		f := c.Request.PostForm
		d.URL = f.Get("url")
		d.Title = f.Get("title")
		d.Text = f.Get("text")
	}
	if !c.Config.Rules.IsSkip(d.URL) && !strings.HasPrefix(d.URL, c.Config.BaseURL("/")) {
		err := indexer.Add(d)
		log.Debug().Str("URL", d.URL).Msg("item added to index")
		if err != nil {
			log.Error().Err(err).Str("URL", d.URL).Msg("failed to create index")
			serve500(c)
			return
		}
		c.Response.WriteHeader(http.StatusCreated)
	} else {
		log.Debug().Str("url", d.URL).Msg("skip indexing")
		c.Response.WriteHeader(http.StatusNotAcceptable)
	}
	if jsonData {
		return
	}
	serve200(c)
}

func serveHistory(c *webContext) {
	m := c.Request.Method
	if m == http.MethodGet {
		hs, err := model.GetLatestHistoryItems(40)
		if err != nil {
			serve500(c)
			return
		}
		c.JSON(hs)
		return
	}
	if m != http.MethodPost {
		serve500(c)
		return
	}
	h := &historyItem{}
	err := json.NewDecoder(c.Request.Body).Decode(h)
	if err != nil {
		serve500(c)
		return
	}
	if h.Delete {
		if err := model.DeleteHistoryItem(h.Query, h.URL); err != nil {
			serve500(c)
		}
		return
	}
	err = model.UpdateHistory(strings.TrimSpace(h.Query), strings.TrimSpace(h.URL), strings.TrimSpace(h.Title))
	if err != nil {
		log.Error().Err(err).Msg("failed to update history")
		serve500(c)
		return
	}
}

func serveRules(c *webContext) {
	m := c.Request.Method
	if m == http.MethodGet {
		type rulesResponse struct {
			Skip     []string          `json:"skip"`
			Priority []string          `json:"priority"`
			Aliases  map[string]string `json:"aliases"`
		}
		skip := c.Config.Rules.Skip.ReStrs
		if skip == nil {
			skip = []string{}
		}
		priority := c.Config.Rules.Priority.ReStrs
		if priority == nil {
			priority = []string{}
		}
		aliases := map[string]string(c.Config.Rules.Aliases)
		if aliases == nil {
			aliases = make(map[string]string)
		}
		c.JSON(rulesResponse{Skip: skip, Priority: priority, Aliases: aliases})
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
	c.Config.Rules.Skip.ReStrs = strings.Fields(f.Get("skip"))
	c.Config.Rules.Priority.ReStrs = strings.Fields(f.Get("priority"))
	err = c.Config.SaveRules()
	if err != nil {
		log.Error().Err(err).Msg("failed to save rules")
		serve500(c)
		return
	}
	serve200(c)
}

func serveGet(c *webContext) {
	u := c.Request.URL.Query().Get("url")
	doc := indexer.GetByURL(u)
	if doc == nil {
		serve500(c)
		return
	}
	// We skip generating the body on HEAD requests, since those only check the status.
	// Note that we want to return the same status as a GET request, so **no faillible processing**
	// is to be made inside of this block!
	if c.Request.Method != "HEAD" {
		c.JSON(doc)
	}
}

func serveReadable(c *webContext) {
	u := c.Request.URL.Query().Get("url")
	doc := indexer.GetByURL(u)
	if doc == nil {
		serve500(c)
		return
	}
	pu, err := url.Parse(u)
	if err != nil {
		serve500(c)
		return
	}
	r, err := readability.FromReader(strings.NewReader(doc.HTML), pu)
	if err != nil {
		serve500(c)
		return
	}
	var htmlContent strings.Builder
	if err := r.RenderHTML(&htmlContent); err != nil {
		log.Warn().Err(err).Msg("failed to render readable HTML")
	}
	title := doc.Title
	if r.Title() != "" {
		title = r.Title()
	}
	c.JSON(map[string]string{
		"title":   title,
		"content": htmlContent.String(),
	})
}

func serveFile(c *webContext) {
	filePath := c.Request.URL.Query().Get("path")
	if filePath == "" {
		http.Error(c.Response, "missing path parameter", http.StatusBadRequest)
		return
	}

	// Resolve to absolute and clean the path to prevent traversal
	filePath = filepath.Clean(filePath)
	if !filepath.IsAbs(filePath) {
		http.Error(c.Response, "path must be absolute", http.StatusBadRequest)
		return
	}

	// Resolve symlinks to prevent a symlink inside a configured directory
	// from serving files outside it
	resolvedPath, err := filepath.EvalSymlinks(filePath)
	if err != nil {
		http.Error(c.Response, "file not found", http.StatusNotFound)
		return
	}

	// Verify the resolved file is within a configured directory (also resolved)
	allowed := false
	for _, dir := range c.Config.Indexer.Directories {
		expandedDir := filepath.Clean(files.ExpandHome(dir.Path))
		resolvedDir, err := filepath.EvalSymlinks(expandedDir)
		if err != nil {
			continue
		}
		if strings.HasPrefix(resolvedPath, resolvedDir+"/") || resolvedPath == resolvedDir {
			allowed = true
			break
		}
	}
	if !allowed {
		http.Error(c.Response, "file not in configured directories", http.StatusForbidden)
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

func serveAPI(c *webContext) {
	type endpointArg struct {
		Name        string `json:"name"`
		Type        string `json:"type"`
		Required    bool   `json:"required"`
		Description string `json:"description"`
	}
	type endpointInfo struct {
		Name         string         `json:"name"`
		Path         string         `json:"path"`
		Method       string         `json:"method"`
		CSRFRequired bool           `json:"csrf_required"`
		Description  string         `json:"description"`
		Args         []*endpointArg `json:"args"`
	}
	var result []endpointInfo
	for _, e := range Endpoints {
		info := endpointInfo{
			Name:         e.Name,
			Path:         e.Path,
			Method:       e.Method,
			CSRFRequired: e.CSRFRequired,
			Description:  e.Description,
		}
		for _, a := range e.Args {
			info.Args = append(info.Args, &endpointArg{
				Name:        a.Name,
				Type:        a.Type,
				Required:    a.Required,
				Description: a.Description,
			})
		}
		result = append(result, info)
	}
	c.JSON(result)
}

func serveStats(c *webContext) {
	hs, _ := model.GetLatestHistoryItems(5)
	c.JSON(map[string]any{
		"doc_count":       indexer.DocumentCount(),
		"rule_count":      c.Config.Rules.Count(),
		"alias_count":     len(c.Config.Rules.Aliases),
		"recent_searches": hs,
	})
}

func serveOpensearch(c *webContext) {
	baseURL := strings.TrimSuffix(c.Config.BaseURL("/"), "/")
	xml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<OpenSearchDescription xmlns="http://a9.com/-/spec/opensearch/1.1/">
  <ShortName>Hister</ShortName>
  <Description>Search your history with Hister</Description>
  <Url type="text/html" template="%s/?q={searchTerms}"/>
</OpenSearchDescription>`, baseURL)
	c.Response.Header().Set("Content-Type", "application/xml")
	if _, err := c.Response.Write([]byte(xml)); err != nil {
		log.Warn().Err(err).Msg("failed to write opensearch response")
	}
}

func serveAddAlias(c *webContext) {
	err := c.Request.ParseForm()
	if err != nil {
		serve500(c)
		return
	}
	f := c.Request.PostForm
	if f.Get("alias-keyword") != "" && f.Get("alias-value") != "" {
		c.Config.Rules.Aliases[f.Get("alias-keyword")] = f.Get("alias-value")
	}
	err = c.Config.SaveRules()
	if err != nil {
		log.Error().Err(err).Msg("failed to save rules")
		serve500(c)
		return
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
	if _, ok := c.Config.Rules.Aliases[a]; !ok {
		serve500(c)
		return
	}
	delete(c.Config.Rules.Aliases, a)
	if err := c.Config.SaveRules(); err != nil {
		log.Error().Err(err).Msg("failed to save rules")
		serve500(c)
	}
	serve200(c)
}

func serveDeleteDocument(c *webContext) {
	err := c.Request.ParseForm()
	if err != nil {
		serve500(c)
		return
	}
	u := c.Request.PostForm.Get("url")
	if err := indexer.Delete(u); err != nil {
		log.Error().Err(err).Str("URL", u).Msg("failed to delete URL")
	}
	serve200(c)
}

func serveFavicon(c *webContext) {
	i, err := iofs.ReadFile(appSubFS, "favicon.ico")
	if err != nil {
		serve500(c)
		return
	}
	c.Response.Header().Add("Content-Type", "image/vnd.microsoft.icon")
	if _, err := c.Response.Write(i); err != nil {
		log.Warn().Err(err).Msg("failed to write favicon response")
	}
}

func serveStatic(c *webContext) {
	staticFileServer.ServeHTTP(c.Response, c.Request)
}

func serve200(c *webContext) {
	c.Response.WriteHeader(http.StatusOK)
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

func (c *webContext) Redirect(u string) {
	http.Redirect(c.Response, c.Request, u, http.StatusFound)
}
