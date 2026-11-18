package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/asciimoo/nogon/config"
	"github.com/asciimoo/nogon/files"
	"github.com/asciimoo/nogon/logging"
	"github.com/asciimoo/nogon/server/diagnostics"
	"github.com/asciimoo/nogon/server/extractor"
	"github.com/asciimoo/nogon/server/indexer"
	"github.com/asciimoo/nogon/server/metrics"
	"github.com/asciimoo/nogon/server/model"

	"github.com/rs/zerolog/log"
)

// serverRuntime owns a single running generation. Global model, extractor,
// session, and asset state may only change once that generation is drained.
type serverRuntime struct {
	mu                  sync.Mutex
	reloadMu            sync.Mutex
	current             *runtimeGeneration
	paused              bool
	ctx                 context.Context
	overrides           []config.Override
	serving             *runtimeListener
	failures            chan error
	reviews             map[configurationActor]configurationReviewGrant
	pending             *pendingAccessChange // guarded by reloadMu
	confirmationTimeout time.Duration
}

type runtimeGeneration struct {
	authID        string
	pendingAccess *pendingAccessStatus // guarded by serverRuntime.mu
	cfg           *config.Config
	idx           *indexer.Indexer
	db            *sql.DB
	handler       http.Handler
	active        int
	drained       chan struct{}
	closeOnce     sync.Once
	workers       sync.WaitGroup
	queueWorker   sync.WaitGroup
	stop          context.CancelFunc
	stopQueue     context.CancelFunc
	sockets       context.Context
	closeSockets  context.CancelFunc
	metrics       *metrics.Metrics
}

type runtimeListener struct {
	listener net.Listener
	server   *http.Server
	done     chan struct{}
}

type runtimeRequestKey struct{}

type runtimeListenerKey struct{}

type runtimeRequest struct {
	runtime           *serverRuntime
	generation        *runtimeGeneration
	sockets           context.Context
	finish            func()
	actor             configurationActor
	confirmationToken string
	request           *http.Request
}

type configurationReviewGrant struct {
	token      string
	revision   string
	digest     [32]byte
	generation *runtimeGeneration
}

type pendingAccessStatus struct {
	Revision          string    `json:"revision"`
	Deadline          time.Time `json:"deadline"`
	DestinationURL    string    `json:"destination_url"`
	ConfirmationToken string    `json:"confirmation_token,omitempty"`
	Authentication    string    `json:"authentication,omitempty"`
	OAuthProvider     string    `json:"oauth_provider,omitempty"`
}

type pendingAccessChange struct {
	status           pendingAccessStatus
	previous         *config.Config
	generation       *runtimeGeneration
	write            *config.ManagedWrite
	timer            *time.Timer
	confirmationHash [32]byte
}

type runtimeApplyOptions struct {
	staged    *config.ManagedWrite
	temporary bool
	rollback  bool
}

// activatePending is a runtime boundary for an already reviewed candidate.
func (r *serverRuntime) activatePending(lease *runtimeRequest, next *config.Config) (int, pendingAccessStatus, error) {
	lease.finish()
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	return r.activatePendingLocked(lease, next)
}

func (r *serverRuntime) activatePendingLocked(lease *runtimeRequest, next *config.Config) (int, pendingAccessStatus, error) {
	if !r.isCurrent(lease) || r.pending != nil {
		return http.StatusConflict, pendingAccessStatus{}, errors.New("configuration changed or is awaiting confirmation")
	}
	previous := lease.generation.cfg
	if !lease.actor.authorized(previous) {
		return http.StatusForbidden, pendingAccessStatus{}, errors.New("administrative authority is no longer available")
	}
	if !previous.SourcesUnchanged(r.overrides...) {
		return http.StatusConflict, pendingAccessStatus{}, config.ErrManagedConflict
	}
	if err := checkConfigurationReviewAdministrator(next); err != nil {
		return http.StatusUnprocessableEntity, pendingAccessStatus{}, err
	}
	staged, err := next.StageManaged()
	if err != nil {
		return http.StatusInternalServerError, pendingAccessStatus{}, &runtimeApplyError{code: "persistence_failed", message: "cannot stage managed configuration; active configuration retained"}
	}
	status, err := r.apply(r.ctx, lease, next, runtimeApplyOptions{staged: staged, temporary: true})
	if err != nil {
		closeManagedWrite(staged)
		return status, pendingAccessStatus{}, err
	}
	timeout := r.confirmationTimeout
	if timeout == 0 {
		timeout = 2 * time.Minute
	}
	pending := &pendingAccessChange{
		status:   pendingAccessStatus{Revision: rand.Text(), Deadline: time.Now().Add(timeout), DestinationURL: next.BaseURL("/settings")},
		previous: previous, generation: r.current, write: staged,
	}
	pending.status.Authentication, pending.status.OAuthProvider = configurationAccessVerification(previous, next)
	confirmationToken := rand.Text()
	pending.confirmationHash = sha256.Sum256([]byte(confirmationToken))
	r.pending = pending
	r.mu.Lock()
	pending.generation.pendingAccess = &pending.status
	r.mu.Unlock()
	pending.timer = time.AfterFunc(timeout, func() {
		r.reloadMu.Lock()
		defer r.reloadMu.Unlock()
		if r.pending == pending && r.ctx.Err() == nil {
			_, _ = r.rollbackPending()
		}
	})
	result := pending.status
	if !next.App.UserHandling && next.App.AccessToken == "" {
		result.ConfirmationToken = confirmationToken
	}
	return http.StatusAccepted, result, nil
}

