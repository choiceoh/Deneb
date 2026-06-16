package lmtpd

import "testing"

func TestSelectListenFD(t *testing.T) {
	const pid = 4242
	tests := []struct {
		name        string
		listenPid   string
		listenFds   string
		listenNames string
		want        int
		wantOK      bool
	}{
		{name: "unset env is a no-op", listenPid: "", listenFds: "", wantOK: false},
		{name: "pid mismatch ignores fds", listenPid: "9999", listenFds: "1", wantOK: false},
		{name: "named match picks the right fd", listenPid: "4242", listenFds: "1", listenNames: "lmtp", want: 3, wantOK: true},
		{name: "named match at second position", listenPid: "4242", listenFds: "2", listenNames: "http:lmtp", want: 4, wantOK: true},
		{name: "lone unnamed fd is accepted", listenPid: "4242", listenFds: "1", listenNames: "", want: 3, wantOK: true},
		{name: "multiple fds without a name match are rejected", listenPid: "4242", listenFds: "2", listenNames: "http:other", wantOK: false},
		{name: "zero fds rejected", listenPid: "4242", listenFds: "0", wantOK: false},
		{name: "garbage fds rejected", listenPid: "4242", listenFds: "x", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fd, ok := selectListenFD(pid, tt.listenPid, tt.listenFds, tt.listenNames, "lmtp")
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && fd != tt.want {
				t.Fatalf("fd = %d, want %d", fd, tt.want)
			}
		})
	}
}
