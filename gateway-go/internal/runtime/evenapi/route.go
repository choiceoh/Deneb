// route.go — POST /api/even/route: Korean turn-by-turn for the glasses.
//
// Why this exists: the G2's built-in Navigate is not backed by Korean map data,
// which in Korea is not a small handicap — map-export law keeps the global
// providers from serving driving directions here at all, so the native app is
// working from data that cannot be good. Deneb routes through TMap instead
// (see internal/platform/tmap for why TMap and not Naver/Kakao).
//
// The glasses do the display and the position stream; this endpoint does the
// two things the glasses cannot: resolve a spoken place name to coordinates,
// and turn a route into lines that fit 576px of monochrome. Which maneuver is
// "current" as the wearer moves is decided on the glasses — it must keep
// working when the network does not.
package evenapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/tmap"
)

// Test seams — swapped by handler tests so route wiring can be asserted
// without a TMap key or a network (same pattern as the web package's
// provider seams).
var (
	routeDirectionsFn = tmap.Directions
	routeSearchFn     = tmap.SearchPlaces
)

type routeRequest struct {
	// From is the wearer's current position. Required — there is no useful
	// route without it, and guessing one would send them the wrong way.
	From tmap.Coord `json:"from"`
	// To is a place name ("강남역"). Resolved against TMap POI search, biased
	// by From so "주민센터" means the nearby one.
	To string `json:"to"`
	// ToCoord skips the search when the caller already has coordinates.
	ToCoord *tmap.Coord `json:"toCoord"`
	// Mode is "walk" (default) or "car".
	Mode string `json:"mode"`
}

// Route resolves a destination and returns a normalized route.
func (h *Handler) Route(w http.ResponseWriter, r *http.Request) {
	if !h.Enabled() {
		writeErr(w, http.StatusServiceUnavailable, "even g2 bridge disabled (set "+EnvBridgeToken+")")
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if !tokenMatch(h.token, ParseBearer(r.Header.Get("Authorization"))) {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body routeRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad json")
		return
	}
	if body.From.Lat == 0 && body.From.Lon == 0 {
		writeErr(w, http.StatusBadRequest, "현재 위치가 필요합니다")
		return
	}

	mode := tmap.ModeWalk
	if strings.EqualFold(strings.TrimSpace(body.Mode), "car") {
		mode = tmap.ModeCar
	}

	dest := body.ToCoord
	var destName, destAddr string
	if dest == nil {
		keyword := strings.TrimSpace(body.To)
		if keyword == "" {
			writeErr(w, http.StatusBadRequest, "목적지가 필요합니다")
			return
		}
		places, err := routeSearchFn(r.Context(), keyword, &body.From, 5)
		if err != nil {
			h.logRoute("poi search failed", "keyword", keyword, "error", err)
			writeRouteErr(w, err)
			return
		}
		if len(places) == 0 {
			h.logRoute("destination not found", "keyword", keyword)
			writeErr(w, http.StatusNotFound, "목적지를 찾지 못했습니다")
			return
		}
		// First hit wins. TMap ranks by relevance against the bias point, and
		// a chooser is not affordable on four gestures — the wearer re-asks
		// with a longer name if it picked wrong.
		dest = &places[0].Coord
		destName, destAddr = places[0].Name, places[0].Address
	}

	route, err := routeDirectionsFn(r.Context(), body.From, *dest, mode)
	if err != nil {
		h.logRoute("routing failed", "dest", destName, "mode", string(mode), "error", err)
		writeRouteErr(w, err)
		return
	}
	// Logged on success too: when the wearer reports the HUD looked wrong, this
	// line is the only server-side record of what was actually sent. Without it
	// the whole feature is diagnosable only by re-running it by hand, which is
	// how the last three wire defects had to be found.
	h.logRoute("route served",
		"dest", destName, "mode", string(mode),
		"steps", len(route.Steps), "totalM", route.TotalM,
		"truncated", route.Truncated)

	writeJSON(w, http.StatusOK, map[string]any{
		"route": route,
		"destination": map[string]any{
			"name":    destName,
			"address": destAddr,
			"coord":   dest,
		},
	})
}

// writeRouteErr keeps "no key configured" distinct from "routing failed".
// They look identical on the glass otherwise, and only one of them is fixed by
// the wearer doing something differently.
func writeRouteErr(w http.ResponseWriter, err error) {
	if errors.Is(err, tmap.ErrNoKey) {
		writeErr(w, http.StatusServiceUnavailable, "길안내가 설정되지 않았습니다 (TMAP_APP_KEY)")
		return
	}
	writeErr(w, http.StatusBadGateway, err.Error())
}

// logRoute emits one structured line per route attempt, or nothing when the
// handler was built without a logger (tests).
func (h *Handler) logRoute(msg string, args ...any) {
	if h == nil || h.logger == nil {
		return
	}
	h.logger.Info("even g2 route: "+msg, args...)
}

// ── HUD mirror ───────────────────────────────────────────────────────────────

// hudFrame is what the glasses are showing right now, as the plugin sees it.
//
// This exists because remote diagnosis was impossible without it. Three separate
// device reports this session ("화살표가 안떠", "배터리 0%", "경로계산중만 나온다")
// each cost a round trip of guessing, because the only record of what the panel
// actually displayed was a WebView console nobody could read. The plugin now
// posts each frame it draws and the gateway logs it, so the rendered state is
// visible from srv4.
type hudFrame struct {
	// Screen is the plugin's mode ("page", "nav", "recall", …).
	Screen string `json:"screen"`
	// Text is the literal content handed to the text container.
	Text string `json:"text"`
	// Images reports each image container's last push and its result, which is
	// the part that was invisible: a failed bitmap looks identical to one that
	// was never attempted.
	Images []hudImage `json:"images"`
	// Note carries anything the plugin wants to say about this frame.
	Note string `json:"note"`
}

type hudImage struct {
	Name   string `json:"name"`
	Bytes  int    `json:"bytes"`
	Result string `json:"result"`
}

// Hud records one rendered frame. Fire-and-forget: a mirror that can fail the
// render it is mirroring would be worse than no mirror.
func (h *Handler) Hud(w http.ResponseWriter, r *http.Request) {
	if !h.Enabled() {
		writeErr(w, http.StatusServiceUnavailable, "even g2 bridge disabled")
		return
	}
	if !tokenMatch(h.token, ParseBearer(r.Header.Get("Authorization"))) {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var f hudFrame
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		writeErr(w, http.StatusBadRequest, "bad json")
		return
	}
	args := []any{"screen", f.Screen, "text", f.Text}
	if f.Note != "" {
		args = append(args, "note", f.Note)
	}
	for _, im := range f.Images {
		args = append(args, "image:"+im.Name, im.Result, "image:"+im.Name+":bytes", im.Bytes)
	}
	if h.logger != nil {
		h.logger.Info("even g2 hud", args...)
	}
	h.mu.Lock()
	h.lastHud = f
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// HudLast returns the most recent frame, so the current glasses display can be
// read from a shell on srv4 rather than inferred from a description.
func (h *Handler) HudLast(w http.ResponseWriter, r *http.Request) {
	if !tokenMatch(h.token, ParseBearer(r.Header.Get("Authorization"))) {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	h.mu.Lock()
	f := h.lastHud
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, f)
}
