package contactsdedup

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
)

type fakeCompleter struct {
	responses []string // returned in call order
	err       error
	calls     int
	lastUser  string
}

func (f *fakeCompleter) Complete(_ context.Context, req llm.ChatRequest) (string, error) {
	i := f.calls
	f.calls++
	for _, m := range req.Messages {
		f.lastUser = m.Content.String() // JSON-encoded user text; names appear literally
	}
	if f.err != nil {
		return "", f.err
	}
	if i < len(f.responses) {
		return f.responses[i], nil
	}
	return "", nil
}

func pairsOf(n int) ([]contacts.Contact, []contacts.AmbiguousPair) {
	cs := make([]contacts.Contact, 2*n)
	pairs := make([]contacts.AmbiguousPair, n)
	for i := 0; i < n; i++ {
		cs[2*i] = contacts.Contact{Name: "A", Phones: []string{"010"}}
		cs[2*i+1] = contacts.Contact{Name: "B", Phones: []string{"010"}}
		pairs[i] = contacts.AmbiguousPair{A: 2 * i, B: 2*i + 1, Shared: "010"}
	}
	return cs, pairs
}

func TestAdjudicateParsesVerdicts(t *testing.T) {
	cs, pairs := pairsOf(3)
	fc := &fakeCompleter{responses: []string{
		`{"verdicts":[{"id":0,"verdict":"SAME"},{"id":1,"verdict":"DIFF"},{"id":2,"verdict":"UNSURE"}]}`,
	}}
	got, err := New(fc, "glm").Adjudicate(context.Background(), cs, pairs)
	if err != nil {
		t.Fatal(err)
	}
	want := []contacts.Verdict{contacts.VerdictSame, contacts.VerdictDifferent, contacts.VerdictUnsure}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("verdict[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestAdjudicateBatches(t *testing.T) {
	cs, pairs := pairsOf(20) // > 2 * batchSize
	fc := &fakeCompleter{}   // empty responses -> all Unsure, but must still batch
	if _, err := New(fc, "glm").Adjudicate(context.Background(), cs, pairs); err != nil {
		t.Fatal(err)
	}
	wantCalls := (20 + batchSize - 1) / batchSize
	if fc.calls != wantCalls {
		t.Errorf("calls = %d, want %d (batchSize=%d)", fc.calls, wantCalls, batchSize)
	}
}

func TestAdjudicateClientErrorDegradesToUnsure(t *testing.T) {
	cs, pairs := pairsOf(2)
	got, err := New(&fakeCompleter{err: errors.New("down")}, "glm").Adjudicate(context.Background(), cs, pairs)
	if err != nil {
		t.Fatalf("client error must not hard-fail: %v", err)
	}
	for i, v := range got {
		if v != contacts.VerdictUnsure {
			t.Errorf("verdict[%d] = %d, want Unsure on error", i, v)
		}
	}
}

func TestAdjudicateStripsProsePreamble(t *testing.T) {
	cs, pairs := pairsOf(1)
	fc := &fakeCompleter{responses: []string{
		"판정 결과입니다:\n{\"verdicts\":[{\"id\":0,\"verdict\":\"SAME\"}]}\n이상입니다.",
	}}
	got, _ := New(fc, "glm").Adjudicate(context.Background(), cs, pairs)
	if got[0] != contacts.VerdictSame {
		t.Errorf("prose-wrapped JSON not parsed: got %d", got[0])
	}
}

func TestAdjudicateUnparseableIsUnsure(t *testing.T) {
	cs, pairs := pairsOf(1)
	got, _ := New(&fakeCompleter{responses: []string{"완전히 JSON 아님"}}, "glm").Adjudicate(context.Background(), cs, pairs)
	if got[0] != contacts.VerdictUnsure {
		t.Errorf("unparseable must be Unsure, got %d", got[0])
	}
}

func TestNewNilWhenMisconfigured(t *testing.T) {
	if New(nil, "glm") != nil {
		t.Error("nil client -> nil adjudicator")
	}
	if New(&fakeCompleter{}, "  ") != nil {
		t.Error("blank model -> nil adjudicator")
	}
}

func TestAdjudicateSendsNamesInPrompt(t *testing.T) {
	cs := []contacts.Contact{
		{Name: "강상민", Org: "브라이트"},
		{Name: "브라이트강상민"},
	}
	pairs := []contacts.AmbiguousPair{{A: 0, B: 1, Shared: "01099998888"}}
	fc := &fakeCompleter{responses: []string{`{"verdicts":[{"id":0,"verdict":"SAME"}]}`}}
	New(fc, "glm").Adjudicate(context.Background(), cs, pairs)
	if !strings.Contains(fc.lastUser, "강상민") || !strings.Contains(fc.lastUser, "브라이트강상민") {
		t.Errorf("prompt missing contact names: %s", fc.lastUser)
	}
}
