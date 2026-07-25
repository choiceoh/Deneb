package chatport

import "testing"

func TestDefaultNativeSessionKeyContract(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: "client:main"},
		{name: "spaces", in: "   ", want: "client:main"},
		{name: "tabs and newlines", in: "\t\n", want: "client:main"},
		{name: "explicit", in: "client:main:project", want: "client:main:project"},
		{name: "explicit trimmed", in: "  client:main:project  ", want: "client:main:project"},
		{name: "other channel preserved", in: "telegram:123", want: "telegram:123"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := DefaultNativeSessionKey(tt.in); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
