// Aurora context store backed by JSON files.
//
// Manages context_items, messages, summaries, and compaction_events
// that power the Rust Aurora hierarchical compaction engine via FFI.
// Uses a single JSON file for persistence with in-memory working state.
// Optimized for single-user deployment (no concurrent access concerns).
package aurora

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Store is the Aurora context store (in-memory + JSON file persistence).
type Store struct {
	mu     sync.RWMutex
	path   string
	logger *slog.Logger
	data   storeData
}

// storeData is the on-disk schema.
type storeData struct {
	ContextItems     []ContextItem            `json:"contextItems"`
	Messages         map[string]MessageRecord `json:"messages"`        // key: messageId as string
	Summaries        map[string]SummaryRecord `json:"summaries"`       // key: summaryId
	SummaryParents   map[string][]string      `json:"summaryParents"`  // summaryId -> parentIds
	SummaryMessages  map[string][]uint64      `json:"summaryMessages"` // summaryId -> messageIds
	CompactionEvents []CompactionEvent        `json:"compactionEvents"`
	NextOrdinalVal   uint64                   `json:"nextOrdinal"`
	NextMessageID    uint64                   `json:"nextMessageId"`
}

// CompactionEvent is a persisted compaction event record.
type CompactionEvent struct {
	ConversationID   uint64 `json:"conversationId"`
	Pass             string `json:"pass"`
	Level            string `json:"level"`
	TokensBefore     uint64 `json:"tokensBefore"`
	TokensAfter      uint64 `json:"tokensAfter"`
	CreatedSummaryID string `json:"createdSummaryId"`
	CreatedAt        int64  `json:"createdAt"`
}

// StoreConfig configures the Aurora store.
type StoreConfig struct {
	// DatabasePath is the JSON store file path.
	// Default: ~/.deneb/aurora.json
	DatabasePath string `json:"databasePath"`
}

// DefaultStoreConfig returns production defaults for single-user DGX Spark.
func DefaultStoreConfig() StoreConfig {
	home, _ := os.UserHomeDir()
	return StoreConfig{
		DatabasePath: filepath.Join(home, ".deneb", "aurora.json"),
	}
}

// ── Data types matching Rust core-rs types ──────────────────────────────────

// ContextItem corresponds to core-rs compaction::ContextItem.
type ContextItem struct {
	ConversationID uint64  `json:"conversationId"`
	Ordinal        uint64  `json:"ordinal"`
	ItemType       string  `json:"itemType"` // "message" or "summary"
	MessageID      *uint64 `json:"messageId,omitempty"`
	SummaryID      *string `json:"summaryId,omitempty"`
	CreatedAt      int64   `json:"createdAt"` // epoch ms
}

// MessageRecord corresponds to core-rs compaction::MessageRecord.
type MessageRecord struct {
	MessageID      uint64 `json:"messageId"`
	ConversationID uint64 `json:"conversationId"`
	Seq            uint64 `json:"seq"`
	Role           string `json:"role"`
	Content        string `json:"content"`
	TokenCount     uint64 `json:"tokenCount"`
	CreatedAt      int64  `json:"createdAt"` // epoch ms
}

// SummaryRecord corresponds to core-rs compaction::SummaryRecord.
type SummaryRecord struct {
	SummaryID               string   `json:"summaryId"`
	ConversationID          uint64   `json:"conversationId"`
	Kind                    string   `json:"kind"` // "leaf" or "condensed"
	Depth                   uint32   `json:"depth"`
	Content                 string   `json:"content"`
	TokenCount              uint64   `json:"tokenCount"`
	FileIDs                 []string `json:"fileIds"`
	EarliestAt              *int64   `json:"earliestAt,omitempty"`
	LatestAt                *int64   `json:"latestAt,omitempty"`
	DescendantCount         uint64   `json:"descendantCount"`
	DescendantTokenCount    uint64   `json:"descendantTokenCount"`
	SourceMessageTokenCount uint64   `json:"sourceMessageTokenCount"`
	CreatedAt               int64    `json:"createdAt"` // epoch ms
}

