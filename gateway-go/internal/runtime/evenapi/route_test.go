package evenapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/tmap"
)

func routeHandler(t *testing.T) *Handler {
	t.Helper()
	return New(Config{Token: "tok"})
}

func postRoute(t *testing.T, h *Handler, body string, auth bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/even/route", strings.NewReader(body))
	if auth {
		req.Header.Set("Authorization", "Bearer tok")
	}
	rec := httptest.NewRecorder()
	h.Route(rec, req)
	return rec
}

// stubRoute swaps both TMap seams and restores them afterwards.
func stubRoute(t *testing.T,
	search func(context.Context, string, *tmap.Coord, int) ([]tmap.Place, error),
	directions func(context.Context, tmap.Coord, tmap.Coord, tmap.Mode) (*tmap.Route, error),
) {
	t.Helper()
	ps, pd := routeSearchFn, routeDirectionsFn
	if search != nil {
		routeSearchFn = search
	}
	if directions != nil {
		routeDirectionsFn = directions
	}
	t.Cleanup(func() { routeSearchFn, routeDirectionsFn = ps, pd })
}

func okRoute() *tmap.Route {
	return &tmap.Route{
		Mode:   tmap.ModeWalk,
		TotalM: 820,
		Steps:  []tmap.Step{{Short: "출발", TurnType: 200}, {Short: "190m 앞 좌회전", TurnType: 12}},
	}
}

func TestRouteRequiresAuth(t *testing.T) {
	rec := postRoute(t, routeHandler(t), `{"from":{"lat":37.5,"lon":127.0},"to":"강남역"}`, false)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", rec.Code)
	}
}

func TestRouteRequiresPosition(t *testing.T) {
	// Routing from a zero position would silently start the wearer in the Gulf
	// of Guinea; this must be an error, not a route.
	rec := postRoute(t, routeHandler(t), `{"to":"강남역"}`, true)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", rec.Code)
	}
}

func TestRouteRequiresDestination(t *testing.T) {
	rec := postRoute(t, routeHandler(t), `{"from":{"lat":37.5,"lon":127.0},"to":"  "}`, true)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", rec.Code)
	}
}

func TestRouteResolvesNameThenRoutes(t *testing.T) {
	var searched string
	var biasedBy *tmap.Coord
	stubRoute(t,
		func(_ context.Context, kw string, near *tmap.Coord, _ int) ([]tmap.Place, error) {
			searched, biasedBy = kw, near
			return []tmap.Place{{Name: "강남역", Address: "서울 강남구", Coord: tmap.Coord{Lat: 37.4979, Lon: 127.0276}}}, nil
		},
		func(_ context.Context, from, to tmap.Coord, mode tmap.Mode) (*tmap.Route, error) {
			if to.Lat != 37.4979 {
				t.Errorf("routed to %+v, want the searched POI", to)
			}
			if mode != tmap.ModeWalk {
				t.Errorf("mode = %q, want walk by default", mode)
			}
			return okRoute(), nil
		})

	rec := postRoute(t, routeHandler(t), `{"from":{"lat":37.5,"lon":127.0},"to":"강남역"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", rec.Code, rec.Body.String())
	}
	if searched != "강남역" {
		t.Errorf("searched %q", searched)
	}
	// Biasing by current position is what makes a generic name resolve nearby
	// rather than to the Seoul one.
	if biasedBy == nil || biasedBy.Lat != 37.5 {
		t.Errorf("search bias = %+v, want the caller's position", biasedBy)
	}
	var got struct {
		Route       tmap.Route `json:"route"`
		Destination struct {
			Name    string `json:"name"`
			Address string `json:"address"`
		} `json:"destination"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Destination.Name != "강남역" || got.Destination.Address != "서울 강남구" {
		t.Errorf("destination = %+v", got.Destination)
	}
	if len(got.Route.Steps) != 2 || got.Route.Steps[1].Short != "190m 앞 좌회전" {
		t.Errorf("route = %+v", got.Route)
	}
}

func TestRouteSkipsSearchWhenCoordGiven(t *testing.T) {
	stubRoute(t,
		func(context.Context, string, *tmap.Coord, int) ([]tmap.Place, error) {
			t.Error("POI search must not run when the caller supplied coordinates")
			return nil, nil
		},
		func(_ context.Context, _, to tmap.Coord, _ tmap.Mode) (*tmap.Route, error) {
			if to.Lat != 35.1 {
				t.Errorf("routed to %+v", to)
			}
			return okRoute(), nil
		})
	rec := postRoute(t, routeHandler(t), `{"from":{"lat":37.5,"lon":127.0},"toCoord":{"lat":35.1,"lon":129.0}}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRouteCarMode(t *testing.T) {
	stubRoute(t, nil, func(_ context.Context, _, _ tmap.Coord, mode tmap.Mode) (*tmap.Route, error) {
		if mode != tmap.ModeCar {
			t.Errorf("mode = %q, want car", mode)
		}
		return okRoute(), nil
	})
	rec := postRoute(t, routeHandler(t), `{"from":{"lat":37.5,"lon":127.0},"toCoord":{"lat":35.1,"lon":129.0},"mode":"CAR"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestRouteNoResults(t *testing.T) {
	stubRoute(t, func(context.Context, string, *tmap.Coord, int) ([]tmap.Place, error) {
		return nil, nil
	}, nil)
	rec := postRoute(t, routeHandler(t), `{"from":{"lat":37.5,"lon":127.0},"to":"없는곳"}`, true)
	if rec.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", rec.Code)
	}
}

func TestRouteUnconfiguredIsNotAFailure(t *testing.T) {
	// "No key" and "routing broke" must not look the same: only one of them is
	// something the wearer can act on, and the other needs the operator.
	stubRoute(t, func(context.Context, string, *tmap.Coord, int) ([]tmap.Place, error) {
		return nil, tmap.ErrNoKey
	}, nil)
	rec := postRoute(t, routeHandler(t), `{"from":{"lat":37.5,"lon":127.0},"to":"강남역"}`, true)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("code = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "TMAP_APP_KEY") {
		t.Errorf("body should name the missing key: %s", rec.Body.String())
	}
}

func TestRouteBadJSON(t *testing.T) {
	rec := postRoute(t, routeHandler(t), `{nope`, true)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", rec.Code)
	}
}
