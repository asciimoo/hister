package server

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/gob"
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
	"strconv"
	"strings"
	"time"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/files"
	"github.com/asciimoo/hister/server/enricher"
	"github.com/asciimoo/hister/server/indexer"
	"github.com/asciimoo/hister/server/indexer/types"
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
	multiUserNotSupportedMsg = map[string]string{"error": "rule management is not yet supported in multi-user mode"}
	enricherManager          *enricher.Manager
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
	Request   *http.Request
	Response  http.ResponseWriter
	Config    *config.Config
	nonce     string
	csrf      string
	UserID    uint
	Username  string
	IsAdmin   bool
	userRules *config.Rules
}

func (c *webContext) effectiveRules() *config.Rules {
	if c.Config.App.UserHandling && c.userRules != nil {
		return c.userRules
	}
	return c.Config.Rules
}

func init() {
	gob.Register(uint(0))
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

	if cfg.Enrichment.Enabled {
		enricherManager = enricher.New(
			cfg.Enrichment.Workers,
			cfg.Enrichment.Timeout,
			func(url string, result *enricher.Result, existing *enricher.ExistingDoc) error {
				d := &indexer.Document{
					URL:        url,
					Title:      result.Title,
					Text:       result.Text,
					HTML:       result.StoredData,
					Type:       types.Media,
					Properties: result.Properties,
				}
				if existing != nil {
					d.Domain = existing.Domain
					d.Favicon = existing.Favicon
					d.Added = existing.Added
					d.Language = existing.Language
				}
				return indexer.AddEnriched(d)
			},
			func(url string) *enricher.ExistingDoc {
				d := indexer.GetByURL(url)
				if d == nil {
					return nil
				}
				return &enricher.ExistingDoc{
					Domain:   d.Domain,
					Favicon:  d.Favicon,
					Added:    d.Added,
					Language: d.Language,
				}
			},
		)
		log.Info().Msg("Enrichment enabled")
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
	if enricherManager != nil {
		enricherManager.Close()
	}
}

func registerEndpoints(cfg *config.Config) http.Handler {
	mux := http.NewServeMux()
	tokenAuth := cfg.App.AccessToken != ""
	userHandling := cfg.App.UserHandling

	for _, e := range Endpoints {
		log.Debug().Str("Endpoint", e.Pattern()).Msg("Registering endpoint")
		h := e.Handler
		if e.CSRFRequired {
			h = withCSRF(h)
		}
		if tokenAuth && !userHandling {
			h = withTokenAuth(h)
		} else if userHandling && !e.NoAuth {
			if e.AdminOnly {
				h = withAdminAuth(h)
			} else {
				h = withUserAuth(h)
			}
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
		if cfg.App.UserHandling {
			populateUserContext(c)
		}
		h(c)
	}
}

func withTokenAuth(handler endpointHandler) endpointHandler {
	return func(c *webContext) {
		session, err := sessionStore.Get(c.Request, storeName)
		if err != nil && session == nil {
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

func populateUserContext(c *webContext) {
	session, err := sessionStore.Get(c.Request, storeName)
	if err != nil && session == nil {
		return
	}
	if uid, ok := session.Values["user_id"].(uint); ok && uid > 0 {
		c.UserID = uid
	}
	if name, ok := session.Values["username"].(string); ok {
		c.Username = name
	}
	if c.UserID == 0 {
		if tok := c.Request.Header.Get("X-Access-Token"); tok != "" {
			if u, err := model.GetUserByToken(tok); err == nil {
				c.UserID = u.ID
				c.Username = u.Username
				c.IsAdmin = u.IsAdmin
				if rules, err := u.ParseRules(); err == nil {
					c.userRules = rules
				}
			}
		}
		return
	}
	if u, err := model.GetUserByID(c.UserID); err == nil {
		c.IsAdmin = u.IsAdmin
		if rules, err := u.ParseRules(); err == nil {
			c.userRules = rules
		}
	}
}

func withUserAuth(handler endpointHandler) endpointHandler {
	return func(c *webContext) {
		if c.UserID == 0 {
			serve403(c)
			return
		}
		handler(c)
	}
}

func withAdminAuth(handler endpointHandler) endpointHandler {
	return func(c *webContext) {
		if c.UserID == 0 {
			serve403(c)
			return
		}
		if !c.IsAdmin {
			log.Warn().Msg("Admin permission required")
			serve403(c)
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
		// Allow add, config requests from the addons
		for _, p := range []string{"/add", "/api/add", "/api/config", "/api/rules"} {
			if c.Request.URL.Path != c.Config.BasePathPrefix()+p {
				continue
			}
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
		if err != nil && session == nil {
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
	if err != nil && session == nil {
		serve500(c)
		return
	}
	session.Values["user_id"] = user.ID
	session.Values["username"] = user.Username
	if err := session.Save(c.Request, c.Response); err != nil {
		serve500(c)
		return
	}
	c.JSON(map[string]string{"username": user.Username})
}

func serveLogout(c *webContext) {
	session, err := sessionStore.Get(c.Request, storeName)
	if err != nil && session == nil {
		serve500(c)
		return
	}
	delete(session.Values, "user_id")
	delete(session.Values, "username")
	if err := session.Save(c.Request, c.Response); err != nil {
		serve500(c)
		return
	}
	serve200(c)
}

func serveProfile(c *webContext) {
	if c.Config.App.UserHandling {
		c.JSON(map[string]any{
			"user_id":  c.UserID,
			"username": c.Username,
			"is_admin": c.IsAdmin,
		})
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
		BaseURL             string            `json:"baseUrl"`
		BasePath            string            `json:"basePath"`
		WsURL               string            `json:"wsUrl"`
		SearchURL           string            `json:"searchUrl"`
		OpenResultsOnNewTab bool              `json:"openResultsOnNewTab"`
		Hotkeys             map[string]string `json:"hotkeys"`
		AuthMode            string            `json:"authMode"`
		Username            string            `json:"username,omitempty"`
		UserID              uint              `json:"userId,omitempty"`
	}
	authMode := "none"
	if c.Config.App.UserHandling {
		authMode = "user"
	} else if c.Config.App.AccessToken != "" {
		authMode = "token"
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
		AuthMode:            authMode,
		Username:            c.Username,
		UserID:              c.UserID,
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
					ts := t.Unix()
					if param == "date_to" {
						// Include the entire end date by advancing to end of day (23:59:59)
						ts += 24*60*60 - 1
					}
					*field = ts
				}
			}
		}
		r, err := doSearch(query, c.Config, c.effectiveRules(), c.UserID)
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
		res, err := doSearch(query, c.Config, c.effectiveRules(), c.UserID)
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

func doSearch(query *indexer.Query, cfg *config.Config, rules *config.Rules, userID uint) (*indexer.Results, error) {
	start := time.Now()
	oq := query.Text
	query.Text = rules.ResolveAliases(query.Text)
	query.UserID = userID
	res, err := indexer.Search(cfg, query)
	if err != nil {
		log.Error().Err(err).Msg("failed to get indexer results")
	}
	if res == nil {
		res = &indexer.Results{}
	}
	hr, err := model.GetURLsByQuery(userID, oq)
	if err == nil && len(hr) > 0 {
		res.History = hr
	}
	if oq != "" {
		res.QuerySuggestion = model.GetQuerySuggestion(userID, oq)
	}
	for _, doc := range res.Documents {
		if doc.Type != types.Local {
			continue
		}
		if cfp, cut := strings.CutPrefix(doc.URL, "file://"); cut {
			doc.URL = cfg.BaseURL("/api/file?path=") + url.QueryEscape(cfp)
		}
	}
	duration := float32(time.Since(start).Milliseconds()) / 1000.
	res.SearchDuration = fmt.Sprintf("%.3f seconds", duration)
	return res, nil
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
	if !c.effectiveRules().IsSkip(d.URL) && !strings.HasPrefix(d.URL, c.Config.BaseURL("/")) {
		d.UserID = c.UserID
		err := indexer.Add(d)
		log.Debug().Str("URL", d.URL).Msg("item added to index")
		if err != nil {
			log.Error().Err(err).Str("URL", d.URL).Msg("failed to create index")
			serve500(c)
			return
		}
		if enricherManager != nil {
			enricherManager.Enqueue(d.URL, d.Domain)
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
	if c.Request.URL.Query().Get("opened") == "true" {
		var lastID uint
		if v := c.Request.URL.Query().Get("last_id"); v != "" {
			if parsed, err := strconv.ParseUint(v, 10, 64); err == nil {
				lastID = uint(parsed)
			}
		}
		items, err := model.GetLatestHistoryItems(c.UserID, 100, lastID)
		if err != nil {
			serve500(c)
			return
		}
		type openedItem struct {
			ID    uint   `json:"id"`
			URL   string `json:"url"`
			Title string `json:"title"`
			Query string `json:"query"`
			Added int64  `json:"added"`
		}
		type openedResponse struct {
			Documents []*openedItem `json:"documents"`
			LastID    uint          `json:"last_id"`
		}
		docs := make([]*openedItem, 0, len(items))
		for _, item := range items {
			docs = append(docs, &openedItem{
				ID:    item.ID,
				URL:   item.URL,
				Title: item.Title,
				Query: item.Query,
				Added: item.UpdatedAt.Unix(),
			})
		}
		var nextLastID uint
		if len(docs) > 0 {
			nextLastID = docs[len(docs)-1].ID
		}
		c.JSON(&openedResponse{Documents: docs, LastID: nextLastID})
		return
	}
	ds := indexer.GetLatestDocuments(100, c.Request.URL.Query().Get("last"))
	c.JSON(ds)
}

func serveSaveHistory(c *webContext) {
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
	err = model.UpdateHistory(c.UserID, strings.TrimSpace(h.Query), strings.TrimSpace(h.URL), strings.TrimSpace(h.Title))
	if err != nil {
		log.Error().Err(err).Msg("failed to update history")
		serve500(c)
		return
	}
}

func serveRules(c *webContext) {
	m := c.Request.Method
	rules := c.effectiveRules()
	if m == http.MethodGet {
		type rulesResponse struct {
			Skip     []string          `json:"skip"`
			Priority []string          `json:"priority"`
			Aliases  map[string]string `json:"aliases"`
		}
		skip := rules.Skip.ReStrs
		if skip == nil {
			skip = []string{}
		}
		priority := rules.Priority.ReStrs
		if priority == nil {
			priority = []string{}
		}
		aliases := map[string]string(rules.Aliases)
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
	rules.Skip.ReStrs = strings.Fields(f.Get("skip"))
	rules.Priority.ReStrs = strings.Fields(f.Get("priority"))
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

func serveGet(c *webContext) {
	u := c.Request.URL.Query().Get("url")
	doc := indexer.GetByURL(u)
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

func serveReadable(c *webContext) {
	u := c.Request.URL.Query().Get("url")
	doc := indexer.GetByURL(u)
	if doc == nil {
		serve500(c)
		return
	}

	// If this document was enriched, return structured properties
	if enricher.IsStoredEnrichment(doc.HTML) {
		enricherName, _ := enricher.ParseStoredEnricherName(doc.HTML)
		c.JSON(map[string]any{
			"title":      doc.Title,
			"properties": doc.Properties,
			"enricher":   enricherName,
		})
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
	c.JSON(map[string]any{
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
	hs, _ := model.GetLatestHistoryItems(c.UserID, 5, 0)
	var docCount uint64
	if c.Config.App.UserHandling {
		docCount = indexer.DocumentCountByUser(c.UserID)
	} else {
		docCount = indexer.DocumentCount()
	}
	rules := c.effectiveRules()
	c.JSON(map[string]any{
		"doc_count":       docCount,
		"rule_count":      rules.Count(),
		"alias_count":     len(rules.Aliases),
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
		Query string `json:"query"`
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
	count, err := indexer.DeleteByQuery(req.Query, userID)
	if err != nil {
		if errors.Is(err, indexer.ErrEmptyFilter) {
			http.Error(c.Response, err.Error(), http.StatusBadRequest)
			return
		}
		log.Error().Err(err).Msg("delete failed")
		serve500(c)
		return
	}
	c.JSON(map[string]any{"deleted": count})
}

type batchOp struct {
	Op      string `json:"op"`
	URL     string `json:"url"`
	Title   string `json:"title"`
	Text    string `json:"text"`
	HTML    string `json:"html"`
	Favicon string `json:"favicon"`
}

type batchOpResult struct {
	Status   int               `json:"status"`
	Error    string            `json:"error,omitempty"`
	Document *indexer.Document `json:"document,omitempty"`
}

type batchRequest struct {
	Ops []batchOp `json:"ops"`
}

type batchResponse struct {
	Results []batchOpResult `json:"results,omitempty"`
	Error   string          `json:"error,omitempty"`
}

const (
	maxBatchOps = 100
	batchOpAdd  = "add"
	batchOpDel  = "delete"
	batchOpGet  = "get"
)

func serveBatch(c *webContext) {
	c.Request.Body = http.MaxBytesReader(c.Response, c.Request.Body, 5<<20) // 5 MB
	var req batchRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
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

	batch := indexer.NewMultiBatch()
	results := make([]batchOpResult, len(req.Ops))
	for i, op := range req.Ops {
		switch op.Op {
		case batchOpAdd:
			if op.URL == "" {
				results[i] = batchOpResult{Status: http.StatusBadRequest, Error: "missing url"}
				continue
			}
			d := &indexer.Document{URL: op.URL, Title: op.Title, Text: op.Text, HTML: op.HTML, Favicon: op.Favicon}
			if c.effectiveRules().IsSkip(d.URL) || strings.HasPrefix(d.URL, c.Config.BaseURL("/")) {
				results[i] = batchOpResult{Status: http.StatusNotAcceptable, Error: "url skipped by rules"}
				continue
			}
			if err := batch.Add(d); err != nil {
				log.Error().Err(err).Str("URL", op.URL).Msg("batch add error")
				results[i] = batchOpResult{Status: http.StatusInternalServerError, Error: "internal error"}
			} else {
				results[i] = batchOpResult{Status: http.StatusCreated}
			}
		case batchOpDel:
			if op.URL == "" {
				results[i] = batchOpResult{Status: http.StatusBadRequest, Error: "missing url"}
				continue
			}
			batch.Delete(op.URL)
			results[i] = batchOpResult{Status: http.StatusOK}
		case batchOpGet:
			if op.URL == "" {
				results[i] = batchOpResult{Status: http.StatusBadRequest, Error: "missing url"}
				continue
			}
			d := indexer.GetByURL(op.URL)
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

	if enricherManager != nil {
		for i, op := range req.Ops {
			if op.Op == batchOpAdd && results[i].Status == http.StatusCreated {
				pu, err := url.Parse(op.URL)
				if err == nil && pu.Host != "" {
					enricherManager.Enqueue(op.URL, pu.Host)
				}
			}
		}
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
	if err := indexer.Reindex(c.Config.FullPath(""), c.Config.Rules, req.SkipSensitive, req.DetectLanguages, c.Config.Indexer.Directories); err != nil {
		log.Error().Err(err).Msg("reindex failed")
		serve500(c)
		return
	}
	if err := model.SetIndexerVersion(indexer.Version); err != nil {
		log.Error().Err(err).Msg("failed to update indexer version")
		serve500(c)
		return
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
