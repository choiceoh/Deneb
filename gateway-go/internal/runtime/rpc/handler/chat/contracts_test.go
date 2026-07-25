package chat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func requestFrame(id, params string) *protocol.RequestFrame {
	return &protocol.RequestFrame{ID: id, Params: json.RawMessage(params)}
}

func decodeResponsePayload[T any](t *testing.T, resp *protocol.ResponseFrame) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("decode payload %q: %v", resp.Payload, err)
	}
	return out
}

func requireRPCErrorCode(t *testing.T, resp *protocol.ResponseFrame, code string) {
	t.Helper()
	if resp == nil || resp.OK || resp.Error == nil {
		t.Fatalf("response = %+v, want error %s", resp, code)
	}
	if resp.Error.Code != code {
		t.Fatalf("error code = %s, want %s: %+v", resp.Error.Code, code, resp.Error)
	}
}

func TestBtwMethodsContract(t *testing.T) {
	methods := BtwMethods(BtwDeps{})
	if len(methods) != 1 || methods["chat.btw"] == nil {
		t.Fatalf("methods = %+v", methods)
	}

	tests := []struct {
		name     string
		params   string
		wantCode string
	}{
		{name: "malformed json", params: `{`, wantCode: protocol.ErrInvalidRequest},
		{name: "missing question", params: `{"sessionKey":"s"}`, wantCode: protocol.ErrMissingParam},
		{name: "blank question", params: `{"question":"  \n ","sessionKey":"s"}`, wantCode: protocol.ErrMissingParam},
		{name: "missing session", params: `{"question":"q"}`, wantCode: protocol.ErrMissingParam},
		{name: "blank session", params: `{"question":"q","sessionKey":" \t "}`, wantCode: protocol.ErrMissingParam},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := methods["chat.btw"](context.Background(), requestFrame("id", tt.params))
			requireRPCErrorCode(t, resp, tt.wantCode)
		})
	}

	resp := methods["chat.btw"](context.Background(), requestFrame("id", `{"question":"q","sessionKey":"s"}`))
	requireRPCErrorCode(t, resp, protocol.ErrUnavailable)
}

type recordingBtwHandler struct {
	mu       sync.Mutex
	session  string
	question string
	result   string
	err      error
}

func (h *recordingBtwHandler) HandleBtw(_ context.Context, session, question string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.session = session
	h.question = question
	return h.result, h.err
}

