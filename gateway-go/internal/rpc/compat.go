// compat.go provides backward-compatible type aliases and registration functions.
//
// Server code continues to call rpc.RegisterXxxMethods(d, rpc.XxxDeps{...}).
// Under the hood, these delegate to the new handler/* domain packages.
// This file will shrink over time as callers migrate to importing handler
// packages directly.
package rpc

import (
	handleragent "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/agent"
	handlerchannel "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/channel"
	handlerchat "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/chat"
	handlerffi "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/ffi"
	handlernode "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/node"
	handlerplatform "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/platform"
	handlerpresence "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/presence"
	handlerprocess "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/process"
	handlerprovider "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/provider"
	handlersession "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/session"
	handlerskill "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/skill"
	handlersystem "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/system"
)

// --- Type aliases for handler package Deps structs ---
// These allow server.go to keep using rpc.XxxDeps{...} syntax.

type ChatDeps = handlerchat.Deps
type ChatBtwDeps = handlerchat.BtwDeps
type SessionExecDeps = handlersession.ExecDeps
type ChannelLifecycleDeps = handlerchannel.LifecycleDeps
type EventsDeps = handlerchannel.EventsDeps
type MessagingDeps = handlerchannel.MessagingDeps
type NodeDeps = handlernode.Deps
type DeviceDeps = handlernode.DeviceDeps
type ApprovalDeps = handlerprocess.ApprovalDeps
type ACPDeps = handlerprocess.ACPDeps
type CronAdvancedDeps = handlerprocess.CronAdvancedDeps
type ProviderDeps = handlerprovider.Deps
type ModelsDeps = handlerprovider.ModelsDeps
type PluginDeps = handlerskill.PluginDeps
type ToolDeps = handlerskill.ToolDeps
type SkillDeps = handlerskill.Deps
type MonitoringDeps = handlersystem.MonitoringDeps
type DoctorDeps = handlersystem.DoctorDeps
type MaintenanceDeps = handlersystem.MaintenanceDeps
type UpdateDeps = handlersystem.UpdateDeps
type UsageDeps = handlersystem.UsageDeps
type LogsDeps = handlersystem.LogsDeps
type ConfigReloadDeps = handlersystem.ConfigReloadDeps
type ConfigAdvancedDeps = handlersystem.ConfigAdvancedDeps
type WizardDeps = handlerplatform.WizardDeps
type SecretDeps = handlerplatform.SecretDeps
type TalkDeps = handlerplatform.TalkDeps
type ExtendedDeps = handleragent.ExtendedDeps
type AgentsDeps = handleragent.AgentsDeps
type AutonomousDeps = handleragent.AutonomousDeps
type SessionDeps = handlersession.Deps
type HeartbeatDeps = handlerpresence.HeartbeatDeps
type PresenceDeps = handlerpresence.Deps
type VegaDeps = handlerffi.VegaDeps

// HeartbeatState and PresenceStore re-exports.
type HeartbeatState = handlerpresence.HeartbeatState
type PresenceStore = handlerpresence.Store
type PresenceEntry = handlerpresence.PresenceEntry

// BroadcastFunc re-export for server usage.
type BroadcastFunc = handlerprocess.BroadcastFunc

// Constructor re-exports.
var NewHeartbeatState = handlerpresence.NewHeartbeatState
var NewPresenceStore = handlerpresence.NewStore

// --- Registration wrapper functions ---

func RegisterChatMethods(d *Dispatcher, deps ChatDeps) {
	d.RegisterDomain(handlerchat.Methods(deps))
}

func RegisterChatBtwMethods(d *Dispatcher, deps ChatBtwDeps) {
	d.RegisterDomain(handlerchat.BtwMethods(deps))
}

func RegisterSessionMethods(d *Dispatcher, deps SessionDeps) {
	d.RegisterDomain(handlersession.Methods(deps))
}

func RegisterSessionRepairMethods(d *Dispatcher, _ SessionDeps) {
	// Repair methods are now included in handlersession.Methods().
	// This is a no-op for backward compatibility.
}

func RegisterSessionExecMethods(d *Dispatcher, deps SessionExecDeps) {
	d.RegisterDomain(handlersession.ExecMethods(deps))
}

func RegisterExtendedMethods(d *Dispatcher, deps ExtendedDeps) {
	d.RegisterDomain(handleragent.ExtendedMethods(deps))
}

func RegisterAgentsMethods(d *Dispatcher, deps AgentsDeps) {
	d.RegisterDomain(handleragent.CRUDMethods(deps))
}

func RegisterAutonomousMethods(d *Dispatcher, deps AutonomousDeps) {
	d.RegisterDomain(handleragent.AutonomousMethods(deps))
}

func RegisterChannelLifecycleMethods(d *Dispatcher, deps ChannelLifecycleDeps) {
	d.RegisterDomain(handlerchannel.LifecycleMethods(deps))
}

func RegisterEventsMethods(d *Dispatcher, deps EventsDeps) {
	d.RegisterDomain(handlerchannel.EventsMethods(deps))
}

func RegisterMessagingMethods(d *Dispatcher, deps MessagingDeps) {
	d.RegisterDomain(handlerchannel.MessagingMethods(deps))
}

