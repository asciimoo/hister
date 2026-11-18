package oauth

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestProviderTokenRequestRejectsUnsuccessfulResponse(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Body:       io.NopCloser(strings.NewReader("provider unavailable")),
			Header:     make(http.Header),
		}, nil
	})}

	providers := map[string]Provider{
		"github": GitHubOAuth{TokenURL: "https://provider.example/token", Client: client},
		"google": GoogleOAuth{TokenURL: "https://provider.example/token", Client: client},
		"oidc":   &OIDCOAuth{TokenURL: "https://provider.example/token", Client: client},
	}
	for name, provider := range providers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			resp, err := provider.GetToken(context.Background(), NewTokenRequest(
				"client-id", "client-secret", "code", "https://nogon.example/callback",
			))
			if err == nil {
				t.Fatal("GetToken returned no error")
			}
			if resp != nil {
				t.Fatal("GetToken returned a response for an unsuccessful status")
			}
			if !strings.Contains(err.Error(), "unexpected response status code: 502") {
				t.Fatalf("GetToken error = %q", err)
			}
		})
	}
}

func TestProviderUserInfoRejectsUnsuccessfulResponse(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Body:       io.NopCloser(strings.NewReader("unauthorized")),
			Header:     make(http.Header),
		}, nil
	})}
	providers := map[string]Provider{
		"github": GitHubOAuth{Client: client},
		"google": GoogleOAuth{Client: client},
		"oidc":   &OIDCOAuth{Client: client, UserInfoURL: "https://provider.example/userinfo"},
	}
	responses := map[string]TokenResponse{
		"github": []byte("access_token=token"),
		"google": []byte(`{"access_token":"token"}`),
		"oidc":   []byte(`{"access_token":"token"}`),
	}
	for name, provider := range providers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := provider.GetUserInfo(context.Background(), responses[name])
			if err == nil {
				t.Fatal("GetUserInfo returned no error")
			}
			if !strings.Contains(err.Error(), "unexpected response status code: 401") {
				t.Fatalf("GetUserInfo error = %q", err)
			}
		})
	}
}

func TestProviderHTTPClientTimeout(t *testing.T) {
	t.Parallel()

	provider := GoogleOAuth{
		TokenURL: "https://provider.example/token",
		Client: &http.Client{
			Timeout: 20 * time.Millisecond,
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				<-req.Context().Done()
				return nil, req.Context().Err()
			}),
		},
	}
	started := time.Now()
	resp, err := provider.GetToken(context.Background(), NewTokenRequest(
		"client-id", "client-secret", "code", "https://nogon.example/callback",
	))
	if err == nil {
		t.Fatal("GetToken returned no error")
	}
	if resp != nil {
		t.Fatal("GetToken returned a response after timing out")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("GetToken took %s to honor the HTTP client timeout", elapsed)
	}
}

func TestReadTokenResponseRejectsOversizedBody(t *testing.T) {
	t.Parallel()

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(
			strings.Repeat("x", maxOAuthResponseBodySize+1),
		)),
	}
	_, err := ReadTokenResponse(resp)
	if err == nil {
		t.Fatal("ReadTokenResponse returned no error")
	}
	if !strings.Contains(err.Error(), "response body exceeds") {
		t.Fatalf("ReadTokenResponse error = %q", err)
	}
}

func TestReadTokenResponseRejectsUnsuccessfulResponse(t *testing.T) {
	t.Parallel()

	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(strings.NewReader(`{"error":"invalid_grant"}`)),
	}
	_, err := ReadTokenResponse(resp)
	if err == nil {
		t.Fatal("ReadTokenResponse returned no error")
	}
	if !strings.Contains(err.Error(), "unexpected response status code: 400") {
		t.Fatalf("ReadTokenResponse error = %q", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
