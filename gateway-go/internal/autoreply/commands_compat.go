// commands_compat.go — Re-exports from the autoreply/handlers subpackage.
// TODO: Remove after all callers are updated to import autoreply/handlers directly.
package autoreply

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/handlers"
)

// --- commands.go ---

type CommandScope = handlers.CommandScope
type CommandCategory = handlers.CommandCategory
type CommandArgDefinition = handlers.CommandArgDefinition
type ChatCommandDefinition = handlers.ChatCommandDefinition
type NativeCommandSpec = handlers.NativeCommandSpec
type CommandArgs = handlers.CommandArgs
type CommandDetection = handlers.CommandDetection
type CommandRegistry = handlers.CommandRegistry

const (
	ScopeText   CommandScope = "text"
	ScopeNative CommandScope = "native"
	ScopeBoth   CommandScope = "both"
)

const (
	CategorySession    CommandCategory = "session"
	CategoryOptions    CommandCategory = "options"
	CategoryStatus     CommandCategory = "status"
	CategoryManagement CommandCategory = "management"
	CategoryMedia      CommandCategory = "media"
	CategoryTools      CommandCategory = "tools"
	CategoryDocks      CommandCategory = "docks"
)

var NewCommandRegistry = handlers.NewCommandRegistry
var HasInlineCommandTokens = handlers.HasInlineCommandTokens
var ParseCommandArgs = handlers.ParseCommandArgs
var BuildCommandText = handlers.BuildCommandText

// --- commands_types.go ---

type CommandContextFull = handlers.CommandContextFull
type HandleCommandsFullParams = handlers.HandleCommandsFullParams
type ElevatedState = handlers.ElevatedState
type ElevatedFailure = handlers.ElevatedFailure
type CommandHandlerFullResult = handlers.CommandHandlerFullResult
type CommandHandlerFull = handlers.CommandHandlerFull

// --- commands_handlers.go ---

type CommandHandler = handlers.CommandHandler
type CommandContext = handlers.CommandContext
type CommandDeps = handlers.CommandDeps
type StatusDeps = handlers.StatusDeps
type ProviderUsageStats = handlers.ProviderUsageStats
type ChannelHealthEntry = handlers.ChannelHealthEntry
type CommandResult = handlers.CommandResult
type BtwContext = handlers.BtwContext
type CommandRouter = handlers.CommandRouter
type McpServerStore = handlers.McpServerStore

var NewCommandRouter = handlers.NewCommandRouter

// --- commands_dispatch.go ---

type ResetCommandAction = handlers.ResetCommandAction
type SendPolicyFunc = handlers.SendPolicyFunc
type CommandDispatcher = handlers.CommandDispatcher

const ResetActionNew ResetCommandAction = "new"
const ResetActionReset ResetCommandAction = "reset"

var NewCommandDispatcher = handlers.NewCommandDispatcher
var IsResetCommand = handlers.IsResetCommand
var ParseResetCommand = handlers.ParseResetCommand

// --- commands_context_build.go ---

type InboundCommandContext = handlers.InboundCommandContext

var BuildInboundCommandContext = handlers.BuildInboundCommandContext

// --- commands_data.go ---

var BuiltinChatCommands = handlers.BuiltinChatCommands

// --- commands_session_store.go ---

type SessionEntry = handlers.SessionEntry
type SessionStore = handlers.SessionStore

var PersistSessionEntry = handlers.PersistSessionEntry
var PersistAbortTargetEntry = handlers.PersistAbortTargetEntry
var ClearSessionAbortCutoff = handlers.ClearSessionAbortCutoff
var SessionEntryHasAbortCutoff = handlers.SessionEntryHasAbortCutoff
var ShouldSkipBySessionAbortCutoff = handlers.ShouldSkipBySessionAbortCutoff

// --- commands_setunset.go ---

type SetUnsetKind = handlers.SetUnsetKind
type SetUnsetParseResult = handlers.SetUnsetParseResult
type SetUnsetCallbacks[T any] = handlers.SetUnsetCallbacks[T]
type SetUnsetSlashParams[T any] = handlers.SetUnsetSlashParams[T]

const (
	SetUnsetSet   SetUnsetKind = 0
	SetUnsetUnset SetUnsetKind = 1
	SetUnsetError SetUnsetKind = 2
)

var ParseSetUnsetCommand = handlers.ParseSetUnsetCommand
var ParseSetUnsetCommandAction = handlers.ParseSetUnsetCommandAction[any]
var ParseSlashCommandWithSetUnset = handlers.ParseSlashCommandWithSetUnset[any]

// --- commands_setunset_standard.go ---

type StandardSetUnsetAction = handlers.StandardSetUnsetAction
type StandardSetUnsetParams = handlers.StandardSetUnsetParams

var ParseStandardSetUnsetSlashCommand = handlers.ParseStandardSetUnsetSlashCommand

// --- commands_slash_parse.go ---

type SlashParseKind = handlers.SlashParseKind
type SlashCommandParseResult = handlers.SlashCommandParseResult
type ParsedSlashCommand = handlers.ParsedSlashCommand

