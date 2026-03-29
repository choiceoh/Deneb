package telegram

import "testing"

func TestParseChatID(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{name: "plain positive", input: "12345", want: 12345},
		{name: "plain negative", input: "-10012345", want: -10012345},
		{name: "telegram prefix", input: "telegram:12345", want: 12345},
		{name: "short telegram prefix", input: "te:12345", want: 12345},
		{name: "session like key", input: "te:7074071666:task:1774610550265", want: 7074071666},
		{name: "prefixed with suffix", input: "telegram:-1001234567890:topic:99", want: -1001234567890},
		{name: "empty", input: "", wantErr: true},
		{name: "non numeric", input: "telegram:abc", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseChatID(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseChatID(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("ParseChatID(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
