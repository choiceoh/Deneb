// Package main provides the entry point for the Deneb gateway server.
//
// This is the standalone Go gateway — all RPC methods are handled natively
// without a Node.js Plugin Host bridge.
package main

import (
	"os"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/bootstrap"
)

// Version is the gateway version, injected at build time via ldflags.
// Falls back to "dev" for untagged builds.
var Version = "dev"

func main() {
	os.Exit(bootstrap.Run(Version))
}
