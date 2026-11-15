package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"reflect"
	"slices"
	"sync"
	"time"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/files"
	"github.com/asciimoo/hister/server/extractor"
	"github.com/asciimoo/hister/server/model"

	"github.com/gorilla/sessions"
	"gopkg.in/yaml.v3"
)

func configurationCatalog(current *config.Config) []config.FieldMetadata {
	fields := slices.DeleteFunc(config.FieldCatalog(), func(field config.FieldMetadata) bool { return field.Path == "extractors" })
	return append(fields, extractor.NewDefaultRegistry().ConfigurationFields(current.Extractors)...)
}

func configurationStates(current *config.Config, revision string) []config.FieldState {
	return append(current.FieldStatesForRevision(revision), extractor.NewDefaultRegistry().ConfigurationStates(current, revision)...)
}

// prepareConfigurationDraft keeps extractor validation in the server layer.
// The config package owns composition and never imports the extractor runtime.
func prepareConfigurationDraft(current *config.Config, edits []config.SettingEdit, revisions ...string) (*config.Config, []config.FieldError) {
	revision := ""
	if len(revisions) > 0 {
		revision = revisions[0]
	}
	return current.PrepareDraftForRevision(revision, edits, func(candidate *config.Config) []config.FieldError {
		errors := extractor.ValidateConfiguration(candidate.Extractors)
		providers := changedConfigurationProviders(current, candidate)
		if len(providers) > 1 {
			errors = append(errors, config.FieldError{Path: "server.oauth", Code: "invalid_settings", Message: "Change one OAuth provider at a time so its login can be verified."})
		}
		if ((len(providers) > 0 || (!current.Server.OAuthOnly && candidate.Server.OAuthOnly)) && !candidate.App.UserHandling) || (candidate.App.UserHandling && candidate.Server.OAuthOnly && len(candidate.Server.OAuth) == 0) {
			errors = append(errors, config.FieldError{Path: "server.oauth", Code: "invalid_settings", Message: "OAuth verification requires user handling and a configured provider."})
		}
		for _, name := range providers {
			if name == "oidc" && len(candidate.Server.OAuth[name].AdminGroups) > 0 {
				errors = append(errors, config.FieldError{Path: "server.oauth.oidc", Code: "read_only", Message: "OIDC providers that synchronize administrator roles remain file managed."})
			}
		}
		directoriesEdited := slices.ContainsFunc(edits, func(edit config.SettingEdit) bool {
			return edit.Path == "indexer.directories"
		})
		if directoriesEdited || current.App.UserHandling != candidate.App.UserHandling {
			errors = append(errors, files.ValidateDirectorySettings(candidate)...)
		}
		return errors
	})
}

func changedConfigurationProviders(current, candidate *config.Config) []string {
	var changed []string
	for _, name := range slices.Sorted(maps.Keys(candidate.Server.OAuth)) {
		if next := candidate.Server.OAuth[name]; next != nil && !reflect.DeepEqual(current.Server.OAuth[name], next) {
			changed = append(changed, name)
		}
	}
	return changed
}

// Record the mechanism that actually authenticated the request. Runtime
// operations recheck it after releasing their generation lease and waiting for
// serialization. Credentials themselves are never stored in a review grant.
type configurationActor struct {
	userID       uint
	userRevision uint64
	session      string
	token        [32]byte
	proxy        bool
}

func configurationTokenActor(user *model.User, token string) configurationActor {
	actor := configurationActor{token: sha256.Sum256([]byte(token))}
	if user != nil {
		actor.userID, actor.userRevision = user.ID, user.Revision
	}
	return actor
}

