// commands_compat.go — Re-exports from the autoreply/commands subpackage.
// TODO: Remove after all callers are updated to import autoreply/commands directly.
package autoreply

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/commands"
)

// --- commands.go ---

type CommandScope = commands.CommandScope
type CommandCategory = commands.CommandCategory
type CommandArgDefinition = commands.CommandArgDefinition
type ChatCommandDefinition = commands.ChatCommandDefinition
type NativeCommandSpec = commands.NativeCommandSpec
type CommandArgs = commands.CommandArgs
type CommandDetection = commands.CommandDetection
type CommandRegistry = commands.CommandRegistry

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

var NewCommandRegistry = commands.NewCommandRegistry
var HasInlineCommandTokens = commands.HasInlineCommandTokens
var ParseCommandArgs = commands.ParseCommandArgs
var BuildCommandText = commands.BuildCommandText

// --- commands_types.go ---

type CommandContextFull = commands.CommandContextFull
type HandleCommandsFullParams = commands.HandleCommandsFullParams
type ElevatedState = commands.ElevatedState
type ElevatedFailure = commands.ElevatedFailure
type CommandHandlerFullResult = commands.CommandHandlerFullResult
type CommandHandlerFull = commands.CommandHandlerFull

// --- commands_handlers.go ---

type CommandHandler = commands.CommandHandler
type CommandContext = commands.CommandContext
type CommandDeps = commands.CommandDeps
type StatusDeps = commands.StatusDeps
type ProviderUsageStats = commands.ProviderUsageStats
type ChannelHealthEntry = commands.ChannelHealthEntry
type CommandResult = commands.CommandResult
type BtwContext = commands.BtwContext
type CommandRouter = commands.CommandRouter
type McpServerStore = commands.McpServerStore

var NewCommandRouter = commands.NewCommandRouter

// --- commands_dispatch.go ---

type ResetCommandAction = commands.ResetCommandAction
type SendPolicyFunc = commands.SendPolicyFunc
type CommandDispatcher = commands.CommandDispatcher

const ResetActionNew ResetCommandAction = "new"
const ResetActionReset ResetCommandAction = "reset"

var NewCommandDispatcher = commands.NewCommandDispatcher
var IsResetCommand = commands.IsResetCommand
var ParseResetCommand = commands.ParseResetCommand

// --- commands_context_build.go ---

type InboundCommandContext = commands.InboundCommandContext

var BuildInboundCommandContext = commands.BuildInboundCommandContext

// --- commands_data.go ---

var BuiltinChatCommands = commands.BuiltinChatCommands

// --- commands_session_store.go ---

type SessionEntry = commands.SessionEntry
type SessionStore = commands.SessionStore

var PersistSessionEntry = commands.PersistSessionEntry
var PersistAbortTargetEntry = commands.PersistAbortTargetEntry
var ClearSessionAbortCutoff = commands.ClearSessionAbortCutoff
var SessionEntryHasAbortCutoff = commands.SessionEntryHasAbortCutoff
var ShouldSkipBySessionAbortCutoff = commands.ShouldSkipBySessionAbortCutoff

// --- commands_setunset.go ---

type SetUnsetKind = commands.SetUnsetKind
type SetUnsetParseResult = commands.SetUnsetParseResult
type SetUnsetCallbacks[T any] = commands.SetUnsetCallbacks[T]
type SetUnsetSlashParams[T any] = commands.SetUnsetSlashParams[T]

const (
	SetUnsetSet   SetUnsetKind = 0
	SetUnsetUnset SetUnsetKind = 1
	SetUnsetError SetUnsetKind = 2
)

var ParseSetUnsetCommand = commands.ParseSetUnsetCommand
var ParseSetUnsetCommandAction = commands.ParseSetUnsetCommandAction[any]
var ParseSlashCommandWithSetUnset = commands.ParseSlashCommandWithSetUnset[any]

// --- commands_setunset_standard.go ---

type StandardSetUnsetAction = commands.StandardSetUnsetAction
type StandardSetUnsetParams = commands.StandardSetUnsetParams

var ParseStandardSetUnsetSlashCommand = commands.ParseStandardSetUnsetSlashCommand

// --- commands_slash_parse.go ---

type SlashParseKind = commands.SlashParseKind
type SlashCommandParseResult = commands.SlashCommandParseResult
type ParsedSlashCommand = commands.ParsedSlashCommand

