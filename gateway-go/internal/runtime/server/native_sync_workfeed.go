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
	out, err := s.store.Append(item)
	if err != nil {
		return workfeed.Item{}, err
	}
	s.record(nativesync.TypeWorkFeedCreated, out.ID, out.SessionKey, out.ID, map[string]any{
		"item": out,
	})
	return out, nil
}

func (s *nativeWorkFeedStore) List(limit int, includeAcked bool) ([]workfeed.Item, int, error) {
	return s.store.List(limit, includeAcked)
}

func (s *nativeWorkFeedStore) Ack(id string) (workfeed.Item, error) {
	item, err := s.store.Ack(id)
	if err != nil {
		return workfeed.Item{}, err
	}
	s.record(nativesync.TypeWorkFeedUpdated, item.ID, item.SessionKey, item.ID, map[string]any{
		"item": item,
	})
	return item, nil
}

func (s *nativeWorkFeedStore) RunAction(itemID, actionID string) (workfeed.ActionResult, error) {
	result, err := s.store.RunAction(itemID, actionID)
	if err != nil {
		return workfeed.ActionResult{}, err
	}
	s.record(nativesync.TypeWorkFeedActionRun, result.Item.ID, result.SessionKey, result.Item.ID, map[string]any{
		"item":           result.Item,
		"action":         result.Action,
		"sessionKey":     result.SessionKey,
		"prompt":         result.Prompt,
		"message":        result.Message,
		"removeFromFeed": result.RemoveFromFeed,
	})
	return result, nil
}

func (s *nativeWorkFeedStore) record(typ, entityID, sessionKey, workFeedItemID string, payload any) {
	if s == nil || s.sync == nil {
		return
	}
	if _, err := s.sync.Append(nativesync.AppendInput{
		Type:           typ,
		EntityID:       entityID,
		SessionKey:     sessionKey,
		WorkFeedItemID: workFeedItemID,
		Payload:        payload,
	}); err != nil && s.log != nil {
		s.log.Error("native sync: work feed event append failed",
			"type", typ, "entityID", entityID, "error", err)
	}
}
