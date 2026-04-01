package rpc

import (
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
)

// RegisterDomain bulk-registers all handlers from a domain handler package.
// Each domain package returns a map[string]rpcutil.HandlerFunc from its
// Methods() function; this helper iterates and registers them on the dispatcher.
// HandlerFunc is an alias for rpcutil.HandlerFunc, so no conversion is needed.
//
// All methods are added under a single lock and the snapshot is published once,
// making the registration atomic from the dispatcher's perspective.
func (d *Dispatcher) RegisterDomain(methods map[string]rpcutil.HandlerFunc) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for name, h := range methods {
		if d.registryValidation != nil {
			if _, exists := d.staging[name]; exists {
				module := d.registryValidation.module
				if module == "" {
					module = "unknown"
				}
				d.registryValidation.errs = append(d.registryValidation.errs,
					fmt.Errorf("duplicate rpc method %q in module %q", name, module))
				continue
			}
		}
		d.staging[name] = h
	}
	d.publishLocked()
}
