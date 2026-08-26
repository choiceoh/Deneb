package server

import (
	"strings"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/nativesync"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/groupware"
)

type nativeWorkFeedStore struct {
	store            *workfeed.Store
	sync             *nativesync.Store
	log              interface{ Error(string, ...any) }
	onEvolveVerdict  func(workfeed.Item, string) error
	onLadderAction   func(workfeed.Item, string) error
	onApprovalAct    func(workfeed.Item, string, string) error
	onSelfCorrection func(workfeed.Item, string, string) error
	onWikiMaint      func(workfeed.Item, string, string) error
	onDreamRevert    func(workfeed.Item, string) error
	// wikiDir routes dream-card corrections into the dreamer's 반증 queue
	// (improvement-ideas 5.7); empty disables the funnel.
	wikiDir string
}

func (s *Server) nativeWorkFeedStore() *nativeWorkFeedStore {
	if s == nil || s.workFeedStore == nil {
		return nil
	}
	var wikiDir string
	if s.wikiStore != nil {
		wikiDir = s.wikiStore.Dir()
	}
	return &nativeWorkFeedStore{
		store:            s.workFeedStore,
		sync:             s.nativeSyncStore,
		log:              s.logger,
		onEvolveVerdict:  s.handleEvolveVerdictAction,
		onLadderAction:   s.handleLadderCardAction,
		onApprovalAct:    s.handleGroupwareApprovalAction,
		onSelfCorrection: s.handleSelfCorrectionCardAction,
		onWikiMaint:      s.handleWikiMaintCardAction,
		onDreamRevert:    s.handleDreamRevertCardAction,
		wikiDir:          wikiDir,
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

func (s *nativeWorkFeedStore) HasActiveSourceRef(source, refID string) (bool, error) {
	if s == nil || s.store == nil {
		return false, nil
	}
	_, ok, err := s.store.FindActiveBySourceRef(source, refID)
	return ok, err
}

// AckBySourceRef idempotently acknowledges every active card matching source and
// refID, routing each update through Ack so native sync receives the existing event.
func (s *nativeWorkFeedStore) AckBySourceRef(source, refID string) error {
	if s == nil || s.store == nil {
		return nil
	}
	for {
		item, ok, err := s.store.FindActiveBySourceRef(source, refID)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if _, err := s.Ack(item.ID); err != nil {
			return err
		}
	}
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

// EscalateApprovalBySourceRef updates one existing approval card and mirrors
// the workfeed.updated event. Missing cards are an idempotent no-op.
func (s *nativeWorkFeedStore) EscalateApprovalBySourceRef(refID string, level int, ageLabel string) (bool, error) {
	item, ok, err := s.store.FindActiveBySourceRef(workfeed.SourceGroupwareApproval, refID)
	if err != nil || !ok {
		return false, err
	}
	item, err = s.store.EscalateApproval(item.ID, level, ageLabel)
	if err != nil {
		return false, err
	}
	s.record(nativesync.WorkFeedUpdated(item))
	return true, nil
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

// minDreamCorrectionRunes is the shortest note that can carry a correction.
const minDreamCorrectionRunes = 4

// Correct attaches operator feedback to an item and mirrors the change.
func (s *nativeWorkFeedStore) Correct(id, note string) (workfeed.Item, error) {
	item, err := s.store.Correct(id, note)
	if err != nil {
		return workfeed.Item{}, err
	}
	s.record(nativesync.WorkFeedUpdated(item))
	s.funnelDreamCorrection(item, note)
	return item, nil
}

// funnelDreamCorrection routes an operator correction into the dreamer's
// disconfirming-evidence queue (improvement-ideas 5.7) so the next
// critique-eligible cycle re-examines what it got wrong instead of the
// correction dying in the card body.
//
// It used to accept dream cards ONLY, and the operator corrects almost
// everything else: of 30 corrections on the live feed (2026-06~08) — 박암민→
// 박환민, 남해연구소→남양연구소, 한미 조선→한미 전선, 박승수→박상수 — exactly
// ZERO came from a dream card, so the queue held one record in its lifetime and
// the 5.7 loop was effectively empty. The facts themselves did get fixed in the
// session that received them; what was lost is the dreamer ever learning which
// of its own outputs were wrong.
//
// Agent-internal log cards are still excluded: a correction typed on a ladder
// or model-tuner card ("잠금해제해") is an instruction, not a fact the wiki holds.
// Best-effort by design.
func (s *nativeWorkFeedStore) funnelDreamCorrection(item workfeed.Item, note string) {
	note = strings.TrimSpace(note)
	if s.wikiDir == "" || note == "" {
		return
	}
	// Bare acknowledgements ("ㅇㅇ", "승인") carry no disconfirming content and
	// would only dilute the critique prompt.
	if utf8.RuneCountInString(note) < minDreamCorrectionRunes {
		return
	}
	if workfeed.IsLogSource(item.Source) {
		return
	}
	if err := wiki.RecordDreamCorrection(s.wikiDir, "workfeed-correct:"+item.Source, item.RefID, note); err != nil {
		if s.log != nil {
			s.log.Error("native sync: dream correction queue append failed", "error", err)
		}
	}
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
func (s *nativeWorkFeedStore) RunAction(itemID, actionID, comment string) (workfeed.ActionResult, error) {
	approvalComment := ""
	if actionID == groupwareApprovalActionReject {
		approvalComment = groupware.SanitizeApprovalComment(comment)
	}
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
			return s.onApprovalAct(item, action.ID, approvalComment)
		}
		if s.onSelfCorrection != nil && item.Source == selfCorrectionSource &&
			(action.ID == workfeedApproveAction || action.ID == workfeedRejectAction) {
			return s.onSelfCorrection(item, action.ID, approvalComment)
		}
		if s.onWikiMaint != nil && item.Source == wikiMaintSource &&
			(action.ID == workfeedApproveAction || action.ID == workfeedRejectAction) {
			return s.onWikiMaint(item, action.ID, approvalComment)
		}
		if s.onDreamRevert != nil && item.Source == workfeed.SourceDream &&
			strings.HasPrefix(action.ID, dreamRevertActionPrefix) {
			return s.onDreamRevert(item, action.ID)
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
