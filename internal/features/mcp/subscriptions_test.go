package mcp

import (
	"testing"
	"time"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/mcp/protocol/v20260728"
)

func TestSubscriptionBroker_PublishDeliversToOptedInSubscriber(t *testing.T) {
	b := NewSubscriptionBroker()
	id, ch := b.Subscribe([]string{v20260728.NotifyToolsListChanged})
	defer b.Unsubscribe(id)

	b.Publish(v20260728.NotifyToolsListChanged)

	select {
	case n := <-ch:
		if n.Meta.SubscriptionID != id {
			t.Errorf("notification subscriptionId = %q, want %q", n.Meta.SubscriptionID, id)
		}
		if n.Method != "notifications/"+v20260728.NotifyToolsListChanged {
			t.Errorf("notification method = %q, want %q", n.Method, "notifications/"+v20260728.NotifyToolsListChanged)
		}
	case <-time.After(time.Second):
		t.Fatal("expected a notification, got none")
	}
}

func TestSubscriptionBroker_SkipsNotOptedInType(t *testing.T) {
	b := NewSubscriptionBroker()
	id, ch := b.Subscribe([]string{v20260728.NotifyPromptsListChanged})
	defer b.Unsubscribe(id)

	b.Publish(v20260728.NotifyToolsListChanged)

	select {
	case n := <-ch:
		t.Fatalf("expected no notification (not opted in), got %+v", n)
	case <-time.After(100 * time.Millisecond):
		// expected: nothing delivered
	}
}

func TestSubscriptionBroker_MultipleSubscribers(t *testing.T) {
	b := NewSubscriptionBroker()
	id1, ch1 := b.Subscribe([]string{v20260728.NotifyToolsListChanged})
	id2, ch2 := b.Subscribe([]string{v20260728.NotifyToolsListChanged})
	defer b.Unsubscribe(id1)
	defer b.Unsubscribe(id2)

	if b.Count() != 2 {
		t.Fatalf("Count() = %d, want 2", b.Count())
	}

	b.Publish(v20260728.NotifyToolsListChanged)

	for _, ch := range []<-chan v20260728.Notification{ch1, ch2} {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatal("expected both subscribers to receive the notification")
		}
	}
}

func TestSubscriptionBroker_UnsubscribeStopsDelivery(t *testing.T) {
	b := NewSubscriptionBroker()
	id, ch := b.Subscribe([]string{v20260728.NotifyToolsListChanged})
	b.Unsubscribe(id)

	if b.Count() != 0 {
		t.Fatalf("Count() = %d, want 0 after Unsubscribe", b.Count())
	}

	b.Publish(v20260728.NotifyToolsListChanged) // must not panic / block

	if _, ok := <-ch; ok {
		t.Error("expected channel to be closed after Unsubscribe")
	}
}

func TestSubscriptionBroker_UnsubscribeIsIdempotent(t *testing.T) {
	b := NewSubscriptionBroker()
	id, _ := b.Subscribe([]string{v20260728.NotifyToolsListChanged})
	b.Unsubscribe(id)
	b.Unsubscribe(id) // must not panic (double-close)
	b.Unsubscribe("never-existed")
}

func TestRegistry_OnToolsChanged_FiresOnSyncTools(t *testing.T) {
	r := &Registry{servers: map[string]*ServerInfo{"s1": {ID: "s1"}}}

	fired := make(chan struct{}, 1)
	r.OnToolsChanged(func() {
		select {
		case fired <- struct{}{}:
		default:
		}
	})

	if err := r.SyncTools("s1", []ToolDefinition{{Name: "t1"}}); err != nil {
		t.Fatalf("SyncTools error: %v", err)
	}

	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("expected OnToolsChanged callback to fire after SyncTools")
	}
}

func TestRegistry_OnToolsChanged_FiresOnRegisterAndUnregister(t *testing.T) {
	r := &Registry{servers: map[string]*ServerInfo{}}

	var fireCount int
	done := make(chan struct{}, 2)
	r.OnToolsChanged(func() {
		fireCount++
		done <- struct{}{}
	})

	r.RegisterServer("s2", config.MCPServerConfig{ID: "s2"}, nil)
	r.UnregisterServer("s2")

	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("expected 2 callback fires, got %d", fireCount)
		}
	}
}
