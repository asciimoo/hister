package cmd

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/asciimoo/hister/client"
	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/crawler"
	"github.com/asciimoo/hister/server/document"

	"github.com/spf13/cobra"
)

func TestServiceImportCommands(t *testing.T) {
	const updatedAt = 1704153600 // 2024-01-02 UTC
	for _, source := range []struct {
		command                        *cobra.Command
		env, authScheme, response      string
		updatedParameter, updatedValue string
	}{
		{importLinkdingCmd, "NOGON_IMPORT_LINKDING_TOKEN", "Token", `{"results":[],"next":null}`, "modified_since", "2024-01-02T00:00:00Z"},
		{importLinkwardenCmd, "NOGON_IMPORT_LINKWARDEN_TOKEN", "Bearer", `{"data":{"links":[],"nextCursor":null}}`, "searchQueryString", "after:2024-01-02"},
		{importKarakeepCmd, "NOGON_IMPORT_KARAKEEP_TOKEN", "Bearer", `{"bookmarks":[],"nextCursor":null}`, "q", "after:2024-01-02"},
		{importReadeckCmd, "NOGON_IMPORT_READECK_TOKEN", "Bearer", `[]`, "since", "2024-01-02T00:00:00Z"},
		{importWallabagCmd, "NOGON_IMPORT_WALLABAG_TOKEN", "Bearer", `{"_embedded":{"items":[]},"page":1,"pages":1}`, "since", "1704153600"},
		{importShaarliCmd, "NOGON_IMPORT_SHAARLI_SECRET", "JWT", `[]`, "since", "2024-01-02T00:00:00Z"},
		{importReadwiseCmd, "NOGON_IMPORT_READWISE_TOKEN", "Token", `{"results":[],"nextPageCursor":""}`, "", ""},
		{importRaindropCmd, "NOGON_IMPORT_RAINDROP_TOKEN", "Bearer", `{"result":true,"items":[]}`, "", ""},
	} {
		legacyEnv := strings.Replace(source.env, "NOGON_", "HISTER_", 1)
		t.Run(source.command.Name()+"/credential help", func(t *testing.T) {
			if !strings.Contains(source.command.Long, source.env) || !strings.Contains(source.command.Long, legacyEnv) {
				t.Errorf("command help must name both %s and %s", source.env, legacyEnv)
			}
			if !strings.Contains(source.command.Flags().Lookup("api-token").Usage, source.env) {
				t.Errorf("flag help must name %s", source.env)
			}
		})
		for _, scenario := range []string{"environment token", "Nogon token only", "legacy token only", "flag token", "missing token", "empty environment", "blank environment", "empty flag", "invalid batch", "invalid instance", "lookup failure", "source failure"} {
			if (scenario == "lookup failure" || scenario == "invalid instance") && source.updatedParameter == "" {
				continue
			}
			t.Run(source.command.Name()+"/"+scenario, func(t *testing.T) {
				oldCfg, oldTransport := cfg, http.DefaultTransport
				t.Cleanup(func() { cfg, http.DefaultTransport = oldCfg, oldTransport })
				cfg = config.CreateDefaultConfig()
				cfg.Indexer.DetectLanguages = false
				cfg.Server.BaseURL = "https://hister.example/base"
				cfg.App.AccessToken = "destination-token"
				t.Setenv(source.env, " environment-token ")
				t.Setenv(legacyEnv, " legacy-token ")
				command := &cobra.Command{Use: source.command.Use}
				command.SetContext(context.Background())
				addServiceImportFlags(command, source.command.Name(), source.env)
				if err := command.Flags().Set("format", "json"); err != nil {
					t.Fatal(err)
				}
				wantToken := "environment-token"
				switch scenario {
				case "Nogon token only":
					if err := os.Unsetenv(legacyEnv); err != nil {
						t.Fatal(err)
					}
				case "legacy token only":
					wantToken = "legacy-token"
					if err := os.Unsetenv(source.env); err != nil {
						t.Fatal(err)
					}
				case "flag token":
					wantToken = "flag-token"
					if err := command.Flags().Set("api-token", " flag-token "); err != nil {
						t.Fatal(err)
					}
				case "missing token":
					t.Setenv(legacyEnv, "")
					t.Setenv(source.env, "")
				case "empty environment":
					t.Setenv(source.env, "")
				case "blank environment":
					t.Setenv(source.env, " \t ")
				case "empty flag":
					if err := command.Flags().Set("api-token", ""); err != nil {
						t.Fatal(err)
					}
				case "invalid batch":
					if err := command.Flags().Set("batch-size", "0"); err != nil {
						t.Fatal(err)
					}
				}
				lookups, sourceRequests := 0, 0
				http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
					if req.URL.Host == "hister.example" {
						lookups++
						if req.URL.Path != "/base/search" || req.Header.Get("X-Access-Token") != "destination-token" || req.Header.Get("Authorization") != "" {
							t.Errorf("unexpected destination request: %s, headers %v", req.URL, req.Header)
						}
						var query client.SearchQuery
						if err := json.Unmarshal([]byte(req.URL.Query().Get("query")), &query); err != nil {
							t.Fatal(err)
						}
						if query.Text != "metadata.source:"+source.command.Name() || query.Sort != "date" || query.Limit != 1 {
							t.Errorf("unexpected incremental lookup: %+v", query)
						}
						if scenario == "lookup failure" {
							return jsonHTTPResponse(req, http.StatusInternalServerError, "lookup failed"), nil
						}
						return jsonHTTPResponse(req, http.StatusOK, fmt.Sprintf(`{"documents":[{"updated":%d}]}`, updatedAt)), nil
					}
					sourceRequests++
					wantHost := "source.example"
					switch source.command {
					case importReadwiseCmd:
						wantHost = "readwise.io"
					case importRaindropCmd:
						wantHost = "api.raindrop.io"
					}
					if req.Method != http.MethodGet || req.URL.Host != wantHost || req.Header.Get("X-Access-Token") != "" {
						t.Errorf("unexpected source request: %s %s, headers %v", req.Method, req.URL, req.Header)
					}
					authorization := req.Header.Get("Authorization")
					if source.authScheme == "JWT" {
						parts := strings.Split(strings.TrimPrefix(authorization, "Bearer "), ".")
						if len(parts) != 3 {
							t.Fatalf("expected a JWT, got %q", authorization)
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
						assertShaarliAuthorization(t, req, wantToken, claims.IssuedAt)
					} else if authorization != source.authScheme+" "+wantToken {
						t.Errorf("source authorization = %q", authorization)
					}
					if source.updatedParameter != "" && req.URL.Query().Get(source.updatedParameter) != source.updatedValue {
						t.Errorf("incremental request = %s, want %s=%s", req.URL, source.updatedParameter, source.updatedValue)
					}
					if scenario == "source failure" {
						return jsonHTTPResponse(req, http.StatusUnauthorized, "denied: environment-token legacy-token destination-token"), nil
					}
					return jsonHTTPResponse(req, http.StatusOK, source.response), nil
				})
				var output bytes.Buffer
				command.SetOut(&output)
				args := []string{"https://source.example/base"}
				if scenario == "invalid instance" {
					args[0] = "file:///private/data"
				}
				if source.command == importReadwiseCmd || source.command == importRaindropCmd {
					args = nil
				}
				err := source.command.RunE(command, args)
				for _, secret := range []string{"environment-token", "legacy-token", "flag-token", "destination-token"} {
					if strings.Contains(output.String(), secret) || (err != nil && strings.Contains(err.Error(), secret)) {
						t.Errorf("import output or error disclosed %s", secret)
					}
				}
				if !command.SilenceUsage {
					t.Error("execution errors should not print usage")
				}
				wantLookups, wantSourceRequests := 0, 0
				switch scenario {
				case "missing token", "empty environment", "blank environment", "empty flag":
					if err == nil || !strings.Contains(err.Error(), source.env) || !strings.Contains(err.Error(), "--api-token") {
						t.Errorf("credential error = %v", err)
					}
				case "invalid batch":
					if err == nil || !strings.Contains(err.Error(), "--batch-size") {
						t.Errorf("batch size error = %v", err)
					}
				case "invalid instance":
					if err == nil || !strings.Contains(err.Error(), "instance URL") {
						t.Errorf("instance URL error = %v", err)
					}
				case "lookup failure":
					wantLookups = 1
					if err == nil || !strings.Contains(err.Error(), "latest") || !strings.Contains(err.Error(), "lookup failed") {
						t.Errorf("lookup error = %v", err)
					}
				default:
					if source.updatedParameter != "" {
						wantLookups = 1
					}
					wantSourceRequests = 1
					if scenario == "source failure" {
						if err == nil || !strings.Contains(err.Error(), "import") || !strings.Contains(err.Error(), "authentication failed") || !strings.Contains(err.Error(), source.env) {
							t.Errorf("source error = %v", err)
						}
					} else {
						if err != nil {
							t.Fatal(err)
						}
						switch source.command {
						case importLinkdingCmd:
							wantSourceRequests = 2 // Active and archived bookmarks.
						case importRaindropCmd:
							wantSourceRequests = 3 // Root collections, child collections, and bookmarks.
						}
					}
					var summaries []map[string]int
					if err := json.Unmarshal(output.Bytes(), &summaries); err != nil || len(summaries) != 1 || len(summaries[0]) != 3 {
						t.Fatalf("invalid import summary %q: %v", output.String(), err)
					}
					for _, key := range []string{"imported", "skipped", "errors"} {
						if value, ok := summaries[0][key]; !ok || value != 0 {
							t.Errorf("summary %s = %d, present=%v", key, value, ok)
						}
					}
				}
				if lookups != wantLookups || sourceRequests != wantSourceRequests {
					t.Errorf("requests: destination=%d source=%d, want %d and %d", lookups, sourceRequests, wantLookups, wantSourceRequests)
				}
				if wantSourceRequests == 0 && output.Len() != 0 {
					t.Errorf("setup failure printed a completed import summary: %q", output.String())
				}
			})
		}
	}
}

