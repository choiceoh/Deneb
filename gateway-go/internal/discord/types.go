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
	Token      string              `json:"token"`
	Intents    int                 `json:"intents"`
	Properties IdentifyProperties  `json:"properties"`
	Presence   *PresenceUpdate     `json:"presence,omitempty"`
}

// PresenceUpdate sets the bot's presence/activity on connect.
type PresenceUpdate struct {
	Status     string     `json:"status"` // "online", "idle", "dnd", "invisible"
	Activities []Activity `json:"activities,omitempty"`
}

// Activity represents a Discord activity (e.g., "Playing", "Watching").
type Activity struct {
	Name string `json:"name"`
	Type int    `json:"type"` // 0=Playing, 1=Streaming, 2=Listening, 3=Watching, 5=Competing
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

// --- Embed types ---

// Embed represents a Discord rich embed.
type Embed struct {
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	Color       int          `json:"color,omitempty"`
	Fields      []EmbedField `json:"fields,omitempty"`
	Footer      *EmbedFooter `json:"footer,omitempty"`
	Timestamp   string       `json:"timestamp,omitempty"` // ISO8601
}

// EmbedField represents a field in an embed.
type EmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

// EmbedFooter represents an embed footer.
type EmbedFooter struct {
	Text string `json:"text"`
}

// Embed color constants for semantic status coding.
const (
	ColorSuccess  = 0x2ECC71 // green — pass, complete
	ColorError    = 0xE74C3C // red — fail, error
	ColorInfo     = 0x3498DB // blue — informational, diff
	ColorProgress = 0xF39C12 // orange — in-progress
	ColorWarning  = 0xF1C40F // yellow — warning
)

// --- Component types (buttons, action rows) ---

// Component represents a Discord message component.
type Component struct {
	Type       int         `json:"type"`                        // 1=ActionRow, 2=Button
	Style      int         `json:"style,omitempty"`             // 1=Primary, 2=Secondary, 3=Success, 4=Danger
	Label      string      `json:"label,omitempty"`
	CustomID   string      `json:"custom_id,omitempty"`
	Disabled   bool        `json:"disabled,omitempty"`
	Components []Component `json:"components,omitempty"` // children for ActionRow
}

// Component type constants.
const (
	ComponentActionRow = 1
	ComponentButton    = 2
)

// Button style constants.
const (
	ButtonPrimary   = 1
	ButtonSecondary = 2
	ButtonSuccess   = 3
	ButtonDanger    = 4
)

// --- Interaction types ---

// Interaction represents a Discord interaction (button click, slash command).
type Interaction struct {
	ID        string          `json:"id"`
	Type      int             `json:"type"` // 2=Component, 3=ApplicationCommand
	Data      InteractionData `json:"data,omitempty"`
	Token     string          `json:"token"`
	ChannelID string          `json:"channel_id"`
	Message   *Message        `json:"message,omitempty"`
	Member    *struct {
		User *User `json:"user,omitempty"`
	} `json:"member,omitempty"`
}

// InteractionData holds interaction-specific data.
type InteractionData struct {
	CustomID      string `json:"custom_id,omitempty"`
	ComponentType int    `json:"component_type,omitempty"`
	Name          string `json:"name,omitempty"` // for slash commands
}

// InteractionResponse is sent in reply to an interaction.
type InteractionResponse struct {
	Type int                      `json:"type"` // 4=ChannelMessage, 6=DeferredUpdate, 7=UpdateMessage
	Data *InteractionResponseData `json:"data,omitempty"`
}

// InteractionResponseData is the data payload for an interaction response.
type InteractionResponseData struct {
	Content    string      `json:"content,omitempty"`
	Embeds     []Embed     `json:"embeds,omitempty"`
	Components []Component `json:"components,omitempty"`
	Flags      int         `json:"flags,omitempty"` // 64=Ephemeral
}

// Interaction response type constants.
const (
	InteractionResponseMessage       = 4
	InteractionResponseDeferredUpdate = 6
	InteractionResponseUpdateMessage = 7
)

// --- Message request/edit types ---

// SendMessageRequest is the request body for POST /channels/{id}/messages.
type SendMessageRequest struct {
	Content          string            `json:"content,omitempty"`
	Embeds           []Embed           `json:"embeds,omitempty"`
	Components       []Component       `json:"components,omitempty"`
	MessageReference *MessageReference `json:"message_reference,omitempty"`
	AllowedMentions  *AllowedMentions  `json:"allowed_mentions,omitempty"`
}

// EditMessageRequest is the request body for PATCH /channels/{id}/messages/{id}.
type EditMessageRequest struct {
	Content    *string     `json:"content,omitempty"`    // pointer to allow empty string
	Embeds     []Embed     `json:"embeds,omitempty"`
	Components []Component `json:"components,omitempty"`
}

// AllowedMentions controls which mentions ping users.
type AllowedMentions struct {
	Parse []string `json:"parse"`
}
