package rl

import (
	"encoding/json"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/localai"
)

// Collector captures local AI hub calls as RL training trajectories.
// Registered as the hub's observer via Hub.SetObserver(c.Observe).
type Collector struct {
	store        *Store
	enabledTasks map[string]bool
	logger       *slog.Logger
}

// NewCollector creates a collector that filters by enabled task types.
func NewCollector(store *Store, envConfigs []EnvConfig, logger *slog.Logger) *Collector {
	if logger == nil {
		logger = slog.Default()
	}
	enabled := make(map[string]bool, len(envConfigs))
	for _, ec := range envConfigs {
		if ec.Enabled {
			enabled[ec.TaskType] = true
		}
	}
	return &Collector{
		store:        store,
		enabledTasks: enabled,
		logger:       logger,
	}
}

// Observe is the callback registered on localai.Hub.SetObserver.
// Called after each successful local AI request completion.
func (c *Collector) Observe(req localai.Request, resp localai.Response, err error) {
	if err != nil {
		return // only collect successful completions
	}
	if resp.FromCache {
		return // cached responses are duplicates, skip
	}

	taskType := req.CallerTag
	if taskType == "" {
		return
	}

	if !c.enabledTasks[taskType] {
		return
	}

	userMsg := extractUserMessage(req.Messages)

	c.store.Add(Trajectory{
		TaskType:    taskType,
		System:      req.System,
		UserMessage: userMsg,
		Response:    resp.Text,
		Metadata: map[string]any{
			"max_tokens": req.MaxTokens,
			"priority":   int(req.Priority),
		},
	})

	c.logger.Debug("rl: trajectory captured",
		"task", taskType,
		"resp_len", len(resp.Text),
	)
}

// extractUserMessage pulls the text content from the last user message.
// Message.Content is json.RawMessage: either a JSON string or []ContentBlock.
func extractUserMessage(messages []llm.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role != "user" {
			continue
		}
		if len(msg.Content) == 0 {
			continue
		}

		// Try plain string first (most common for local AI tasks).
		var text string
		if json.Unmarshal(msg.Content, &text) == nil {
			return text
		}

		// Try []ContentBlock.
		var blocks []llm.ContentBlock
		if json.Unmarshal(msg.Content, &blocks) == nil {
			for _, b := range blocks {
				if b.Type == "text" && b.Text != "" {
					return b.Text
				}
			}
		}
	}
	return ""
}