func (actor configurationActor) authorized(cfg *config.Config) bool {
	var session *sessions.Session
	if actor.session != "" {
		record, err := model.GetWebSession(actor.session)
		if err != nil || !record.ExpiresAt.After(time.Now()) {
			return false
		}
		session = sessions.NewSession(sessionStore, storeName)
		if err := decodeSessionValues(record.Data, &session.Values); err != nil {
			return false
		}
	}
	if cfg.App.UserHandling {
		user, err := model.GetUserByID(actor.userID)
		if err != nil || !user.IsAdmin || user.Revision != actor.userRevision {
			return false
		}
		if actor.proxy {
			return cfg.Server.ProxyAuthHeader != ""
		}
		if session != nil {
			return sessionStore.userAuthenticated(session, user)
		}
		return user.Token != "" && actor.token == sha256.Sum256([]byte(user.Token))
	}
	if cfg.App.AccessToken != "" {
		if session != nil {
			return sessionStore.tokenAuthenticated(session, cfg.App.AccessToken)
		}
		return actor.token == sha256.Sum256([]byte(cfg.App.AccessToken))
	}
	return actor.userID == 0 && actor.token == [32]byte{} && !actor.proxy
}

func configurationNeedsReview(edits []config.SettingEdit) bool {
	for _, field := range config.FieldCatalog() {
		if field.RequiresConfirmation && slices.ContainsFunc(edits, func(edit config.SettingEdit) bool { return edit.Path == field.Path }) {
			return true
		}
	}
	return false
}

func configurationConfirmation(current, candidate *config.Config, edits []config.SettingEdit) (config.ConfirmationMode, []string) {
	var mode config.ConfirmationMode
	var consequences []string
	for _, field := range configurationCatalog(current) {
		policy := field.Confirmation
		if policy == nil || !slices.ContainsFunc(edits, func(edit config.SettingEdit) bool { return edit.Path == field.Path }) {
			continue
		}
		if message := policy.Describe(current, candidate); message != "" {
			consequences = append(consequences, message)
			if mode == "" || policy.Mode == config.ConfirmAccess {
				mode = policy.Mode
			}
		}
	}
	return mode, consequences
}

type configurationReview struct {
	Mode           config.ConfirmationMode `json:"mode"`
	Acknowledgment string                  `json:"acknowledgment"`
	Changes        []configurationChange   `json:"changes"`
	Consequences   []string                `json:"consequences"`
	DestinationURL string                  `json:"destination_url,omitempty"`
}

type configurationChange struct {
	Path   string `json:"path"`
	Before string `json:"before"`
	After  string `json:"after"`
}

func describeConfigurationReview(current, candidate *config.Config, edits []config.SettingEdit) configurationReview {
	mode, consequences := configurationConfirmation(current, candidate, edits)
	review := configurationReview{Mode: mode, Consequences: consequences}
	if mode == config.ConfirmAccess {
		review.DestinationURL = candidate.BaseURL("/settings")
	}
	old := make(map[string]config.FieldState)
	for _, state := range configurationStates(current, "") {
		old[state.Path] = state
	}
	fields := make(map[string]config.FieldMetadata)
	for _, field := range configurationCatalog(current) {
		fields[field.Path] = field
	}
	display := func(state config.FieldState) string {
		if fields[state.Path].Schema.Sensitivity != "none" {
			if state.SecretPresent {
				return "Value stored (hidden)"
			}
			return "No stored value"
		}
		if value, ok := state.Value.(string); ok {
			if value == "" {
				return "Empty"
			}
			return value
		}
		return fmt.Sprint(state.Value)
	}
	for _, state := range configurationStates(candidate, "") {
		previous := old[state.Path]
		if !reflect.DeepEqual(previous, state) || slices.ContainsFunc(edits, func(edit config.SettingEdit) bool { return edit.Path == state.Path }) {
			review.Changes = append(review.Changes, configurationChange{Path: state.Path, Before: display(previous), After: display(state)})
		}
	}
	return review
}

func configurationReviewDigest(candidate *config.Config, edits []config.SettingEdit) ([32]byte, error) {
	draft, err := json.Marshal(edits)
	if err != nil {
		return [32]byte{}, err
	}
	serialized, err := yaml.Marshal(candidate)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(append(append(draft, 0), serialized...)), nil
}

