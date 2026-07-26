package nativepush

import "testing"

func TestClientPushHub_PublishEmitsToSubscribers(t *testing.T) {
	h := NewHub()
	ch1, unsub1 := h.Subscribe(KindMobile)
	ch2, unsub2 := h.Subscribe(KindMobile)
	defer unsub1()
	defer unsub2()

	if got := h.SubscriberCount(); got != 2 {
		t.Fatalf("SubscriberCount = %d, want 2", got)
	}

	h.Publish(Event{Title: "Deneb", Body: "morning letter"})

	for i, ch := range []<-chan Event{ch1, ch2} {
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
	h := NewHub()
	ch, unsub := h.Subscribe(KindMobile)
	unsub()
	if got := h.SubscriberCount(); got != 0 {
		t.Fatalf("SubscriberCount after unsub = %d, want 0", got)
	}
	// Channel is closed; a publish must not panic and the closed channel drains.
	h.Publish(Event{Title: "x", Body: "y"})
	if _, ok := <-ch; ok {
		t.Fatalf("expected closed channel after unsubscribe")
	}
}

func TestClientPushHub_SlowConsumerDropsWithoutBlocking(t *testing.T) {
	h := NewHub()
	_, unsub := h.Subscribe(KindMobile) // never drained
	defer unsub()
	// Far more than the buffer (8). Must not block.
	for i := 0; i < 100; i++ {
		h.Publish(Event{Title: "Deneb", Body: "spam"})
	}
}

func TestClientPushHub_MobileCountWithoutDesktopSubscribers(t *testing.T) {
	h := NewHub()
	// A connected desktop must NOT count as a mobile subscriber, so it cannot
	// suppress the phone's FCM fallback (the Codex-flagged multi-subscriber bug).
	_, unsubDesk := h.Subscribe(KindDesktop)
	defer unsubDesk()
	_, unsubUnknown := h.Subscribe(KindUnknown)
	defer unsubUnknown()
	if got := h.MobileSubscriberCount(); got != 0 {
		t.Fatalf("MobileSubscriberCount with only desktop/unknown = %d, want 0", got)
	}
	if got := h.SubscriberCount(); got != 2 {
		t.Fatalf("SubscriberCount = %d, want 2", got)
	}

	_, unsubMobile := h.Subscribe(KindMobile)
	defer unsubMobile()
	if got := h.MobileSubscriberCount(); got != 1 {
		t.Fatalf("MobileSubscriberCount with one mobile = %d, want 1", got)
	}
}

func TestClientKindFromHeaderParsesKnownValues(t *testing.T) {
	cases := map[string]ClientKind{
		"mobile":  KindMobile,
		"desktop": KindDesktop,
		"":        KindUnknown,
		"phone":   KindUnknown,
	}
	for header, want := range cases {
		if got := ClientKindFromHeader(header); got != want {
			t.Fatalf("ClientKindFromHeader(%q) = %v, want %v", header, got, want)
		}
	}
}

func TestClientPushHub_DesktopSubscriberCount(t *testing.T) {
	h := NewHub()
	if got := h.DesktopSubscriberCount(); got != 0 {
		t.Fatalf("DesktopSubscriberCount empty = %d, want 0", got)
	}
	// Mobile and unknown subscribers must not count as desktops — the
	// workstation-control gate keys on an actual Andromeda connection.
	_, unsubMobile := h.Subscribe(KindMobile)
	defer unsubMobile()
	_, unsubUnknown := h.Subscribe(KindUnknown)
	defer unsubUnknown()
	if got := h.DesktopSubscriberCount(); got != 0 {
		t.Fatalf("DesktopSubscriberCount with mobile+unknown = %d, want 0", got)
	}
	_, unsubDesk := h.Subscribe(KindDesktop)
	if got := h.DesktopSubscriberCount(); got != 1 {
		t.Fatalf("DesktopSubscriberCount with one desktop = %d, want 1", got)
	}
	unsubDesk()
	if got := h.DesktopSubscriberCount(); got != 0 {
		t.Fatalf("DesktopSubscriberCount after unsubscribe = %d, want 0", got)
	}
}
