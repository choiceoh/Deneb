package mailarchive

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestIMAPConnCloseOnContextInterruptsConnection(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	c := &imapConn{conn: client, r: bufio.NewReader(client)}
	ctx, cancel := context.WithCancel(context.Background())
	stop := c.closeOnContext(ctx)
	defer stop()
	cancel()
	if err := server.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := server.Read(make([]byte, 1)); err == nil {
		t.Fatal("peer connection remained open after context cancellation")
	}
}

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
	if got := quote("sender\r\na99 STORE 1 +FLAGS (\\Deleted)\x00"); strings.ContainsAny(got, "\r\n\x00") {
		t.Fatalf("quote retained command-separating control bytes: %q", got)
	}
}

func TestUIDSearchUsesUTF8CharsetAndFallsBackForLegacyServer(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	c := &imapConn{conn: client, r: bufio.NewReader(client)}
	serverDone := make(chan error, 1)
	go func() {
		r := bufio.NewReader(server)
		for _, step := range []struct {
			want  string
			reply string
		}{
			{want: "a1 UID SEARCH CHARSET UTF-8 TEXT \"견적\"\r\n", reply: "a1 NO [BADCHARSET (US-ASCII)] unsupported\r\n"},
			{want: "a2 UID SEARCH TEXT \"견적\"\r\n", reply: "* SEARCH 7 9\r\na2 OK done\r\n"},
		} {
			line, err := r.ReadString('\n')
			if err != nil {
				serverDone <- err
				return
			}
			if line != step.want {
				serverDone <- fmt.Errorf("command = %q, want %q", line, step.want)
				return
			}
			if _, err := io.WriteString(server, step.reply); err != nil {
				serverDone <- err
				return
			}
		}
		serverDone <- nil
	}()

	uids, err := c.uidSearch(`TEXT "견적"`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(uids, ",") != "7,9" {
		t.Fatalf("uids = %v", uids)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
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
