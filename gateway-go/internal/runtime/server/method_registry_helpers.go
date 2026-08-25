package server

import (
	"fmt"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/mailpriority"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calprop"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localcal"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localtodo"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
	minischedule "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/schedule"
	handlermail "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/mail"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
)

// makeMailAnalysisWikiSink returns the SaveToWiki callback the Mini App's
// gmail.analyze handler invokes after a fresh LLM run. We persist into the
// wiki so the analysis (a) shows up in recall/search, (b) accumulates per
// sender for RAG context on future analyses. Page assembly lives in
// wiki_mail_analysis.go so this file stays focused on wiring. Returns nil
// if no wiki store is available, which is the handler's signal to skip
// persistence entirely.
func makeMailAnalysisWikiSink(hub *rpcutil.GatewayHub) func(handlermail.WikiAnalysisInput) error {
	return func(in handlermail.WikiAnalysisInput) error {
		store := hub.Opt.WikiStore
		if store == nil {
			return nil
		}
		// Same gate as the autonomous sink: a body that is process narration or
		// an error string is not an analysis, and persisting it would put it in
		// recall. Refuse rather than store — the caller surfaces the failure.
		if err := mailanalysis.AnalysisUsable(in.Analysis); err != nil {
			return fmt.Errorf("분석 본문을 위키에 저장할 수 없음: %w", err)
		}
		return store.WritePage(mailAnalysisWikiPath(in.MsgID, in.RelatedProjects), buildMailAnalysisPage(in))
	}
}

// resolveLocalCalendar returns the process-wide local calendar store, or a nil
// interface (so handlers degrade) when its file can't be read. Returning a nil
// literal — not the (nil, err) store — avoids a non-nil interface wrapping a nil
// pointer. The store lives at {stateDir}/calendar.json (dev uses its own dir).
// localFileStoreOrNil opens the default on-box file store, returning a nil
// interface (not a typed-nil *LocalStore) on error so FilesBrowseMethods skips
// the domain rather than panicking on a nil deref later.
func localFileStoreOrNil(logger *slog.Logger) filestore.Store {
	store, err := filestore.DefaultLocalStore()
	if err != nil {
		if logger != nil {
			logger.Error("local file store unavailable — miniapp.files.* disabled", "error", err)
		}
		return nil
	}
	return store
}

func resolveLocalCalendar(logger *slog.Logger) minischedule.LocalCalendar {
	store, err := localcal.Default()
	if err != nil {
		if logger != nil {
			logger.Error("local calendar store unavailable — add/edit/delete disabled", "error", err)
		}
		return nil
	}
	return store
}

// resolveCalendarProposals returns the process-wide calendar-proposal store
// (the bell), or a nil interface when its file can't be read. Mirrors
// resolveLocalCalendar. The store lives at {stateDir}/calendar_proposals.json.
func resolveCalendarProposals(logger *slog.Logger) minischedule.CalProposals {
	store, err := calprop.Default()
	if err != nil {
		if logger != nil {
			logger.Error("calendar proposal store unavailable — bell disabled", "error", err)
		}
		return nil
	}
	return store
}

// resolveLocalTodos returns the process-wide to-do store, or a nil interface (so
// handlers degrade to UNAVAILABLE) when its file can't be read. Mirrors
// resolveLocalCalendar. The store lives at {stateDir}/todos.json.
func resolveLocalTodos(logger *slog.Logger) minischedule.LocalTodos {
	store, err := localtodo.Default()
	if err != nil {
		if logger != nil {
			logger.Error("local todo store unavailable — to-do list disabled", "error", err)
		}
		return nil
	}
	return store
}

// mailPriorityScorer builds the inbox-row scorer for the gmail list handler.
// The scorer is stateless and cheap to construct. The VIP signal binds the
// (possibly nil) contacts store; the active-counterparty signal binds the
// wiki-derived cached lookup — either nil simply drops its signal.
func mailPriorityScorer(cs *contacts.Store, cp interface{ Has(string) bool }) *mailpriority.Scorer {
	var vip func(string) bool
	if cs != nil {
		vip = cs.HasEmail
	}
	var counterparty func(string) bool
	if cp != nil {
		counterparty = cp.Has
	}
	return mailpriority.New(vip, counterparty)
}
