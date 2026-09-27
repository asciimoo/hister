package qutebrowser

import "sync"

// cdpEventQueue lets the socket reader keep delivering command replies while
// the monitor is busy attaching pages. Event handling can itself wait for a
// command reply, so queueing an event must never wait for the monitor.
type cdpEventQueue struct {
	mu      sync.Mutex
	pending []rpcMessage
	ready   chan struct{}
}

func newCDPEventQueue() *cdpEventQueue {
	return &cdpEventQueue{ready: make(chan struct{}, 1)}
}

func (q *cdpEventQueue) push(event rpcMessage) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.pending = append(q.pending, event)
	if len(q.pending) == 1 {
		q.ready <- struct{}{}
	}
}

// pop is called by the monitor after receiving a notification from ready.
func (q *cdpEventQueue) pop() rpcMessage {
	q.mu.Lock()
	defer q.mu.Unlock()

	event := q.pending[0]
	q.pending[0] = rpcMessage{}
	q.pending = q.pending[1:]
	if len(q.pending) == 0 {
		q.pending = nil
	} else {
		q.ready <- struct{}{}
	}
	return event
}

func isMonitoredEvent(method string) bool {
	switch method {
	case "Target.targetCreated", "Target.targetInfoChanged",
		"Target.targetDestroyed", "Target.targetCrashed", "Target.detachedFromTarget",
		"Page.frameNavigated", "Page.loadEventFired", "Page.navigatedWithinDocument",
		"Page.lifecycleEvent", "Runtime.bindingCalled":
		return true
	default:
		return false
	}
}
