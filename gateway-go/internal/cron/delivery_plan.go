// delivery_plan.go — Full delivery plan resolution and failure destination handling.
// Mirrors src/cron/delivery.ts resolveCronDeliveryPlan() + resolveFailureDestination().
package cron

import "strings"

// CronDeliveryPlan holds the resolved delivery parameters for a cron job run.
// This is the Go equivalent of the TypeScript CronDeliveryPlan type.
type CronDeliveryPlan struct {
	Mode      CronDeliveryMode `json:"mode"`
	Channel   string           `json:"channel,omitempty"`
	To        string           `json:"to,omitempty"`
	AccountID string           `json:"accountId,omitempty"`
	Source    string           `json:"source"` // "delivery" or "payload"
	Requested bool             `json:"requested"`
}

// ResolveCronDeliveryPlan extracts delivery config from a job's delivery field,
// falling back to legacy payload fields.
func ResolveCronDeliveryPlan(job CronJobFull) CronDeliveryPlan {
	delivery := job.Delivery
	hasDelivery := delivery != nil

	// Extract legacy payload fields (only for agentTurn).
	var payloadChannel, payloadTo string
	if job.Payload.Kind == "agentTurn" {
		payloadChannel = normalizeChannelStr(job.Payload.Model) // legacy: provider field mapped to channel
		payloadTo = normalizeToStr("")
	}

	if hasDelivery {
		mode := resolveDeliveryMode(string(delivery.Mode))
		channel := normalizeChannelStr(delivery.Channel)
		if channel == "" {
			channel = payloadChannel
		}
		if channel == "" {
			channel = "last"
		}
		to := normalizeToStr(delivery.To)
		if to == "" {
			to = payloadTo
		}

		resolvedMode := mode
		if resolvedMode == "" {
			resolvedMode = DeliveryModeAnnounce
		}

		return CronDeliveryPlan{
			Mode:      resolvedMode,
			Channel:   conditionalChannel(resolvedMode, channel),
			To:        to,
			AccountID: strings.TrimSpace(delivery.AccountID),
			Source:    "delivery",
			Requested: resolvedMode == DeliveryModeAnnounce,
		}
	}

	// Legacy payload-based delivery (no delivery object).
	channel := payloadChannel
	if channel == "" {
		channel = "last"
	}
	to := payloadTo
	hasExplicitTarget := to != ""
	requested := hasExplicitTarget

	mode := DeliveryModeNone
	if requested {
		mode = DeliveryModeAnnounce
	}

	return CronDeliveryPlan{
		Mode:      mode,
		Channel:   channel,
		To:        to,
		Source:    "payload",
		Requested: requested,
	}
}

// ResolveCronDeliveryPlanFromStore is the same as ResolveCronDeliveryPlan but works
// with StoreJob (the existing type). Adapts the simpler JobDeliveryConfig to CronDeliveryFull.
func ResolveCronDeliveryPlanFromStore(job StoreJob) CronDeliveryPlan {
	if job.Delivery == nil {
		return CronDeliveryPlan{
			Mode:      DeliveryModeNone,
			Channel:   "last",
			Source:    "payload",
			Requested: false,
		}
	}

	channel := normalizeChannelStr(job.Delivery.Channel)
	if channel == "" {
		channel = "last"
	}
	to := normalizeToStr(job.Delivery.To)

	return CronDeliveryPlan{
		Mode:      DeliveryModeAnnounce,
		Channel:   channel,
		To:        to,
		AccountID: strings.TrimSpace(job.Delivery.AccountID),
		Source:    "delivery",
		Requested: true,
	}
}

// CronFailureDeliveryPlan holds the resolved failure notification target.
type CronFailureDeliveryPlan struct {
	Mode      string `json:"mode"` // "announce" or "webhook"
	Channel   string `json:"channel,omitempty"`
	To        string `json:"to,omitempty"`
	AccountID string `json:"accountId,omitempty"`
}

