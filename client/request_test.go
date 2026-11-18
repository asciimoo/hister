package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type trackedResponseBody struct {
	io.Reader
	closed   bool
	closeErr error
}

func (b *trackedResponseBody) Close() error {
	b.closed = true
	return b.closeErr
}

// Keep the request formats and literal protocol identifiers compatible with
// Hister servers even when the client identifies itself as Nogon.
func TestRequestFormatsAndHeaders(t *testing.T) {
	const sourceURL = "https://example.com/a?x=1&y=two words"
	tests := []struct {
		name, method, path, contentType string
		payload                         string
		call                            func(*Client) error
	}{
		{"history", "POST", "/api/history", "application/json", `{"url":"` + sourceURL + `","title":"A title","query":"test"}`, func(c *Client) error { return c.PostHistory("test", sourceURL, "A title") }},
		{"delete history", "POST", "/api/history", "application/json", `{"url":"` + sourceURL + `","query":"test","delete":true}`, func(c *Client) error { return c.DeleteHistoryEntry("test", sourceURL) }},
		{"delete document", "POST", "/api/delete", "application/json", `{"query":"url:` + sourceURL + `"}`, func(c *Client) error { return c.DeleteDocument(sourceURL) }},
		{"delete user", "DELETE", "/api/users/7?purge=true", "", "", func(c *Client) error {
			result, err := c.DeleteUser(context.Background(), 7, true)
			if err == nil && (result.ID != 7 || result.Username != "alice" || result.DeletedDocuments != 2) {
				t.Errorf("delete user result = %+v", result)
			}
			return err
		}},
		{"reindex", "POST", "/api/reindex", "application/json", `{"skipSensitive":true,"detectLanguages":false}`, func(c *Client) error { return c.Reindex(true, false) }},
		{"add page", "POST", "/api/add", "application/x-www-form-urlencoded", url.Values{"url": {sourceURL}, "title": {"A title"}, "text": {"a+b & c"}}.Encode(), func(c *Client) error { return c.AddPage(sourceURL, "A title", "a+b & c") }},
		{"add alias", "POST", "/api/add_alias", "application/x-www-form-urlencoded", url.Values{"alias-keyword": {"test"}, "alias-value": {"a+b & c"}}.Encode(), func(c *Client) error { return c.AddAlias("test", "a+b & c") }},
		{"delete alias", "POST", "/api/delete_alias", "application/x-www-form-urlencoded", url.Values{"alias": {"a+b"}}.Encode(), func(c *Client) error { return c.DeleteAlias("a+b") }},
		{"cleanup", "POST", "/api/cleanup", "", "", func(c *Client) error {
			result, err := c.Cleanup()
			if err == nil && result.HTMLRemoved != 3 {
				t.Errorf("cleanup result = %+v", result)
			}
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := &trackedResponseBody{Reader: strings.NewReader(`{"htmlRemoved":3,"id":7,"username":"alice","deleted_documents":2}`)}
			calls := 0
			c := New("http://nogon.test/prefix/", WithAccessToken("secret"), WithUserAgent("Nogon/test"), WithTargetUserID(7), WithHTTPClient(&http.Client{
				Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					if r.Method != tt.method || r.URL.RequestURI() != "/prefix"+tt.path {
						t.Errorf("request = %s %s", r.Method, r.URL)
					}
					for key, want := range map[string]string{"Content-Type": tt.contentType, "Origin": "hister://", "X-Access-Token": "secret", "User-Agent": "Nogon/test", "X-Nogon-Target-User-ID": "7", "X-Hister-Target-User-ID": "7"} {
						if got := r.Header.Get(key); got != want {
							t.Errorf("header %s = %q, want %q", key, got, want)
						}
					}
					var data []byte
					if r.Body != nil {
						var err error
						data, err = io.ReadAll(r.Body)
						if err != nil {
							t.Fatal(err)
						}
					}
					if tt.contentType == "application/json" {
						var got, want any
						if err := json.Unmarshal(data, &got); err != nil {
							t.Fatal(err)
						}
						if err := json.Unmarshal([]byte(tt.payload), &want); err != nil {
							t.Fatal(err)
						}
						if !reflect.DeepEqual(got, want) {
							t.Errorf("payload = %s, want %s", data, tt.payload)
						}
					} else if string(data) != tt.payload {
						t.Errorf("payload = %q, want %q", data, tt.payload)
					}
					return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
				}),
			}))
			if err := tt.call(c); err != nil {
				t.Fatal(err)
			}
			if calls != 1 || !body.closed {
				t.Errorf("requests = %d, body closed = %t", calls, body.closed)
			}
		})
	}
}

