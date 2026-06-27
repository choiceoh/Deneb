package chat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	chatpkg "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// NativeClientChannel is the channel identity for standalone native-client chat
// turns — it keys the session, delivery, and the system prompt's runtime line.
// (deneb-ui blocks still render on the native side; the gateway no longer
// prompts the model to emit them — see PR removing the deneb-ui instructions.)
const NativeClientChannel = "client"

const nativeWorkSessionKey = NativeClientChannel + ":main"

// DefaultSessionKey normalizes the native client's optional session key onto the
// shared default conversation so the blocking and streaming bridges cannot drift.
func DefaultSessionKey(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return nativeWorkSessionKey
	}
	return sessionKey
}

// NormalizeMiniappSessionKey restricts remote native-client traffic to the
// current client-owned session namespaces. This prevents the miniapp HTTP/RPC
// surface from reading or mutating internal cron/system/background sessions.
func NormalizeMiniappSessionKey(sessionKey string) (string, error) {
	sessionKey = DefaultSessionKey(sessionKey)
	switch {
	case sessionKey == nativeWorkSessionKey:
		return sessionKey, nil
	case strings.HasPrefix(sessionKey, nativeWorkSessionKey+":"):
		return sessionKey, nil
	case strings.HasPrefix(sessionKey, "chat:") && strings.TrimPrefix(sessionKey, "chat:") != "":
		return sessionKey, nil
	default:
		return "", fmt.Errorf("sessionKey must be %q, %q, or %q", nativeWorkSessionKey, nativeWorkSessionKey+":<id>", "chat:<id>")
	}
}

// Work-feed digest bounds: read at most maxFeedDigestItems rows, keep at most
// feedDigestLineCap of today's, each trimmed to feedDigestRuneCap runes — so a
// busy day can't bloat the per-turn 업무 context.
const (
	maxFeedDigestItems = 100
	feedDigestLineCap  = 20
	feedDigestRuneCap  = 200
)

