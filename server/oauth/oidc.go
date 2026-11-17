package oauth

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	servererrors "github.com/asciimoo/nogon/server/errors"
)

const (
	scopeOpenID  ScopeValue = "openid"
	scopeProfile ScopeValue = "profile"
	scopeEmail   ScopeValue = "email"
)

// OIDCOAuth implements OAuth 2.0 authentication for generic OpenID Connect providers.
// It automatically discovers provider configuration from a well-known URL.
type OIDCOAuth struct {
	AuthURL     string       `json:"authorization_endpoint"`
	TokenURL    string       `json:"token_endpoint"`
	UserInfoURL string       `json:"userinfo_endpoint"`
	Client      *http.Client `json:"-"`

	// Identity and role mapping are local configuration, never set by discovery.
	IdentityClaim string   `json:"-"`
	AdminGroups   []string `json:"-"`
	GroupsClaim   string   `json:"-"`

	Scopes       []ScopeValue   `json:"scopes_supported"`
	ResponseType []ResponseType `json:"response_types_supported"`
	GrantType    []GrantType    `json:"grant_types_supported"`

	ConfigurationURL string
}

func (o *OIDCOAuth) fetch(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.ConfigurationURL, nil)
	if err != nil {
		return fmt.Errorf("oidc: failed to create request: %w", err)
	}

	resp, err := doRequest(o.Client, req)
	if err != nil {
		return fmt.Errorf("oidc: failed to fetch configuration: %w", err)
	}
	defer servererrors.LogCloseBody(resp.Body)

	body, err := readResponseBody(resp)
	if err != nil {
		return fmt.Errorf("oidc: failed to read configuration response: %w", err)
	}

	err = json.Unmarshal(body, &o)
	if err != nil {
		return fmt.Errorf("oidc: failed to parse configuration: %w", err)
	}

	return nil
}

// Prepare initializes the OIDC OAuth provider by fetching its configuration.
// This must be called before using other methods.
func (o *OIDCOAuth) Prepare(ctx context.Context, req *PrepareRequest) error {
	if req.configurationURL == "" {
		return fmt.Errorf("oidc: missing configuration URL")
	}

	o.ConfigurationURL = req.configurationURL

	// A manually configured userinfo_url takes priority over the discovery document.
	manualUserInfoURL := o.UserInfoURL

	if err := o.fetch(ctx); err != nil {
		return err
	}

	if manualUserInfoURL != "" {
		o.UserInfoURL = manualUserInfoURL
	}

	if o.AuthURL == "" || o.TokenURL == "" {
		return fmt.Errorf("oidc: empty AuthURL or TokenURL")
	}

	if o.UserInfoURL == "" {
		return fmt.Errorf("oidc: userinfo_endpoint not found in discovery document; set userinfo_url in config to specify it manually")
	}

	if len(o.Scopes) < 1 || slices.Index(o.Scopes, scopeOpenID) == -1 {
		return fmt.Errorf("oidc: must have \"%s\" in scopes_supported", scopeOpenID)
	}

	if len(o.ResponseType) < 1 || slices.Index(o.ResponseType, responseTypeCode) == -1 {
		return fmt.Errorf("oidc: must have \"%s\" in response_types_supported", responseTypeCode)
	}

	if len(o.GrantType) < 1 || slices.Index(o.GrantType, grantTypeAuthorizationCode) == -1 {
		return fmt.Errorf("oidc: must have \"%s\" in grant_types_supported", grantTypeAuthorizationCode)
	}

	return nil
}

// GetRedirectURL constructs the OIDC authorization URL for redirecting users.
func (o *OIDCOAuth) GetRedirectURL(req *RedirectURIRequest) string {
	params := &url.Values{}

	params.Add("client_id", req.clientID)
	params.Add("response_type", responseTypeCode.String())
	params.Add("redirect_uri", req.redirectURI)
	params.Add("state", req.state)
	req.addPKCE(*params)

	scopeName, defaultScopes := o.GetScope()
	addScopes(*params, scopeName, defaultScopes, req.scopes)

	return o.AuthURL + "?" + params.Encode()
}

