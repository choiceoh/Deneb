package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// messageToolSchema returns the JSON Schema for the message tool.
// This is a flattened schema with per-action runtime validation, matching
// the Node.js message-tool.ts approach.
func messageToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Message action: send, react, reply, thread-reply",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "Text content to send",
			},
			"to": map[string]any{
				"type":        "string",
				"description": "Recipient (chat ID or user identifier)",
			},
			"channel": map[string]any{
				"type":        "string",
				"description": "Target channel (e.g., telegram)",
			},
			"replyTo": map[string]any{
				"type":        "string",
				"description": "Message ID to reply to",
			},
			"emoji": map[string]any{
				"type":        "string",
				"description": "Emoji for react action",
			},
			"messageId": map[string]any{
				"type":        "string",
				"description": "Message ID for react/reply actions",
			},
			"silent": map[string]any{
				"type":        "boolean",
				"description": "Send without notification sound",
			},
		},
		"required": []string{"action"},
	}
}

// toolMessage implements the message tool for proactive channel sends.
// It uses context values to access the ReplyFunc and DeliveryContext.
func toolMessage() ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action    string `json:"action"`
			Message   string `json:"message"`
			To        string `json:"to"`
			Channel   string `json:"channel"`
			ReplyTo   string `json:"replyTo"`
			Emoji     string `json:"emoji"`
			MessageID string `json:"messageId"`
			Silent    bool   `json:"silent"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid message params: %w", err)
		}
		if p.Action == "" {
			p.Action = "send"
		}

		switch p.Action {
		case "send", "reply", "thread-reply":
			if p.Message == "" {
				return "", fmt.Errorf("message is required for action=%q", p.Action)
			}

			// Get reply function from context.
			replyFn := ReplyFuncFromContext(ctx)
			if replyFn == nil {
				return "Message tool: no reply function available (channel not connected).", nil
			}

			// Build delivery context: use explicit params or fall back to current session delivery.
			delivery := DeliveryFromContext(ctx)
			if delivery == nil {
				delivery = &DeliveryContext{}
			}

			// Override with explicit params if provided.
			sendDelivery := &DeliveryContext{
				Channel:   delivery.Channel,
				To:        delivery.To,
				AccountID: delivery.AccountID,
				ThreadID:  delivery.ThreadID,
			}
			if p.Channel != "" {
				sendDelivery.Channel = p.Channel
			}
			if p.To != "" {
				sendDelivery.To = p.To
			}

			sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			if err := replyFn(sendCtx, sendDelivery, p.Message); err != nil {
				return fmt.Sprintf("Failed to send message: %s", err.Error()), nil
			}
			return "Message sent successfully.", nil

		case "react":
			// Reaction support requires channel-specific implementation.
			return "Reaction support is not yet available in the Go gateway.", nil

		default:
			return fmt.Sprintf("Unknown message action: %q. Supported: send, reply.", p.Action), nil
		}
	}
}
