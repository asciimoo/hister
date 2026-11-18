package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// targetUserIDHeader is sent by admin CLI callers to request a specific user_id
// for indexed documents. The server only honours it for admin users in
// multiuser mode.
const targetUserIDHeader = "X-Nogon-Target-User-ID"

type Client struct {
	baseURL        string
	httpClient     *http.Client
	userAgent      string
	accessToken    string
	targetUserID   *uint
	allowSensitive bool
	ignoreRules    bool
	batchLimitOnce sync.Once
	batchBodyBytes int64
}

type HTTPError struct {
	StatusCode int
	Detail     string
	Message    string
}

func (e *HTTPError) Error() string {
	return e.Message
}

// HTTPStatusCode returns the response status associated with the error.
func (e *HTTPError) HTTPStatusCode() int {
	return e.StatusCode
}

type Option func(*Client)

func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.httpClient.Timeout = d }
}

func WithUserAgent(ua string) Option {
	return func(c *Client) { c.userAgent = ua }
}

func WithAccessToken(token string) Option {
	return func(c *Client) { c.accessToken = token }
}

func WithAllowSensitive() Option {
	return func(c *Client) { c.allowSensitive = true }
}

// WithIgnoreRules marks submitted documents as explicitly saved, bypassing URL
// indexing rules on submission and on subsequent index rebuilds.
func WithIgnoreRules() Option {
	return func(c *Client) { c.ignoreRules = true }
}

// WithMaxBatchBodyBytes overrides batch capability discovery. It is primarily
// useful for clients that already obtained the server limit out of band.
func WithMaxBatchBodyBytes(limit int64) Option {
	return func(c *Client) {
		if limit > 0 {
			c.batchBodyBytes = limit
		}
	}
}

// WithTargetUserID instructs the server to index submitted documents under the
// given user ID instead of the authenticated caller's ID. The server only
// honours this for admin users in multiuser mode.
func WithTargetUserID(uid uint) Option {
	return func(c *Client) { c.targetUserID = &uid }
}

func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	if c.accessToken != "" {
		c.httpClient = sameOriginHTTPClient(c.httpClient)
	}
	return c
}

// Preserve caller supplied clients and redirect policies while protecting credentials.
func sameOriginHTTPClient(httpClient *http.Client) *http.Client {
	hc := *httpClient
	checkRedirect := hc.CheckRedirect
	hc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if checkRedirect != nil {
			if err := checkRedirect(req, via); err != nil {
				return err
			}
		} else if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		if !sameOrigin(via[0].URL, req.URL) {
			return errors.New("refusing to send credentials to a different origin")
		}
		return nil
	}
	return &hc
}

func sameOrigin(a, b *url.URL) bool {
	port := func(u *url.URL) string {
		if p := u.Port(); p != "" {
			return p
		}
		switch strings.ToLower(u.Scheme) {
		case "http":
			return "80"
		case "https":
			return "443"
		}
		return ""
	}
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Hostname(), b.Hostname()) && port(a) == port(b)
}

// FetchConfig retrieves capabilities from the server the client is connected
// to. This avoids assuming that local configuration describes a remote server.
func (c *Client) FetchConfig() (*ServerConfig, error) {
	return c.FetchConfigContext(context.Background())
}

func (c *Client) FetchConfigContext(ctx context.Context) (*ServerConfig, error) {
	var serverConfig ServerConfig
	if err := c.requestJSON(ctx, http.MethodGet, "/api/config", nil, &serverConfig); err != nil {
		return nil, err
	}
	return &serverConfig, nil
}

// GetUser resolves an exact username on the connected server without retrieving credentials.
func (c *Client) GetUser(ctx context.Context, username string) (*UserInfo, error) {
	if strings.TrimSpace(username) == "" {
		return nil, errors.New("username must not be empty")
	}
	params := url.Values{"username": {username}, "limit": {"1"}}
	var result struct {
		Users []UserInfo `json:"users"`
	}
	if err := c.requestJSON(ctx, http.MethodGet, "/api/users?"+params.Encode(), nil, &result); err != nil {
		return nil, err
	}
	if result.Users == nil {
		return nil, errors.New("invalid user lookup response")
	}
	if len(result.Users) == 0 {
		return nil, fmt.Errorf("user %q not found", username)
	}
	if len(result.Users) != 1 || result.Users[0].ID == 0 || result.Users[0].Username != username {
		return nil, errors.New("invalid user lookup response")
	}
	return &result.Users[0], nil
}