func (pending *pendingAccessChange) close() {
	pending.timer.Stop()
	closeManagedWrite(pending.write)
}

func closeManagedWrite(write *config.ManagedWrite) {
	if err := write.Close(); err != nil {
		log.Warn().Msg("Cannot remove configuration staging files")
	}
}

// confirmPending commits a candidate only after the caller has verified access.
// Fresh authentication and destination verification belong to the HTTP boundary.
func (r *serverRuntime) confirmPending(lease *runtimeRequest, revision string) (int, error) {
	lease.finish()
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	pending := r.pending
	if pending == nil || pending.status.Revision != revision || pending.generation != lease.generation || !r.isCurrent(lease) {
		return http.StatusConflict, errors.New("no matching pending configuration")
	}
	if !lease.actor.authorized(pending.generation.cfg) {
		return http.StatusForbidden, errors.New("administrative authority is no longer available")
	}
	if !pending.initiatorVerified(lease.confirmationToken) {
		return http.StatusForbidden, errors.New("the initiating operation's confirmation token is required")
	}
	if !pending.destinationVerified(lease.request, r.serving) {
		return http.StatusForbidden, &runtimeApplyError{code: "destination_required", message: "Reconnect through the temporary configuration's destination before confirming access."}
	}
	if !pending.authenticationVerified(lease.actor) {
		return http.StatusForbidden, &runtimeApplyError{code: "fresh_authentication_required", message: "Sign in again using the temporary configuration before confirming access."}
	}
	if !time.Now().Before(pending.status.Deadline) {
		return r.expirePending()
	}
	if status, err := r.drain(r.ctx, pending.generation); err != nil {
		return status, err
	}
	resume := true
	defer func() {
		if resume {
			r.resume(pending.generation)
		}
	}()
	if !time.Now().Before(pending.status.Deadline) {
		resume = false
		return r.expirePending()
	}
	if !lease.actor.authorized(pending.generation.cfg) {
		return http.StatusForbidden, errors.New("administrative authority is no longer available")
	}
	if !pending.authenticationVerified(lease.actor) {
		return http.StatusForbidden, &runtimeApplyError{code: "fresh_authentication_required", message: "Sign in again using the temporary configuration before confirming access."}
	}
	if err := checkConfigurationReviewAdministrator(pending.generation.cfg); err != nil {
		resume = false
		return r.rejectPending(http.StatusUnprocessableEntity, err)
	}
	if !pending.previous.SourcesUnchanged(r.overrides...) {
		resume = false
		return r.rejectPending(http.StatusConflict, &runtimeApplyError{code: "sources_changed", message: "configuration sources changed; previous configuration restored"})
	}
	if err := pending.write.Commit(); err != nil {
		resume = false
		if errors.Is(err, config.ErrManagedRecovery) {
			pending.close()
			r.pending = nil
			failure := &runtimeApplyError{code: "recovery_failed", message: "managed configuration recovery failed; inspect config.web.yml and .config.web-recovery files before restarting the server"}
			r.failRecovery(failure)
			return http.StatusServiceUnavailable, failure
		}
		if errors.Is(err, config.ErrManagedConflict) {
			return r.rejectPending(http.StatusConflict, &runtimeApplyError{code: "sources_changed", message: "configuration sources changed; previous configuration restored"})
		}
		return r.rejectPending(http.StatusInternalServerError, &runtimeApplyError{code: "persistence_failed", message: "cannot commit managed configuration; previous configuration restored"})
	}
	pending.close()
	r.pending = nil
	r.mu.Lock()
	pending.generation.pendingAccess = nil
	r.mu.Unlock()
	return http.StatusOK, nil
}

