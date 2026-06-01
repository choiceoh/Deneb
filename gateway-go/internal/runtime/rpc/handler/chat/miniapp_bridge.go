package chat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	chatpkg "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// nativeClientChannel routes standalone native-client chat turns. The chat
// pipeline's richUIChannel treats this channel as rich-UI-capable, so the agent
// emits kai-ui fences for the native app (and Telegram stays unaffected).
const nativeClientChannel = "client"

// MiniappMethods returns the miniapp-namespaced chat bridge. The standalone
// native client authenticates via the X-Deneb-Client-Token header and reaches
// the gateway through POST /api/v1/miniapp/rpc, which only admits the miniapp.*
// namespace — so chat.send (a chat.* method) is not reachable from it.
//
// Unlike chat.send (async ingestion whose reply is delivered out-of-band to a
// channel), miniapp.chat.send uses SendSync and returns the reply text in the
// RPC response, matching the native client's request/response model.
//
// Registered late (needs the chat handler); see method_registry.go.
func MiniappMethods(deps Deps) map[string]rpcutil.HandlerFunc {
	if deps.Chat == nil {
		return nil
	}
	m := map[string]rpcutil.HandlerFunc{
		"miniapp.chat.send":    handleMiniappChatSend(deps),
		"miniapp.chat.history": handleHistory(deps),
	}
	// Image capture (share a photo/screenshot to Deneb) needs the OCR sidecar
	// wired; skip the method cleanly when it isn't.
	if deps.OcrImage != nil {
		m["miniapp.capture.image"] = handleMiniappCaptureImage(deps)
	}
	// Audio capture (share a voice memo / meeting recording to Deneb) needs the
	// VibeVoice-ASR sidecar wired; skip the method cleanly when it isn't.
	if deps.Transcribe != nil {
		m["miniapp.capture.audio"] = handleMiniappCaptureAudio(deps)
	}
	// Contacts sync (enrich existing wiki people with phone/email from the shared
	// address book) needs the wiki store wired; skip the method cleanly otherwise.
	if deps.EnrichContacts != nil {
		m["miniapp.capture.contacts"] = handleMiniappCaptureContacts(deps)
	}
	return m
}

// handleMiniappCaptureImage OCRs a directly-shared image and runs one agent turn
// over the extracted text — the native client's "share an image to Deneb" path.
//
// Params:
//   - image      (base64, required; an optional `data:...;base64,` prefix is stripped)
//   - mimeType   (string, optional)
//   - sessionKey (string, optional): defaults to "client:main"
//   - caption    (string, optional): source context the image alone lacks — e.g.
//     the originating notification's app/sender/text. Prepended to the OCR turn
//     so the agent sees both the picture and where it came from.
func handleMiniappCaptureImage(deps Deps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Image      string `json:"image"`
			MimeType   string `json:"mimeType"`
			SessionKey string `json:"sessionKey"`
			Caption    string `json:"caption"`
		}](req)
		if errResp != nil {
			return errResp
		}
		raw := strings.TrimSpace(p.Image)
		if strings.HasPrefix(raw, "data:") {
			if i := strings.IndexByte(raw, ','); i > 0 {
				raw = raw[i+1:]
			}
		}
		if raw == "" {
			return rpcerr.MissingParam("image").Response(req.ID)
		}
		img, err := base64.StdEncoding.DecodeString(raw)
		if err != nil || len(img) == 0 {
			return rpcerr.InvalidParams(fmt.Errorf("image is not valid base64")).Response(req.ID)
		}
		text, err := deps.OcrImage(ctx, img)
		if err != nil {
			return rpcerr.WrapDependencyFailed("image OCR failed", err).Response(req.ID)
		}
		if strings.TrimSpace(text) == "" {
			return rpcerr.Unavailable("no text found in image").Response(req.ID)
		}
		sessionKey := strings.TrimSpace(p.SessionKey)
		if sessionKey == "" {
			sessionKey = nativeClientChannel + ":main"
		}
		message := "📷 공유 이미지에서 추출한 텍스트 (OCR):\n\n" + strings.TrimSpace(text)
		if c := strings.TrimSpace(p.Caption); c != "" {
			// The caption carries context the image itself can't (which app/sender
			// the picture came from, the notification body). Lead with it so the
			// turn analyzes the photo in light of where it originated.
			message = "📲 공유 맥락:\n" + c + "\n\n" + message
		}
		res, err := deps.Chat.SendSync(ctx, sessionKey, message, "", &chatpkg.SyncOptions{
			Delivery:            &chatpkg.DeliveryContext{Channel: nativeClientChannel, To: sessionKey},
			AutoDeliveredOutput: true,
		})
		if err != nil {
			return rpcerr.WrapDependencyFailed("chat send failed", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"text":       res.Text,
			"ocr":        strings.TrimSpace(text),
			"model":      res.Model,
			"sessionKey": sessionKey,
		})
	}
}