func TestRequestRedirectOrigins(t *testing.T) {
	for _, tt := range []struct {
		name, baseURL string
		locations     []string
		anonymous     bool
		blocked       bool
	}{
		{name: "relative", baseURL: "https://nogon.test", locations: []string{"/redirected"}},
		{name: "same origin", baseURL: "https://nogon.test", locations: []string{"https://NOGON.test/redirected"}},
		{name: "default HTTPS port", baseURL: "https://nogon.test", locations: []string{"https://nogon.test:443/redirected"}},
		{name: "default HTTP port", baseURL: "http://nogon.test:80", locations: []string{"http://nogon.test/redirected"}},
		{name: "different host", baseURL: "https://nogon.test", locations: []string{"https://other.test/redirected"}, blocked: true},
		{name: "subdomain", baseURL: "https://nogon.test", locations: []string{"https://other.nogon.test/redirected"}, blocked: true},
		{name: "different port", baseURL: "https://nogon.test", locations: []string{"https://nogon.test:8443/redirected"}, blocked: true},
		{name: "HTTPS downgrade", baseURL: "https://nogon.test", locations: []string{"http://nogon.test/redirected"}, blocked: true},
		{name: "HTTP upgrade", baseURL: "http://nogon.test", locations: []string{"https://nogon.test/redirected"}, blocked: true},
		{name: "later origin change", baseURL: "https://nogon.test", locations: []string{"/redirected", "https://other.test/final"}, blocked: true},
		{name: "anonymous", baseURL: "https://nogon.test", locations: []string{"https://other.test/redirected"}, anonymous: true},
	} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			t.Run(tt.name+"/"+method, func(t *testing.T) {
				token := "redirect-test-secret"
				if tt.anonymous {
					token = ""
				}
				var bodies []*trackedResponseBody
				calls := 0
				c := New(tt.baseURL, WithAccessToken(token), WithHTTPClient(&http.Client{
					Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
						if r.Method != method || r.Header.Get("X-Access-Token") != token {
							t.Errorf("redirect lost method or token: method = %s, token present = %t", r.Method, r.Header.Get("X-Access-Token") != "")
						}
						body := &trackedResponseBody{Reader: strings.NewReader(`{}`)}
						bodies = append(bodies, body)
						response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body, Request: r}
						if calls < len(tt.locations) {
							response.StatusCode = http.StatusTemporaryRedirect
							response.Header.Set("Location", tt.locations[calls])
						}
						calls++
						return response, nil
					}),
				}))
				var err error
				if method == http.MethodGet {
					_, err = c.Search(&SearchQuery{Text: "test"})
				} else {
					err = c.AddDocumentJSON(&Document{URL: "https://example.com", Text: "private document"})
				}
				wantCalls := len(tt.locations) + 1
				if tt.blocked {
					wantCalls--
					if err == nil || !strings.Contains(err.Error(), "different origin") {
						t.Errorf("error = %v, want an origin rejection", err)
					}
				} else if err != nil {
					t.Errorf("allowed redirect failed: %v", err)
				}
				if calls != wantCalls {
					t.Errorf("sent %d requests, want %d; blocked redirects must not reach their destination", calls, wantCalls)
				}
				if err != nil && strings.Contains(err.Error(), "redirect-test-secret") {
					t.Error("redirect error exposed the access token")
				}
				for _, body := range bodies {
					if !body.closed {
						t.Error("redirect response body was not closed")
					}
				}
			})
		}
	}
}

