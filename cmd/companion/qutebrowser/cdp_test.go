package qutebrowser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestMonitorInitializesManyTabs(t *testing.T) {
	const tabCount = 300
	targets := make([]targetInfo, tabCount)
	for i := range targets {
		targets[i] = targetInfo{
			TargetID: fmt.Sprintf("page-%d", i),
			Type:     "page",
			URL:      fmt.Sprintf("https://example.com/%d", i),
		}
	}
	client := newTestCDPClient(t, func(conn *websocket.Conn, request rpcMessage) error {
		var result any = struct{}{}
		switch request.Method {
		case "Target.setDiscoverTargets":
			for _, target := range targets {
				params, err := json.Marshal(map[string]any{"targetInfo": target})
				if err != nil {
					return err
				}
				if err := conn.WriteJSON(rpcMessage{
					Method: "Target.targetCreated",
					Params: params,
				}); err != nil {
					return err
				}
			}
		case "Target.getTargets":
			result = map[string]any{"targetInfos": targets}
		case "Target.attachToTarget":
			var params struct {
				TargetID string `json:"targetId"`
			}
			if err := json.Unmarshal(request.Params, &params); err != nil {
				return err
			}
			result = map[string]any{"sessionId": params.TargetID}
		case "Page.enable":
			if err := conn.WriteJSON(rpcMessage{
				Method:    "Page.loadEventFired",
				SessionID: request.SessionID,
				Params:    json.RawMessage(`{"timestamp":1}`),
			}); err != nil {
				return err
			}
		case "Page.getFrameTree":
			result = map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": request.SessionID}}}
		case "Page.createIsolatedWorld":
			result = map[string]any{"executionContextId": 1}
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return conn.WriteJSON(rpcMessage{ID: request.ID, Result: encoded})
	})

	input := DefaultOptions()
	input.InitialDelay = time.Hour
	opts, err := normalizeOptions(input)
	if err != nil {
		t.Fatal(err)
	}
	m := &monitor{
		companion:   newCompanion(opts, &recordingSubmitter{}),
		client:      client,
		bindingName: "testBinding",
		pages:       make(map[string]*pageState),
		targets:     make(map[string]string),
		extraction:  make(chan extractionDue, tabCount),
	}
	t.Cleanup(m.stop)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := m.initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if len(m.pages) != tabCount {
		t.Fatalf("watching %d pages, want %d", len(m.pages), tabCount)
	}
	select {
	case <-client.done:
		t.Fatalf("connection ended during initialization: %v", client.connectionError())
	default:
	}

	// Discovery and page setup events must remain available after initialization.
	for _, method := range []string{"Target.targetCreated", "Page.loadEventFired"} {
		for _, target := range targets {
			event := nextTestCDPEvent(t, ctx, client)
			if event.Method != method {
				t.Fatalf("event method = %q, want %q", event.Method, method)
			}
			if method == "Page.loadEventFired" && event.SessionID != target.TargetID {
				t.Fatalf("event session = %q, want %q", event.SessionID, target.TargetID)
			}
			m.handleEvent(ctx, event)
		}
	}
}

func TestCDPEventBurstPreservesRepliesAndOrder(t *testing.T) {
	const eventCount = 4096
	methods := []string{
		"Target.targetCreated", "Target.targetInfoChanged",
		"Target.targetDestroyed", "Target.targetCrashed", "Target.detachedFromTarget",
		"Page.frameNavigated", "Page.loadEventFired", "Page.navigatedWithinDocument",
		"Page.lifecycleEvent", "Runtime.bindingCalled",
	}
	eventAt := func(i int) rpcMessage {
		return rpcMessage{
			Method:    methods[i%len(methods)],
			SessionID: fmt.Sprintf("session-%d", i),
			Params:    json.RawMessage(fmt.Sprintf(`{"sequence":%d}`, i)),
		}
	}
	client := newTestCDPClient(t, func(conn *websocket.Conn, request rpcMessage) error {
		for i := range eventCount {
			if err := conn.WriteJSON(rpcMessage{
				Method: "Runtime.consoleAPICalled",
				Params: json.RawMessage(`{"type":"log"}`),
			}); err != nil {
				return err
			}
			if err := conn.WriteJSON(eventAt(i)); err != nil {
				return err
			}
		}
		return conn.WriteJSON(rpcMessage{ID: request.ID, Result: json.RawMessage(`{"product":"test"}`)})
	})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	// Cover both paused and active consumers, reusing the queue after it empties.
	for _, consumeDuringCall := range []bool{false, true} {
		var result struct {
			Product string `json:"product"`
		}
		replied := make(chan error, 1)
		go func() {
			replied <- client.call(ctx, "Browser.getVersion", nil, "", &result)
		}()
		waitForReply := func() {
			t.Helper()
			select {
			case err := <-replied:
				if err != nil {
					t.Fatalf("command failed while events were queued: %v", err)
				}
			case <-ctx.Done():
				t.Fatalf("waiting for command reply: %v", ctx.Err())
			}
			if result.Product != "test" {
				t.Fatalf("product = %q, want test", result.Product)
			}
		}
		if !consumeDuringCall {
			waitForReply()
		}
		for i := range eventCount {
			got := nextTestCDPEvent(t, ctx, client)
			want := eventAt(i)
			if got.Method != want.Method || got.SessionID != want.SessionID || string(got.Params) != string(want.Params) {
				t.Fatalf("event %d = %+v, want %+v", i, got, want)
			}
		}
		if consumeDuringCall {
			waitForReply()
		}
		select {
		case <-client.events.ready:
			t.Fatalf("unexpected queued event: %+v", client.events.pop())
		default:
		}
	}
}

func nextTestCDPEvent(t *testing.T, ctx context.Context, client *cdpClient) rpcMessage {
	t.Helper()
	select {
	case <-client.events.ready:
		return client.events.pop()
	case <-client.done:
		t.Fatalf("connection ended before event delivery: %v", client.connectionError())
	case <-ctx.Done():
		t.Fatalf("waiting for DevTools event: %v", ctx.Err())
	}
	return rpcMessage{}
}

func newTestCDPClient(t *testing.T, handle func(*websocket.Conn, rpcMessage) error) *cdpClient {
	t.Helper()
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/version" {
			if err := json.NewEncoder(w).Encode(map[string]string{
				"webSocketDebuggerUrl": "ws://" + r.Host + "/devtools/browser/test",
			}); err != nil {
				t.Errorf("write DevTools discovery response: %v", err)
			}
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade DevTools connection: %v", err)
			return
		}
		defer func() {
			_ = conn.Close()
		}()
		for {
			var request rpcMessage
			if err := conn.ReadJSON(&request); err != nil {
				return
			}
			if err := handle(conn, request); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)
	client, err := dialCDP(t.Context(), mustURL(t, server.URL), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.close)
	return client
}
