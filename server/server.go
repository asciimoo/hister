package server

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/indexer"
	"github.com/asciimoo/hister/server/model"
	"github.com/asciimoo/hister/server/static"
	"github.com/asciimoo/hister/server/templates"

	readability "codeberg.org/readeck/go-readability/v2"
	"github.com/gorilla/sessions"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

var (
	tpls            map[string]*template.Template
	fs              http.Handler
	sessionStore    *sessions.CookieStore
	errCSRFMismatch = errors.New("CSRF token mismatch")
	storeName       = "hister"
	tokName         = "csrf_token"
)

type tArgs map[string]any

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

var tFns = template.FuncMap{
	"FormatDate": func(t time.Time) string { return t.Format("2006-01-02") },
	"FormatTime": func(t time.Time) string { return t.Format("2006-01-02 15:04:05") },
	"ToHTML":     func(s string) template.HTML { return template.HTML(s) },
	"Join":       func(s []string, delim string) string { return strings.Join(s, delim) },
	"Replace":    strings.ReplaceAll,
	"ToLower":    strings.ToLower,
	"Truncate": func(s string, maxLen int) string {
		if len(s) > maxLen {
			return s[:maxLen] + "[..]"
		}
		return s
	},
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
	fs = http.StripPrefix("/static/", http.FileServerFS(static.FS))
	tpls = make(map[string]*template.Template)
	addTemplate("index", "layout/base.tpl", "index.tpl")
	addTemplate("add", "layout/base.tpl", "add.tpl")
	addTemplate("rules", "layout/base.tpl", "rules.tpl")
	addTemplate("help", "layout/base.tpl", "help.tpl")
	addTemplate("api", "layout/base.tpl", "api.tpl")
	addTemplate("about", "layout/base.tpl", "about.tpl")
	addTemplate("history", "layout/base.tpl", "history.tpl")
	addTemplate("opensearch", "opensearch.tpl")
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
	http.ListenAndServe(cfg.Server.Address, handler)
}

func addTemplate(name string, paths ...string) {
	t, err := template.New(name).Funcs(tFns).ParseFS(templates.FS, paths...)
	if err != nil {
		panic(err)
	}
	tpls[name] = t
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
		c.Response.Header().Add("Content-Security-Policy", fmt.Sprintf("script-src 'nonce-%s'", c.nonce))
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

func serveIndex(c *webContext) {
	q := c.Request.URL.Query().Get("q")
	if strings.HasPrefix(q, "!!") {
		c.Redirect(strings.Replace(c.Config.App.SearchURL, "{query}", q[2:], 1))
		return
	}
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
	c.Render("index", nil)
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
		// Go uses a reference time (2006-01-02 15:04:05)
		// "2006-01-02" = YYYY-MM-DD format
		// Very weird...
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
		c.Response.Header().Add("Content-Type", "application/xml")
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
	hr, err := model.GetURLsByQuery(oq)
	if err == nil && len(hr) > 0 {
		res.History = hr
	}
	if oq != "" {
		res.QuerySuggestion = model.GetQuerySuggestion(oq)
	}
	res.SearchDuration = fmt.Sprintf("%v", time.Since(start))
	return res, nil
}

func serveAdd(c *webContext) {
	m := c.Request.Method
	if m == http.MethodGet {
		c.Render("add", nil)
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
	c.Render("add", nil)
}

func serveHistory(c *webContext) {
	m := c.Request.Method
	if m == http.MethodGet {
		hs, err := model.GetLatestHistoryItems(40)
		if err != nil {
			serve500(c)
			return
		}
		c.Render("history", tArgs{
			"History": hs,
		})
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
		c.Render("rules", nil)
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
	c.Render("rules", tArgs{
		"Success": "Rules saved",
	})
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
	r.RenderHTML(c.Response)
}

func serveHelp(c *webContext) {
	c.Render("help", nil)
}

func serveAbout(c *webContext) {
	c.Render("about", nil)
}

func serveAPI(c *webContext) {
	c.Render("api", tArgs{
		"Endpoints": Endpoints,
	})
}

func serveAPIHistory(c *webContext) {
	origin := c.Request.Header.Get("Origin")
	if origin != "hister://" && !c.Config.IsSameHost(origin) {
		serve500(c)
		return
	}
	limitStr := c.Request.URL.Query().Get("limit")
	limit := 10
	if n, err := fmt.Sscanf(limitStr, "%d", &limit); n != 1 || err != nil || limit <= 0 {
		limit = 10
	}
	kind := c.Request.URL.Query().Get("kind")
	var result any
	var err error
	if kind == "top" {
		items, e := model.GetURLsByQuery("")
		if e != nil {
			err = e
		} else {
			if len(items) > limit {
				items = items[:limit]
			}
			result = items
		}
	} else {
		result, err = model.GetLatestHistoryItems(limit)
	}
	if err != nil {
		serve500(c)
		return
	}
	jr, err := json.Marshal(result)
	if err != nil {
		serve500(c)
		return
	}
	c.Response.Header().Set("Content-Type", "application/json")
	c.Response.Write(jr)
}

func serveOpensearch(c *webContext) {
	c.Response.Header().Add("Content-Type", "application/xml")
	c.Render("opensearch", nil)
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
	c.Redirect("/rules")
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
	c.Redirect("/rules")
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
	i, err := static.FS.ReadFile("favicon.ico")
	if err != nil {
		serve500(c)
		return
	}
	c.Response.Header().Add("Content-Type", "image/vnd.microsoft.icon")
	c.Response.Write(i)
}

func serveStatic(c *webContext) {
	fs.ServeHTTP(c.Response, c.Request)
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

func (c *webContext) Render(tpl string, args tArgs) {
	if args == nil {
		args = make(tArgs)
	}
	args["Config"] = c.Config
	args["Nonce"] = c.nonce
	args["CSRF"] = c.csrf
	t, ok := tpls[tpl]
	if !ok {
		log.Error().Str("template", tpl).Msg("template not found")
		serve500(c)
		return
	}
	base := "base"
	if tpl == "opensearch" {
		base = "opensearch.tpl"
	}
	err := t.ExecuteTemplate(c.Response, base, args)
	if err != nil {
		log.Error().Err(err).Msg("template render error")
		serve500(c)
		return
	}
}

func (c *webContext) Redirect(u string) {
	http.Redirect(c.Response, c.Request, u, http.StatusFound)
}
