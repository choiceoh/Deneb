package server

import "testing"

func TestExtractCronDefaultTo(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "empty raw",
			raw:  "",
			want: "",
		},
		{
			name: "malformed json",
			raw:  "{not json",
			want: "",
		},
		{
			name: "no channels block",
			raw:  `{}`,
			want: "",
		},
		{
			name: "chatID but no token at all",
			raw:  `{"channels":{"telegram":{"chatID":12345}}}`,
			want: "",
		},
		{
			name: "token but chatID missing",
			raw:  `{"channels":{"telegram":{"botToken":"abc"}}}`,
			want: "",
		},
		{
			name: "token but chatID zero",
			raw:  `{"channels":{"telegram":{"chatID":0,"botToken":"abc"}}}`,
			want: "",
		},
		{
			name: "raw botToken + chatID",
			raw:  `{"channels":{"telegram":{"chatID":12345,"botToken":"abc"}}}`,
			want: "12345",
		},
		{
			name: "botTokenRef + chatID (no raw token)",
			raw:  `{"channels":{"telegram":{"chatID":-1001234567890,"botTokenRef":"op://x/y/z"}}}`,
			want: "-1001234567890",
		},
		{
			name: "both botToken and botTokenRef present",
			raw:  `{"channels":{"telegram":{"chatID":7,"botToken":"a","botTokenRef":"op://x"}}}`,
			want: "7",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractCronDefaultTo(tc.raw); got != tc.want {
				t.Fatalf("extractCronDefaultTo(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
