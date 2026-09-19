package enginecontrol

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// RouterProvider is the provider id Deneb reaches the router (wormhole) under:
// a role on the engine is bound to "wormhole/<entry>".
const RouterProvider = "wormhole"

// Entry is one router entry that reaches the engine — what it answers to,
// what it asks the engine for, and the variant it is (configresolve.EngineEntry,
// passed in so this package stays below the runtime layer).
type Entry struct {
	Name          string
	UpstreamModel string
	ThinkingMode  string
	Vision        *bool // nil: the entry declares nothing
}

// Move re-points one role. From and To are full model ids (provider/entry).
type Move struct {
	Role string `json:"role"`
	From string `json:"from"`
	To   string `json:"to"`
}

// Skip is a role on the engine that could not follow, and why — said in the
// operator's language because it ends up on the engine screen.
type Skip struct {
	Role   string `json:"role"`
	From   string `json:"from"`
	Reason string `json:"reason"`
}

// VisionRole is the one role that must not follow to a text-only door.
const VisionRole = "vision"

// PlanFollow decides which roles follow the engine to the model it serves.
//
// roles maps a role to its full model id. A role follows when it is bound to
// an engine entry whose model the engine does not serve now: it moves to the
// engine entry of the same variant — the same thinking mode — that asks for
// the served model. Every request to the old entry would otherwise fail over
// (the router marks an entry whose upstream model its backend does not list),
// and a switch of the engine would quietly move every local role to the
// fallback chain.
//
// The vision role moves only to an entry that says it takes images. A door
// without its vision tower refuses an image outright (ST: "images and videos
// are not served by this deployment") — the model can see (Qwen3.8's checkpoint
// has the tower), but a deployment that dropped it at preshard cannot, and the
// router's entry is where that is declared.
//
// Roles bound to anything else — a cloud model, another engine — are not the
// engine's and never move. Nothing moves while served is empty.
func PlanFollow(roles map[string]string, entries []Entry, served string) (moves []Move, skips []Skip) {
	served = strings.TrimSpace(served)
	if served == "" {
		return nil, nil
	}
	byName := make(map[string]Entry, len(entries))
	for _, e := range entries {
		byName[e.Name] = e
	}
	names := make([]string, 0, len(roles))
	for role := range roles {
		names = append(names, role)
	}
	sort.Strings(names)
	for _, role := range names {
		from := strings.TrimSpace(roles[role])
		provider, entryName, ok := strings.Cut(from, "/")
		if !ok || provider != RouterProvider {
			continue
		}
		cur, onEngine := byName[entryName]
		if !onEngine || cur.UpstreamModel == served {
			continue
		}
		target, found := sameVariant(entries, cur, served)
		switch {
		case !found:
			skips = append(skips, Skip{
				Role: role, From: from,
				Reason: "라우터에 " + served + " 의 " + variantLabel(cur.ThinkingMode) + " 엔트리가 없습니다",
			})
		case role == VisionRole && (target.Vision == nil || !*target.Vision):
			skips = append(skips, Skip{
				Role: role, From: from,
				Reason: target.Name + " 엔트리는 이미지를 받지 않습니다",
			})
		default:
			moves = append(moves, Move{Role: role, From: from, To: RouterProvider + "/" + target.Name})
		}
	}
	return moves, skips
}

// sameVariant finds the engine entry asking for model in cur's thinking mode;
// the lowest name wins when the router lists more than one.
func sameVariant(entries []Entry, cur Entry, model string) (Entry, bool) {
	var best Entry
	found := false
	for _, e := range entries {
		if e.UpstreamModel != model || e.ThinkingMode != cur.ThinkingMode {
			continue
		}
		if !found || e.Name < best.Name {
			best, found = e, true
		}
	}
	return best, found
}

func variantLabel(mode string) string {
	switch mode {
	case "off":
		return "생각 끈"
	case "on":
		return "생각 켠"
	case "":
		return "기본"
	default:
		return mode
	}
}

// FollowReport is what the routing follow did last: the pass that moved roles
// (At, Served, Moves) and what the latest pass held back (Skips).
type FollowReport struct {
	At     time.Time // when roles last moved; zero before any
	Served string    // the model they moved to
	Moves  []Move
	Skips  []Skip // held back as of the latest pass that asked
}

// FollowLog keeps that for the engine screen: after a switch the operator reads
// there which roles went with the engine and which stayed, and why. A held-back
// role is planned again on every pass, so a pass that only holds back must not
// overwrite the moves before it. A nil log records and reports nothing.
type FollowLog struct {
	mu   sync.Mutex
	last FollowReport
}

// Record folds one pass in: its moves replace the last moves when it made any,
// and its skips always replace the held-back list (empty clears it).
func (l *FollowLog) Record(r FollowReport) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(r.Moves) > 0 {
		l.last.At, l.last.Served, l.last.Moves = r.At, r.Served, r.Moves
	}
	l.last.Skips = r.Skips
}

// Last is the most recent report; zero before any.
func (l *FollowLog) Last() FollowReport {
	if l == nil {
		return FollowReport{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.last
}
