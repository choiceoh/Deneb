package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// eventToResourceURI maps gateway event names to affected resource URIs.
var eventToResourceURI = map[string]string{
	"session.created":   "deneb://sessions",
	"session.completed": "deneb://sessions",
	"session.failed":    "deneb://sessions",
	"session.killed":    "deneb://sessions",
	"agent.completed":   "deneb://sessions",
	"config.changed":    "deneb://config",
	"skills.changed":    "deneb://skills",
	"memory.updated":    "deneb://memory",
}

// eventRequiresSampling lists events that should trigger Claude analysis.
var eventRequiresSampling = map[string]bool{
	"session.failed":  true,
	"agent.completed": true,
	"cron.fired":      true,
}

// EventListener connects to the gateway WebSocket and forwards relevant
// events as MCP notifications. It also triggers sampling requests for
// events that need Claude analysis.
type EventListener struct {
	bridge    *Bridge
	transport *Transport
	resources *ResourceManager
	sampler   *Sampler
	logger    *slog.Logger
}

// NewEventListener creates an event listener.
func NewEventListener(bridge *Bridge, transport *Transport, resources *ResourceManager, sampler *Sampler, logger *slog.Logger) *EventListener {
	return &EventListener{
		bridge:    bridge,
		transport: transport,
		resources: resources,
		sampler:   sampler,
		logger:    logger,
	}
}

// gatewayEvent is the wire format for gateway WebSocket events.
type gatewayEvent struct {
	Type    string          `json:"type"`
	Event   string          `json:"event"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Run connects to the gateway WebSocket and processes events.
// Blocks until context is cancelled. Automatically reconnects with backoff.
func (el *EventListener) Run(ctx context.Context) error {
	wsURL := strings.Replace(el.bridge.baseURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL += "/ws"

	header := http.Header{}
	if el.bridge.token != "" {
		header.Set("Authorization", "Bearer "+el.bridge.token)
	}

	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		el.logger.Info("connecting to gateway WebSocket", "url", wsURL)

		conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
			HTTPHeader: header,
		})
		if err != nil {
			el.logger.Warn("websocket connect failed, retrying", "err", err, "backoff", backoff)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, maxBackoff)
			continue
		}

		el.logger.Info("gateway WebSocket connected")
		backoff = time.Second // reset on success

		err = el.readLoop(ctx, conn)
		conn.CloseNow()

		if ctx.Err() != nil {
			return ctx.Err()
		}
		el.logger.Warn("websocket disconnected, reconnecting", "err", err, "backoff", backoff)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

// readLoop reads events from the WebSocket until an error occurs.
func (el *EventListener) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		var event gatewayEvent
		if err := wsjson.Read(ctx, conn, &event); err != nil {
			return err
		}
		if event.Type != "event" {
			continue
		}
		el.handleEvent(ctx, &event)
	}
}

func (el *EventListener) handleEvent(ctx context.Context, event *gatewayEvent) {
	el.logger.Debug("gateway event", "event", event.Event)

	// Forward bridge messages as MCP channel notifications.
	if event.Event == "bridge.message" {
		el.forwardBridgeMessage(event)
		return
	}

	// Send resource update notifications for subscribed URIs.
	if uri, ok := eventToResourceURI[event.Event]; ok {
		if el.resources.IsSubscribed(uri) {
			params, _ := json.Marshal(ResourceUpdatedParams{URI: uri})
			_ = el.transport.WriteNotification(&Notification{
				JSONRPC: "2.0",
				Method:  "notifications/resources/updated",
				Params:  params,
			})
		}
	}

	// Trigger sampling for events that need analysis.
	if eventRequiresSampling[event.Event] && el.sampler != nil {
		go el.sampler.HandleEvent(ctx, event.Event, event.Payload)
	}
}

// forwardBridgeMessage converts a bridge.message gateway event into an MCP
// channel notification (notifications/claude/channel). This is the push path
// for Deneb main agent → Claude Code communication.
func (el *EventListener) forwardBridgeMessage(event *gatewayEvent) {
	// Parse the bridge payload.
	var payload struct {
		Message string `json:"message"`
		Source  string `json:"source"`
		TS      int64  `json:"ts"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		el.logger.Warn("bridge message parse error", "err", err)
		return
	}

	channelParams := ChannelNotificationParams{
		Content: payload.Message,
		Meta: map[string]any{
			"source": payload.Source,
			"ts":     payload.TS,
		},
	}

	params, err := json.Marshal(channelParams)
	if err != nil {
		el.logger.Warn("bridge channel notification marshal error", "err", err)
		return
	}

	if err := el.transport.WriteNotification(&Notification{
		JSONRPC: "2.0",
		Method:  "notifications/claude/channel",
		Params:  params,
	}); err != nil {
		el.logger.Warn("bridge channel notification write error", "err", err)
		return
	}

	el.logger.Info("bridge message forwarded to channel", "source", payload.Source)
}
