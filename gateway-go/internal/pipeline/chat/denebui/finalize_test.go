package denebui

import (
	"log/slog"
	"strings"
	"testing"
)

func TestNormalizeFinalReply(t *testing.T) {
	t.Parallel()

	valid := "요약\n```deneb-ui\n<column><text>정상 카드</text></column>\n```"
	glued := "요약.```deneb-ui<column><text>붙은 카드</text></column>``` 뒤 설명"
	invalidInput := "```deneb-ui\n<column><input/></column>\n```"
	unparseable := "```deneb-ui\n{not-json}\n```"
	multiple := valid + "\n```deneb-ui\n<column><text>두 번째 카드</text></column>\n```"

	tests := []struct {
		name       string
		input      string
		want       string
		wantFences int
	}{
		{name: "plain text unchanged", input: "짧은 답변", want: "짧은 답변"},
		{name: "one valid card preserved", input: valid, want: valid, wantFences: 1},
		{name: "glued card and surrounding prose preserved", input: glued, want: "요약.\n```deneb-ui\n<column><text>붙은 카드</text></column>\n```\n뒤 설명", wantFences: 1},
		{name: "invalid interactive input becomes readable text", input: invalidInput, want: invalidCardFallback},
		{name: "unparseable card becomes fallback", input: unparseable, want: invalidCardFallback},
		{name: "additional card becomes plain text", input: multiple, want: valid + "\n두 번째 카드", wantFences: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeFinalReply(tt.input, "client:main", slog.New(slog.DiscardHandler))
			if got != tt.want {
				t.Fatalf("NormalizeFinalReply() = %q, want %q", got, tt.want)
			}
			if gotFences := len(ExtractFences(got)); gotFences != tt.wantFences {
				t.Fatalf("fence count = %d, want %d; output = %q", gotFences, tt.wantFences, got)
			}
			if strings.Contains(got, "<input") || strings.Contains(got, "{not-json}") {
				t.Fatalf("rejected markup leaked into output: %q", got)
			}
		})
	}
}

func TestNormalizeFinalReplyIsIdempotent(t *testing.T) {
	t.Parallel()
	input := "설명\n```deneb-ui\n<column><text>카드</text></column>\n```"
	once := NormalizeFinalReply(input, "client:main", slog.New(slog.DiscardHandler))
	twice := NormalizeFinalReply(once, "client:main", slog.New(slog.DiscardHandler))
	if twice != once {
		t.Fatalf("second normalization changed output:\nonce: %q\ntwice: %q", once, twice)
	}
}
