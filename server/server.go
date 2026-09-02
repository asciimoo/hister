package server

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/gob"
	"errors"
	iofs "io/fs"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/indexer"
	"github.com/asciimoo/hister/server/model"
	"github.com/asciimoo/hister/server/static"

	"github.com/rs/zerolog/log"
)

// Version is set by the main package before Listen() is called so that MCP
// and other server responses can expose the running binary version.
var Version = "unknown"

const sessionMaxAge = 60 * 60 * 24 * 30

var (
	appSubFS         iofs.FS
	staticFileServer http.Handler
	sessionStore     *databaseSessionStore
	errCSRFMismatch  = errors.New("CSRF token mismatch")
	storeName        = "hister"
	tokName          = "csrf_token"
	staticTextFiles  map[string][]byte
)

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

type webContext struct {
	Request       *http.Request
	Response      http.ResponseWriter
	Config        *config.Config
	Indexer       *indexer.Indexer
	nonce         string
	csrf          string
	UserID        uint
	Username      string
	IsAdmin       bool
	Authenticated bool
	userRules     *config.Rules
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

func Listen(cfg *config.Config, idx *indexer.Indexer) error {
	sessionStore = newSessionStore(cfg.SecretKey(), cfg.BaseURL(""), sessionMaxAge)

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

	handler := registerEndpoints(cfg, idx)
	handler = withLogging(handler)

	log.Info().Str("Address", cfg.Server.Address).Str("Version", Version).Str("URL", cfg.BaseURL("/")).Msg("Starting webserver")
	return http.ListenAndServe(cfg.Server.Address, handler)
}

func createHandler(cfg *config.Config, idx *indexer.Indexer, h func(*webContext)) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		c := &webContext{
			Request:  r,
			Response: w,
			Config:   cfg,
			Indexer:  idx,
			nonce:    rand.Text(),
		}
		if cfg.App.UserHandling {
			populateUserContext(c)
		} else if cfg.App.AccessToken != "" {
			populateTokenContext(c)
		} else {
			c.Authenticated = true
		}
		if c.Authenticated && sessionStore != nil {
			if err := sessionStore.Refresh(c.Request, c.Response, storeName); err != nil {
				serve500(c)
				return
			}
		}
		h(c)
	}
}

func requestAccessToken(c *webContext) string {
	tok := c.Request.Header.Get("X-Access-Token")
	if tok == "" {
		if auth := c.Request.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			tok = strings.TrimPrefix(auth, "Bearer ")
		}
	}
	return tok
}

func populateTokenContext(c *webContext) {
	if tok := requestAccessToken(c); tok != "" && tok == c.Config.App.AccessToken {
		c.Authenticated = true
		return
	}
	session, err := sessionStore.Get(c.Request, storeName)
	if err == nil && session != nil {
		if sessionStore.tokenAuthenticated(session, c.Config.App.AccessToken) {
			c.Authenticated = true
		}
	}
}

func withTokenAuth(handler endpointHandler) endpointHandler {
	return func(c *webContext) {
		if tok := requestAccessToken(c); tok != "" && tok == c.Config.App.AccessToken {
			c.Authenticated = true
			handler(c)
			return
		}
		session, err := sessionStore.Get(c.Request, storeName)
		if err != nil {
			serve500(c)
			return
		}
		if sessionStore.tokenAuthenticated(session, c.Config.App.AccessToken) {
			c.Authenticated = true
			handler(c)
			return
		}
		serve403(c)
	}
}

func populateUserContext(c *webContext) {
	session, err := sessionStore.Get(c.Request, storeName)
	if err != nil {
		return
	}
	if uid, ok := session.Values["user_id"].(uint); ok && uid > 0 {
		if u, err := model.GetUserByID(uid); err == nil {
			c.UserID = u.ID
			c.Username = u.Username
			c.IsAdmin = u.IsAdmin
			c.Authenticated = true
			if rules, err := u.ParseRules(); err == nil {
				c.userRules = rules
			}
			return
		}
	}
	tok := requestAccessToken(c)
	if tok != "" {
		if u, err := model.GetUserByToken(tok); err == nil {
			c.UserID = u.ID
			c.Username = u.Username
			c.IsAdmin = u.IsAdmin
			c.Authenticated = true
			if rules, err := u.ParseRules(); err == nil {
				c.userRules = rules
			}
		}
	}
}

func withUserAuth(handler endpointHandler) endpointHandler {
	return func(c *webContext) {
		if c.UserID == 0 {
			serve403(c)
			return
		}
		c.Authenticated = true
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
		c.Authenticated = true
		handler(c)
	}
}

func historyEnabled(c *webContext) bool {
	return !c.Config.App.Public || c.Authenticated
}

func canWrite(c *webContext) bool {
	if c.Config.App.UserHandling {
		return c.UserID > 0
	}
	if c.Config.App.AccessToken != "" {
		return c.Authenticated
	}
	return !c.Config.App.Public
}

func targetUserID(c *webContext) uint {
	uid := c.UserID
	if c.Config.App.UserHandling && c.IsAdmin {
		if h := c.Request.Header.Get("X-Hister-Target-User-ID"); h != "" {
			if parsed, err := strconv.ParseUint(h, 10, 64); err == nil {
				uid = uint(parsed)
			}
		}
	}
	return uid
}

func publicDocumentRequested(c *webContext) bool {
	if c.UserID == 0 {
		return false
	}
	h := strings.TrimSpace(c.Request.Header.Get("X-Hister-Public"))
	if h == "" || h == "0" || strings.EqualFold(h, "false") || strings.EqualFold(h, "no") {
		return false
	}
	return true
}

func submittedDocumentUserID(c *webContext) uint {
	if publicDocumentRequested(c) {
		return 0
	}
	return targetUserID(c)
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
		for _, p := range []string{"/add", "/api/add", "/api/add_pdf", "/api/config", "/api/rules", "/api/delete", "/api/label", "/api/versions"} {
			if c.Request.URL.Path != p {
				continue
			}
			if strings.HasPrefix(c.Request.Header.Get("Origin"), "moz-extension://") {
				handler(c)
				return
			}
			if strings.HasPrefix(c.Request.Header.Get("Origin"), "safari-web-extension://") {
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
				http.Error(c.Response, errCSRFMismatch.Error(), http.StatusForbidden)
				return
			}
			token := c.Request.PostFormValue(tokName)
			if token == "" {
				token = c.Request.Header.Get("X-CSRF-Token")
			}
			if token != sToken {
				http.Error(c.Response, errCSRFMismatch.Error(), http.StatusForbidden)
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
