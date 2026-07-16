package server

import (
	"sync"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// sharedReadOnlyServer returns one package-level Server for tests that only
// read HTTP/RPC surfaces and never assign Server fields (cronService, ready,
// GenesisSubsystem, …). Mutating tests must call New themselves.
//
// New(":0") costs ~2s; the slow server suite was dominated by repeating that
// construction. Sharing the read-only fixture collapses that wall without
// changing assertions.
func sharedReadOnlyServer(t *testing.T) *Server {
	t.Helper()
	sharedROServer.once.Do(func() {
		sharedROServer.srv = testutil.Must(New(":0"))
	})
	return sharedROServer.srv
}

var sharedROServer struct {
	once sync.Once
	srv  *Server
}