func TestBtwHandlerTrimsInputAndBroadcastsContract(t *testing.T) {
	handler := &recordingBtwHandler{result: "answer"}
	var event string
	var payload map[string]any
	methods := BtwMethods(BtwDeps{
		Chat: handler,
		Broadcaster: func(gotEvent string, gotPayload events.EventPayload) (int, []error) {
			event = gotEvent
			_ = json.Unmarshal(gotPayload.Bytes(), &payload)
			return 1, nil
		},
	})
	resp := methods["chat.btw"](context.Background(), requestFrame("req-1", `{"question":"  what now?  ","sessionKey":"  client:main  "}`))
	if !resp.OK {
		t.Fatalf("response = %+v", resp)
	}
	handler.mu.Lock()
	if handler.question != "what now?" || handler.session != "client:main" {
		t.Fatalf("handler input = %q/%q", handler.session, handler.question)
	}
	handler.mu.Unlock()
	got := decodeResponsePayload[struct {
		Text string `json:"text"`
	}](t, resp)
	if got.Text != "answer" {
		t.Fatalf("text = %q", got.Text)
	}
	if event != "chat.side_result" {
		t.Fatalf("event = %q", event)
	}
	if payload["kind"] != "btw" || payload["sessionKey"] != "client:main" || payload["question"] != "what now?" || payload["text"] != "answer" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestBtwHandlerDependencyErrorDoesNotBroadcast(t *testing.T) {
	handler := &recordingBtwHandler{err: errors.New("model down")}
	broadcasts := 0
	method := BtwMethods(BtwDeps{
		Chat: handler,
		Broadcaster: func(string, events.EventPayload) (int, []error) {
			broadcasts++
			return 0, nil
		},
	})["chat.btw"]
	resp := method(context.Background(), requestFrame("id", `{"question":"q","sessionKey":"s"}`))
	requireRPCErrorCode(t, resp, protocol.ErrDependencyFailed)
	if broadcasts != 0 {
		t.Fatalf("broadcasts = %d", broadcasts)
	}
}

func TestMethodsAndMiniappMethodsNilContract(t *testing.T) {
	if got := Methods(Deps{}); got != nil {
		t.Fatalf("Methods nil chat = %+v", got)
	}
	if got := MiniappMethods(Deps{}); got != nil {
		t.Fatalf("MiniappMethods nil chat = %+v", got)
	}
}

func TestWebTranslateHandlerContract(t *testing.T) {
	t.Run("success preserves request order", func(t *testing.T) {
		var gotSegments []string
		var gotTarget string
		method := handleMiniappWebTranslate(Deps{Translate: func(_ context.Context, segments []string, target string) ([]string, error) {
			gotSegments = append([]string(nil), segments...)
			gotTarget = target
			return []string{"하나", "둘", "셋"}, nil
		}})
		resp := method(context.Background(), requestFrame("id", `{"segments":["one","two","three"],"targetLang":"ko"}`))
		if !resp.OK {
			t.Fatalf("response = %+v", resp)
		}
		if !reflect.DeepEqual(gotSegments, []string{"one", "two", "three"}) || gotTarget != "ko" {
			t.Fatalf("translator input = %v/%q", gotSegments, gotTarget)
		}
		got := decodeResponsePayload[struct {
			Translated []string `json:"translated"`
		}](t, resp)
		if !reflect.DeepEqual(got.Translated, []string{"하나", "둘", "셋"}) {
			t.Fatalf("translations = %v", got.Translated)
		}
	})

	tests := []struct {
		name      string
		params    string
		translate func(context.Context, []string, string) ([]string, error)
		wantCode  string
	}{
		{name: "malformed params", params: `{`, translate: func(context.Context, []string, string) ([]string, error) { return nil, nil }, wantCode: protocol.ErrInvalidRequest},
		{name: "missing segments", params: `{}`, translate: func(context.Context, []string, string) ([]string, error) { return nil, nil }, wantCode: protocol.ErrMissingParam},
		{name: "empty segments", params: `{"segments":[]}`, translate: func(context.Context, []string, string) ([]string, error) { return nil, nil }, wantCode: protocol.ErrMissingParam},
		{name: "dependency failure", params: `{"segments":["one"]}`, translate: func(context.Context, []string, string) ([]string, error) { return nil, errors.New("down") }, wantCode: protocol.ErrDependencyFailed},
		{name: "too few translations", params: `{"segments":["one","two"]}`, translate: func(context.Context, []string, string) ([]string, error) { return []string{"하나"}, nil }, wantCode: protocol.ErrDependencyFailed},
		{name: "too many translations", params: `{"segments":["one"]}`, translate: func(context.Context, []string, string) ([]string, error) { return []string{"하나", "둘"}, nil }, wantCode: protocol.ErrDependencyFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method := handleMiniappWebTranslate(Deps{Translate: tt.translate})
			resp := method(context.Background(), requestFrame("id", tt.params))
			requireRPCErrorCode(t, resp, tt.wantCode)
		})
	}
}

