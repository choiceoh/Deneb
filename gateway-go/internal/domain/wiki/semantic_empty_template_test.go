package wiki

import "testing"

// The 현장 template is byte-identical across projects apart from the place name,
// so those pages are each other's nearest neighbours and the cosine floor
// cannot separate them. Bodies here are verbatim from ~/.deneb/wiki.
func TestHasOwnProseRejectsEmptyScaffolding(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{{
		name: "site template with nothing under any heading",
		body: "# 당진시 현장\n\n## 개요\n\n## 공정 현황\n\n## 이슈\n",
		want: false,
	}, {
		name: "duplicate-folded template is still empty",
		body: "# 당진시 현장\n\n## 개요\n\n## 이슈\n\n## 병합된 중복 문서 (프로젝트/pl2-tha-epc-001/현장/당진시.md)\n\n# 당진시 현장\n\n## 개요\n\n## 이슈\n",
		want: false,
	}, {
		name: "provenance blockquote alone is not prose",
		body: "# 분석\n\n> From: 이준호 <ma3112@topsolar.kr>\n> Date: Wed, 29 Jul 2026 11:54:59 +0900\n",
		want: false,
	}, {
		name: "one written line clears it",
		body: "# 당진시 현장\n\n## 개요\n\n98MW 부지 조성 완료, 2026-09 착공 예정.\n\n## 이슈\n",
		want: true,
	}, {
		name: "a table is content",
		body: "# 거래\n\n| 일자 | 금액 |\n|---|---|\n| 6/24 | 1,041억 |\n",
		want: true,
	}, {
		name: "a list is content",
		body: "# 현장\n\n## 이슈\n\n- 진입로 사용 협의 미완\n",
		want: true,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasOwnProse(tt.body); got != tt.want {
				t.Fatalf("HasOwnProse = %v, want %v", got, tt.want)
			}
		})
	}
}
