package sdsocket

import "testing"

// The sd_listen_fds selection contract, ported from lmtpd's original
// implementation and extended for the two-consumer world (lmtp + http).
func TestSelectFDContractByNameAndPid(t *testing.T) {
	const pid = 4242
	tests := []struct {
		name        string
		listenPid   string
		listenFds   string
		listenNames string
		ask         string
		want        int
		wantOK      bool
	}{
		{name: "unset env is a no-op", ask: "lmtp", wantOK: false},
		{name: "pid mismatch ignores fds", listenPid: "9999", listenFds: "1", ask: "lmtp", wantOK: false},
		{name: "named match picks the right fd", listenPid: "4242", listenFds: "1", listenNames: "lmtp", ask: "lmtp", want: 3, wantOK: true},
		{name: "named match at second position", listenPid: "4242", listenFds: "2", listenNames: "http:lmtp", ask: "lmtp", want: 4, wantOK: true},
		{name: "http claims its own fd in the same set", listenPid: "4242", listenFds: "2", listenNames: "http:lmtp", ask: "http", want: 3, wantOK: true},
		{name: "lone unnamed fd is accepted", listenPid: "4242", listenFds: "1", ask: "lmtp", want: 3, wantOK: true},
		{name: "multiple fds without a name match are rejected", listenPid: "4242", listenFds: "2", listenNames: "http:other", ask: "lmtp", wantOK: false},
		{name: "zero fds rejected", listenPid: "4242", listenFds: "0", ask: "lmtp", wantOK: false},
		{name: "garbage fds rejected", listenPid: "4242", listenFds: "x", ask: "lmtp", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := parseActivation(pid, tt.listenPid, tt.listenFds, tt.listenNames)
			fd, ok := selectFD(a, tt.ask)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && fd != tt.want {
				t.Fatalf("fd = %d, want %d", fd, tt.want)
			}
		})
	}
}

// A lone unnamed fd may satisfy any single consumer, but never two — the
// second claimant must fall back to self-binding instead of fighting over
// accepts on the same socket.
func TestUnnamedLoneFdResolvesForBothNamesButClaimGuardsApply(t *testing.T) {
	a := parseActivation(4242, "4242", "1", "")
	for _, ask := range []string{"lmtp", "http"} {
		if fd, ok := selectFD(a, ask); !ok || fd != 3 {
			t.Fatalf("selectFD(%q) = %d,%v — want 3,true (claim guard, not selection, dedups)", ask, fd, ok)
		}
	}
}

// Boundary/malformed-input matrix ported from lmtpd's original contract test.
// One deliberate change: a lone fd carrying a DIFFERENT name is now rejected
// (was accepted) — with two consumers (lmtp + http), the liberal fallback
// could hand the LMTP socket to the HTTP listener.
func TestParseAndSelectBoundaryAndMalformedInputs(t *testing.T) {
	const pid = 4242
	for _, tt := range []struct {
		name   string
		pidEnv string
		fds    string
		names  string
		wantFD int
		wantOK bool
	}{
		{name: "missing pid", fds: "1"},
		{name: "missing fds", pidEnv: "4242"},
		{name: "bad pid", pidEnv: "bad", fds: "1"},
		{name: "other pid", pidEnv: "4243", fds: "1"},
		{name: "bad fds", pidEnv: "4242", fds: "bad"},
		{name: "zero fds", pidEnv: "4242", fds: "0"},
		{name: "negative fds", pidEnv: "4242", fds: "-1"},
		{name: "single unnamed", pidEnv: "4242", fds: "1", wantFD: 3, wantOK: true},
		{name: "single WRONG name is rejected (two-consumer safety)", pidEnv: "4242", fds: "1", names: "other"},
		{name: "second named", pidEnv: "4242", fds: "3", names: "one:lmtp:three", wantFD: 4, wantOK: true},
		{name: "multiple no match", pidEnv: "4242", fds: "2", names: "one:two"},
		{name: "fewer names than fds", pidEnv: "4242", fds: "3", names: "one"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := parseActivation(pid, tt.pidEnv, tt.fds, tt.names)
			fd, ok := selectFD(a, "lmtp")
			if fd != tt.wantFD || ok != tt.wantOK {
				t.Fatalf("selectFD = %d/%v, want %d/%v", fd, ok, tt.wantFD, tt.wantOK)
			}
		})
	}
}
