// Package server — model quick-change UI for the Telegram inline keyboard.
//
// Extracted from inbound.go: modelEntry type, /models command handler,
// inline keyboard builder, and model-switch callback handler.
package server

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/handlers"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
)

// modelEntry describes a model shown in the /models quick-change keyboard.
type modelEntry struct {
	label   string // button label (e.g., "main: glm-5-turbo")
	fullID  string // full model ID sent to LLM (e.g., "zai/glm-5-turbo")
	display string // short display name (e.g., "glm-5-turbo")
}

// quickChangeModels returns the ordered list of models for the /models keyboard.
// Includes role-based models from the registry + extra frequently-used models.
func (p *InboundProcessor) quickChangeModels() []modelEntry {
	var entries []modelEntry

	// 1. Role-based models from registry.
	if reg := p.server.modelRegistry; reg != nil {
		roles := []struct {
			role  modelrole.Role
			label string
		}{
			{modelrole.RoleMain, "main"},
			{modelrole.RoleLightweight, "lightweight"},
			{modelrole.RoleFallback, "fallback"},
		}
		seen := make(map[string]struct{})
		for _, r := range roles {
			cfg := reg.Config(r.role)
			if cfg.Model == "" {
				continue
			}
			fullID := reg.FullModelID(r.role)
			seen[fullID] = struct{}{}
			entries = append(entries, modelEntry{
				label:   r.label + ": " + shortModelName(cfg.Model),
				fullID:  fullID,
				display: shortModelName(cfg.Model),
			})
		}

		// 2. Extra models not already covered by roles.
		extras := []struct {
			provider string
			model    string
		}{
			{"zai", "glm-5-turbo"},
			{"zai", "glm-5.1"},
		}
		for _, e := range extras {
			fullID := e.provider + "/" + e.model
			if _, ok := seen[fullID]; ok {
				continue
			}
			entries = append(entries, modelEntry{
				label:   e.model,
				fullID:  fullID,
				display: e.model,
			})
		}
	}

	return entries
}

// shortModelName strips the provider prefix from a model name.
func shortModelName(model string) string {
	if idx := strings.LastIndex(model, "/"); idx >= 0 && idx < len(model)-1 {
		return model[idx+1:]
	}
	return model
}

// handleModelsCommand sends a model quick-change message with an inline keyboard.
func (p *InboundProcessor) handleModelsCommand(chatID string) {
	entries := p.quickChangeModels()
	if len(entries) == 0 {
		p.sendCommandReply(chatID, &handlers.CommandResult{Reply: "모델 레지스트리를 사용할 수 없습니다.", SkipAgent: true})
		return
	}

	client := p.server.telegramPlug.Client()
	if client == nil {
		return
	}

	currentModel := p.chatHandler.DefaultModel()
	if currentModel == "" && p.server.modelRegistry != nil {
		currentModel = p.server.modelRegistry.FullModelID(modelrole.RoleMain)
	}

	text := "🤖 <b>모델 퀵체인지</b>\n\n"
	text += "현재: <code>" + currentModel + "</code>"

	keyboard := p.buildModelKeyboard(entries, currentModel)

	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := telegram.SendText(ctx, client, id, text, telegram.SendOptions{
		ParseMode: "HTML",
		Keyboard:  keyboard,
	}); err != nil {
		p.logger.Warn("failed to send models command reply", "error", err)
	}
}

// buildModelKeyboard builds a 2-column inline keyboard from model entries.
func (p *InboundProcessor) buildModelKeyboard(entries []modelEntry, currentModel string) *telegram.InlineKeyboardMarkup {
	var rows [][]telegram.InlineKeyboardButton
	var row []telegram.InlineKeyboardButton
	for i, e := range entries {
		label := e.label
		if e.fullID == currentModel {
			label = "✓ " + label
		}

		row = append(row, telegram.InlineKeyboardButton{
			Text:         label,
			CallbackData: telegram.ActionModelSwitch + ":" + e.fullID,
		})

		if len(row) == 2 || i == len(entries)-1 {
			rows = append(rows, row)
			row = nil
		}
	}
	return telegram.BuildInlineKeyboard(rows)
}

// handleModelSwitchCallback processes a model quick-change button press.
func (p *InboundProcessor) handleModelSwitchCallback(cb *telegram.CallbackQuery, chatID string, fullModelID string) {
	client := p.server.telegramPlug.Client()
	if client == nil {
		return
	}

	// Apply model change.
	p.chatHandler.SetDefaultModel(fullModelID)

	// Persist to deneb.json so the choice survives restarts.
	go func() {
		cfgPath := config.ResolveConfigPath()
		if err := config.PersistDefaultModel(cfgPath, fullModelID, p.logger); err != nil {
			p.logger.Warn("failed to persist model choice", "model", fullModelID, "error", err)
		}
	}()

	displayModel := shortModelName(fullModelID)

	// Acknowledge with toast.
	ackCtx, ackCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer ackCancel()
	if err := telegram.AnswerCallbackQuery(ackCtx, client, cb.ID, "✓ "+displayModel); err != nil {
		p.logger.Warn("failed to answer model switch callback", "error", err)
	}

	// Edit original message to update the checkmark.
	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return
	}

	text := "🤖 <b>모델 퀵체인지</b>\n\n"
	text += "현재: <code>" + fullModelID + "</code>"

	entries := p.quickChangeModels()
	keyboard := p.buildModelKeyboard(entries, fullModelID)

	editCtx, editCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer editCancel()
	if _, err := telegram.EditMessageText(editCtx, client, id, cb.Message.MessageID, text, "HTML", keyboard); err != nil {
		p.logger.Warn("failed to edit model switch message", "error", err)
	}
}
