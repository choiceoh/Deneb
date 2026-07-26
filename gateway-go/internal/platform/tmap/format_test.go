package tmap

import (
	"encoding/json"
	"testing"
)

func TestShortInstruction(t *testing.T) {
	cases := []struct {
		name     string
		turnType int
		distance int
		want     string
	}{
		{"좌회전에 거리", turnLeft, 190, "190m 앞 좌회전"},
		{"우회전에 거리", turnRight, 45, "45m 앞 우회전"},
		{"유턴", turnUTurn, 300, "300m 앞 유턴"},
		{"직진은 앞이 아니라 그대로", turnStraight, 310, "310m 직진"},
		{"출발은 거리 없음", turnStart, 0, "출발"},
		{"목적지는 거리 무시", turnGoal, 120, "목적지"},
		{"경유지", 185, 500, "경유지"},
		{"고속도로 구간", 103, 2400, "2.4km 고속도로"},
		{"거리 0인 회전은 방향만", turnLeft, 0, "좌회전"},
		{"모르는 코드는 지어내지 않는다", 77, 250, "250m 이동"},
		{"모르는 코드에 거리도 없으면", 77, 0, "이동"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shortInstruction(tc.turnType, tc.distance); got != tc.want {
				t.Errorf("shortInstruction(%d, %d) = %q, want %q", tc.turnType, tc.distance, got, tc.want)
			}
		})
	}
}

func TestFormatDistance(t *testing.T) {
	cases := []struct {
		m    int
		want string
	}{
		{0, ""},
		{-5, ""},
		{1, "1m"},
		{999, "999m"},
		{1000, "1km"},
		{1050, "1.1km"},
		{2400, "2.4km"},
		{12000, "12km"},
	}
	for _, tc := range cases {
		if got := formatDistance(tc.m); got != tc.want {
			t.Errorf("formatDistance(%d) = %q, want %q", tc.m, got, tc.want)
		}
	}
}

func TestCleanDescription(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			"TMap이 조사를 띄어 보내는 실제 형태",
			"소공로 을 따라 소공로 방면으로 310m 이동",
			"소공로을 따라 소공로 방면으로 310m 이동",
		},
		{
			"공백 정규화",
			"  남산3호터널/반포대교   방면으로 좌회전  ",
			"남산3호터널/반포대교 방면으로 좌회전",
		},
		{
			"문장 끝 조사도 붙인다",
			"세종대로 를",
			"세종대로를",
		},
		{
			// The bug this list was pruned for: 이 is a demonstrative, so
			// re-attaching it would corrupt a sentence that was already right.
			"지시관형사 이는 절대 건드리지 않는다",
			"다음 이 건물을 지나 우회전",
			"다음 이 건물을 지나 우회전",
		},
		{
			"조사로 시작하는 도로명은 쪼개지 않는다",
			"을지로 방면으로 좌회전",
			"을지로 방면으로 좌회전",
		},
		{
			"에서도 붙인다",
			"강남대로 에서 우회전",
			"강남대로에서 우회전",
		},
		{"빈 문자열", "", ""},
		{"공백만", "   ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cleanDescription(tc.in); got != tc.want {
				t.Errorf("cleanDescription(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParsePoint(t *testing.T) {
	// GeoJSON is [lon, lat] — swapping these puts Seoul in the Yellow Sea, so
	// the order is asserted explicitly rather than left to the reader.
	lat, lon := parsePoint(json.RawMessage(`[126.978, 37.566]`))
	if lat != 37.566 || lon != 126.978 {
		t.Errorf("parsePoint = (%v, %v), want (37.566, 126.978)", lat, lon)
	}
	for _, bad := range []string{`[]`, `[1]`, `"nope"`, `null`, `{}`} {
		if la, lo := parsePoint(json.RawMessage(bad)); la != 0 || lo != 0 {
			t.Errorf("parsePoint(%s) = (%v, %v), want zeroes", bad, la, lo)
		}
	}
}

func TestParseCoord(t *testing.T) {
	if got := parseCoord(" 37.566 "); got != 37.566 {
		t.Errorf("parseCoord = %v, want 37.566", got)
	}
	for _, bad := range []string{"", "nope", "  "} {
		if got := parseCoord(bad); got != 0 {
			t.Errorf("parseCoord(%q) = %v, want 0", bad, got)
		}
	}
}

func TestJoinAddr(t *testing.T) {
	if got := joinAddr("서울", "중구", "", "소공로", "70"); got != "서울 중구 소공로 70" {
		t.Errorf("joinAddr = %q", got)
	}
	if got := joinAddr("", "  ", ""); got != "" {
		t.Errorf("joinAddr(blanks) = %q, want empty", got)
	}
}
