package tmap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// stubHTTP swaps the client seam for one canned response and restores it after
// the test. Tests that need to inspect the outgoing request install their own
// closure instead (see TestDirectionsSendsKeyAndCoordOrder) — the request shape
// is the part no fixture can prove on its own, since the live API was
// unreachable when this was written.
func stubHTTP(t *testing.T, status int, body string) {
	t.Helper()
	prev := httpDoFn
	httpDoFn = func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	}
	t.Cleanup(func() { httpDoFn = prev })
}

func TestAPIKeyAndAvailable(t *testing.T) {
	t.Setenv("TMAP_APP_KEY", "  ")
	if Available() {
		t.Error("blank key must not count as available")
	}
	t.Setenv("TMAP_APP_KEY", " k ")
	if !Available() || APIKey() != "k" {
		t.Errorf("APIKey() = %q, want trimmed k", APIKey())
	}
}

func TestMissingKeyIsDistinctError(t *testing.T) {
	t.Setenv("TMAP_APP_KEY", "")
	// A missing key must be tellable apart from a routing failure: the fix is
	// "configure a key", not "pick another destination".
	if _, err := Directions(context.Background(), Coord{}, Coord{}, ModeWalk); !errors.Is(err, ErrNoKey) {
		t.Errorf("Directions err = %v, want ErrNoKey", err)
	}
	if _, err := SearchPlaces(context.Background(), "강남역", nil, 5); !errors.Is(err, ErrNoKey) {
		t.Errorf("SearchPlaces err = %v, want ErrNoKey", err)
	}
}

func TestSearchPlacesDecodes(t *testing.T) {
	t.Setenv("TMAP_APP_KEY", "k")
	body := `{"searchPoiInfo":{"pois":{"poi":[
	  {"name":"강남역","upperAddrName":"서울","middleAddrName":"강남구","lowerAddrName":"역삼동",
	   "roadName":"강남대로","firstBuildNo":"396","noorLat":"37.4979","noorLon":"127.0276",
	   "frontLat":"37.4980","frontLon":"127.0280"},
	  {"name":"좌표없는곳","noorLat":"","noorLon":""}
	]}}}`
	stubHTTP(t, http.StatusOK, body)

	got, err := SearchPlaces(context.Background(), "강남역", &Coord{Lat: 37.5, Lon: 127.0}, 5)
	if err != nil {
		t.Fatalf("SearchPlaces: %v", err)
	}
	// The coordless POI is dropped — it cannot be routed to, so on a ten-line
	// screen it is pure noise.
	if len(got) != 1 {
		t.Fatalf("got %d places, want 1 (coordless dropped)", len(got))
	}
	// The entrance wins over the centroid — they differ by hundreds of metres
	// at large venues, which is the difference between arriving and standing
	// in the middle of a building.
	if got[0].Name != "강남역" || got[0].Coord.Lat != 37.4980 || got[0].Coord.Lon != 127.0280 {
		t.Errorf("place = %+v, want the frontLat/frontLon entrance", got[0])
	}
	if got[0].Address != "서울 강남구 역삼동 강남대로 396" {
		t.Errorf("address = %q", got[0].Address)
	}
}

func TestSearchPlacesRejectsEmptyKeyword(t *testing.T) {
	t.Setenv("TMAP_APP_KEY", "k")
	if _, err := SearchPlaces(context.Background(), "   ", nil, 5); err == nil {
		t.Error("empty keyword must error before spending a call")
	}
}