func (pending *pendingAccessChange) initiatorVerified(token string) bool {
	if cfg := pending.generation.cfg; cfg.App.UserHandling || cfg.App.AccessToken != "" {
		return true
	}
	hash := sha256.Sum256([]byte(token))
	return token != "" && subtle.ConstantTimeCompare(hash[:], pending.confirmationHash[:]) == 1
}

func (pending *pendingAccessChange) destinationVerified(req *http.Request, listener *runtimeListener) bool {
	cfg := pending.generation.cfg
	destination, err := url.Parse(cfg.BaseURL("/api/settings/confirm"))
	if err != nil || req == nil || !sameConfigurationAuthority(req.Host, destination) || req.URL.Path != destination.Path {
		return false
	}
	if origin := req.Header.Get("Origin"); origin != "" && origin != "nogon://" && origin != "hister://" && origin != "same-origin" {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Scheme != destination.Scheme || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || !sameConfigurationAuthority(parsed.Host, destination) {
			return false
		}
	}
	// An old connection can outlive listener replacement. Its request must not
	// prove reachability of the new listener, even with a matching Host header.
	return pending.previous.Server.Address == cfg.Server.Address || req.Context().Value(runtimeListenerKey{}) == listener
}

func sameConfigurationAuthority(host string, destination *url.URL) bool {
	actual, err := url.Parse("//" + host)
	if err != nil || actual.User != nil || actual.Path != "" || actual.RawQuery != "" || actual.Fragment != "" || !strings.EqualFold(actual.Hostname(), destination.Hostname()) {
		return false
	}
	port := func(u *url.URL) string {
		if p := u.Port(); p != "" {
			return p
		}
		if destination.Scheme == "https" {
			return "443"
		}
		return "80"
	}
	return port(actual) == port(destination)
}

func (r *serverRuntime) rejectPending(status int, cause error) (int, error) {
	if status, err := r.rollbackPending(); err != nil {
		return status, err
	}
	return status, cause
}

func (pending *pendingAccessChange) authenticationVerified(actor configurationActor) bool {
	method := pending.status.Authentication
	if method == "" {
		return true
	}
	if actor.session == "" {
		return method == "credentials" && (actor.token != [32]byte{} || actor.proxy)
	}
	record, err := model.GetWebSession(actor.session)
	if err != nil || !record.ExpiresAt.After(time.Now()) {
		return false
	}
	var values map[any]any
	if decodeSessionValues(record.Data, &values) != nil {
		return false
	}
	if values[configurationLoginKey] != pending.generation.authID {
		return false
	}
	proof, _ := values[configurationLoginMethodKey].(string)
	switch method {
	case "oauth":
		return strings.HasPrefix(proof, "oauth:") && (pending.status.OAuthProvider == "" || proof == "oauth:"+pending.status.OAuthProvider)
	case "password":
		return proof == "password"
	default:
		return proof != ""
	}
}

func configurationAccessVerification(old, next *config.Config) (string, string) {
	if !next.App.UserHandling && next.App.AccessToken == "" {
		return "", ""
	}
	if providers := changedConfigurationProviders(old, next); len(providers) == 1 {
		return "oauth", providers[0]
	}
	if next.App.UserHandling {
		if next.Server.OAuthOnly && (!old.Server.OAuthOnly || !old.App.UserHandling || !reflect.DeepEqual(old.Server.OAuth, next.Server.OAuth)) {
			return "oauth", ""
		}
		if old.Server.OAuthOnly && !next.Server.OAuthOnly {
			return "password", ""
		}
	}
	if old.App.AccessToken != next.App.AccessToken || old.App.UserHandling != next.App.UserHandling || old.App.Public != next.App.Public || !reflect.DeepEqual(old.Server.OAuth, next.Server.OAuth) {
		return "credentials", ""
	}
	return "", ""
}

func (r *serverRuntime) expirePending() (int, error) {
	if status, err := r.rollbackPending(); err != nil {
		return status, err
	}
	return http.StatusConflict, errors.New("confirmation expired; previous configuration restored")
}

func (r *serverRuntime) cancelPending(lease *runtimeRequest, revision string) (int, error) {
	lease.finish()
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	pending := r.pending
	if pending == nil || pending.status.Revision != revision || pending.generation != lease.generation || !r.isCurrent(lease) {
		return http.StatusConflict, errors.New("no matching pending configuration")
	}
	if !lease.actor.authorized(pending.generation.cfg) {
		return http.StatusForbidden, errors.New("administrative authority is no longer available")
	}
	if !pending.initiatorVerified(lease.confirmationToken) {
		return http.StatusForbidden, errors.New("the initiating operation's confirmation token is required")
	}
	return r.rollbackPending()
}

