package mailarchive

import (
	"bytes"
	"testing"
	"time"
)

func TestExtractLiteralPayload(t *testing.T) {
	tests := []struct {
		name  string
		entry string
		want  string
		ok    bool
	}{
		{
			name:  "fetch body literal",
			entry: "* 1 FETCH (UID 5 BODY[] {11}\r\nHELLO WORLD)\r\n",
			want:  "HELLO WORLD",
			ok:    true,
		},
		{
			name:  "no literal",
			entry: "* SEARCH 1 2 3\r\n",
			want:  "",
			ok:    false,
		},
		{
			name:  "over-announced literal is clamped to available bytes",
			entry: "* 1 FETCH (BODY[] {99}\r\nshort)\r\n",
			want:  "short)\r\n",
			ok:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := extractLiteralPayload([]byte(tt.entry))
			if ok != tt.ok {
				t.Fatalf("ok=%v want %v", ok, tt.ok)
			}
			if ok && !bytes.Equal(got, []byte(tt.want)) {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestExtractAddr(t *testing.T) {
	cases := map[string]string{
		`Christina Gu <christina.gu@zttgroup.com>`: "christina.gu@zttgroup.com",
		`plain@example.com`:                        "plain@example.com",
		`"Name, Comma" <a.b+tag@sub.example.co>`:   "a.b+tag@sub.example.co",
		`no address here`:                          "",
	}
	for in, want := range cases {
		if got := extractAddr(in); got != want {
			t.Errorf("extractAddr(%q)=%q want %q", in, got, want)
		}
	}
}

func TestImapSinceDate(t *testing.T) {
	got := imapSinceDate(time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC))
	if got != "05-Jan-2026" {
		t.Fatalf("got %q want 05-Jan-2026", got)
	}
}

func TestDedupStrings(t *testing.T) {
	got := dedupStrings([]string{"1", "2", "1", "", "3", "2"})
	want := []string{"1", "2", "3"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestQuote(t *testing.T) {
	if got := quote(`a"b\c`); got != `"a\"b\\c"` {
		t.Fatalf("quote escaping wrong: %s", got)
	}
}