type serviceImportClosingCrawler struct {
	crawler.Crawler
	closed bool
}

func (c *serviceImportClosingCrawler) Close() error {
	c.closed = true
	return nil
}

func TestServiceImportCommandFinishesAndClosesCrawler(t *testing.T) {
	for _, tc := range []struct {
		name     string
		stats    serviceImportStats
		runErr   error
		wantExit int
	}{
		{"success", serviceImportStats{Imported: 3, Skipped: 2}, nil, 0},
		{"partial failure", serviceImportStats{Imported: 3, Errors: 1}, nil, 2},
		{"canceled import", serviceImportStats{Imported: 3, Errors: 1}, context.Canceled, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldCfg := cfg
			t.Cleanup(func() { cfg = oldCfg })
			cfg = config.CreateDefaultConfig()
			cfg.Indexer.DetectLanguages = false
			t.Setenv(linkdingTokenEnv, "source-token")
			command := &cobra.Command{Use: "linkding"}
			ctx := t.Context()
			command.SetContext(ctx)
			addServiceImportFlags(command, "Linkding", linkdingTokenEnv)
			if err := command.Flags().Set("format", "json"); err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			command.SetOut(&output)
			backend := &serviceImportClosingCrawler{}
			run := serviceImportRunE("Linkding", linkdingTokenEnv, "API token", newLinkdingClient,
				func(gotCtx context.Context, _ *linkdingClient, _ *client.Client, _ document.LanguageDetector, fetcher serviceContentFetcher, _ serviceImportOptions) (serviceImportStats, error) {
					if gotCtx != ctx {
						t.Error("import did not receive the command context")
					}
					fetcher.(*crawlerServiceContentFetcher).crawler = backend
					return tc.stats, tc.runErr
				}, nil)
			err := run(command, []string{"https://source.example"})
			if ExitCode(err) != tc.wantExit || (tc.runErr != nil && !errors.Is(err, tc.runErr)) {
				t.Errorf("result = %v, exit = %d, want %d with cause %v", err, ExitCode(err), tc.wantExit, tc.runErr)
			}
			if !backend.closed {
				t.Error("import left its crawler open")
			}
			var summaries []serviceImportStats
			if err := json.Unmarshal(output.Bytes(), &summaries); err != nil || len(summaries) != 1 || summaries[0] != tc.stats {
				t.Errorf("summary = %q, want %+v: %v", output.String(), tc.stats, err)
			}
		})
	}
}

