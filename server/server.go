package server

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	iofs "io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/asciimoo/hister/config"
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
	spaFileServer    http.Handler
	staticFileServer http.Handler
	sessionStore     *sessions.CookieStore
	errCSRFMismatch  = errors.New("CSRF token mismatch")
	storeName        = "hister"
	tokName          = "csrf_token"
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
	appSubFS = sub
	spaFileServer = http.FileServerFS(appSubFS)
	staticFileServer = http.StripPrefix("/static/", http.FileServerFS(appSubFS))
}

func Listen(cfg *config.Config) {
	sessionStore = sessions.NewCookieStore(cfg.SecretKey()[:32])
	sessionStore.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   60 * 60 * 24 * 365,
		HttpOnly: true,
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
	for _, e := range Endpoints {
		log.Debug().Str("Endpoint", e.Pattern()).Msg("Registering endpoint")
		h := e.Handler
		if e.CSRFRequired {
			h = withCSRF(h)
		}
		mux.HandleFunc(e.Pattern(), createHandler(cfg, h))
	}
	// SPA catch-all: serve index.html for any path not matched above
	mux.HandleFunc("/", createHandler(cfg, serveSPA))
	return mux
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

func withCSRF(handler endpointHandler) endpointHandler {
	return func(c *webContext) {
		// Allow requests coming from the command line
		if c.Request.Header.Get("Origin") == "hister://" {
			handler(c)
			return
		}
		// Allow /add requests from the addons
		if c.Request.URL.Path == "/add" {
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
		safeRequest := strings.HasPrefix(c.Request.Header.Get("Origin"), c.Config.BaseURL("/")) || c.Request.Header.Get("Origin") == "same-origin"
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

// serveSPA serves the SPA index.html for any route not matching a static file.
func serveSPA(c *webContext) {
	path := strings.TrimPrefix(c.Request.URL.Path, "/")
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
		c.Response.Write(content)
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
	content, err := iofs.ReadFile(appSubFS, "index.html")
	if err != nil {
		serve500(c)
		return
	}
	c.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
	c.Response.Write(content)
}

// serveConfig returns app configuration as JSON and refreshes CSRF token.
func serveConfig(c *webContext) {
	type configResponse struct {
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
		c.Response.Write(jr)
		return
	}
	conn, err := ws.Upgrade(c.Response, c.Request, nil)
	if err != nil {
		log.Error().Err(err).Msg("failed to upgrade websocket request")
		return
	}
	defer conn.Close()
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
	// Boost priority-matching documents to the front.
	if cfg.Rules != nil && len(res.Documents) > 0 {
		var priority, rest []*indexer.Document
		for _, d := range res.Documents {
			if cfg.Rules.IsPriority(d.URL) {
				priority = append(priority, d)
			} else {
				rest = append(rest, d)
			}
		}
		if len(priority) > 0 {
			res.Documents = append(priority, rest...)
		}
	}
	hr, err := model.GetURLsByQuery(oq)
	if err == nil && len(hr) > 0 {
		res.History = hr
	}
	if oq != "" {
		res.QuerySuggestion = model.GetQuerySuggestion(oq)
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
	if !c.Config.Rules.IsSkip(d.URL) && !strings.HasPrefix(d.URL, c.Config.BaseURL("/")) {
		if err := d.Process(); err != nil {
			log.Error().Err(err).Str("URL", d.URL).Msg("failed to process document")
			serve500(c)
			return
		}
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
	c.JSON(doc)
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
	r.RenderHTML(&htmlContent)
	title := doc.Title
	if r.Title() != "" {
		title = r.Title()
	}
	c.JSON(map[string]string{
		"title":   title,
		"content": htmlContent.String(),
	})
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

func serveOpensearch(c *webContext) {
	baseURL := strings.TrimSuffix(c.Config.BaseURL("/"), "/")
	xml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<OpenSearchDescription xmlns="http://a9.com/-/spec/opensearch/1.1/">
  <ShortName>Hister</ShortName>
  <Description>Search your history with Hister</Description>
  <Url type="text/html" template="%s/?q={searchTerms}"/>
</OpenSearchDescription>`, baseURL)
	c.Response.Header().Set("Content-Type", "application/xml")
	c.Response.Write([]byte(xml))
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
	c.Response.Write(i)
}

func serveStatic(c *webContext) {
	staticFileServer.ServeHTTP(c.Response, c.Request)
}

func serve200(c *webContext) {
	c.Response.WriteHeader(http.StatusOK)
}

func serve404(c *webContext) {
	c.Response.WriteHeader(http.StatusNotFound)
}

func serve500(c *webContext) {
	http.Error(c.Response, "Internal Server Error", http.StatusInternalServerError)
}

func (c *webContext) JSON(o any) {
	c.Response.Header().Set("Content-Type", "application/json")
	json.NewEncoder(c.Response).Encode(o)
}

func (c *webContext) Redirect(u string) {
	http.Redirect(c.Response, c.Request, u, http.StatusFound)
}
