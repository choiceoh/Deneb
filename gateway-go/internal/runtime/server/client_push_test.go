package server

import "testing"

func TestClientPushHub_PublishToSubscribers(t *testing.T) {
	h := newClientPushHub()
	ch1, unsub1 := h.subscribe()
	ch2, unsub2 := h.subscribe()
	defer unsub1()
	defer unsub2()

	if got := h.subscriberCount(); got != 2 {
		t.Fatalf("subscriberCount = %d, want 2", got)
	}

	h.publish(clientPushEvent{Title: "Deneb", Body: "morning letter"})

	for i, ch := range []<-chan clientPushEvent{ch1, ch2} {
		select {
		case ev := <-ch:
			if ev.Body != "morning letter" {
				t.Fatalf("sub %d got body %q", i, ev.Body)
			}
		default:
			t.Fatalf("sub %d received no event", i)
		}
	}
}

func TestClientPushHub_UnsubscribeStopsDelivery(t *testing.T) {
	h := newClientPushHub()
	ch, unsub := h.subscribe()
	unsub()
	if got := h.subscriberCount(); got != 0 {
		t.Fatalf("subscriberCount after unsub = %d, want 0", got)
	}
	// Channel is closed; a publish must not panic and the closed channel drains.
	h.publish(clientPushEvent{Title: "x", Body: "y"})
	if _, ok := <-ch; ok {
		t.Fatalf("expected closed channel after unsubscribe")
	}
}

func TestClientPushHub_SlowConsumerDropsNotBlocks(t *testing.T) {
	h := newClientPushHub()
	_, unsub := h.subscribe() // never drained
	defer unsub()
	// Far more than the buffer (8). Must not block.
	for i := 0; i < 100; i++ {
		h.publish(clientPushEvent{Title: "Deneb", Body: "spam"})
	}
}

func TestPushPreview(t *testing.T) {
	if got := pushPreview("  첫 줄\n둘째 줄\n셋째"); got != "첫 줄" {
		t.Fatalf("pushPreview first-line = %q", got)
	}
	long := ""
	for i := 0; i < 200; i++ {
		long += "가"
	}
	got := pushPreview(long)
	if []rune(got)[len([]rune(got))-1] != '…' {
		t.Fatalf("pushPreview should ellipsize long input")
	}
	if n := len([]rune(got)); n != 141 { // 140 + ellipsis
		t.Fatalf("pushPreview rune len = %d, want 141", n)
	}
}
