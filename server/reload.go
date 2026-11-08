package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"os"
	"sync"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/files"
	"github.com/asciimoo/hister/server/extractor"
)

func configurationCatalog(current *config.Config) []config.FieldMetadata {
	fields := config.FieldCatalog()
	return append(fields, extractor.NewDefaultRegistry().ConfigurationFields(current.Extractors)...)
}

func configurationStates(current *config.Config, revision string) []config.FieldState {
	return append(current.FieldStatesForRevision(revision), extractor.NewDefaultRegistry().ConfigurationStates(current)...)
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
		for _, edit := range edits {
			if edit.Path == "indexer.directories" {
				errors = append(errors, files.ValidateDirectorySettings(candidate)...)
				break
			}
		}
		return errors
	})
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
	c.JSON(map[string]any{
		"revision": c.reloader.revision,
		"fields":   configurationCatalog(c.Config),
		"values":   configurationStates(c.Config, c.reloader.revision),
		"pending":  pending,
	})
}

func serveValidateSettings(c *webContext) {
	edits, ok := readSettingsDraft(c)
	if !ok {
		return
	}
	candidate, fields := prepareConfigurationDraft(c.Config, edits, c.reloader.revision)
	if len(fields) > 0 {
		c.JSONStatus(http.StatusUnprocessableEntity, map[string]any{"code": "invalid_settings", "errors": fields})
		return
	}
	c.JSON(map[string]any{"revision": c.reloader.revision, "values": configurationStates(candidate, "")})
}

func serveApplySettings(c *webContext) {
	edits, ok := readSettingsDraft(c)
	if !ok {
		return
	}
	lease, ok := c.Request.Context().Value(runtimeRequestKey{}).(*runtimeRequest)
	if !ok {
		c.JSONStatus(http.StatusServiceUnavailable, map[string]string{"code": "unavailable", "error": "Server runtime is unavailable."})
		return
	}
	c.releaseConfig()
	lease.actorID = c.UserID
	status, result := lease.runtime.save(lease, edits, c.reloader.revision)
	c.JSONStatus(status, result)
}

func readSettingsDraft(c *webContext) ([]config.SettingEdit, bool) {
	var revision string
	var rawEdits []json.RawMessage
	if err := decodeSettingsObject(c.Request.Body, map[string]any{"revision": &revision, "edits": &rawEdits}); err != nil {
		if !requestBodyTooLarge(c, err) {
			c.JSONStatus(http.StatusBadRequest, map[string]string{"code": "invalid_request", "error": "Supply a settings revision and edits array."})
		}
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
	lease.actorID = c.UserID
	status, err := lease.runtime.reload(c.Request, lease, next)
	if err != nil {
		http.Error(c.Response, err.Error(), status)
		return
	}
	serve200(c)
}
