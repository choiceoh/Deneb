package recall

import (
	"strings"
	"testing"
)

// The routing table's contract is the recall-bench residue (#4238): the exact
// questions page search structurally cannot answer must route, and ordinary
// project questions must not.
func TestRoutingHintMatchesBenchResidue(t *testing.T) {
	routed := map[string]string{
		// ledger totals
		"SunKean 솔라케이블 견적 총액이 얼마였지?": "deal_ledger",
		// capacity aggregates / superlatives
		"올해 하반기 착공 파이프라인 총 용량이 얼마지?":          "전수",
		"하반기 착공 파이프라인에서 제일 큰 프로젝트가 뭐고 몇 MW야?": "전수",
		// temporal mail
		"비금도 케이블 관련 마지막 메일이 온 게 언제였지?": "mail_archive",
		// graph enumeration
		"ZTT가 엮여 있는 프로젝트가 뭐지?": "graphify",
	}
	for msg, want := range routed {
		hint := routingHint(msg)
		if hint == "" {
			t.Errorf("expected a routing hint for %q", msg)
			continue
		}
		if !strings.Contains(hint, want) {
			t.Errorf("hint for %q = %q, want mention of %q", msg, hint, want)
		}
	}

	// Ordinary lookups stay hint-free — the common path must not change bytes.
	for _, msg := range []string{
		"기아 오토랜드 화성 건 진행상황 알려줘",
		"완도 관산포 태양광 담당자가 누구지?",
		"어제 회의에서 뭐 정했지?",
		"",
	} {
		if hint := routingHint(msg); hint != "" {
			t.Errorf("unexpected hint for %q: %q", msg, hint)
		}
	}
}

// The hint rides only on an already-emitted block, outside the fence.
func TestAppendRoutingHintPlacement(t *testing.T) {
	block := recallContextOpenTag + "\n내용\n" + recallContextCloseTag
	out := appendRoutingHint(block, "SunKean 견적 총액 얼마였지?")
	if !strings.HasPrefix(out, block) {
		t.Fatalf("hint must append after the block, got %q", out)
	}
	tail := strings.TrimPrefix(out, block)
	if !strings.Contains(tail, "[회상 라우팅]") || strings.Contains(tail, recallContextCloseTag) {
		t.Errorf("hint must sit outside the fence, got tail %q", tail)
	}

	// Silent turns stay silent: an empty block gains no bytes.
	if got := appendRoutingHint("", "견적 총액 얼마?"); got != "" {
		t.Errorf("empty block must stay empty, got %q", got)
	}
}
