package chat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// NativeClientChannel is the channel identity for standalone native-client chat
// turns — it keys the session, delivery, and the system prompt's runtime line.
// (deneb-ui blocks still render on the native side; the gateway no longer
// prompts the model to emit them — see PR removing the deneb-ui instructions.)
const NativeClientChannel = "client"

// DefaultSessionKey normalizes the native client's optional session key onto the
// shared default conversation so the blocking and streaming bridges cannot drift.
func DefaultSessionKey(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return NativeClientChannel + ":main"
	}
	return sessionKey
}

// Work-feed digest bounds: read at most maxFeedDigestItems rows, keep at most
// feedDigestLineCap of today's, each trimmed to feedDigestRuneCap runes — so a
// busy day can't bloat the per-turn 업무 context.
const (
	maxFeedDigestItems = 100
	feedDigestLineCap  = 20
	feedDigestRuneCap  = 200
	// Keep the blocking fallback and capture-triggered turns bounded like the
	// streaming native route, so a connected client cannot strand a run forever.
	nativeSyncTurnDeadline = chatport.InteractiveTurnDeadline
)

// buildTodayFeedDigest renders the work-feed items created today (Asia/Seoul)
// into a compact reference block injected on the 업무 chat tail. Returns "" when
// nothing landed today, so a quiet day adds no context.
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
	if deps.Chat == nil || !deps.Chat.ChatReady() {
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
	// ASR sidecar wired; skip the method cleanly when it isn't.
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
		sessionKey := DefaultSessionKey(p.SessionKey)
		var savedPath string
		if deps.SaveCapture != nil {
			if rel, _, _, serr := deps.SaveCapture("image", p.Caption, text); serr != nil {
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
		res, err := sendUntrustedCapture(ctx, deps, sessionKey, message)
		if err != nil {
			return rpcerr.WrapDependencyFailed("chat send failed", err).Response(req.ID)
		}
		recordWorkFeed(deps, workfeed.Item{
			Source:     workfeed.SourceCaptureImage,
			Title:      "공유 이미지",
			Summary:    workfeed.Preview(res.BestText, 180),
			Body:       res.BestText,
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
		sessionKey := DefaultSessionKey(p.SessionKey)
		// Persist the raw extracted text before the turn: the agent only
		// summarizes, and the original must outlive the chat transcript.
		var savedPath, savedAbs string
		var savedBodyLine int
		if deps.SaveCapture != nil {
			if rel, abs, bodyLine, serr := deps.SaveCapture("document", p.Caption, text); serr != nil {
				slog.Error("capture document: raw persistence failed", "error", serr)
			} else {
				savedPath, savedAbs, savedBodyLine = rel, abs, bodyLine
			}
		}
		// Oversized documents would flood the turn's context; digest AFTER the
		// raw persistence above so the full original still outlives the digest —
		// the digest map's line numbers point into that archived file.
		if deps.DigestOversized != nil {
			text = deps.DigestOversized(ctx, p.Filename, text, savedAbs, savedBodyLine)
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
		// Turn start — the dedup window for a model-published deliverable card below.
		turnStartMs := time.Now().UnixMilli()
		res, err := sendUntrustedCapture(ctx, deps, sessionKey, message)
		if err != nil {
			return rpcerr.WrapDependencyFailed("chat send failed", err).Response(req.ID)
		}
		cardCapturedDocument(deps, sessionKey, res, turnStartMs)
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
		if len(translated) != len(p.Segments) {
			return rpcerr.WrapDependencyFailed("translate failed",
				fmt.Errorf("translator returned %d segments for %d inputs", len(translated), len(p.Segments))).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"translated": translated,
		})
	}
}

// handleMiniappCaptureAudio transcribes a directly-shared audio recording (a
// voice memo or meeting audio) via the ASR sidecar and runs one agent turn over
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
		sessionKey := DefaultSessionKey(p.SessionKey)
		// Persist the full diarized transcript before the turn: minutes are a
		// summary, and the one number the summary dropped lives only here.
		var savedPath string
		if deps.SaveCapture != nil {
			if rel, _, _, serr := deps.SaveCapture("audio", "", transcript); serr != nil {
				slog.Error("capture audio: raw persistence failed", "error", serr)
			} else {
				savedPath = rel
			}
		}
		// Drive a meeting-minutes turn, not a bare transcript dump. For a real
		// discussion the agent must LOAD the meeting-minutes skill (single source
		// of the minutes procedure — inlining a paraphrase here let the skill
		// drift unused; 30d forensics showed zero organic consults) and follow
		// it: minutes + analysis, saved to the wiki so the next meeting builds on
		// it. Short memos skip the ceremony. The skills tool is DEFERRED (absent
		// from the initial schema), so the instruction follows the system
		// prompt's fetch_tools contract — telling the model to call `skills`
		// directly made it a schema-less call some models never emit.
		message := "🎙️ 공유 녹음을 받아썼습니다 (화자분리·타임스탬프).\n\n" +
			"회의·통화·논의 녹음이면 먼저 `fetch_tools`(query=\"skills\")로 `skills` 도구를 활성화한 뒤 " +
			"`skills`(action=\"read\", name=\"meeting-minutes\")로 회의록 절차를 로드해 그대로 따르라 — 핵심 논의, " +
			"결정사항, 액션아이템(담당·기한), 리스크·후속을 빠짐없이 정리하고, 위키에 남겨 " +
			"다음에 이어보게 하라. 기한이 있는 항목은 due로 남겨 임박 알림이 챙기게 한다. " +
			"짧은 음성 메모면 스킬 없이 과하게 격식 차리지 말고 핵심만 정리하라. 한국어로.\n\n" +
			"## 전사\n" + strings.TrimSpace(transcript)
		if savedPath != "" {
			message += "\n\n(전사 원문 보관: memory/" + savedPath + " — 회의록에 이 경로를 출처로 남겨라)"
		}
		res, err := sendUntrustedCapture(ctx, deps, sessionKey, message)
		if err != nil {
			return rpcerr.WrapDependencyFailed("chat send failed", err).Response(req.ID)
		}
		recordWorkFeed(deps, workfeed.Item{
			Source:     workfeed.SourceCaptureAudio,
			Title:      "공유 녹음",
			Summary:    workfeed.Preview(res.BestText, 180),
			Body:       res.BestText,
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

func sendUntrustedCapture(ctx context.Context, deps Deps, sessionKey, message string) (*chatport.SyncResult, error) {
	return runNativeSync(ctx, deps, chatport.SyncRequest{
		SessionKey:          sessionKey,
		Message:             message,
		Delivery:            &chatport.DeliveryContext{Channel: NativeClientChannel, To: sessionKey},
		AutoDeliveredOutput: true,
		GateUntrustedTools:  true,
	})
}

func runNativeSync(ctx context.Context, deps Deps, req chatport.SyncRequest) (*chatport.SyncResult, error) {
	turnCtx, cancel := context.WithTimeout(ctx, nativeSyncTurnDeadline)
	defer cancel()
	return deps.Chat.RunSync(turnCtx, req)
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
		sessionKey := DefaultSessionKey(p.SessionKey)
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

// cardCapturedDocument files the feed card for a shared-document analysis turn. It
// prefers a proper doc_analysis deliverable card (derived title, via
// PublishDeliverable) over the raw "공유 문서" capture card, and:
//   - skips entirely when the model already published a deliverable itself this turn
//     (guidance path) — the turn is synchronous, so any such card is already in the
//     feed (alreadyCardedThisTurn), preventing a double card;
//   - falls back to the raw capture card when the analysis is too thin to be a
//     deliverable (PublishDeliverable suppressed it) or PublishDeliverable is
//     unwired, so a shared document is never silently dropped.
func cardCapturedDocument(deps Deps, sessionKey string, res *chatport.SyncResult, turnStartMs int64) {
	body := res.BestText
	if alreadyCardedThisTurn(deps, sessionKey, turnStartMs) {
		return
	}
	if deps.PublishDeliverable != nil {
		if published, _ := deps.PublishDeliverable(body); published {
			return
		}
	}
	recordWorkFeed(deps, workfeed.Item{
		Source:     workfeed.SourceCaptureDocument,
		Title:      "공유 문서",
		Summary:    workfeed.Preview(body, 180),
		Body:       body,
		SessionKey: sessionKey,
	})
}

// alreadyCardedThisTurn reports whether a doc_analysis deliverable card for this
// session was created since turnStartMs — i.e. the model published one itself
// during the (synchronous) turn, so the server must not add a duplicate.
func alreadyCardedThisTurn(deps Deps, sessionKey string, turnStartMs int64) bool {
	if deps.WorkFeed == nil {
		return false
	}
	items, _, err := deps.WorkFeed.List(10, false)
	if err != nil {
		return false
	}
	for _, it := range items {
		if it.Source == workfeed.SourceDocAnalysis && it.SessionKey == sessionKey && it.CreatedAtMs >= turnStartMs {
			return true
		}
	}
	return false
}

// handleMiniappEventIngest queues a proactive judgment turn for a phone event from
// the native client — notification capture, proactive context events, and cached
// phone-state updates. The native, token-authenticated equivalent of the loopback
// /api/event/ingest: the gateway does the per-type judgment + relay, so
// OTP/spam/routine alerts are suppressed (NO_REPLY) and only signal reaches the
// work feed + push. Cache-only event types return before any judgment turn.
// Fire-and-forget — the judgment runs async on the server lifecycle; the client
// only needs the "accepted" ack.
//
// Params:
//   - type   (string, optional): "notification" (default) / "context" / "clipboard" / "sms" / "*_update"
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
		sessionKey := DefaultSessionKey(p.SessionKey)

		// Recall-on turns carry today's work feed as wire-only context — this is
		// what makes a chat aware of the day's proactive reports/captures.
		// Best-effort: a nil store or a read error just yields no context.
		// SkipRecall turns (memory-off toggle) get none, by design.
		feedCtx := ""
		if !p.SkipRecall && deps.WorkFeed != nil {
			if items, _, lerr := deps.WorkFeed.List(maxFeedDigestItems, true); lerr == nil {
				feedCtx = buildTodayFeedDigest(items, time.Now())
			}
		}

		res, err := runNativeSync(ctx, deps, chatport.SyncRequest{
			SessionKey: sessionKey,
			Message:    p.Message,
			Model:      strings.TrimSpace(p.Model),
			Delivery:   &chatport.DeliveryContext{Channel: NativeClientChannel, To: sessionKey},
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
			"text":       res.BestText,
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