// CreateUser creates an account on the connected server without retrieving credentials.
func (c *Client) CreateUser(ctx context.Context, username, password string, isAdmin bool) (*UserInfo, error) {
	payload := struct {
		Username string `json:"username"`
		Password string `json:"password"`
		IsAdmin  bool   `json:"is_admin"`
	}{Username: username, Password: password, IsAdmin: isAdmin}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var result UserInfo
	// Protect the password even when the caller did not supply an access token.
	err = c.requestWithClient(ctx, sameOriginHTTPClient(c.httpClient), http.MethodPost, "/api/users", bytes.NewReader(data), "application/json", func(resp *http.Response) error {
		return decodeResponse(resp, &result)
	})
	if err != nil {
		return nil, err
	}
	if result.ID == 0 || result.Username != username || result.IsAdmin != isAdmin {
		return nil, errors.New("invalid user creation response")
	}
	return &result, nil
}

// UpdateUser applies account changes together using the revision from a prior read.
// Conflicts are returned to the caller without retrying the update.
func (c *Client) UpdateUser(ctx context.Context, userID uint, changes UserUpdate) (*UserUpdateResult, error) {
	if userID == 0 {
		return nil, errors.New("user ID must be positive")
	}
	if changes.Revision == 0 || changes.Revision >= math.MaxInt64 {
		return nil, errors.New("a valid current user revision is required")
	}
	data, err := json.Marshal(changes)
	if err != nil {
		return nil, err
	}
	var result UserUpdateResult
	// Protect passwords even when the caller did not supply an access token.
	err = c.requestWithClient(ctx, sameOriginHTTPClient(c.httpClient), http.MethodPatch, fmt.Sprintf("/api/users/%d", userID), bytes.NewReader(data), "application/json", func(resp *http.Response) error {
		return decodeResponse(resp, &result)
	})
	if err != nil {
		return nil, err
	}
	if result.ID != userID || result.Username == "" || result.Revision != changes.Revision+1 ||
		(changes.Username != nil && result.Username != *changes.Username) ||
		(changes.IsAdmin != nil && result.IsAdmin != *changes.IsAdmin) ||
		(changes.RegenerateToken != (result.Token != "")) {
		return nil, errors.New("invalid user update response")
	}
	return &result, nil
}

// GetUserToken explicitly retrieves an account's existing token without rotating it.
func (c *Client) GetUserToken(ctx context.Context, userID uint) (string, error) {
	if userID == 0 {
		return "", errors.New("user ID must be positive")
	}
	var result struct {
		Token string `json:"token"`
	}
	if err := c.requestJSON(ctx, http.MethodGet, fmt.Sprintf("/api/users/%d/token", userID), nil, &result); err != nil {
		return "", err
	}
	if result.Token == "" {
		return "", errors.New("invalid user token response")
	}
	return result.Token, nil
}

// DeleteUser deletes an account by ID and optionally its documents on the connected server.
func (c *Client) DeleteUser(ctx context.Context, userID uint, purge bool) (*DeleteUserResult, error) {
	if userID == 0 {
		return nil, errors.New("user ID must be positive")
	}
	path := fmt.Sprintf("/api/users/%d", userID)
	if purge {
		path += "?purge=true"
	}
	// Require an explicit nonnegative count, including for empty accounts.
	result := DeleteUserResult{DeletedDocuments: -1}
	if err := c.requestJSON(ctx, http.MethodDelete, path, nil, &result); err != nil {
		return nil, err
	}
	if result.ID != userID || result.Username == "" || result.DeletedDocuments < 0 || (!purge && result.DeletedDocuments != 0) {
		return nil, errors.New("invalid user deletion response")
	}
	return &result, nil
}

const legacyMaxBatchBodyBytes int64 = 5 << 20

// MaxBatchBodyBytes returns the server advertised batch request limit. Servers
// that predate capability discovery use the former 5 MiB limit.
func (c *Client) MaxBatchBodyBytes() int64 {
	c.batchLimitOnce.Do(func() {
		if c.batchBodyBytes > 0 {
			return
		}
		c.batchBodyBytes = legacyMaxBatchBodyBytes
		_ = c.request(context.Background(), http.MethodGet, "/api/config", nil, "", func(resp *http.Response) error {
			if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
				return nil
			}
			var capabilities struct {
				MaxBatchBodyBytes int64 `json:"maxBatchBodyBytes"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&capabilities); err == nil && capabilities.MaxBatchBodyBytes > 0 {
				c.batchBodyBytes = capabilities.MaxBatchBodyBytes
			}
			return nil
		})
	})
	return c.batchBodyBytes
}
