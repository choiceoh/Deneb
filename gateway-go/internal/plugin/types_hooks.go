// types_hooks.go — Hook event/context types for the plugin system.
// Mirrors src/plugins/types-hooks.ts (615 LOC).
//
// These types define the payloads and contexts for each hook point in the
// gateway lifecycle. They are used by the hook runner to provide strongly-typed
// event delivery.
package plugin

// AllPluginHookNames is the canonical list of all plugin hook names.
// Mirrors PLUGIN_HOOK_NAMES from types-hooks.ts.
var AllPluginHookNames = []HookName{
	HookBeforeModelResolve,
	HookBeforePromptBuild,
	HookBeforeAgentStart,
	HookLLMInput,
	HookLLMOutput,
	HookAgentEnd,
	HookBeforeCompaction,
	HookAfterCompaction,
	HookBeforeReset,
	HookInboundClaim,
	HookMessageReceived,
	HookMessageSending,
	HookMessageSent,
	HookBeforeToolCall,
	HookAfterToolCall,
	HookToolResultPersist,
	HookBeforeMessageWrite,
	HookSessionStart,
	HookSessionEnd,
	HookSubagentSpawning,
	HookSubagentDeliveryTarget,
	HookSubagentSpawned,
	HookSubagentEnded,
	HookGatewayStart,
	HookGatewayStop,
}

// PromptInjectionHookNames are hooks that can inject into prompts.
var PromptInjectionHookNames = []HookName{
	HookBeforePromptBuild,
	HookBeforeAgentStart,
}

// IsPluginHookName returns true if the given string is a valid hook name.
func IsPluginHookName(name string) bool {
	return ValidateHookName(HookName(name)) == nil
}

// IsPromptInjectionHookName returns true if the hook can inject into prompts.
func IsPromptInjectionHookName(name HookName) bool {
	for _, n := range PromptInjectionHookNames {
		if n == name {
			return true
		}
	}
	return false
}

// --- Agent context shared across agent hooks ---

// HookAgentContext provides agent-scoped context for hooks.
type HookAgentContext struct {
	AgentID         string `json:"agentId,omitempty"`
	SessionKey      string `json:"sessionKey,omitempty"`
	SessionID       string `json:"sessionId,omitempty"`
	WorkspaceDir    string `json:"workspaceDir,omitempty"`
	MessageProvider string `json:"messageProvider,omitempty"`
	// What initiated this agent run: "user", "heartbeat", "cron", or "memory".
	Trigger   string `json:"trigger,omitempty"`
	ChannelID string `json:"channelId,omitempty"`
}

// --- before_model_resolve ---

type HookBeforeModelResolveEvent struct {
	Prompt string `json:"prompt"`
}

// (Result type already exists as BeforeModelResolveResult in hook_runner.go)

// --- before_prompt_build ---

type HookBeforePromptBuildEvent struct {
	Prompt   string `json:"prompt"`
	Messages []any  `json:"messages"`
}

// PromptMutationResultFields are the fields that can be mutated by prompt hooks.
var PromptMutationResultFields = []string{
	"systemPrompt",
	"prependContext",
	"prependSystemContext",
	"appendSystemContext",
}

// --- before_agent_start (legacy compatibility: combines both phases) ---

type HookBeforeAgentStartEvent struct {
	Prompt   string `json:"prompt"`
	Messages []any  `json:"messages,omitempty"`
}

// StripPromptMutationFieldsFromLegacyResult removes prompt mutation fields
// from a before_agent_start result, returning only override fields.
func StripPromptMutationFieldsFromLegacyResult(result map[string]any) map[string]any {
	if result == nil {
		return nil
	}
	stripped := make(map[string]any, len(result))
	for k, v := range result {
		stripped[k] = v
	}
	for _, field := range PromptMutationResultFields {
		delete(stripped, field)
	}
	if len(stripped) == 0 {
		return nil
	}
	return stripped
}

// --- llm_input ---

type HookLLMInputEvent struct {
	RunID           string `json:"runId"`
	SessionID       string `json:"sessionId"`
	Provider        string `json:"provider"`
	Model           string `json:"model"`
	SystemPrompt    string `json:"systemPrompt,omitempty"`
	Prompt          string `json:"prompt"`
	HistoryMessages []any  `json:"historyMessages"`
	ImagesCount     int    `json:"imagesCount"`
}

// --- llm_output ---

type HookLLMOutputEvent struct {
	RunID          string           `json:"runId"`
	SessionID      string           `json:"sessionId"`
	Provider       string           `json:"provider"`
	Model          string           `json:"model"`
	AssistantTexts []string         `json:"assistantTexts"`
	LastAssistant  any              `json:"lastAssistant,omitempty"`
	Usage          *HookUsageInfo   `json:"usage,omitempty"`
}

type HookUsageInfo struct {
	Input      int `json:"input,omitempty"`
	Output     int `json:"output,omitempty"`
	CacheRead  int `json:"cacheRead,omitempty"`
	CacheWrite int `json:"cacheWrite,omitempty"`
	Total      int `json:"total,omitempty"`
}