func TestRequestRedirectPolicy(t *testing.T) {
	policyErr := errors.New("custom redirect rejection")
	for _, tt := range []struct {
		name, location string
		policy         func(*http.Request, []*http.Request) error
		wantErr        error
		wantMessage    string
		wantCalls      int
	}{
		{name: "custom policy rejects", location: "/redirected", policy: func(*http.Request, []*http.Request) error { return policyErr }, wantErr: policyErr, wantCalls: 1},
		{name: "custom policy allows", location: "/redirected", policy: func(*http.Request, []*http.Request) error { return nil }, wantCalls: 2},
		{name: "custom policy cannot allow another origin", location: "https://other.test/redirected", policy: func(*http.Request, []*http.Request) error { return nil }, wantMessage: "different origin", wantCalls: 1},
		{name: "default redirect limit", location: "/loop", wantMessage: "stopped after 10 redirects", wantCalls: 10},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			hc := &http.Client{
				CheckRedirect: tt.policy,
				Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}
					if calls == 1 || tt.location == "/loop" {
						response.StatusCode = http.StatusFound
						response.Header.Set("Location", tt.location)
					}
					return response, nil
				}),
			}
			c := New("https://nogon.test", WithAccessToken("secret"), WithHTTPClient(hc))
			_, err := c.Search(&SearchQuery{})
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("error = %v, want %v", err, tt.wantErr)
				}
			} else if tt.wantMessage != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantMessage) {
					t.Errorf("error = %v, want %q", err, tt.wantMessage)
				}
			} else if err != nil {
				t.Errorf("allowed redirect failed: %v", err)
			}
			if calls != tt.wantCalls {
				t.Errorf("sent %d requests, want %d", calls, tt.wantCalls)
			}
			// Sharing an HTTP client must not apply this client's token policy to
			// unrelated anonymous requests.
			if tt.location == "https://other.test/redirected" {
				calls = 0
				response, err := hc.Get("https://nogon.test/search")
				if err != nil {
					t.Fatalf("original HTTP client's redirect policy changed: %v", err)
				}
				if err := response.Body.Close(); err != nil {
					t.Errorf("close response body: %v", err)
				}
				if calls != 2 {
					t.Errorf("original HTTP client sent %d requests, want 2", calls)
				}
			}
		})
	}
}

func TestRequestResponseErrorsAndCleanup(t *testing.T) {
	closeErr := errors.New("body close failed")
	for _, tt := range []struct {
		name     string
		status   int
		body     string
		closeErr error
		wantErr  string
	}{
		{"success", 200, `{"title":"Preview"}`, nil, ""},
		{"close error", 200, `{"title":"Preview"}`, closeErr, "closing response body"},
		{"decode error", 200, `{"title":`, closeErr, "unexpected EOF"},
		{"unauthorized", 401, " access denied \n", closeErr, "authentication required"},
		{"forbidden", 403, " access denied \n", closeErr, "access denied"},
		{"not found", 404, " missing \n", closeErr, "resource not found"},
		{"server error", 503, " unavailable \n", closeErr, "server error"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := &trackedResponseBody{Reader: strings.NewReader(tt.body), closeErr: tt.closeErr}
			c := New("http://nogon.test", WithHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tt.status, Body: body}, nil
			})}))
			preview, err := c.FetchPreview("https://example.com")
			if tt.wantErr == "" {
				if err != nil || preview == nil || preview.Title != "Preview" {
					t.Fatalf("preview = %+v, error = %v", preview, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
			}
			if tt.status >= 400 {
				var httpErr *HTTPError
				if !errors.As(err, &httpErr) || httpErr.HTTPStatusCode() != tt.status || httpErr.Detail != strings.TrimSpace(tt.body) {
					t.Errorf("HTTP error lost its status or detail: %v", err)
				}
			}
			if tt.name == "close error" && !errors.Is(err, closeErr) {
				t.Errorf("close error was not wrapped: %v", err)
			}
			if !body.closed {
				t.Error("response body was not closed")
			}
		})
	}
}

func TestRequestCancellation(t *testing.T) {
	for name, call := range map[string]func(*Client, context.Context) error{
		"config":      func(c *Client, ctx context.Context) error { _, err := c.FetchConfigContext(ctx); return err },
		"delete user": func(c *Client, ctx context.Context) error { _, err := c.DeleteUser(ctx, 7, true); return err },
		"get user":    func(c *Client, ctx context.Context) error { _, err := c.GetUser(ctx, "alice"); return err },
		"create user": func(c *Client, ctx context.Context) error {
			_, err := c.CreateUser(ctx, "alice", "password123", false)
			return err
		},
		"update user": func(c *Client, ctx context.Context) error {
			_, err := c.UpdateUser(ctx, 7, UserUpdate{Revision: 1, RegenerateToken: true})
			return err
		},
		"user token":  func(c *Client, ctx context.Context) error { _, err := c.GetUserToken(ctx, 7); return err },
		"diagnostics": func(c *Client, ctx context.Context) error { _, err := c.FetchDiagnostics(ctx); return err },
		"exists": func(c *Client, ctx context.Context) error {
			_, err := c.DocumentExistsContext(ctx, "https://example.com")
			return err
		},
		"add": func(c *Client, ctx context.Context) error {
			return c.AddDocumentJSONContext(ctx, &Document{URL: "https://example.com"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			c := New("http://nogon.test", WithHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Context().Err() == nil {
					return nil, errors.New("request lost caller cancellation")
				}
				return nil, r.Context().Err()
			})}))
			if err := call(c, ctx); !errors.Is(err, context.Canceled) {
				t.Errorf("error = %v, want context.Canceled", err)
			}
		})
	}
}

