// Package servermail is a composition-root feature package for mail,
// calendar, phone, wiki-mail-analysis, and work-feed wiring. It owns the
// memory stores (wiki/notebook/contacts/workfeed/native-sync/mailstore) and
// never imports runtime/server — cross-cutting state it needs from its
// siblings (chat, auto) comes through serverport.Host.
package servermail

import (
	"path/filepath"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/nativesync"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/notebook"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailstore"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/mailflow"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/phoneevents"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/proactive"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/serverport"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/wikiwork"
)

// Manager owns the memory stores (wiki/notebook/contacts/workfeed/native-sync/
// mailstore) plus phone-action + phone-event wiring. Its exported fields are
// set as each store comes online during composition-root boot (New /
// registerEarlyMethods / initMemorySubsystem-equivalent init in serverchat),
// and read directly by the composition root (runtime/server) — only lateral
// reads from serverchat/serverauto go through serverport.Host.
type Manager struct {
	Host serverport.Host

	WikiStore       *wiki.Store       // set once wiki init runs (see servermail.InitMemory)
	NotebookStore   *notebook.Store   // set during chat's tool-deps init; deal-anchored source collections
	ContactsStore   *contacts.Store   // set during registerEarlyMethods
	WorkFeedStore   *workfeed.Store   // set during registerEarlyMethods
	NativeSyncStore *nativesync.Store // set during registerEarlyMethods
	MailStore       *mailstore.Store  // local file-backed mail archive mirror

	// CPProjects caches the wiki-derived counterparty→projects map for the
	// mail-analysis party anchor. Zero value ready; reads tolerate a
	// not-yet-set WikiStore.
	CPProjects mailflow.CounterpartyProjectsCache

	// PushHub, PhoneActions, PhoneEventLedger, and SiteVisitRecorder are the
	// phone-event ingest wiring (see phone_action.go). PushHub is shared with
	// serverchat/serverauto notifications too, so it is also reachable via
	// Host.PushHub() for siblings; servermail itself owns construction.
	PushHub *proactive.Hub

	phoneActions          *phoneActionAwaiter
	phoneEventLedger      *phoneevents.Ledger
	phoneEventLedgerOnce  sync.Once
	siteVisitRecorder     *wikiwork.SiteVisitRecorder
	siteVisitRecorderOnce sync.Once
}

// New creates a Manager bound to host. PushHub is created eagerly (matches
// the pre-existing Server.New behavior of never leaving it nil).
func New(host serverport.Host) *Manager {
	return &Manager{
		Host:         host,
		PushHub:      proactive.NewHub(),
		phoneActions: newPhoneActionAwaiter(),
	}
}

// InitMemory opens the local mail-store mirror and the wiki knowledge base.
// Split out of the legacy initMemorySubsystem (model-registry construction
// now lives in serverchat.Manager.InitModelRegistry) so servermail owns the
// stores it is the composition root for. Must run before serverchat and
// serverauto read WikiStore()/MailStore() through Host.
func (m *Manager) InitMemory() {
	logger := m.Host.Logger()

	mailStoreDir := filepath.Join(config.ResolveStateDir(), "mailstore")
	if ms, err := mailstore.New(mailStoreDir); err != nil {
		logger.Warn("mailstore unavailable", "error", err)
	} else {
		m.MailStore = ms
		logger.Info("mailstore enabled", "dir", mailStoreDir, "messages", ms.Len())
		// Seed historical mail into the store once, in the background, so older-mail
		// reads hit the fast path instead of the ~12.9s per-call IMAP fallback (the
		// store only auto-fills with NEW mail otherwise). One-shot + best-effort.
		m.maybeAutoBackfillMailStore(mailStoreDir)
	}

	if wikiCfg := wiki.ConfigFromEnv(); wikiCfg.Enabled {
		wikiStore, err := wiki.NewStore(wikiCfg.Dir, wikiCfg.DiaryDir)
		if err != nil {
			logger.Warn("wiki store unavailable", "error", err)
		} else {
			m.WikiStore = wikiStore
			logger.Info("wiki knowledge base enabled", "dir", wikiCfg.Dir)
		}
	}
}