const (
	SlashNoMatch  SlashParseKind = 0
	SlashEmpty    SlashParseKind = 1
	SlashInvalid  SlashParseKind = 2
	SlashParsed   SlashParseKind = 3
)

var ParseSlashCommandActionArgs = handlers.ParseSlashCommandActionArgs
var ParseSlashCommandOrNull = handlers.ParseSlashCommandOrNull

// --- config_value.go ---

type ConfigValueResult = handlers.ConfigValueResult

var ParseConfigValue = handlers.ParseConfigValue

// --- config_commands.go ---

type ConfigCommandAction = handlers.ConfigCommandAction
type ConfigCommand = handlers.ConfigCommand
type ConfigWriteAuthResult = handlers.ConfigWriteAuthResult
type ConfigWriteScope = handlers.ConfigWriteScope
type ConfigWriteTarget = handlers.ConfigWriteTarget

const (
	ConfigActionShow  ConfigCommandAction = "show"
	ConfigActionSet   ConfigCommandAction = "set"
	ConfigActionUnset ConfigCommandAction = "unset"
	ConfigActionError ConfigCommandAction = "error"
)

const (
	ConfigWriteTargetGlobal  ConfigWriteTarget = "global"
	ConfigWriteTargetChannel ConfigWriteTarget = "channel"
	ConfigWriteTargetAccount ConfigWriteTarget = "account"
)

var ParseConfigCommand = handlers.ParseConfigCommand
var AuthorizeConfigWrite = handlers.AuthorizeConfigWrite
var CanBypassConfigWritePolicy = handlers.CanBypassConfigWritePolicy

// --- debug_commands.go ---

type DebugCommand = handlers.DebugCommand

var ParseDebugCommand = handlers.ParseDebugCommand

// --- mcp_commands.go ---

type McpCommand = handlers.McpCommand

var ParseMcpCommand = handlers.ParseMcpCommand

// --- skill_commands.go ---

type SkillCommandSpec = handlers.SkillCommandSpec

var BuildSkillCommandDefinitions = handlers.BuildSkillCommandDefinitions
var ResolveSkillCommand = handlers.ResolveSkillCommand

// --- btw_command.go ---

var IsBtwRequestText = handlers.IsBtwRequestText
var ExtractBtwQuestion = handlers.ExtractBtwQuestion

// --- commands_plugins.go ---

type PluginCommandAction = handlers.PluginCommandAction
type PluginCommand = handlers.PluginCommand
type PluginRecord = handlers.PluginRecord
type PluginStatusReport = handlers.PluginStatusReport
type HandlePluginsCommandResult = handlers.HandlePluginsCommandResult

const (
	PluginActionList    PluginCommandAction = "list"
	PluginActionInspect PluginCommandAction = "inspect"
	PluginActionEnable  PluginCommandAction = "enable"
	PluginActionDisable PluginCommandAction = "disable"
	PluginActionError   PluginCommandAction = "error"
)

var ParsePluginsCommand = handlers.ParsePluginsCommand
var FormatPluginLabel = handlers.FormatPluginLabel
var FormatPluginsList = handlers.FormatPluginsList
var FindPlugin = handlers.FindPlugin
var RenderJSONBlock = handlers.RenderJSONBlock
var HandlePluginsCommand = handlers.HandlePluginsCommand

// --- commands_plugin_match.go ---

type PluginCommandMatch = handlers.PluginCommandMatch

var MatchPluginCommand = handlers.MatchPluginCommand
var ExecutePluginCommand = handlers.ExecutePluginCommand
var HandlePluginCommandInPipeline = handlers.HandlePluginCommandInPipeline

// --- commands_subagents_shared.go ---

type SubagentsAction = handlers.SubagentsAction
type SubagentRunRecord = handlers.SubagentRunRecord
type SubagentsCommandContext = handlers.SubagentsCommandContext
type SubagentCommandResult = handlers.SubagentCommandResult
type SubagentListItem = handlers.SubagentListItem
type ResolvedSubagentController = handlers.ResolvedSubagentController

const (
	SubagentsActionList    SubagentsAction = "list"
	SubagentsActionKill    SubagentsAction = "kill"
	SubagentsActionLog     SubagentsAction = "log"
	SubagentsActionSend    SubagentsAction = "send"
	SubagentsActionSteer   SubagentsAction = "steer"
	SubagentsActionInfo    SubagentsAction = "info"
	SubagentsActionSpawn   SubagentsAction = "spawn"
	SubagentsActionFocus   SubagentsAction = "focus"
	SubagentsActionUnfocus SubagentsAction = "unfocus"
	SubagentsActionAgents  SubagentsAction = "agents"
	SubagentsActionHelp    SubagentsAction = "help"
)

const (
	SubagentsCmdPrefix  = handlers.SubagentsCmdPrefix
	SubagentsCmdKill    = handlers.SubagentsCmdKill
	SubagentsCmdSteer   = handlers.SubagentsCmdSteer
	SubagentsCmdTell    = handlers.SubagentsCmdTell
	SubagentsCmdFocus   = handlers.SubagentsCmdFocus
	SubagentsCmdUnfocus = handlers.SubagentsCmdUnfocus
	SubagentsCmdAgents  = handlers.SubagentsCmdAgents
)

