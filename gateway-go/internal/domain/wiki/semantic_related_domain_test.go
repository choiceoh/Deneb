package wiki

import "testing"

// Cases are the cross-category `related` edges the 2026-08-25 corpus audit
// found: every 프로젝트→시스템/기타 edge in the wiki was embedding noise, and every
// 프로젝트→인물/업무 edge was the knowledge graph working. The rule has to keep
// that split exactly — a broader guard would cut the 107 business edges recall
// depends on.
func TestRelatedDomainCompatibleKeepsBusinessEdgesAndDropsOffDomainOnes(t *testing.T) {
	tests := []struct {
		name string
		src  string
		dst  string
		want bool
	}{{
		name: "project mail must not link personal car hotspot page",
		src:  "프로젝트/pl2-skb-epc-001/메일분석/ac2d8bc4aeae491eb2074276d2d322ef@huawei.com.md",
		dst:  "기타/lr458313-—-차량-핫스팟-(range-rover-sport-l461).md",
		want: false,
	}, {
		name: "project mail must not link home wifi page",
		src:  "프로젝트/pl2-dsv-epc-001/메일분석/PUUP216MB343364E070578EEB5C74E935C9E22.md",
		dst:  "기타/stsjhouse-wifi-—-집-와이파이.md",
		want: false,
	}, {
		name: "project mail must not link gateway tone rule",
		src:  "프로젝트/pl2-kia-epc-003/메일분석/19ea64cd4e16abf4.md",
		dst:  "시스템/톤-규칙:-반말-사용-금지.md",
		want: false,
	}, {
		name: "project page keeps person link",
		src:  "프로젝트/pl2-dsv-epc-001/대표.md",
		dst:  "인물/차남두.md",
		want: true,
	}, {
		name: "project page keeps business topic link",
		src:  "프로젝트/pl2-dsv-epc-001/대표.md",
		dst:  "업무/2026년-전체-파이프라인.md",
		want: true,
	}, {
		name: "project page keeps sibling project link",
		src:  "프로젝트/pl2-dsv-epc-001/로그.md",
		dst:  "프로젝트/nde-ztt-cbl-001/대표.md",
		want: true,
	}, {
		// The rule is scoped to project sources: two personal pages are
		// genuinely related to each other and must stay linkable.
		name: "personal pages may link each other",
		src:  "기타/lr458313-—-차량-핫스팟-(range-rover-sport-l461).md",
		dst:  "기타/stsjhouse-wifi-—-집-와이파이.md",
		want: true,
	}, {
		name: "system pages may link each other",
		src:  "시스템/모닝레터-발송-규칙.md",
		dst:  "시스템/톤-규칙:-반말-사용-금지.md",
		want: true,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := relatedDomainCompatible(tt.src, tt.dst); got != tt.want {
				t.Fatalf("relatedDomainCompatible(%q, %q) = %v, want %v", tt.src, tt.dst, got, tt.want)
			}
		})
	}
}
