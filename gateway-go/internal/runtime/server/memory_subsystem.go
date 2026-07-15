package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/domainbind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/svcops"
)

// MemorySubsystem groups the wiki knowledge base and contacts address-book mirror.
// wikiStore is late-bound during initMemorySubsystem() in the chat pipeline setup.
// contactsStore is created earlier, during registerEarlyMethods() (no chat dep), so
// it is available when the contacts tool is wired during chat init.
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type MemorySubsystem struct {
	wikiStore       *domainbind.WikiStore       // set during initMemorySubsystem()
	notebookStore   *domainbind.NotebookStore   // set during initToolsAndDeps(); deal-anchored source collections
	contactsStore   *domainbind.ContactsStore   // set during registerEarlyMethods()
	workFeedStore   *domainbind.WorkFeedStore   // set during registerEarlyMethods()
	nativeSyncStore *domainbind.NativeSyncStore // set during registerEarlyMethods()

	// cpProjects caches the wiki-derived counterparty→projects map for the
	// mail-analysis party anchor (mail_counterparty.go). Zero value ready;
	// reads tolerate the late-bound wikiStore.
	cpProjects svcops.CounterpartyProjectsCache
}