func TestGetUserResponse(t *testing.T) {
	const username = "alice/a+b &?"
	for _, tc := range []struct {
		name, response, wantError string
		status                    int
	}{
		{"found", `{"users":[{"id":7,"username":"alice/a+b &?","is_admin":true}]}`, "", http.StatusOK},
		{"not found", `{"users":[]}`, "not found", http.StatusOK},
		{"missing users", `{}`, "invalid user lookup response", http.StatusOK},
		{"null users", `{"users":null}`, "invalid user lookup response", http.StatusOK},
		{"wrong user", `{"users":[{"id":7,"username":"bob"}]}`, "invalid user lookup response", http.StatusOK},
		{"missing id", `{"users":[{"username":"alice/a+b &?"}]}`, "invalid user lookup response", http.StatusOK},
		{"multiple users", `{"users":[{"id":7,"username":"alice/a+b &?"},{"id":8,"username":"bob"}]}`, "invalid user lookup response", http.StatusOK},
		{"invalid JSON", `<html>old server</html>`, "invalid character", http.StatusOK},
		{"denied", `denied`, "access denied", http.StatusForbidden},
		{"old server", `not found`, "resource not found", http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &trackedResponseBody{Reader: strings.NewReader(tc.response)}
			c := New("https://nogon.test/prefix", WithAccessToken("admin-token"), WithHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodGet || req.URL.Path != "/prefix/api/users" || req.URL.Query().Get("username") != username || req.URL.Query().Get("limit") != "1" || len(req.URL.Query()) != 2 {
					t.Errorf("request = %s %s", req.Method, req.URL)
				}
				if req.Header.Get("X-Access-Token") != "admin-token" {
					t.Error("lookup lost authentication")
				}
				return &http.Response{StatusCode: tc.status, Body: body}, nil
			})}))
			user, err := c.GetUser(t.Context(), username)
			if tc.wantError == "" {
				if err != nil || user == nil || user.ID != 7 || user.Username != username || !user.IsAdmin {
					t.Fatalf("user = %+v, error = %v", user, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) || user != nil {
				t.Fatalf("user = %+v, error = %v, want %q", user, err, tc.wantError)
			}
			if !body.closed {
				t.Error("lookup response was not closed")
			}
		})
	}
}

func TestCreateUserResponse(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		status         int
		wantError      bool
	}{
		{"created", `{"id":7,"username":"alice","is_admin":true}`, http.StatusCreated, false},
		{"empty response", `{}`, http.StatusCreated, true},
		{"null response", `null`, http.StatusCreated, true},
		{"wrong account", `{"id":7,"username":"bob","is_admin":true}`, http.StatusCreated, true},
		{"wrong privileges", `{"id":7,"username":"alice","is_admin":false}`, http.StatusCreated, true},
		{"invalid JSON", `<html>old server</html>`, http.StatusOK, true},
		{"duplicate", `{"code":"username_exists","error":"username already exists"}`, http.StatusConflict, true},
		{"denied", `denied`, http.StatusForbidden, true},
		{"old server", `not found`, http.StatusNotFound, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &trackedResponseBody{Reader: strings.NewReader(tc.response)}
			calls := 0
			c := New("https://nogon.test/prefix", WithAccessToken("admin-token"), WithHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method != http.MethodPost || req.URL.Path != "/prefix/api/users" || req.URL.RawQuery != "" || req.Header.Get("X-Access-Token") != "admin-token" || req.Header.Get("Origin") != "hister://" || req.Header.Get("Content-Type") != "application/json" {
					t.Errorf("unexpected creation request: %s %s", req.Method, req.URL)
				}
				var payload map[string]any
				if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if len(payload) != 3 || payload["username"] != "alice" || payload["password"] != "password123" || payload["is_admin"] != true {
					t.Error("creation request has incorrect account fields")
				}
				return &http.Response{StatusCode: tc.status, Body: body}, nil
			})}))
			user, err := c.CreateUser(t.Context(), "alice", "password123", true)
			if tc.wantError {
				if err == nil || user != nil {
					t.Fatalf("user = %+v, error = %v, want creation failure", user, err)
				}
			} else if err != nil || user == nil || user.ID != 7 || user.Username != "alice" || !user.IsAdmin {
				t.Fatalf("user = %+v, error = %v", user, err)
			}
			if calls != 1 || !body.closed {
				t.Errorf("requests = %d, response closed = %t", calls, body.closed)
			}
		})
	}
}

