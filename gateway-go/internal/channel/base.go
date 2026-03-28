package channel

import "sync"

// PluginBase provides mutex-protected status tracking for channel.Plugin
// implementations. Embed it in your Plugin struct and call SetStatus instead
// of assigning to the status field directly.
//
//	type Plugin struct {
//	    channel.PluginBase          // provides Status() + SetStatus()
//	    mu     sync.Mutex           // protects plugin-specific fields
//	    client *SomeClient
//	    ...
//	}
type PluginBase struct {
	statusMu sync.Mutex
	status   Status
}

// Status implements channel.Plugin.
func (b *PluginBase) Status() Status {
	b.statusMu.Lock()
	defer b.statusMu.Unlock()
	return b.status
}

// SetStatus updates the plugin's status. Safe to call from any goroutine.
func (b *PluginBase) SetStatus(s Status) {
	b.statusMu.Lock()
	defer b.statusMu.Unlock()
	b.status = s
}
