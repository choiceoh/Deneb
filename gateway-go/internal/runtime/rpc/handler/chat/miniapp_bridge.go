package chat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
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
	// Contacts sync stores the whole address book (phone lookup / name search / ASR
	// hotwords) and, as a bonus, enriches existing wiki people. Either dependency is
	// enough to register; skip the method cleanly only when both are absent.
	if deps.SaveContacts != nil || deps.EnrichContacts != nil {
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
		recordWorkFeed(deps, workfeed.Item{
			Source:     workfeed.SourceCaptureImage,
			Title:      "공유 이미지",
			Summary:    workfeed.Preview(res.BestText(), 180),
			Body:       res.BestText(),
			SessionKey: sessionKey,
		})
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
		recordWorkFeed(deps, workfeed.Item{
			Source:     workfeed.SourceCaptureAudio,
			Title:      "공유 녹음",
			Summary:    workfeed.Preview(res.BestText(), 180),
			Body:       res.BestText(),
			SessionKey: sessionKey,
		})
		return rpcutil.RespondOK(req.ID, map[string]any{
			"text":       res.Text,
			"transcript": strings.TrimSpace(transcript),
			"model":      res.Model,
			"sessionKey": sessionKey,
		})
	}
}

// handleMiniappCaptureContacts stores a shared address book into the contacts
// mirror — the native client's "sync my contacts" path. The full book (thousands
// of entries) is saved so the agent can answer "whose number is this?", run name
// search, and bias ASR toward the user's proper nouns. As a bonus it also enriches
// EXISTING wiki 사람 (people) pages whose name matches a contact, writing the
// phone/email/org into that page's "## 연락처" section (the wiki itself stays
// curated — no contact pages are created). No agent turn runs; the reply is a
// short Korean summary the native client shows inline.
//
// Params:
//   - contacts ([]{name, phones[], emails[], org}, required)
func handleMiniappCaptureContacts(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Contacts   json.RawMessage `json:"contacts"`
			SessionKey string          `json:"sessionKey"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if len(p.Contacts) == 0 {
			return rpcerr.MissingParam("contacts").Response(req.ID)
		}
		// Re-wrap the array into the {"contacts": ...} envelope both SaveContacts
		// and EnrichContacts parse.
		payload := make([]byte, 0, len(p.Contacts)+13)
		payload = append(payload, []byte(`{"contacts":`)...)
		payload = append(payload, p.Contacts...)
		payload = append(payload, '}')

		// Primary path: persist the whole book to the contacts store.
		saved := 0
		if deps.SaveContacts != nil {
			n, err := deps.SaveContacts(payload)
			if err != nil {
				return rpcerr.WrapDependencyFailed("contacts save failed", err).Response(req.ID)
			}
			saved = n
		}

		// Bonus path: enrich matching wiki people. Best-effort — a wiki failure
		// must not fail the sync once the book is already stored.
		var enrich wiki.ContactEnrichResult
		if deps.EnrichContacts != nil {
			if res, err := deps.EnrichContacts(payload); err == nil {
				enrich = res
			}
		}
		sessionKey := strings.TrimSpace(p.SessionKey)
		if sessionKey == "" {
			sessionKey = nativeClientChannel + ":main"
		}
		text := contactsSummary(saved, enrich)
		recordWorkFeed(deps, workfeed.Item{
			Source:     workfeed.SourceCaptureContacts,
			Title:      "주소록 동기화",
			Summary:    text,
			Body:       text,
			SessionKey: sessionKey,
		})

		return rpcutil.RespondOK(req.ID, map[string]any{
			"text":     text,
			"saved":    saved,
			"enriched": enrich.Updated,
			"matched":  enrich.Matched,
			"total":    enrich.Total,
		})
	}
}

func recordWorkFeed(deps Deps, item workfeed.Item) {
	if deps.WorkFeed == nil {
		return
	}
	_, _ = deps.WorkFeed.Append(item)
}

// contactsSummary renders a short Korean summary of an address-book sync for the
// native client to show inline. The store save is the headline; wiki enrichment,
// when any people were updated, is appended as a parenthetical bonus.
func contactsSummary(saved int, enrich wiki.ContactEnrichResult) string {
	msg := fmt.Sprintf("📇 주소록 %d개를 저장했습니다. 이제 '이 번호 누구?' 검색과 회의 전사 고유명사 교정에 활용됩니다.", saved)
	if enrich.Updated == 0 {
		return msg
	}
	const maxShown = 6
	shown := enrich.Names
	extra := 0
	if len(shown) > maxShown {
		extra = len(shown) - maxShown
		shown = shown[:maxShown]
	}
	tail := ""
	if extra > 0 {
		tail = fmt.Sprintf(" 외 %d명", extra)
	}
	return msg + fmt.Sprintf(" (위키 인물 %d명 보강: %s%s)", enrich.Updated, strings.Join(shown, ", "), tail)
}

// handleMiniappChatSend drives one synchronous agent turn for the native client
// and returns the reply text.
//
// Params:
//   - message    (string, required): the user message
//   - sessionKey  (string, optional): conversation key; defaults to "client:main"
//   - model       (string, optional): model override; empty uses the default
func handleMiniappChatSend(deps Deps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			SessionKey string `json:"sessionKey"`
			Message    string `json:"message"`
			Model      string `json:"model"`
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
