package server

import (
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/nativesync"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/notebook"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/mailflow"
)

// MemorySubsystem groups the wiki knowledge base and contacts address-book mirror.
// wikiStore is late-bound during initMemorySubsystem() in the chat pipeline setup.
// contactsStore is created earlier, during registerEarlyMethods() (no chat dep), so
// it is available when the contacts tool is wired during chat init.
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type MemorySubsystem struct {
	wikiStore *wiki.Store // set during initMemorySubsystem()
	// factCutover bounds how many consecutive startups may die on a failing
	// fact-plane cutover before the gateway starts with wiki disabled instead
	// (fact_cutover_guard.go). Lazily built so a zero-value Server is usable.
	factCutover   *factCutoverGuard
	notebookStore *notebook.Store // set during initToolsAndDeps(); deal-anchored source collections
	contactsStore *contacts.Store // set during registerEarlyMethods()
	workFeedStore *workfeed.Store // set during registerEarlyMethods()
	// Cached wiki proper-noun bias list for the glasses ASR path (see
	// evenWikiHotwords): rebuilt on a TTL because a live caption asks for it
	// every three seconds.
	evenHotwordMu    sync.Mutex
	evenHotwordCache string
	evenHotwordAt    time.Time
	nativeSyncStore  *nativesync.Store // set during registerEarlyMethods()

	// cpProjects caches the wiki-derived counterparty→projects map for the
	// mail-analysis party anchor (mail_counterparty.go). Zero value ready;
	// reads tolerate the late-bound wikiStore.
	cpProjects mailflow.CounterpartyProjectsCache
}