const RecentWindowMinutes = handlers.RecentWindowMinutes

var ResolveHandledPrefix = handlers.ResolveHandledPrefix
var ResolveSubagentsAction = handlers.ResolveSubagentsAction
var FormatRunLabel = handlers.FormatRunLabel
var FormatRunStatus = handlers.FormatRunStatus
var ResolveDisplayStatus = handlers.ResolveDisplayStatus
var FormatDurationCompact = handlers.FormatDurationCompact
var SortSubagentRuns = handlers.SortSubagentRuns
var TruncateLine = handlers.TruncateLine
var ResolveSubagentTarget = handlers.ResolveSubagentTarget
var BuildSubagentsHelp = handlers.BuildSubagentsHelp

// --- commands_subagents.go ---

type SubagentCommandDeps = handlers.SubagentCommandDeps

var HandleSubagentsCommand = handlers.HandleSubagentsCommand

// --- commands_subagents_actions.go ---

type BuildSubagentListResult = handlers.BuildSubagentListResult
type SubagentKillDeps = handlers.SubagentKillDeps
type SubagentLogDeps = handlers.SubagentLogDeps
type ChatLogMessage = handlers.ChatLogMessage
type SubagentSendDeps = handlers.SubagentSendDeps
type SubagentSendResult = handlers.SubagentSendResult
type SubagentSteerResult = handlers.SubagentSteerResult
type SubagentSpawnDeps = handlers.SubagentSpawnDeps
type SubagentSpawnParams = handlers.SubagentSpawnParams
type SubagentSpawnContext = handlers.SubagentSpawnContext
type SubagentSpawnResult = handlers.SubagentSpawnResult
type SubagentFocusDeps = handlers.SubagentFocusDeps
type SubagentUnfocusDeps = handlers.SubagentUnfocusDeps
type SubagentAgentsDeps = handlers.SubagentAgentsDeps

var BuildSubagentList = handlers.BuildSubagentList
var HandleSubagentsListAction = handlers.HandleSubagentsListAction
var HandleSubagentsKillAction = handlers.HandleSubagentsKillAction
var HandleSubagentsInfoAction = handlers.HandleSubagentsInfoAction
var HandleSubagentsLogAction = handlers.HandleSubagentsLogAction
var HandleSubagentsSendAction = handlers.HandleSubagentsSendAction
var HandleSubagentsSpawnAction = handlers.HandleSubagentsSpawnAction
var HandleSubagentsFocusAction = handlers.HandleSubagentsFocusAction
var HandleSubagentsUnfocusAction = handlers.HandleSubagentsUnfocusAction
var HandleSubagentsAgentsAction = handlers.HandleSubagentsAgentsAction
var HandleSubagentsHelpAction = handlers.HandleSubagentsHelpAction

// --- commands_subagents_acp.go ---

type ACPCommandDepsConfig = handlers.ACPCommandDepsConfig
type ACPSubagentCommandHandler = handlers.ACPSubagentCommandHandler

var NewSubagentCommandDepsFromACP = handlers.NewSubagentCommandDepsFromACP
var NewACPSubagentCommandHandler = handlers.NewACPSubagentCommandHandler
var RegisterACPSubagentRPC = handlers.RegisterACPSubagentRPC
var FormatACPSubagentSummary = handlers.FormatACPSubagentSummary
var PruneStaleACPAgents = handlers.PruneStaleACPAgents

// --- status.go ---

type StatusReport = handlers.StatusReport

var BuildStatusMessage = handlers.BuildStatusMessage
var BuildHelpMessage = handlers.BuildHelpMessage
var FormatTokenCount = handlers.FormatTokenCount
var FormatContextUsageShort = handlers.FormatContextUsageShort
var BuildCommandsMessage = handlers.BuildCommandsMessage

// --- subagents_utils.go ---

type SubagentRunListEntry = handlers.SubagentRunListEntry

var BuildSubagentRunListEntries = handlers.BuildSubagentRunListEntries
var ResolveSubagentEntryForToken = handlers.ResolveSubagentEntryForToken
var FormatSubagentInfo = handlers.FormatSubagentInfo

// --- commands_root_types.go (AllowlistMatcher/BashCommandConfig moved to commands/) ---
// SessionUsage and AbortCutoffContext are aliased from session/ via session_compat.go
// and are not re-exported here to avoid duplicate declarations.

type AllowlistEntry = handlers.AllowlistEntry
type AllowlistMatcher = handlers.AllowlistMatcher
type BashCommandConfig = handlers.BashCommandConfig

var NewAllowlistMatcher = handlers.NewAllowlistMatcher
var DefaultBashConfig = handlers.DefaultBashConfig
var ValidateBashCommand = handlers.ValidateBashCommand
var ElevatedUnavailableMessage = handlers.ElevatedUnavailableMessage