// rollbackPending runs under reloadMu and never depends on the initiating
// credential or HTTP connection remaining valid.
func (r *serverRuntime) rollbackPending() (int, error) {
	pending := r.pending
	r.pending = nil
	defer pending.close()
	lease := &runtimeRequest{generation: pending.generation}
	_, err := r.apply(r.ctx, lease, pending.previous, runtimeApplyOptions{rollback: true})
	if err != nil {
		log.Error().Err(err).Msg("Cannot restore confirmed configuration")
		failure := &runtimeApplyError{code: "recovery_failed", message: "cannot restore confirmed configuration; restart the server"}
		r.failRecovery(failure)
		return http.StatusServiceUnavailable, failure
	}
	return http.StatusOK, nil
}

func (r *serverRuntime) failRecovery(err error) {
	r.mu.Lock()
	r.paused = true
	if r.current != nil {
		r.current.closeSockets()
	}
	r.mu.Unlock()
	select {
	case r.failures <- err:
	default:
	}
}

// runtimeApplyError carries a stable failure code through runtime recovery.
// Its message is safe for callers; underlying resource errors stay in logs.
type runtimeApplyError struct {
	code, message string
	fields        []config.FieldError
}

func (e *runtimeApplyError) Error() string { return e.message }

func (r *serverRuntime) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	if r.paused || r.current == nil || r.ctx.Err() != nil {
		r.mu.Unlock()
		w.Header().Set("Retry-After", "1")
		http.Error(w, "server is reloading", http.StatusServiceUnavailable)
		return
	}
	g := r.current
	if g.active == 0 {
		g.drained = make(chan struct{})
	}
	g.active++
	finish := sync.OnceFunc(func() {
		r.mu.Lock()
		g.active--
		if g.active == 0 {
			close(g.drained)
		}
		r.mu.Unlock()
	})
	lease := &runtimeRequest{runtime: r, generation: g, sockets: g.sockets, finish: finish, request: req}
	r.mu.Unlock()
	defer lease.finish()
	g.handler.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), runtimeRequestKey{}, lease)))
}

func (r *serverRuntime) startListener(listener net.Listener) *runtimeListener {
	// Leave WriteTimeout unset so reindexing and other long running handlers
	// can finish their responses after the request body has been read.
	s := &runtimeListener{listener: listener, done: make(chan struct{})}
	s.server = &http.Server{
		Handler:           r,
		BaseContext:       func(net.Listener) context.Context { return context.WithValue(r.ctx, runtimeListenerKey{}, s) },
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute,
		IdleTimeout:       time.Minute,
	}
	go func() {
		defer close(s.done)
		err := s.server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			select {
			case r.failures <- err:
			default:
			}
		}
	}()
	return s
}

func (r *serverRuntime) generation(cfg *config.Config, idx *indexer.Indexer) (*runtimeGeneration, error) {
	warning, err := model.CheckAdministratorAvailability(cfg)
	if err != nil {
		return nil, err
	}
	if warning != "" {
		log.Warn().Msg(warning)
	}
	db, err := model.DB.DB()
	if err != nil {
		return nil, err
	}
	if err := parseStaticFiles(cfg.BasePathPrefix()); err != nil {
		return nil, err
	}
	var watcher *files.Watcher
	if len(cfg.Indexer.Directories) > 0 {
		watcher, err = files.NewDirectoryWatcher(cfg.Indexer.Directories)
		if err != nil {
			return nil, err
		}
	}
	sessionStore = newSessionStore(cfg.SecretKey(), cfg.BaseURL(""), sessionMaxAge)
	ctx, cancel := context.WithCancel(r.ctx)
	g := &runtimeGeneration{authID: rand.Text(), cfg: cfg, idx: idx, db: db, stop: cancel}
	g.drained = make(chan struct{})
	close(g.drained)
	g.sockets, g.closeSockets = context.WithCancel(r.ctx)
	g.handler = withLogging(registerEndpoints(cfg, idx, r.overrides...))
	if cfg.Server.Metrics {
		g.metrics = metrics.New(ctx, idx)
		idx.SetMetrics(g.metrics)
	}
	if len(cfg.Indexer.Directories) > 0 {
		queue := idx.NewFileIndexQueue()
		// Stop producers before the queue so pending file removals and writes
		// finish before the old index and database are closed.
		queueCtx, cancelQueue := context.WithCancel(context.Background())
		g.stopQueue = cancelQueue
		g.queueWorker.Go(func() {
			if err := queue.Run(queueCtx); err != nil && !errors.Is(err, context.Canceled) {
				log.Error().Err(err).Msg("File index queue failed")
			}
		})
		g.workers.Go(func() { queue.EnqueueAll(cfg.Indexer.Directories) })
		g.workers.Go(func() {
			defer func() {
				if err := watcher.Close(); err != nil {
					log.Error().Err(err).Msg("Failed to close file watcher")
				}
			}()
			err := watcher.Run(ctx, func(path string) {
				userID, err := files.FindDirUser(cfg.Indexer.Directories, path)
				if err != nil || (userID != 0 && !cfg.App.UserHandling) {
					log.Error().Str("path", path).Msg("Failed to resolve file owner")
					return
				}
				queue.EnqueueIndex(path, userID)
			}, queue.EnqueueDelete)
			if err != nil && !errors.Is(err, context.Canceled) {
				log.Error().Err(err).Msg("File watcher failed")
			}
		})
	}
	return g, nil
}