// buildTodayFeedDigest renders the work-feed items created today (Asia/Seoul)
// into a compact reference block injected on the 업무 chat tail. Returns "" when
// nothing landed today (so a quiet day adds no context, and 챗봇 turns — which
// never call this — stay context-free).
func buildTodayFeedDigest(items []workfeed.Item, now time.Time) string {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		loc = time.Local
	}
	n := now.In(loc)
	startOfDay := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, loc).UnixMilli()

	var b strings.Builder
	count := 0
	for _, it := range items {
		if it.CreatedAtMs < startOfDay {
			continue
		}
		line := strings.TrimSpace(it.Title)
		if s := strings.TrimSpace(it.Summary); s != "" {
			if line != "" {
				line += ": "
			}
			line += s
		}
		line = strings.Join(strings.Fields(line), " ") // collapse newlines/runs
		if line == "" {
			continue
		}
		if r := []rune(line); len(r) > feedDigestRuneCap {
			line = string(r[:feedDigestRuneCap]) + "…"
		}
		if count == 0 {
			b.WriteString("[오늘의 업무 피드 — 참고용] 사용자가 오늘 받은 능동 리포트·캡처 요약입니다. 질문이 이와 관련되면 활용하세요.\n")
		}
		b.WriteString("- ")
		b.WriteString(line)
		b.WriteString("\n")
		count++
		if count >= feedDigestLineCap {
			break
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

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
		"miniapp.chat.history": handleMiniappHistory(deps),
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
	// Document capture (attach a pdf/doc/spreadsheet in the chat composer) needs
	// the in-house document extractor wired; skip the method cleanly when it isn't.
	if deps.ExtractDocument != nil {
		m["miniapp.capture.document"] = handleMiniappCaptureDocument(deps)
	}
	// Web translation (in-app browser in-place translate) needs the translation
	// model role wired; skip the method cleanly when it isn't.
	if deps.Translate != nil {
		m["miniapp.web.translate"] = handleMiniappWebTranslate(deps)
	}
	// Contacts sync stores the whole address book (phone lookup / name search / ASR
	// hotwords) and, as a bonus, enriches existing wiki people. Either dependency is
	// enough to register; skip the method cleanly only when both are absent.
	if deps.SaveContacts != nil || deps.EnrichContacts != nil {
		m["miniapp.capture.contacts"] = handleMiniappCaptureContacts(deps)
	}
	// Work-feed feedback (long-press a card → 정정·피드백): annotate the card with
	// the user's correction and run one agent turn to fix the durable wiki
	// knowledge. Needs the work-feed store wired (List + Correct).
	if deps.WorkFeed != nil {
		m["miniapp.workfeed.feedback"] = handleMiniappWorkfeedFeedback(deps)
		// Rewrite (long-press a card → 다시 작성): one agent turn regenerates the
		// card's analysis and the result replaces the card body in place.
		m["miniapp.workfeed.rewrite"] = handleMiniappWorkfeedRewrite(deps)
	}
	// Notification sensing: the native NotificationListener forwards broadly-captured
	// phone events; the gateway runs the proactive judgment + relay (OTP/spam/routine
	// suppressed, signal → work feed + push). The native equivalent of the loopback
	// /api/event/ingest, which only the SSH-tunneled phone can reach.
	if deps.IngestEvent != nil {
		m["miniapp.event.ingest"] = handleMiniappEventIngest(deps)
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
		sessionKey, err := NormalizeMiniappSessionKey(p.SessionKey)
		if err != nil {
			return rpcerr.InvalidParams(err).Response(req.ID)
		}
		var savedPath string
		if deps.SaveCapture != nil {
			if rel, serr := deps.SaveCapture("image", p.Caption, text); serr != nil {
				slog.Error("capture image: raw persistence failed", "error", serr)
			} else {
				savedPath = rel
			}
		}
		message := "📷 공유 이미지에서 추출한 텍스트 (OCR):\n\n" + strings.TrimSpace(text)
		if c := strings.TrimSpace(p.Caption); c != "" {
			// The caption carries context the image itself can't (which app/sender
			// the picture came from, the notification body). Lead with it so the
			// turn analyzes the photo in light of where it originated.
			message = "📲 공유 맥락:\n" + c + "\n\n" + message
		}
		if savedPath != "" {
			message += "\n\n(원문 보관: memory/" + savedPath + ")"
		}
		res, err := deps.Chat.SendSync(ctx, sessionKey, message, "", &chatpkg.SyncOptions{
			Delivery:            &chatpkg.DeliveryContext{Channel: NativeClientChannel, To: sessionKey},
			AutoDeliveredOutput: true,
			// OCR'd text is untrusted (a malicious screenshot): block exec/gmail send if it carries promptware.
			GateUntrustedTools: true,
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

// handleMiniappCaptureDocument extracts text from a directly-attached document and
// runs one agent turn over it — the native client's "attach a pdf/doc/sheet to
// Deneb" path. Mirrors handleMiniappCaptureImage but uses the in-house document
// extractor (PDF/Excel/Word/PowerPoint/CSV/text, with a scanned-PDF / image OCR
// fallback) instead of plain image OCR.
//
// Params:
//   - document   (base64, required; an optional `data:...;base64,` prefix is stripped)
//   - filename   (string, optional): drives the extractor's format dispatch
//   - mimeType   (string, optional)
//   - sessionKey (string, optional): defaults to "client:main"
//   - caption    (string, optional): source context — e.g. the question the user
//     typed alongside the attachment. Prepended to the turn.
func handleMiniappCaptureDocument(deps Deps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Document   string `json:"document"`
			Filename   string `json:"filename"`
			MimeType   string `json:"mimeType"`
			SessionKey string `json:"sessionKey"`
			Caption    string `json:"caption"`
		}](req)
		if errResp != nil {
			return errResp
		}
		raw := strings.TrimSpace(p.Document)
		if strings.HasPrefix(raw, "data:") {
			if i := strings.IndexByte(raw, ','); i > 0 {
				raw = raw[i+1:]
			}
		}
		if raw == "" {
			return rpcerr.MissingParam("document").Response(req.ID)
		}
		data, err := base64.StdEncoding.DecodeString(raw)
		if err != nil || len(data) == 0 {
			return rpcerr.InvalidParams(fmt.Errorf("document is not valid base64")).Response(req.ID)
		}
		text := deps.ExtractDocument(ctx, data, p.Filename, p.MimeType)
		if strings.TrimSpace(text) == "" {
			return rpcerr.Unavailable("no text could be extracted from the document").Response(req.ID)
		}
		sessionKey, err := NormalizeMiniappSessionKey(p.SessionKey)
		if err != nil {
			return rpcerr.InvalidParams(err).Response(req.ID)
		}
		// Persist the raw extracted text before the turn: the agent only
		// summarizes, and the original must outlive the chat transcript.
		var savedPath string
		if deps.SaveCapture != nil {
			if rel, serr := deps.SaveCapture("document", p.Caption, text); serr != nil {
				slog.Error("capture document: raw persistence failed", "error", serr)
			} else {
				savedPath = rel
			}
		}
		header := "📄 공유 문서에서 추출한 텍스트"
		if name := strings.TrimSpace(p.Filename); name != "" {
			header += " (" + name + ")"
		}
		message := header + ":\n\n" + strings.TrimSpace(text)
		if c := strings.TrimSpace(p.Caption); c != "" {
			// The caption carries the question the user typed with the attachment;
			// lead with it so the turn analyzes the document in that light.
			message = "📲 공유 맥락:\n" + c + "\n\n" + message
		}
		if savedPath != "" {
			message += "\n\n(원문 보관: memory/" + savedPath + ")"
		}
		res, err := deps.Chat.SendSync(ctx, sessionKey, message, "", &chatpkg.SyncOptions{
			Delivery:            &chatpkg.DeliveryContext{Channel: NativeClientChannel, To: sessionKey},
			AutoDeliveredOutput: true,
			// Document content is untrusted (a malicious attachment): block exec/gmail send if it carries promptware.
			GateUntrustedTools: true,
		})
		if err != nil {
			return rpcerr.WrapDependencyFailed("chat send failed", err).Response(req.ID)
		}
		recordWorkFeed(deps, workfeed.Item{
			Source:     workfeed.SourceCaptureDocument,
			Title:      "공유 문서",
			Summary:    workfeed.Preview(res.BestText(), 180),
			Body:       res.BestText(),
			SessionKey: sessionKey,
		})
		return rpcutil.RespondOK(req.ID, map[string]any{
			"text":       res.Text,
			"document":   strings.TrimSpace(text),
			"model":      res.Model,
			"sessionKey": sessionKey,
		})
	}
}

// handleMiniappWebTranslate translates a batch of web-page text segments for the
// in-app browser's in-place translation (en/ru → ko). The native DOM walker
// sends the page's visible text segments and applies the returned — same-length,
// same-order — translations in place. No agent turn: a direct call to the
// translation model role, so it is cheap enough to run per batch as the page
// loads and mutates.
//
// Params:
//   - segments   ([]string, required): page text segments to translate
//   - targetLang (string, optional): defaults to Korean
func handleMiniappWebTranslate(deps Deps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Segments   []string `json:"segments"`
			TargetLang string   `json:"targetLang"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if len(p.Segments) == 0 {
			return rpcerr.MissingParam("segments").Response(req.ID)
		}
		translated, err := deps.Translate(ctx, p.Segments, p.TargetLang)
		if err != nil {
			return rpcerr.WrapDependencyFailed("translate failed", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"translated": translated,
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
		sessionKey, err := NormalizeMiniappSessionKey(p.SessionKey)
		if err != nil {
			return rpcerr.InvalidParams(err).Response(req.ID)
		}
		// Persist the full diarized transcript before the turn: minutes are a
		// summary, and the one number the summary dropped lives only here.
		var savedPath string
		if deps.SaveCapture != nil {
			if rel, serr := deps.SaveCapture("audio", "", transcript); serr != nil {
				slog.Error("capture audio: raw persistence failed", "error", serr)
			} else {
				savedPath = rel
			}
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
		if savedPath != "" {
			message += "\n\n(전사 원문 보관: memory/" + savedPath + " — 회의록에 이 경로를 출처로 남겨라)"
		}
		res, err := deps.Chat.SendSync(ctx, sessionKey, message, "", &chatpkg.SyncOptions{
			Delivery:            &chatpkg.DeliveryContext{Channel: NativeClientChannel, To: sessionKey},
			AutoDeliveredOutput: true,
			// Transcribed audio is untrusted input: block exec/gmail send if it carries promptware.
			GateUntrustedTools: true,
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
		sessionKey, err := NormalizeMiniappSessionKey(p.SessionKey)
		if err != nil {
			return rpcerr.InvalidParams(err).Response(req.ID)
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

func handleMiniappHistory(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		SessionKey string `json:"sessionKey"`
		Limit      int    `json:"limit,omitempty"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p params
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}
		if strings.TrimSpace(p.SessionKey) == "" {
			return rpcerr.MissingParam("sessionKey").Response(req.ID)
		}
		sessionKey, err := NormalizeMiniappSessionKey(p.SessionKey)
		if err != nil {
			return rpcerr.InvalidParams(err).Response(req.ID)
		}
		paramsJSON, err := json.Marshal(map[string]any{
			"sessionKey": sessionKey,
			"limit":      p.Limit,
		})
		if err != nil {
			return rpcerr.InvalidParams(err).Response(req.ID)
		}
		reqCopy := *req
		reqCopy.Params = paramsJSON
		return deps.Chat.History(ctx, &reqCopy)
	}
}

