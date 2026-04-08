package server

import (
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
)

func TestMediaGroupBatcher_SingleGroup(t *testing.T) {
	var mu sync.Mutex
	var dispatched []*telegram.Message

	batcher := NewMediaGroupBatcher(func(msgs []*telegram.Message) {
		mu.Lock()
		dispatched = msgs
		mu.Unlock()
	})

	msg1 := &telegram.Message{MessageID: 1, MediaGroupID: "g1", Photo: []telegram.PhotoSize{{FileID: "p1"}}}
	msg2 := &telegram.Message{MessageID: 2, MediaGroupID: "g1", Photo: []telegram.PhotoSize{{FileID: "p2"}}}
	msg3 := &telegram.Message{MessageID: 3, MediaGroupID: "g1", Photo: []telegram.PhotoSize{{FileID: "p3"}}}

	if !batcher.Add(msg1) {
		t.Fatal("expected Add to return true")
	}
	if !batcher.Add(msg2) {
		t.Fatal("expected Add to return true")
	}
	if !batcher.Add(msg3) {
		t.Fatal("expected Add to return true")
	}

	// Wait for dispatch (mediaGroupDelay + buffer).
	time.Sleep(mediaGroupDelay + 200*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(dispatched) != 3 {
		t.Fatalf("got %d messages, want 3", len(dispatched))
	}
}


func TestMediaGroupBatcher_MultipleGroups(t *testing.T) {
	var mu sync.Mutex
	groups := make(map[string]int)

	batcher := NewMediaGroupBatcher(func(msgs []*telegram.Message) {
		mu.Lock()
		if len(msgs) > 0 {
			groups[msgs[0].MediaGroupID] = len(msgs)
		}
		mu.Unlock()
	})

	batcher.Add(&telegram.Message{MessageID: 1, MediaGroupID: "a", Photo: []telegram.PhotoSize{{FileID: "p1"}}})
	batcher.Add(&telegram.Message{MessageID: 2, MediaGroupID: "a", Photo: []telegram.PhotoSize{{FileID: "p2"}}})
	batcher.Add(&telegram.Message{MessageID: 3, MediaGroupID: "b", Photo: []telegram.PhotoSize{{FileID: "p3"}}})

	time.Sleep(mediaGroupDelay + 200*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if groups["a"] != 2 {
		t.Errorf("group a: got %d messages, want 2", groups["a"])
	}
	if groups["b"] != 1 {
		t.Errorf("group b: got %d messages, want 1", groups["b"])
	}
}
