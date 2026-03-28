// acp_compat.go — temporary re-exports from the autoreply/acp subpackage.
// TODO: Remove after all callers are updated to import autoreply/acp directly.
package autoreply

import "github.com/choiceoh/deneb/gateway-go/internal/autoreply/acp"

// Type aliases — acp.go
type ACPAgent = acp.ACPAgent
type ACPRegistry = acp.ACPRegistry
type ACPTurnResult = acp.ACPTurnResult
type ACPTokenUsage = acp.ACPTokenUsage
type ACPProjector = acp.ACPProjector
type SubagentListEntry = acp.SubagentListEntry

// Type aliases — acp_persistence.go
type BindingStoreFile = acp.BindingStoreFile
type BindingStore = acp.BindingStore

// Type aliases — acp_translator.go
type ACPPromptInput = acp.ACPPromptInput
type ACPResource = acp.ACPResource
type ACPEventOutput = acp.ACPEventOutput
type ACPDispatchConfig = acp.ACPDispatchConfig
type ACPDispatch = acp.ACPDispatch
type ACPDelivery = acp.ACPDelivery
type ACPTranslator = acp.ACPTranslator

// Type aliases — subagent_deps.go
type SubagentInfraDeps = acp.SubagentInfraDeps
type SpawnSubagentParams = acp.SpawnSubagentParams
type SpawnSubagentResult = acp.SpawnSubagentResult

// Type aliases — bindings.go
type SessionBindParams = acp.SessionBindParams
type SessionBindResult = acp.SessionBindResult
type SessionBindingEntry = acp.SessionBindingEntry
type AgentBindingEntry = acp.AgentBindingEntry
type StoredBinding = acp.StoredBinding
type SessionBindingService = acp.SessionBindingService

// Const re-exports
const DefaultACPDir = acp.DefaultACPDir
const ACPSessionPrefix = acp.ACPSessionPrefix

// Function re-exports — acp.go
var NewACPRegistry = acp.NewACPRegistry
var NewACPProjector = acp.NewACPProjector
var StartACPLifecycleSync = acp.StartACPLifecycleSync
var FormatSubagentList = acp.FormatSubagentList

// Function re-exports — acp_persistence.go
var DefaultBindingStorePath = acp.DefaultBindingStorePath
var NewBindingStore = acp.NewBindingStore

// Function re-exports — acp_translator.go
var NewACPTranslator = acp.NewACPTranslator
var IsACPSession = acp.IsACPSession
var TranslateStopReason = acp.TranslateStopReason
var TranslateACPStopReasonToStatus = acp.TranslateACPStopReasonToStatus
var BuildACPDispatch = acp.BuildACPDispatch
var BuildACPDelivery = acp.BuildACPDelivery

// Function re-exports — bindings.go
var NewSessionBindingService = acp.NewSessionBindingService
