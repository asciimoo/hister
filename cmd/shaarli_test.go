package cmd

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/asciimoo/nogon/client"
	"github.com/asciimoo/nogon/server/document"

	"github.com/spf13/cobra"
)

func TestImportShaarliPaginatesMapsNotesAndDownloadsLinks(t *testing.T) {
	fixedTime := time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)
	var sourceOffsets []string
	sourceHTTPClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.Path != "/base/api/v1/links" {
			t.Errorf("Shaarli request = %s %s, want GET /base/api/v1/links", req.Method, req.URL.Path)
		}
		assertShaarliAuthorization(t, req, "shaarli-secret", fixedTime.Unix())
		if got := req.URL.Query().Get("limit"); got != "2" {
			t.Errorf("limit = %q, want 2", got)
		}
		if got := req.URL.Query().Get("visibility"); got != "all" {
			t.Errorf("visibility = %q, want all", got)
		}

		offset := req.URL.Query().Get("offset")
		sourceOffsets = append(sourceOffsets, offset)
		var response string
		switch offset {
		case "0":
			response = `[
				{
					"id": 10,
					"url": "https://example.com/article?utm_source=shaarli&keep=1#section",
					"shorturl": "abc123",
					"title": "Saved article",
					"description": "Shaarli description",
					"tags": ["reading", "go"],
					"private": true,
					"created": "2024-01-02T03:04:05Z",
					"updated": "2024-02-03T04:05:06Z"
				},
				{
					"id": 11,
					"url": "",
					"shorturl": "note123",
					"title": "Saved note",
					"description": "Complete note contents",
					"tags": ["notes"],
					"created": "2024-03-04T05:06:07Z",
					"updated": null
				}
			]`
		case "2":
			response = `[{
				"id": 12,
				"url": "",
				"shorturl": "",
				"title": "Invalid note"
			}]`
		default:
			return nil, fmt.Errorf("unexpected offset %q", offset)
		}
		return jsonHTTPResponse(req, http.StatusOK, response), nil
	})}
	source, err := newShaarliClient("https://shaarli.example/base/", "shaarli-secret", sourceHTTPClient)
	if err != nil {
		t.Fatal(err)
	}
	source.pageSize = 2
	source.now = func() time.Time { return fixedTime }

	var fetchedURLs []string
	contentFetcher := serviceContentFetchFunc(func(_ context.Context, rawURL string) (*document.Document, error) {
		fetchedURLs = append(fetchedURLs, rawURL)
		return &document.Document{
			URL:  rawURL,
			HTML: `<html><head><title>Downloaded title</title></head><body><main><p>Downloaded Shaarli content.</p></main></body></html>`,
		}, nil
	})

	var receivedDocs []*document.Document
	targetHTTPClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.Path != "/api/batch" {
			return nil, fmt.Errorf("unexpected Nogon request %s %s", req.Method, req.URL.Path)
		}
		var body struct {
			Ops []struct {
				*document.Document
			} `json:"ops"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			return nil, err
		}
		results := make([]map[string]any, len(body.Ops))
		for i, op := range body.Ops {
			receivedDocs = append(receivedDocs, op.Document)
			results[i] = map[string]any{"status": http.StatusCreated}
		}
		var response bytes.Buffer
		if err := json.NewEncoder(&response).Encode(map[string]any{"results": results}); err != nil {
			return nil, err
		}
		return jsonHTTPResponse(req, http.StatusOK, response.String()), nil
	})}
	target := client.New("http://nogon.example", client.WithHTTPClient(targetHTTPClient), client.WithMaxBatchBodyBytes(40<<20))

	stats, err := importShaarli(
		context.Background(),
		source,
		target,
		document.NewNullLanguageDetector(),
		contentFetcher,
		serviceImportOptions{BatchSize: 10},
	)
	if err != nil {
		t.Fatal(err)
	}
	if stats != (serviceImportStats{Imported: 2, Skipped: 1}) {
		t.Fatalf("stats = %+v, want 2 imported and 1 skipped", stats)
	}
	if !reflect.DeepEqual(sourceOffsets, []string{"0", "2"}) {
		t.Fatalf("source offsets = %v, want [0 2]", sourceOffsets)
	}
	if !reflect.DeepEqual(fetchedURLs, []string{"https://example.com/article?utm_source=shaarli&keep=1#section"}) {
		t.Fatalf("fetched URLs = %v, want only the external bookmark", fetchedURLs)
	}
	if len(receivedDocs) != 2 {
		t.Fatalf("received %d documents, want 2", len(receivedDocs))
	}

	article := receivedDocs[0]
	if article.URL != "https://example.com/article?keep=1" {
		t.Errorf("article URL = %q, want normalized URL", article.URL)
	}
	if article.Title != "Saved article" || article.Label != "shaarli" {
		t.Errorf("article title = %q, label = %q", article.Title, article.Label)
	}
	if !strings.Contains(article.Text, "Shaarli description") || !strings.Contains(article.Text, "Downloaded Shaarli content.") {
		t.Errorf("article text = %q", article.Text)
	}
	if article.HTML == "" {
		t.Error("downloaded article HTML was not preserved")
	}
	if article.Updated != mustUnixTime(t, "2024-02-03T04:05:06Z") {
		t.Errorf("article updated = %d", article.Updated)
	}
	if article.Metadata["source"] != "shaarli" || article.Metadata["shaarli_id"] != float64(10) {
		t.Errorf("article metadata = %#v", article.Metadata)
	}
	if article.Metadata["shaarli_private"] != true {
		t.Errorf("private metadata = %#v", article.Metadata["shaarli_private"])
	}
	if !reflect.DeepEqual(article.Metadata["shaarli_tags"], []any{"reading", "go"}) {
		t.Errorf("tag metadata = %#v", article.Metadata["shaarli_tags"])
	}

	note := receivedDocs[1]
	if note.URL != "https://shaarli.example/base/?note123" {
		t.Errorf("note URL = %q, want Shaarli permalink", note.URL)
	}
	if note.Text != "Complete note contents" || note.Label != "shaarli" {
		t.Errorf("note text = %q, label = %q", note.Text, note.Label)
	}
	if note.Updated != note.Added {
		t.Errorf("note timestamps = added %d updated %d", note.Added, note.Updated)
	}
	if note.Metadata["shaarli_note"] != true {
		t.Errorf("note metadata = %#v", note.Metadata)
	}
}

func TestShaarliWalkLinksUsesIncrementalHistory(t *testing.T) {
	var historyOffsets []string
	var fetchedIDs []string
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/v1/history":
			if got := req.URL.Query().Get("since"); got != "2024-01-02T03:04:05Z" {
				t.Errorf("history since = %q, want checkpoint timestamp", got)
			}
			offset := req.URL.Query().Get("offset")
			historyOffsets = append(historyOffsets, offset)
			switch offset {
			case "0":
				return jsonHTTPResponse(req, http.StatusOK, `[
					{"event":"CREATED","datetime":"2024-01-03T00:00:00Z","id":8},
					{"event":"DELETED","datetime":"2024-01-03T01:00:00Z","id":9}
				]`), nil
			case "2":
				return jsonHTTPResponse(req, http.StatusOK, `[
					{"event":"UPDATED","datetime":"2024-01-04T00:00:00Z","id":8},
					{"event":"SETTINGS","datetime":"2024-01-04T01:00:00Z"}
				]`), nil
			case "4":
				return jsonHTTPResponse(req, http.StatusOK, `[]`), nil
			default:
				return nil, fmt.Errorf("unexpected history offset %q", offset)
			}
		case "/api/v1/links/8":
			fetchedIDs = append(fetchedIDs, "8")
			return jsonHTTPResponse(req, http.StatusOK, `{"id":8,"url":"https://example.com/changed"}`), nil
		default:
			return nil, fmt.Errorf("unexpected Shaarli request %s", req.URL.Path)
		}
	})}
	source, err := newShaarliClient("https://shaarli.example", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	source.updatedAfter = mustUnixTime(t, "2024-01-02T03:04:05Z")
	source.pageSize = 2

	var links []shaarliLink
	if err := source.walkLinks(context.Background(), func(link shaarliLink) {
		links = append(links, link)
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(historyOffsets, []string{"0", "2", "4"}) {
		t.Fatalf("history offsets = %v, want [0 2 4]", historyOffsets)
	}
	if !reflect.DeepEqual(fetchedIDs, []string{"8"}) {
		t.Fatalf("fetched IDs = %v, want only changed nondeleted ID 8", fetchedIDs)
	}
	if len(links) != 1 || links[0].ID != 8 {
		t.Fatalf("changed links = %+v, want link 8", links)
	}
}

func TestShaarliDocumentRecognizesSelfLinkedNote(t *testing.T) {
	source, err := newShaarliClient("https://shaarli.example/base", "secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	d, contentRequest, err := source.document(shaarliLink{
		ID:          14,
		URL:         "https://shaarli.example/base/?note123",
		ShortURL:    "note123",
		Title:       "Self linked note",
		Description: "Stored note contents",
	}, document.NewNullLanguageDetector())
	if err != nil {
		t.Fatal(err)
	}
	if contentRequest != nil {
		t.Fatal("self linked Shaarli note unexpectedly requested a content download")
	}
	if d.URL != "https://shaarli.example/base/?note123" || d.Text != "Stored note contents" {
		t.Fatalf("note document = %+v", d)
	}
	if d.Metadata["shaarli_note"] != true {
		t.Fatalf("note metadata = %#v", d.Metadata)
	}
}

func TestShaarliRejectsAuthenticationFailureWithoutExposingSecret(t *testing.T) {
	const secret = "private-shaarli-secret"
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(req, http.StatusUnauthorized, `{"code":401,"message":"Not authorized"}`), nil
	})}
	source, err := newShaarliClient("https://shaarli.example", secret, httpClient)
	if err != nil {
		t.Fatal(err)
	}
	_, err = source.links(context.Background(), 0)
	if err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("links error = %v, want authentication failure", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("links error exposes the Shaarli API secret: %v", err)
	}
	if !strings.Contains(err.Error(), shaarliSecretEnv) || !strings.Contains(err.Error(), "--api-token") {
		t.Fatalf("links error = %v, want credential guidance", err)
	}
}

func TestGenerateShaarliJWT(t *testing.T) {
	issuedAt := time.Unix(1_700_000_000, 0)
	token := generateShaarliJWT("secret", issuedAt)
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT has %d parts, want 3", len(parts))
	}
	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(header) != `{"typ":"JWT","alg":"HS512"}` {
		t.Errorf("JWT header = %s", header)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims struct {
		IssuedAt int64 `json:"iat"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatal(err)
	}
	if claims.IssuedAt != issuedAt.Unix() {
		t.Errorf("JWT issued at = %d, want %d", claims.IssuedAt, issuedAt.Unix())
	}

	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha512.New, []byte("secret"))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		t.Error("JWT signature is invalid")
	}
}

func TestShaarliAPITokenFlagOverridesEnvironment(t *testing.T) {
	t.Setenv(shaarliSecretEnv, "environment-secret")
	cmd := &cobra.Command{}
	cmd.Flags().String("api-token", "", "")
	if got := serviceAPIToken(cmd, shaarliSecretEnv); got != "environment-secret" {
		t.Fatalf("API secret = %q, want environment secret", got)
	}
	if err := cmd.Flags().Set("api-token", "flag-secret"); err != nil {
		t.Fatal(err)
	}
	if got := serviceAPIToken(cmd, shaarliSecretEnv); got != "flag-secret" {
		t.Fatalf("API secret = %q, want flag secret", got)
	}
}

func assertShaarliAuthorization(t *testing.T, req *http.Request, secret string, issuedAt int64) {
	t.Helper()
	authorization := req.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, "Bearer ") {
		t.Errorf("Authorization = %q, want Bearer JWT", authorization)
		return
	}
	want := generateShaarliJWT(secret, time.Unix(issuedAt, 0))
	if authorization != "Bearer "+want {
		t.Errorf("Authorization contains an invalid Shaarli JWT")
	}
	if strings.Contains(authorization, secret) {
		t.Error("Authorization exposes the Shaarli API secret")
	}
}
