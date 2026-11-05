package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/testutil"

	"github.com/gorilla/websocket"
)

func TestReindexReportsDimensionMismatch(t *testing.T) {
	embeddings := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"embedding":[0.25,0.75]}]}`)
	}))
	defer embeddings.Close()
	cfg := testutil.Config(t)
	cfg.App.AccessToken = "secret"
	testutil.InitModelWithConfig(t, cfg)
	idx := newServerTestIndexer(t, cfg)
	if err := idx.Add(&document.Document{URL: "https://example.com/reindex", Text: "Stored content", Processed: true}); err != nil {
		t.Fatal(err)
	}
	idx.Close()
	cfg.SemanticSearch.Enable = true
	cfg.SemanticSearch.Dimensions = 2000
	cfg.SemanticSearch.EmbeddingEndpoint = embeddings.URL
	idx = newServerTestIndexer(t, cfg)
	response := testutil.ServeHTTP(t, registerEndpoints(cfg, idx), http.MethodPost, "/api/reindex", strings.NewReader(`{}`), map[string]string{
		"X-Access-Token": "secret",
		"Origin":         "hister://",
		"Content-Type":   "application/json",
	})
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "expected 2000, got 2") || !strings.Contains(response.Body.String(), "semantic_search.dimensions") {
		t.Fatalf("reindex did not report the configuration problem: status=%d body=%s", response.Code, response.Body)
	}
}

func TestRequestBodyLimits(t *testing.T) {
	_, handler := newPublicTokenTestServer(t)
	padding := strings.Repeat("x", 1<<20)
	for _, tc := range []struct {
		path, contentType, body, token string
	}{
		{"/mcp", "application/json", `{"jsonrpc":"2.0","id":1,"method":"ping","padding":"` + padding + `"}`, ""},
		{"/api/login", "application/json", `{"username":"nobody","password":"secret","padding":"` + padding + `"}`, ""},
		{"/api/token-login", "application/json", `{"token":"secret","padding":"` + padding + `"}`, ""},
		{"/api/update", "application/json", `{"padding":"` + padding + `"}`, "secret"},
		{"/api/label", "application/json", `{"padding":"` + padding + `"}`, "secret"},
		{"/api/history", "application/json", `{"query":"limit test","padding":"` + padding + `"}`, "secret"},
		{"/api/delete", "application/json", `{"query":"limit test","padding":"` + padding + `"}`, "secret"},
		{"/api/reindex", "application/json", `{"padding":"` + padding + `"}`, "secret"},
		{"/api/rules", "application/x-www-form-urlencoded", "skip=" + padding, "secret"},
		{"/api/add_alias", "application/x-www-form-urlencoded", "alias-keyword=test&alias-value=" + padding, "secret"},
		{"/api/delete_alias", "application/x-www-form-urlencoded", "alias=" + padding, "secret"},
	} {
		for _, chunked := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/chunked=%t", tc.path, chunked), func(t *testing.T) {
				req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
				req.Header.Set("Content-Type", tc.contentType)
				req.Header.Set("Origin", "hister://")
				req.Header.Set("X-Access-Token", tc.token)
				if chunked {
					req.ContentLength = -1
					req.TransferEncoding = []string{"chunked"}
				}
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if rec.Code != http.StatusRequestEntityTooLarge {
					t.Fatalf("status = %d, want 413", rec.Code)
				}
				var response struct {
					Code       string `json:"code"`
					LimitBytes int64  `json:"limit_bytes"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if response.Code != "request_too_large" || response.LimitBytes != 1<<20 {
					t.Fatalf("unexpected size error: %+v", response)
				}
			})
		}
	}
}

