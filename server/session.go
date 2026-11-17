// SPDX-FileContributor: Adam Tauber <asciimoo@gmail.com>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/asciimoo/nogon/server/model"

	"github.com/gorilla/sessions"
	"github.com/rs/zerolog/log"
)

const sessionTokenBytes = 32

const tokenAuthKey = "token_auth_proof"

const userSessionVersionKey = "user_session_version"

const configurationLoginKey = "configuration_login"

const configurationLoginMethodKey = "configuration_login_method"

func recordConfigurationLogin(c *webContext, session *sessions.Session, method string) {
	if lease, ok := c.Request.Context().Value(runtimeRequestKey{}).(*runtimeRequest); ok {
		session.Values[configurationLoginKey] = lease.generation.authID
		session.Values[configurationLoginMethodKey] = method
	}
}

type databaseSessionStore struct {
	Options      *sessions.Options
	maxAge       time.Duration
	authKey      []byte
	cleanupMutex sync.Mutex
	nextCleanup  time.Time
}

func newSessionStore(secretKey []byte, baseURL string, maxAge int) *databaseSessionStore {
	parsedBaseURL, _ := url.Parse(baseURL)
	authKey := append([]byte(nil), secretKey...)
	if len(authKey) == 0 {
		authKey = []byte(rand.Text())
	}
	return &databaseSessionStore{
		Options: &sessions.Options{
			Path:     "/",
			MaxAge:   maxAge,
			HttpOnly: true,
			Secure:   parsedBaseURL != nil && parsedBaseURL.Scheme == "https",
			SameSite: http.SameSiteLaxMode,
		},
		maxAge:  time.Duration(maxAge) * time.Second,
		authKey: authKey,
	}
}

func (s *databaseSessionStore) Get(r *http.Request, name string) (*sessions.Session, error) {
	return sessions.GetRegistry(r).Get(s, name)
}

func (s *databaseSessionStore) New(r *http.Request, name string) (*sessions.Session, error) {
	session := sessions.NewSession(s, name)
	options := *s.Options
	session.Options = &options
	session.IsNew = true

	cookie, err := r.Cookie(name)
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			return session, nil
		}
		return session, err
	}
	if !validSessionToken(cookie.Value) {
		return session, nil
	}

	now := time.Now()
	record, err := model.GetWebSession(sessionTokenHash(cookie.Value))
	if err != nil {
		if errors.Is(err, model.ErrWebSessionNotFound) {
			return session, nil
		}
		return session, err
	}
	if !record.ExpiresAt.After(now) {
		if err := model.DeleteWebSession(record.TokenHash); err != nil {
			return session, err
		}
		return session, nil
	}
	if err := decodeSessionValues(record.Data, &session.Values); err != nil {
		return session, err
	}
	s.maybeCleanup(now)
	session.ID = cookie.Value
	session.IsNew = false
	return session, nil
}

func (s *databaseSessionStore) Save(_ *http.Request, w http.ResponseWriter, session *sessions.Session) error {
	if session.Options.MaxAge < 0 {
		if session.ID != "" {
			if err := model.DeleteWebSession(sessionTokenHash(session.ID)); err != nil {
				return err
			}
		}
		http.SetCookie(w, sessions.NewCookie(session.Name(), "", session.Options))
		return nil
	}

	data, err := encodeSessionValues(session.Values)
	if err != nil {
		return err
	}
	now := time.Now()
	if session.ID == "" {
		token, err := newSessionToken()
		if err != nil {
			return err
		}
		session.ID = token
		if err := model.CreateWebSession(&model.WebSession{
			TokenHash: sessionTokenHash(token),
			Data:      data,
			ExpiresAt: now.Add(s.maxAge),
		}); err != nil {
			return err
		}
		session.IsNew = false
	} else if err := model.UpdateWebSession(sessionTokenHash(session.ID), data, now.Add(s.maxAge)); err != nil {
		return err
	}

	http.SetCookie(w, sessions.NewCookie(session.Name(), session.ID, session.Options))
	s.maybeCleanup(now)
	return nil
}

func (s *databaseSessionStore) Rotate(session *sessions.Session) error {
	if session.ID == "" {
		return nil
	}
	if err := model.DeleteWebSession(sessionTokenHash(session.ID)); err != nil {
		return err
	}
	session.ID = ""
	session.IsNew = true
	return nil
}

func (s *databaseSessionStore) Refresh(r *http.Request, w http.ResponseWriter, name string) error {
	session, err := s.Get(r, name)
	if err != nil || session.ID == "" {
		return err
	}
	return session.Save(r, w)
}

func (s *databaseSessionStore) tokenProof(token string) string {
	mac := hmac.New(sha256.New, s.authKey)
	_, _ = mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *databaseSessionStore) tokenAuthenticated(session *sessions.Session, token string) bool {
	proof, ok := session.Values[tokenAuthKey].(string)
	if !ok {
		return false
	}
	return hmac.Equal([]byte(proof), []byte(s.tokenProof(token)))
}

func (s *databaseSessionStore) authenticateToken(session *sessions.Session, token string) {
	resetSessionValuesAfterAuthentication(session)
	session.Values[tokenAuthKey] = s.tokenProof(token)
}

func (s *databaseSessionStore) userAuthenticated(session *sessions.Session, user *model.User) bool {
	userID, ok := session.Values["user_id"].(uint)
	if !ok || userID == 0 || userID != user.ID {
		return false
	}
	value, present := session.Values[userSessionVersionKey]
	if !present {
		// Legacy sessions remain valid until the account's first password reset.
		return user.SessionVersion == 0
	}
	version, ok := value.(uint)
	return ok && version == user.SessionVersion
}

func (s *databaseSessionStore) authenticateUser(session *sessions.Session, user *model.User) {
	resetSessionValuesAfterAuthentication(session)
	session.Values["user_id"] = user.ID
	session.Values[userSessionVersionKey] = user.SessionVersion
}

func resetSessionValuesAfterAuthentication(session *sessions.Session) {
	csrfToken, hasCSRFToken := session.Values[tokName]
	session.Values = make(map[any]any)
	if hasCSRFToken {
		session.Values[tokName] = csrfToken
	}
}

func (s *databaseSessionStore) maybeCleanup(now time.Time) {
	s.cleanupMutex.Lock()
	if now.Before(s.nextCleanup) {
		s.cleanupMutex.Unlock()
		return
	}
	s.nextCleanup = now.Add(time.Hour)
	s.cleanupMutex.Unlock()
	if err := model.DeleteExpiredWebSessions(now); err != nil {
		log.Error().Err(err).Msg("Failed to clean up expired web sessions")
	}
}

func newSessionToken() (string, error) {
	token := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(token); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(token), nil
}

func validSessionToken(token string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(decoded) == sessionTokenBytes
}

func sessionTokenHash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func encodeSessionValues(values map[any]any) ([]byte, error) {
	var data bytes.Buffer
	if err := gob.NewEncoder(&data).Encode(values); err != nil {
		return nil, err
	}
	return data.Bytes(), nil
}

func decodeSessionValues(data []byte, values *map[any]any) error {
	return gob.NewDecoder(bytes.NewReader(data)).Decode(values)
}