// Access changes require recovery access even though startup and explicit file
// reload deliberately allow existing deployments to serve recovery logins.
func checkConfigurationReviewAdministrator(candidate *config.Config) error {
	warning, err := model.CheckAdministratorAvailability(candidate)
	if err != nil {
		return errors.New("cannot verify administrator availability; check database access before reviewing this change")
	}
	if warning != "" {
		return errors.New("a usable administrator is required, including an independent local administrator when OIDC manages roles; stop the server and use hister user create --offline --admin USERNAME to create a recovery administrator")
	}
	return nil
}

// configReloader owns one generation's HTTP configuration. Readers receive
// immutable rule snapshots while writes are serialized with reload preparation.
type configReloader struct {
	mu        sync.Mutex
	current   *config.Config
	overrides []config.Override
	revision  string
}

type pendingConfiguration struct {
	Status string              `json:"status"`
	Values []config.FieldState `json:"values,omitempty"`
}

func serveSettings(c *webContext) {
	var pending *pendingConfiguration
	var pendingAccess *pendingAccessStatus
	if lease, ok := c.Request.Context().Value(runtimeRequestKey{}).(*runtimeRequest); ok {
		lease.runtime.mu.Lock()
		pendingAccess = lease.generation.pendingAccess
		lease.runtime.mu.Unlock()
	}
	next, err := c.Config.PendingConfiguration(c.reloader.overrides...)
	if err != nil {
		// Configuration errors may contain credentials or private paths.
		pending = &pendingConfiguration{Status: "invalid"}
		if _, ok := errors.AsType[*os.PathError](err); ok {
			pending.Status = "unreadable"
		}
	} else if next != nil {
		if len(extractor.ValidateConfiguration(next.Extractors)) > 0 {
			pending = &pendingConfiguration{Status: "invalid"}
		} else {
			pending = &pendingConfiguration{Status: "changed", Values: configurationStates(next, "")}
		}
	}
	if pendingAccess != nil && c.Config.SourcesUnchanged(c.reloader.overrides...) {
		// Unconfirmed web selections intentionally differ from the disk file.
		pending = nil
	}
	c.JSON(map[string]any{
		"revision":       c.reloader.revision,
		"fields":         configurationCatalog(c.Config),
		"values":         configurationStates(c.Config, c.reloader.revision),
		"pending":        pending,
		"pending_access": pendingAccess,
	})
}

func serveValidateSettings(c *webContext) {
	edits, ok := readSettingsDraft(c, nil)
	if !ok {
		return
	}
	if configurationNeedsReview(edits) {
		lease, ok := c.Request.Context().Value(runtimeRequestKey{}).(*runtimeRequest)
		if !ok {
			candidate, fields := prepareConfigurationDraft(c.Config, edits, c.reloader.revision)
			if len(fields) > 0 {
				c.JSONStatus(http.StatusUnprocessableEntity, map[string]any{"code": "invalid_settings", "errors": fields})
				return
			}
			if mode, _ := configurationConfirmation(c.Config, candidate, edits); mode == "" {
				c.JSON(map[string]any{"revision": c.reloader.revision, "values": configurationStates(candidate, ""), "requires_confirmation": false})
				return
			}
			c.JSONStatus(http.StatusServiceUnavailable, map[string]string{"code": "unavailable", "error": "Configuration reviews require a running server."})
			return
		}
		if c.settingsActor == (configurationActor{}) {
			session, err := sessionStore.Get(c.Request, storeName)
			if err == nil && session.ID == "" {
				err = session.Save(c.Request, c.Response)
			}
			if err != nil {
				c.JSONStatus(http.StatusServiceUnavailable, map[string]string{"code": "unavailable", "error": "Cannot establish the review session."})
				return
			}
			c.settingsActor = configurationActor{session: sessionTokenHash(session.ID)}
		}
		c.releaseConfig()
		lease.actor = c.settingsActor
		status, result := lease.runtime.review(lease, edits, c.reloader.revision)
		c.JSONStatus(status, result)
		return
	}
	candidate, fields := prepareConfigurationDraft(c.Config, edits, c.reloader.revision)
	if len(fields) > 0 {
		c.JSONStatus(http.StatusUnprocessableEntity, map[string]any{"code": "invalid_settings", "errors": fields})
		return
	}
	c.JSON(map[string]any{"revision": c.reloader.revision, "values": configurationStates(candidate, ""), "requires_confirmation": false})
}