func TestRequestBodyLimitDuringCSRFParsing(t *testing.T) {
	cfg, handler := newPublicTokenTestServer(t)
	for _, multipartForm := range []bool{false, true} {
		for _, oversized := range []bool{false, true} {
			for _, headerToken := range []bool{false, true} {
				t.Run(fmt.Sprintf("multipart=%t/oversized=%t/header=%t", multipartForm, oversized, headerToken), func(t *testing.T) {
					configRec := httptest.NewRecorder()
					handler.ServeHTTP(configRec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
					token := configRec.Header().Get("X-CSRF-Token")
					if configRec.Code != http.StatusOK || token == "" {
						t.Fatal("config response did not supply a CSRF token")
					}
					fields := url.Values{"alias-keyword": {t.Name()}, "alias-value": {"replacement"}}
					if !headerToken {
						fields.Set(tokName, token)
					}
					if oversized {
						fields.Set("padding", strings.Repeat("x", 1<<20))
					}
					body := fields.Encode()
					contentType := "application/x-www-form-urlencoded"
					if multipartForm {
						var buf bytes.Buffer
						writer := multipart.NewWriter(&buf)
						for key, values := range fields {
							if err := writer.WriteField(key, values[0]); err != nil {
								t.Fatal(err)
							}
						}
						if err := writer.Close(); err != nil {
							t.Fatal(err)
						}
						body, contentType = buf.String(), writer.FormDataContentType()
					}
					req := httptest.NewRequest(http.MethodPost, "/api/add_alias", strings.NewReader(body))
					req.ContentLength = -1
					req.Header.Set("Content-Type", contentType)
					req.Header.Set("Origin", "https://proxy.example")
					req.Header.Set("X-Access-Token", "secret")
					if headerToken {
						req.Header.Set("X-CSRF-Token", token)
					}
					req.AddCookie(responseSessionCookie(t, configRec))
					rec := httptest.NewRecorder()
					handler.ServeHTTP(rec, req)
					want := http.StatusOK
					if oversized {
						want = http.StatusRequestEntityTooLarge
					}
					if rec.Code != want {
						t.Fatalf("status = %d, want %d: %s", rec.Code, want, rec.Body.String())
					}
					_, saved := cfg.Rules.Aliases[t.Name()]
					if saved == oversized {
						t.Fatalf("alias saved = %t for oversized = %t", saved, oversized)
					}
				})
			}
		}
	}
}

func TestRulePreviewBodyLimitBoundary(t *testing.T) {
	_, handler := newPublicTokenTestServer(t)
	const prefix = `{"pattern":"example","padding":"`
	for _, size := range []int{64<<10 + 1, 1 << 20, 1<<20 + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			body := prefix + strings.Repeat("x", size-len(prefix)-2) + `"}`
			req := httptest.NewRequest(http.MethodPost, "/api/rules/preview", strings.NewReader(body))
			req.ContentLength = -1
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", "hister://")
			req.Header.Set("X-Access-Token", "secret")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			want := http.StatusOK
			if size > 1<<20 {
				want = http.StatusRequestEntityTooLarge
			}
			if rec.Code != want {
				t.Fatalf("status = %d, want %d", rec.Code, want)
			}
		})
	}
}

func TestMCPBodyLimitBoundary(t *testing.T) {
	_, handler := newPublicTokenTestServer(t)
	const prefix = `{"jsonrpc":"2.0","id":1,"method":"ping","padding":"`
	for _, size := range []int{1 << 20, 1<<20 + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			body := prefix + strings.Repeat("x", size-len(prefix)-2) + `"}`
			req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
			req.ContentLength = -1
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			want := http.StatusOK
			if size > 1<<20 {
				want = http.StatusRequestEntityTooLarge
			}
			if rec.Code != want {
				t.Fatalf("status = %d, want %d", rec.Code, want)
			}
		})
	}
}

func TestDocumentBodyLimitsPreserveConfiguredAllowance(t *testing.T) {
	cfg, handler := newPublicTokenTestServer(t)
	cfg.Server.MaxBatchBodySize = 2
	for _, size := range []int{1<<20 + 1, 2<<20 + 1} {
		padding := strings.Repeat("x", size)
		doc := `{"url":"https://example.com/large","processed":true,"text":"` + padding + `"}`
		for _, tc := range []struct {
			path, body string
			status     int
		}{
			{"/api/add", doc, http.StatusCreated},
			{"/api/batch", `{"ops":[{"op":"add",` + doc[1:] + `]}`, http.StatusOK},
			// A deliberately invalid PDF should reach validation within the upload
			// allowance, but oversized requests must be rejected before processing.
			{"/api/add_pdf", `{"document":{},"pdf":"invalid base64","padding":"` + padding + `"}`, http.StatusBadRequest},
		} {
			t.Run(fmt.Sprintf("%s/%d", tc.path, size), func(t *testing.T) {
				req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
				req.ContentLength = -1
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Origin", "hister://")
				req.Header.Set("X-Access-Token", "secret")
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				want := tc.status
				if size > 2<<20 {
					want = http.StatusRequestEntityTooLarge
				}
				if rec.Code != want {
					t.Fatalf("status = %d, want %d: %s", rec.Code, want, rec.Body.String())
				}
			})
		}
	}
}

