package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/wiki"
)

// MemorySubsystem groups the wiki knowledge base.
// wikiStore is late-bound during initMemorySubsystem() in the chat pipeline setup.
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type MemorySubsystem struct {
	wikiStore *wiki.Store // set during initMemorySubsystem()
}
