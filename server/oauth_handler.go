// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
	"time"

	servererrors "github.com/asciimoo/hister/server/errors"
	"github.com/asciimoo/hister/server/model"
	"github.com/asciimoo/hister/server/oauth"

	"github.com/rs/zerolog/log"
)

const (
	oauthStateKey       = "oauth_state"
	oauthProviderKey    = "oauth_provider"
	oauthVerifierKey    = "oauth_code_verifier"
	oauthReturnToKey    = "oauth_return_to"
	oauthRequestTimeout = 10 * time.Second
)

func generateOAuthState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// authReturnPath accepts only UI pages beneath the configured base path.
// Auth and API routes are excluded to prevent redirect loops and chains.
func authReturnPath(target, prefix string) string {
	home := prefix + "/"
	if !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") || strings.ContainsAny(target, "\\\r\n") {
		return home
	}
	u, err := url.Parse(target)
	if err != nil || u.IsAbs() || u.Host != "" || u.User != nil {
		return home
	}
	if u.Path != prefix && !strings.HasPrefix(u.Path, prefix+"/") {
		return home
	}
	page := strings.TrimSuffix(strings.TrimPrefix(u.Path, prefix), "/")
	switch page {
	case "", "/add", "/rules", "/history", "/profile", "/help", "/about", "/api-docs", "/preview", "/extractors", "/settings":
	default:
		return home
	}
	u.Path = prefix + page
	if page == "" {
		u.Path = home
	}
	u.RawPath = ""
	return u.String()
}

// serveOAuthRedirect starts the OAuth flow for a given provider.
// It stores the state, provider, PKCE verifier, and return path in the server side session.
func serveOAuthRedirect(c *webContext) {
	if !c.Config.App.UserHandling {
		http.Error(c.Response, "user handling is disabled", http.StatusForbidden)
		return
	}
	providerName := c.Request.URL.Query().Get("provider")
	entry, ok := c.Config.Server.OAuth[providerName]
	if !ok || entry == nil {
		http.Error(c.Response, "unknown oauth provider", http.StatusBadRequest)
		return
	}
	provider, ok := oauth.NewProvider(providerName, oauth.ProviderConfig{
		AuthURL:       entry.AuthURL,
		TokenURL:      entry.TokenURL,
		UserInfoURL:   entry.UserInfoURL,
		IdentityClaim: entry.IdentityClaim,
		AdminGroups:   entry.AdminGroups,
		GroupsClaim:   entry.GroupsClaim,
	})
	if !ok {
		http.Error(c.Response, "oauth provider not available", http.StatusBadRequest)
		return
	}
	if entry.ConfigurationURL != "" {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		if err := provider.Prepare(ctx, oauth.NewPrepareRequest(entry.ConfigurationURL)); err != nil {
			log.Error().Err(err).Str("provider", providerName).Msg("oauth: failed to prepare provider")
			serve500(c)
			return
		}
	}
	state, err := generateOAuthState()
	if err != nil {
		serve500(c)
		return
	}
	var verifier string
	if !entry.DisablePKCE {
		verifier, err = oauth.NewCodeVerifier()
		if err != nil {
			serve500(c)
			return
		}
	}
	session, err := sessionStore.Get(c.Request, storeName)
	if err != nil {
		serve500(c)
		return
	}
	session.Values[oauthStateKey] = state
	session.Values[oauthProviderKey] = providerName
	session.Values[oauthVerifierKey] = verifier
	session.Values[oauthReturnToKey] = authReturnPath(c.Request.URL.Query().Get("return_to"), c.Config.BasePathPrefix())
	if err := session.Save(c.Request, c.Response); err != nil {
		serve500(c)
		return
	}
	callbackURL := c.Config.BaseURL("/api/oauth/callback") + "?provider=" + providerName
	redirectURL := provider.GetRedirectURL(oauth.NewRedirectURIRequest(entry.ClientID, callbackURL, state, entry.Scopes).WithPKCE(verifier))
	http.Redirect(c.Response, c.Request, redirectURL, http.StatusFound)
}