// --- agent_end ---

type HookAgentEndEvent struct {
	Messages   []any  `json:"messages"`
	Success    bool   `json:"success"`
	Error      string `json:"error,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
}

// --- Compaction hooks ---

type HookBeforeCompactionEvent struct {
	MessageCount   int    `json:"messageCount"`
	CompactingCount int   `json:"compactingCount,omitempty"`
	TokenCount     int    `json:"tokenCount,omitempty"`
	Messages       []any  `json:"messages,omitempty"`
	SessionFile    string `json:"sessionFile,omitempty"`
}

type HookAfterCompactionEvent struct {
	MessageCount   int    `json:"messageCount"`
	TokenCount     int    `json:"tokenCount,omitempty"`
	CompactedCount int    `json:"compactedCount"`
	SessionFile    string `json:"sessionFile,omitempty"`
}

// --- before_reset ---

type HookBeforeResetEvent struct {
	SessionFile string `json:"sessionFile,omitempty"`
	Messages    []any  `json:"messages,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

// --- Message context ---

type HookMessageContext struct {
	ChannelID      string `json:"channelId"`
	AccountID      string `json:"accountId,omitempty"`
	ConversationID string `json:"conversationId,omitempty"`
}

// --- inbound_claim ---

type HookInboundClaimContext struct {
	ChannelID            string `json:"channelId"`
	AccountID            string `json:"accountId,omitempty"`
	ConversationID       string `json:"conversationId,omitempty"`
	ParentConversationID string `json:"parentConversationId,omitempty"`
	SenderID             string `json:"senderId,omitempty"`
	MessageID            string `json:"messageId,omitempty"`
}

type HookInboundClaimEvent struct {
	Content              string         `json:"content"`
	Body                 string         `json:"body,omitempty"`
	BodyForAgent         string         `json:"bodyForAgent,omitempty"`
	Transcript           string         `json:"transcript,omitempty"`
	Timestamp            int64          `json:"timestamp,omitempty"`
	Channel              string         `json:"channel"`
	AccountID            string         `json:"accountId,omitempty"`
	ConversationID       string         `json:"conversationId,omitempty"`
	ParentConversationID string         `json:"parentConversationId,omitempty"`
	SenderID             string         `json:"senderId,omitempty"`
	SenderName           string         `json:"senderName,omitempty"`
	SenderUsername       string         `json:"senderUsername,omitempty"`
	ThreadID             string         `json:"threadId,omitempty"`
	MessageID            string         `json:"messageId,omitempty"`
	IsGroup              bool           `json:"isGroup"`
	CommandAuthorized    bool           `json:"commandAuthorized,omitempty"`
	WasMentioned         bool           `json:"wasMentioned,omitempty"`
	Metadata             map[string]any `json:"metadata,omitempty"`
}

type HookInboundClaimResult struct {
	Handled bool `json:"handled"`
}

// --- message_received ---

type HookMessageReceivedEvent struct {
	From      string         `json:"from"`
	Content   string         `json:"content"`
	Timestamp int64          `json:"timestamp,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// --- message_sending ---

type HookMessageSendingEvent struct {
	To       string         `json:"to"`
	Content  string         `json:"content"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type HookMessageSendingResult struct {
	Content string `json:"content,omitempty"`
	Cancel  bool   `json:"cancel,omitempty"`
}

// --- message_sent ---

type HookMessageSentEvent struct {
	To      string `json:"to"`
	Content string `json:"content"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// --- Tool context ---

type HookToolContext struct {
	AgentID    string `json:"agentId,omitempty"`
	SessionKey string `json:"sessionKey,omitempty"`
	SessionID  string `json:"sessionId,omitempty"`
	RunID      string `json:"runId,omitempty"`
	ToolName   string `json:"toolName"`
	ToolCallID string `json:"toolCallId,omitempty"`
}

// --- before_tool_call ---

type HookBeforeToolCallEvent struct {
	ToolName   string         `json:"toolName"`
	Params     map[string]any `json:"params"`
	RunID      string         `json:"runId,omitempty"`
	ToolCallID string         `json:"toolCallId,omitempty"`
}

type HookBeforeToolCallResult struct {
	Params      map[string]any `json:"params,omitempty"`
	Block       bool           `json:"block,omitempty"`
	BlockReason string         `json:"blockReason,omitempty"`
}

// --- after_tool_call ---

type HookAfterToolCallEvent struct {
	ToolName   string         `json:"toolName"`
	Params     map[string]any `json:"params"`
	RunID      string         `json:"runId,omitempty"`
	ToolCallID string         `json:"toolCallId,omitempty"`
	Result     any            `json:"result,omitempty"`
	Error      string         `json:"error,omitempty"`
	DurationMs int64          `json:"durationMs,omitempty"`
}

// --- tool_result_persist ---

type HookToolResultPersistContext struct {
	AgentID    string `json:"agentId,omitempty"`
	SessionKey string `json:"sessionKey,omitempty"`
	ToolName   string `json:"toolName,omitempty"`
	ToolCallID string `json:"toolCallId,omitempty"`
}

type HookToolResultPersistEvent struct {
	ToolName    string         `json:"toolName,omitempty"`
	ToolCallID  string         `json:"toolCallId,omitempty"`
	Message     map[string]any `json:"message"`
	IsSynthetic bool           `json:"isSynthetic,omitempty"`
}

type HookToolResultPersistResult struct {
	Message map[string]any `json:"message,omitempty"`
}

// --- before_message_write ---

type HookBeforeMessageWriteEvent struct {
	Message    map[string]any `json:"message"`
	SessionKey string         `json:"sessionKey,omitempty"`
	AgentID    string         `json:"agentId,omitempty"`
}

type HookBeforeMessageWriteResult struct {
	Block   bool           `json:"block,omitempty"`
	Message map[string]any `json:"message,omitempty"`
}

// --- Session context ---

type HookSessionContext struct {
	AgentID    string `json:"agentId,omitempty"`
	SessionID  string `json:"sessionId"`
	SessionKey string `json:"sessionKey,omitempty"`
}

// --- session_start ---

type HookSessionStartEvent struct {
	SessionID  string `json:"sessionId"`
	SessionKey string `json:"sessionKey,omitempty"`
	ResumedFrom string `json:"resumedFrom,omitempty"`
}

// --- session_end ---

type HookSessionEndEvent struct {
	SessionID    string `json:"sessionId"`
	SessionKey   string `json:"sessionKey,omitempty"`
	MessageCount int    `json:"messageCount"`
	DurationMs   int64  `json:"durationMs,omitempty"`
}

// --- Subagent context ---

type HookSubagentContext struct {
	RunID               string `json:"runId,omitempty"`
	ChildSessionKey     string `json:"childSessionKey,omitempty"`
	RequesterSessionKey string `json:"requesterSessionKey,omitempty"`
}

// SubagentTargetKind identifies whether the target is a subagent or ACP.
type SubagentTargetKind string

const (
	SubagentTargetSubagent SubagentTargetKind = "subagent"
	SubagentTargetACP      SubagentTargetKind = "acp"
)

// SubagentSpawnRequester holds origin info for the requester.
type SubagentSpawnRequester struct {
	Channel   string `json:"channel,omitempty"`
	AccountID string `json:"accountId,omitempty"`
	To        string `json:"to,omitempty"`
	ThreadID  string `json:"threadId,omitempty"`
}

// --- subagent_spawning ---

type HookSubagentSpawningEvent struct {
	ChildSessionKey string                  `json:"childSessionKey"`
	AgentID         string                  `json:"agentId"`
	Label           string                  `json:"label,omitempty"`
	Mode            string                  `json:"mode"` // "run" or "session"
	Requester       *SubagentSpawnRequester `json:"requester,omitempty"`
	ThreadRequested bool                    `json:"threadRequested"`
}

// (Result type already exists as SubagentSpawningResult in hook_runner.go)

// --- subagent_delivery_target ---

type HookSubagentDeliveryTargetEvent struct {
	ChildSessionKey          string                  `json:"childSessionKey"`
	RequesterSessionKey      string                  `json:"requesterSessionKey"`
	RequesterOrigin          *SubagentSpawnRequester `json:"requesterOrigin,omitempty"`
	ChildRunID               string                  `json:"childRunId,omitempty"`
	SpawnMode                string                  `json:"spawnMode,omitempty"`
	ExpectsCompletionMessage bool                    `json:"expectsCompletionMessage"`
}

type HookSubagentDeliveryTargetResult struct {
	Origin *SubagentSpawnRequester `json:"origin,omitempty"`
}

// --- subagent_spawned ---

type HookSubagentSpawnedEvent struct {
	ChildSessionKey string                  `json:"childSessionKey"`
	AgentID         string                  `json:"agentId"`
	Label           string                  `json:"label,omitempty"`
	Mode            string                  `json:"mode"`
	Requester       *SubagentSpawnRequester `json:"requester,omitempty"`
	ThreadRequested bool                    `json:"threadRequested"`
	RunID           string                  `json:"runId"`
}

// --- subagent_ended ---

type HookSubagentEndedEvent struct {
	TargetSessionKey string             `json:"targetSessionKey"`
	TargetKind       SubagentTargetKind `json:"targetKind"`
	Reason           string             `json:"reason"`
	SendFarewell     bool               `json:"sendFarewell,omitempty"`
	AccountID        string             `json:"accountId,omitempty"`
	RunID            string             `json:"runId,omitempty"`
	EndedAt          int64              `json:"endedAt,omitempty"`
	Outcome          string             `json:"outcome,omitempty"` // "ok", "error", "timeout", "killed", "reset", "deleted"
	Error            string             `json:"error,omitempty"`
}

// --- Gateway context ---

type HookGatewayContext struct {
	Port int `json:"port,omitempty"`
}

// --- gateway_start ---

type HookGatewayStartEvent struct {
	Port int `json:"port"`
}

// --- gateway_stop ---

type HookGatewayStopEvent struct {
	Reason string `json:"reason,omitempty"`
}