func recordWorkFeed(deps Deps, item workfeed.Item) {
	if deps.WorkFeed == nil {
		return
	}
	_, _ = deps.WorkFeed.Append(item)
}

// handleMiniappEventIngest queues a proactive judgment turn for a phone event from
// the native client — the NotificationListener's broad notification capture (and,
// later, context/clipboard). The native, token-authenticated equivalent of the
// loopback /api/event/ingest: the gateway does the per-type judgment + relay, so
// OTP/spam/routine alerts are suppressed (NO_REPLY) and only signal reaches the
// work feed + push. Fire-and-forget — the judgment runs async on the server
// lifecycle; the client only needs the "accepted" ack.
//
// Params:
//   - type   (string, optional): "notification" (default) / "context" / "clipboard" / "sms"
//   - source (string, optional): app/sender label (e.g. "카카오톡")
//   - text   (string, required): the notification/event body
func handleMiniappEventIngest(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Type   string `json:"type"`
			Source string `json:"source"`
			Text   string `json:"text"`
		}](req)
		if errResp != nil {
			return errResp
		}
		text := strings.TrimSpace(p.Text)
		if text == "" {
			return rpcerr.MissingParam("text").Response(req.ID)
		}
		deps.IngestEvent(p.Type, p.Source, text)
		return rpcutil.RespondOK(req.ID, map[string]any{"status": "accepted"})
	}
}