// routeBody mirrors the LIVE pedestrian response shape observed 2026-07-26:
// Point features carry the maneuver and NO distance field, and the metres sit
// on the LineString(s) that follow — sometimes more than one per leg. An
// earlier version of this fixture invented a `distance` on the Points, which
// is why the code shipped reading a field the API never sends.
const routeBody = `{"features":[
  {"type":"Feature","geometry":{"type":"Point","coordinates":[126.978,37.566]},
   "properties":{"index":0,"description":"보행자도로를 따라 310m 이동","turnType":200,
                 "totalDistance":820,"totalTime":610}},
  {"type":"Feature","geometry":{"type":"LineString","coordinates":[[126.978,37.566],[126.979,37.567]]},
   "properties":{"index":1,"description":"소공로, 310m","distance":310}},
  {"type":"Feature","geometry":{"type":"Point","coordinates":[126.979,37.567]},
   "properties":{"index":2,"description":"좌회전 후 소공로 을 따라 190m 이동","turnType":12}},
  {"type":"Feature","geometry":{"type":"LineString","coordinates":[[126.979,37.567],[126.9795,37.5675]]},
   "properties":{"index":3,"description":"소공로, 190m","distance":190}},
  {"type":"Feature","geometry":{"type":"LineString","coordinates":[[126.9795,37.5675],[126.980,37.568]]},
   "properties":{"index":4,"description":"무교로, 320m","distance":320}},
  {"type":"Feature","geometry":{"type":"Point","coordinates":[126.980,37.568]},
   "properties":{"index":5,"description":"목적지","turnType":201}}
]}`

func TestDirectionsNormalizes(t *testing.T) {
	t.Setenv("TMAP_APP_KEY", "k")
	stubHTTP(t, http.StatusOK, routeBody)

	r, err := Directions(context.Background(), Coord{Lat: 37.566, Lon: 126.978}, Coord{Lat: 37.568, Lon: 126.980}, ModeWalk)
	if err != nil {
		t.Fatalf("Directions: %v", err)
	}
	// LineStrings are geometry, not maneuvers — three Points in, three steps out.
	if len(r.Steps) != 3 {
		t.Fatalf("got %d steps, want 3 (LineString must not become a step)", len(r.Steps))
	}
	if r.TotalM != 820 || r.TotalSec != 610 {
		t.Errorf("totals = %dm/%ds, want 820/610", r.TotalM, r.TotalSec)
	}
	if r.Steps[0].Short != "출발" || r.Steps[2].Short != "목적지" {
		t.Errorf("endpoints = %q … %q", r.Steps[0].Short, r.Steps[2].Short)
	}
	// The distance comes from the LineString BEFORE the maneuver — the metres
	// needed to reach it, which is how the HUD phrases it. The Point itself
	// carries no distance at all.
	if r.Steps[1].Short != "310m 앞 좌회전" {
		t.Errorf("short = %q, want 310m 앞 좌회전 (from the preceding LineString)", r.Steps[1].Short)
	}
	if r.Steps[1].DistanceM != 310 {
		t.Errorf("distanceM = %d, want 310", r.Steps[1].DistanceM)
	}
	if r.Steps[1].Full != "좌회전 후 소공로을 따라 190m 이동" {
		t.Errorf("full = %q (particle should be re-attached)", r.Steps[1].Full)
	}
	// Two LineStrings separate the turn from the destination; both count.
	if r.Steps[2].DistanceM != 510 {
		t.Errorf("destination distance = %d, want 510 (190+320 summed)", r.Steps[2].DistanceM)
	}
	if r.Steps[1].Coord.Lat != 37.567 || r.Steps[1].Coord.Lon != 126.979 {
		t.Errorf("coord = %+v, want lat 37.567 lon 126.979", r.Steps[1].Coord)
	}
}

func TestDirectionsSendsKeyAndCoordOrder(t *testing.T) {
	t.Setenv("TMAP_APP_KEY", "secret")
	var captured *http.Request
	prev := httpDoFn
	httpDoFn = func(req *http.Request) (*http.Response, error) {
		captured = req
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(routeBody))}, nil
	}
	t.Cleanup(func() { httpDoFn = prev })

	if _, err := Directions(context.Background(), Coord{Lat: 37.566, Lon: 126.978}, Coord{Lat: 37.568, Lon: 126.980}, ModeCar); err != nil {
		t.Fatalf("Directions: %v", err)
	}
	if captured.Header.Get("appKey") != "secret" {
		t.Errorf("appKey header = %q", captured.Header.Get("appKey"))
	}
	// Car and walk are different endpoints; routing a driver down a footpath
	// is the kind of error that only shows up on the road.
	if !strings.Contains(captured.URL.Path, "/tmap/routes") || strings.Contains(captured.URL.Path, "pedestrian") {
		t.Errorf("car mode hit %q, want the non-pedestrian route path", captured.URL.Path)
	}
	raw, err := io.ReadAll(captured.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var sent map[string]any
	if err := json.Unmarshal(raw, &sent); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	// TMap takes X as longitude and Y as latitude. Swapping them is the classic
	// geo bug and it fails silently — the API happily routes you to the Yellow
	// Sea — so the mapping is pinned here rather than trusted to the reader.
	if sent["startX"] != "126.978000" || sent["startY"] != "37.566000" {
		t.Errorf("start = X:%v Y:%v, want X=lon 126.978000 Y=lat 37.566000", sent["startX"], sent["startY"])
	}
	if sent["endX"] != "126.980000" || sent["endY"] != "37.568000" {
		t.Errorf("end = X:%v Y:%v, want X=lon 126.980000 Y=lat 37.568000", sent["endX"], sent["endY"])
	}
}

