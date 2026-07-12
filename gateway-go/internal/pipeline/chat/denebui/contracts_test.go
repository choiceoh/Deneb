package denebui

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestIssueStringContract(t *testing.T) {
	if got := (Issue{Path: "$.children[1]", Msg: "missing type"}).String(); got != "$.children[1]: missing type" {
		t.Fatalf("Issue.String() = %q", got)
	}
	if got := (Issue{}).String(); got != ": " {
		t.Fatalf("zero Issue.String() = %q", got)
	}
}

func TestFenceRecognitionContract(t *testing.T) {
	openTests := []struct {
		name string
		line string
		want bool
	}{
		{name: "canonical", line: "```deneb-ui", want: true},
		{name: "uppercase", line: "```DENEB-UI", want: true},
		{name: "mixed case", line: "```Deneb-Ui", want: true},
		{name: "surrounding spaces", line: "  ```deneb-ui  ", want: true},
		{name: "four backticks tolerated", line: "````deneb-ui", want: true},
		// Lenient tail match: models glue the opener to a prose sentence.
		{name: "glued to prose", line: "가져올게요.```deneb-ui", want: true},
		{name: "glued with space", line: "카드로 정리했어요. ```deneb-ui", want: true},
		{name: "glued body tag", line: "```deneb-ui<column>", want: true},
		{name: "prose and glued body tag", line: "정리했어요.```deneb-ui<card>", want: true},
		{name: "no info", line: "```", want: false},
		{name: "json", line: "```json", want: false},
		{name: "prefix only", line: "```deneb", want: false},
		{name: "extra info", line: "```deneb-ui extra", want: false},
		{name: "mid-sentence mention", line: "```deneb-ui 펜스를 쓰세요", want: false},
		{name: "too few backticks", line: "프로즈``deneb-ui", want: false},
		{name: "tilde fence", line: "~~~deneb-ui", want: false},
		{name: "plain text", line: "deneb-ui", want: false},
	}
	for _, tt := range openTests {
		t.Run("open/"+tt.name, func(t *testing.T) {
			if got := isDenebUIFenceOpen(tt.line); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
	closeTests := []struct {
		line string
		want bool
	}{
		{line: "```", want: true},
		{line: "````", want: true},
		{line: "```   ", want: true},
		{line: "```json", want: false},
		{line: "``", want: false},
		{line: "", want: false},
	}
	for _, tt := range closeTests {
		if got := isFenceClose(tt.line); got != tt.want {
			t.Errorf("isFenceClose(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func TestExtractFencesDocumentOrderAndBoundaries(t *testing.T) {
	text := strings.Join([]string{
		"before",
		"```json",
		`{"ignored":true}`,
		"```",
		"```deneb-ui",
		`{"type":"text","value":"first"}`,
		"```",
		"between",
		"  ```DENEB-UI  ",
		"<text>second</text>",
		"  ```  ",
		"after",
	}, "\n")
	got := ExtractFences(text)
	want := []string{
		`{"type":"text","value":"first"}`,
		"<text>second</text>",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fences = %#v, want %#v", got, want)
	}
	if !HasFence(text) {
		t.Fatal("HasFence missed valid block")
	}
	if HasFence("```json\n{}\n```") {
		t.Fatal("HasFence matched non-Deneb fence")
	}
	if got := ExtractFences("```deneb-ui\nunclosed"); !reflect.DeepEqual(got, []string{"unclosed"}) {
		t.Fatalf("unclosed fence = %#v", got)
	}
}

func TestValidateJSONNodeTypeContract(t *testing.T) {
	validTypes := []string{
		"column", "row", "card", "box", "accordion", "list", "divider", "tabs",
		"text", "markdown", "image", "icon", "code", "quote", "badge", "stat",
		"avatar", "table", "button", "text_input", "date_input", "time_input",
		"checkbox", "select", "switch", "slider", "radio_group", "chip_group",
		"progress", "alert", "countdown",
	}
	for _, typ := range validTypes {
		t.Run("valid/"+typ, func(t *testing.T) {
			node := map[string]any{"type": typ}
			if spec := nodeSpecs[typ]; spec.requireID {
				node["id"] = "field-id"
			}
			raw, err := json.Marshal(node)
			if err != nil {
				t.Fatal(err)
			}
			issues, err := Validate(string(raw))
			if err != nil || len(issues) != 0 {
				t.Fatalf("issues = %v error=%v", issues, err)
			}
		})
	}

	invalid := []struct {
		name string
		body string
		want string
	}{
		{name: "empty object", body: `{}`, want: `missing or non-string "type"`},
		{name: "numeric type", body: `{"type":1}`, want: `missing or non-string "type"`},
		{name: "null type", body: `{"type":null}`, want: `missing or non-string "type"`},
		{name: "unknown type", body: `{"type":"widget"}`, want: `unknown node type "widget"`},
		{name: "primitive string", body: `"text"`, want: "expected a UI node object"},
		{name: "primitive number", body: `42`, want: "expected a UI node object"},
		{name: "primitive boolean", body: `true`, want: "expected a UI node object"},
		{name: "null", body: `null`, want: "expected a UI node object"},
	}
	for _, tt := range invalid {
		t.Run("invalid/"+tt.name, func(t *testing.T) {
			issues, err := Validate(tt.body)
			if err != nil {
				t.Fatal(err)
			}
			if len(issues) != 1 || !strings.Contains(issues[0].Msg, tt.want) {
				t.Fatalf("issues = %+v, want %q", issues, tt.want)
			}
		})
	}
}

func TestValidateRequiredIDContract(t *testing.T) {
	idTypes := []string{
		"text_input",
		"date_input",
		"time_input",
		"checkbox",
		"select",
		"switch",
		"slider",
		"radio_group",
		"chip_group",
	}
	for _, typ := range idTypes {
		for _, idCase := range []struct {
			name string
			id   any
		}{
			{name: "missing", id: nil},
			{name: "empty", id: ""},
			{name: "number", id: 7.0},
		} {
			t.Run(typ+"/"+idCase.name, func(t *testing.T) {
				node := map[string]any{"type": typ}
				if idCase.id != nil {
					node["id"] = idCase.id
				}
				issues := validateNode(node, "$")
				if len(issues) != 1 || !strings.Contains(issues[0].Msg, "requires a non-empty") {
					t.Fatalf("issues = %+v", issues)
				}
			})
		}
		t.Run(typ+"/present", func(t *testing.T) {
			issues := validateNode(map[string]any{"type": typ, "id": "field"}, "$")
			if len(issues) != 0 {
				t.Fatalf("issues = %+v", issues)
			}
		})
	}
}

func TestValidateEnumContract(t *testing.T) {
	tests := []struct {
		name    string
		typ     string
		field   string
		allowed []string
	}{
		{name: "text style", typ: "text", field: "style", allowed: textStyles},
		{name: "keyboard", typ: "text_input", field: "keyboard", allowed: keyboardTypes},
		{name: "button variant", typ: "button", field: "variant", allowed: buttonVariants},
		{name: "alert severity", typ: "alert", field: "severity", allowed: alertSeverities},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, value := range tt.allowed {
				node := map[string]any{"type": tt.typ, tt.field: value}
				if nodeSpecs[tt.typ].requireID {
					node["id"] = "id"
				}
				if issues := validateNode(node, "$"); len(issues) != 0 {
					t.Errorf("allowed %q rejected: %+v", value, issues)
				}
			}
			for name, value := range map[string]any{
				"unknown": "bogus",
				"empty":   "",
				"number":  7.0,
				"bool":    true,
			} {
				t.Run(name, func(t *testing.T) {
					node := map[string]any{"type": tt.typ, tt.field: value}
					if nodeSpecs[tt.typ].requireID {
						node["id"] = "id"
					}
					issues := validateNode(node, "$")
					if len(issues) != 1 || issues[0].Path != "$/."+tt.field {
						// Path assertion is made below using the actual JSON-ish dot form;
						// retain a focused failure if issue count drifts.
						if len(issues) != 1 {
							t.Fatalf("issues = %+v", issues)
						}
					}
					if issues[0].Path != "$."+tt.field || !strings.Contains(issues[0].Msg, "invalid "+tt.field) {
						t.Fatalf("issue = %+v", issues[0])
					}
				})
			}
		})
	}
}

func TestValidateChildrenContract(t *testing.T) {
	containers := []struct {
		typ   string
		field string
	}{
		{typ: "column", field: "children"},
		{typ: "row", field: "children"},
		{typ: "card", field: "children"},
		{typ: "box", field: "children"},
		{typ: "accordion", field: "children"},
		{typ: "list", field: "items"},
	}
	for _, container := range containers {
		t.Run(container.typ, func(t *testing.T) {
			valid := map[string]any{
				"type": container.typ,
				container.field: []any{
					map[string]any{"type": "text"},
					map[string]any{"type": "divider"},
				},
			}
			if issues := validateNode(valid, "$"); len(issues) != 0 {
				t.Fatalf("valid issues = %+v", issues)
			}
			for name, value := range map[string]any{
				"object": map[string]any{"type": "text"},
				"string": "text",
				"number": 3.0,
			} {
				t.Run(name, func(t *testing.T) {
					node := map[string]any{"type": container.typ, container.field: value}
					issues := validateNode(node, "$")
					if len(issues) != 1 || issues[0].Path != "$."+container.field {
						t.Fatalf("issues = %+v", issues)
					}
				})
			}
			badChild := map[string]any{
				"type": container.typ,
				container.field: []any{
					map[string]any{"type": "unknown"},
				},
			}
			issues := validateNode(badChild, "$")
			if len(issues) != 1 || !strings.Contains(issues[0].Path, container.field+"[0]") {
				t.Fatalf("nested issues = %+v", issues)
			}
		})
	}
}

func TestValidateTabsContract(t *testing.T) {
	valid := map[string]any{
		"type": "tabs",
		"tabs": []any{
			map[string]any{
				"label": "One",
				"children": []any{
					map[string]any{"type": "text"},
				},
			},
			map[string]any{
				"label": "Two",
				"children": []any{
					map[string]any{"type": "divider"},
				},
			},
		},
	}
	if issues := validateNode(valid, "$"); len(issues) != 0 {
		t.Fatalf("valid tabs issues = %+v", issues)
	}
	tests := []struct {
		name     string
		tabs     any
		wantPath string
		wantMsg  string
	}{
		{name: "tabs not array", tabs: "bad", wantPath: "$.tabs", wantMsg: "must be an array"},
		{name: "tab not object", tabs: []any{"bad"}, wantPath: "$.tabs[0]", wantMsg: "tab must be an object"},
		{name: "children not array", tabs: []any{map[string]any{"children": "bad"}}, wantPath: "$.tabs[0].children", wantMsg: "must be an array"},
		{name: "bad child", tabs: []any{map[string]any{"children": []any{map[string]any{"type": "bad"}}}}, wantPath: "$.tabs[0].children[0]", wantMsg: "unknown node type"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issues := validateNode(map[string]any{"type": "tabs", "tabs": tt.tabs}, "$")
			if len(issues) != 1 || issues[0].Path != tt.wantPath || !strings.Contains(issues[0].Msg, tt.wantMsg) {
				t.Fatalf("issues = %+v", issues)
			}
		})
	}
}

func TestValidateActionsContract(t *testing.T) {
	for _, typ := range actionTypes {
		t.Run("valid/"+typ, func(t *testing.T) {
			issues := validateAction(map[string]any{"type": typ}, "$.action")
			if len(issues) != 0 {
				t.Fatalf("issues = %+v", issues)
			}
		})
	}
	tests := []struct {
		name   string
		action any
		want   string
	}{
		{name: "string", action: "callback", want: "action must be an object"},
		{name: "array", action: []any{}, want: "action must be an object"},
		{name: "missing type", action: map[string]any{}, want: `action missing "type"`},
		{name: "numeric type", action: map[string]any{"type": 1.0}, want: `action missing "type"`},
		{name: "unknown", action: map[string]any{"type": "execute"}, want: "unknown action type"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issues := validateAction(tt.action, "$.action")
			if len(issues) != 1 || !strings.Contains(issues[0].Msg, tt.want) {
				t.Fatalf("issues = %+v", issues)
			}
		})
	}
	for _, typ := range []string{"button", "countdown"} {
		node := map[string]any{"type": typ, "action": map[string]any{"type": "callback"}}
		if issues := validateNode(node, "$"); len(issues) != 0 {
			t.Errorf("%s valid action issues = %+v", typ, issues)
		}
		node["action"] = "bad"
		issues := validateNode(node, "$")
		if len(issues) != 1 || issues[0].Path != "$.action" {
			t.Errorf("%s bad action issues = %+v", typ, issues)
		}
	}
}

func TestNDJSONContract(t *testing.T) {
	body := strings.Join([]string{
		`{"type":"text","value":"one"}`,
		"",
		`  {"type":"divider"}  `,
		`{"type":"alert","severity":"warning"}`,
	}, "\n")
	nodes, err := parseNDJSON(body)
	if err != nil || len(nodes) != 3 {
		t.Fatalf("nodes = %+v/%v", nodes, err)
	}
	issues, err := Validate(body)
	if err != nil || len(issues) != 0 {
		t.Fatalf("validate = %+v/%v", issues, err)
	}
	issues, err = Validate(`{"type":"text"}` + "\n" + `{"type":"bad"}`)
	if err != nil || len(issues) != 1 || issues[0].Path != "[1]" {
		t.Fatalf("invalid node issues = %+v/%v", issues, err)
	}
	for name, input := range map[string]string{
		"empty":      "  \n ",
		"malformed":  `{"type":"text"}` + "\n" + `{bad}`,
		"multi-line": "{\n\"type\":\"text\"\n}",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseNDJSON(input); err == nil {
				t.Fatal("expected NDJSON error")
			}
		})
	}
}

