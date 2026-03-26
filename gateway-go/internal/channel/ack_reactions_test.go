package channel

import "testing"

func TestShouldAckReaction(t *testing.T) {
	tests := []struct {
		name string
		p    AckReactionGateParams
		want bool
	}{
		{
			name: "off scope",
			p:    AckReactionGateParams{Scope: AckScopeOff},
			want: false,
		},
		{
			name: "none scope",
			p:    AckReactionGateParams{Scope: AckScopeNone},
			want: false,
		},
		{
			name: "all scope",
			p:    AckReactionGateParams{Scope: AckScopeAll},
			want: true,
		},
		{
			name: "direct scope, is direct",
			p:    AckReactionGateParams{Scope: AckScopeDirect, IsDirect: true},
			want: true,
		},
		{
			name: "direct scope, is group",
			p:    AckReactionGateParams{Scope: AckScopeDirect, IsDirect: false, IsGroup: true},
			want: false,
		},
		{
			name: "group-all scope, is group",
			p:    AckReactionGateParams{Scope: AckScopeGroupAll, IsGroup: true},
			want: true,
		},
		{
			name: "group-all scope, is direct",
			p:    AckReactionGateParams{Scope: AckScopeGroupAll, IsGroup: false},
			want: false,
		},
		{
			name: "group-mentions, mentioned",
			p: AckReactionGateParams{
				Scope: AckScopeGroupMentions, IsGroup: true, IsMentionableGroup: true,
				RequireMention: true, CanDetectMention: true, EffectiveWasMentioned: true,
			},
			want: true,
		},
		{
			name: "group-mentions, not mentioned",
			p: AckReactionGateParams{
				Scope: AckScopeGroupMentions, IsGroup: true, IsMentionableGroup: true,
				RequireMention: true, CanDetectMention: true, EffectiveWasMentioned: false,
			},
			want: false,
		},
		{
			name: "group-mentions, bypass",
			p: AckReactionGateParams{
				Scope: AckScopeGroupMentions, IsGroup: true, IsMentionableGroup: true,
				RequireMention: true, CanDetectMention: true,
				EffectiveWasMentioned: false, ShouldBypassMention: true,
			},
			want: true,
		},
		{
			name: "group-mentions, not mentionable group",
			p: AckReactionGateParams{
				Scope: AckScopeGroupMentions, IsGroup: true, IsMentionableGroup: false,
				RequireMention: true, CanDetectMention: true, EffectiveWasMentioned: true,
			},
			want: false,
		},
		{
			name: "default scope (empty) behaves like group-mentions",
			p: AckReactionGateParams{
				Scope: "", IsGroup: true, IsMentionableGroup: true,
				RequireMention: true, CanDetectMention: true, EffectiveWasMentioned: true,
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldAckReaction(tt.p)
			if got != tt.want {
				t.Errorf("ShouldAckReaction() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRemoveAckReactionAfterReply(t *testing.T) {
	t.Run("removes when configured", func(t *testing.T) {
		removed := false
		RemoveAckReactionAfterReply(RemoveAckReactionAfterReplyParams{
			RemoveAfterReply: true,
			DidAck:           true,
			Remove:           func() error { removed = true; return nil },
		})
		if !removed {
			t.Error("expected remove to be called")
		}
	})

	t.Run("skips when not configured", func(t *testing.T) {
		removed := false
		RemoveAckReactionAfterReply(RemoveAckReactionAfterReplyParams{
			RemoveAfterReply: false,
			DidAck:           true,
			Remove:           func() error { removed = true; return nil },
		})
		if removed {
			t.Error("expected remove to not be called")
		}
	})

	t.Run("skips when not acked", func(t *testing.T) {
		removed := false
		RemoveAckReactionAfterReply(RemoveAckReactionAfterReplyParams{
			RemoveAfterReply: true,
			DidAck:           false,
			Remove:           func() error { removed = true; return nil },
		})
		if removed {
			t.Error("expected remove to not be called")
		}
	})
}