// handleMiniappWorkfeedFeedback records a user's correction on a work-feed card
// and runs one agent turn to reconcile the durable knowledge — the native
// client's "long-press a feed card → 정정·피드백" path, where the user teaches the
// agent something it got wrong or didn't know. Two effects, by design (both):
//
//  1. The card is annotated in place with the user's verbatim correction (an
//     on-card erratum), so the wrong analysis is never shown unqualified. This
//     happens first, so the correction is never lost even if the turn below fails.
//  2. One agent turn — with the wiki tool — updates the durable knowledge base
//     (인물/프로젝트/거래처/시스템 pages) so future analysis and recall reflect the fix.
//
// The turn runs ephemeral (EphemeralUser+EphemeralAssistant): a correction made
// from the feed must not land as visible messages in the client:main chat
// transcript, but the wiki write (a tool side effect) still persists.
//
// Params:
//   - itemId   (string, required): the work-feed card id
//   - feedback (string, required): the user's correction / teaching text
func handleMiniappWorkfeedFeedback(deps Deps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			ItemID   string `json:"itemId"`
			Feedback string `json:"feedback"`
		}](req)
		if errResp != nil {
			return errResp
		}
		itemID := strings.TrimSpace(p.ItemID)
		feedback := strings.TrimSpace(p.Feedback)
		if itemID == "" {
			return rpcerr.MissingParam("itemId").Response(req.ID)
		}
		if feedback == "" {
			return rpcerr.MissingParam("feedback").Response(req.ID)
		}
		// Locate the card so the turn can reconcile against the exact analysis the
		// user is correcting. List(0, true) returns every retained item (no limit,
		// includes acked/snoozed) — the card may have been acked before correcting.
		var card workfeed.Item
		found := false
		if items, _, lerr := deps.WorkFeed.List(0, true); lerr == nil {
			for _, it := range items {
				if it.ID == itemID {
					card = it
					found = true
					break
				}
			}
		}
		if !found {
			return rpcerr.NotFound("work feed item").Response(req.ID)
		}
		// 1) Annotate the card immediately (built from the pre-correction card body
		// below, so the turn still sees the original analysis).
		updated, cerr := deps.WorkFeed.Correct(itemID, feedback)
		if cerr != nil {
			return rpcerr.WrapDependencyFailed("work feed correct failed", cerr).Response(req.ID)
		}
		sessionKey := DefaultSessionKey(card.SessionKey)
		// 2) One agent turn updates the durable knowledge (wiki) from the correction.
		message := buildWorkfeedFeedbackMessage(card, feedback)
		res, serr := deps.Chat.SendSync(ctx, sessionKey, message, "", &chatpkg.SyncOptions{
			Delivery:            &chatpkg.DeliveryContext{Channel: NativeClientChannel, To: sessionKey},
			AutoDeliveredOutput: true,
			// A feed correction is a side action, not a chat message — keep it out of
			// the client:main transcript (the wiki write still persists).
			EphemeralUser:      true,
			EphemeralAssistant: true,
			// The card body can carry untrusted mail/doc content: block exec/gmail send.
			GateUntrustedTools: true,
		})
		if serr != nil {
			// The card annotation already succeeded; surface the knowledge-turn
			// failure softly but still return the annotated item so the client
			// reflects the on-card correction.
			return rpcutil.RespondOK(req.ID, map[string]any{
				"ok":         true,
				"item":       updated,
				"text":       "정정 내용을 카드에 반영했습니다. (지식 업데이트는 일시적으로 실패했어요.)",
				"sessionKey": sessionKey,
			})
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"ok":         true,
			"item":       updated,
			"text":       res.BestText(),
			"model":      res.Model,
			"sessionKey": sessionKey,
		})
	}
}

