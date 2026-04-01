// Package mcp implements an MCP (Model Context Protocol) server that bridges
// Claude Desktop to the Deneb gateway. It exposes Deneb's RPC methods as MCP
// tools, resources, and prompts over stdio JSON-RPC 2.0.
package mcp

import "encoding/json"

// MCP protocol version.
const ProtocolVersion = "2024-11-05"

// --- JSON-RPC 2.0 ---

// JSONRPCRequest is a JSON-RPC 2.0 request or notification.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"` // null for notifications
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// IsNotification returns true if the request has no ID (notification).
func (r *JSONRPCRequest) IsNotification() bool {
	return len(r.ID) == 0 || string(r.ID) == "null"
}

// JSONRPCResponse is a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError is a JSON-RPC 2.0 error object.
type JSONRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Standard JSON-RPC error codes.
const (
	ErrCodeParse          = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternal       = -32603
)

// --- MCP Initialize ---

// InitializeParams is the params for the initialize request.
type InitializeParams struct {
	ProtocolVersion string     `json:"protocolVersion"`
	Capabilities    ClientCaps `json:"capabilities"`
	ClientInfo      ClientInfo `json:"clientInfo"`
}

// ClientCaps describes capabilities the client supports.
type ClientCaps struct {
	Sampling *json.RawMessage `json:"sampling,omitempty"`
	Roots    *json.RawMessage `json:"roots,omitempty"`
}

// ClientInfo identifies the MCP client.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// InitializeResult is the result for the initialize response.
type InitializeResult struct {
	ProtocolVersion string     `json:"protocolVersion"`
	Capabilities    ServerCaps `json:"capabilities"`
	ServerInfo      ServerInfo `json:"serverInfo"`
}

// ServerCaps declares server capabilities.
type ServerCaps struct {
	Tools     *ToolsCap     `json:"tools,omitempty"`
	Resources *ResourcesCap `json:"resources,omitempty"`
	Prompts   *PromptsCap   `json:"prompts,omitempty"`
	Sampling  *SamplingCap  `json:"sampling,omitempty"`
}

// ToolsCap indicates the server supports tools.
type ToolsCap struct{}

// ResourcesCap indicates the server supports resources.
type ResourcesCap struct {
	Subscribe bool `json:"subscribe,omitempty"`
}

// PromptsCap indicates the server supports prompts.
type PromptsCap struct{}

// SamplingCap indicates the server can send sampling requests.
type SamplingCap struct{}

// ServerInfo identifies the MCP server.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// --- MCP Tools ---

// Tool describes an MCP tool.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema"`
}

// ToolsListResult is the result for tools/list.
type ToolsListResult struct {
	Tools []Tool `json:"tools"`
}

// ToolCallParams is the params for tools/call.
type ToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// ToolCallResult is the result for tools/call.
type ToolCallResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// ContentBlock is a content item in MCP results.
type ContentBlock struct {
	Type     string `json:"type"` // "text" or "image"
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`     // base64 for image
	MimeType string `json:"mimeType,omitempty"` // for image
}

// TextContent creates a text content block.
func TextContent(text string) ContentBlock {
	return ContentBlock{Type: "text", Text: text}
}

// --- MCP Resources ---

// Resource describes an MCP resource.
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// ResourcesListResult is the result for resources/list.
type ResourcesListResult struct {
	Resources []Resource `json:"resources"`
}

// ResourceReadParams is the params for resources/read.
type ResourceReadParams struct {
	URI string `json:"uri"`
}

// ResourceReadResult is the result for resources/read.
type ResourceReadResult struct {
	Contents []ResourceContent `json:"contents"`
}

// ResourceContent is a content item in resource read results.
type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
}

// ResourceSubscribeParams is the params for resources/subscribe.
type ResourceSubscribeParams struct {
	URI string `json:"uri"`
}

// --- MCP Prompts ---

// Prompt describes an MCP prompt.
type Prompt struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Arguments   []PromptArg     `json:"arguments,omitempty"`
}

// PromptArg describes a prompt argument.
type PromptArg struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// PromptsListResult is the result for prompts/list.
type PromptsListResult struct {
	Prompts []Prompt `json:"prompts"`
}

// PromptGetParams is the params for prompts/get.
type PromptGetParams struct {
	Name      string            `json:"name"`
	Arguments map[string]string `json:"arguments,omitempty"`
}

// PromptGetResult is the result for prompts/get.
type PromptGetResult struct {
	Description string          `json:"description,omitempty"`
	Messages    []PromptMessage `json:"messages"`
}

// PromptMessage is a message in a prompt result.
type PromptMessage struct {
	Role    string       `json:"role"` // "user" or "assistant"
	Content ContentBlock `json:"content"`
}

// --- MCP Sampling (server → client) ---

// SamplingRequest is sent from server to client to request a completion.
type SamplingRequest struct {
	Messages         []SamplingMessage `json:"messages"`
	SystemPrompt     string            `json:"systemPrompt,omitempty"`
	MaxTokens        int               `json:"maxTokens"`
	IncludeContext   string            `json:"includeContext,omitempty"` // "none", "thisServer", "allServers"
	ModelPreferences *ModelPrefs       `json:"modelPreferences,omitempty"`
}

// SamplingMessage is a message in a sampling request.
type SamplingMessage struct {
	Role    string       `json:"role"`
	Content ContentBlock `json:"content"`
}

// ModelPrefs describes model preferences for sampling.
type ModelPrefs struct {
	CostPriority         float64 `json:"costPriority,omitempty"`
	SpeedPriority        float64 `json:"speedPriority,omitempty"`
	IntelligencePriority float64 `json:"intelligencePriority,omitempty"`
}

// SamplingResult is the response from the client for a sampling request.
type SamplingResult struct {
	Role    string       `json:"role"`
	Content ContentBlock `json:"content"`
	Model   string       `json:"model"`
}

// --- Notifications ---

// Notification is a JSON-RPC notification (no ID).
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// ResourceUpdatedParams is the params for notifications/resources/updated.
type ResourceUpdatedParams struct {
	URI string `json:"uri"`
}