func TestValidateParseErrorsContract(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
		want string
	}{
		{name: "empty", body: "", want: "empty deneb-ui block"},
		{name: "whitespace", body: " \n\t ", want: "empty deneb-ui block"},
		{name: "broken json", body: `{bad}`, want: "invalid JSON"},
		{name: "broken array", body: `[{]`, want: "invalid JSON"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			issues, err := Validate(tt.body)
			if err == nil || !strings.Contains(err.Error(), tt.want) || issues != nil {
				t.Fatalf("result = %+v/%v", issues, err)
			}
		})
	}
}

func TestDecodeEntitiesContract(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "none", in: "plain", want: "plain"},
		{name: "named lowercase", in: "&lt;&gt;&amp;&quot;&apos;&nbsp;", want: "<>&\"' "},
		{name: "named uppercase", in: "&LT;&GT;&AMP;&QUOT;&APOS;&NBSP;", want: "<>&\"' "},
		{name: "decimal ascii", in: "&#65;", want: "A"},
		{name: "decimal unicode", in: "&#54620;", want: "한"},
		{name: "hex lowercase", in: "&#x1f600;", want: "😀"},
		{name: "hex uppercase", in: "&#X1F600;", want: "😀"},
		{name: "zero preserved", in: "&#0;", want: "&#0;"},
		{name: "negative malformed preserved", in: "&#-1;", want: "&#-1;"},
		{name: "bad decimal preserved", in: "&#abc;", want: "&#abc;"},
		{name: "bad hex preserved", in: "&#xxyz;", want: "&#xxyz;"},
		{name: "unknown named preserved", in: "&copy;", want: "&copy;"},
		{name: "missing semicolon preserved", in: "&amp", want: "&amp"},
		{name: "overlong entity preserved", in: "&thisiswaytoolong;", want: "&thisiswaytoolong;"},
		{name: "above unicode max preserved", in: "&#x110000;", want: "&#x110000;"},
		{name: "high surrogate preserved", in: "&#xD800;", want: "&#xD800;"},
		{name: "low surrogate preserved", in: "&#xDFFF;", want: "&#xDFFF;"},
		{name: "mixed", in: "A &amp; B &#x1f600; C", want: "A & B 😀 C"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeEntities(tt.in)
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("invalid UTF-8: %q", got)
			}
		})
	}
}

