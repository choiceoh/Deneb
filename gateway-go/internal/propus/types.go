// Package propus implements the Propus desktop coding channel for Deneb.
//
// Propus is a Slint-based desktop coding assistant that connects to Deneb
// via WebSocket. This package bridges the Propus client protocol
// (ClientMessage/ServerMessage JSON) with Deneb's internal chat pipeline.
package propus

import "encoding/json"

// --- Inbound: Propus client → Deneb ---

// ClientMessage is the envelope for all messages from the Propus desktop client.
// Wire format: {"type":"SendMessage","data":{"text":"..."}}
type ClientMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// SendMessageData is the payload for ClientMessage type "SendMessage".
type SendMessageData struct {
	Text string `json:"text"`
}

// SetApiKeyData is the payload for ClientMessage type "SetApiKey".
type SetApiKeyData struct {
	Key string `json:"key"`
}

// --- Outbound: Deneb → Propus client ---

// ServerMessage is the envelope for all messages from Deneb to the Propus client.
// Wire format: {"type":"Text","data":{"content":"..."}}
type ServerMessage struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

// Convenience constructors for ServerMessage.

func MsgText(content string) ServerMessage {
	return ServerMessage{Type: "Text", Data: map[string]string{"content": content}}
}

func MsgToolStart(name, args string) ServerMessage {
	return ServerMessage{Type: "ToolStart", Data: map[string]string{"name": name, "args": args}}
}

func MsgToolResult(name, result string) ServerMessage {
	return ServerMessage{Type: "ToolResult", Data: map[string]string{"name": name, "result": result}}
}

func MsgUsage(prompt, completion, total int) ServerMessage {
	return ServerMessage{Type: "Usage", Data: map[string]int{"prompt": prompt, "completion": completion, "total": total}}
}

func MsgDone() ServerMessage {
	return ServerMessage{Type: "Done"}
}

func MsgError(message string) ServerMessage {
	return ServerMessage{Type: "Error", Data: map[string]string{"message": message}}
}

func MsgChatCleared() ServerMessage {
	return ServerMessage{Type: "ChatCleared"}
}

func MsgPong() ServerMessage {
	return ServerMessage{Type: "Pong"}
}

func MsgSessionSaved(path string) ServerMessage {
	return ServerMessage{Type: "SessionSaved", Data: map[string]string{"path": path}}
}

func MsgFile(name, mediaType string, size int64, url string) ServerMessage {
	return ServerMessage{Type: "File", Data: map[string]any{
		"name":       name,
		"media_type": mediaType,
		"size":       size,
		"url":        url,
	}}
}

func MsgTyping() ServerMessage {
	return ServerMessage{Type: "Typing"}
}

func MsgConfigStatus(model, service, denebStatus, connID string) ServerMessage {
	return ServerMessage{Type: "ConfigStatus", Data: map[string]any{
		"needs_api_key": false,
		"model":         model,
		"service":       service,
		"deneb_status":  denebStatus,
		"conn_id":       connID,
	}}
}

// --- Session browsing types ---

// SessionPreview is a lightweight summary of a session for the sidebar list.
type SessionPreview struct {
	Key          string `json:"key"`
	Title        string `json:"title"`
	UpdatedAt    int64  `json:"updated_at"`    // unix ms
	MessageCount int    `json:"message_count"`
	Status       string `json:"status"` // "active", "done", "idle"
}

// SwitchSessionData is the payload for ClientMessage type "SwitchSession".
type SwitchSessionData struct {
	SessionKey string `json:"session_key"`
}

// SearchSessionsData is the payload for ClientMessage type "SearchSessions".
type SearchSessionsData struct {
	Query string `json:"query"`
}

func MsgSessionList(sessions []SessionPreview) ServerMessage {
	return ServerMessage{Type: "SessionList", Data: map[string]any{
		"sessions": sessions,
	}}
}

// MsgSessionHistory sends the full message history for a switched session.
func MsgSessionHistory(messages []SessionHistoryMsg) ServerMessage {
	return ServerMessage{Type: "SessionHistory", Data: map[string]any{
		"messages": messages,
	}}
}

// SessionHistoryMsg is a message in a session's history, sent to the client
// when switching sessions.
type SessionHistoryMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
