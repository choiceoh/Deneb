package tmap

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// TMap turnType codes. Only the codes confirmed against TMap's published
// reference are mapped; anything else falls through to a distance-only phrase
// rather than being guessed at, because a wrong maneuver word on a HUD is
// worse than a vague one — the wearer acts on it at speed.
const (
	turnStraight = 11
	turnLeft     = 12
	turnRight    = 13
	turnUTurn    = 14
	turnStart    = 200
	turnGoal     = 201

	viaLo, viaHi         = 184, 189 // 경유지
	highwayLo, highwayHi = 101, 116 // 고속도로 진입/진출 (direction not distinguished)
)

// shortInstruction builds the fitted form for a narrow display: a distance and
// a maneuver, nothing else.
//
// This exists because TMap's own description is a full sentence with road names
// and 방면 hints — excellent on a phone, unreadable on 576px of monochrome. The
// renderer shows this first and demotes the sentence to a secondary line.
func shortInstruction(turnType, distanceM int, description string) string {
	d := formatDistance(distanceM)
	switch {
	case turnType == turnStart:
		return "출발"
	case turnType == turnGoal:
		return "목적지"
	case turnType >= viaLo && turnType <= viaHi:
		return "경유지"
	case turnType >= highwayLo && turnType <= highwayHi:
		if d == "" {
			return "고속도로"
		}
		return d + " 고속도로"
	case turnType == turnStraight:
		if d == "" {
			return "직진"
		}
		return d + " 직진"
	case turnType == turnLeft:
		return withDistance(d, "좌회전")
	case turnType == turnRight:
		return withDistance(d, "우회전")
	case turnType == turnUTurn:
		return withDistance(d, "유턴")
	default:
		// Unmapped code. Pedestrian routes are full of them — a single live
		// walk through central Seoul returned turnType 212 (좌측 횡단보도)
		// repeatedly, and "이동" tells a wearer at a crossing nothing.
		//
		// Rather than guess at codes that cannot be verified, take the maneuver
		// from TMap's OWN sentence: its head clause before "후" is the action
		// ("좌측 횡단보도 후 보행자도로를 따라 34m 이동"). That is not a guess,
		// it is the provider's wording, shortened.
		if m := maneuverPhrase(description); m != "" {
			return withDistance(d, m)
		}
		if d == "" {
			return "이동"
		}
		return d + " 이동"
	}
}

// maneuverPhrase extracts the action from a TMap instruction sentence.
//
// "" when the sentence is only a travel leg ("보행자도로를 따라 37m 이동") —
// there is no maneuver in it to name, and inventing one would be worse than
// falling back to a distance.
func maneuverPhrase(description string) string {
	s := strings.TrimSpace(description)
	if s == "" {
		return ""
	}
	idx := strings.Index(s, " 후 ")
	if idx <= 0 {
		return ""
	}
	head := strings.TrimSpace(s[:idx])
	// A head clause long enough to wrap defeats the purpose of the fitted form.
	if head == "" || len([]rune(head)) > 20 {
		return ""
	}
	return head
}

// withDistance renders "190m 앞 좌회전", or the bare maneuver when the distance
// is zero (which TMap emits for a turn you are already standing on).
func withDistance(d, maneuver string) string {
	if d == "" {
		return maneuver
	}
	return d + " 앞 " + maneuver
}

// formatDistance renders metres the way a driver reads them: whole metres up
// close, one decimal of a kilometre beyond that. "" for zero.
func formatDistance(m int) string {
	switch {
	case m <= 0:
		return ""
	case m < 1000:
		return strconv.Itoa(m) + "m"
	default:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(m)/1000), ".0") + "km"
	}
}

// particles that TMap routinely emits with a stray space in front, e.g.
// "소공로 을 따라 190m 이동". The API attaches the road name and the particle
// as separate tokens; joined text reads as broken Korean, and on a HUD there
// is no room for the reader to shrug it off.
// Deliberately excludes 이·가·로: "이" is also the demonstrative ("이 건물을
// 지나"), and re-attaching it would corrupt a correct sentence into "다음이
// 건물". Only particles that are never a standalone word are listed, so a
// false attachment is impossible rather than merely unlikely.
var strayParticles = []string{"에서", "으로", "을", "를", "은", "는", "와", "과"}

// cleanDescription normalizes TMap's instruction sentence for display:
// collapses whitespace and re-attaches particles that arrived detached.
func cleanDescription(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return ""
	}
	for _, p := range strayParticles {
		// Only a space-delimited particle is a stray one — " 을 " never occurs
		// as a real word, while "을지로" must not be touched, so the trailing
		// delimiter is required too.
		s = strings.ReplaceAll(s, " "+p+" ", p+" ")
		if strings.HasSuffix(s, " "+p) {
			s = strings.TrimSuffix(s, " "+p) + p
		}
	}
	return s
}

// parseCoord reads TMap's string-encoded coordinates. Returns 0 on anything
// unparseable, which callers treat as "drop this result".
func parseCoord(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return v
}

// parsePoint reads a GeoJSON Point's [lon, lat] pair. Returns zeroes when the
// shape is not a two-element numeric array.
func parsePoint(raw json.RawMessage) (lat, lon float64) {
	var pair []float64
	if err := json.Unmarshal(raw, &pair); err != nil || len(pair) < 2 {
		return 0, 0
	}
	return pair[1], pair[0]
}

// joinAddr assembles the human address from TMap's split fields, skipping the
// blanks it leaves for rural POIs that have no road name or building number.
func joinAddr(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " ")
}
