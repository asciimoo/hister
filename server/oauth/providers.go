// Package oauth provides OAuth 2.0 authentication support for external providers.
//
// This package implements OAuth 2.0 authentication flows for multiple identity providers:
//   - GitHub
//   - Google
//   - Generic OIDC (OpenID Connect) providers
//
// Each provider implements a common OAuth interface that handles the authorization
// code flow. The package supports:
//   - Authorization URL generation
//   - Token exchange
//   - User profile fetching
//   - Automatic provider discovery via OIDC
//
// OAuth providers are configured in the application config file with client IDs,
// secrets, and scopes. Users can sign in using any configured provider, and their
// OAuth ID is linked to their Hister account.
//
// NewProvider creates a fresh instance with optional endpoint overrides:
//
//	provider, ok := oauth.NewProvider("github", oauth.ProviderConfig{})
//	if ok {
//		authURL := provider.GetRedirectURL(oauth.NewRedirectURIRequest(clientID, callbackURL, state, nil))
//		// Redirect the user to authURL.
//	}
//
// Example configuration:
//
//	oauth:
//	  github:
//	    client_id: "your-client-id"
//	    client_secret: "your-client-secret"
//	    scopes: ["user:email"]
package oauth

import (
	"cmp"
	"context"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

const (
	responseTypeCode           ResponseType = "code"
	grantTypeAuthorizationCode GrantType    = "authorization_code"
	scopeName                  ScopeName    = "scope"
)

type (
	// ScopeName represents the name of an OAuth scope parameter.
	ScopeName string
	// ScopeValue represents the value of an OAuth scope.
	ScopeValue string
	// ResponseType represents the OAuth response type (e.g., "code").
	ResponseType string
	// GrantType represents the OAuth grant type (e.g., "authorization_code").
	GrantType string

	// TokenResponse contains the raw token response from an OAuth provider.
	TokenResponse []byte

	// PrepareRequest contains parameters for preparing an OAuth provider.
	PrepareRequest struct {
		configurationURL string
	}

	// RedirectURIRequest contains parameters for building an OAuth redirect URI.
	RedirectURIRequest struct {
		clientID      string
		redirectURI   string
		state         string
		scopes        []string
		codeChallenge string
	}

	// TokenRequest contains parameters for exchanging an authorization code for a token.
	TokenRequest struct {
		clientID     string
		clientSecret string
		code         string
		redirectURI  string
		codeVerifier string
	}

	// UserInfoResponse contains user information retrieved from an OAuth provider.
	UserInfoResponse struct {
		UID      string
		Email    string
		Username string
		// IsAdmin is nil when the provider does not manage administrator privileges.
		IsAdmin *bool
	}
)

// String returns the string representation of ScopeName.
func (sn ScopeName) String() string {
	return string(sn)
}

// String returns the string representation of ScopeValue.
func (sv ScopeValue) String() string {
	return string(sv)
}

// String returns the string representation of ResponseType.
func (rt ResponseType) String() string {
	return string(rt)
}

// String returns the string representation of GrantType.
func (gt GrantType) String() string {
	return string(gt)
}

// NewPrepareRequest creates a new PrepareRequest with the given configuration URL.
func NewPrepareRequest(cURL string) *PrepareRequest {
	return &PrepareRequest{configurationURL: cURL}
}

// NewRedirectURIRequest creates a new RedirectURIRequest with the given parameters.
// scopes is a list of additional OAuth scopes. Provider defaults are always included.
func NewRedirectURIRequest(clientID string, redirectURI string, state string, scopes []string) *RedirectURIRequest {
	return &RedirectURIRequest{clientID: clientID, redirectURI: redirectURI, state: state, scopes: scopes}
}

func addScopes(params url.Values, name ScopeName, defaults ScopeValue, configured []string) {
	scopes := make([]string, 0, len(configured)+1)
	seen := make(map[string]struct{}, cap(scopes))

	add := func(value string) {
		for scope := range strings.FieldsSeq(value) {
			if _, ok := seen[scope]; ok {
				continue
			}
			seen[scope] = struct{}{}
			scopes = append(scopes, scope)
		}
	}

	add(defaults.String())
	for _, scope := range configured {
		add(scope)
	}

	if len(scopes) > 0 {
		params.Set(name.String(), strings.Join(scopes, " "))
	}
}

// NewTokenRequest creates a new TokenRequest with the given parameters.
func NewTokenRequest(clientID string, clientSecret string, code string, redirectURI string) *TokenRequest {
	return &TokenRequest{clientID: clientID, clientSecret: clientSecret, code: code, redirectURI: redirectURI}
}

// Provider is the exported interface for OAuth provider implementations.
type Provider interface {
	Prepare(context.Context, *PrepareRequest) error
	GetRedirectURL(*RedirectURIRequest) string
	GetToken(context.Context, *TokenRequest) (*http.Response, error)
	GetUserInfo(context.Context, TokenResponse) (*UserInfoResponse, error)
	GetScope() (ScopeName, ScopeValue)
}

// ProviderConfig holds endpoint overrides and OIDC identity and role mapping.
type ProviderConfig struct {
	AuthURL       string
	TokenURL      string
	UserInfoURL   string
	IdentityClaim string
	AdminGroups   []string
	GroupsClaim   string
}

// ProviderNames returns the supported provider names in alphabetical order.
func ProviderNames() []string {
	return slices.Sorted(maps.Keys(providerFactories))
}

// NewProvider creates a fresh provider instance for the given provider name.
// Empty endpoints use provider defaults or OIDC discovery.
// Returns nil, false if the provider name is unknown.
func NewProvider(name string, cfg ProviderConfig) (Provider, bool) {
	create, ok := providerFactories[name]
	if !ok {
		return nil, false
	}
	return create(cfg), true
}

var providerFactories = map[string]func(ProviderConfig) Provider{
	"github": func(cfg ProviderConfig) Provider {
		return GitHubOAuth{
			AuthURL:  cmp.Or(cfg.AuthURL, "https://github.com/login/oauth/authorize"),
			TokenURL: cmp.Or(cfg.TokenURL, "https://github.com/login/oauth/access_token"),
		}
	},
	"google": func(cfg ProviderConfig) Provider {
		return GoogleOAuth{
			AuthURL:  cmp.Or(cfg.AuthURL, "https://accounts.google.com/o/oauth2/auth"),
			TokenURL: cmp.Or(cfg.TokenURL, "https://accounts.google.com/o/oauth2/token"),
		}
	},
	"oidc": func(cfg ProviderConfig) Provider {
		return &OIDCOAuth{
			AuthURL:       cfg.AuthURL,
			TokenURL:      cfg.TokenURL,
			UserInfoURL:   cfg.UserInfoURL,
			IdentityClaim: cfg.IdentityClaim,
			AdminGroups:   cfg.AdminGroups,
			GroupsClaim:   cfg.GroupsClaim,
		}
	},
}

type (
	tokenData struct {
		AccessToken string `json:"access_token"`
	}

	userData struct {
		Email             string `json:"email"`              // all
		Login             string `json:"login"`              // github
		Name              string `json:"name"`               // google
		PreferredUsername string `json:"preferred_username"` // oidc
	}
)
