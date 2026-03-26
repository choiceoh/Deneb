package telegram

import "testing"

func boolPtr(b bool) *bool { return &b }

// --- DM policy tests ---

func TestCheckAccess_NilFrom(t *testing.T) {
	cfg := &Config{BotToken: "tok"}
	msg := &Message{Chat: Chat{Type: "private"}}
	r := CheckAccess(cfg, msg)
	if r.Allowed {
		t.Error("expected denied for nil From")
	}
}

func TestCheckAccess_AccountDisabled(t *testing.T) {
	cfg := &Config{BotToken: "tok", Enabled: boolPtr(false)}
	msg := &Message{From: &User{ID: 1}, Chat: Chat{Type: "private"}}
	r := CheckAccess(cfg, msg)
	if r.Allowed {
		t.Error("expected denied when account disabled")
	}
	if r.Reason != "account disabled" {
		t.Errorf("unexpected reason: %s", r.Reason)
	}
}

func TestCheckAccess_DmPolicyDisabled(t *testing.T) {
	cfg := &Config{BotToken: "tok", DmPolicy: DmPolicyDisabled}
	msg := &Message{From: &User{ID: 1}, Chat: Chat{Type: "private"}}
	r := CheckAccess(cfg, msg)
	if r.Allowed {
		t.Error("expected denied with dm policy disabled")
	}
}

func TestCheckAccess_DmPolicyOpen(t *testing.T) {
	cfg := &Config{BotToken: "tok", DmPolicy: DmPolicyOpen}
	msg := &Message{From: &User{ID: 1}, Chat: Chat{Type: "private"}}
	r := CheckAccess(cfg, msg)
	if !r.Allowed {
		t.Errorf("expected allowed with dm policy open, reason: %s", r.Reason)
	}
}

func TestCheckAccess_DmPolicyAllowlist_Allowed(t *testing.T) {
	cfg := &Config{BotToken: "tok", DmPolicy: DmPolicyAllowlist, AllowFrom: AllowList{IDs: []int64{42}}}
	msg := &Message{From: &User{ID: 42}, Chat: Chat{Type: "private"}}
	r := CheckAccess(cfg, msg)
	if !r.Allowed {
		t.Error("expected allowed for user in allowlist")
	}
}

func TestCheckAccess_DmPolicyAllowlist_Denied(t *testing.T) {
	cfg := &Config{BotToken: "tok", DmPolicy: DmPolicyAllowlist, AllowFrom: AllowList{IDs: []int64{42}}}
	msg := &Message{From: &User{ID: 99}, Chat: Chat{Type: "private"}}
	r := CheckAccess(cfg, msg)
	if r.Allowed {
		t.Error("expected denied for user not in allowlist")
	}
}

func TestCheckAccess_DmPolicyAllowlist_Username(t *testing.T) {
	cfg := &Config{BotToken: "tok", DmPolicy: DmPolicyAllowlist, AllowFrom: AllowList{Usernames: []string{"peter"}}}
	msg := &Message{From: &User{ID: 1, Username: "Peter"}, Chat: Chat{Type: "private"}}
	r := CheckAccess(cfg, msg)
	if !r.Allowed {
		t.Error("expected case-insensitive username match")
	}
}

func TestCheckAccess_DmPolicyAllowlist_Wildcard(t *testing.T) {
	cfg := &Config{BotToken: "tok", DmPolicy: DmPolicyAllowlist, AllowFrom: AllowList{Wildcard: true}}
	msg := &Message{From: &User{ID: 999}, Chat: Chat{Type: "private"}}
	r := CheckAccess(cfg, msg)
	if !r.Allowed {
		t.Error("expected allowed with wildcard allowlist")
	}
}

func TestCheckAccess_DmPolicyPairing_Paired(t *testing.T) {
	cfg := &Config{BotToken: "tok", DmPolicy: DmPolicyPairing, AllowFrom: AllowList{IDs: []int64{42}}}
	msg := &Message{From: &User{ID: 42}, Chat: Chat{Type: "private"}}
	r := CheckAccess(cfg, msg)
	if !r.Allowed {
		t.Error("expected allowed for paired user")
	}
}

func TestCheckAccess_DmPolicyPairing_NotPaired(t *testing.T) {
	cfg := &Config{BotToken: "tok", DmPolicy: DmPolicyPairing, AllowFrom: AllowList{IDs: []int64{42}}}
	msg := &Message{From: &User{ID: 99}, Chat: Chat{Type: "private"}}
	r := CheckAccess(cfg, msg)
	if r.Allowed {
		t.Error("expected denied for unpaired user")
	}
}