func RegisterNodeMethods(d *Dispatcher, deps NodeDeps) {
	d.RegisterDomain(handlernode.Methods(deps))
}

func RegisterDeviceMethods(d *Dispatcher, deps DeviceDeps) {
	d.RegisterDomain(handlernode.DeviceMethods(deps))
}

func RegisterApprovalMethods(d *Dispatcher, deps ApprovalDeps) {
	d.RegisterDomain(handlerprocess.ApprovalMethods(deps))
}

func RegisterACPMethods(d *Dispatcher, deps *ACPDeps) {
	d.RegisterDomain(handlerprocess.ACPMethods(deps))
}

func RegisterCronAdvancedMethods(d *Dispatcher, deps CronAdvancedDeps) {
	d.RegisterDomain(handlerprocess.CronAdvancedMethods(deps))
}

func RegisterProviderMethods(d *Dispatcher, deps ProviderDeps) {
	d.RegisterDomain(handlerprovider.Methods(deps))
}

func RegisterModelsMethods(d *Dispatcher, deps ModelsDeps) {
	d.RegisterDomain(handlerprovider.ModelsMethods(deps))
}

func RegisterPluginMethods(d *Dispatcher, deps PluginDeps) {
	d.RegisterDomain(handlerskill.PluginMethods(deps))
}

func RegisterToolMethods(d *Dispatcher, deps ToolDeps) {
	d.RegisterDomain(handlerskill.ToolMethods(deps))
}

func RegisterSkillMethods(d *Dispatcher, deps SkillDeps) {
	d.RegisterDomain(handlerskill.Methods(deps))
}

func RegisterMonitoringMethods(d *Dispatcher, deps MonitoringDeps) {
	d.RegisterDomain(handlersystem.MonitoringMethods(deps))
}

func RegisterDoctorMethods(d *Dispatcher, deps DoctorDeps) {
	d.RegisterDomain(handlersystem.DoctorMethods(deps))
}

func RegisterMaintenanceMethods(d *Dispatcher, deps MaintenanceDeps) {
	d.RegisterDomain(handlersystem.MaintenanceMethods(deps))
}

func RegisterUpdateMethods(d *Dispatcher, deps UpdateDeps) {
	d.RegisterDomain(handlersystem.UpdateMethods(deps))
}

func RegisterUsageMethods(d *Dispatcher, deps UsageDeps) {
	d.RegisterDomain(handlersystem.UsageMethods(deps))
}

func RegisterLogsMethods(d *Dispatcher, deps LogsDeps) {
	d.RegisterDomain(handlersystem.LogsMethods(deps))
}

func RegisterConfigReloadMethod(d *Dispatcher, deps ConfigReloadDeps) {
	d.RegisterDomain(handlersystem.ConfigReloadMethods(deps))
}

func RegisterConfigAdvancedMethods(d *Dispatcher, deps ConfigAdvancedDeps) {
	d.RegisterDomain(handlersystem.ConfigAdvancedMethods(deps))
}

func RegisterIdentityMethods(d *Dispatcher, version string) {
	d.RegisterDomain(handlersystem.IdentityMethods(version))
}

func RegisterWizardMethods(d *Dispatcher, deps WizardDeps) {
	d.RegisterDomain(handlerplatform.WizardMethods(deps))
}

func RegisterSecretMethods(d *Dispatcher, deps SecretDeps) {
	d.RegisterDomain(handlerplatform.SecretMethods(deps))
}

func RegisterTalkMethods(d *Dispatcher, deps TalkDeps) {
	d.RegisterDomain(handlerplatform.TalkMethods(deps))
}

func RegisterHeartbeatMethods(d *Dispatcher, deps HeartbeatDeps) {
	d.RegisterDomain(handlerpresence.HeartbeatMethods(deps))
}

func RegisterPresenceMethods(d *Dispatcher, deps PresenceDeps) {
	d.RegisterDomain(handlerpresence.Methods(deps))
}

func RegisterVegaMethods(d *Dispatcher, deps VegaDeps) {
	d.RegisterDomain(handlerffi.VegaMethods(deps))
}

// RegisterBuiltinMethods registers the core Go-native RPC methods.
// Delegates to the FFI and session handler packages for the bulk of methods,
// while keeping health/channels/system in the rpc package.
func RegisterBuiltinMethods(d *Dispatcher, deps Deps) {
	// Health, sessions CRUD, channels, system — kept in rpc package (methods.go).
	registerCoreBuiltins(d, deps)

	// FFI-backed methods: protocol, security, media, parsing, memory, markdown,
	// compaction, context engine, ML.
	d.RegisterDomain(handlerffi.ProtocolMethods())
	d.RegisterDomain(handlerffi.SecurityMethods())
	d.RegisterDomain(handlerffi.MediaMethods())
	d.RegisterDomain(handlerffi.ParsingMethods())
	d.RegisterDomain(handlerffi.MemoryMethods())
	d.RegisterDomain(handlerffi.MarkdownMethods())
	d.RegisterDomain(handlerffi.CompactionMethods())
	d.RegisterDomain(handlerffi.ContextEngineMethods())
	d.RegisterDomain(handlerffi.MLMethods())

	// Tools catalog (static core tool definitions).
	d.RegisterDomain(handlerskill.CatalogMethods())
}
