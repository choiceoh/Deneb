// Package server — media group batching for Telegram multi-photo messages.
//
// When a user sends multiple photos at once on Telegram, each photo arrives
// as a separate Update with the same media_group_id. This batcher collects
// all updates in the same group and dispatches them together so the agent
// sees all images in a single run.
package server

import (
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
)

// mediaGroupDelay is the time to wait for additional media group members
// before dispatching. Telegram typically delivers all members within ~200ms.
const mediaGroupDelay = 750 * time.Millisecond

// mediaGroupEntry holds buffered messages for a single media group.
type mediaGroupEntry struct {
	messages []*telegram.Message
	timer    *time.Timer
}

// MediaGroupBatcher collects media group messages and dispatches them together.
type MediaGroupBatcher struct {
	mu      sync.Mutex
	groups  map[string]*mediaGroupEntry
	handler func(messages []*telegram.Message)
}

// NewMediaGroupBatcher creates a batcher that calls handler with all messages
// in a media group once the batch delay expires.
func NewMediaGroupBatcher(handler func(messages []*telegram.Message)) *MediaGroupBatcher {
	return &MediaGroupBatcher{
		groups:  make(map[string]*mediaGroupEntry),
		handler: handler,
	}
}

// Add buffers a message for its media group. Returns true if the message was
// buffered (is part of a media group), false if it has no media_group_id.
func (b *MediaGroupBatcher) Add(msg *telegram.Message) bool {
	if msg.MediaGroupID == "" {
		return false
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	entry, exists := b.groups[msg.MediaGroupID]
	if !exists {
		entry = &mediaGroupEntry{}
		b.groups[msg.MediaGroupID] = entry

		groupID := msg.MediaGroupID
		entry.timer = time.AfterFunc(mediaGroupDelay, func() {
			b.dispatch(groupID)
		})
	} else {
		// Reset timer — more photos arriving.
		entry.timer.Reset(mediaGroupDelay)
	}

	entry.messages = append(entry.messages, msg)
	return true
}

// dispatch fires the handler with all collected messages for a group.
func (b *MediaGroupBatcher) dispatch(groupID string) {
	b.mu.Lock()
	entry, ok := b.groups[groupID]
	if !ok {
		b.mu.Unlock()
		return
	}
	messages := entry.messages
	delete(b.groups, groupID)
	b.mu.Unlock()

	if len(messages) > 0 && b.handler != nil {
		b.handler(messages)
	}
}