const (
	SlashNoMatch  SlashParseKind = 0
	SlashEmpty    SlashParseKind = 1
	SlashInvalid  SlashParseKind = 2
	SlashParsed   SlashParseKind = 3
)

var ParseSlashCommandActionArgs = commands.ParseSlashCommandActionArgs
var ParseSlashCommandOrNull = commands.ParseSlashCommandOrNull

// --- config_value.go ---

type ConfigValueResult = commands.ConfigValueResult

var ParseConfigValue = commands.ParseConfigValue

// --- config_commands.go ---

type ConfigCommandAction = commands.ConfigCommandAction
type ConfigCommand = commands.ConfigCommand
type ConfigWriteAuthResult = commands.ConfigWriteAuthResult
type ConfigWriteScope = commands.ConfigWriteScope
type ConfigWriteTarget = commands.ConfigWriteTarget

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

var ParseConfigCommand = commands.ParseConfigCommand
var AuthorizeConfigWrite = commands.AuthorizeConfigWrite
var CanBypassConfigWritePolicy = commands.CanBypassConfigWritePolicy

// --- debug_commands.go ---

type DebugCommand = commands.DebugCommand

var ParseDebugCommand = commands.ParseDebugCommand

// --- mcp_commands.go ---

type McpCommand = commands.McpCommand

var ParseMcpCommand = commands.ParseMcpCommand

// --- skill_commands.go ---

type SkillCommandSpec = commands.SkillCommandSpec

var BuildSkillCommandDefinitions = commands.BuildSkillCommandDefinitions
var ResolveSkillCommand = commands.ResolveSkillCommand

// --- btw_command.go ---

var IsBtwRequestText = commands.IsBtwRequestText
var ExtractBtwQuestion = commands.ExtractBtwQuestion

// --- commands_plugins.go ---

type PluginCommandAction = commands.PluginCommandAction
type PluginCommand = commands.PluginCommand
type PluginRecord = commands.PluginRecord
type PluginStatusReport = commands.PluginStatusReport
type HandlePluginsCommandResult = commands.HandlePluginsCommandResult

const (
	PluginActionList    PluginCommandAction = "list"
	PluginActionInspect PluginCommandAction = "inspect"
	PluginActionEnable  PluginCommandAction = "enable"
	PluginActionDisable PluginCommandAction = "disable"
	PluginActionError   PluginCommandAction = "error"
)

var ParsePluginsCommand = commands.ParsePluginsCommand
var FormatPluginLabel = commands.FormatPluginLabel
var FormatPluginsList = commands.FormatPluginsList
var FindPlugin = commands.FindPlugin
var RenderJSONBlock = commands.RenderJSONBlock
var HandlePluginsCommand = commands.HandlePluginsCommand

// --- commands_plugin_match.go ---

type PluginCommandMatch = commands.PluginCommandMatch

var MatchPluginCommand = commands.MatchPluginCommand
var ExecutePluginCommand = commands.ExecutePluginCommand
var HandlePluginCommandInPipeline = commands.HandlePluginCommandInPipeline

// --- commands_subagents_shared.go ---

type SubagentsAction = commands.SubagentsAction
type SubagentRunRecord = commands.SubagentRunRecord
type SubagentsCommandContext = commands.SubagentsCommandContext
type SubagentCommandResult = commands.SubagentCommandResult
type SubagentListItem = commands.SubagentListItem
type ResolvedSubagentController = commands.ResolvedSubagentController

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
	SubagentsCmdPrefix  = commands.SubagentsCmdPrefix
	SubagentsCmdKill    = commands.SubagentsCmdKill
	SubagentsCmdSteer   = commands.SubagentsCmdSteer
	SubagentsCmdTell    = commands.SubagentsCmdTell
	SubagentsCmdFocus   = commands.SubagentsCmdFocus
	SubagentsCmdUnfocus = commands.SubagentsCmdUnfocus
	SubagentsCmdAgents  = commands.SubagentsCmdAgents
)

const RecentWindowMinutes = commands.RecentWindowMinutes

