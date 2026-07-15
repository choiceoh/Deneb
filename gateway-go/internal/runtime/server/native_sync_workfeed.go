package server

import (
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/nativesync"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

type nativeWorkFeedStore struct {
	store           *workfeed.Store
	sync            *nativesync.Store
	log             interface{ Error(string, ...any) }
	onEvolveVerdict func(workfeed.Item, string) error
	onLadderAction  func(workfeed.Item, string) error
	onApprovalAct   func(workfeed.Item, string) error
}

func (s *Server) nativeWorkFeedStore() *nativeWorkFeedStore {
	if s == nil || s.workFeedStore == nil {
		return nil
	}
	return &nativeWorkFeedStore{
		store:           s.workFeedStore,
		sync:            s.nativeSyncStore,
		log:             s.logger,
		onEvolveVerdict: s.handleEvolveVerdictAction,
		onLadderAction:  s.handleLadderCardAction,
		onApprovalAct:   s.handleGroupwareApprovalAction,
	}
}

// Append stores a new work-feed item and mirrors its creation to native clients.
func (s *nativeWorkFeedStore) Append(item workfeed.Item) (workfeed.Item, error) {
	out, created, err := s.store.AppendIfNew(item)
	if err != nil {
		return workfeed.Item{}, err
	}
	// A duplicate of the most recent card writes no new item; skip the "created"
	// sync event so the client doesn't re-receive the same card.
	if created {
		s.record(nativesync.WorkFeedCreated(out))
	}
	return out, nil
}

// List returns the newest work-feed items using the underlying store's filters.
func (s *nativeWorkFeedStore) List(limit int, includeAcked bool) ([]workfeed.Item, int, error) {
	return s.store.List(limit, includeAcked)
}

// ListRange returns work-feed items bounded by creation time.
func (s *nativeWorkFeedStore) ListRange(limit int, includeAcked bool, sinceMs, beforeMs int64) ([]workfeed.Item, int, error) {
	return s.store.ListRange(limit, includeAcked, sinceMs, beforeMs)
}

// Ack acknowledges an item and publishes the updated state to native clients.
func (s *nativeWorkFeedStore) Ack(id string) (workfeed.Item, error) {
	item, err := s.store.Ack(id)
	if err != nil {
		return workfeed.Item{}, err
	}
	s.record(nativesync.WorkFeedUpdated(item))
	return item, nil
}

// MarkRead records that an item was opened and publishes the updated state.
func (s *nativeWorkFeedStore) MarkRead(id string) (workfeed.Item, error) {
	item, err := s.store.MarkRead(id)
	if err != nil {
		return workfeed.Item{}, err
	}
	// Mirror the read stamp to the native stream so the phone de-emphasizes the
	// card too (cross-surface read state). Reuses the generic updated event.
	s.record(nativesync.WorkFeedUpdated(item))
	return item, nil
}

// Correct attaches operator feedback to an item and mirrors the change.
func (s *nativeWorkFeedStore) Correct(id, note string) (workfeed.Item, error) {
	item, err := s.store.Correct(id, note)
	if err != nil {
		return workfeed.Item{}, err
	}
	s.record(nativesync.WorkFeedUpdated(item))
	return item, nil
}

// Rewrite replaces an item's body and mirrors the resulting item.
func (s *nativeWorkFeedStore) Rewrite(id, newBody string) (workfeed.Item, error) {
	item, err := s.store.Rewrite(id, newBody)
	if err != nil {
		return workfeed.Item{}, err
	}
	s.record(nativesync.WorkFeedUpdated(item))
	return item, nil
}

// RunAction executes a declared item action and publishes its result.
func (s *nativeWorkFeedStore) RunAction(itemID, actionID string) (workfeed.ActionResult, error) {
	effect := func(item workfeed.Item, action workfeed.Action) error {
		if s.onEvolveVerdict != nil && item.Source == evolveVerdictSource &&
			strings.HasPrefix(action.ID, "evolve-verdict:") {
			return s.onEvolveVerdict(item, action.ID)
		}
		if s.onLadderAction != nil && item.Source == ladderReadySource &&
			strings.HasPrefix(action.ID, ladderActionRelockPrefix) {
			return s.onLadderAction(item, action.ID)
		}
		if s.onApprovalAct != nil && item.Source == workfeed.SourceGroupwareApproval &&
			strings.HasPrefix(action.ID, "approval:") {
			return s.onApprovalAct(item, action.ID)
		}
		return nil
	}
	return s.RunActionWithEffect(itemID, actionID, effect)
}

// RunActionWithEffect keeps source-specific operator decisions retryable until
// their durable side effect succeeds, then publishes the settled result.
func (s *nativeWorkFeedStore) RunActionWithEffect(itemID, actionID string, effect workfeed.ActionEffect) (workfeed.ActionResult, error) {
	result, err := s.store.RunActionWithEffect(itemID, actionID, effect)
	if err != nil {
		return workfeed.ActionResult{}, err
	}
	s.record(nativesync.WorkFeedActionRun(result))
	return result, nil
}

func (s *nativeWorkFeedStore) record(in nativesync.AppendInput) {
	if s == nil || s.sync == nil {
		return
	}
	if _, err := s.sync.Append(in); err != nil && s.log != nil {
		s.log.Error("native sync: work feed event append failed",
			"type", in.Type, "entityID", in.EntityID, "error", err)
	}
}