func (g *runtimeGeneration) close() {
	g.closeOnce.Do(func() {
		g.closeSockets()
		g.stop()
		g.workers.Wait()
		if g.stopQueue != nil {
			g.stopQueue()
		}
		g.queueWorker.Wait()
		if g.metrics != nil {
			g.metrics.Stop()
		}
		g.idx.Close()
		if err := g.db.Close(); err != nil {
			log.Error().Err(err).Msg("Failed to close database")
		}
	})
}

func (r *serverRuntime) open(cfg *config.Config, registry *extractor.Registry) (*runtimeGeneration, error) {
	if err := model.Init(cfg); err != nil {
		closeRuntimeDatabase()
		return nil, errors.New("cannot open database")
	}
	extractor.DefaultRegistry().Replace(registry)
	idx, err := indexer.New(cfg)
	if err != nil {
		closeRuntimeDatabase()
		return nil, errors.New("cannot open search index")
	}
	if cfg.SemanticSearch.Enable && !idx.SemanticSearchEnabled() {
		idx.Close()
		closeRuntimeDatabase()
		return nil, errors.New("cannot initialize semantic search")
	}
	for _, check := range diagnostics.Index(cfg, idx) {
		if check.Status != "ok" {
			log.Warn().Msg(check.Message)
		}
	}
	g, err := r.generation(cfg, idx)
	if err != nil {
		idx.Close()
		closeRuntimeDatabase()
	}
	return g, err
}

func closeRuntimeDatabase() {
	if model.DB != nil {
		if db, err := model.DB.DB(); err == nil {
			_ = db.Close()
		}
	}
}

// save shares the runtime owner's lock with explicit reloads. Once accepted,
// completion is independent of the HTTP connection, including a lost response.
func (r *serverRuntime) save(lease *runtimeRequest, edits []config.SettingEdit, revision, acknowledgment string) (int, any) {
	lease.finish()
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	failure := func(status int, code, message string) (int, any) {
		return status, map[string]string{"code": code, "error": message}
	}
	if r.pending != nil {
		return failure(http.StatusConflict, "confirmation_pending", "Confirm or cancel the pending configuration before saving another draft.")
	}
	if !r.isCurrent(lease) {
		return failure(http.StatusConflict, "stale_revision", "Server configuration changed; read settings again.")
	}
	old := lease.generation.cfg
	if !lease.actor.authorized(old) {
		return failure(http.StatusForbidden, "admin_required", "Administrative authority is no longer available.")
	}
	if !old.SourcesUnchanged(r.overrides...) {
		return failure(http.StatusConflict, "sources_changed", "Configuration sources changed; reload and read settings again.")
	}
	grant, ok := r.reviews[lease.actor]
	matched := ok && subtle.ConstantTimeCompare([]byte(grant.token), []byte(acknowledgment)) == 1
	// Presenting the issued acknowledgment consumes it even if the draft
	// was edited. An obsolete or forged token cannot consume a new review.
	if matched {
		delete(r.reviews, lease.actor)
	}
	next, fields := prepareConfigurationDraft(old, edits, revision)
	if len(fields) > 0 {
		return http.StatusUnprocessableEntity, map[string]any{"code": "invalid_settings", "errors": fields}
	}
	mode, _ := configurationConfirmation(old, next, edits)
	if mode != "" || acknowledgment != "" {
		if mode != "" && acknowledgment == "" {
			return failure(http.StatusPreconditionRequired, "acknowledgment_required", "Review and explicitly confirm the sensitive changes before applying them.")
		}
		digest, err := configurationReviewDigest(next, edits)
		if mode == "" || !matched || err != nil || grant.revision != revision || grant.generation != lease.generation || grant.digest != digest {
			return failure(http.StatusConflict, "invalid_acknowledgment", "This acknowledgment does not match a current review. Review the draft again.")
		}
		if mode == config.ConfirmAccess {
			if err := checkConfigurationReviewAdministrator(next); err != nil {
				return failure(http.StatusUnprocessableEntity, "admin_required", err.Error())
			}
			status, pending, err := r.activatePendingLocked(lease, next)
			if err != nil {
				if applyErr, ok := errors.AsType[*runtimeApplyError](err); ok {
					return failure(status, applyErr.code, applyErr.message)
				}
				return failure(status, "activation_failed", err.Error())
			}
			return status, pending
		}
	}
	if old.ManagedSelectionsEqual(next) {
		return http.StatusOK, map[string]bool{"saved": true}
	}
	staged, err := next.StageManaged()
	if err != nil {
		if errors.Is(err, config.ErrManagedConflict) {
			return failure(http.StatusConflict, "sources_changed", err.Error())
		}
		return failure(http.StatusInternalServerError, "persistence_failed", "Cannot stage managed configuration; active configuration retained.")
	}
	defer func() {
		if err := staged.Close(); err != nil {
			log.Warn().Msg("Cannot remove configuration staging files")
		}
	}()
	status, err := r.apply(r.ctx, lease, next, runtimeApplyOptions{staged: staged})
	if err != nil {
		code := "apply_failed"
		if applyErr, ok := errors.AsType[*runtimeApplyError](err); ok {
			code = applyErr.code
			if len(applyErr.fields) > 0 {
				return status, map[string]any{"code": code, "errors": applyErr.fields}
			}
		}
		return failure(status, code, err.Error())
	}
	return http.StatusOK, map[string]bool{"saved": true}
}