func TestCreateUserDoesNotRedirectPassword(t *testing.T) {
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			// Passwords need protection even when no administrator token was supplied.
			c := New("https://nogon.test", WithHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return &http.Response{StatusCode: status, Header: http.Header{"Location": {"https://other.test/api/users"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
				}
				return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(`{"id":7,"username":"alice","is_admin":false}`))}, nil
			})}))
			_, err := c.CreateUser(t.Context(), "alice", "password123", false)
			if err == nil || !strings.Contains(err.Error(), "different origin") || calls != 1 {
				t.Fatalf("password redirect: requests = %d, error = %v", calls, err)
			}
		})
	}
}

func TestGetUserTokenResponse(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		status         int
		wantError      bool
	}{
		{"found", `{"token":"user-token"}`, http.StatusOK, false},
		{"missing token", `{}`, http.StatusOK, true},
		{"empty token", `{"token":""}`, http.StatusOK, true},
		{"denied", `denied`, http.StatusForbidden, true},
		{"deleted account", `not found`, http.StatusNotFound, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &trackedResponseBody{Reader: strings.NewReader(tc.response)}
			c := New("https://nogon.test/prefix", WithAccessToken("admin-token"), WithHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodGet || req.URL.Path != "/prefix/api/users/7/token" || req.URL.RawQuery != "" || req.Header.Get("X-Access-Token") != "admin-token" {
					t.Errorf("unexpected token request: %s %s", req.Method, req.URL)
				}
				return &http.Response{StatusCode: tc.status, Body: body}, nil
			})}))
			token, err := c.GetUserToken(t.Context(), 7)
			if (err != nil) != tc.wantError || (!tc.wantError && token != "user-token") || (tc.wantError && token != "") {
				t.Fatalf("token present = %t, error = %v", token != "", err)
			}
			if !body.closed {
				t.Error("token response was not closed")
			}
		})
	}
}

func TestDocumentExistsResponseHandling(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNoContent, http.StatusNotFound, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			body := &trackedResponseBody{Reader: strings.NewReader("")}
			c := New("http://nogon.test", WithHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodHead || r.URL.Query().Get("url") != "https://example.com/?a=1&b=2" {
					t.Errorf("request = %s %s", r.Method, r.URL)
				}
				return &http.Response{StatusCode: status, Body: body}, nil
			})}))
			exists, err := c.DocumentExists("https://example.com/?a=1&b=2")
			if exists != (status == http.StatusOK) || (err != nil) != (status == http.StatusForbidden) {
				t.Errorf("exists = %t, error = %v", exists, err)
			}
			if !body.closed {
				t.Error("response body was not closed")
			}
		})
	}
}

func TestSearchRequestAndResponse(t *testing.T) {
	query := &SearchQuery{Text: `label:"a & b"`, Limit: 3, PageKey: "next+page"}
	for _, response := range []string{`{"total":2}`, `null`, `{"total":2}invalid`} {
		t.Run(response, func(t *testing.T) {
			body := &trackedResponseBody{Reader: strings.NewReader(response)}
			c := New("http://nogon.test", WithHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				var got SearchQuery
				if err := json.Unmarshal([]byte(r.URL.Query().Get("query")), &got); err != nil {
					t.Fatal(err)
				}
				if r.Method != http.MethodGet || r.URL.Path != "/search" || !reflect.DeepEqual(got, *query) {
					t.Errorf("request = %s %s, query = %+v", r.Method, r.URL, got)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
			})}))
			result, err := c.Search(query)
			switch response {
			case `null`:
				if result != nil || err != nil {
					t.Errorf("result = %+v, error = %v", result, err)
				}
			case `{"total":2}`:
				if err != nil || result == nil || result.Total != 2 {
					t.Errorf("result = %+v, error = %v", result, err)
				}
			default:
				if err == nil {
					t.Error("accepted a response with trailing invalid data")
				}
			}
			if !body.closed {
				t.Error("response body was not closed")
			}
		})
	}
}