func TestServiceImportBufferDownloadsMissingFavicon(t *testing.T) {
	var received *document.Document
	faviconDownloads := 0
	targetHTTPClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body struct {
			Ops []struct {
				*document.Document
			} `json:"ops"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			return nil, err
		}
		if len(body.Ops) != 1 {
			return nil, fmt.Errorf("received %d batch operations, want one", len(body.Ops))
		}
		received = body.Ops[0].Document
		return jsonHTTPResponse(req, http.StatusOK, `{"results":[{"status":201}]}`), nil
	})}
	target := client.New("http://hister.example", client.WithHTTPClient(targetHTTPClient), client.WithMaxBatchBodyBytes(40<<20))
	buffer, err := newServiceImportBuffer(
		"test",
		target,
		document.NewNullLanguageDetector(),
		nil,
		serviceImportOptions{
			BatchSize: 1,
			FaviconDownloader: func(d *document.Document) error {
				faviconDownloads++
				d.Favicon = "data:image/png;base64,ZGVmYXVsdCBpY29u"
				return nil
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	d := &document.Document{
		URL:       "https://example.com/article",
		Title:     "Article",
		Text:      "Contents",
		Processed: true,
	}
	buffer.Add(context.Background(), d, nil)

	if received == nil {
		t.Fatal("no document was submitted")
	}
	if received.Favicon != "data:image/png;base64,ZGVmYXVsdCBpY29u" {
		t.Errorf("favicon = %q, want downloaded default icon", received.Favicon)
	}
	if faviconDownloads != 1 {
		t.Errorf("favicon downloads = %d, want one", faviconDownloads)
	}
	if buffer.stats.Imported != 1 || buffer.stats.Errors != 0 {
		t.Errorf("stats = %+v, want one import without errors", buffer.stats)
	}
}

func TestServiceImportRejectsUnsupportedLanguages(t *testing.T) {
	oldCfg := cfg
	t.Cleanup(func() { cfg = oldCfg })
	for _, enabled := range []bool{true, false} {
		cfg = config.CreateDefaultConfig()
		cfg.Indexer.DetectLanguages = enabled
		cfg.Indexer.Languages = []string{"en", "xx"}
		command := &cobra.Command{Use: "import-test"}
		addCommonImportFlags(command)
		runtime, err := newServiceImportRuntime(command)
		if runtime != nil {
			_ = runtime.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "indexer.languages") || !strings.Contains(err.Error(), "xx") {
			t.Errorf("detect_languages=%v: error = %v, want unsupported language xx", enabled, err)
		}
	}
}

func TestServiceImportLanguageSettings(t *testing.T) {
	oldCfg := cfg
	t.Cleanup(func() { cfg = oldCfg })
	for _, tc := range []struct {
		name     string
		enabled  bool
		accuracy string
		want     string
	}{
		{"default accuracy", true, "", "en"},
		{"low accuracy", true, "low", "en"},
		{"high accuracy", true, "high", "en"},
		{"disabled", false, "low", document.UnknownLanguage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg = config.CreateDefaultConfig()
			cfg.Indexer.DetectLanguages = tc.enabled
			cfg.Indexer.Languages = []string{"en", "de", "nl", "id"}
			cfg.Indexer.LanguageDetectionAccuracy = tc.accuracy
			command := &cobra.Command{Use: "import-test"}
			addCommonImportFlags(command)
			runtime, err := newServiceImportRuntime(command)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := runtime.Close(); err != nil {
					t.Errorf("close service import runtime: %v", err)
				}
			})
			for text, want := range map[string]string{
				"This document explains how to search through saved articles and find useful information.": tc.want,
				"hello world": document.UnknownLanguage,
				"bonjour":     document.UnknownLanguage,
			} {
				d := &document.Document{URL: "https://example.com/article"}
				fetched := &document.Document{URL: d.URL, Text: text}
				if err := applyServiceContent(context.Background(), d, fetched, "", "", runtime.languageDetector); err != nil {
					t.Fatal(err)
				}
				if d.Language != want {
					t.Errorf("imported %q: language = %q, want %q", text, d.Language, want)
				}
			}
		})
	}
}

func TestApplyServiceContentPreservesFetchedFavicon(t *testing.T) {
	d := &document.Document{URL: "https://example.com/article"}
	fetched := &document.Document{
		URL:     "https://example.com/article",
		HTML:    `<html><head><title>Article</title></head><body><main><p>Downloaded contents.</p></main></body></html>`,
		Favicon: "data:image/png;base64,bGlua2VkIGljb24=",
	}
	if err := applyServiceContent(context.Background(), d, fetched, "", "", document.NewNullLanguageDetector()); err != nil {
		t.Fatal(err)
	}
	if d.Favicon != fetched.Favicon {
		t.Errorf("favicon = %q, want fetched favicon %q", d.Favicon, fetched.Favicon)
	}
}

func TestServiceImportPreservesMetadataDuringLanguageDetection(t *testing.T) {
	for _, prefix := range []string{"", "An introduction to this useful document."} {
		d := &document.Document{URL: "https://example.com/evidence", Metadata: map[string]any{"author": "Alice"}}
		fetched := &document.Document{URL: d.URL, Text: "This document explains how a search engine stores web pages and helps people find useful information in their personal library of saved articles."}
		if err := applyServiceContent(context.Background(), d, fetched, prefix, "", document.NewLanguageDetectorFor([]string{"en", "de"}, true)); err != nil {
			t.Fatal(err)
		}
		if d.Language != "en" || len(d.Metadata) != 1 || d.Metadata["author"] != "Alice" {
			t.Fatalf("language=%s metadata=%+v", d.Language, d.Metadata)
		}
	}
}

type serviceLanguageDetectorFunc func(string) string

func (f serviceLanguageDetectorFunc) DetectLanguage(text string) string { return f(text) }

func TestApplyServiceContentReusesFreshLanguageDetection(t *testing.T) {
	const body = "The contents of the fetched page."
	const prefix = "Eine deutsche Zusammenfassung."
	const combined = prefix + "\n\n" + body
	for _, tc := range []struct {
		name             string
		prefix           string
		alreadyProcessed bool
		bodyLanguage     string
		wantText         string
		wantLanguage     string
		wantInputs       []string
	}{
		{"unchanged text", "", false, "en", body, "en", []string{body}},
		{"blank prefix", " \n\t", false, "en", body, "en", []string{body}},
		{"duplicate prefix", body, false, "en", body, "en", []string{body}},
		{"unknown language", "", false, document.UnknownLanguage, body, document.UnknownLanguage, []string{body}},
		{"changed text", prefix, false, "en", combined, "de", []string{body, combined}},
		{"previously processed text", "", true, "en", body, "en", []string{body}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var inputs []string
			detector := serviceLanguageDetectorFunc(func(text string) string {
				inputs = append(inputs, text)
				if text == combined {
					return "de"
				}
				return tc.bodyLanguage
			})
			d := &document.Document{URL: "https://example.com/article", Language: "fr"}
			fetched := &document.Document{
				URL: d.URL, Text: body, Language: "fr", Processed: tc.alreadyProcessed,
			}
			if err := applyServiceContent(context.Background(), d, fetched, tc.prefix, "", detector); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(inputs, tc.wantInputs) {
				t.Errorf("detected texts = %q, want %q", inputs, tc.wantInputs)
			}
			if d.Text != tc.wantText || d.Language != tc.wantLanguage {
				t.Errorf("imported text = %q, language = %q; want %q, %q", d.Text, d.Language, tc.wantText, tc.wantLanguage)
			}
		})
	}
}

func TestServiceImportUsesSourceTitleForLanguage(t *testing.T) {
	d := &document.Document{URL: "https://example.com/title-language"}
	fetched := &document.Document{URL: d.URL, Text: "123", Title: "This document explains how to find useful information in your saved articles."}
	const title = "Eine Suchmaschine organisiert gespeicherte Artikel, damit Leser wichtige Informationen später wiederfinden können."
	if err := applyServiceContent(context.Background(), d, fetched, "", title, document.NewLanguageDetectorFor([]string{"en", "de"}, true)); err != nil {
		t.Fatal(err)
	}
	if d.Language != "de" || d.Title != title || len(d.Metadata) != 0 {
		t.Fatalf("document = %+v, want German source title with no metadata", d)
	}
}
