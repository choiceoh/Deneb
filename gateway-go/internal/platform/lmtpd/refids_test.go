package lmtpd

import (
	"strings"
	"testing"
)

func TestParseRefIDs(t *testing.T) {
	tests := []struct {
		name       string
		references string
		inReplyTo  string
		want       []string
	}{
		{
			name: "empty",
		},
		{
			name:       "in-reply-to first, then references, deduped",
			references: "<a@h> <b@h>\r\n <c@h>",
			inReplyTo:  "<c@h>",
			want:       []string{"<c@h>", "<a@h>", "<b@h>"},
		},
		{
			name:       "references only",
			references: "<x@host1> <y@host2>",
			want:       []string{"<x@host1>", "<y@host2>"},
		},
		{
			name:      "ignores non-bracketed noise",
			inReplyTo: "garbage <real@id> trailing",
			want:      []string{"<real@id>"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRefIDs(tt.references, tt.inReplyTo)
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestParseDetail_ThreadingHeaders(t *testing.T) {
	raw := "Message-ID: <new@deneb>\r\n" +
		"In-Reply-To: <parent@deneb>\r\n" +
		"References: <root@deneb> <parent@deneb>\r\n" +
		"From: Someone <a@b.com>\r\n" +
		"Subject: Re: hi\r\n" +
		"\r\nbody text\r\n"
	d, err := ParseDetail([]byte(raw))
	if err != nil {
		t.Fatalf("ParseDetail: %v", err)
	}
	if d.MessageIDHeader != "<new@deneb>" {
		t.Errorf("MessageIDHeader=%q", d.MessageIDHeader)
	}
	// In-Reply-To first, then References (parent deduped).
	want := []string{"<parent@deneb>", "<root@deneb>"}
	if strings.Join(d.References, ",") != strings.Join(want, ",") {
		t.Errorf("References=%v want %v", d.References, want)
	}
}
