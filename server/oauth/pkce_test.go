// SPDX-License-Identifier: AGPL-3.0-or-later

package oauth

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestNewCodeVerifier(t *testing.T) {
	t.Parallel()
	seen := make(map[string]bool)
	for range 10 {
		verifier, err := NewCodeVerifier()
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := base64.RawURLEncoding.DecodeString(verifier)
		if err != nil || len(decoded) != 32 || len(verifier) != 43 {
			t.Fatalf("invalid verifier %q: %v", verifier, err)
		}
		if seen[verifier] {
			t.Fatal("reused a verifier")
		}
		seen[verifier] = true
	}
}

func TestProviderPKCE(t *testing.T) {
	t.Parallel()
	// RFC 7636, Appendix B.
	const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	const challenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	const callback = "https://hister.example/api/oauth/callback"
	for name, create := range map[string]func(*http.Client) Provider{
		"github": func(client *http.Client) Provider {
			return GitHubOAuth{AuthURL: "https://provider.example/auth", TokenURL: "https://provider.example/token", Client: client}
		},
		"google": func(client *http.Client) Provider {
			return GoogleOAuth{AuthURL: "https://provider.example/auth", TokenURL: "https://provider.example/token", Client: client}
		},
		"oidc": func(client *http.Client) Provider {
			return &OIDCOAuth{AuthURL: "https://provider.example/auth", TokenURL: "https://provider.example/token", Client: client}
		},
	} {
		for _, mode := range []string{"enabled", "disabled", "existing constructors"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				wantVerifier := ""
				if mode == "enabled" {
					wantVerifier = verifier
				}
				client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					if req.Method != http.MethodPost || req.URL.RawQuery != "" {
						t.Errorf("token request must use POST without credentials in the URL: %s %s", req.Method, req.URL)
					}
					if got := req.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
						t.Errorf("Content-Type = %q", got)
					}
					if err := req.ParseForm(); err != nil {
						t.Fatal(err)
					}
					for key, want := range map[string]string{
						"client_id": "client-id", "client_secret": "client-secret", "code": "code",
						"redirect_uri": callback, "code_verifier": wantVerifier,
					} {
						if got := req.PostForm.Get(key); got != want {
							t.Errorf("%s = %q, want %q", key, got, want)
						}
					}
					if wantVerifier == "" && req.PostForm.Has("code_verifier") {
						t.Error("disabled PKCE sent code_verifier")
					}
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("access_token=token"))}, nil
				})}
				provider := create(client)
				redirectReq := NewRedirectURIRequest("client-id", callback, "state", nil)
				tokenReq := NewTokenRequest("client-id", "client-secret", "code", callback)
				if mode != "existing constructors" {
					redirectReq.WithPKCE(wantVerifier)
					tokenReq.WithPKCE(wantVerifier)
				}
				u, err := url.Parse(provider.GetRedirectURL(redirectReq))
				if err != nil {
					t.Fatal(err)
				}
				query := u.Query()
				if wantVerifier != "" {
					if query.Get("code_challenge") != challenge || query.Get("code_challenge_method") != "S256" {
						t.Fatalf("incorrect S256 challenge: %v", query)
					}
				} else if query.Has("code_challenge") || query.Has("code_challenge_method") {
					t.Fatalf("disabled PKCE sent a challenge: %v", query)
				}
				if query.Has("code_verifier") || strings.Contains(u.String(), verifier) {
					t.Fatal("authorization URL exposes the verifier")
				}
				resp, err := provider.GetToken(context.Background(), tokenReq)
				if err != nil {
					t.Fatal(err)
				}
				_ = resp.Body.Close()
			})
		}
	}
}