func TestCaptureImageValidationContract(t *testing.T) {
	var seen []byte
	method := handleMiniappCaptureImage(Deps{OcrImage: func(_ context.Context, image []byte) (string, error) {
		seen = append([]byte(nil), image...)
		return "", nil
	}})
	tests := []struct {
		name     string
		params   string
		wantCode string
	}{
		{name: "malformed", params: `{`, wantCode: protocol.ErrInvalidRequest},
		{name: "missing", params: `{}`, wantCode: protocol.ErrMissingParam},
		{name: "blank", params: `{"image":"  "}`, wantCode: protocol.ErrMissingParam},
		{name: "invalid base64", params: `{"image":"%%%"}`, wantCode: protocol.ErrInvalidRequest},
		{name: "empty decoded", params: `{"image":""}`, wantCode: protocol.ErrMissingParam},
		{name: "no text", params: `{"image":"aW1hZ2U="}`, wantCode: protocol.ErrUnavailable},
		{name: "data uri no text", params: `{"image":"data:image/png;base64,aW1hZ2U="}`, wantCode: protocol.ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := method(context.Background(), requestFrame("id", tt.params))
			requireRPCErrorCode(t, resp, tt.wantCode)
		})
	}
	if string(seen) != "image" {
		t.Fatalf("decoded image = %q", seen)
	}

	errMethod := handleMiniappCaptureImage(Deps{OcrImage: func(context.Context, []byte) (string, error) {
		return "", errors.New("ocr down")
	}})
	resp := errMethod(context.Background(), requestFrame("id", `{"image":"aW1hZ2U="}`))
	requireRPCErrorCode(t, resp, protocol.ErrDependencyFailed)
}

func TestCaptureDocumentValidationContract(t *testing.T) {
	var gotData []byte
	var gotFilename, gotMIME string
	method := handleMiniappCaptureDocument(Deps{ExtractDocument: func(_ context.Context, data []byte, filename, mime string) string {
		gotData = append([]byte(nil), data...)
		gotFilename = filename
		gotMIME = mime
		return ""
	}})
	tests := []struct {
		name     string
		params   string
		wantCode string
	}{
		{name: "malformed", params: `{`, wantCode: protocol.ErrInvalidRequest},
		{name: "missing", params: `{}`, wantCode: protocol.ErrMissingParam},
		{name: "blank", params: `{"document":"  "}`, wantCode: protocol.ErrMissingParam},
		{name: "invalid base64", params: `{"document":"%%%"}`, wantCode: protocol.ErrInvalidRequest},
		{name: "no text", params: `{"document":"ZG9j","filename":"a.pdf","mimeType":"application/pdf"}`, wantCode: protocol.ErrUnavailable},
		{name: "data uri no text", params: `{"document":"data:application/pdf;base64,ZG9j","filename":"a.pdf"}`, wantCode: protocol.ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := method(context.Background(), requestFrame("id", tt.params))
			requireRPCErrorCode(t, resp, tt.wantCode)
		})
	}
	if string(gotData) != "doc" || gotFilename != "a.pdf" {
		t.Fatalf("extractor input = %q/%q/%q", gotData, gotFilename, gotMIME)
	}
}

func TestCaptureAudioValidationAndHotwordContract(t *testing.T) {
	var gotAudio []byte
	var gotMIME, gotHotwords string
	method := handleMiniappCaptureAudio(Deps{
		Hotwords: func() string { return "Deneb, SolarPrime" },
		Transcribe: func(_ context.Context, audio []byte, mime, hotwords string) (string, error) {
			gotAudio = append([]byte(nil), audio...)
			if mime != "" {
				gotMIME = mime
			}
			gotHotwords = hotwords
			return "", nil
		},
	})
	tests := []struct {
		name     string
		params   string
		wantCode string
	}{
		{name: "malformed", params: `{`, wantCode: protocol.ErrInvalidRequest},
		{name: "missing", params: `{}`, wantCode: protocol.ErrMissingParam},
		{name: "blank", params: `{"audio":"  "}`, wantCode: protocol.ErrMissingParam},
		{name: "invalid base64", params: `{"audio":"%%%"}`, wantCode: protocol.ErrInvalidRequest},
		{name: "no speech", params: `{"audio":"YXVkaW8=","mimeType":"audio/wav"}`, wantCode: protocol.ErrUnavailable},
		{name: "data uri no speech", params: `{"audio":"data:audio/wav;base64,YXVkaW8="}`, wantCode: protocol.ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := method(context.Background(), requestFrame("id", tt.params))
			requireRPCErrorCode(t, resp, tt.wantCode)
		})
	}
	if string(gotAudio) != "audio" || gotMIME != "audio/wav" || gotHotwords != "Deneb, SolarPrime" {
		t.Fatalf("transcriber input = %q/%q/%q", gotAudio, gotMIME, gotHotwords)
	}
	errMethod := handleMiniappCaptureAudio(Deps{Transcribe: func(context.Context, []byte, string, string) (string, error) {
		return "", errors.New("asr down")
	}})
	resp := errMethod(context.Background(), requestFrame("id", `{"audio":"YXVkaW8="}`))
	requireRPCErrorCode(t, resp, protocol.ErrDependencyFailed)
}