func serveApplySettings(c *webContext) {
	var acknowledgment string
	edits, ok := readSettingsDraft(c, &acknowledgment)
	if !ok {
		return
	}
	lease, ok := c.Request.Context().Value(runtimeRequestKey{}).(*runtimeRequest)
	if !ok {
		c.JSONStatus(http.StatusServiceUnavailable, map[string]string{"code": "unavailable", "error": "Server runtime is unavailable."})
		return
	}
	c.releaseConfig()
	lease.actor = c.settingsActor
	if lease.actor == (configurationActor{}) && acknowledgment != "" {
		if cookie, err := c.Request.Cookie(storeName); err == nil {
			lease.actor.session = sessionTokenHash(cookie.Value)
		}
	}
	status, result := lease.runtime.save(lease, edits, c.reloader.revision, acknowledgment)
	c.JSONStatus(status, result)
}

func readSettingsDraft(c *webContext, acknowledgment *string) ([]config.SettingEdit, bool) {
	var revision string
	var rawEdits []json.RawMessage
	var rawAcknowledgment json.RawMessage
	fields := map[string]any{"revision": &revision, "edits": &rawEdits}
	if acknowledgment != nil {
		fields["acknowledgment"] = &rawAcknowledgment
	}
	if err := decodeSettingsObject(c.Request.Body, fields); err != nil {
		if !requestBodyTooLarge(c, err) {
			c.JSONStatus(http.StatusBadRequest, map[string]string{"code": "invalid_request", "error": "Supply a settings revision and edits array."})
		}
		return nil, false
	}
	if len(rawAcknowledgment) > 0 && (string(rawAcknowledgment) == "null" || json.Unmarshal(rawAcknowledgment, acknowledgment) != nil || len(*acknowledgment) > 128) {
		c.JSONStatus(http.StatusBadRequest, map[string]string{"code": "invalid_request", "error": "The acknowledgment must be a short opaque string from a review."})
		return nil, false
	}
	if revision == "" || rawEdits == nil {
		c.JSONStatus(http.StatusBadRequest, map[string]string{"code": "invalid_request", "error": "Supply a settings revision and edits array."})
		return nil, false
	}
	edits := make([]config.SettingEdit, 0, len(rawEdits))
	for _, raw := range rawEdits {
		var edit config.SettingEdit
		if err := decodeSettingsObject(bytes.NewReader(raw), map[string]any{"path": &edit.Path, "action": &edit.Action, "value": &edit.Value}); err != nil || edit.Path == "" || edit.Action == "" {
			c.JSONStatus(http.StatusBadRequest, map[string]string{"code": "invalid_request", "error": "Each edit requires a path, an action, and an optional value."})
			return nil, false
		}
		edits = append(edits, edit)
	}
	if revision != c.reloader.revision {
		c.JSONStatus(http.StatusConflict, map[string]string{"code": "stale_revision", "error": "Read the current settings before validating the draft."})
		return nil, false
	}
	if !c.Config.SourcesUnchanged(c.reloader.overrides...) {
		c.JSONStatus(http.StatusConflict, map[string]string{"code": "sources_changed", "error": "Configuration sources changed or cannot be read. Reload the server configuration and read settings again."})
		return nil, false
	}
	return edits, true
}

func serveConfirmSettings(c *webContext) { completePendingSettings(c, false) }

func serveCancelSettings(c *webContext) { completePendingSettings(c, true) }