func TestCheckAccess_DmPolicyDefault_Pairing(t *testing.T) {
	// No dmPolicy set → defaults to "pairing".
	cfg := &Config{BotToken: "tok"}
	msg := &Message{From: &User{ID: 1}, Chat: Chat{Type: "private"}}
	r := CheckAccess(cfg, msg)
	if r.Allowed {
		t.Error("expected denied with default pairing policy and empty allowlist")
	}
}

// --- Per-DM override tests ---

func TestCheckAccess_PerDmOverride_Disabled(t *testing.T) {
	cfg := &Config{
		BotToken: "tok",
		DmPolicy: DmPolicyOpen,
		Direct: map[string]*DirectConfig{
			"100": {Enabled: boolPtr(false)},
		},
	}
	msg := &Message{From: &User{ID: 1}, Chat: Chat{ID: 100, Type: "private"}}
	r := CheckAccess(cfg, msg)
	if r.Allowed {
		t.Error("expected denied for disabled DM chat")
	}
}

func TestCheckAccess_PerDmOverride_PolicyOverride(t *testing.T) {
	cfg := &Config{
		BotToken: "tok",
		DmPolicy: DmPolicyOpen, // Account says open.
		Direct: map[string]*DirectConfig{
			"100": {DmPolicy: DmPolicyDisabled}, // But this DM is disabled.
		},
	}
	msg := &Message{From: &User{ID: 1}, Chat: Chat{ID: 100, Type: "private"}}
	r := CheckAccess(cfg, msg)
	if r.Allowed {
		t.Error("expected denied for per-DM disabled override")
	}
}

func TestCheckAccess_PerDmOverride_AllowlistWithChatLevel(t *testing.T) {
	cfg := &Config{
		BotToken: "tok",
		DmPolicy: DmPolicyDisabled, // Account disabled.
		Direct: map[string]*DirectConfig{
			"100": {DmPolicy: DmPolicyAllowlist, AllowFrom: AllowList{IDs: []int64{7}}},
		},
	}
	// User 7 in chat 100 should be allowed via per-DM override.
	msg := &Message{From: &User{ID: 7}, Chat: Chat{ID: 100, Type: "private"}}
	r := CheckAccess(cfg, msg)
	if !r.Allowed {
		t.Errorf("expected allowed via per-DM allowlist, reason: %s", r.Reason)
	}

	// User 99 should be denied.
	msg.From.ID = 99
	r = CheckAccess(cfg, msg)
	if r.Allowed {
		t.Error("expected denied for user not in per-DM allowlist")
	}
}

// --- Group policy tests ---

func TestCheckAccess_GroupPolicyDisabled(t *testing.T) {
	cfg := &Config{BotToken: "tok", GroupPolicy: GroupPolicyDisabled}
	msg := &Message{From: &User{ID: 1}, Chat: Chat{ID: -100, Type: "group"}}
	r := CheckAccess(cfg, msg)
	if r.Allowed {
		t.Error("expected denied with group policy disabled")
	}
}

func TestCheckAccess_GroupPolicyOpen(t *testing.T) {
	cfg := &Config{BotToken: "tok", GroupPolicy: GroupPolicyOpen}
	msg := &Message{From: &User{ID: 1}, Chat: Chat{ID: -100, Type: "group"}}
	r := CheckAccess(cfg, msg)
	if !r.Allowed {
		t.Error("expected allowed with group policy open")
	}
}

func TestCheckAccess_GroupPolicyDefault_Open(t *testing.T) {
	// No groupPolicy set → defaults to "open".
	cfg := &Config{BotToken: "tok"}
	msg := &Message{From: &User{ID: 1}, Chat: Chat{ID: -100, Type: "supergroup"}}
	r := CheckAccess(cfg, msg)
	if !r.Allowed {
		t.Error("expected allowed with default open group policy")
	}
}

func TestCheckAccess_GroupPolicyAllowlist_WithGroupAllowFrom(t *testing.T) {
	cfg := &Config{
		BotToken:       "tok",
		GroupPolicy:    GroupPolicyAllowlist,
		GroupAllowFrom: AllowList{IDs: []int64{42}},
	}
	msg := &Message{From: &User{ID: 42}, Chat: Chat{ID: -100, Type: "group"}}
	r := CheckAccess(cfg, msg)
	if !r.Allowed {
		t.Error("expected allowed for user in groupAllowFrom")
	}

	msg.From.ID = 99
	r = CheckAccess(cfg, msg)
	if r.Allowed {
		t.Error("expected denied for user not in groupAllowFrom")
	}
}