func TestMarkdownBlockDetectionContract(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{name: "empty", text: "", want: false},
		{name: "plain", text: "hello world", want: false},
		{name: "lone pipe", text: "| a | b |", want: false},
		{name: "table", text: "| a | b |\n| --- | --- |", want: true},
		{name: "h1", text: "# Heading", want: true},
		{name: "h6", text: "###### Heading", want: true},
		{name: "seven hashes", text: "####### Not heading", want: false},
		{name: "hash no space", text: "#Heading", want: false},
		{name: "single dash bullet", text: "- one", want: false},
		{name: "two dash bullets", text: "- one\n- two", want: true},
		{name: "two star bullets", text: "* one\n* two", want: true},
		{name: "two dot bullets", text: "• one\n• two", want: true},
		{name: "numbered dot", text: "1. one\n2. two", want: true},
		{name: "numbered paren", text: "1) one\n2) two", want: true},
		{name: "fence", text: "```go\nfmt.Println()\n```", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := looksLikeMarkdownBlock(tt.text); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMarkdownHeadingAndBulletContract(t *testing.T) {
	for text, want := range map[string]bool{
		"# H":       true,
		"## H":      true,
		"###### H":  true,
		"####### H": false,
		"#H":        false,
		"plain":     false,
	} {
		if got := isMarkdownHeading(text); got != want {
			t.Errorf("heading %q = %v, want %v", text, got, want)
		}
	}
	for text, want := range map[string]bool{
		"- item":   true,
		"* item":   true,
		"• item":   true,
		"1. item":  true,
		"12) item": true,
		"-item":    false,
		"1.item":   false,
		"item":     false,
	} {
		if got := isMarkdownBullet(text); got != want {
			t.Errorf("bullet %q = %v, want %v", text, got, want)
		}
	}
}

func TestTextBlockNodeContract(t *testing.T) {
	for _, tt := range []struct {
		name      string
		input     string
		wantType  string
		wantValue string
	}{
		{name: "plain", input: "  hello world  ", wantType: "text", wantValue: "hello world"},
		{name: "heading", input: "# Heading", wantType: "markdown", wantValue: "# Heading"},
		{name: "table", input: "| a |\n| --- |", wantType: "markdown", wantValue: "| a |\n| --- |"},
		{name: "bullet list", input: "- a\n- b", wantType: "markdown", wantValue: "- a\n- b"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := textBlockNode(tt.input)
			if got["type"] != tt.wantType || got["value"] != tt.wantValue {
				t.Fatalf("node = %+v", got)
			}
		})
	}
}

func TestNameCharacterContract(t *testing.T) {
	for _, c := range []byte{'a', 'z', 'A', 'Z'} {
		if !isNameStart(c) {
			t.Errorf("%q not name start", c)
		}
	}
	for _, c := range []byte{'0', '9', '-', '_'} {
		if isNameStart(c) || !isNameChar(c) {
			t.Errorf("%q start/char classification wrong", c)
		}
	}
	for _, c := range []byte{' ', ':', '/', '>', '<'} {
		if isNameStart(c) || isNameChar(c) {
			t.Errorf("%q unexpectedly accepted", c)
		}
	}
	for _, c := range []byte{' ', '\t', '\n', '\r'} {
		if !isSpace(c) {
			t.Errorf("%q not whitespace", c)
		}
	}
	if isSpace('\v') {
		t.Fatal("vertical tab unexpectedly grammar whitespace")
	}
}

func TestLenientFloatContract(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  float64
		ok    bool
	}{
		{name: "empty", input: "", ok: false},
		{name: "spaces", input: "   ", ok: false},
		{name: "integer", input: "42", want: 42, ok: true},
		{name: "negative", input: "-42", want: -42, ok: true},
		{name: "decimal", input: "3.14", want: 3.14, ok: true},
		{name: "percent", input: "68%", want: 68, ok: true},
		{name: "pixels", input: "16px", want: 16, ok: true},
		{name: "thousands", input: "1,200톤", want: 1200, ok: true},
		{name: "prefixed currency", input: "₩1,200.50원", want: 1200.5, ok: true},
		{name: "negative units", input: "-12.5kg", want: -12.5, ok: true},
		{name: "no digits", input: "auto", ok: false},
		{name: "trailing dot", input: "12.", want: 12, ok: true},
		{name: "second dot stops", input: "1.2.3", want: 1.2, ok: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := lenientFloat(tt.input)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("got %v/%v, want %v/%v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestCanonicalizationHelpersContract(t *testing.T) {
	for input, want := range map[string]string{
		"red": "error", " RED ": "error", "green": "success", "yellow": "warning",
		"amber": "warning", "orange": "warning", "blue": "primary", "gray": "secondary",
		"grey": "secondary", "neutral": "secondary", "custom": "custom", "": "",
	} {
		if got := canonBadgeColor(input); got != want {
			t.Errorf("badge %q = %q, want %q", input, got, want)
		}
	}
	for input, want := range map[string]string{
		"warn": "warning", "caution": "warning", "danger": "error", "critical": "error",
		"fatal": "error", "ok": "success", "done": "success", "note": "info",
		"notice": "info", "information": "info", "warning": "warning", "": "",
	} {
		if got := canonSeverity(input); got != want {
			t.Errorf("severity %q = %q, want %q", input, got, want)
		}
	}
	for input, want := range map[string]string{
		"bars": "bar", "column": "bar", "columns": "bar", "lines": "line",
		"area": "line", "trend": "line", "pie": "pie", "": "",
	} {
		if got := canonChartType(input); got != want {
			t.Errorf("chart %q = %q, want %q", input, got, want)
		}
	}
	for input, want := range map[string]string{
		"heading": "headline", "header": "headline", "subtitle": "title",
		"subheading": "title", "body": "body", "": "",
	} {
		if got := canonTextStyle(input); got != want {
			t.Errorf("style %q = %q, want %q", input, got, want)
		}
	}
}

func TestTruthyContract(t *testing.T) {
	for _, input := range []string{"false", "False", " FALSE ", "0", "no", "off", "OFF"} {
		if truthy(input) {
			t.Errorf("truthy(%q) = true", input)
		}
	}
	for _, input := range []string{"", "true", "1", "yes", "on", "required", "anything"} {
		if !truthy(input) {
			t.Errorf("truthy(%q) = false", input)
		}
	}
}

func TestActionFromAttrsPrecedenceAndData(t *testing.T) {
	tests := []struct {
		name  string
		attrs map[string]string
		want  map[string]any
	}{
		{name: "none", attrs: map[string]string{}, want: nil},
		{name: "href", attrs: map[string]string{"href": "https://example.com"}, want: map[string]any{"type": "open_url", "url": "https://example.com"}},
		{name: "toggle", attrs: map[string]string{"toggle": "details"}, want: map[string]any{"type": "toggle", "targetId": "details"}},
		{name: "copy", attrs: map[string]string{"copy": "text"}, want: map[string]any{"type": "copy_to_clipboard", "text": "text"}},
		{name: "event", attrs: map[string]string{"event": "submit"}, want: map[string]any{"type": "callback", "event": "submit"}},
		{name: "event wins", attrs: map[string]string{"event": "submit", "href": "https://example.com", "toggle": "x", "copy": "y"}, want: map[string]any{"type": "callback", "event": "submit"}},
		{name: "href wins toggle copy", attrs: map[string]string{"href": "https://example.com", "toggle": "x", "copy": "y"}, want: map[string]any{"type": "open_url", "url": "https://example.com"}},
		{name: "toggle wins copy", attrs: map[string]string{"toggle": "x", "copy": "y"}, want: map[string]any{"type": "toggle", "targetId": "x"}},
		{name: "event data and collect", attrs: map[string]string{
			"event": "submit", "data-id": "42", "data-kind": "mail", "data-": "ignored", "collect": " name, email, , note ",
		}, want: map[string]any{
			"type": "callback", "event": "submit",
			"data":        map[string]any{"id": "42", "kind": "mail"},
			"collectFrom": []any{"name", "email", "note"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := actionFromAttrs(tt.attrs); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParseHTMLCoreNodeContract(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantType   string
		wantIssues int
		assert     func(*testing.T, map[string]any)
	}{
		{
			name:     "text",
			body:     `<text style="heading" bold> Hello &amp; world </text>`,
			wantType: "text",
			assert: func(t *testing.T, node map[string]any) {
				if node["value"] != "Hello & world" || node["style"] != "headline" || node["bold"] != true {
					t.Fatalf("node = %+v", node)
				}
			},
		},
		{
			name:     "progress percent clamped",
			body:     `<progress value="125%" label="Done"/>`,
			wantType: "progress",
			assert: func(t *testing.T, node map[string]any) {
				if node["value"] != float64(1) || node["label"] != "Done" {
					t.Fatalf("node = %+v", node)
				}
			},
		},
		{
			name:     "alert canonical severity",
			body:     `<alert severity="danger" title="Risk">Act now</alert>`,
			wantType: "alert",
			assert: func(t *testing.T, node map[string]any) {
				if node["severity"] != "error" || node["message"] != "Act now" {
					t.Fatalf("node = %+v", node)
				}
			},
		},
		{
			name:     "button callback",
			body:     `<button label="Send" event="submit" data-id="42" collect="name,email"/>`,
			wantType: "button",
			assert: func(t *testing.T, node map[string]any) {
				action, ok := node["action"].(map[string]any)
				if !ok || action["type"] != "callback" || action["event"] != "submit" {
					t.Fatalf("node = %+v", node)
				}
			},
		},
		{
			name:     "input date",
			body:     `<input type="date" id="due" label="Due" required/>`,
			wantType: "date_input",
			assert: func(t *testing.T, node map[string]any) {
				if node["id"] != "due" || node["required"] != true {
					t.Fatalf("node = %+v", node)
				}
			},
		},
		{
			name:     "textarea",
			body:     `<textarea id="notes">hello</textarea>`,
			wantType: "text_input",
			assert: func(t *testing.T, node map[string]any) {
				if node["id"] != "notes" || node["multiline"] != true || node["value"] != "hello" {
					t.Fatalf("node = %+v", node)
				}
			},
		},
		{
			name:     "select options",
			body:     `<select id="choice"><option>A</option><option selected>B</option></select>`,
			wantType: "select",
			assert: func(t *testing.T, node map[string]any) {
				if node["selected"] != "B" || !reflect.DeepEqual(node["options"], []any{"A", "B"}) {
					t.Fatalf("node = %+v", node)
				}
			},
		},
		{
			name:       "unknown tag reports issue",
			body:       `<widgit>text</widgit>`,
			wantType:   "text",
			wantIssues: 1,
			assert: func(t *testing.T, node map[string]any) {
				if node["value"] != "text" {
					t.Fatalf("node = %+v", node)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, issues := ParseHTML(tt.body)
			if len(issues) != tt.wantIssues {
				t.Fatalf("issues = %+v", issues)
			}
			node, ok := root.(map[string]any)
			if !ok || node["type"] != tt.wantType {
				t.Fatalf("root = %#v, want type %s", root, tt.wantType)
			}
			tt.assert(t, node)
		})
	}
}

func TestParseHTMLMultipleRootsWrapColumn(t *testing.T) {
	root, issues := ParseHTML(`<text>one</text><hr/><text>two</text>`)
	if len(issues) != 0 {
		t.Fatalf("issues = %+v", issues)
	}
	node := root.(map[string]any)
	if node["type"] != "column" {
		t.Fatalf("root = %+v", node)
	}
	children, ok := node["children"].([]any)
	if !ok || len(children) != 3 {
		t.Fatalf("children = %#v", node["children"])
	}
}

func TestCollapsedReportFenceContract(t *testing.T) {
	tests := []struct {
		name     string
		title    string
		body     string
		wantSame bool
	}{
		{name: "empty title", title: "", body: "body", wantSame: true},
		{name: "blank title", title: "  ", body: "body", wantSame: true},
		{name: "empty body", title: "title", body: "", wantSame: true},
		{name: "blank body", title: "title", body: " \n ", wantSame: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CollapsedReportFence(tt.title, tt.body)
			if got != tt.body {
				t.Fatalf("got %q, want original body %q", got, tt.body)
			}
		})
	}
	title := `Risk "A" & <B>`
	body := "# Report\n\n```go\nfmt.Println(`<x> & y`)\n```"
	got := CollapsedReportFence(title, body)
	if !strings.HasPrefix(got, "```deneb-ui\n<accordion") || !strings.HasSuffix(got, "</accordion>\n```") {
		t.Fatalf("fence shape:\n%s", got)
	}
	if strings.Count(got, "```") != 2 {
		t.Fatalf("inner code fence escaped incorrectly:\n%s", got)
	}
	if !strings.Contains(got, "&quot;") || !strings.Contains(got, "&amp;") || !strings.Contains(got, "&lt;") {
		t.Fatalf("attribute/raw escaping missing:\n%s", got)
	}
	fences := ExtractFences(got)
	if len(fences) != 1 {
		t.Fatalf("fences = %#v", fences)
	}
	issues, err := Validate(fences[0])
	if err != nil || len(issues) != 0 {
		t.Fatalf("wrapped report invalid = %+v/%v", issues, err)
	}
}

func TestIsHTMLBodyContract(t *testing.T) {
	for _, input := range []string{"<text>x</text>", "  <card/>\n", "\n\t<div>x</div>"} {
		if !IsHTMLBody(input) {
			t.Errorf("HTML body missed: %q", input)
		}
	}
	for _, input := range []string{"", "{}", "[]", `{"type":"text"}`, "plain <text>"} {
		if IsHTMLBody(input) {
			t.Errorf("non-HTML body matched: %q", input)
		}
	}
}

func TestContainsHelpersContract(t *testing.T) {
	if !contains([]string{"a", "b"}, "a") || contains([]string{"a", "b"}, "A") || contains(nil, "a") {
		t.Fatal("contains contract failed")
	}
	if !containsStr([]string{"a", "b"}, "b") || containsStr([]string{"a", "b"}, "B") || containsStr(nil, "a") {
		t.Fatal("containsStr contract failed")
	}
	if got := firstNonEmpty("", "a", "b"); got != "a" {
		t.Fatalf("firstNonEmpty = %q", got)
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Fatalf("empty firstNonEmpty = %q", got)
	}
}