// buildWorkfeedFeedbackMessage assembles the one-turn instruction: take the user's
// correction as ground truth, fix the durable wiki knowledge, and report briefly.
// The card's on-card erratum is handled by the store, so the turn is told not to
// repeat it.
func buildWorkfeedFeedbackMessage(card workfeed.Item, feedback string) string {
	var b strings.Builder
	b.WriteString("사용자가 아래 업무 피드 카드의 분석 내용에 대해 정정·보강 피드백을 보냈다. ")
	b.WriteString("[사용자 피드백]이 사용자가 직접 알려준 정확한 지식이니 사실로 받아들여라.\n\n")
	b.WriteString("할 일:\n")
	b.WriteString("1. 관련 위키 지식(인물·프로젝트·거래처·시스템 등)을 wiki 도구로 정정하거나 보강하라. ")
	b.WriteString("기존 페이지가 있으면 고치고 없으면 적절한 카테고리에 새로 만들되, 바뀐 사실을 정확히 반영하고 ")
	b.WriteString("출처가 '사용자 직접 정정(업무 피드 피드백)'임을 남겨라.\n")
	b.WriteString("2. 무엇을 어떻게 반영했는지 한국어로 1~3줄로 간단히 보고하라. ")
	b.WriteString("(이 카드 자체의 정정 표기는 시스템이 이미 처리했으니 다시 하지 마라.)\n\n")
	b.WriteString("## 원본 카드\n")
	if t := strings.TrimSpace(card.Title); t != "" {
		b.WriteString("제목: ")
		b.WriteString(t)
		b.WriteByte('\n')
	}
	if body := strings.TrimSpace(card.Body); body != "" {
		b.WriteString(body)
		b.WriteByte('\n')
	} else if s := strings.TrimSpace(card.Summary); s != "" {
		b.WriteString(s)
		b.WriteByte('\n')
	}
	b.WriteString("\n## 사용자 피드백\n")
	b.WriteString(feedback)
	return b.String()
}

// handleMiniappWorkfeedRewrite regenerates a work-feed card's analysis and
// replaces the card body in place — the native "다시 작성" path. One ephemeral
// agent turn rewrites the analysis from the card's current content; its reply
// becomes the new body (title/summary stay, so the row preview is stable). The
// turn is ephemeral so the rewrite never lands in the client:main transcript; a
// blank rewrite is rejected so a failed turn never wipes the card.
//
// Params:
//   - itemId (string, required): the work-feed card id
func handleMiniappWorkfeedRewrite(deps Deps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			ItemID string `json:"itemId"`
		}](req)
		if errResp != nil {
			return errResp
		}
		itemID := strings.TrimSpace(p.ItemID)
		if itemID == "" {
			return rpcerr.MissingParam("itemId").Response(req.ID)
		}
		var card workfeed.Item
		found := false
		if items, _, lerr := deps.WorkFeed.List(0, true); lerr == nil {
			for _, it := range items {
				if it.ID == itemID {
					card = it
					found = true
					break
				}
			}
		}
		if !found {
			return rpcerr.NotFound("work feed item").Response(req.ID)
		}
		sessionKey := DefaultSessionKey(card.SessionKey)
		message := buildWorkfeedRewriteMessage(card)
		res, serr := deps.Chat.SendSync(ctx, sessionKey, message, "", &chatpkg.SyncOptions{
			Delivery:            &chatpkg.DeliveryContext{Channel: NativeClientChannel, To: sessionKey},
			AutoDeliveredOutput: true,
			EphemeralUser:       true,
			EphemeralAssistant:  true,
			GateUntrustedTools:  true,
		})
		if serr != nil {
			return rpcerr.WrapDependencyFailed("chat send failed", serr).Response(req.ID)
		}
		newBody := strings.TrimSpace(res.BestText())
		if newBody == "" {
			// Never wipe the card on an empty regeneration; report softly.
			return rpcutil.RespondOK(req.ID, map[string]any{
				"ok":         true,
				"item":       card,
				"text":       "다시 작성에 실패했어요. 카드는 그대로 두었습니다.",
				"sessionKey": sessionKey,
			})
		}
		updated, rerr := deps.WorkFeed.Rewrite(itemID, newBody)
		if rerr != nil {
			return rpcerr.WrapDependencyFailed("work feed rewrite failed", rerr).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"ok":         true,
			"item":       updated,
			"text":       "카드를 다시 작성했습니다.",
			"model":      res.Model,
			"sessionKey": sessionKey,
		})
	}
}

