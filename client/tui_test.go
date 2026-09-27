package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestFetchPreview(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/preview" || r.URL.Query().Get("url") != "https://example.com/article" {
			t.Errorf("request URL = %s", r.URL.String())
		}
		_ = json.NewEncoder(w).Encode(PreviewResponse{
			Title:        "An article",
			Content:      "<p>Readable</p>",
			Added:        123,
			VersionCount: 2,
			Meta:         map[string]any{"image": "https://cdn.example.com/cover.png"},
		})
	}))
	defer server.Close()

	preview, err := New(server.URL).FetchPreview("https://example.com/article")
	if err != nil {
		t.Fatal(err)
	}
	if preview.Title != "An article" || preview.VersionCount != 2 {
		t.Fatalf("preview = %#v", preview)
	}
}

func TestFetchConfigReadsServerSemanticCapabilities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/config" || r.Method != http.MethodGet {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"semanticEnabled":     true,
			"semanticWeight":      0.65,
			"similarityThreshold": 0.82,
		})
	}))
	defer server.Close()

	serverConfig, err := New(server.URL).FetchConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !serverConfig.SemanticEnabled || serverConfig.SemanticWeight != 0.65 || serverConfig.SimilarityThreshold != 0.82 {
		t.Fatalf("server config = %#v", serverConfig)
	}
}

func TestSaveRulesIncludesVersioningAndAllow(t *testing.T) {
	var got url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/rules" || r.Method != http.MethodPost {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		got = r.PostForm
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	if err := New(server.URL).SaveRules("skip", "priority", "version", "allowed"); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"skip": "skip", "priority": "priority", "versioning": "version", "allow": "allowed"} {
		if got.Get(key) != want {
			t.Errorf("form[%q] = %q, want %q", key, got.Get(key), want)
		}
	}
}

func TestSaveRulesKeepsVersioningOptional(t *testing.T) {
	var got url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		got = r.PostForm
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	if err := New(server.URL).SaveRules("skip", "priority"); err != nil {
		t.Fatal(err)
	}
	if got.Get("versioning") != "" {
		t.Errorf("form[%q] = %q, want empty", "versioning", got.Get("versioning"))
	}
	if got.Has("allow") {
		t.Error("legacy save must omit allow so existing rules are preserved")
	}
}

func TestUpdateLabelPayload(t *testing.T) {
	var got map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/label" || r.Method != http.MethodPost {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
			t.Errorf("Content-Type = %q", contentType)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	if err := New(server.URL).UpdateLabel("https://example.com", "research"); err != nil {
		t.Fatal(err)
	}
	if got["url"] != "https://example.com" || got["label"] != "research" {
		t.Fatalf("payload = %#v", got)
	}
}
