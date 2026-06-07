package chat

import (
	"strings"
	"testing"
)

// Each test uses a unique session key so the shared in-memory store doesn't bleed
// across tests (and cleans up after itself).

func TestPinFact_AddListUnpinClear(t *testing.T) {
	sk := "test:pin-basic"
	defer clearPinnedFacts(sk)

	if ok, _ := pinFact(sk, "거래처 X 담당자는 김부장"); !ok {
		t.Fatal("first pin should succeed")
	}
	if ok, _ := pinFact(sk, "마감은 6/15"); !ok {
		t.Fatal("second pin should succeed")
	}
	got := listPinnedFacts(sk)
	if len(got) != 2 || got[0] != "거래처 X 담당자는 김부장" || got[1] != "마감은 6/15" {
		t.Fatalf("unexpected list: %v", got)
	}

	removed, ok := unpinFact(sk, 1)
	if !ok || removed != "거래처 X 담당자는 김부장" {
		t.Fatalf("unpin(1) = (%q,%v), want first fact", removed, ok)
	}
	got = listPinnedFacts(sk)
	if len(got) != 1 || got[0] != "마감은 6/15" {
		t.Fatalf("after unpin: %v", got)
	}

	clearPinnedFacts(sk)
	if got := listPinnedFacts(sk); got != nil {
		t.Fatalf("after clear: want nil, got %v", got)
	}
}

func TestPinFact_RejectsEmptyDuplicateAndTooLong(t *testing.T) {
	sk := "test:pin-reject"
	defer clearPinnedFacts(sk)

	if ok, reason := pinFact(sk, "   "); ok || reason == "" {
		t.Fatal("empty fact must be rejected with a reason")
	}
	if ok, _ := pinFact(sk, "사실 A"); !ok {
		t.Fatal("valid pin should succeed")
	}
	if ok, reason := pinFact(sk, "사실 A"); ok || !strings.Contains(reason, "이미") {
		t.Fatalf("duplicate must be rejected, got ok=%v reason=%q", ok, reason)
	}
	long := strings.Repeat("가", maxPinnedFactRunes+1)
	if ok, reason := pinFact(sk, long); ok || !strings.Contains(reason, "깁니다") {
		t.Fatalf("too-long must be rejected, got ok=%v reason=%q", ok, reason)
	}
}

func TestPinFact_CapacityLimit(t *testing.T) {
	sk := "test:pin-cap"
	defer clearPinnedFacts(sk)

	for i := range maxPinnedFacts {
		if ok, reason := pinFact(sk, string(rune('a'+i))+" fact"); !ok {
			t.Fatalf("pin %d should succeed, got %q", i, reason)
		}
	}
	if ok, reason := pinFact(sk, "one too many"); ok || !strings.Contains(reason, "최대") {
		t.Fatalf("over-capacity pin must be rejected, got ok=%v reason=%q", ok, reason)
	}
	if got := listPinnedFacts(sk); len(got) != maxPinnedFacts {
		t.Fatalf("want %d facts, got %d", maxPinnedFacts, len(got))
	}
}

func TestUnpinFact_OutOfRange(t *testing.T) {
	sk := "test:pin-oor"
	defer clearPinnedFacts(sk)
	_, _ = pinFact(sk, "only one")
	if _, ok := unpinFact(sk, 0); ok {
		t.Fatal("index 0 must fail")
	}
	if _, ok := unpinFact(sk, 2); ok {
		t.Fatal("index past end must fail")
	}
	if _, ok := unpinFact(sk, 1); !ok {
		t.Fatal("valid index must succeed")
	}
}

func TestFormatPinnedFactsBlock(t *testing.T) {
	if got := formatPinnedFactsBlock(nil); got != "" {
		t.Fatalf("empty → empty, got %q", got)
	}
	got := formatPinnedFactsBlock([]string{"가", "나"})
	want := "1. 가\n2. 나"
	if got != want {
		t.Fatalf("block = %q, want %q", got, want)
	}
}

func TestRenderPinnedFactsReply(t *testing.T) {
	if got := renderPinnedFactsReply(nil); !strings.Contains(got, "없습니다") {
		t.Fatalf("empty reply should say none, got %q", got)
	}
	got := renderPinnedFactsReply([]string{"가"})
	if !strings.Contains(got, "📌") || !strings.Contains(got, "1. 가") {
		t.Fatalf("reply = %q", got)
	}
}

func TestParsePinIndex(t *testing.T) {
	cases := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"1", 1, false},
		{" 3 ", 3, false},
		{"2번", 2, false},
		{"0", 0, true},
		{"abc", 0, true},
		{"", 0, true},
	}
	for _, c := range cases {
		got, err := parsePinIndex(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parsePinIndex(%q) expected error", c.in)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("parsePinIndex(%q) = (%d,%v), want (%d,nil)", c.in, got, err, c.want)
		}
	}
}

func TestParseSlashCommand_Pin(t *testing.T) {
	cases := []struct {
		in      string
		wantCmd string
		wantArg string
	}{
		{"/pin 거래처 X 담당자는 김부장", "pin", "거래처 X 담당자는 김부장"},
		{"/고정 마감 6/15", "pin", "마감 6/15"},
		{"/unpin 2", "unpin", "2"},
		{"/고정해제 1", "unpin", "1"},
		{"/pins", "pins", ""},
		{"/고정목록", "pins", ""},
	}
	for _, c := range cases {
		got := ParseSlashCommand(c.in)
		if got == nil || !got.Handled {
			t.Fatalf("ParseSlashCommand(%q) not handled", c.in)
		}
		if got.Command != c.wantCmd || got.Args != c.wantArg {
			t.Errorf("ParseSlashCommand(%q) = cmd=%q args=%q, want cmd=%q args=%q",
				c.in, got.Command, got.Args, c.wantCmd, c.wantArg)
		}
	}
}