// handleMiniappCaptureAudio transcribes a directly-shared audio recording (a
// voice memo or meeting audio) via VibeVoice-ASR and runs one agent turn over
// the diarized transcript — the native client's "share a recording to Deneb"
// path. The transcript carries speaker labels and timestamps, so the agent can
// summarize, pull action items, or capture it to the wiki.
//
// Params:
//   - audio      (base64, required; an optional `data:...;base64,` prefix is stripped)
//   - mimeType   (string, optional): codec hint (server sniffs the real codec)
//   - sessionKey (string, optional): defaults to "client:main"
func handleMiniappCaptureAudio(deps Deps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Audio      string `json:"audio"`
			MimeType   string `json:"mimeType"`
			SessionKey string `json:"sessionKey"`
		}](req)
		if errResp != nil {
			return errResp
		}
		raw := strings.TrimSpace(p.Audio)
		if strings.HasPrefix(raw, "data:") {
			if i := strings.IndexByte(raw, ','); i > 0 {
				raw = raw[i+1:]
			}
		}
		if raw == "" {
			return rpcerr.MissingParam("audio").Response(req.ID)
		}
		audio, err := base64.StdEncoding.DecodeString(raw)
		if err != nil || len(audio) == 0 {
			return rpcerr.InvalidParams(fmt.Errorf("audio is not valid base64")).Response(req.ID)
		}
		// Bias ASR toward the user's wiki proper nouns (people, companies, deals,
		// domain terms) so Korean names aren't mis-heard.
		var hotwords string
		if deps.Hotwords != nil {
			hotwords = deps.Hotwords()
		}
		transcript, err := deps.Transcribe(ctx, audio, p.MimeType, hotwords)
		if err != nil {
			return rpcerr.WrapDependencyFailed("audio transcription failed", err).Response(req.ID)
		}
		if strings.TrimSpace(transcript) == "" {
			return rpcerr.Unavailable("no speech found in audio").Response(req.ID)
		}
		sessionKey := strings.TrimSpace(p.SessionKey)
		if sessionKey == "" {
			sessionKey = nativeClientChannel + ":main"
		}
		// Drive a meeting-minutes turn, not a bare transcript dump. The agent
		// judges meeting vs short memo (the meeting-minutes skill carries the
		// stance); for a real discussion it writes minutes + analysis and saves
		// them to the wiki so the next meeting can build on it.
		message := "🎙️ 공유 녹음을 받아썼습니다 (화자분리·타임스탬프).\n\n" +
			"회의·통화·논의 녹음이면 회의록을 작성하고 업무 관점에서 분석하라 — 핵심 논의, " +
			"결정사항, 액션아이템(담당·기한), 리스크·후속을 빠짐없이 정리하고, 위키에 남겨 " +
			"다음에 이어보게 하라. 기한이 있는 항목은 due로 남겨 임박 알림이 챙기게 한다. " +
			"짧은 음성 메모면 과하게 격식 차리지 말고 핵심만 정리하라. 한국어로.\n\n" +
			"## 전사\n" + strings.TrimSpace(transcript)
		res, err := deps.Chat.SendSync(ctx, sessionKey, message, "", &chatpkg.SyncOptions{
			Delivery:            &chatpkg.DeliveryContext{Channel: nativeClientChannel, To: sessionKey},
			AutoDeliveredOutput: true,
		})
		if err != nil {
			return rpcerr.WrapDependencyFailed("chat send failed", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"text":       res.Text,
			"transcript": strings.TrimSpace(transcript),
			"model":      res.Model,
			"sessionKey": sessionKey,
		})
	}
}