func TestCheckAccess_GroupPolicyAllowlist_FallbackToAllowFrom(t *testing.T) {
	cfg := &Config{
		BotToken:    "tok",
		GroupPolicy: GroupPolicyAllowlist,
		AllowFrom:   AllowList{IDs: []int64{42}}, // Falls back to DM allowlist.
	}
	msg := &Message{From: &User{ID: 42}, Chat: Chat{ID: -100, Type: "group"}}
	r := CheckAccess(cfg, msg)
	if !r.Allowed {
		t.Error("expected allowed via allowFrom fallback")
	}
}

func TestCheckAccess_GroupPolicyAllowlist_Username(t *testing.T) {
	cfg := &Config{
		BotToken:       "tok",
		GroupPolicy:    GroupPolicyAllowlist,
		GroupAllowFrom: AllowList{Usernames: []string{"alice"}},
	}
	msg := &Message{From: &User{ID: 1, Username: "Alice"}, Chat: Chat{ID: -100, Type: "group"}}
	r := CheckAccess(cfg, msg)
	if !r.Allowed {
		t.Error("expected case-insensitive username match in group")
	}
}

// --- Per-group override tests ---

func TestCheckAccess_PerGroupDisabled(t *testing.T) {
	cfg := &Config{
		BotToken:    "tok",
		GroupPolicy: GroupPolicyOpen,
		Groups: map[string]*GroupConfig{
			"-100": {Enabled: boolPtr(false)},
		},
	}
	msg := &Message{From: &User{ID: 1}, Chat: Chat{ID: -100, Type: "group"}}
	r := CheckAccess(cfg, msg)
	if r.Allowed {
		t.Error("expected denied for disabled group")
	}
}

func TestCheckAccess_PerGroupPolicyOverride(t *testing.T) {
	cfg := &Config{
		BotToken:    "tok",
		GroupPolicy: GroupPolicyOpen,
		Groups: map[string]*GroupConfig{
			"-100": {GroupPolicy: GroupPolicyDisabled},
		},
	}
	msg := &Message{From: &User{ID: 1}, Chat: Chat{ID: -100, Type: "group"}}
	r := CheckAccess(cfg, msg)
	if r.Allowed {
		t.Error("expected denied for per-group disabled override")
	}
}

func TestCheckAccess_PerGroupAllowlist(t *testing.T) {
	cfg := &Config{
		BotToken:    "tok",
		GroupPolicy: GroupPolicyDisabled,
		Groups: map[string]*GroupConfig{
			"-100": {GroupPolicy: GroupPolicyAllowlist, AllowFrom: AllowList{IDs: []int64{7}}},
		},
	}
	msg := &Message{From: &User{ID: 7}, Chat: Chat{ID: -100, Type: "group"}}
	r := CheckAccess(cfg, msg)
	if !r.Allowed {
		t.Errorf("expected allowed via per-group allowlist, reason: %s", r.Reason)
	}

	msg.From.ID = 99
	r = CheckAccess(cfg, msg)
	if r.Allowed {
		t.Error("expected denied for user not in per-group allowlist")
	}
}

// --- Per-topic override tests ---

func TestCheckAccess_TopicOverride_Disabled(t *testing.T) {
	cfg := &Config{
		BotToken:    "tok",
		GroupPolicy: GroupPolicyOpen,
		Groups: map[string]*GroupConfig{
			"-100": {
				Topics: map[string]*TopicConfig{
					"5": {Enabled: boolPtr(false)},
				},
			},
		},
	}
	msg := &Message{
		From:            &User{ID: 1},
		Chat:            Chat{ID: -100, Type: "supergroup"},
		MessageThreadID: 5,
	}
	r := CheckAccess(cfg, msg)
	if r.Allowed {
		t.Error("expected denied for disabled topic")
	}
}

func TestCheckAccess_TopicOverride_PolicyOverride(t *testing.T) {
	cfg := &Config{
		BotToken:    "tok",
		GroupPolicy: GroupPolicyOpen,
		Groups: map[string]*GroupConfig{
			"-100": {
				GroupPolicy: GroupPolicyOpen,
				Topics: map[string]*TopicConfig{
					"5": {GroupPolicy: GroupPolicyAllowlist, AllowFrom: AllowList{IDs: []int64{42}}},
				},
			},
		},
	}
	// User 42 in topic 5 → allowed.
	msg := &Message{
		From:            &User{ID: 42},
		Chat:            Chat{ID: -100, Type: "supergroup"},
		MessageThreadID: 5,
	}
	r := CheckAccess(cfg, msg)
	if !r.Allowed {
		t.Errorf("expected allowed for user in topic allowlist, reason: %s", r.Reason)
	}

	// User 99 in topic 5 → denied.
	msg.From.ID = 99
	r = CheckAccess(cfg, msg)
	if r.Allowed {
		t.Error("expected denied for user not in topic allowlist")
	}

	// User 99 in a different topic → falls through to group policy (open).
	msg.MessageThreadID = 10
	r = CheckAccess(cfg, msg)
	if !r.Allowed {
		t.Errorf("expected allowed in unconfigured topic with open group policy, reason: %s", r.Reason)
	}
}

