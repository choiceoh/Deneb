// Package telegram implements a Telegram Bot API client using direct HTTP calls.
//
// Only the types and methods needed for Deneb's single-user Telegram deployment
// are included. This is not a general-purpose Telegram library.
package telegram

import "encoding/json"

// --- Telegram Bot API constants ---

const (
	// TextChunkLimit is the conservative chunk size for outbound messages.
	// Telegram's hard limit is 4096 chars; we leave headroom for HTML overhead.
	TextChunkLimit = 4000

	// MaxCallbackData is the maximum size of callback_data in bytes.
	MaxCallbackData = 64

	// DefaultPollTimeout is the long-polling timeout in seconds.
	DefaultPollTimeout = 30

	// MaxDedupeEntries is the maximum number of cached update IDs for dedup.
	MaxDedupeEntries = 2000

	// DedupeTTLMs is the TTL for deduplication entries in milliseconds (5 min).
	DedupeTTLMs = 300_000

	// MaxMessageBuffer is the hard cap on buffered inbound messages.
	// Kept small for single-user deployment; DrainMessages() should be called regularly.
	MaxMessageBuffer = 200

	// MessageBufferTrimTarget is the number of messages kept when the buffer is trimmed.
	MessageBufferTrimTarget = 100
)

// Update represents an incoming update from Telegram.
type Update struct {
	UpdateID        int64            `json:"update_id"`
	Message         *Message         `json:"message,omitempty"`
	EditedMessage   *Message         `json:"edited_message,omitempty"`
	ChannelPost     *Message         `json:"channel_post,omitempty"`
	CallbackQuery   *CallbackQuery   `json:"callback_query,omitempty"`
	MessageReaction *MessageReaction `json:"message_reaction,omitempty"`
}

// Message represents a Telegram message.
type Message struct {
	MessageID       int64           `json:"message_id"`
	From            *User           `json:"from,omitempty"`
	Chat            Chat            `json:"chat"`
	Date            int64           `json:"date"`
	Text            string          `json:"text,omitempty"`
	Entities        []MessageEntity `json:"entities,omitempty"`
	ReplyToMessage  *Message        `json:"reply_to_message,omitempty"`
	Photo           []PhotoSize     `json:"photo,omitempty"`
	Document        *Document       `json:"document,omitempty"`
	Video           *Video          `json:"video,omitempty"`
	Audio           *Audio          `json:"audio,omitempty"`
	Voice           *Voice          `json:"voice,omitempty"`
	VideoNote       *VideoNote      `json:"video_note,omitempty"`
	Sticker         *Sticker        `json:"sticker,omitempty"`
	Animation       *Animation      `json:"animation,omitempty"`
	Caption         string          `json:"caption,omitempty"`
	CaptionEntities []MessageEntity `json:"caption_entities,omitempty"`
	MediaGroupID    string          `json:"media_group_id,omitempty"`
	MessageThreadID int64           `json:"message_thread_id,omitempty"`
	IsTopicMessage  bool            `json:"is_topic_message,omitempty"`
}

// User represents a Telegram user or bot.
type User struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name,omitempty"`
	Username  string `json:"username,omitempty"`
}