func TestBatchRequestSplittingPreservesResults(t *testing.T) {
	var sizes []int
	var bodies []*trackedResponseBody
	c := New("http://nogon.test", WithMaxBatchBodyBytes(1<<20), WithAllowSensitive(), WithIgnoreRules(), WithHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Ops []addDocumentOperation `json:"ops"`
		}
		if r.URL.Path != "/api/batch" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected batch request: %s", r.URL)
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		sizes = append(sizes, len(request.Ops))
		for _, op := range request.Ops {
			if op.Op != "add" || !op.SkipSensitiveCheck || op.Metadata["ignore_skip_rules"] != true {
				t.Errorf("document options lost: %+v", op)
			}
		}
		status, response := http.StatusOK, `{"results":[{"status":201}]}`
		if len(request.Ops) > 1 || request.Ops[0].URL == "https://example.com/large" {
			status, response = http.StatusRequestEntityTooLarge, `{"error":"too large","limit_bytes":100}`
		}
		body := &trackedResponseBody{Reader: strings.NewReader(response)}
		bodies = append(bodies, body)
		return &http.Response{StatusCode: status, Body: body}, nil
	})}))
	results, err := c.AddDocumentsJSON([]*Document{{URL: "https://example.com/first"}, {URL: "https://example.com/large"}, {URL: "https://example.com/last"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 || results[0].Status != 201 || results[1].Status != 413 || results[2].Status != 201 || !strings.Contains(results[1].Error, "too large; encoded document request") {
		t.Errorf("batch results = %+v", results)
	}
	if !reflect.DeepEqual(sizes, []int{3, 1, 2, 1, 1}) {
		t.Errorf("batch sizes = %v", sizes)
	}
	for _, body := range bodies {
		if !body.closed {
			t.Error("batch response body was not closed")
		}
	}
}

func TestBatchCapabilityDiscovery(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		status     int
		want       int64
	}{
		{"advertised", `{"maxBatchBodyBytes":12345}`, 200, 12345},
		{"legacy", `{}`, 200, legacyMaxBatchBodyBytes},
		{"invalid limit", `{"maxBatchBodyBytes":-1}`, 200, legacyMaxBatchBodyBytes},
		{"invalid JSON", `{`, 200, legacyMaxBatchBodyBytes},
		{"HTTP error", `{"maxBatchBodyBytes":12345}`, 403, legacyMaxBatchBodyBytes},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := &trackedResponseBody{Reader: strings.NewReader(tt.body), closeErr: errors.New("close failed")}
			calls := 0
			c := New("http://nogon.test", WithHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: tt.status, Body: body}, nil
			})}))
			for range 2 {
				if got := c.MaxBatchBodyBytes(); got != tt.want {
					t.Errorf("batch limit = %d, want %d", got, tt.want)
				}
			}
			if calls != 1 || !body.closed {
				t.Errorf("discovery requests = %d, body closed = %t", calls, body.closed)
			}
		})
	}
}

func TestRequestEncodingAndTransportErrors(t *testing.T) {
	transportErr := errors.New("transport unavailable")
	calls := 0
	c := New("http://nogon.test", WithHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return nil, transportErr
	})}))
	d := &Document{URL: "https://example.com", Metadata: map[string]any{"invalid": func() {}}}
	var encodingErr *json.UnsupportedTypeError
	if err := c.AddDocumentJSON(d); !errors.As(err, &encodingErr) {
		t.Errorf("encoding error = %v", err)
	}
	if calls != 0 {
		t.Fatal("sent a document that could not be encoded")
	}
	if err := c.PostHistory("test", d.URL, "title"); !errors.Is(err, transportErr) {
		t.Errorf("transport error was not preserved: %v", err)
	}
	if calls != 1 {
		t.Errorf("requests = %d, want 1", calls)
	}
}

func TestBatchRequestResponseValidation(t *testing.T) {
	for _, tt := range []struct {
		name, response, wantError string
		status                    int
	}{
		{"missing results", `{"results":[]}`, "batch response contained 0 results for 1 documents", http.StatusOK},
		{"invalid JSON", `{"results":`, "unexpected EOF", http.StatusOK},
		{"forbidden", "denied", "access denied", http.StatusForbidden},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := &trackedResponseBody{Reader: strings.NewReader(tt.response), closeErr: errors.New("close failed")}
			calls := 0
			c := New("http://nogon.test", WithMaxBatchBodyBytes(1<<20), WithHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: tt.status, Body: body}, nil
			})}))
			results, err := c.AddDocumentsJSON([]*Document{{URL: "https://example.com"}})
			if len(results) != 0 || err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Errorf("results = %+v, error = %v, want %q", results, err, tt.wantError)
			}
			if calls != 1 || !body.closed {
				t.Errorf("requests = %d, body closed = %t", calls, body.closed)
			}
		})
	}
}

