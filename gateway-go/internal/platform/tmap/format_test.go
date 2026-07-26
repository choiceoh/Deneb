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
		desc     string
		want     string
	}{
		{"좌회전에 거리", turnLeft, 190, "", "190m 앞 좌회전"},
		{"우회전에 거리", turnRight, 45, "", "45m 앞 우회전"},
		{"유턴", turnUTurn, 300, "", "300m 앞 유턴"},
		{"직진은 앞이 아니라 그대로", turnStraight, 310, "", "310m 직진"},
		{"출발은 거리 없음", turnStart, 0, "", "출발"},
		{"목적지는 거리 무시", turnGoal, 120, "", "목적지"},
		{"경유지", 185, 500, "", "경유지"},
		{"고속도로 구간", 103, 2400, "", "2.4km 고속도로"},
		{"거리 0인 회전은 방향만", turnLeft, 0, "", "좌회전"},
		{"모르는 코드는 지어내지 않는다", 77, 250, "", "250m 이동"},
		{"모르는 코드에 거리도 없으면", 77, 0, "", "이동"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shortInstruction(tc.turnType, tc.distance, tc.desc); got != tc.want {
				t.Errorf("shortInstruction(%d, %d, %q) = %q, want %q", tc.turnType, tc.distance, tc.desc, got, tc.want)
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

// TestShortInstructionUsesDescriptionForUnmappedCodes covers the pedestrian
// reality found on the live API: crossings come back as turnType codes that are
// not in the published mapping, and a bare "이동" is useless at a crossing.
func TestShortInstructionUsesDescriptionForUnmappedCodes(t *testing.T) {
	cases := []struct {
		name     string
		turnType int
		distance int
		desc     string
		want     string
	}{
		{
			"횡단보도(212)는 TMap 문장에서 조작을 가져온다",
			212, 34, "좌측 횡단보도 후 보행자도로를 따라 34m 이동",
			"34m 앞 좌측 횡단보도",
		},
		{
			"조작 없는 이동 문장에서는 지어내지 않는다",
			99, 37, "보행자도로를 따라 37m 이동",
			"37m 이동",
		},
		{
			"머리절이 너무 길면 버린다 (HUD 한 줄을 넘긴다)",
			99, 13,
			"아주아주 긴 지하철역 몇 번 출구 앞 광장에서 우회전 후 세종대로를 따라 13m 이동",
			"13m 이동",
		},
		{
			"매핑된 코드는 문장을 무시하고 짧은 형태를 쓴다",
			turnLeft, 190, "좌회전 후 소공로를 따라 190m 이동",
			"190m 앞 좌회전",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shortInstruction(tc.turnType, tc.distance, tc.desc); got != tc.want {
				t.Errorf("= %q, want %q", got, tc.want)
			}
		})
	}
}