// handleMiniappCaptureContacts merges a shared address book into EXISTING wiki
// 사람 (people) pages — the native client's "sync my contacts" path. For each
// contact whose name matches a person already in the wiki, the phone/email/org
// is written into that page's "## 연락처" section; the hundreds of unmatched
// numbers in a phone book are ignored by design. This enriches "whose number is
// this?" / meeting-prep lookups and strengthens the ASR proper-noun bias (drawn
// from wiki titles + tags). No agent turn runs; the reply is a short Korean
// summary the native client shows inline.
//
// Params:
//   - contacts ([]{name, phones[], emails[], org}, required)
func handleMiniappCaptureContacts(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Contacts json.RawMessage `json:"contacts"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if len(p.Contacts) == 0 {
			return rpcerr.MissingParam("contacts").Response(req.ID)
		}
		// Re-wrap the array into the {"contacts": ...} envelope EnrichContacts parses.
		payload := make([]byte, 0, len(p.Contacts)+13)
		payload = append(payload, []byte(`{"contacts":`)...)
		payload = append(payload, p.Contacts...)
		payload = append(payload, '}')
		res, err := deps.EnrichContacts(payload)
		if err != nil {
			return rpcerr.WrapDependencyFailed("contacts enrich failed", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"text":    contactsSummary(res),
			"matched": res.Matched,
			"updated": res.Updated,
			"total":   res.Total,
		})
	}
}

// contactsSummary renders a short Korean summary of an address-book sync for the
// native client to show inline.
func contactsSummary(res wiki.ContactEnrichResult) string {
	if res.Updated == 0 {
		if res.Matched == 0 {
			return fmt.Sprintf("📇 주소록 %d개를 확인했지만 위키에 등록된 인물과 일치하는 연락처가 없어 새로 반영한 정보는 없습니다. (위키에 이미 있는 사람만 보강합니다.)", res.Total)
		}
		return fmt.Sprintf("📇 주소록 %d개 확인 — 위키 인물 %d명과 매칭됐고 이미 최신이라 변경은 없습니다.", res.Total, res.Matched)
	}
	const maxShown = 6
	shown := res.Names
	extra := 0
	if len(shown) > maxShown {
		extra = len(shown) - maxShown
		shown = shown[:maxShown]
	}
	tail := ""
	if extra > 0 {
		tail = fmt.Sprintf(" 외 %d명", extra)
	}
	return fmt.Sprintf("📇 주소록 %d개 중 위키 인물 %d명의 연락처를 보강했습니다: %s%s. 회의 전사 고유명사 교정과 인물 조회에 반영됩니다.",
		res.Total, res.Updated, strings.Join(shown, ", "), tail)
}

// handleMiniappChatSend drives one synchronous agent turn for the native client
// and returns the reply text.
//
// Params:
//   - message    (string, required): the user message
//   - sessionKey  (string, optional): conversation key; defaults to "client:main"
//   - model       (string, optional): model override; empty uses the default
//   - topicKey    (string, optional): per-topic knowledge selector (see
//     miniapp.topics.list); empty means no per-topic injection
func handleMiniappChatSend(deps Deps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			SessionKey string `json:"sessionKey"`
			Message    string `json:"message"`
			Model      string `json:"model"`
			TopicKey   string `json:"topicKey"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if strings.TrimSpace(p.Message) == "" {
			return rpcerr.MissingParam("message").Response(req.ID)
		}
		sessionKey := strings.TrimSpace(p.SessionKey)
		if sessionKey == "" {
			sessionKey = nativeClientChannel + ":main"
		}

		res, err := deps.Chat.SendSync(ctx, sessionKey, p.Message, strings.TrimSpace(p.Model), &chatpkg.SyncOptions{
			// Channel "client" flips on kai-ui emission (richUIChannel).
			Delivery: &chatpkg.DeliveryContext{Channel: nativeClientChannel, To: sessionKey},
			// Per-topic knowledge selector: the gateway maps this key back to its
			// forum threadID so injection matches the Telegram surface exactly.
			TopicKey: strings.TrimSpace(p.TopicKey),
			// The reply text is returned here, not pushed via the message tool.
			AutoDeliveredOutput: true,
		})
		if err != nil {
			return rpcerr.WrapDependencyFailed("chat send failed", err).Response(req.ID)
		}

		return rpcutil.RespondOK(req.ID, map[string]any{
			// BestText so a tool wrap-up final turn (e.g. "위키에 기록했습니다"
			// after writing the answer to the wiki) doesn't replace the real body.
			"text":       res.BestText(),
			"model":      res.Model,
			"fellBack":   res.FellBack,
			"sessionKey": sessionKey,
			"usage": map[string]int{
				"inputTokens":  res.InputTokens,
				"outputTokens": res.OutputTokens,
			},
		})
	}
}
