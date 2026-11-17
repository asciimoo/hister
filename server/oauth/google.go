package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	servererrors "github.com/asciimoo/nogon/server/errors"
)

const (
	scopeUserinfoEmail   ScopeValue = "https://www.googleapis.com/auth/userinfo.email"
	scopeUserinfoProfile ScopeValue = "https://www.googleapis.com/auth/userinfo.profile"
	googleUserInfoURL               = "https://www.googleapis.com/oauth2/v3/userinfo"
)

// GoogleOAuth implements OAuth 2.0 authentication for Google.
type GoogleOAuth struct {
	AuthURL  string
	TokenURL string
	Client   *http.Client
}

// Prepare initializes the Google OAuth provider. No preparation needed for Google.
func (g GoogleOAuth) Prepare(_ context.Context, _ *PrepareRequest) error { return nil }

// GetRedirectURL constructs the Google authorization URL for redirecting users.
func (g GoogleOAuth) GetRedirectURL(req *RedirectURIRequest) string {
	params := &url.Values{}

	params.Add("client_id", req.clientID)
	params.Add("response_type", responseTypeCode.String())
	params.Add("redirect_uri", req.redirectURI)
	params.Add("state", req.state)
	req.addPKCE(*params)
	scopeName, defaultScopes := g.GetScope()
	addScopes(*params, scopeName, defaultScopes, req.scopes)

	return g.AuthURL + "?" + params.Encode()
}

// GetToken exchanges an authorization code for an access token.
func (g GoogleOAuth) GetToken(ctx context.Context, req *TokenRequest) (*http.Response, error) {
	params := &url.Values{}

	params.Add("grant_type", grantTypeAuthorizationCode.String())
	params.Add("code", req.code)
	params.Add("redirect_uri", req.redirectURI)
	params.Add("client_id", req.clientID)
	params.Add("client_secret", req.clientSecret)
	req.addPKCE(*params)

	tokenReq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.TokenURL, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, fmt.Errorf("google: failed to create token request: %w", err)
	}

	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := doRequest(g.Client, tokenReq)
	if err != nil {
		return nil, fmt.Errorf("google: failed to exchange token: %w", err)
	}
	return resp, nil
}

// GetUserInfo fetches user information from Google using the access token.
func (g GoogleOAuth) GetUserInfo(ctx context.Context, response TokenResponse) (*UserInfoResponse, error) {
	var bearer tokenData

	if err := json.Unmarshal(response, &bearer); err != nil {
		return nil, fmt.Errorf("google: failed to parse token response: %w", err)
	}

	if len(bearer.AccessToken) < 1 {
		return nil, errors.New("google: access token not found")
	}

	req, err := newGoogleUserInfoRequest(ctx, bearer.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("google: failed to create UserInfo request: %w", err)
	}

	resp, err := doRequest(g.Client, req)
	if err != nil {
		return nil, fmt.Errorf("google: failed to execute UserInfo request: %w", err)
	}
	defer servererrors.LogCloseBody(resp.Body)

	uBody, err := readResponseBody(resp)
	if err != nil {
		return nil, fmt.Errorf("google: failed to read UserInfo response: %w", err)
	}

	var uData userData

	if err := json.Unmarshal(uBody, &uData); err != nil {
		return nil, fmt.Errorf("google: failed to parse UserInfo response: %w", err)
	}

	if len(uData.Email) < 1 {
		return nil, errors.New("google: failed to parse email from UserInfo response")
	}

	return &UserInfoResponse{
		UID:      "goog-" + uData.Email,
		Email:    uData.Email,
		Username: uData.Name,
	}, nil
}

func newGoogleUserInfoRequest(ctx context.Context, accessToken string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, googleUserInfoURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	return req, nil
}

// GetScope returns the OAuth scopes required for Google authentication.
func (g GoogleOAuth) GetScope() (ScopeName, ScopeValue) {
	str := &strings.Builder{}

	str.WriteString(scopeUserinfoEmail.String())
	str.WriteRune(' ')
	str.WriteString(scopeUserinfoProfile.String())

	return scopeName, ScopeValue(str.String())
}
