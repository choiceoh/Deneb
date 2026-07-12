package denebui

import (
	"slices"
	"testing"
)

func TestCompositionAdvisories(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "prose dump — paragraphs only, no structure",
			body: `<column><card>
				<text>첫 번째 문단입니다. 상황을 길게 설명합니다.</text>
				<text>두 번째 문단입니다.</text>
				<text>세 번째 문단입니다.</text>
				<text>네 번째 문단입니다.</text>
			</card></column>`,
			want: []string{AdvisoryProseDump},
		},
		{
			name: "multi-card answer without a single section header",
			body: `<column>
				<card><text style="title">현황</text><ul><li>a</li></ul></card>
				<card><text style="title">일정</text><ul><li>b</li></ul></card>
			</column>`,
			want: []string{AdvisoryNoSectionHeader},
		},
		{
			name: "contract-conformant card is clean",
			body: `<column><card>
				<row><icon name="calendar" size="16"/><text style="caption">오늘 일정</text></row>
				<ul><li>10:00 — 회의</li></ul>
			</card></column>`,
			want: nil,
		},
		{
			name: "structural sibling defuses the prose count",
			body: `<column><card>
				<text>리드 문장.</text><text>둘.</text><text>셋.</text><text>넷.</text>
				<table><tr><th>a</th></tr><tr><td>1</td></tr></table>
			</card></column>`,
			want: nil,
		},
		{
			name: "single card never triggers the header advisory",
			body: `<column><card><text style="title">제목</text><ul><li>x</li></ul></card></column>`,
			want: nil,
		},
		{
			name: "cardless body is out of scope",
			body: `<column><text>그냥 텍스트</text></column>`,
			want: nil,
		},
		{
			name: "legacy JSON is out of scope",
			body: `{"type":"card","children":[]}`,
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CompositionAdvisories(tt.body)
			if !slices.Equal(got, tt.want) {
				t.Errorf("CompositionAdvisories() = %v, want %v", got, tt.want)
			}
		})
	}
}
