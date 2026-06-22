package wiki

import "testing"

func TestParseProjectDigests_FencedAndPlain(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"plain", `[{"project":"영산고","headline":"모듈 발주 완료","bullets":["계약 체결","납기 6월"]}]`},
		{"fenced", "```json\n" + `[{"project":"영산고","headline":"모듈 발주 완료","bullets":["계약 체결","납기 6월"]}]` + "\n```"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseProjectDigests(tc.in)
			if err != nil {
				t.Fatalf("parseProjectDigests: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("got %d digests, want 1", len(got))
			}
			d := got[0]
			if d.Project != "영산고" || d.Headline != "모듈 발주 완료" {
				t.Errorf("unexpected digest: %+v", d)
			}
			if len(d.Bullets) != 2 {
				t.Errorf("bullets = %v, want 2", d.Bullets)
			}
		})
	}
}

func TestParseProjectDigests_Empty(t *testing.T) {
	for _, in := range []string{"", "[]", "  []  ", "```\n[]\n```"} {
		got, err := parseProjectDigests(in)
		if err != nil {
			t.Fatalf("parseProjectDigests(%q): %v", in, err)
		}
		if len(got) != 0 {
			t.Errorf("parseProjectDigests(%q) = %d, want 0", in, len(got))
		}
	}
}

func TestParseProjectDigests_Malformed(t *testing.T) {
	if _, err := parseProjectDigests(`{not json`); err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestParseProjectDigests_DropsIncompleteAndCapsBullets(t *testing.T) {
	in := `[
	  {"project":"","headline":"오너 없음"},
	  {"project":"무헤드라인","headline":""},
	  {"project":"영산고","headline":"진행 중","bullets":["a","b","c","d","e"]}
	]`
	got, err := parseProjectDigests(in)
	if err != nil {
		t.Fatalf("parseProjectDigests: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d digests, want 1 (incomplete dropped)", len(got))
	}
	if len(got[0].Bullets) != projectDigestMaxBullets {
		t.Errorf("bullets = %d, want capped at %d", len(got[0].Bullets), projectDigestMaxBullets)
	}
}
