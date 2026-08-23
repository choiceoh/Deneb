package recall

import "testing"

// TestRoutingHintWithShape_NamesTheQueryClass: the hint text is prose that will
// be reworded, so telemetry keys off the shape. Without it the journal cannot
// say whether the nudge ever fires — it has been unmeasured since it landed.
func TestRoutingHintWithShape_NamesTheQueryClass(t *testing.T) {
	cases := []struct {
		message string
		shape   string
	}{
		{"sunkean 견적 총액 얼마야?", "ledger-total"},
		{"하반기 파이프라인 총 용량은?", "aggregate"},
		{"ZTT에서 마지막 메일 언제 왔어?", "temporal-mail"},
		{"ZTT랑 엮여 있는 프로젝트 전부 뭐야?", "graph-relations"},
		{"오늘 날씨 어때?", ""},
		{"", ""},
	}
	for _, c := range cases {
		hint, shape := routingHintWithShape(c.message)
		if shape != c.shape {
			t.Errorf("%q: shape = %q, want %q", c.message, shape, c.shape)
		}
		if (hint != "") != (c.shape != "") {
			t.Errorf("%q: hint/shape disagree (hint=%q shape=%q)", c.message, hint, shape)
		}
	}
}