func (r *serverRuntime) review(lease *runtimeRequest, edits []config.SettingEdit, revision string) (int, any) {
	lease.finish()
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	failure := func(status int, code, message string) (int, any) {
		return status, map[string]string{"code": code, "error": message}
	}
	if !r.isCurrent(lease) {
		return failure(http.StatusConflict, "stale_revision", "Server configuration changed; read settings again.")
	}
	current := lease.generation.cfg
	if !lease.actor.authorized(current) {
		return failure(http.StatusForbidden, "admin_required", "Administrative authority is no longer available.")
	}
	if !current.SourcesUnchanged(r.overrides...) {
		return failure(http.StatusConflict, "sources_changed", "Configuration sources changed; reload and read settings again.")
	}
	delete(r.reviews, lease.actor)
	candidate, fields := prepareConfigurationDraft(current, edits, revision)
	if len(fields) > 0 {
		return http.StatusUnprocessableEntity, map[string]any{"code": "invalid_settings", "errors": fields}
	}
	review := describeConfigurationReview(current, candidate, edits)
	if review.Mode == "" {
		return http.StatusOK, map[string]any{"revision": revision, "values": configurationStates(candidate, ""), "requires_confirmation": false}
	}
	if review.Mode == config.ConfirmAccess {
		if err := checkConfigurationReviewAdministrator(candidate); err != nil {
			return failure(http.StatusUnprocessableEntity, "admin_required", err.Error())
		}
	}
	digest, err := configurationReviewDigest(candidate, edits)
	if err != nil {
		return failure(http.StatusInternalServerError, "invalid_review", "Cannot prepare the configuration review.")
	}
	if r.reviews == nil {
		r.reviews = make(map[configurationActor]configurationReviewGrant)
	}
	for actor, grant := range r.reviews {
		if grant.generation != lease.generation {
			delete(r.reviews, actor)
		}
	}
	if len(r.reviews) >= 128 {
		return failure(http.StatusTooManyRequests, "review_limit", "Too many outstanding configuration reviews. Reload configuration before creating another review.")
	}
	review.Acknowledgment = rand.Text()
	r.reviews[lease.actor] = configurationReviewGrant{token: review.Acknowledgment, revision: revision, digest: digest, generation: lease.generation}
	return http.StatusOK, map[string]any{"revision": revision, "values": configurationStates(candidate, ""), "review": review, "requires_confirmation": true}
}

func (r *serverRuntime) isCurrent(lease *runtimeRequest) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.paused && r.current == lease.generation && r.ctx.Err() == nil
}

// reload is called after authorization. Release this request's generation
// lease before draining; the rest of this handler only writes its response.
func (r *serverRuntime) reload(req *http.Request, lease *runtimeRequest, next *config.Config) (int, error) {
	lease.finish()
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	if r.pending != nil {
		return http.StatusConflict, errors.New("confirm or cancel the pending configuration before reloading")
	}
	if !r.isCurrent(lease) {
		return http.StatusConflict, errors.New("server configuration changed; retry reload")
	}
	if !lease.actor.authorized(lease.generation.cfg) {
		return http.StatusForbidden, errors.New("administrative authority is no longer available")
	}
	return r.apply(req.Context(), lease, next, runtimeApplyOptions{})
}