func TestCheckAccess_TopicFallbackToGroupAllowFrom(t *testing.T) {
	cfg := &Config{
		BotToken:    "tok",
		GroupPolicy: GroupPolicyAllowlist,
		Groups: map[string]*GroupConfig{
			"-100": {
				AllowFrom: AllowList{IDs: []int64{42}},
				Topics: map[string]*TopicConfig{
					"5": {GroupPolicy: GroupPolicyAllowlist},
				},
			},
		},
	}
	// Topic has allowlist policy but no own allowFrom → falls back to group's.
	msg := &Message{
		From:            &User{ID: 42},
		Chat:            Chat{ID: -100, Type: "supergroup"},
		MessageThreadID: 5,
	}
	r := CheckAccess(cfg, msg)
	if !r.Allowed {
		t.Errorf("expected allowed via group allowFrom fallback, reason: %s", r.Reason)
	}
}

// --- Config helper tests ---

func TestConfig_PolicyDefaults(t *testing.T) {
	cfg := &Config{}

	if cfg.IsEnabled() != true {
		t.Error("expected enabled by default")
	}
	if cfg.EffectiveDmPolicy() != DmPolicyPairing {
		t.Error("expected default dm policy to be pairing")
	}
	if cfg.EffectiveGroupPolicy() != GroupPolicyOpen {
		t.Error("expected default group policy to be open")
	}
	if cfg.EffectiveStreamingMode() != StreamingOff {
		t.Error("expected default streaming to be off")
	}
	if cfg.EffectiveChunkMode() != ChunkModeLength {
		t.Error("expected default chunk mode to be length")
	}
	if cfg.EffectiveReplyToMode() != ReplyToOff {
		t.Error("expected default replyTo mode to be off")
	}
	if cfg.EffectiveReactionLevel() != ReactionAck {
		t.Error("expected default reaction level to be ack")
	}
	if cfg.IsConfigWritesEnabled() != true {
		t.Error("expected config writes enabled by default")
	}
	if cfg.EffectiveLinkPreview() != true {
		t.Error("expected link preview enabled by default")
	}
	if cfg.EffectiveTextChunkLimit() != TextChunkLimit {
		t.Errorf("expected default text chunk limit %d, got %d", TextChunkLimit, cfg.EffectiveTextChunkLimit())
	}
}

func TestConfig_PolicyOverrides(t *testing.T) {
	cfg := &Config{
		Enabled:        boolPtr(false),
		DmPolicy:       DmPolicyOpen,
		GroupPolicy:    GroupPolicyAllowlist,
		Streaming:      StreamingPartial,
		BlockStreaming: boolPtr(true),
		ChunkMode:      ChunkModeNewline,
		ReplyToMode:    ReplyToAll,
		ReactionLevel:  ReactionExtensive,
		ConfigWrites:   boolPtr(false),
		LinkPreview:    boolPtr(false),
		TextChunkLimit: 2000,
	}

	if cfg.IsEnabled() {
		t.Error("expected disabled")
	}
	if cfg.EffectiveDmPolicy() != DmPolicyOpen {
		t.Error("expected open")
	}
	if cfg.EffectiveGroupPolicy() != GroupPolicyAllowlist {
		t.Error("expected allowlist")
	}
	if cfg.EffectiveStreamingMode() != StreamingPartial {
		t.Error("expected partial")
	}
	if !cfg.IsBlockStreamingDisabled() {
		t.Error("expected block streaming disabled when set to true")
	}
	if cfg.EffectiveChunkMode() != ChunkModeNewline {
		t.Error("expected newline")
	}
	if cfg.EffectiveReplyToMode() != ReplyToAll {
		t.Error("expected all")
	}
	if cfg.EffectiveReactionLevel() != ReactionExtensive {
		t.Error("expected extensive")
	}
	if cfg.IsConfigWritesEnabled() {
		t.Error("expected config writes disabled")
	}
	if cfg.EffectiveLinkPreview() {
		t.Error("expected link preview disabled")
	}
	if cfg.EffectiveTextChunkLimit() != 2000 {
		t.Errorf("expected 2000, got %d", cfg.EffectiveTextChunkLimit())
	}
}
