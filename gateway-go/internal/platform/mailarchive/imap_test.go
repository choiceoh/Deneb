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

func TestExtractFetchUIDParsesUIDOrReturnsEmpty(t *testing.T) {
	tests := map[string]string{
		"* 1 FETCH (UID 5 BODY[] {11}\r\nHELLO WORLD)\r\n": "5",
		"* 9 FETCH (FLAGS () UID 42 BODY[] {1}\r\nx)\r\n":  "42",
		"* SEARCH 1 2 3\r\n":                               "",
	}
	for entry, want := range tests {
		if got := extractFetchUID([]byte(entry)); got != want {
			t.Errorf("extractFetchUID(%q) = %q, want %q", entry, got, want)
		}
	}
}

func TestExtractAddrParsesAddressOrReturnsEmpty(t *testing.T) {
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

func TestImapSinceDateFormatsDateForIMAPSearch(t *testing.T) {
	got := imapSinceDate(time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC))
	if got != "05-Jan-2026" {
		t.Fatalf("got %q want 05-Jan-2026", got)
	}
}

func TestDedupStringsDeduplicatesPreservingOrder(t *testing.T) {
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

func TestQuoteEncodesBackslashesAndQuotes(t *testing.T) {
	if got := quote(`a"b\c`); got != `"a\"b\\c"` {
		t.Fatalf("quote escaping wrong: %s", got)
	}
}

func TestStripSentDateCriteriaDropsSentKeys(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		changed bool
	}{
		{in: "ALL", want: "ALL", changed: false},
		{in: "SENTSINCE 15-Jul-2026 SENTBEFORE 16-Jul-2026", want: "ALL", changed: true},
		{in: `FROM "a@b.c" SENTSINCE 15-Jul-2026`, want: `FROM "a@b.c"`, changed: true},
		{in: "SINCE 14-Jul-2026", want: "SINCE 14-Jul-2026", changed: false},
	}
	for _, tt := range tests {
		got, changed := stripSentDateCriteria(tt.in)
		if got != tt.want || changed != tt.changed {
			t.Errorf("stripSentDateCriteria(%q) = (%q, %v), want (%q, %v)", tt.in, got, changed, tt.want, tt.changed)
		}
	}
}

func TestSentInHalfOpenRangeBucketsByKSTDay(t *testing.T) {
	since := time.Date(2026, 7, 15, 0, 0, 0, 0, archiveDayLoc)
	before := time.Date(2026, 7, 16, 0, 0, 0, 0, archiveDayLoc)
	tests := []struct {
		date string
		want bool
	}{
		{date: "Wed, 15 Jul 2026 16:02:03 +0900", want: true},
		{date: "Tue, 14 Jul 2026 23:00:00 +0900", want: false},
		{date: "Thu, 16 Jul 2026 00:00:00 +0900", want: false},
		{date: "", want: false},
	}
	for _, tt := range tests {
		if got := sentInHalfOpenRange(tt.date, since, before); got != tt.want {
			t.Errorf("sentInHalfOpenRange(%q) = %v, want %v", tt.date, got, tt.want)
		}
	}
}