// apply runs under reloadMu. Temporary changes leave the confirmed file intact.
func (r *serverRuntime) apply(ctx context.Context, lease *runtimeRequest, next *config.Config, options runtimeApplyOptions) (int, error) {
	staged := options.staged
	old := lease.generation
	registry, err := diagnostics.Extractors(next)
	if err != nil {
		return http.StatusBadRequest, errors.New("invalid extractor configuration; active configuration retained")
	}
	if _, err := next.CompileSensitiveContentPatterns(); err != nil {
		return http.StatusBadRequest, errors.New("invalid sensitive content patterns; active configuration retained")
	}
	if next.App.Directory != old.cfg.App.Directory {
		if err := next.PrepareDataDirectory(); err != nil {
			return http.StatusBadRequest, errors.New("cannot initialize new data directory; active configuration retained")
		}
	}
	var replacement net.Listener
	if next.Server.Address != old.cfg.Server.Address {
		replacement, err = newListener(next.Server)
		if err != nil {
			return http.StatusBadRequest, errors.New("cannot bind new server.address; active configuration retained")
		}
		defer func() {
			if replacement != nil {
				_ = replacement.Close()
			}
		}()
	}
	logChange, err := logging.Default.Prepare(next.App)
	if err != nil {
		log.Error().Err(err).Msg("Failed to prepare logging for reload")
		return http.StatusBadRequest, errors.New("cannot open app.log_file; active configuration retained")
	}
	defer func() {
		if err := logChange.Close(); err != nil {
			log.Warn().Err(err).Msg("Failed to close prepared log file")
		}
	}()
	if status, err := r.drain(ctx, old); err != nil {
		return status, err
	}
	if !options.rollback && !lease.actor.authorized(old.cfg) {
		r.resume(old)
		return http.StatusForbidden, &runtimeApplyError{code: "admin_required", message: "administrative authority is no longer available"}
	}
	if options.temporary {
		if err := checkConfigurationReviewAdministrator(next); err != nil {
			r.resume(old)
			return http.StatusUnprocessableEntity, &runtimeApplyError{code: "admin_required", message: err.Error()}
		}
	} else if next.App.Directory == old.cfg.App.Directory && next.Server.Database == old.cfg.Server.Database {
		if _, err := model.CheckAdministratorAvailability(next); err != nil {
			r.resume(old)
			return http.StatusBadRequest, err
		}
	}
	// A request completing during draining may have removed a selected owner.
	// Recheck paths too before retiring the current runtime or committing a save.
	if staged != nil && (old.cfg.App.UserHandling != next.App.UserHandling || !reflect.DeepEqual(old.cfg.Indexer.Directories, next.Indexer.Directories)) {
		if fields := files.ValidateDirectorySettings(next); len(fields) > 0 {
			r.resume(old)
			return http.StatusUnprocessableEntity, &runtimeApplyError{code: "invalid_settings", message: "Watched directories changed during apply.", fields: fields}
		}
	}
	// Include rule writes that completed while this reload was draining.
	if err := next.ReadRules(); err != nil {
		r.resume(old)
		return http.StatusBadRequest, errors.New("cannot reload rules.json; active configuration retained")
	}
	// Recovery must include rule writes completed during request draining too.
	if old.cfg.App.Directory == next.App.Directory {
		old.cfg.Rules = next.Rules
	} else if err := old.cfg.ReadRules(); err != nil {
		r.resume(old)
		return http.StatusBadRequest, errors.New("cannot read previous rules.json; active configuration retained")
	}
	if staged != nil && !old.cfg.SourcesUnchanged(r.overrides...) {
		r.resume(old)
		return http.StatusConflict, &runtimeApplyError{code: "sources_changed", message: "configuration sources changed; active configuration retained"}
	}
	old.close()
	failureCode := "apply_failed"
	g, err := r.open(next, registry)
	if err == nil && staged != nil {
		if !old.cfg.SourcesUnchanged(r.overrides...) {
			err = config.ErrManagedConflict
		} else if !options.temporary {
			if commitErr := staged.Commit(); commitErr != nil {
				failureCode = "persistence_failed"
				err = fmt.Errorf("cannot commit managed configuration: %w", commitErr)
			}
		}
		if err != nil {
			g.close()
		}
	}
	if err != nil {
		if options.rollback {
			return http.StatusServiceUnavailable, err
		}
		log.Error().Err(err).Msg("Runtime reload failed; restoring previous configuration")
		previousRegistry, registryErr := diagnostics.Extractors(old.cfg)
		var restoreErr error
		if errors.Is(err, config.ErrManagedRecovery) {
			restoreErr = err
		} else if registryErr == nil {
			g, restoreErr = r.open(old.cfg, previousRegistry)
		} else {
			restoreErr = registryErr
		}
		if restoreErr != nil {
			r.mu.Lock()
			r.current = nil
			r.mu.Unlock()
			failure := &runtimeApplyError{code: "recovery_failed", message: "reload and recovery failed; restart the server"}
			if errors.Is(err, config.ErrManagedRecovery) {
				failure.message = "managed configuration recovery failed; inspect config.web.yml and .config.web-recovery files before restarting the server"
			}
			select {
			case r.failures <- failure:
			default:
			}
			return http.StatusServiceUnavailable, failure
		}
		r.mu.Lock()
		r.current, r.paused = g, false
		r.mu.Unlock()
		if errors.Is(err, config.ErrManagedConflict) {
			return http.StatusConflict, &runtimeApplyError{code: "sources_changed", message: "configuration sources changed; previous runtime restored; reload and read settings again"}
		}
		message := "reload failed; previous configuration restored"
		if failureCode == "persistence_failed" {
			message = "cannot save managed configuration; previous configuration restored"
		}
		return http.StatusInternalServerError, &runtimeApplyError{code: failureCode, message: message}
	}
	if replacement != nil {
		previous := r.serving
		r.serving = r.startListener(replacement)
		replacement = nil
		previous.server.SetKeepAlivesEnabled(false)
		_ = previous.listener.Close()
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := previous.server.Shutdown(ctx); err != nil {
				_ = previous.server.Close()
			}
		}()
	}
	if err := logChange.Apply(); err != nil {
		log.Warn().Err(err).Msg("Failed to close previous log file")
	}
	r.mu.Lock()
	r.current, r.paused = g, false
	r.mu.Unlock()
	log.Info().Msg("Server, database, and indexer reloaded")
	return http.StatusOK, nil
}