func TestEventIngestHandlerContract(t *testing.T) {
	type ingested struct{ typ, source, text string }
	var events []ingested
	method := handleMiniappEventIngest(Deps{IngestEvent: func(typ, source, text string) {
		events = append(events, ingested{typ: typ, source: source, text: text})
	}})
	for _, tt := range []struct {
		name     string
		params   string
		wantCode string
	}{
		{name: "malformed", params: `{`, wantCode: protocol.ErrInvalidRequest},
		{name: "missing text", params: `{}`, wantCode: protocol.ErrMissingParam},
		{name: "blank text", params: `{"text":" \n "}`, wantCode: protocol.ErrMissingParam},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before := len(events)
			resp := method(context.Background(), requestFrame("id", tt.params))
			requireRPCErrorCode(t, resp, tt.wantCode)
			if len(events) != before {
				t.Fatal("invalid event was ingested")
			}
		})
	}
	resp := method(context.Background(), requestFrame("id", `{"type":"notification","source":"Kakao","text":"  important body  "}`))
	if !resp.OK {
		t.Fatalf("response = %+v", resp)
	}
	if !reflect.DeepEqual(events, []ingested{{typ: "notification", source: "Kakao", text: "important body"}}) {
		t.Fatalf("events = %+v", events)
	}
	got := decodeResponsePayload[map[string]string](t, resp)
	if got["status"] != "accepted" {
		t.Fatalf("payload = %+v", got)
	}
}

type contractFeed struct {
	mu         sync.Mutex
	items      []workfeed.Item
	listErr    error
	appendErr  error
	correctErr error
	rewriteErr error
}

func (f *contractFeed) Append(item workfeed.Item) (workfeed.Item, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.appendErr != nil {
		return workfeed.Item{}, f.appendErr
	}
	f.items = append(f.items, item)
	return item, nil
}

func (f *contractFeed) List(_ int, _ bool) ([]workfeed.Item, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, 0, f.listErr
	}
	return append([]workfeed.Item(nil), f.items...), len(f.items), nil
}

func (f *contractFeed) Correct(id, note string) (workfeed.Item, error) {
	if f.correctErr != nil {
		return workfeed.Item{}, f.correctErr
	}
	return workfeed.Item{ID: id, Body: note}, nil
}

func (f *contractFeed) Rewrite(id, newBody string) (workfeed.Item, error) {
	if f.rewriteErr != nil {
		return workfeed.Item{}, f.rewriteErr
	}
	return workfeed.Item{ID: id, Body: newBody}, nil
}

func TestRecordAndFindWorkFeedContract(t *testing.T) {
	recordWorkFeed(Deps{}, workfeed.Item{ID: "ignored"})
	feed := &contractFeed{}
	deps := Deps{WorkFeed: feed}
	recordWorkFeed(deps, workfeed.Item{ID: "a", Title: "A"})
	recordWorkFeed(deps, workfeed.Item{ID: "b", Title: "B"})
	if len(feed.items) != 2 {
		t.Fatalf("items = %+v", feed.items)
	}
	got, ok := findWorkFeedItem(deps, "b")
	if !ok || got.Title != "B" {
		t.Fatalf("find b = %+v/%v", got, ok)
	}
	if got, ok := findWorkFeedItem(deps, "missing"); ok || !reflect.DeepEqual(got, workfeed.Item{}) {
		t.Fatalf("find missing = %+v/%v", got, ok)
	}
	feed.listErr = errors.New("read failed")
	if _, ok := findWorkFeedItem(deps, "a"); ok {
		t.Fatal("list error reported found")
	}
	feed.listErr = nil
	feed.appendErr = errors.New("write failed")
	// Best-effort append failure must not panic or alter existing items.
	recordWorkFeed(deps, workfeed.Item{ID: "c"})
	if len(feed.items) != 2 {
		t.Fatalf("append failure changed items = %+v", feed.items)
	}
}