// SummaryStats holds aggregate summary info for context assembly.
type SummaryStats struct {
	MaxDepth           uint32 `json:"maxDepth"`
	CondensedCount     int    `json:"condensedCount"`
	LeafCount          int    `json:"leafCount"`
	TotalSummaryTokens uint64 `json:"totalSummaryTokens"`
}

// PersistLeafInput matches core-rs sweep::PersistLeafInput.
type PersistLeafInput struct {
	SummaryID               string   `json:"summaryId"`
	ConversationID          uint64   `json:"conversationId"`
	Content                 string   `json:"content"`
	TokenCount              uint64   `json:"tokenCount"`
	FileIDs                 []string `json:"fileIds"`
	EarliestAt              *int64   `json:"earliestAt,omitempty"`
	LatestAt                *int64   `json:"latestAt,omitempty"`
	SourceMessageTokenCount uint64   `json:"sourceMessageTokenCount"`
	MessageIDs              []uint64 `json:"messageIds"`
	StartOrdinal            uint64   `json:"startOrdinal"`
	EndOrdinal              uint64   `json:"endOrdinal"`
}

// PersistCondensedInput matches core-rs sweep::PersistCondensedInput.
type PersistCondensedInput struct {
	SummaryID               string   `json:"summaryId"`
	ConversationID          uint64   `json:"conversationId"`
	Depth                   uint32   `json:"depth"`
	Content                 string   `json:"content"`
	TokenCount              uint64   `json:"tokenCount"`
	FileIDs                 []string `json:"fileIds"`
	EarliestAt              *int64   `json:"earliestAt,omitempty"`
	LatestAt                *int64   `json:"latestAt,omitempty"`
	DescendantCount         uint64   `json:"descendantCount"`
	DescendantTokenCount    uint64   `json:"descendantTokenCount"`
	SourceMessageTokenCount uint64   `json:"sourceMessageTokenCount"`
	ParentSummaryIDs        []string `json:"parentSummaryIds"`
	StartOrdinal            uint64   `json:"startOrdinal"`
	EndOrdinal              uint64   `json:"endOrdinal"`
}

// PersistEventInput matches core-rs sweep::PersistEventInput.
type PersistEventInput struct {
	ConversationID   uint64 `json:"conversationId"`
	Pass             string `json:"pass"`
	Level            string `json:"level"`
	TokensBefore     uint64 `json:"tokensBefore"`
	TokensAfter      uint64 `json:"tokensAfter"`
	CreatedSummaryID string `json:"createdSummaryId"`
}

// ── Constructor ─────────────────────────────────────────────────────────────

// NewStore opens or creates an Aurora JSON store.
func NewStore(cfg StoreConfig, logger *slog.Logger) (*Store, error) {
	if logger == nil {
		logger = slog.Default()
	}

	dir := filepath.Dir(cfg.DatabasePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("aurora store: mkdir %s: %w", dir, err)
	}

	s := &Store{
		path:   cfg.DatabasePath,
		logger: logger,
		data: storeData{
			Messages:        make(map[string]MessageRecord),
			Summaries:       make(map[string]SummaryRecord),
			SummaryParents:  make(map[string][]string),
			SummaryMessages: make(map[string][]uint64),
		},
	}

	// Load existing data if file exists.
	if raw, err := os.ReadFile(cfg.DatabasePath); err == nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, &s.data); err != nil {
			logger.Warn("aurora store: corrupt file, starting fresh", "error", err)
			s.data = storeData{
				Messages:        make(map[string]MessageRecord),
				Summaries:       make(map[string]SummaryRecord),
				SummaryParents:  make(map[string][]string),
				SummaryMessages: make(map[string][]uint64),
			}
		}
	}

	// Ensure maps are initialized.
	if s.data.Messages == nil {
		s.data.Messages = make(map[string]MessageRecord)
	}
	if s.data.Summaries == nil {
		s.data.Summaries = make(map[string]SummaryRecord)
	}
	if s.data.SummaryParents == nil {
		s.data.SummaryParents = make(map[string][]string)
	}
	if s.data.SummaryMessages == nil {
		s.data.SummaryMessages = make(map[string][]uint64)
	}

	logger.Info("aurora store opened", "path", cfg.DatabasePath,
		"items", len(s.data.ContextItems),
		"messages", len(s.data.Messages),
		"summaries", len(s.data.Summaries))
	return s, nil
}

