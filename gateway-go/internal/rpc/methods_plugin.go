package rpc

import (
	"context"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// PluginDeps holds the dependencies for plugin RPC methods.
type PluginDeps struct {
	Deps
	PluginRegistry PluginRegistry
}

// PluginRegistry is the interface for querying registered plugins.
// This decouples the RPC layer from the concrete plugin manager.
type PluginRegistry interface {
	ListPlugins() []protocol.PluginMeta
	GetPluginHealth(id string) *protocol.PluginHealthStatus
}

// RegisterPluginMethods registers plugin-related RPC methods.
func RegisterPluginMethods(d *Dispatcher, deps PluginDeps) {
	if deps.PluginRegistry == nil {
		return
	}

	d.Register("plugins.list", pluginsList(deps))
	d.Register("plugins.snapshot", pluginsSnapshot(deps))
}

func pluginsList(deps PluginDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		plugins := deps.PluginRegistry.ListPlugins()
		resp := protocol.MustResponseOK(req.ID, plugins)
		return resp
	}
}

func pluginsSnapshot(deps PluginDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		plugins := deps.PluginRegistry.ListPlugins()
		health := make([]protocol.PluginHealthStatus, 0, len(plugins))
		for _, p := range plugins {
			if h := deps.PluginRegistry.GetPluginHealth(p.ID); h != nil {
				health = append(health, *h)
			}
		}
		snapshot := protocol.PluginRegistrySnapshot{
			Plugins:    plugins,
			Health:     health,
			SnapshotAt: time.Now().UnixMilli(),
		}
		resp := protocol.MustResponseOK(req.ID, snapshot)
		return resp
	}
}