func TestDeleteUserResponse(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		status         int
		purge          bool
		wantError      bool
	}{
		{"empty account", `{"id":7,"username":"alice","deleted_documents":0}`, http.StatusOK, false, false},
		{"purged", `{"id":7,"username":"renamed","deleted_documents":2}`, http.StatusOK, true, false},
		{"empty purge", `{"id":7,"username":"alice","deleted_documents":0}`, http.StatusOK, true, false},
		{"empty response", `{}`, http.StatusOK, false, true},
		{"null response", `null`, http.StatusOK, false, true},
		{"wrong ID", `{"id":8,"username":"alice","deleted_documents":0}`, http.StatusOK, false, true},
		{"missing ID", `{"username":"alice","deleted_documents":0}`, http.StatusOK, false, true},
		{"missing name", `{"id":7,"deleted_documents":0}`, http.StatusOK, false, true},
		{"missing count", `{"id":7,"username":"alice"}`, http.StatusOK, false, true},
		{"null count", `{"id":7,"username":"alice","deleted_documents":null}`, http.StatusOK, false, true},
		{"negative count", `{"id":7,"username":"alice","deleted_documents":-1}`, http.StatusOK, true, true},
		{"unexpected purge", `{"id":7,"username":"alice","deleted_documents":2}`, http.StatusOK, false, true},
		{"invalid JSON", `<html>old server</html>`, http.StatusOK, false, true},
		{"denied", "denied", http.StatusForbidden, true, true},
		{"missing endpoint", "not found", http.StatusNotFound, true, true},
		{"purge required", `{"code":"purge_required","error":"enable purge"}`, http.StatusConflict, false, true},
		{"last admin", `{"code":"last_admin","error":"cannot remove the last administrator"}`, http.StatusConflict, true, true},
		{"failed purge", "failed to purge", http.StatusInternalServerError, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &trackedResponseBody{Reader: strings.NewReader(tc.response)}
			calls := 0
			c := New("https://nogon.test/prefix", WithAccessToken("admin-token"), WithHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				query := ""
				if tc.purge {
					query = "purge=true"
				}
				if req.Method != http.MethodDelete || req.URL.Path != "/prefix/api/users/7" || req.URL.RawQuery != query || req.Body != nil {
					t.Errorf("unexpected deletion request: %s %s", req.Method, req.URL)
				}
				if req.Header.Get("X-Access-Token") != "admin-token" || req.Header.Get("Origin") != "hister://" {
					t.Error("deletion lost authentication headers")
				}
				return &http.Response{StatusCode: tc.status, Body: body}, nil
			})}))
			result, err := c.DeleteUser(t.Context(), 7, tc.purge)
			if tc.wantError {
				if err == nil || result != nil {
					t.Fatalf("unexpected deletion success: %+v, error = %v", result, err)
				}
				if tc.status >= 400 {
					var httpErr *HTTPError
					if !errors.As(err, &httpErr) || httpErr.StatusCode != tc.status || httpErr.Detail != tc.response {
						t.Fatalf("lost server error details: %v", err)
					}
				}
			} else if err != nil || result == nil || result.ID != 7 || result.Username == "" || result.DeletedDocuments < 0 {
				t.Fatalf("incorrect deletion result: %+v, error = %v", result, err)
			}
			if calls != 1 || !body.closed {
				t.Errorf("requests = %d, body closed = %t", calls, body.closed)
			}
		})
	}
}

func TestDeleteUserRequiresID(t *testing.T) {
	c := New("https://nogon.test", WithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("invalid user ID contacted the server")
		return nil, errors.New("unexpected request")
	})}))
	if result, err := c.DeleteUser(t.Context(), 0, true); err == nil || result != nil {
		t.Fatalf("invalid user ID: result = %+v, error = %v", result, err)
	}
}