func TestDirectionsHTTPError(t *testing.T) {
	t.Setenv("TMAP_APP_KEY", "k")
	stubHTTP(t, http.StatusUnauthorized, `{"error":"bad key"}`)
	_, err := Directions(context.Background(), Coord{}, Coord{}, ModeWalk)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("err = %v, want one naming HTTP 401", err)
	}
}

func TestDirectionsEmptyRoute(t *testing.T) {
	t.Setenv("TMAP_APP_KEY", "k")
	stubHTTP(t, http.StatusOK, `{"features":[]}`)
	if _, err := Directions(context.Background(), Coord{}, Coord{}, ModeWalk); err == nil {
		t.Error("empty feature list must be an error, not an empty route")
	}
}

func TestDirectionsGeometryOnlyRoute(t *testing.T) {
	t.Setenv("TMAP_APP_KEY", "k")
	// A response with geometry but no Point features would otherwise yield a
	// route with zero steps — a HUD showing nothing while claiming success.
	stubHTTP(t, http.StatusOK, `{"features":[
	  {"type":"Feature","geometry":{"type":"LineString","coordinates":[[1,2],[3,4]]},
	   "properties":{"description":"길","distance":10}}]}`)
	if _, err := Directions(context.Background(), Coord{}, Coord{}, ModeWalk); err == nil {
		t.Error("geometry-only route must error rather than return zero maneuvers")
	}
}

func TestDirectionsTruncatesLongRoute(t *testing.T) {
	t.Setenv("TMAP_APP_KEY", "k")
	var b strings.Builder
	b.WriteString(`{"features":[`)
	for i := 0; i < MaxSteps+25; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"type":"Feature","geometry":{"type":"Point","coordinates":[126.9,37.5]},`+
			`"properties":{"index":%d,"description":"직진","turnType":11,"distance":100,"totalDistance":9999}}`, i)
	}
	b.WriteString(`]}`)
	stubHTTP(t, http.StatusOK, b.String())

	r, err := Directions(context.Background(), Coord{}, Coord{}, ModeCar)
	if err != nil {
		t.Fatalf("Directions: %v", err)
	}
	if len(r.Steps) != MaxSteps || !r.Truncated {
		t.Errorf("steps=%d truncated=%v, want %d and true", len(r.Steps), r.Truncated, MaxSteps)
	}
}

func TestSearchPlacesFallsBackToCentroid(t *testing.T) {
	t.Setenv("TMAP_APP_KEY", "k")
	// Rural POIs come back with no mapped entrance. Dropping them would lose
	// exactly the destinations that are hardest to find by eye.
	stubHTTP(t, http.StatusOK, `{"searchPoiInfo":{"pois":{"poi":[
	  {"name":"석문호","noorLat":"36.9","noorLon":"126.6","frontLat":"","frontLon":""}
	]}}}`)
	got, err := SearchPlaces(context.Background(), "석문호", nil, 5)
	if err != nil {
		t.Fatalf("SearchPlaces: %v", err)
	}
	if len(got) != 1 || got[0].Coord.Lat != 36.9 {
		t.Errorf("got %+v, want the centroid fallback", got)
	}
}