// serveOAuthCallback handles the OAuth provider callback.
// It verifies the state, exchanges the code for a token, retrieves user info,
// and either finds or creates a local user account before establishing a session.
func serveOAuthCallback(c *webContext) {
	if !c.Config.App.UserHandling {
		http.Error(c.Response, "user handling is disabled", http.StatusForbidden)
		return
	}
	providerName := c.Request.URL.Query().Get("provider")
	entry, ok := c.Config.Server.OAuth[providerName]
	if !ok || entry == nil {
		http.Error(c.Response, "unknown oauth provider", http.StatusBadRequest)
		return
	}
	code := c.Request.URL.Query().Get("code")
	state := c.Request.URL.Query().Get("state")
	session, err := sessionStore.Get(c.Request, storeName)
	if err != nil {
		serve500(c)
		return
	}
	storedState, ok := session.Values[oauthStateKey].(string)
	if !ok || storedState == "" || storedState != state {
		http.Error(c.Response, "invalid oauth state", http.StatusBadRequest)
		return
	}
	if session.Values[oauthProviderKey] != providerName {
		http.Error(c.Response, "invalid oauth provider", http.StatusBadRequest)
		return
	}
	verifier, hasVerifier := session.Values[oauthVerifierKey].(string)
	returnTo, _ := session.Values[oauthReturnToKey].(string)
	delete(session.Values, oauthStateKey)
	delete(session.Values, oauthProviderKey)
	delete(session.Values, oauthVerifierKey)
	delete(session.Values, oauthReturnToKey)
	// Consume the flow before contacting the provider, including on errors.
	if err := session.Save(c.Request, c.Response); err != nil {
		serve500(c)
		return
	}
	if !hasVerifier || (verifier == "" && !entry.DisablePKCE) {
		http.Error(c.Response, "invalid oauth verifier; restart login", http.StatusBadRequest)
		return
	}
	if c.Request.URL.Query().Get("error") != "" || code == "" {
		http.Error(c.Response, "oauth authorization failed; restart login", http.StatusBadRequest)
		return
	}
	provider, ok := oauth.NewProvider(providerName, oauth.ProviderConfig{
		AuthURL:       entry.AuthURL,
		TokenURL:      entry.TokenURL,
		UserInfoURL:   entry.UserInfoURL,
		IdentityClaim: entry.IdentityClaim,
		AdminGroups:   entry.AdminGroups,
		GroupsClaim:   entry.GroupsClaim,
	})
	if !ok {
		serve500(c)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), oauthRequestTimeout)
	defer cancel()
	if entry.ConfigurationURL != "" {
		if err := provider.Prepare(ctx, oauth.NewPrepareRequest(entry.ConfigurationURL)); err != nil {
			log.Error().Err(err).Str("provider", providerName).Msg("oauth: failed to prepare provider")
			serve500(c)
			return
		}
	}
	callbackURL := c.Config.BaseURL("/api/oauth/callback") + "?provider=" + providerName
	tokenResp, err := provider.GetToken(ctx, oauth.NewTokenRequest(
		entry.ClientID, entry.ClientSecret, code, callbackURL,
	).WithPKCE(verifier))
	if err != nil {
		log.Error().Err(err).Str("provider", providerName).Msg("oauth: failed to exchange token")
		serve500(c)
		return
	}
	defer servererrors.LogCloseBody(tokenResp.Body)
	tokenBody, err := oauth.ReadTokenResponse(tokenResp)
	if err != nil {
		log.Error().Err(err).Msg("oauth: failed to read token response")
		serve500(c)
		return
	}
	userInfo, err := provider.GetUserInfo(ctx, tokenBody)
	if err != nil {
		log.Error().Err(err).Str("provider", providerName).Msg("oauth: failed to get user info")
		serve500(c)
		return
	}
	user, err := model.GetUserByOAuthID(userInfo.UID)
	if err != nil {
		username := cmp.Or(userInfo.Username, userInfo.Email, userInfo.UID)
		user, err = model.CreateOAuthUser(username, userInfo.UID)
		if err == model.ErrUserAlreadyExists {
			suffix := userInfo.UID
			if len(suffix) > 8 {
				suffix = suffix[len(suffix)-8:]
			}
			user, err = model.CreateOAuthUser(username+"-"+suffix, userInfo.UID)
		}
		if err != nil {
			log.Error().Err(err).Str("provider", providerName).Msg("oauth: failed to create user")
			serve500(c)
			return
		}
	}
	if userInfo.IsAdmin != nil {
		user, err = model.SyncOAuthAdmin(userInfo.UID, *userInfo.IsAdmin)
		if err != nil {
			log.Error().Err(err).Str("provider", providerName).Msg("oauth: failed to synchronize administrator privileges")
			serve500(c)
			return
		}
	}
	if err := sessionStore.Rotate(session); err != nil {
		serve500(c)
		return
	}
	sessionStore.authenticateUser(session, user)
	if err := session.Save(c.Request, c.Response); err != nil {
		serve500(c)
		return
	}
	returnPath := authReturnPath(returnTo, c.Config.BasePathPrefix())
	basePath := (&url.URL{Path: c.Config.BasePathPrefix()}).EscapedPath()
	http.Redirect(c.Response, c.Request, c.Config.BaseURL(strings.TrimPrefix(returnPath, basePath)), http.StatusFound)
}
