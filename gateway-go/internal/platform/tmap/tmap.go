// Package tmap reads the SK TMap Open API for Korean turn-by-turn directions.
//
// Why TMap and not Naver/Kakao — the three Korean providers were compared on
// 2026-07-26 for a text-only monochrome HUD (the Even G2 glasses), and TMap is
// the only one that clears all three bars:
//
//   - 도보 + 자동차 both covered. Naver Directions is car-only — it has no
//     pedestrian API at all, which alone disqualifies it for glasses.
//   - Display-ready Korean sentences with the distance already embedded
//     ("소공로 을 따라 310m 이동"). Kakao Mobility's car API returns bare
//     maneuver tokens ("우회전") that the caller must sentence-build.
//   - Caching is permitted for 24 hours. Naver forbids storing results at all
//     ("결과 데이터를 별도로 저장해서는 안되며"), and Kakao staff enforce the
//     same reading actively. On a HUD that re-walks the same commute, a route
//     you may keep for a day is worth more than a marginally better polyline.
//
// None of the three requires results to be drawn on the provider's own map —
// that was the feared blocker and no such clause exists. What none of them
// addresses either way is a display with NO map at all, which is exactly this
// use case; absence of a prohibition is not permission, so that question is
// still open with the provider.
//
// Auth is a plain `appKey` header (env TMAP_APP_KEY, key from
// https://openapi.sk.com). Free tier is 1,000 routes/day, which is ample for
// one wearer; there is no metered fallback, so a missing key is reported as
// unavailable rather than silently degraded.
//
// CAVEAT — the request/response wire shapes here are implemented from TMap's
// published reference, NOT verified against the live service: no key existed
// when this was written. The normalization and formatting below are covered by
// fixture tests, but the HTTP layer is unproven. First live call should be
// treated as a test.
package tmap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	apiBase = "https://apis.openapi.sk.com"

	// requestTimeout — a wearer waiting on a route has no patience and no
	// screen to explain a stall; fail fast and let the caller re-ask.
	requestTimeout = 8 * time.Second

	// MaxSteps caps a normalized route. Long intercity routes run to hundreds
	// of maneuvers, and nothing downstream can show more than a handful; the
	// cap keeps a stray Seoul→Busan request from ballooning the response.
	MaxSteps = 200
)

// ErrNoKey is returned when TMAP_APP_KEY is unset. Callers surface this as
// "not configured" — deliberately distinct from a routing failure, because the
// remedies are different (add a key vs. try another destination).
var ErrNoKey = errors.New("tmap: TMAP_APP_KEY unset")

// Mode selects the routing profile. TMap serves these from different paths.
type Mode string

const (
	ModeWalk Mode = "walk"
	ModeCar  Mode = "car"
)

// Coord is a WGS84 point. TMap speaks lon/lat (x/y) in that order.
type Coord struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// Place is one POI search hit.
type Place struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Coord   Coord  `json:"coord"`
}

// Step is one maneuver, normalized for a narrow display.
//
// Both texts are kept on purpose. Full is TMap's own sentence, which is good
// Korean but routinely too long for 576px; Short is synthesized from turnType
// and distance so there is always something that fits. The renderer picks.
type Step struct {
	// Short is the fitted form — "190m 앞 좌회전". Never empty.
	Short string `json:"short"`
	// Full is TMap's cleaned description, "" when it carried nothing useful.
	Full string `json:"full"`
	// DistanceM is metres from the previous maneuver to this one.
	DistanceM int `json:"distanceM"`
	// TurnType is TMap's raw code, kept so a renderer can pick a glyph.
	TurnType int `json:"turnType"`
	// Coord is where the maneuver happens — the caller matches position to it.
	Coord Coord `json:"coord"`
}

// Route is a normalized route ready for display.
type Route struct {
	Steps      []Step `json:"steps"`
	TotalM     int    `json:"totalM"`
	TotalSec   int    `json:"totalSec"`
	Mode       Mode   `json:"mode"`
	Truncated  bool   `json:"truncated"`
	Suppressed int    `json:"suppressed"`
}

// APIKey returns the configured TMap app key, "" when unset.
func APIKey() string { return strings.TrimSpace(os.Getenv("TMAP_APP_KEY")) }

// Available reports whether routing can be attempted at all.
func Available() bool { return APIKey() != "" }

// httpDoFn is the test seam — fixture tests swap it to exercise decoding and
// error mapping without a key or a network.
var httpDoFn = func(req *http.Request) (*http.Response, error) {
	return (&http.Client{Timeout: requestTimeout}).Do(req)
}

func do(ctx context.Context, method, path string, body any, out any) error {
	key := APIKey()
	if key == "" {
		return ErrNoKey
	}
	var rdr *bytes.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("tmap: encode: %w", err)
		}
		rdr = bytes.NewReader(buf)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, apiBase+path, rdr)
	if err != nil {
		return fmt.Errorf("tmap: request: %w", err)
	}
	req.Header.Set("appKey", key)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpDoFn(req)
	if err != nil {
		return fmt.Errorf("tmap: call: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("tmap: %s: HTTP %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("tmap: decode: %w", err)
	}
	return nil
}