// Close flushes data to disk.
func (s *Store) Close() error {
	return s.flush()
}

func (s *Store) flush() error {
	// Hold the exclusive write lock for the entire flush operation.
	// Using RLock here was unsafe: between releasing the read lock after
	// json.Marshal and writing the temp file, a concurrent PersistLeaf /
	// PersistCondensed could call flushLocked() with newer data, then
	// flush() would overwrite the file with the stale marshaled snapshot.
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flushLocked()
}

// msgKey converts a uint64 message ID to a map key.
func msgKey(id uint64) string {
	return fmt.Sprintf("%d", id)
}

// ── Context items ───────────────────────────────────────────────────────────

// FetchContextItems returns all context items for a conversation ordered by ordinal.
func (s *Store) FetchContextItems(conversationID uint64) ([]ContextItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var items []ContextItem
	for _, ci := range s.data.ContextItems {
		if ci.ConversationID == conversationID {
			items = append(items, ci)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Ordinal < items[j].Ordinal
	})
	return items, nil
}

// NextOrdinal returns the next available ordinal.
func (s *Store) NextOrdinal(conversationID uint64) (uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.NextOrdinalVal, nil
}

// ── Messages ────────────────────────────────────────────────────────────────

// FetchMessages returns messages by their IDs as a map.
func (s *Store) FetchMessages(messageIDs []uint64) (map[uint64]MessageRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[uint64]MessageRecord, len(messageIDs))
	for _, id := range messageIDs {
		if m, ok := s.data.Messages[msgKey(id)]; ok {
			result[id] = m
		}
	}
	return result, nil
}

// FetchTokenCount returns total token count for all context items.
func (s *Store) FetchTokenCount(conversationID uint64) (uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var total uint64
	for _, ci := range s.data.ContextItems {
		if ci.ConversationID != conversationID {
			continue
		}
		if ci.ItemType == "message" && ci.MessageID != nil {
			if m, ok := s.data.Messages[msgKey(*ci.MessageID)]; ok {
				total += m.TokenCount
			}
		} else if ci.ItemType == "summary" && ci.SummaryID != nil {
			if sr, ok := s.data.Summaries[*ci.SummaryID]; ok {
				total += sr.TokenCount
			}
		}
	}
	return total, nil
}

// ── Summaries ───────────────────────────────────────────────────────────────

// FetchSummaries returns summaries by their IDs as a map.
func (s *Store) FetchSummaries(summaryIDs []string) (map[string]SummaryRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]SummaryRecord, len(summaryIDs))
	for _, id := range summaryIDs {
		if sr, ok := s.data.Summaries[id]; ok {
			result[id] = sr
		}
	}
	return result, nil
}

// FetchDistinctDepths returns distinct summary depths for context items
// with ordinal <= maxOrdinal.
func (s *Store) FetchDistinctDepths(conversationID uint64, maxOrdinal uint64) ([]uint32, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	depthSet := make(map[uint32]bool)
	for _, ci := range s.data.ContextItems {
		if ci.ConversationID != conversationID || ci.Ordinal > maxOrdinal {
			continue
		}
		if ci.ItemType == "summary" && ci.SummaryID != nil {
			if sr, ok := s.data.Summaries[*ci.SummaryID]; ok {
				depthSet[sr.Depth] = true
			}
		}
	}

	depths := make([]uint32, 0, len(depthSet))
	for d := range depthSet {
		depths = append(depths, d)
	}
	sort.Slice(depths, func(i, j int) bool { return depths[i] < depths[j] })
	return depths, nil
}