func TestAlreadyCardedThisTurnContract(t *testing.T) {
	now := time.Now().UnixMilli()
	feed := &contractFeed{items: []workfeed.Item{
		{ID: "old", Source: workfeed.SourceDocAnalysis, SessionKey: "client:main", CreatedAtMs: now - 10},
		{ID: "other-session", Source: workfeed.SourceDocAnalysis, SessionKey: "client:other", CreatedAtMs: now + 1},
		{ID: "wrong-source", Source: workfeed.SourceCaptureDocument, SessionKey: "client:main", CreatedAtMs: now + 1},
		{ID: "match", Source: workfeed.SourceDocAnalysis, SessionKey: "client:main", CreatedAtMs: now},
	}}
	deps := Deps{WorkFeed: feed}
	if !alreadyCardedThisTurn(deps, "client:main", now) {
		t.Fatal("matching card was not detected")
	}
	if alreadyCardedThisTurn(deps, "client:main", now+1) {
		t.Fatal("strictly older card matched")
	}
	if alreadyCardedThisTurn(Deps{}, "client:main", now) {
		t.Fatal("nil feed matched")
	}
	feed.listErr = errors.New("failed")
	if alreadyCardedThisTurn(deps, "client:main", now) {
		t.Fatal("list failure matched")
	}
}

func TestCardCapturedDocumentFallbackAndPublishContract(t *testing.T) {
	result := &chatport.SyncResult{Text: "analysis body", DeliverableText: "final body", BestText: "final body"}
	t.Run("nil feed is safe", func(t *testing.T) {
		cardCapturedDocument(Deps{}, "client:main", result, 0)
	})
	t.Run("raw fallback", func(t *testing.T) {
		feed := &contractFeed{}
		cardCapturedDocument(Deps{WorkFeed: feed}, "client:main", result, 0)
		if len(feed.items) != 1 {
			t.Fatalf("items = %+v", feed.items)
		}
		item := feed.items[0]
		if item.Source != workfeed.SourceCaptureDocument || item.Title != "공유 문서" || item.Body != result.BestText || item.SessionKey != "client:main" {
			t.Fatalf("item = %+v", item)
		}
	})
	t.Run("successful deliverable suppresses raw", func(t *testing.T) {
		feed := &contractFeed{}
		publishedBody := ""
		cardCapturedDocument(Deps{
			WorkFeed: feed,
			PublishDeliverable: func(body string) (bool, error) {
				publishedBody = body
				return true, nil
			},
		}, "client:main", result, 0)
		if publishedBody != result.BestText || len(feed.items) != 0 {
			t.Fatalf("published=%q items=%+v", publishedBody, feed.items)
		}
	})
	t.Run("publish error falls back raw", func(t *testing.T) {
		feed := &contractFeed{}
		cardCapturedDocument(Deps{
			WorkFeed:           feed,
			PublishDeliverable: func(string) (bool, error) { return false, errors.New("down") },
		}, "client:main", result, 0)
		if len(feed.items) != 1 || feed.items[0].Source != workfeed.SourceCaptureDocument {
			t.Fatalf("items = %+v", feed.items)
		}
	})
}

