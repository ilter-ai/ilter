package mcp

import (
	"crypto/rand"
	"encoding/hex"
	"sync"

	"github.com/ilter-ai/ilter/internal/features/mcp/protocol/v20260728"
)

// SubscriptionBroker fans out change notifications (toolsListChanged, etc.)
// to every active 2026-07-28 `subscriptions/listen` stream that opted into
// that notification type. This is the real mechanism behind that method —
// replacing the legacy SSE-GET change-notification model for sessions
// negotiated at this version — wired to the Registry's own tool-change
// events (SyncTools/RegisterServer/UnregisterServer) via
// Registry.OnToolsChanged, so a real event (a downstream MCP server's
// tools actually changing) is what drives a real notification, not a
// synthetic/demo trigger.
type SubscriptionBroker struct {
	mu   sync.Mutex
	subs map[string]*subscription
}

type subscription struct {
	id    string
	types map[string]bool
	ch    chan v20260728.Notification
}

// NewSubscriptionBroker creates an empty broker.
func NewSubscriptionBroker() *SubscriptionBroker {
	return &SubscriptionBroker{subs: make(map[string]*subscription)}
}

// Subscribe registers a new listener for the given notification types and
// returns its subscription id and the channel notifications will arrive
// on. The channel is buffered so a slow/absent reader doesn't block
// Publish; call Unsubscribe when the listener disconnects to release it.
func (b *SubscriptionBroker) Subscribe(types []string) (id string, ch <-chan v20260728.Notification) {
	typeSet := make(map[string]bool, len(types))
	for _, t := range types {
		typeSet[t] = true
	}
	sub := &subscription{
		id:    generateSubscriptionID(),
		types: typeSet,
		ch:    make(chan v20260728.Notification, 32),
	}

	b.mu.Lock()
	b.subs[sub.id] = sub
	b.mu.Unlock()

	return sub.id, sub.ch
}

// Unsubscribe removes a listener and closes its channel. Safe to call more
// than once or with an unknown id.
func (b *SubscriptionBroker) Unsubscribe(id string) {
	b.mu.Lock()
	sub, ok := b.subs[id]
	if ok {
		delete(b.subs, id)
	}
	b.mu.Unlock()
	if ok {
		close(sub.ch)
	}
}

// Publish delivers notifyType to every currently-subscribed listener that
// opted into it. Non-blocking per listener: a full channel (a listener
// that isn't draining its stream) drops the notification for that
// listener rather than stalling every other subscriber or the caller
// (typically Registry, mid-sync).
func (b *SubscriptionBroker) Publish(notifyType string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, sub := range b.subs {
		if !sub.types[notifyType] {
			continue
		}
		n := v20260728.BuildNotification(notifyType, sub.id)
		select {
		case sub.ch <- n:
		default:
		}
	}
}

// Count reports the number of currently active subscriptions — exposed
// for tests and metrics, not part of the wire protocol.
func (b *SubscriptionBroker) Count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}

func generateSubscriptionID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte("fallback-subscription-id"))
	}
	return hex.EncodeToString(buf)
}
