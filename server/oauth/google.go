package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	servererrors "github.com/asciimoo/hister/server/errors"
)

const (
	scopeUserinfoEmail   ScopeValue = "https://www.googleapis.com/auth/userinfo.email"
	scopeUserinfoProfile ScopeValue = "https://www.googleapis.com/auth/userinfo.profile"
)

// GoogleOAuth implements OAuth 2.0 authentication for Google.
type GoogleOAuth struct {
	AuthURL  string
	TokenURL string
}

// Prepare initializes the Google OAuth provider. No preparation needed for Google.
func (g GoogleOAuth) Prepare(_ context.Context, _ *PrepareRequest) error { return nil }

// GetRedirectURL constructs the Google authorization URL for redirecting users.
func (g GoogleOAuth) GetRedirectURL(req *RedirectURIRequest) string {
	params := &url.Values{}

	scopeName, scopeValue := g.GetScope()
	if len(req.scopes) > 0 { // config-based scopes
		params.Add(scopeName.String(), strings.Join(req.scopes, " "))
	} else { // default scopes
		params.Add(scopeName.String(), scopeValue.String())
	}

	params.Add("client_id", req.clientID)
	params.Add("response_type", responseTypeCode.String())
	params.Add("redirect_uri", req.redirectURI)
	params.Add("state", req.state)

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

	tokenReq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.TokenURL, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, fmt.Errorf("google: failed to create token request: %w", err)
	}

	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	return http.DefaultClient.Do(tokenReq)
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

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"https://www.googleapis.com/oauth2/v3/userinfo?access_token="+bearer.AccessToken,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("google: failed to create UserInfo request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+bearer.AccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google: failed to execute UserInfo request: %w", err)
	}
	defer servererrors.LogCloseBody(resp.Body)

	uBody, err := io.ReadAll(resp.Body)
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

// GetScope returns the OAuth scopes required for Google authentication.
func (g GoogleOAuth) GetScope() (ScopeName, ScopeValue) {
	str := &strings.Builder{}

	str.WriteString(scopeUserinfoEmail.String())
	str.WriteRune(' ')
	str.WriteString(scopeUserinfoProfile.String())

	return scopeName, ScopeValue(str.String())
}