func TestUpdateUserResponse(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		status         int
		wantError      bool
	}{
		{"updated", `{"id":7,"username":"renamed","is_admin":false,"revision":5,"token":"new-token"}`, http.StatusOK, false},
		{"empty", `{}`, http.StatusOK, true},
		{"null", `null`, http.StatusOK, true},
		{"wrong ID", `{"id":8,"username":"renamed","is_admin":false,"revision":5,"token":"new-token"}`, http.StatusOK, true},
		{"wrong name", `{"id":7,"username":"alice","is_admin":false,"revision":5,"token":"new-token"}`, http.StatusOK, true},
		{"wrong privileges", `{"id":7,"username":"renamed","is_admin":true,"revision":5,"token":"new-token"}`, http.StatusOK, true},
		{"unchanged revision", `{"id":7,"username":"renamed","is_admin":false,"revision":4,"token":"new-token"}`, http.StatusOK, true},
		{"missing token", `{"id":7,"username":"renamed","is_admin":false,"revision":5}`, http.StatusOK, true},
		{"invalid JSON", `<html>old server</html>`, http.StatusOK, true},
		{"stale revision", `{"code":"stale_revision","error":"user has changed"}`, http.StatusConflict, true},
		{"last admin", `{"code":"last_admin","error":"cannot remove the last administrator"}`, http.StatusConflict, true},
		{"denied", "denied", http.StatusForbidden, true},
		{"missing endpoint", "not found", http.StatusNotFound, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &trackedResponseBody{Reader: strings.NewReader(tc.response)}
			calls := 0
			c := New("https://nogon.test/prefix", WithAccessToken("admin-token"), WithHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method != http.MethodPatch || req.URL.Path != "/prefix/api/users/7" || req.URL.RawQuery != "" || req.Header.Get("X-Access-Token") != "admin-token" || req.Header.Get("Origin") != "hister://" || req.Header.Get("Content-Type") != "application/json" {
					t.Errorf("unexpected update: %s %s", req.Method, req.URL)
				}
				var payload map[string]any
				if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if len(payload) != 5 || payload["revision"] != float64(4) || payload["username"] != "renamed" || payload["password"] != "replacement123" || payload["is_admin"] != false || payload["regenerate_token"] != true {
					t.Error("update did not send the combined changes")
				}
				return &http.Response{StatusCode: tc.status, Body: body}, nil
			})}))
			user, err := c.UpdateUser(t.Context(), 7, UserUpdate{Revision: 4, Username: new("renamed"), Password: new("replacement123"), IsAdmin: new(false), RegenerateToken: true})
			if tc.wantError {
				if err == nil || user != nil {
					t.Fatalf("unexpected update success: %+v, error %v", user, err)
				}
				if tc.status >= 400 {
					var httpErr *HTTPError
					if !errors.As(err, &httpErr) || httpErr.StatusCode != tc.status || httpErr.Detail != tc.response {
						t.Fatalf("lost server error details: %v", err)
					}
				}
			} else if err != nil || user.ID != 7 || user.Revision != 5 || user.Username != "renamed" || user.IsAdmin || user.Token != "new-token" {
				t.Fatalf("incorrect update result: %+v, error %v", user, err)
			}
			if calls != 1 || !body.closed {
				t.Errorf("requests = %d, body closed = %t", calls, body.closed)
			}
		})
	}
}

func TestUpdateUserPartialRequest(t *testing.T) {
	for _, response := range []string{
		`{"id":7,"username":"renamed","is_admin":false,"revision":5}`,
		`{"id":7,"username":"renamed","is_admin":false,"revision":5,"token":"unexpected-secret"}`,
	} {
		c := New("https://nogon.test", WithHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			var payload map[string]any
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if len(payload) != 2 || payload["revision"] != float64(4) || payload["username"] != "renamed" {
				t.Errorf("partial request changed omitted fields: %v", payload)
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(response))}, nil
		})}))
		_, err := c.UpdateUser(t.Context(), 7, UserUpdate{Revision: 4, Username: new("renamed")})
		if (err != nil) != strings.Contains(response, "token") {
			t.Fatalf("partial update error = %v", err)
		}
	}
}

func TestUpdateUserDoesNotRedirectPassword(t *testing.T) {
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			c := New("https://nogon.test", WithHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: status, Header: http.Header{"Location": {"https://other.test/api/users/7"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
			})}))
			_, err := c.UpdateUser(t.Context(), 7, UserUpdate{Revision: 1, Password: new("replacement123")})
			if err == nil || !strings.Contains(err.Error(), "different origin") || calls != 1 {
				t.Fatalf("password redirect: requests = %d, error = %v", calls, err)
			}
		})
	}
}

func TestUpdateUserRequiresIDAndRevision(t *testing.T) {
	for _, tc := range []struct {
		id       uint
		revision uint64
	}{{0, 1}, {7, 0}, {7, 9223372036854775807}, {7, 18446744073709551615}} {
		c := New("https://nogon.test", WithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Error("invalid update identifiers contacted the server")
			return nil, errors.New("unexpected request")
		})}))
		if user, err := c.UpdateUser(t.Context(), tc.id, UserUpdate{Revision: tc.revision, RegenerateToken: true}); err == nil || user != nil {
			t.Fatalf("invalid update identifiers: user = %+v, error = %v", user, err)
		}
	}
}
