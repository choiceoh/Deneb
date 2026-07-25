// Package contactsdedup is the runtime bridge that resolves the address-book
// dedup's ambiguous pairs with an LLM. The pure domain (internal/domain/contacts)
// produces AmbiguousPairs and defines the Adjudicator interface; this package
// implements it against a chat model so the domain never imports the LLM stack.
//
// Only the genuinely ambiguous pairs reach the model (same personal identifier,
// incompatible names) — a fraction of the book — and they are judged in batches.
// A batch that errors or comes back unparseable degrades to Unsure, which keeps
// those entries apart: a missed merge is recoverable, a fabricated person is not.
package contactsdedup

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
)

// batchSize caps pairs per model request. Small enough that one bad batch loses
// little, large enough to amortize the round-trip.
const batchSize = 8

// maxTokens is generous because reasoning models (glm) think even with thinking
// off, so a tight budget yields an empty reply.
const maxTokens = 4000

// Completer is the narrow slice of *llm.Client this adjudicator needs.
type Completer interface {
	Complete(ctx context.Context, req llm.ChatRequest) (string, error)
}

// Adjudicator judges dedup ambiguous pairs with a chat model. It satisfies
// contacts.Adjudicator.
type Adjudicator struct {
	client Completer
	model  string
}

// New returns an Adjudicator, or nil when the client or model is missing (callers
// treat a nil adjudicator as "leave everything ambiguous").
func New(client Completer, model string) *Adjudicator {
	if client == nil || strings.TrimSpace(model) == "" {
		return nil
	}
	return &Adjudicator{client: client, model: model}
}

const systemPrompt = `너는 연락처 중복정리 판정기다. 각 쌍(A/B)은 같은 전화번호 또는 이메일을 ` +
	`공유하지만 이름 표기가 다르다. 각 쌍이 동일인인지 판정하라.
- SAME: 같은 사람. 회사명·직함·별칭·로마자·오타 변형이거나 이직으로 소속만 다른 경우.
- DIFF: 다른 사람. 회사 대표번호나 팀 공유번호를 우연히 같이 쓰는 동료 등.
- UNSURE: 판단 불가.
반드시 JSON 객체만 출력: {"verdicts":[{"id":<번호>,"verdict":"SAME|DIFF|UNSURE"}]}`

type pairView struct {
	ID     int      `json:"id"`
	Shared string   `json:"shared"`
	A      partyLLM `json:"a"`
	B      partyLLM `json:"b"`
}

type partyLLM struct {
	Name   string   `json:"name"`
	Org    string   `json:"org,omitempty"`
	Phones []string `json:"phones,omitempty"`
	Emails []string `json:"emails,omitempty"`
}

type verdictResponse struct {
	Verdicts []struct {
		ID      int    `json:"id"`
		Verdict string `json:"verdict"`
	} `json:"verdicts"`
}

// Adjudicate judges every pair, returning one Verdict per pair in order. It never
// hard-fails on model trouble: a failed or unparseable batch leaves its pairs at
// the zero value (Unsure), so the caller keeps them apart.
func (a *Adjudicator) Adjudicate(ctx context.Context, cs []contacts.Contact, pairs []contacts.AmbiguousPair) ([]contacts.Verdict, error) {
	out := make([]contacts.Verdict, len(pairs)) // zero value = VerdictUnsure
	if a == nil {
		return out, nil
	}
	for start := 0; start < len(pairs); start += batchSize {
		end := start + batchSize
		if end > len(pairs) {
			end = len(pairs)
		}
		a.judgeBatch(ctx, cs, pairs[start:end], out[start:end])
	}
	return out, nil
}

func (a *Adjudicator) judgeBatch(ctx context.Context, cs []contacts.Contact, pairs []contacts.AmbiguousPair, out []contacts.Verdict) {
	views := make([]pairView, len(pairs))
	for i, p := range pairs {
		views[i] = pairView{ID: i, Shared: p.Shared, A: party(cs, p.A), B: party(cs, p.B)}
	}
	payload, err := json.Marshal(views)
	if err != nil {
		return // leave Unsure
	}
	raw, err := a.client.Complete(ctx, llm.ChatRequest{
		Model:          a.model,
		System:         llm.SystemString(systemPrompt),
		Messages:       []llm.Message{llm.NewTextMessage("user", "판정할 쌍:\n"+string(payload))},
		MaxTokens:      maxTokens,
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		return
	}
	var resp verdictResponse
	if err := json.Unmarshal([]byte(extractJSONObject(raw)), &resp); err != nil {
		return
	}
	for _, v := range resp.Verdicts {
		if v.ID >= 0 && v.ID < len(out) {
			out[v.ID] = parseVerdict(v.Verdict)
		}
	}
}

func party(cs []contacts.Contact, i int) partyLLM {
	if i < 0 || i >= len(cs) {
		return partyLLM{}
	}
	c := cs[i]
	return partyLLM{Name: c.Name, Org: c.Org, Phones: capStrings(c.Phones, 3), Emails: capStrings(c.Emails, 2)}
}

func capStrings(ss []string, n int) []string {
	if len(ss) > n {
		return ss[:n]
	}
	return ss
}

func parseVerdict(s string) contacts.Verdict {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "SAME":
		return contacts.VerdictSame
	case "DIFF", "DIFFERENT":
		return contacts.VerdictDifferent
	default:
		return contacts.VerdictUnsure
	}
}

// extractJSONObject returns the outermost {...} so a reasoning model's prose
// preamble around the JSON does not break parsing.
func extractJSONObject(s string) string {
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return s
	}
	return s[start : end+1]
}

// assert the interface is satisfied at compile time.
var _ contacts.Adjudicator = (*Adjudicator)(nil)
