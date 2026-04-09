package cron

import (
	"fmt"
	"strings"
	"time"
)

// NormalizeJobInput validates and normalizes a job creation/update input.
func NormalizeJobInput(job *StoreJob) {
	if job == nil {
		return
	}

	// Normalize schedule.
	normalizeSchedule(&job.Schedule)

	// Normalize payload.
	normalizePayload(&job.Payload)

	// Normalize delivery.
	if job.Delivery != nil {
		normalizeDelivery(job.Delivery)
	}

	// Normalize agent ID.
	job.AgentID = strings.TrimSpace(job.AgentID)

	// Ensure timestamps.
	now := time.Now().UnixMilli()
	if job.CreatedAtMs == 0 {
		job.CreatedAtMs = now
	}
	job.UpdatedAtMs = now
}

func normalizeSchedule(s *StoreSchedule) {
	// Normalize kind.
	kind := strings.ToLower(strings.TrimSpace(s.Kind))
	switch kind {
	case "at", "every", "cron":
		s.Kind = kind
	default:
		// Infer kind from fields.
		switch {
		case s.At != "":
			s.Kind = "at"
		case s.EveryMs > 0:
			s.Kind = "every"
		case s.Expr != "":
			s.Kind = "cron"
		}
	}

	// Normalize expression (legacy "cron" field to "expr").
	s.Expr = strings.TrimSpace(s.Expr)

	// Normalize timezone.
	s.Tz = strings.TrimSpace(s.Tz)

	// Ensure stagger is non-negative.
	if s.StaggerMs < 0 {
		s.StaggerMs = 0
	}
}

func normalizePayload(p *StorePayload) {
	// Normalize kind.
	kind := strings.ToLower(strings.TrimSpace(p.Kind))
	switch kind {
	case "agentturn":
		p.Kind = "agentTurn"
	case "systemevent":
		p.Kind = "systemEvent"
	default:
		// Infer kind.
		switch {
		case p.Message != "":
			p.Kind = "agentTurn"
		case p.Text != "":
			p.Kind = "systemEvent"
		case p.Model != "" || p.Thinking != "":
			p.Kind = "agentTurn"
		}
	}

	// Trim strings.
	p.Message = strings.TrimSpace(p.Message)
	p.Text = strings.TrimSpace(p.Text)
	p.Model = strings.TrimSpace(p.Model)
	p.Thinking = strings.TrimSpace(p.Thinking)

	// Ensure timeout is non-negative.
	if p.TimeoutSeconds < 0 {
		p.TimeoutSeconds = 0
	}
}

func normalizeDelivery(d *JobDeliveryConfig) {
	// Normalize mode keywords.
	channel := strings.ToLower(strings.TrimSpace(d.Channel))
	if channel == "deliver" {
		channel = "" // legacy
	}
	d.Channel = channel

	d.To = strings.TrimSpace(d.To)
	d.AccountID = strings.TrimSpace(d.AccountID)
}

// InferLegacyName generates a job name from payload or schedule info
// when no explicit name is provided.
func InferLegacyName(job StoreJob) string {
	// Try payload text/message.
	var text string
	switch job.Payload.Kind {
	case "systemEvent":
		text = job.Payload.Text
	case "agentTurn":
		text = job.Payload.Message
	}
	if text != "" {
		firstLine := strings.SplitN(text, "\n", 2)[0]
		firstLine = strings.TrimSpace(firstLine)
		if firstLine != "" {
			if len(firstLine) > 60 {
				return firstLine[:59] + "…"
			}
			return firstLine
		}
	}

	// Fall back to schedule info.
	switch job.Schedule.Kind {
	case "cron":
		if job.Schedule.Expr != "" {
			label := "Cron: " + job.Schedule.Expr
			if len(label) > 58 {
				return label[:57] + "…"
			}
			return label
		}
	case "every":
		if job.Schedule.EveryMs > 0 {
			return fmt.Sprintf("Every: %dms", job.Schedule.EveryMs)
		}
	case "at":
		return "One-shot"
	}
	return "Cron job"
}
