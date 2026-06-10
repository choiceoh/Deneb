package denebui

import "testing"

func TestExtractFences(t *testing.T) {
	t.Run("single block", func(t *testing.T) {
		text := "여기 대시보드입니다:\n\n```deneb-ui\n{\"type\":\"text\",\"value\":\"hi\"}\n```\n\n끝."
		got := ExtractFences(text)
		if len(got) != 1 {
			t.Fatalf("want 1 block, got %d", len(got))
		}
		if got[0] != `{"type":"text","value":"hi"}` {
			t.Errorf("unexpected body: %q", got[0])
		}
	})

	t.Run("no block", func(t *testing.T) {
		if got := ExtractFences("그냥 텍스트\n```json\n{}\n```"); len(got) != 0 {
			t.Errorf("want 0 blocks, got %d", len(got))
		}
		if HasFence("```python\nprint(1)\n```") {
			t.Errorf("HasFence should be false for non-deneb-ui fence")
		}
	})

	t.Run("case-insensitive and multiple", func(t *testing.T) {
		text := "```DENEB-UI\n{\"type\":\"text\"}\n```\nmid\n```deneb-ui\n{\"type\":\"divider\"}\n```"
		got := ExtractFences(text)
		if len(got) != 2 {
			t.Fatalf("want 2 blocks, got %d", len(got))
		}
		if !HasFence(text) {
			t.Errorf("HasFence should be true")
		}
	})
}

func TestValidate_Valid(t *testing.T) {
	cases := map[string]string{
		"dashboard": `{"type":"column","children":[
			{"type":"card","children":[
				{"type":"stat","value":"42","label":"Open deals"},
				{"type":"text","value":"Pipeline healthy","style":"body"},
				{"type":"button","label":"Refresh","variant":"tonal","action":{"type":"callback","event":"refresh"}}
			]}
		]}`,
		"form": `{"type":"column","children":[
			{"type":"text_input","id":"name","label":"이름"},
			{"type":"checkbox","id":"agree","label":"동의"},
			{"type":"button","label":"제출","action":{"type":"callback","event":"submit","collectFrom":["name","agree"]}}
		]}`,
		"datetime form": `{"type":"column","children":[
			{"type":"date_input","id":"due","label":"마감일","required":true},
			{"type":"time_input","id":"at","label":"시각","value":"14:30"},
			{"type":"button","label":"저장","action":{"type":"callback","event":"save","collectFrom":["due","at"]}}
		]}`,
		"required form with keyboard and placeholder": `{"type":"column","children":[
			{"type":"text_input","id":"qty","label":"수량","required":true,"keyboard":"number"},
			{"type":"select","id":"cat","label":"분류","options":["업무","개인"],"placeholder":"선택…","required":true},
			{"type":"radio_group","id":"prio","options":["높음","낮음"],"required":true},
			{"type":"button","label":"저장","action":{"type":"callback","event":"save","collectFrom":["qty","cat","prio"]}}
		]}`,
		"tabs": `{"type":"tabs","tabs":[
			{"label":"A","children":[{"type":"text","value":"a"}]},
			{"label":"B","children":[{"type":"alert","message":"hey","severity":"warning"}]}
		]}`,
		"ndjson": "{\"type\":\"text\",\"value\":\"a\"}\n{\"type\":\"text\",\"value\":\"b\"}",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			issues, err := Validate(body)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(issues) != 0 {
				t.Errorf("expected valid, got issues: %v", issues)
			}
		})
	}
}

func TestValidate_Invalid(t *testing.T) {
	cases := map[string]string{
		"unknown type":        `{"type":"hologram"}`,
		"missing required id": `{"type":"column","children":[{"type":"text_input","label":"x"}]}`,
		"bad text style":      `{"type":"text","value":"x","style":"gigantic"}`,
		"bad button variant":  `{"type":"button","label":"x","variant":"ghost"}`,
		"bad keyboard type":   `{"type":"text_input","id":"x","keyboard":"qwerty"}`,
		"date_input no id":    `{"type":"date_input","label":"마감일"}`,
		"bad action type":     `{"type":"button","label":"x","action":{"type":"explode"}}`,
		"children not array":  `{"type":"column","children":"nope"}`,
		"missing type":        `{"value":"x"}`,
		"nested unknown":      `{"type":"card","children":[{"type":"text"},{"type":"bogus"}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			issues, err := Validate(body)
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}
			if len(issues) == 0 {
				t.Errorf("expected at least one issue, got none")
			}
		})
	}
}

func TestValidate_NotJSON(t *testing.T) {
	if _, err := Validate("{this is not json"); err == nil {
		t.Errorf("expected error for non-JSON body")
	}
	if _, err := Validate("   "); err == nil {
		t.Errorf("expected error for empty body")
	}
}