// Chat represents a Telegram chat.
type Chat struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"` // "private", "group", "supergroup", "channel"
	Title     string `json:"title,omitempty"`
	Username  string `json:"username,omitempty"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	IsForum   bool   `json:"is_forum,omitempty"`
}

// MessageEntity represents a special entity in a message (bold, link, etc.).
type MessageEntity struct {
	Type     string `json:"type"`
	Offset   int    `json:"offset"`
	Length   int    `json:"length"`
	URL      string `json:"url,omitempty"`
	User     *User  `json:"user,omitempty"`
	Language string `json:"language,omitempty"`
}

// PhotoSize represents one size of a photo or thumbnail.
type PhotoSize struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	FileSize     int64  `json:"file_size,omitempty"`
}

// Document represents a general file.
type Document struct {
	FileID       string     `json:"file_id"`
	FileUniqueID string     `json:"file_unique_id"`
	FileName     string     `json:"file_name,omitempty"`
	MimeType     string     `json:"mime_type,omitempty"`
	FileSize     int64      `json:"file_size,omitempty"`
	Thumbnail    *PhotoSize `json:"thumbnail,omitempty"`
}

// Video represents a video file.
type Video struct {
	FileID       string     `json:"file_id"`
	FileUniqueID string     `json:"file_unique_id"`
	Width        int        `json:"width"`
	Height       int        `json:"height"`
	Duration     int        `json:"duration"`
	FileName     string     `json:"file_name,omitempty"`
	MimeType     string     `json:"mime_type,omitempty"`
	FileSize     int64      `json:"file_size,omitempty"`
	Thumbnail    *PhotoSize `json:"thumbnail,omitempty"`
}

// Audio represents an audio file.
type Audio struct {
	FileID       string     `json:"file_id"`
	FileUniqueID string     `json:"file_unique_id"`
	Duration     int        `json:"duration"`
	Performer    string     `json:"performer,omitempty"`
	Title        string     `json:"title,omitempty"`
	FileName     string     `json:"file_name,omitempty"`
	MimeType     string     `json:"mime_type,omitempty"`
	FileSize     int64      `json:"file_size,omitempty"`
	Thumbnail    *PhotoSize `json:"thumbnail,omitempty"`
}

// Voice represents a voice note.
type Voice struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Duration     int    `json:"duration"`
	MimeType     string `json:"mime_type,omitempty"`
	FileSize     int64  `json:"file_size,omitempty"`
}

// VideoNote represents a video message (round video).
type VideoNote struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Length       int    `json:"length"`
	Duration     int    `json:"duration"`
	FileSize     int64  `json:"file_size,omitempty"`
}

// Sticker represents a sticker.
type Sticker struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Type         string `json:"type"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	Emoji        string `json:"emoji,omitempty"`
	SetName      string `json:"set_name,omitempty"`
	FileSize     int64  `json:"file_size,omitempty"`
}

// Animation represents an animation (GIF or H.264/MPEG-4 AVC without sound).
type Animation struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	Duration     int    `json:"duration"`
	FileName     string `json:"file_name,omitempty"`
	MimeType     string `json:"mime_type,omitempty"`
	FileSize     int64  `json:"file_size,omitempty"`
}

// ForumTopic represents a forum topic in a supergroup.
type ForumTopic struct {
	Name string `json:"name"`
}

// CallbackQuery represents an incoming callback query from inline keyboard.
type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message,omitempty"`
	Data    string   `json:"data,omitempty"`
}

// ReactionType represents a reaction emoji or custom emoji.
type ReactionType struct {
	Type          string `json:"type"`                      // "emoji" or "custom_emoji"
	Emoji         string `json:"emoji,omitempty"`           // Standard emoji character
	CustomEmojiID string `json:"custom_emoji_id,omitempty"` // Custom emoji ID
}

// MessageReaction represents a change in message reactions.
type MessageReaction struct {
	MessageID   int64          `json:"message_id"`
	Chat        Chat           `json:"chat"`
	User        *User          `json:"user,omitempty"`
	ActorChat   *Chat          `json:"actor_chat,omitempty"`
	Date        int64          `json:"date"`
	OldReaction []ReactionType `json:"old_reaction"`
	NewReaction []ReactionType `json:"new_reaction"`
}

// InlineKeyboardMarkup represents an inline keyboard.
type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

// InlineKeyboardButton represents one button in an inline keyboard.
type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
	URL          string `json:"url,omitempty"`
}

// LinkPreviewOptions controls link preview behavior.
type LinkPreviewOptions struct {
	IsDisabled bool `json:"is_disabled,omitempty"`
}

// APIResponse wraps the Telegram Bot API response envelope.
type APIResponse struct {
	OK          bool                `json:"ok"`
	Result      json.RawMessage     `json:"result,omitempty"`
	Description string              `json:"description,omitempty"`
	ErrorCode   int                 `json:"error_code,omitempty"`
	Parameters  *ResponseParameters `json:"parameters,omitempty"`
}

// ResponseParameters contains information about why a request was unsuccessful.
type ResponseParameters struct {
	MigrateToChatID int64 `json:"migrate_to_chat_id,omitempty"`
	RetryAfter      int   `json:"retry_after,omitempty"`
}
