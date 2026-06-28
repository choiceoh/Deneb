package server

import "testing"

func TestClientPushHub_PublishToSubscribers(t *testing.T) {
	h := newClientPushHub()
	ch1, unsub1 := h.subscribe(kindMobile)
	ch2, unsub2 := h.subscribe(kindMobile)
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
	ch, unsub := h.subscribe(kindMobile)
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
	_, unsub := h.subscribe(kindMobile) // never drained
	defer unsub()
	// Far more than the buffer (8). Must not block.
	for i := 0; i < 100; i++ {
		h.publish(clientPushEvent{Title: "Deneb", Body: "spam"})
	}
}

func TestClientPushHub_MobileSubscriberCount(t *testing.T) {
	h := newClientPushHub()
	// A connected desktop must NOT count as a mobile subscriber, so it cannot
	// suppress the phone's FCM fallback (the Codex-flagged multi-subscriber bug).
	_, unsubDesk := h.subscribe(kindDesktop)
	defer unsubDesk()
	_, unsubUnknown := h.subscribe(kindUnknown)
	defer unsubUnknown()
	if got := h.mobileSubscriberCount(); got != 0 {
		t.Fatalf("mobileSubscriberCount with only desktop/unknown = %d, want 0", got)
	}
	if got := h.subscriberCount(); got != 2 {
		t.Fatalf("subscriberCount = %d, want 2", got)
	}

	_, unsubMobile := h.subscribe(kindMobile)
	defer unsubMobile()
	if got := h.mobileSubscriberCount(); got != 1 {
		t.Fatalf("mobileSubscriberCount with one mobile = %d, want 1", got)
	}
}

func TestClientKindFromHeader(t *testing.T) {
	cases := map[string]clientKind{
		"mobile":  kindMobile,
		"desktop": kindDesktop,
		"":        kindUnknown,
		"phone":   kindUnknown,
	}
	for header, want := range cases {
		if got := clientKindFromHeader(header); got != want {
			t.Fatalf("clientKindFromHeader(%q) = %v, want %v", header, got, want)
		}
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
