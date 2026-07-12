package denebui

import (
	"reflect"
	"strings"
	"testing"
)

func TestStructuredHTMLConversionPreservesRenderMeaning(t *testing.T) {
	t.Run("chart keeps points and numeric fallback", func(t *testing.T) {
		got := mustParseHTML(t, `<chart id="trend" type="column" label="생산">`+
			`<point label="A" value="1,200톤"/>`+
			`<tab label="wrong parent"><text>ignored</text></tab>`+
			`<point label="unknown" value="n/a"/>`+
			`</chart>`)
		want := map[string]any{
			"type":      "chart",
			"id":        "trend",
			"chartType": "bar",
			"label":     "생산",
			"labels":    []any{"A", "unknown"},
			"values":    []any{1200.0, 0.0},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("chart = %#v, want %#v", got, want)
		}
	})

	t.Run("table promotes the first header-bearing row", func(t *testing.T) {
		got := mustParseHTML(t, `<table id="prices">`+
			`<tr><td>품목</td><th>가격</th></tr>`+
			`<tr><th>구리</th><td>9,540</td></tr>`+
			`</table>`)
		want := map[string]any{
			"type":    "table",
			"id":      "prices",
			"headers": []any{"품목", "가격"},
			"rows":    []any{[]any{"구리", "9,540"}},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("table = %#v, want %#v", got, want)
		}
	})

	t.Run("list selects text child or column shape", func(t *testing.T) {
		got := mustParseHTML(t, `<list id="items" ordered="false">`+
			`<li>plain</li>`+
			`<li><badge>one</badge></li>`+
			`<li><text>A</text><text>B</text></li>`+
			`</list>`)
		want := map[string]any{
			"type": "list",
			"id":   "items",
			"items": []any{
				map[string]any{"type": "text", "value": "plain"},
				map[string]any{"type": "badge", "value": "one"},
				map[string]any{"type": "column", "children": []any{
					map[string]any{"type": "text", "value": "A"},
					map[string]any{"type": "text", "value": "B"},
				}},
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("list = %#v, want %#v", got, want)
		}
		if _, exists := got["ordered"]; exists {
			t.Fatalf("ordered=false must omit the ordered flag: %#v", got)
		}
	})

	t.Run("tabs keep labels children and truncated integer selection", func(t *testing.T) {
		got := mustParseHTML(t, `<tabs id="views" selected-index="1.9">`+
			`<tab label="요약"><text>A</text></tab>`+
			`<point label="wrong parent" value="3"/>`+
			`<tab label="상세"><text>B</text><badge>C</badge></tab>`+
			`</tabs>`)
		want := map[string]any{
			"type":          "tabs",
			"id":            "views",
			"selectedIndex": 1.0,
			"tabs": []any{
				map[string]any{"label": "요약", "children": []any{map[string]any{"type": "text", "value": "A"}}},
				map[string]any{"label": "상세", "children": []any{
					map[string]any{"type": "text", "value": "B"},
					map[string]any{"type": "badge", "value": "C"},
				}},
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("tabs = %#v, want %#v", got, want)
		}
	})

	t.Run("accordion preserves explicit false and children", func(t *testing.T) {
		got := mustParseHTML(t, `<accordion id="details" title="상세" expanded="false"><text>본문</text></accordion>`)
		want := map[string]any{
			"type":     "accordion",
			"id":       "details",
			"title":    "상세",
			"expanded": false,
			"children": []any{map[string]any{"type": "text", "value": "본문"}},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("accordion = %#v, want %#v", got, want)
		}
	})
}

func TestStructuredHTMLAllowedTagFamiliesParseWithoutIssues(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "chart point", body: `<chart type="bar"><point label="A" value="1"/></chart>`},
		{name: "table row cells", body: `<table><tr><th>H</th></tr><tr><td>V</td></tr></table>`},
		{name: "unordered list item", body: `<ul><li>A</li></ul>`},
		{name: "ordered list item", body: `<ol><li>A</li></ol>`},
		{name: "explicit list item", body: `<list><li>A</li></list>`},
		{name: "tabs tab", body: `<tabs><tab label="A"><text>B</text></tab></tabs>`},
		{name: "accordion", body: `<accordion title="A"><text>B</text></accordion>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, issues := ParseHTML(tt.body)
			if root == nil || len(issues) != 0 {
				t.Fatalf("ParseHTML(%q) root=%#v issues=%v", tt.body, root, issues)
			}
		})
	}
}

func TestStructuredHTMLConversionPreservesErrorFallbacks(t *testing.T) {
	t.Run("unknown wrapper reports issue and hoists table rows", func(t *testing.T) {
		root, issues := ParseHTML(`<table><rows><tr><th>H</th></tr><tr><td>V</td></tr></rows></table>`)
		if len(issues) != 1 || !strings.Contains(issues[0].Msg, "unknown tag <rows>") {
			t.Fatalf("issues = %v, want unknown rows wrapper", issues)
		}
		got := root.(map[string]any)
		if !reflect.DeepEqual(got["headers"], []any{"H"}) || !reflect.DeepEqual(got["rows"], []any{[]any{"V"}}) {
			t.Fatalf("hoisted table = %#v", got)
		}
	})

	t.Run("floating parser-only tags are dropped", func(t *testing.T) {
		tests := []string{
			`<point label="A" value="1"/>`,
			`<tr><td>A</td></tr>`,
			`<li>A</li>`,
			`<tab label="A"><text>B</text></tab>`,
		}
		for _, body := range tests {
			root, issues := ParseHTML(body)
			if root != nil {
				t.Errorf("ParseHTML(%q) root = %#v, want nil", body, root)
			}
			if len(issues) != 1 || !strings.Contains(issues[0].Msg, "empty deneb-ui block") {
				t.Errorf("ParseHTML(%q) issues = %v, want empty block", body, issues)
			}
		}
	})
}
