package telegram

import "strconv"

// AccessResult describes the outcome of an access check.
type AccessResult struct {
	// Allowed indicates whether the message is permitted.
	Allowed bool
	// Reason describes why access was denied (empty if allowed).
	Reason string
}

// CheckAccess evaluates whether an inbound message should be processed,
// applying the full DM/group policy resolution chain:
//
//	account-level → per-chat override → per-topic override → sender allowlist.
//
// Mirrors the TypeScript resolution in extensions/telegram/src/dm-access.ts
// and extensions/telegram/src/group-access.ts.
func CheckAccess(cfg *Config, msg *Message) AccessResult {
	if msg.From == nil {
		return AccessResult{Allowed: false, Reason: "no sender"}
	}

	// Account disabled entirely.
	if !cfg.IsEnabled() {
		return AccessResult{Allowed: false, Reason: "account disabled"}
	}

	if msg.Chat.Type == "private" {
		return checkDmAccess(cfg, msg)
	}
	return checkGroupAccess(cfg, msg)
}

// checkDmAccess evaluates DM access using the policy chain:
// per-DM override > account-level dmPolicy.
func checkDmAccess(cfg *Config, msg *Message) AccessResult {
	chatID := strconv.FormatInt(msg.Chat.ID, 10)

	// Per-DM override.
	if dc := cfg.Direct[chatID]; dc != nil {
		if dc.Enabled != nil && !*dc.Enabled {
			return AccessResult{Allowed: false, Reason: "dm disabled for chat " + chatID}
		}
		if dc.DmPolicy != "" {
			return applyDmPolicy(dc.DmPolicy, msg.From, &dc.AllowFrom, &cfg.AllowFrom)
		}
	}

	// Account-level policy.
	return applyDmPolicy(cfg.EffectiveDmPolicy(), msg.From, nil, &cfg.AllowFrom)
}

// applyDmPolicy enforces a resolved DM policy against a sender.
// chatAllowFrom is the per-chat allowlist; accountAllowFrom is the account-level fallback.
func applyDmPolicy(policy DmPolicy, sender *User, chatAllowFrom, accountAllowFrom *AllowList) AccessResult {
	switch policy {
	case DmPolicyDisabled:
		return AccessResult{Allowed: false, Reason: "dm policy: disabled"}
	case DmPolicyOpen:
		return AccessResult{Allowed: true}
	case DmPolicyAllowlist:
		if matchesAnyAllowList(sender, chatAllowFrom, accountAllowFrom) {
			return AccessResult{Allowed: true}
		}
		return AccessResult{Allowed: false, Reason: "dm policy: sender not in allowlist"}
	case DmPolicyPairing:
		// Pairing mode: allow if sender is in the allowlist (already paired),
		// otherwise reject (pairing flow is handled at a higher layer).
		if matchesAnyAllowList(sender, chatAllowFrom, accountAllowFrom) {
			return AccessResult{Allowed: true}
		}
		return AccessResult{Allowed: false, Reason: "dm policy: sender not paired"}
	default:
		// Unknown policy — fail closed.
		return AccessResult{Allowed: false, Reason: "dm policy: unknown policy " + string(policy)}
	}
}

// checkGroupAccess evaluates group access using the policy chain:
// per-topic override > per-group override > account-level groupPolicy.
func checkGroupAccess(cfg *Config, msg *Message) AccessResult {
	chatID := strconv.FormatInt(msg.Chat.ID, 10)

	gc := cfg.Groups[chatID]

	// Per-group enabled check.
	if gc != nil && gc.Enabled != nil && !*gc.Enabled {
		return AccessResult{Allowed: false, Reason: "group disabled: " + chatID}
	}

	// Per-topic override (forum topics).
	if msg.MessageThreadID != 0 && gc != nil && gc.Topics != nil {
		topicID := strconv.FormatInt(msg.MessageThreadID, 10)
		if tc := gc.Topics[topicID]; tc != nil {
			if tc.Enabled != nil && !*tc.Enabled {
				return AccessResult{Allowed: false, Reason: "topic disabled: " + chatID + ":" + topicID}
			}
			if tc.GroupPolicy != "" {
				return applyGroupPolicy(tc.GroupPolicy, msg.From, &tc.AllowFrom, groupAllowChain(gc, cfg))
			}
		}
	}

	// Per-group policy override.
	if gc != nil && gc.GroupPolicy != "" {
		return applyGroupPolicy(gc.GroupPolicy, msg.From, &gc.AllowFrom, accountGroupAllow(cfg))
	}

	// Account-level policy.
	return applyGroupPolicy(cfg.EffectiveGroupPolicy(), msg.From, nil, accountGroupAllow(cfg))
}

// applyGroupPolicy enforces a resolved group policy against a sender.
func applyGroupPolicy(policy GroupPolicy, sender *User, chatAllowFrom, fallbackAllowFrom *AllowList) AccessResult {
	switch policy {
	case GroupPolicyDisabled:
		return AccessResult{Allowed: false, Reason: "group policy: disabled"}
	case GroupPolicyOpen:
		return AccessResult{Allowed: true}
	case GroupPolicyAllowlist:
		if matchesAnyAllowList(sender, chatAllowFrom, fallbackAllowFrom) {
			return AccessResult{Allowed: true}
		}
		return AccessResult{Allowed: false, Reason: "group policy: sender not in allowlist"}
	default:
		return AccessResult{Allowed: false, Reason: "group policy: unknown policy " + string(policy)}
	}
}

// matchesAnyAllowList checks if sender matches any of the given allowlists.
// Nil or empty lists are skipped (not treated as "allow all").
func matchesAnyAllowList(sender *User, lists ...*AllowList) bool {
	for _, list := range lists {
		if list != nil && !list.IsEmpty() && list.MatchesUser(sender) {
			return true
		}
	}
	return false
}

// groupAllowChain returns the fallback allowlist for a topic within a group:
// group-level allowFrom → account-level groupAllowFrom/allowFrom.
func groupAllowChain(gc *GroupConfig, cfg *Config) *AllowList {
	if !gc.AllowFrom.IsEmpty() {
		return &gc.AllowFrom
	}
	return accountGroupAllow(cfg)
}

// accountGroupAllow returns the account-level group sender allowlist,
// falling back to the DM allowlist if groupAllowFrom is not set.
func accountGroupAllow(cfg *Config) *AllowList {
	if !cfg.GroupAllowFrom.IsEmpty() {
		return &cfg.GroupAllowFrom
	}
	return &cfg.AllowFrom
}