// GetToken exchanges an authorization code for an access token.
func (o *OIDCOAuth) GetToken(ctx context.Context, req *TokenRequest) (*http.Response, error) {
	params := &url.Values{}

	params.Set("grant_type", grantTypeAuthorizationCode.String())
	params.Set("code", req.code)
	params.Set("redirect_uri", req.redirectURI)
	params.Set("client_id", req.clientID)
	params.Set("client_secret", req.clientSecret)
	req.addPKCE(*params)

	tokenReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.TokenURL, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, errors.New("oidc: failed to create token request")
	}

	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := doRequest(o.Client, tokenReq)
	if err != nil {
		return nil, fmt.Errorf("oidc: failed to exchange token: %w", err)
	}
	return resp, nil
}

// GetUserInfo fetches user information from the OIDC provider using the access token.
func (o *OIDCOAuth) GetUserInfo(ctx context.Context, response TokenResponse) (*UserInfoResponse, error) {
	var bearer tokenData

	if err := json.Unmarshal(response, &bearer); err != nil {
		return nil, err
	}

	if len(bearer.AccessToken) < 1 {
		return nil, fmt.Errorf("oidc: failed to get access token")
	}

	if o.UserInfoURL == "" {
		return nil, fmt.Errorf("oidc: userinfo endpoint is not configured; set userinfo_url in config or provide a configuration_url that includes userinfo_endpoint")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.UserInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("oidc: failed to create UserInfo request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+bearer.AccessToken)

	resp, err := doRequest(o.Client, req)
	if err != nil {
		return nil, fmt.Errorf("oidc: failed to fetch UserInfo response: %w", err)
	}
	defer servererrors.LogCloseBody(resp.Body)

	uBody, err := readResponseBody(resp)
	if err != nil {
		return nil, fmt.Errorf("oidc: failed to read UserInfo response: %w", err)
	}

	var claims oidcClaims
	if err := json.Unmarshal(uBody, &claims); err != nil {
		return nil, fmt.Errorf("oidc: failed to parse UserInfo response: %w", err)
	}

	claim := cmp.Or(o.IdentityClaim, "email")
	id, err := claims.stringValue(claim)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("oidc: UserInfo claim %q must be a nonempty string", claim)
	}
	email, err := claims.stringValue("email")
	if err != nil {
		return nil, err
	}
	username, err := claims.stringValue("preferred_username")
	if err != nil {
		return nil, err
	}

	// Preserve existing email identities. Other claims have a separate namespace
	// so changing the selected claim cannot match an unrelated existing account.
	uid := "oidc-" + id
	if claim != "email" {
		uid = "oidc:" + url.QueryEscape(claim) + ":" + id
	}

	info := &UserInfoResponse{
		UID:      uid,
		Email:    email,
		Username: username,
	}
	if len(o.AdminGroups) > 0 {
		isAdmin := false
		groupClaim := cmp.Or(o.GroupsClaim, "groups")
		if raw, ok := claims[groupClaim]; ok {
			var groups []any
			if err := json.Unmarshal(raw, &groups); err != nil {
				return nil, fmt.Errorf("oidc: UserInfo claim %q must be an array of strings", groupClaim)
			}
			for _, value := range groups {
				group, ok := value.(string)
				if !ok {
					return nil, fmt.Errorf("oidc: UserInfo claim %q must be an array of strings", groupClaim)
				}
				if slices.Contains(o.AdminGroups, group) {
					isAdmin = true
				}
			}
		}
		info.IsAdmin = &isAdmin
	}
	return info, nil
}

// oidcClaims preserves exact claim names and types, including custom claims.
type oidcClaims map[string]json.RawMessage

func (c oidcClaims) stringValue(name string) (string, error) {
	value, ok := c[name]
	if !ok {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(value, &s); err != nil {
		return "", fmt.Errorf("oidc: UserInfo claim %q must be a string", name)
	}
	return s, nil
}

// GetScope returns the OAuth scopes required for OIDC authentication.
func (o *OIDCOAuth) GetScope() (ScopeName, ScopeValue) {
	str := &strings.Builder{}

	str.WriteString(scopeOpenID.String())
	str.WriteRune(' ')
	str.WriteString(scopeEmail.String())
	str.WriteRune(' ')
	str.WriteString(scopeProfile.String())

	return scopeName, ScopeValue(str.String())
}