// buildWorkfeedRewriteMessage instructs the turn to regenerate the card's analysis
// from its current content — same facts, clearer structure — and to output ONLY the
// rewritten body (no preamble) so the reply can be stored directly as the new card.
func buildWorkfeedRewriteMessage(card workfeed.Item) string {
	var b strings.Builder
	b.WriteString("아래 업무 피드 카드의 분석을 다시 작성하라. 같은 사실·정보를 기반으로 하되, ")
	b.WriteString("더 명확하고 정돈된 구조로 — 핵심 상황, 근거·숫자, 지금 할 다음 행동이 잘 드러나게 한국어로 다시 써라. ")
	b.WriteString("필요하면 wiki 등 도구로 맥락을 보강해도 좋다. ")
	b.WriteString("출력은 **다시 쓴 분석 본문만** 내라 — '다시 작성했습니다' 같은 머리말·맺음말이나 메타 설명 없이 본문 마크다운만.\n\n")
	b.WriteString("## 원본 카드\n")
	if t := strings.TrimSpace(card.Title); t != "" {
		b.WriteString("제목: ")
		b.WriteString(t)
		b.WriteByte('\n')
	}
	if body := strings.TrimSpace(card.Body); body != "" {
		b.WriteString(body)
		b.WriteByte('\n')
	} else if s := strings.TrimSpace(card.Summary); s != "" {
		b.WriteString(s)
		b.WriteByte('\n')
	}
	return b.String()
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
			// SkipRecall is the native client's "focused chat / memory off"
			// toggle: when true the long-term-memory recall preflight is skipped
			// for this turn (faster, no unrelated work-context injection). The
			// persona is unchanged. Default false = full recall.
			SkipRecall bool `json:"skipRecall"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if strings.TrimSpace(p.Message) == "" {
			return rpcerr.MissingParam("message").Response(req.ID)
		}
		sessionKey, err := NormalizeMiniappSessionKey(p.SessionKey)
		if err != nil {
			return rpcerr.InvalidParams(err).Response(req.ID)
		}

		// 업무 turns (recall on) carry today's work feed as wire-only context — this
		// is what makes a 업무 chat aware of the day's proactive reports/captures,
		// versus a context-less 챗봇 chat. Best-effort: a nil store or a read error
		// just yields no context. 챗봇 turns (SkipRecall) get none, by design.
		feedCtx := ""
		if !p.SkipRecall && deps.WorkFeed != nil {
			if items, _, lerr := deps.WorkFeed.List(maxFeedDigestItems, true); lerr == nil {
				feedCtx = buildTodayFeedDigest(items, time.Now())
			}
		}

		res, err := deps.Chat.SendSync(ctx, sessionKey, p.Message, strings.TrimSpace(p.Model), &chatpkg.SyncOptions{
			Delivery: &chatpkg.DeliveryContext{Channel: NativeClientChannel, To: sessionKey},
			// The reply text is returned here, not pushed via the message tool.
			AutoDeliveredOutput: true,
			SkipRecall:          p.SkipRecall,
			FeedContext:         feedCtx,
			// Block irreversible tools (exec, gmail send) if promptware enters the turn.
			GateUntrustedTools: true,
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
