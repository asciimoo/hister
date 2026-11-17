package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	servererrors "github.com/asciimoo/nogon/server/errors"
)

const scopeReadUser ScopeValue = "read:user"

// GitHubOAuth implements OAuth 2.0 authentication for GitHub.
type GitHubOAuth struct {
	AuthURL  string
	TokenURL string
	Client   *http.Client
}

// Prepare initializes the GitHub OAuth provider. No preparation needed for GitHub.
func (g GitHubOAuth) Prepare(_ context.Context, _ *PrepareRequest) error { return nil }

// GetRedirectURL constructs the GitHub authorization URL for redirecting users.
func (g GitHubOAuth) GetRedirectURL(req *RedirectURIRequest) string {
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
func (g GitHubOAuth) GetToken(ctx context.Context, req *TokenRequest) (*http.Response, error) {
	params := &url.Values{}

	params.Add("client_id", req.clientID)
	params.Add("client_secret", req.clientSecret)
	params.Add("code", req.code)
	params.Add("redirect_uri", req.redirectURI)
	req.addPKCE(*params)

	tokenReq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.TokenURL, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, errors.New("github: failed to create token request")
	}
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := doRequest(g.Client, tokenReq)
	if err != nil {
		return nil, fmt.Errorf("github: failed to exchange token: %w", err)
	}
	return resp, nil
}

// GetUserInfo fetches user information from GitHub using the access token.
func (g GitHubOAuth) GetUserInfo(ctx context.Context, response TokenResponse) (*UserInfoResponse, error) {
	v, err := url.ParseQuery(string(response))
	if err != nil {
		return nil, fmt.Errorf("github: failed to parse token response: %w", err)
	}

	accessToken := v.Get("access_token")
	if accessToken == "" {
		return nil, errors.New("github: no access token found")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return nil, fmt.Errorf("github: failed to create UserInfo request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := doRequest(g.Client, req)
	if err != nil {
		return nil, fmt.Errorf("github: failed to execute UserInfo request: %w", err)
	}
	defer servererrors.LogCloseBody(resp.Body)

	uBody, err := readResponseBody(resp)
	if err != nil {
		return nil, fmt.Errorf("github: failed to read UserInfo response: %w", err)
	}

	var uData struct {
		ID    uint64 `json:"id"`
		Login string `json:"login"`
		Email string `json:"email"`
	}

	if err := json.Unmarshal(uBody, &uData); err != nil {
		return nil, fmt.Errorf("github: failed to parse UserInfo response: %w", err)
	}

	if len(uData.Login) < 1 {
		return nil, errors.New("github: failed to get user login from UserInfo")
	}
	if uData.ID == 0 {
		return nil, errors.New("github: failed to get user id from UserInfo")
	}

	return &UserInfoResponse{
		// Logins can be renamed and recycled. Keep numeric IDs in a separate
		// namespace from legacy gh-<login> identities, even for numeric logins.
		UID:      "github:" + strconv.FormatUint(uData.ID, 10),
		Email:    uData.Email,
		Username: uData.Login,
	}, nil
}

// GetScope returns the OAuth scopes required for GitHub authentication.
func (g GitHubOAuth) GetScope() (ScopeName, ScopeValue) {
	return scopeName, scopeReadUser
}