var ResolveHandledPrefix = commands.ResolveHandledPrefix
var ResolveSubagentsAction = commands.ResolveSubagentsAction
var FormatRunLabel = commands.FormatRunLabel
var FormatRunStatus = commands.FormatRunStatus
var ResolveDisplayStatus = commands.ResolveDisplayStatus
var FormatDurationCompact = commands.FormatDurationCompact
var SortSubagentRuns = commands.SortSubagentRuns
var TruncateLine = commands.TruncateLine
var ResolveSubagentTarget = commands.ResolveSubagentTarget
var BuildSubagentsHelp = commands.BuildSubagentsHelp

// --- commands_subagents.go ---

type SubagentCommandDeps = commands.SubagentCommandDeps

var HandleSubagentsCommand = commands.HandleSubagentsCommand

// --- commands_subagents_actions.go ---

type BuildSubagentListResult = commands.BuildSubagentListResult
type SubagentKillDeps = commands.SubagentKillDeps
type SubagentLogDeps = commands.SubagentLogDeps
type ChatLogMessage = commands.ChatLogMessage
type SubagentSendDeps = commands.SubagentSendDeps
type SubagentSendResult = commands.SubagentSendResult
type SubagentSteerResult = commands.SubagentSteerResult
type SubagentSpawnDeps = commands.SubagentSpawnDeps
type SubagentSpawnParams = commands.SubagentSpawnParams
type SubagentSpawnContext = commands.SubagentSpawnContext
type SubagentSpawnResult = commands.SubagentSpawnResult
type SubagentFocusDeps = commands.SubagentFocusDeps
type SubagentUnfocusDeps = commands.SubagentUnfocusDeps
type SubagentAgentsDeps = commands.SubagentAgentsDeps

var BuildSubagentList = commands.BuildSubagentList
var HandleSubagentsListAction = commands.HandleSubagentsListAction
var HandleSubagentsKillAction = commands.HandleSubagentsKillAction
var HandleSubagentsInfoAction = commands.HandleSubagentsInfoAction
var HandleSubagentsLogAction = commands.HandleSubagentsLogAction
var HandleSubagentsSendAction = commands.HandleSubagentsSendAction
var HandleSubagentsSpawnAction = commands.HandleSubagentsSpawnAction
var HandleSubagentsFocusAction = commands.HandleSubagentsFocusAction
var HandleSubagentsUnfocusAction = commands.HandleSubagentsUnfocusAction
var HandleSubagentsAgentsAction = commands.HandleSubagentsAgentsAction
var HandleSubagentsHelpAction = commands.HandleSubagentsHelpAction

// --- commands_subagents_acp.go ---

type ACPCommandDepsConfig = commands.ACPCommandDepsConfig
type ACPSubagentCommandHandler = commands.ACPSubagentCommandHandler

var NewSubagentCommandDepsFromACP = commands.NewSubagentCommandDepsFromACP
var NewACPSubagentCommandHandler = commands.NewACPSubagentCommandHandler
var RegisterACPSubagentRPC = commands.RegisterACPSubagentRPC
var FormatACPSubagentSummary = commands.FormatACPSubagentSummary
var PruneStaleACPAgents = commands.PruneStaleACPAgents

// --- status.go ---

type StatusReport = commands.StatusReport

var BuildStatusMessage = commands.BuildStatusMessage
var BuildHelpMessage = commands.BuildHelpMessage
var FormatTokenCount = commands.FormatTokenCount
var FormatContextUsageShort = commands.FormatContextUsageShort
var BuildCommandsMessage = commands.BuildCommandsMessage

// --- subagents_utils.go ---

type SubagentRunListEntry = commands.SubagentRunListEntry

var BuildSubagentRunListEntries = commands.BuildSubagentRunListEntries
var ResolveSubagentEntryForToken = commands.ResolveSubagentEntryForToken
var FormatSubagentInfo = commands.FormatSubagentInfo

// --- commands_root_types.go (AllowlistMatcher/BashCommandConfig moved to commands/) ---
// SessionUsage and AbortCutoffContext are aliased from session/ via session_compat.go
// and are not re-exported here to avoid duplicate declarations.

type AllowlistEntry = commands.AllowlistEntry
type AllowlistMatcher = commands.AllowlistMatcher
type BashCommandConfig = commands.BashCommandConfig

var NewAllowlistMatcher = commands.NewAllowlistMatcher
var DefaultBashConfig = commands.DefaultBashConfig
var ValidateBashCommand = commands.ValidateBashCommand
var ElevatedUnavailableMessage = commands.ElevatedUnavailableMessage