// FetchSummaryStats returns aggregate summary info for a conversation.
func (s *Store) FetchSummaryStats(conversationID uint64) (SummaryStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var stats SummaryStats
	for _, sr := range s.data.Summaries {
		if sr.ConversationID != conversationID {
			continue
		}
		if sr.Depth > stats.MaxDepth {
			stats.MaxDepth = sr.Depth
		}
		if sr.Kind == "condensed" {
			stats.CondensedCount++
		} else {
			stats.LeafCount++
		}
		stats.TotalSummaryTokens += sr.TokenCount
	}
	return stats, nil
}

// maxCompactionEvents is the maximum number of compaction events retained on disk.
// Older entries are pruned to prevent unbounded file growth.
const maxCompactionEvents = 500

// PersistLeafSummary inserts a leaf summary and replaces compacted messages.
func (s *Store) PersistLeafSummary(input PersistLeafInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixMilli()
	fileIDs := input.FileIDs
	if fileIDs == nil {
		fileIDs = []string{}
	}

	// Insert summary.
	s.data.Summaries[input.SummaryID] = SummaryRecord{
		SummaryID:               input.SummaryID,
		ConversationID:          input.ConversationID,
		Kind:                    "leaf",
		Depth:                   0,
		Content:                 input.Content,
		TokenCount:              input.TokenCount,
		FileIDs:                 fileIDs,
		EarliestAt:              input.EarliestAt,
		LatestAt:                input.LatestAt,
		SourceMessageTokenCount: input.SourceMessageTokenCount,
		CreatedAt:               now,
	}

	// Link messages.
	s.data.SummaryMessages[input.SummaryID] = input.MessageIDs

	// Remove compacted context items in ordinal range and replace with summary.
	var kept []ContextItem
	for _, ci := range s.data.ContextItems {
		if ci.ConversationID == input.ConversationID &&
			ci.Ordinal >= input.StartOrdinal && ci.Ordinal <= input.EndOrdinal &&
			ci.ItemType == "message" {
			continue // remove
		}
		kept = append(kept, ci)
	}

	// Add summary context item at start ordinal.
	sid := input.SummaryID
	kept = append(kept, ContextItem{
		ConversationID: input.ConversationID,
		Ordinal:        input.StartOrdinal,
		ItemType:       "summary",
		SummaryID:      &sid,
		CreatedAt:      now,
	})

	sort.Slice(kept, func(i, j int) bool { return kept[i].Ordinal < kept[j].Ordinal })
	s.data.ContextItems = kept

	// Prune compacted message records — they are now captured by the leaf summary.
	for _, msgID := range input.MessageIDs {
		delete(s.data.Messages, msgKey(msgID))
	}

	return s.flushLocked()
}

// PersistCondensedSummary inserts a condensed summary and replaces children.
func (s *Store) PersistCondensedSummary(input PersistCondensedInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixMilli()
	fileIDs := input.FileIDs
	if fileIDs == nil {
		fileIDs = []string{}
	}

	s.data.Summaries[input.SummaryID] = SummaryRecord{
		SummaryID:               input.SummaryID,
		ConversationID:          input.ConversationID,
		Kind:                    "condensed",
		Depth:                   input.Depth,
		Content:                 input.Content,
		TokenCount:              input.TokenCount,
		FileIDs:                 fileIDs,
		EarliestAt:              input.EarliestAt,
		LatestAt:                input.LatestAt,
		DescendantCount:         input.DescendantCount,
		DescendantTokenCount:    input.DescendantTokenCount,
		SourceMessageTokenCount: input.SourceMessageTokenCount,
		CreatedAt:               now,
	}

	// Link parents.
	s.data.SummaryParents[input.SummaryID] = input.ParentSummaryIDs

	// Remove condensed child context items in range.
	var kept []ContextItem
	for _, ci := range s.data.ContextItems {
		if ci.ConversationID == input.ConversationID &&
			ci.Ordinal >= input.StartOrdinal && ci.Ordinal <= input.EndOrdinal &&
			ci.ItemType == "summary" {
			continue
		}
		kept = append(kept, ci)
	}

	sid := input.SummaryID
	kept = append(kept, ContextItem{
		ConversationID: input.ConversationID,
		Ordinal:        input.StartOrdinal,
		ItemType:       "summary",
		SummaryID:      &sid,
		CreatedAt:      now,
	})

	sort.Slice(kept, func(i, j int) bool { return kept[i].Ordinal < kept[j].Ordinal })
	s.data.ContextItems = kept

	// Prune condensed child summary records — they are now captured by the condensed summary.
	for _, parentID := range input.ParentSummaryIDs {
		delete(s.data.Summaries, parentID)
		delete(s.data.SummaryParents, parentID)
		delete(s.data.SummaryMessages, parentID)
	}

	return s.flushLocked()
}