// CronFailureDestinationConfig is the global failure destination config.
type CronFailureDestinationConfig struct {
	Channel   string `json:"channel,omitempty"`
	To        string `json:"to,omitempty"`
	AccountID string `json:"accountId,omitempty"`
	Mode      string `json:"mode,omitempty"`
}

// ResolveFailureDestination merges global + job-level failure destination configs.
func ResolveFailureDestination(
	delivery *CronDeliveryFull,
	globalConfig *CronFailureDestinationConfig,
) *CronFailureDeliveryPlan {
	var channel, to, accountID, mode string

	// Start with global config.
	if globalConfig != nil {
		channel = normalizeChannelStr(globalConfig.Channel)
		to = normalizeToStr(globalConfig.To)
		accountID = strings.TrimSpace(globalConfig.AccountID)
		mode = normalizeFailureMode(globalConfig.Mode)
	}

	// Override with job-level failure destination.
	jobDest := delivery != nil && delivery.FailureDestination != nil
	if jobDest {
		fd := delivery.FailureDestination
		if ch := normalizeChannelStr(fd.Channel); ch != "" || fd.Channel != "" {
			channel = ch
		}
		if t := normalizeToStr(fd.To); t != "" || fd.To != "" {
			to = t
		}
		if aid := strings.TrimSpace(fd.AccountID); aid != "" || fd.AccountID != "" {
			accountID = aid
		}
		if m := normalizeFailureMode(fd.Mode); m != "" {
			// Mode change clears inherited 'to' if not explicitly set at job level.
			globalMode := "announce"
			if globalConfig != nil && globalConfig.Mode != "" {
				globalMode = globalConfig.Mode
			}
			if fd.To == "" && globalMode != m {
				to = ""
			}
			mode = m
		}
	}

	if channel == "" && to == "" && accountID == "" && mode == "" {
		return nil
	}

	resolvedMode := mode
	if resolvedMode == "" {
		resolvedMode = "announce"
	}

	// Webhook requires a URL.
	if resolvedMode == "webhook" && to == "" {
		return nil
	}

	result := &CronFailureDeliveryPlan{
		Mode:      resolvedMode,
		To:        to,
		AccountID: accountID,
	}
	if resolvedMode == "announce" {
		result.Channel = channel
		if result.Channel == "" {
			result.Channel = "last"
		}
	}

	// Check if failure target is same as primary delivery target.
	if delivery != nil && isSameDeliveryTarget(delivery, result) {
		return nil
	}

	return result
}

func isSameDeliveryTarget(delivery *CronDeliveryFull, failure *CronFailureDeliveryPlan) bool {
	primaryMode := string(delivery.Mode)
	if primaryMode == "" {
		primaryMode = "announce"
	}
	if primaryMode == "none" {
		return false
	}

	if failure.Mode == "webhook" {
		return primaryMode == "webhook" && delivery.To == failure.To
	}

	primaryChannel := delivery.Channel
	if primaryChannel == "" {
		primaryChannel = "last"
	}
	failureChannel := failure.Channel
	if failureChannel == "" {
		failureChannel = "last"
	}

	return failureChannel == primaryChannel &&
		failure.To == delivery.To &&
		failure.AccountID == delivery.AccountID
}

// --- Helpers ---

func normalizeChannelStr(v string) string {
	trimmed := strings.ToLower(strings.TrimSpace(v))
	if trimmed == "" {
		return ""
	}
	return trimmed
}

func normalizeToStr(v string) string {
	return strings.TrimSpace(v)
}

func normalizeFailureMode(v string) string {
	trimmed := strings.ToLower(strings.TrimSpace(v))
	if trimmed == "announce" || trimmed == "webhook" {
		return trimmed
	}
	return ""
}

func resolveDeliveryMode(raw string) CronDeliveryMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "announce", "deliver":
		return DeliveryModeAnnounce
	case "webhook":
		return DeliveryModeWebhook
	case "none":
		return DeliveryModeNone
	default:
		return ""
	}
}

func conditionalChannel(mode CronDeliveryMode, channel string) string {
	if mode == DeliveryModeAnnounce {
		return channel
	}
	return ""
}
