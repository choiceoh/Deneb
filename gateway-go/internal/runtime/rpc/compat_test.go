package rpc

// Test-only type aliases, registration wrappers, and helper shims.
// Production code (server/*.go) imports handler packages directly.

import (
	"encoding/json"
	handleragent "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/agent"
	handlerchat "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/chat"
	handlergateway "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/gateway"
	handlerevents "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerevents"
	handlertelegram "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlertelegram"
	handlerplatform "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/platform"
	handlerprocess "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/process"
	handlerprovider "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/provider"
	handlersession "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/session"
	handlerskill "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/skill"
	handlersystem "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/system"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
)

// --- Type aliases ---

type ChatDeps = handlerchat.Deps
type ChatBtwDeps = handlerchat.BtwDeps
type SessionExecDeps = handlersession.ExecDeps
type ChannelLifecycleDeps = handlertelegram.LifecycleDeps
type EventsDeps = handlerevents.EventsDeps
type ApprovalDeps = handlerprocess.ApprovalDeps
type ACPDeps = handlerprocess.ACPDeps
type CronAdvancedDeps = handlerprocess.CronAdvancedDeps
type CronServiceDeps = handlerprocess.CronServiceDeps
type ProviderDeps = handlerprovider.Deps
type ModelsDeps = handlerprovider.ModelsDeps
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
type SecretDeps = handlerplatform.SecretDeps
type ExtendedDeps = handleragent.ExtendedDeps
type AgentsDeps = handleragent.AgentsDeps
type SessionDeps = handlersession.Deps
type GatewayRuntimeDeps = handlergateway.Deps

// --- Registration wrappers ---

func RegisterChatMethods(d *Dispatcher, deps ChatDeps) { d.RegisterDomain(handlerchat.Methods(deps)) }
func RegisterChatBtwMethods(d *Dispatcher, deps ChatBtwDeps) {
	d.RegisterDomain(handlerchat.BtwMethods(deps))
}
func RegisterSessionMethods(d *Dispatcher, deps SessionDeps) {
	d.RegisterDomain(handlersession.Methods(deps))
}
func RegisterSessionRepairMethods(d *Dispatcher, _ SessionDeps) {} // no-op
func RegisterSessionExecMethods(d *Dispatcher, deps SessionExecDeps) {
	d.RegisterDomain(handlersession.ExecMethods(deps))
}
func RegisterExtendedMethods(d *Dispatcher, deps ExtendedDeps) {
	d.RegisterDomain(handleragent.ExtendedMethods(deps))
}
func RegisterAgentsMethods(d *Dispatcher, deps AgentsDeps) {
	d.RegisterDomain(handleragent.CRUDMethods(deps))
}
func RegisterChannelLifecycleMethods(d *Dispatcher, deps ChannelLifecycleDeps) {
	d.RegisterDomain(handlertelegram.LifecycleMethods(deps))
}
func RegisterEventsMethods(d *Dispatcher, deps EventsDeps) {
	d.RegisterDomain(handlerevents.EventsMethods(deps))
}
func RegisterEventBroadcastMethods(d *Dispatcher, deps EventsDeps) {
	d.RegisterDomain(handlerevents.BroadcastMethods(deps))
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
func RegisterCronServiceMethods(d *Dispatcher, deps CronServiceDeps) {
	d.RegisterDomain(handlerprocess.CronServiceMethods(deps))
}
func RegisterProviderMethods(d *Dispatcher, deps ProviderDeps) {
	d.RegisterDomain(handlerprovider.Methods(deps))
}
func RegisterModelsMethods(d *Dispatcher, deps ModelsDeps) {
	d.RegisterDomain(handlerprovider.ModelsMethods(deps))
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
func RegisterSecretMethods(d *Dispatcher, deps SecretDeps) {
	d.RegisterDomain(handlerplatform.SecretMethods(deps))
}
func RegisterGatewayRuntimeMethods(d *Dispatcher, deps GatewayRuntimeDeps) {
	d.RegisterDomain(handlergateway.RuntimeMethods(deps))
}

// --- Private helper shims for test compatibility (C2: rpcutil delegation) ---

const maxKeyInErrorMsg = rpcutil.MaxKeyInErrorMsg

func unmarshalParams(params json.RawMessage, v any) error {
	return rpcutil.UnmarshalParams(params, v)
}

func truncateForError(s string) string {
	return rpcutil.TruncateForError(s)
}