func (r *serverRuntime) drain(ctx context.Context, g *runtimeGeneration) (int, error) {
	r.mu.Lock()
	r.paused = true
	g.closeSockets()
	drained := g.drained
	r.mu.Unlock()
	drainCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	select {
	case <-drained:
		return http.StatusOK, nil
	case <-drainCtx.Done():
		r.resume(g)
		return http.StatusRequestTimeout, errors.New("reload canceled while draining requests; active configuration retained")
	case <-r.ctx.Done():
		return http.StatusServiceUnavailable, errors.New("server is shutting down")
	}
}

func (r *serverRuntime) resume(g *runtimeGeneration) {
	r.mu.Lock()
	g.sockets, g.closeSockets = context.WithCancel(r.ctx)
	r.paused = false
	r.mu.Unlock()
}

func (r *serverRuntime) close() {
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	if r.pending != nil {
		r.pending.close()
		r.pending = nil
	}
	r.mu.Lock()
	r.paused = true
	g := r.current
	var drained <-chan struct{}
	if g != nil {
		g.closeSockets()
		drained = g.drained
	}
	r.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.serving.server.Shutdown(ctx); err != nil {
		_ = r.serving.server.Close()
	}
	<-r.serving.done
	if g != nil {
		<-drained
		g.close()
	}
}

func listenRuntime(ctx context.Context, cfg *config.Config, idx *indexer.Indexer, overrides ...config.Override) error {
	r := &serverRuntime{ctx: ctx, overrides: overrides, failures: make(chan error, 1)}
	if err := cfg.DiscardManagedStaging(); err != nil {
		idx.Close()
		closeRuntimeDatabase()
		return errors.New("cannot discard abandoned configuration staging files")
	}
	listener, err := newListener(cfg.Server)
	if err != nil {
		idx.Close()
		closeRuntimeDatabase()
		return err
	}
	g, err := r.generation(cfg, idx)
	if err != nil {
		_ = listener.Close()
		idx.Close()
		closeRuntimeDatabase()
		return err
	}
	r.current = g
	r.serving = r.startListener(listener)
	defer r.close()
	log.Info().Str("Address", cfg.Server.Address).Str("Version", Version).Str("URL", cfg.BaseURL("/")).Msg("Starting webserver")
	select {
	case <-ctx.Done():
		return nil
	case err := <-r.failures:
		return fmt.Errorf("webserver failed: %w", err)
	}
}