func TestOriginalWorkFeedCardRenderingContract(t *testing.T) {
	tests := []struct {
		name string
		card workfeed.Item
		want string
	}{
		{name: "empty", card: workfeed.Item{}, want: "## 원본 카드\n"},
		{name: "title", card: workfeed.Item{Title: "  Title  "}, want: "## 원본 카드\n제목: Title\n"},
		{name: "body wins summary", card: workfeed.Item{Title: "Title", Body: "  Body  ", Summary: "Summary"}, want: "## 원본 카드\n제목: Title\nBody\n"},
		{name: "summary fallback", card: workfeed.Item{Summary: "  Summary  "}, want: "## 원본 카드\nSummary\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b strings.Builder
			writeOriginalWorkFeedCard(&b, tt.card)
			if got := b.String(); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWorkfeedFeedbackMessageContract(t *testing.T) {
	card := workfeed.Item{Title: "Risk report", Body: "Old analysis"}
	got := buildWorkfeedFeedbackMessage(card, "The amount is 12, not 10")
	for _, want := range []string{
		"사용자가",
		"wiki 도구",
		"사용자 직접 정정",
		"## 원본 카드",
		"제목: Risk report",
		"Old analysis",
		"## 사용자 피드백",
		"The amount is 12, not 10",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("message missing %q:\n%s", want, got)
		}
	}
	if strings.Index(got, "Old analysis") > strings.Index(got, "The amount is 12") {
		t.Fatal("feedback appeared before original card")
	}
}

func TestWorkfeedRewriteMessageContract(t *testing.T) {
	card := workfeed.Item{Title: "Report", Summary: "Summary only"}
	got := buildWorkfeedRewriteMessage(card)
	for _, want := range []string{
		"더 명확하고 정돈된 구조",
		"다시 쓴 분석 본문만",
		"## 원본 카드",
		"제목: Report",
		"Summary only",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("message missing %q:\n%s", want, got)
		}
	}
}

func TestContactsSummaryContract(t *testing.T) {
	tests := []struct {
		name    string
		saved   int
		enrich  wiki.ContactEnrichResult
		want    []string
		notWant []string
	}{
		{name: "none", saved: 0, want: []string{"0개", "저장"}, notWant: []string{"위키 인물"}},
		{name: "saved only", saved: 12, enrich: wiki.ContactEnrichResult{Matched: 2}, want: []string{"12개"}, notWant: []string{"위키 인물"}},
		{name: "one enriched", saved: 12, enrich: wiki.ContactEnrichResult{Updated: 1, Names: []string{"Jane"}}, want: []string{"12개", "위키 인물 1명", "Jane"}},
		{name: "six names no overflow", saved: 10, enrich: wiki.ContactEnrichResult{Updated: 6, Names: []string{"A", "B", "C", "D", "E", "F"}}, want: []string{"A, B, C, D, E, F"}, notWant: []string{"외"}},
		{name: "seven names overflow", saved: 10, enrich: wiki.ContactEnrichResult{Updated: 7, Names: []string{"A", "B", "C", "D", "E", "F", "G"}}, want: []string{"A, B, C, D, E, F", "외 1명"}, notWant: []string{"G"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contactsSummary(tt.saved, tt.enrich)
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q: %s", want, got)
				}
			}
			for _, notWant := range tt.notWant {
				if strings.Contains(got, notWant) {
					t.Errorf("unexpected %q: %s", notWant, got)
				}
			}
		})
	}
}

