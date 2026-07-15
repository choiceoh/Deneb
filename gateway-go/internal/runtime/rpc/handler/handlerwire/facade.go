// Package handlerwire re-exports core RPC handler leaves for the composition root.
package handlerwire

import (
	handleragent "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/agent"
	handlerchat "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/chat"
	handlercheckpoint "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/checkpoint"
	handlerevents "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerevents"
	handlerprocess "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/process"
	handlerprovider "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/provider"
	handlersession "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/session"
	handlersystem "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/system"
)

// --- agent re-exports ---

type AgentExtendedDeps = handleragent.ExtendedDeps

var AgentExtendedMethods = handleragent.ExtendedMethods

// --- checkpoint re-exports ---

type CheckpointDeps = handlercheckpoint.Deps

var CheckpointMethods = handlercheckpoint.Methods

// --- events re-exports ---

type EventsDeps = handlerevents.EventsDeps

var (
	BroadcastMethods = handlerevents.BroadcastMethods
	EventsMethods    = handlerevents.EventsMethods
)

// --- process re-exports ---

type (
	ProcessACPDeps          = handlerprocess.ACPDeps
	ProcessCronAdvancedDeps = handlerprocess.CronAdvancedDeps
	ProcessCronServiceDeps  = handlerprocess.CronServiceDeps
)

var (
	ProcessACPMethods          = handlerprocess.ACPMethods
	ProcessCronAdvancedMethods = handlerprocess.CronAdvancedMethods
	ProcessCronServiceMethods  = handlerprocess.CronServiceMethods
)

// --- provider re-exports ---

type (
	ProviderDeps       = handlerprovider.Deps
	ProviderModelsDeps = handlerprovider.ModelsDeps
)

var (
	ProviderMethods       = handlerprovider.Methods
	ProviderModelsMethods = handlerprovider.ModelsMethods
)

// --- session re-exports ---

type (
	SessionDeps              = handlersession.Deps
	SessionExecDeps          = handlersession.ExecDeps
	SessionTranscriptDeleter = handlersession.TranscriptDeleter
)

var (
	SessionCRUDMethods = handlersession.CRUDMethods
	SessionExecMethods = handlersession.ExecMethods
	SessionMethods     = handlersession.Methods
)

// --- system re-exports ---

type (
	SystemConfigAdvancedDeps = handlersystem.ConfigAdvancedDeps
	SystemConfigReloadDeps   = handlersystem.ConfigReloadDeps
	SystemHealthDeps         = handlersystem.HealthDeps
	SystemLogsDeps           = handlersystem.LogsDeps
	SystemMaintenanceDeps    = handlersystem.MaintenanceDeps
	SystemMonitoringDeps     = handlersystem.MonitoringDeps
	SystemUpdateDeps         = handlersystem.UpdateDeps
	SystemUsageDeps          = handlersystem.UsageDeps
)

var (
	SystemConfigAdvancedMethods = handlersystem.ConfigAdvancedMethods
	SystemConfigReloadMethods   = handlersystem.ConfigReloadMethods
	SystemHealthMethods         = handlersystem.HealthMethods
	SystemIdentityMethods       = handlersystem.IdentityMethods
	SystemLogsMethods           = handlersystem.LogsMethods
	SystemMaintenanceMethods    = handlersystem.MaintenanceMethods
	SystemMonitoringMethods     = handlersystem.MonitoringMethods
	SystemUpdateMethods         = handlersystem.UpdateMethods
	SystemUsageMethods          = handlersystem.UsageMethods
)

// --- chat re-exports ---

type (
	ChatBtwDeps = handlerchat.BtwDeps
	ChatDeps    = handlerchat.Deps
)

var (
	ChatBtwMethods     = handlerchat.BtwMethods
	ChatMethods        = handlerchat.Methods
	ChatMiniappMethods = handlerchat.MiniappMethods
)