func completePendingSettings(c *webContext, cancel bool) {
	var revision, confirmationToken string
	var rawToken json.RawMessage
	if err := decodeSettingsObject(c.Request.Body, map[string]any{"revision": &revision, "confirmation_token": &rawToken}); err != nil || revision == "" || len(revision) > 128 {
		if !requestBodyTooLarge(c, err) {
			c.JSONStatus(http.StatusBadRequest, map[string]string{"code": "invalid_request", "error": "Supply the pending configuration revision."})
		}
		return
	}
	if len(rawToken) > 0 && (string(rawToken) == "null" || json.Unmarshal(rawToken, &confirmationToken) != nil || len(confirmationToken) > 128) {
		c.JSONStatus(http.StatusBadRequest, map[string]string{"code": "invalid_request", "error": "Supply a short opaque confirmation token."})
		return
	}
	lease, ok := c.Request.Context().Value(runtimeRequestKey{}).(*runtimeRequest)
	if !ok {
		c.JSONStatus(http.StatusServiceUnavailable, map[string]string{"code": "unavailable", "error": "Server runtime is unavailable."})
		return
	}
	c.releaseConfig()
	lease.actor = c.settingsActor
	lease.confirmationToken = confirmationToken
	complete := lease.runtime.confirmPending
	if cancel {
		complete = lease.runtime.cancelPending
	}
	status, err := complete(lease, revision)
	if err != nil {
		code := "confirmation_failed"
		if failure, ok := errors.AsType[*runtimeApplyError](err); ok {
			code = failure.code
		}
		c.JSONStatus(status, map[string]string{"code": code, "error": err.Error()})
		return
	}
	c.JSON(map[string]bool{"confirmed": !cancel, "cancelled": cancel})
}

// Decode exact object keys once each. encoding/json's struct decoder accepts
// duplicate keys and case aliases, which are ambiguous for edit operations.
func decodeSettingsObject(reader io.Reader, fields map[string]any) error {
	decoder := json.NewDecoder(reader)
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token != json.Delim('{') {
		return errors.New("expected an object")
	}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok || fields[key] == nil {
			return errors.New("unknown or duplicate field")
		}
		if err := decoder.Decode(fields[key]); err != nil {
			return err
		}
		delete(fields, key)
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err != nil {
			return err
		}
		return errors.New("expected one object")
	}
	return nil
}

// beginRequest takes a snapshot for readers and serializes reloads with rule
// writes. No lock is held during searches or long lived WebSocket connections.
func (r *configReloader) beginRequest(req *http.Request) (*config.Config, func()) {
	r.mu.Lock()
	if req.Method == http.MethodPost {
		switch req.URL.Path {
		case "/api/reload", "/api/rules", "/api/add_alias", "/api/delete_alias":
			return r.current, sync.OnceFunc(r.mu.Unlock)
		}
	}
	snapshot := r.snapshotLocked()
	r.mu.Unlock()
	return snapshot, func() {}
}

func (r *configReloader) snapshot() *config.Config {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.snapshotLocked()
}

func (r *configReloader) snapshotLocked() *config.Config {
	snapshot := *r.current
	snapshot.Rules = cloneRules(snapshot.Rules)
	return &snapshot
}

func cloneRules(rules *config.Rules) *config.Rules {
	if rules == nil {
		return nil
	}
	copy := *rules
	copy.Aliases = maps.Clone(rules.Aliases)
	for _, field := range []**config.Rule{&copy.Allow, &copy.Skip, &copy.Priority, &copy.Versioning} {
		if *field != nil {
			rule := **field
			rule.ReStrs = append([]string(nil), rule.ReStrs...)
			*field = &rule
		}
	}
	return &copy
}

func serveReload(c *webContext) {
	lease, ok := c.Request.Context().Value(runtimeRequestKey{}).(*runtimeRequest)
	if !ok {
		http.Error(c.Response, "server runtime is unavailable", http.StatusServiceUnavailable)
		return
	}
	next, err := c.Config.Reload(c.reloader.overrides...)
	if err != nil {
		http.Error(c.Response, err.Error(), http.StatusBadRequest)
		return
	}
	c.releaseConfig()
	lease.actor = c.settingsActor
	status, err := lease.runtime.reload(c.Request, lease, next)
	if err != nil {
		http.Error(c.Response, err.Error(), status)
		return
	}
	serve200(c)
}
