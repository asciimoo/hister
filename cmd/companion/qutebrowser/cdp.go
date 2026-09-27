package qutebrowser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("devtools error %d: %s", e.Code, e.Message)
}

type rpcMessage struct {
	ID        int64           `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *rpcError       `json:"error,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
}

type rpcRequest struct {
	ID        int64  `json:"id"`
	Method    string `json:"method"`
	Params    any    `json:"params,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
}

type cdpClient struct {
	conn *websocket.Conn

	nextID  atomic.Int64
	writeMu sync.Mutex

	pendingMu sync.Mutex
	pending   map[int64]chan rpcMessage

	events *cdpEventQueue
	done   chan struct{}

	closeOnce sync.Once
	errMu     sync.Mutex
	err       error
}

func dialCDP(
	ctx context.Context,
	devToolsURL *url.URL,
	timeout time.Duration,
) (*cdpClient, error) {
	webSocketURL, err := browserWebSocketURL(ctx, devToolsURL, timeout)
	if err != nil {
		return nil, err
	}

	dialer := websocket.Dialer{HandshakeTimeout: timeout}
	conn, response, err := dialer.DialContext(ctx, webSocketURL, nil)
	if err != nil {
		if response != nil {
			return nil, fmt.Errorf(
				"connect to DevTools WebSocket: HTTP %s: %w",
				response.Status,
				err,
			)
		}
		return nil, fmt.Errorf("connect to DevTools WebSocket: %w", err)
	}

	client := &cdpClient{
		conn:    conn,
		pending: make(map[int64]chan rpcMessage),
		events:  newCDPEventQueue(),
		done:    make(chan struct{}),
	}
	go client.readLoop()
	return client, nil
}

func browserWebSocketURL(
	ctx context.Context,
	devToolsURL *url.URL,
	timeout time.Duration,
) (_ string, err error) {
	versionURL := *devToolsURL
	versionURL.Path = "/json/version"
	versionURL.RawPath = ""
	versionURL.RawQuery = ""
	versionURL.Fragment = ""

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, versionURL.String(), nil)
	if err != nil {
		return "", fmt.Errorf("create DevTools discovery request: %w", err)
	}
	client := newHTTPClient(timeout)
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("read DevTools version: %w", err)
	}
	defer closeResponseBody(response, &err)
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("read DevTools version: HTTP %s", response.Status)
	}

	var version struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1024*1024))
	if err := decoder.Decode(&version); err != nil {
		return "", fmt.Errorf("decode DevTools version: %w", err)
	}
	if version.WebSocketDebuggerURL == "" {
		return "", errors.New("devtools version response has no browser WebSocket URL")
	}

	return normalizeWebSocketURL(version.WebSocketDebuggerURL, devToolsURL)
}

func normalizeWebSocketURL(rawURL string, devToolsURL *url.URL) (string, error) {
	wsURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid DevTools WebSocket URL: %w", err)
	}
	if wsURL.Scheme != "ws" && wsURL.Scheme != "wss" {
		return "", errors.New("devtools WebSocket URL must use WS or WSS")
	}

	// The discovery response is supplied by a privileged local endpoint. Keep
	// the connection on the exact host selected by the user even if Chromium
	// advertises a wildcard address or a different hostname.
	wsURL.Host = devToolsURL.Host
	if devToolsURL.Scheme == "https" {
		wsURL.Scheme = "wss"
	} else {
		wsURL.Scheme = "ws"
	}
	return wsURL.String(), nil
}

func (c *cdpClient) call(
	ctx context.Context,
	method string,
	params any,
	sessionID string,
	result any,
) error {
	id := c.nextID.Add(1)
	responseChannel := make(chan rpcMessage, 1)
	c.pendingMu.Lock()
	c.pending[id] = responseChannel
	c.pendingMu.Unlock()
	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()

	request, err := json.Marshal(rpcRequest{
		ID:        id,
		Method:    method,
		Params:    params,
		SessionID: sessionID,
	})
	if err != nil {
		return fmt.Errorf("encode %s request: %w", method, err)
	}

	c.writeMu.Lock()
	err = c.conn.WriteMessage(websocket.TextMessage, request)
	c.writeMu.Unlock()
	if err != nil {
		c.fail(fmt.Errorf("write %s request: %w", method, err))
		return c.connectionError()
	}

	select {
	case response := <-responseChannel:
		if response.Error != nil {
			return response.Error
		}
		if result != nil && len(response.Result) != 0 {
			if err := json.Unmarshal(response.Result, result); err != nil {
				return fmt.Errorf("decode %s response: %w", method, err)
			}
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return c.connectionError()
	}
}

func (c *cdpClient) readLoop() {
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			c.fail(fmt.Errorf("read DevTools message: %w", err))
			return
		}
		var message rpcMessage
		if err := json.Unmarshal(data, &message); err != nil {
			c.fail(fmt.Errorf("decode DevTools message: %w", err))
			return
		}
		if message.ID != 0 {
			c.pendingMu.Lock()
			responseChannel := c.pending[message.ID]
			c.pendingMu.Unlock()
			if responseChannel != nil {
				responseChannel <- message
			}
			continue
		}
		if isMonitoredEvent(message.Method) {
			c.events.push(message)
		}
	}
}

func (c *cdpClient) fail(err error) {
	c.closeOnce.Do(func() {
		c.errMu.Lock()
		c.err = err
		c.errMu.Unlock()
		close(c.done)
		_ = c.conn.Close()
	})
}

func (c *cdpClient) close() {
	c.fail(errors.New("devtools connection closed"))
}

func (c *cdpClient) connectionError() error {
	c.errMu.Lock()
	defer c.errMu.Unlock()
	if c.err == nil {
		return errors.New("devtools connection closed")
	}
	return c.err
}
