// Package discord implements a Discord Bot channel using direct HTTP/WebSocket calls.
//
// Only the types and methods needed for Deneb's single-user coding channel are
// included. This is not a general-purpose Discord library.
package discord

import "encoding/json"

// Discord API constants.
const (
	// BaseURL is the Discord REST API base URL.
	BaseURL = "https://discord.com/api/v10"

	// GatewayURL is the Discord Gateway WebSocket URL.
	GatewayURL = "wss://gateway.discord.gg/?v=10&encoding=json"

	// TextChunkLimit is the conservative chunk size for outbound messages.
	// Discord's hard limit is 2000 chars; we leave headroom for code block wrappers.
	TextChunkLimit = 1900

	// MaxFileSize is the maximum file upload size (25 MB for non-boosted servers).
	MaxFileSize = 25 * 1024 * 1024

	// HeartbeatJitterMs is the jitter applied to heartbeat intervals.
	HeartbeatJitterMs = 500
)

// Gateway opcodes.
const (
	OpcodeDispatch        = 0
	OpcodeHeartbeat       = 1
	OpcodeIdentify        = 2
	OpcodeResume          = 6
	OpcodeReconnect       = 7
	OpcodeInvalidSession  = 9
	OpcodeHello           = 10
	OpcodeHeartbeatAck    = 11
)

// Gateway intents.
const (
	IntentGuilds         = 1 << 0
	IntentGuildMessages  = 1 << 9
	IntentMessageContent = 1 << 15
	IntentDirectMessages = 1 << 12
)

// --- Discord API types ---

// GatewayPayload is the envelope for all Gateway messages.
type GatewayPayload struct {
	Op   int              `json:"op"`
	D    json.RawMessage  `json:"d,omitempty"`
	S    *int64           `json:"s,omitempty"`
	T    string           `json:"t,omitempty"`
}

// HelloData is the payload for opcode 10 (Hello).
type HelloData struct {
	HeartbeatInterval int `json:"heartbeat_interval"`
}

// IdentifyData is the payload for opcode 2 (Identify).
type IdentifyData struct {
	Token      string            `json:"token"`
	Intents    int               `json:"intents"`
	Properties IdentifyProperties `json:"properties"`
}

// IdentifyProperties contains OS/browser/device info for identification.
type IdentifyProperties struct {
	OS      string `json:"os"`
	Browser string `json:"browser"`
	Device  string `json:"device"`
}

// ResumeData is the payload for opcode 6 (Resume).
type ResumeData struct {
	Token     string `json:"token"`
	SessionID string `json:"session_id"`
	Seq       int64  `json:"seq"`
}

// ReadyData is the payload for the READY dispatch event.
type ReadyData struct {
	SessionID string `json:"session_id"`
	ResumeURL string `json:"resume_gateway_url"`
	User      *User  `json:"user"`
}

// User represents a Discord user.
type User struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	Discriminator string `json:"discriminator"`
	GlobalName    string `json:"global_name,omitempty"`
	Bot           bool   `json:"bot,omitempty"`
}

// Message represents a Discord message.
type Message struct {
	ID        string   `json:"id"`
	ChannelID string   `json:"channel_id"`
	GuildID   string   `json:"guild_id,omitempty"`
	Author    *User    `json:"author,omitempty"`
	Content   string   `json:"content"`
	Timestamp string   `json:"timestamp,omitempty"`
	// Thread is present when the message is in a thread.
	Thread    *Channel `json:"thread,omitempty"`
	// MessageReference is present when this message is a reply.
	MessageReference *MessageReference `json:"message_reference,omitempty"`
	// Attachments on the message.
	Attachments []Attachment `json:"attachments,omitempty"`
}

// MessageReference represents a reference to another message (reply).
type MessageReference struct {
	MessageID string `json:"message_id,omitempty"`
	ChannelID string `json:"channel_id,omitempty"`
	GuildID   string `json:"guild_id,omitempty"`
}

// Attachment represents a file attached to a message.
type Attachment struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	Size     int    `json:"size"`
	URL      string `json:"url"`
	ProxyURL string `json:"proxy_url,omitempty"`
}

// Channel represents a Discord channel.
type Channel struct {
	ID       string `json:"id"`
	GuildID  string `json:"guild_id,omitempty"`
	Name     string `json:"name,omitempty"`
	Type     int    `json:"type"`
	ParentID string `json:"parent_id,omitempty"`
}

// Emoji represents a Discord emoji for reactions.
type Emoji struct {
	Name string `json:"name"`
	ID   string `json:"id,omitempty"`
}

// APIResponse wraps Discord REST API responses.
type APIResponse struct {
	Code    int    `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// SendMessageRequest is the request body for POST /channels/{id}/messages.
type SendMessageRequest struct {
	Content          string            `json:"content,omitempty"`
	MessageReference *MessageReference `json:"message_reference,omitempty"`
	// AllowedMentions controls ping behavior.
	AllowedMentions *AllowedMentions `json:"allowed_mentions,omitempty"`
}

// AllowedMentions controls which mentions ping users.
type AllowedMentions struct {
	Parse []string `json:"parse"`
}
