package server

import "testing"

func TestSenderEmailFromHeader(t *testing.T) {
	cases := []struct{ in, want string }{
		{"김성훈 <akim@bohae.co.kr>", "akim@bohae.co.kr"},
		{"\"Kim, Sung-hoon\" <sunghoon.kim@marsh.com>", "sunghoon.kim@marsh.com"},
		{"akim@bohae.co.kr", "akim@bohae.co.kr"}, // bare address
		{"오선택 전무", ""},                           // name only, no address
		{"", ""},
		{"weird <no-at-here>", ""}, // angle content without @ is not an address
	}
	for _, c := range cases {
		if got := senderEmailFromHeader(c.in); got != c.want {
			t.Errorf("senderEmailFromHeader(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
