package plugin

import (
	"sync"
)

// ConversationBinding associates a plugin with a specific conversation/session scope.
type ConversationBinding struct {
	PluginID   string `json:"pluginId"`
	Channel    string `json:"channel"`
	AccountID  string `json:"accountId,omitempty"`
	SessionKey string `json:"sessionKey"`
	BoundAt    int64  `json:"boundAt"`
	Approved   bool   `json:"approved"`
}

// ConversationBindingStore manages plugin-to-conversation bindings.
type ConversationBindingStore struct {
	mu       sync.RWMutex
	bindings map[string]*ConversationBinding // key: pluginID:channel:accountID
}

// NewConversationBindingStore creates a new binding store.
func NewConversationBindingStore() *ConversationBindingStore {
	return &ConversationBindingStore{
		bindings: make(map[string]*ConversationBinding),
	}
}

func bindingKey(pluginID, channel, accountID string) string {
	return pluginID + ":" + channel + ":" + accountID
}

// Bind creates or updates a conversation binding.
func (s *ConversationBindingStore) Bind(binding ConversationBinding) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := bindingKey(binding.PluginID, binding.Channel, binding.AccountID)
	s.bindings[key] = &binding
}

// Unbind removes a conversation binding.
func (s *ConversationBindingStore) Unbind(pluginID, channel, accountID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := bindingKey(pluginID, channel, accountID)
	delete(s.bindings, key)
}

// Get returns a binding by plugin/channel/account, or nil.
func (s *ConversationBindingStore) Get(pluginID, channel, accountID string) *ConversationBinding {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := bindingKey(pluginID, channel, accountID)
	b := s.bindings[key]
	if b == nil {
		return nil
	}
	cp := *b
	return &cp
}

// ListByPlugin returns all bindings for a plugin.
func (s *ConversationBindingStore) ListByPlugin(pluginID string) []ConversationBinding {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []ConversationBinding
	for _, b := range s.bindings {
		if b.PluginID == pluginID {
			result = append(result, *b)
		}
	}
	return result
}

// ListByChannel returns all bindings for a channel.
func (s *ConversationBindingStore) ListByChannel(channel string) []ConversationBinding {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []ConversationBinding
	for _, b := range s.bindings {
		if b.Channel == channel {
			result = append(result, *b)
		}
	}
	return result
}

// Count returns the total number of bindings.
func (s *ConversationBindingStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.bindings)
}
