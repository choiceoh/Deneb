package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/domain/nativesync"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

type nativeWorkFeedStore struct {
	store *workfeed.Store
	sync  *nativesync.Store
	log   interface{ Error(string, ...any) }
}

func (s *Server) nativeWorkFeedStore() *nativeWorkFeedStore {
	if s == nil || s.workFeedStore == nil {
		return nil
	}
	return &nativeWorkFeedStore{
		store: s.workFeedStore,
		sync:  s.nativeSyncStore,
		log:   s.logger,
	}
}

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

func (s *nativeWorkFeedStore) List(limit int, includeAcked bool) ([]workfeed.Item, int, error) {
	return s.store.List(limit, includeAcked)
}

func (s *nativeWorkFeedStore) ListRange(limit int, includeAcked bool, sinceMs, beforeMs int64) ([]workfeed.Item, int, error) {
	return s.store.ListRange(limit, includeAcked, sinceMs, beforeMs)
}

func (s *nativeWorkFeedStore) Ack(id string) (workfeed.Item, error) {
	item, err := s.store.Ack(id)
	if err != nil {
		return workfeed.Item{}, err
	}
	s.record(nativesync.WorkFeedUpdated(item))
	return item, nil
}

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

func (s *nativeWorkFeedStore) Correct(id, note string) (workfeed.Item, error) {
	item, err := s.store.Correct(id, note)
	if err != nil {
		return workfeed.Item{}, err
	}
	s.record(nativesync.WorkFeedUpdated(item))
	return item, nil
}

func (s *nativeWorkFeedStore) Rewrite(id, newBody string) (workfeed.Item, error) {
	item, err := s.store.Rewrite(id, newBody)
	if err != nil {
		return workfeed.Item{}, err
	}
	s.record(nativesync.WorkFeedUpdated(item))
	return item, nil
}

func (s *nativeWorkFeedStore) RunAction(itemID, actionID string) (workfeed.ActionResult, error) {
	result, err := s.store.RunAction(itemID, actionID)
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