// SearchPlaces resolves a spoken/typed destination to candidate coordinates.
//
// The glasses have no keyboard, so the destination arrives as a name and this
// is the only way to turn it into a routable point. Results come back in
// TMap's own relevance order; the caller picks (usually the first).
func SearchPlaces(ctx context.Context, keyword string, near *Coord, limit int) ([]Place, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, errors.New("tmap: empty keyword")
	}
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	q := url.Values{}
	q.Set("version", "1")
	q.Set("searchKeyword", keyword)
	q.Set("count", fmt.Sprint(limit))
	if near != nil {
		// Biasing by current position is what makes "역" or "주민센터" resolve
		// to the one you are standing near instead of the one in Seoul.
		q.Set("centerLat", fmt.Sprintf("%.6f", near.Lat))
		q.Set("centerLon", fmt.Sprintf("%.6f", near.Lon))
		q.Set("searchtypCd", "R")
	}

	var raw struct {
		SearchPoiInfo struct {
			Pois struct {
				Poi []struct {
					Name           string `json:"name"`
					UpperAddrName  string `json:"upperAddrName"`
					MiddleAddrName string `json:"middleAddrName"`
					LowerAddrName  string `json:"lowerAddrName"`
					RoadName       string `json:"roadName"`
					FirstBuildNo   string `json:"firstBuildNo"`
					NoorLat        string `json:"noorLat"`
					NoorLon        string `json:"noorLon"`
				} `json:"poi"`
			} `json:"pois"`
		} `json:"searchPoiInfo"`
	}
	if err := do(ctx, http.MethodGet, "/tmap/pois?"+q.Encode(), nil, &raw); err != nil {
		return nil, err
	}

	out := make([]Place, 0, len(raw.SearchPoiInfo.Pois.Poi))
	for _, p := range raw.SearchPoiInfo.Pois.Poi {
		lat, lon := parseCoord(p.NoorLat), parseCoord(p.NoorLon)
		if lat == 0 || lon == 0 {
			continue // a POI we cannot route to is noise on a 10-line screen
		}
		out = append(out, Place{
			Name:    strings.TrimSpace(p.Name),
			Address: joinAddr(p.UpperAddrName, p.MiddleAddrName, p.LowerAddrName, p.RoadName, p.FirstBuildNo),
			Coord:   Coord{Lat: lat, Lon: lon},
		})
	}
	return out, nil
}

// Directions fetches a route between two points.
func Directions(ctx context.Context, from, to Coord, mode Mode) (*Route, error) {
	path := "/tmap/routes/pedestrian?version=1"
	body := map[string]any{
		"startX": fmt.Sprintf("%.6f", from.Lon), "startY": fmt.Sprintf("%.6f", from.Lat),
		"endX": fmt.Sprintf("%.6f", to.Lon), "endY": fmt.Sprintf("%.6f", to.Lat),
		"reqCoordType": "WGS84GEO", "resCoordType": "WGS84GEO",
	}
	if mode == ModeCar {
		path = "/tmap/routes?version=1"
		body["searchOption"] = "0"
	} else {
		// TMap rejects a pedestrian request without names; they are echoed
		// back in the description text, so they are cosmetic but required.
		body["startName"] = "출발"
		body["endName"] = "도착"
	}

	var raw struct {
		Features []struct {
			Geometry struct {
				Type        string          `json:"type"`
				Coordinates json.RawMessage `json:"coordinates"`
			} `json:"geometry"`
			Properties struct {
				Index         int    `json:"index"`
				Description   string `json:"description"`
				TurnType      int    `json:"turnType"`
				Distance      int    `json:"distance"`
				Time          int    `json:"time"`
				TotalDistance int    `json:"totalDistance"`
				TotalTime     int    `json:"totalTime"`
			} `json:"properties"`
		} `json:"features"`
	}
	if err := do(ctx, http.MethodPost, path, body, &raw); err != nil {
		return nil, err
	}
	if len(raw.Features) == 0 {
		return nil, errors.New("tmap: no route")
	}

	route := &Route{Mode: mode}
	for _, f := range raw.Features {
		p := f.Properties
		if p.TotalDistance > 0 {
			route.TotalM, route.TotalSec = p.TotalDistance, p.TotalTime
		}
		// Only Point features are maneuvers; LineStrings are the geometry
		// between them and carry no instruction worth a line on the HUD.
		if f.Geometry.Type != "Point" {
			continue
		}
		full := cleanDescription(p.Description)
		if full == "" && p.TurnType == 0 {
			route.Suppressed++
			continue
		}
		if len(route.Steps) >= MaxSteps {
			route.Truncated = true
			break
		}
		lat, lon := parsePoint(f.Geometry.Coordinates)
		route.Steps = append(route.Steps, Step{
			Short:     shortInstruction(p.TurnType, p.Distance),
			Full:      full,
			DistanceM: p.Distance,
			TurnType:  p.TurnType,
			Coord:     Coord{Lat: lat, Lon: lon},
		})
	}
	if len(route.Steps) == 0 {
		return nil, errors.New("tmap: route had no maneuvers")
	}
	return route, nil
}
