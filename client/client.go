package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// targetUserIDHeader is sent by admin CLI callers to request a specific user_id
// for indexed documents. The server only honours it for admin users in
// multiuser mode.
const targetUserIDHeader = "X-Hister-Target-User-ID"

type Client struct {
	baseURL        string
	httpClient     *http.Client
	userAgent      string
	accessToken    string
	targetUserID   *uint
	allowSensitive bool
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
	return c
}

// FetchConfig retrieves capabilities from the server the client is connected
// to. This avoids assuming that local configuration describes a remote server.
func (c *Client) FetchConfig() (_ *ServerConfig, err error) {
	return c.FetchConfigContext(context.Background())
}

func (c *Client) FetchConfigContext(ctx context.Context) (_ *ServerConfig, err error) {
	req, err := c.newRequest(http.MethodGet, "/api/config", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	defer closeBody(resp, &err)
	if err = checkStatus(resp); err != nil {
		return nil, err
	}
	var serverConfig ServerConfig
	if err = json.NewDecoder(resp.Body).Decode(&serverConfig); err != nil {
		return nil, err
	}
	return &serverConfig, nil
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
		req, err := c.newRequest(http.MethodGet, "/api/config", nil)
		if err != nil {
			return
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return
		}
		var capabilities struct {
			MaxBatchBodyBytes int64 `json:"maxBatchBodyBytes"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&capabilities); err == nil && capabilities.MaxBatchBodyBytes > 0 {
			c.batchBodyBytes = capabilities.MaxBatchBodyBytes
		}
	})
	return c.batchBodyBytes
}

func checkStatus(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	detail := strings.TrimSpace(string(body))
	errWithStatus := func(msg string) error {
		return &HTTPError{
			StatusCode: resp.StatusCode,
			Detail:     detail,
			Message:    msg,
		}
	}

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		msg := "authentication required: the server requires an access token"
		if detail != "" {
			msg += " (" + detail + ")"
		}
		return errWithStatus(fmt.Sprintf("%s\nProvide one with --token / -t or set access_token in your config file", msg))
	case http.StatusForbidden:
		msg := "access denied: the token is invalid or does not have permission for this operation"
		if detail != "" {
			msg += " (" + detail + ")"
		}
		return errWithStatus(fmt.Sprintf("%s\nCheck the token with --token / -t or verify the user's permissions on the server", msg))
	case http.StatusNotFound:
		msg := "server not reachable at the configured URL"
		if detail != "" {
			msg += ": " + detail
		}
		return errWithStatus(fmt.Sprintf("%s\nVerify the server address with --server-url / -u", msg))
	case http.StatusNotAcceptable:
		msg := "page skipped: this URL was rejected by the server (usually due to skip rules or disabled domains)"
		if detail != "" {
			msg += " (" + detail + ")"
		}
		return errWithStatus(msg)
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		msg := fmt.Sprintf("server error (%d)", resp.StatusCode)
		if detail != "" {
			msg += ": " + detail
		}
		return errWithStatus(fmt.Sprintf("%s\nCheck the server logs for details", msg))
	default:
		if detail == "" {
			detail = resp.Status
		}
		return errWithStatus(fmt.Sprintf("unexpected response (%d): %s", resp.StatusCode, detail))
	}
}

func closeBody(resp *http.Response, errp *error) {
	if cerr := resp.Body.Close(); cerr != nil && *errp == nil {
		*errp = fmt.Errorf("closing response body: %w", cerr)
	}
}

// builds an http.Request with Origin: hister:// set for CSRF bypass.
func (c *Client) newRequest(method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Origin", "hister://")
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	if c.accessToken != "" {
		req.Header.Set("X-Access-Token", c.accessToken)
	}
	if c.targetUserID != nil {
		req.Header.Set(targetUserIDHeader, strconv.FormatUint(uint64(*c.targetUserID), 10))
	}
	return req, nil
}
