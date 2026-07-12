package chat

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

type replyDeliverySequenceTranscript struct {
	events *[]string
}

func (*replyDeliverySequenceTranscript) Load(string, int) ([]ChatMessage, int, error) {
	return nil, 0, nil
}

func (t *replyDeliverySequenceTranscript) Append(string, ChatMessage) error {
	*t.events = append(*t.events, "transcript")
	return nil
}
func (*replyDeliverySequenceTranscript) Delete(string) error         { return nil }
func (*replyDeliverySequenceTranscript) ListKeys() ([]string, error) { return nil, nil }
func (*replyDeliverySequenceTranscript) Search(string, int) ([]SearchResult, error) {
	return nil, nil
}
func (*replyDeliverySequenceTranscript) CloneRecent(string, string, int) error { return nil }

func TestDeliverRunReply_PreservesDecisionAndSideEffectOrder(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	params := RunParams{
		SessionKey: "client:main",
		Delivery: &DeliveryContext{
			Channel:    "client",
			MessageID:  "incoming-message",
			DraftMsgID: "streamed-draft",
		},
	}

	t.Run("visible reply precedes media failure event and transcript note", func(t *testing.T) {
		var events []string
		deps := runDeps{
			chatport: chatportAdapters{ParseReplyDirectives: func(raw, currentID, silentToken string) chatport.ReplyDirectives {
				events = append(events, "parse:"+raw+":"+currentID+":"+silentToken)
				return chatport.ReplyDirectives{
					Text:         "  최종 답변  ",
					MediaURLs:    []string{"media://one"},
					AudioAsVoice: true,
				}
			}},
			callbacks: CallbackSnapshot{
				replyFunc: func(_ context.Context, _ *DeliveryContext, text string) error {
					events = append(events, "reply:"+text)
					return nil
				},
				mediaSendFn: func(_ context.Context, _ *DeliveryContext, url, mediaType, _ string, _ bool) error {
					events = append(events, "media:"+url+":"+mediaType)
					return errors.New("media failed")
				},
				draftEditFn: func(context.Context, *DeliveryContext, string, string) (string, error) {
					events = append(events, "draft-edit")
					return "", nil
				},
				deleteMsgFn: func(context.Context, *DeliveryContext, string) error {
					events = append(events, "draft-delete")
					return nil
				},
			},
			broadcast: func(event string, _ any) (int, []error) {
				events = append(events, "event:"+event)
				return 0, nil
			},
		}
		deps.transcript = &replyDeliverySequenceTranscript{events: &events}

		deliverRunReply(params, deps, &agent.AgentResult{Text: "raw", StopReason: "end_turn"}, false, false, logger)

		want := []string{
			"parse:raw:incoming-message:",
			"reply:최종 답변",
			"media:media://one:voice",
			"event:chat.media_delivery_failed",
			"transcript",
		}
		if !reflect.DeepEqual(events, want) {
			t.Fatalf("side-effect order = %#v, want %#v", events, want)
		}
	})

	t.Run("directive silence preserves draft and still sends media", func(t *testing.T) {
		var events []string
		deps := runDeps{
			chatport: chatportAdapters{ParseReplyDirectives: func(_, _, _ string) chatport.ReplyDirectives {
				events = append(events, "parse")
				return chatport.ReplyDirectives{
					IsSilent:  true,
					MediaURLs: []string{"media://silent"},
				}
			}},
			callbacks: CallbackSnapshot{
				replyFunc: func(context.Context, *DeliveryContext, string) error {
					events = append(events, "reply")
					return nil
				},
				mediaSendFn: func(_ context.Context, _ *DeliveryContext, url, mediaType, _ string, _ bool) error {
					events = append(events, "media:"+url+":"+mediaType)
					return nil
				},
				draftEditFn: func(context.Context, *DeliveryContext, string, string) (string, error) {
					events = append(events, "draft-edit")
					return "", nil
				},
				deleteMsgFn: func(context.Context, *DeliveryContext, string) error {
					events = append(events, "draft-delete")
					return nil
				},
			},
		}

		deliverRunReply(params, deps, &agent.AgentResult{Text: "silent-with-media"}, false, false, logger)

		want := []string{"parse", "media:media://silent:"}
		if !reflect.DeepEqual(events, want) {
			t.Fatalf("silent side effects = %#v, want %#v (no reply or draft mutation)", events, want)
		}
	})

	t.Run("missing reply callback emits failure before transcript note", func(t *testing.T) {
		var events []string
		deps := runDeps{
			chatport: chatportAdapters{ParseReplyDirectives: func(_, _, _ string) chatport.ReplyDirectives {
				events = append(events, "parse")
				return chatport.ReplyDirectives{Text: "답변"}
			}},
			broadcast: func(event string, _ any) (int, []error) {
				events = append(events, "event:"+event)
				return 0, nil
			},
		}
		deps.transcript = &replyDeliverySequenceTranscript{events: &events}

		deliverRunReply(params, deps, &agent.AgentResult{Text: "raw"}, false, false, logger)

		want := []string{"parse", "event:chat.delivery_failed", "transcript"}
		if !reflect.DeepEqual(events, want) {
			t.Fatalf("failure side-effect order = %#v, want %#v", events, want)
		}
	})

	t.Run("reply failure retries once before event and transcript note", func(t *testing.T) {
		var events []string
		deps := runDeps{
			chatport: chatportAdapters{ParseReplyDirectives: func(_, _, _ string) chatport.ReplyDirectives {
				events = append(events, "parse")
				return chatport.ReplyDirectives{Text: "답변"}
			}},
			callbacks: CallbackSnapshot{replyFunc: func(context.Context, *DeliveryContext, string) error {
				events = append(events, "reply")
				return errors.New("reply failed")
			}},
			broadcast: func(event string, _ any) (int, []error) {
				events = append(events, "event:"+event)
				return 0, nil
			},
		}
		deps.transcript = &replyDeliverySequenceTranscript{events: &events}

		deliverRunReply(params, deps, &agent.AgentResult{Text: "raw"}, false, false, logger)

		want := []string{"parse", "reply", "reply", "event:chat.delivery_failed", "transcript"}
		if !reflect.DeepEqual(events, want) {
			t.Fatalf("retry side-effect order = %#v, want %#v", events, want)
		}
	})

	t.Run("empty abnormal result uses fallback before directive parsing", func(t *testing.T) {
		var events []string
		deps := runDeps{
			chatport: chatportAdapters{ParseReplyDirectives: func(_, _, _ string) chatport.ReplyDirectives {
				events = append(events, "parse")
				return chatport.ReplyDirectives{}
			}},
			callbacks: CallbackSnapshot{replyFunc: func(_ context.Context, _ *DeliveryContext, text string) error {
				events = append(events, "fallback:"+text)
				return nil
			}},
		}

		deliverRunReply(params, deps, &agent.AgentResult{StopReason: "timeout"}, false, false, logger)

		want := []string{"fallback:" + fallbackForStopReason("timeout")}
		if !reflect.DeepEqual(events, want) {
			t.Fatalf("empty-result order = %#v, want %#v", events, want)
		}
	})
}
