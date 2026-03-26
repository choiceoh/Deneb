package cron

import (
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
		if s.At != "" {
			s.Kind = "at"
		} else if s.EveryMs > 0 {
			s.Kind = "every"
		} else if s.Expr != "" {
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
		if p.Message != "" {
			p.Kind = "agentTurn"
		} else if p.Text != "" {
			p.Kind = "systemEvent"
		} else if p.Model != "" || p.Thinking != "" {
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

// NormalizeJobPatch applies defaults and validation to a job update patch.
func NormalizeJobPatch(existing *StoreJob, patch func(*StoreJob)) {
	if existing == nil {
		return
	}
	patch(existing)
	NormalizeJobInput(existing)
}