func TestCaptureContactsAdditionalValidationContract(t *testing.T) {
	t.Run("enrich only succeeds", func(t *testing.T) {
		var payload []byte
		method := handleMiniappCaptureContacts(Deps{EnrichContacts: func(raw []byte) (wiki.ContactEnrichResult, error) {
			payload = append([]byte(nil), raw...)
			return wiki.ContactEnrichResult{Total: 1, Matched: 1, Updated: 1, Names: []string{"Jane"}}, nil
		}})
		resp := method(context.Background(), requestFrame("id", `{"contacts":[{"name":"Jane"}],"sessionKey":" client:other "}`))
		if !resp.OK || !json.Valid(payload) {
			t.Fatalf("response/payload = %+v/%q", resp, payload)
		}
		got := decodeResponsePayload[struct {
			Saved    int `json:"saved"`
			Enriched int `json:"enriched"`
			Matched  int `json:"matched"`
			Total    int `json:"total"`
		}](t, resp)
		if got.Saved != 0 || got.Enriched != 1 || got.Matched != 1 || got.Total != 1 {
			t.Fatalf("payload = %+v", got)
		}
	})

	t.Run("save receives exact envelope", func(t *testing.T) {
		var payload []byte
		method := handleMiniappCaptureContacts(Deps{SaveContacts: func(raw []byte) (int, error) {
			payload = append([]byte(nil), raw...)
			return 2, nil
		}})
		resp := method(context.Background(), requestFrame("id", `{"contacts":[{"name":"A"},{"name":"B"}]}`))
		if !resp.OK {
			t.Fatalf("response = %+v", resp)
		}
		if string(payload) != `{"contacts":[{"name":"A"},{"name":"B"}]}` {
			t.Fatalf("payload = %s", payload)
		}
	})

	t.Run("save failure short circuits enrich", func(t *testing.T) {
		enriched := false
		method := handleMiniappCaptureContacts(Deps{
			SaveContacts: func([]byte) (int, error) { return 0, errors.New("disk full") },
			EnrichContacts: func([]byte) (wiki.ContactEnrichResult, error) {
				enriched = true
				return wiki.ContactEnrichResult{}, nil
			},
		})
		resp := method(context.Background(), requestFrame("id", `{"contacts":[]}`))
		requireRPCErrorCode(t, resp, protocol.ErrDependencyFailed)
		if enriched {
			t.Fatal("enrichment ran after save failure")
		}
	})
}

func TestBuildTodayFeedDigestHandlesBoundaryAndWhitespaceCases(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, loc)
	start := time.Date(2026, 7, 11, 0, 0, 0, 0, loc).UnixMilli()
	tests := []struct {
		name        string
		items       []workfeed.Item
		wantParts   []string
		wantBullets int
	}{
		{name: "exact midnight included", items: []workfeed.Item{{Title: "Midnight", CreatedAtMs: start}}, wantParts: []string{"Midnight"}, wantBullets: 1},
		{name: "one ms before excluded", items: []workfeed.Item{{Title: "Old", CreatedAtMs: start - 1}}, wantBullets: 0},
		{name: "summary only", items: []workfeed.Item{{Summary: "Summary", CreatedAtMs: start + 1}}, wantParts: []string{"- Summary"}, wantBullets: 1},
		{name: "blank item skipped", items: []workfeed.Item{{Title: " \n ", Summary: "\t", CreatedAtMs: start + 1}}, wantBullets: 0},
		{name: "whitespace collapsed", items: []workfeed.Item{{Title: "A\n B", Summary: " C\tD ", CreatedAtMs: start + 1}}, wantParts: []string{"A B: C D"}, wantBullets: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildTodayFeedDigest(tt.items, now)
			for _, want := range tt.wantParts {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q: %q", want, got)
				}
			}
			if count := bulletCount(got); count != tt.wantBullets {
				t.Fatalf("bullets = %d, want %d: %q", count, tt.wantBullets, got)
			}
		})
	}
}

func TestBase64FixturesDecodeToExpectedPlaintext(t *testing.T) {
	for plain, encoded := range map[string]string{
		"image": "aW1hZ2U=",
		"audio": "YXVkaW8=",
		"doc":   "ZG9j",
	} {
		got, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || string(got) != plain {
			t.Fatalf("fixture %q = %q/%v", encoded, got, err)
		}
	}
}

func TestContractFeedConcurrentAppend(t *testing.T) {
	feed := &contractFeed{}
	const workers = 8
	const each = 25
	var wg sync.WaitGroup
	for worker := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range each {
				_, err := feed.Append(workfeed.Item{ID: fmt.Sprintf("%d-%d", worker, i)})
				if err != nil {
					t.Errorf("append: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
	items, total, err := feed.List(0, true)
	if err != nil || total != workers*each || len(items) != workers*each {
		t.Fatalf("items = %d/%d/%v", len(items), total, err)
	}
}