func TestSearchWebSocketMessageLimit(t *testing.T) {
	cfg, handler := newPublicTokenTestServer(t)
	server := httptest.NewServer(handler)
	defer server.Close()
	const prefix = `{"text":"needle","padding":"`
	for _, size := range []int{64 << 10, 64<<10 + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			// A small write buffer fragments the message, so the limit must apply
			// across frames rather than to each individual frame.
			dialer := websocket.Dialer{WriteBufferSize: 1024}
			conn, resp, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/search", http.Header{"Origin": {cfg.Server.BaseURL}})
			if resp != nil {
				defer func() { _ = resp.Body.Close() }()
			}
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.Close() }()
			if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
				t.Fatal(err)
			}
			body := prefix + strings.Repeat("x", size-len(prefix)-2) + `"}`
			if err := conn.WriteMessage(websocket.TextMessage, []byte(body)); err != nil {
				t.Fatal(err)
			}
			_, _, err = conn.ReadMessage()
			if size <= 64<<10 {
				if err != nil {
					t.Fatal(err)
				}
			} else if !websocket.IsCloseError(err, websocket.CloseMessageTooBig) {
				t.Fatalf("read error = %v, want message too big close", err)
			}
		})
	}
}

// Shorten deadlines that the production server actually sets. Missing
// deadlines stay missing, without making the test wait for their full duration.
type shortenedDeadlineConn struct{ net.Conn }

func (c shortenedDeadlineConn) SetReadDeadline(deadline time.Time) error {
	if !deadline.IsZero() && time.Until(deadline) > 100*time.Millisecond {
		deadline = time.Now().Add(100 * time.Millisecond)
	}
	return c.Conn.SetReadDeadline(deadline)
}

func (c shortenedDeadlineConn) SetWriteDeadline(deadline time.Time) error {
	if !deadline.IsZero() && time.Until(deadline) > 100*time.Millisecond {
		deadline = time.Now().Add(100 * time.Millisecond)
	}
	return c.Conn.SetWriteDeadline(deadline)
}

type shortenedDeadlineListener struct{ net.Listener }

func (l shortenedDeadlineListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return shortenedDeadlineConn{conn}, nil
}

func TestRuntimeConnectionTimeouts(t *testing.T) {
	for _, phase := range []string{"headers", "body", "idle", "handler"} {
		t.Run(phase, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			bodyErrors := make(chan error, 1)
			r := &serverRuntime{
				ctx: context.Background(), failures: make(chan error, 1),
				current: &runtimeGeneration{handler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					if _, err := io.Copy(io.Discard, req.Body); err != nil {
						bodyErrors <- err
						http.Error(w, "request body timeout", http.StatusRequestTimeout)
						return
					}
					if phase == "handler" {
						time.Sleep(150 * time.Millisecond)
						if req.Context().Err() != nil {
							http.Error(w, "handler context canceled", http.StatusInternalServerError)
							return
						}
					}
					w.WriteHeader(http.StatusNoContent)
				})},
			}
			serving := r.startListener(shortenedDeadlineListener{listener})
			defer func() {
				_ = serving.server.Close()
				<-serving.done
			}()
			conn, err := net.Dial("tcp", listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.Close() }()
			if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
				t.Fatal(err)
			}
			request := "GET / HTTP/1.1\r\nHost: localhost\r\n"
			if phase == "body" {
				request = "POST / HTTP/1.1\r\nHost: localhost\r\nContent-Length: 10\r\n\r\nx"
			} else if phase != "headers" {
				request += "\r\n"
			}
			if _, err := io.WriteString(conn, request); err != nil {
				t.Fatal(err)
			}
			reader := bufio.NewReader(conn)
			if phase != "headers" {
				response, err := http.ReadResponse(reader, nil)
				if err != nil {
					t.Fatal(err)
				}
				_ = response.Body.Close()
				if phase == "body" {
					if response.StatusCode != http.StatusRequestTimeout {
						t.Fatalf("status = %d, want 408", response.StatusCode)
					}
					select {
					case err := <-bodyErrors:
						timeout, ok := errors.AsType[net.Error](err)
						if !ok || !timeout.Timeout() {
							t.Fatalf("body read error = %v, want timeout", err)
						}
					default:
						t.Fatal("body read did not time out")
					}
					return
				}
				if response.StatusCode != http.StatusNoContent || response.Close {
					t.Fatal("initial request did not leave an idle keepalive connection")
				}
				if phase == "handler" {
					return
				}
			}
			if _, err := io.ReadAll(reader); err != nil {
				t.Fatalf("server did not close %s connection before client deadline: %v", phase, err)
			}
		})
	}
}
