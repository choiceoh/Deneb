package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

// MemorySubsystem groups the wiki knowledge base and contacts address-book mirror.
// wikiStore is late-bound during initMemorySubsystem() in the chat pipeline setup.
// contactsStore is created earlier, during registerEarlyMethods() (no chat dep), so
// it is available when the contacts tool is wired during chat init.
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type MemorySubsystem struct {
	wikiStore     *wiki.Store     // set during initMemorySubsystem()
	contactsStore *contacts.Store // set during registerEarlyMethods()
	workFeedStore *workfeed.Store // set during registerEarlyMethods()
}