// PersistEvent records a compaction event.
func (s *Store) PersistEvent(input PersistEventInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.CompactionEvents = append(s.data.CompactionEvents, CompactionEvent{
		ConversationID:   input.ConversationID,
		Pass:             input.Pass,
		Level:            input.Level,
		TokensBefore:     input.TokensBefore,
		TokensAfter:      input.TokensAfter,
		CreatedSummaryID: input.CreatedSummaryID,
		CreatedAt:        time.Now().UnixMilli(),
	})

	// Cap the event log to prevent unbounded file growth.
	if len(s.data.CompactionEvents) > maxCompactionEvents {
		s.data.CompactionEvents = s.data.CompactionEvents[len(s.data.CompactionEvents)-maxCompactionEvents:]
	}

	return s.flushLocked()
}

// ── Sync from chat ──────────────────────────────────────────────────────────

// SyncMessage ensures a chat message is tracked in the Aurora store.
func (s *Store) SyncMessage(conversationID uint64, role, content string, tokenCount uint64) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixMilli()
	ordinal := s.data.NextOrdinalVal
	s.data.NextOrdinalVal++

	msgID := s.data.NextMessageID
	s.data.NextMessageID++

	s.data.Messages[msgKey(msgID)] = MessageRecord{
		MessageID:      msgID,
		ConversationID: conversationID,
		Seq:            ordinal,
		Role:           role,
		Content:        content,
		TokenCount:     tokenCount,
		CreatedAt:      now,
	}

	mid := msgID
	s.data.ContextItems = append(s.data.ContextItems, ContextItem{
		ConversationID: conversationID,
		Ordinal:        ordinal,
		ItemType:       "message",
		MessageID:      &mid,
		CreatedAt:      now,
	})

	return msgID, s.flushLocked()
}

// Reset clears all data for a conversation.
func (s *Store) Reset(conversationID uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Filter context items.
	var kept []ContextItem
	for _, ci := range s.data.ContextItems {
		if ci.ConversationID != conversationID {
			kept = append(kept, ci)
		}
	}
	s.data.ContextItems = kept

	// Remove messages.
	for k, m := range s.data.Messages {
		if m.ConversationID == conversationID {
			delete(s.data.Messages, k)
		}
	}

	// Remove summaries and links.
	for k, sr := range s.data.Summaries {
		if sr.ConversationID == conversationID {
			delete(s.data.Summaries, k)
			delete(s.data.SummaryParents, k)
			delete(s.data.SummaryMessages, k)
		}
	}

	// Remove events.
	var keptEvents []CompactionEvent
	for _, e := range s.data.CompactionEvents {
		if e.ConversationID != conversationID {
			keptEvents = append(keptEvents, e)
		}
	}
	s.data.CompactionEvents = keptEvents

	return s.flushLocked()
}

// flushLocked writes data to disk. Caller must hold s.mu.
func (s *Store) flushLocked() error {
	raw, err := json.Marshal(s.data)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
