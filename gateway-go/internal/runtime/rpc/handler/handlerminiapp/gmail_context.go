// gmail_context.go — miniapp.gmail.sender_context RPC.
//
// Given a Gmail sender ("Name <email>", raw email, or just a name), assemble
// what Deneb already knows about that person so the Mini App detail view
// can show a contextual card instead of treating each email as an anonymous
// arrival.
//
// Two sources combined:
//
//   1. Gmail itself — `from:<email> newer_than:30d` to count recent
//      messages and grab the timestamp of the last one. Fast (single API
//      call) and gives the freshness signal a busy operator actually
//      reads.
//
//   2. Wiki memory — `wiki.Store.Search` on the display name. Pulls back
//      the operator's hand-curated notes about this person/company
//      (frontmatter title/summary/category). Empty when the person isn't
//      in memory yet, which is itself useful information ("새로운 연락처").
//
// The graphify CLI integration (`extractWikiGraphContext` in
// `gmailpoll/pipeline.go`) is intentionally **not** included here — it
// shells out to an external binary that may not be present, has a 10s
// timeout that would dominate the response, and the two sources above
// already cover the high-frequency triage need. Folding it in is a clean
// follow-up.

package handlerminiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/mail"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// GmailContextDeps groups the two factories the handler needs. Both can
// fail at construction (no OAuth / no wiki) — the handler then surfaces
// UNAVAILABLE for that source and still returns whatever the other source
// produced.
type GmailContextDeps struct {
	Client      func() (GmailClient, error)
	WikiStore   func() (MemorySearcher, error)
	RecentDays  int // Lookback window for "from:<email> newer_than:..."; 0 → 30.
	MaxRecent   int // Cap on Gmail.Search results; 0 → 50.
	MaxWikiHits int // Cap on wiki search results; 0 → 5.
}

// GmailContextMethods returns the miniapp.gmail.sender_context handler.
// Returns nil if neither source is wired — the gateway then skips
// registration cleanly.
func GmailContextMethods(deps GmailContextDeps) map[string]rpcutil.HandlerFunc {
	if deps.Client == nil && deps.WikiStore == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.gmail.sender_context": senderContext(deps),
	}
}

func senderContext(deps GmailContextDeps) rpcutil.HandlerFunc {
	type params struct {
		Sender string `json:"sender"`
	}
	type wikiHitOut struct {
		Path     string `json:"path"`
		Title    string `json:"title,omitempty"`
		Summary  string `json:"summary,omitempty"`
		Category string `json:"category,omitempty"`
	}
	type recentOut struct {
		Count          int    `json:"count"`
		LastReceivedAt string `json:"lastReceivedAt,omitempty"` // ISO 8601
		WindowDays     int    `json:"windowDays"`
	}
	type out struct {
		Sender      string       `json:"sender"`
		Email       string       `json:"email,omitempty"`
		DisplayName string       `json:"displayName,omitempty"`
		Recent      *recentOut   `json:"recent,omitempty"`
		WikiHits    []wikiHitOut `json:"wikiHits"`
		// Notes the handler attaches when a source was unavailable so
		// the client can render "wiki not configured" hints instead of
		// silently empty cards.
		Notices []string `json:"notices,omitempty"`
	}
	recentDays := deps.RecentDays
	if recentDays <= 0 {
		recentDays = 30
	}
	maxRecent := deps.MaxRecent
	if maxRecent <= 0 {
		maxRecent = 50
	}
	maxWiki := deps.MaxWikiHits
	if maxWiki <= 0 {
		maxWiki = 5
	}

	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		var p params
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}
		raw := strings.TrimSpace(p.Sender)
		if raw == "" {
			return rpcerr.MissingParam("sender").Response(req.ID)
		}

		email, displayName := parseSender(raw)
		resp := out{
			Sender:      raw,
			Email:       email,
			DisplayName: displayName,
			WikiHits:    []wikiHitOut{},
		}

		// --- Gmail recent activity ---
		if deps.Client != nil && email != "" {
			client, err := deps.Client()
			if err != nil {
				resp.Notices = append(resp.Notices, "gmail unavailable: "+err.Error())
			} else {
				query := fmt.Sprintf("from:%s newer_than:%dd", email, recentDays)
				results, qerr := client.Search(ctx, query, maxRecent)
				if qerr != nil {
					resp.Notices = append(resp.Notices, "gmail search failed: "+qerr.Error())
				} else {
					rec := &recentOut{Count: len(results), WindowDays: recentDays}
					if len(results) > 0 {
						// Search results are returned newest-first by
						// Gmail's API; the first row carries the most
						// recent received timestamp.
						rec.LastReceivedAt = normalizeDate(results[0].Date)
					}
					resp.Recent = rec
				}
			}
		}

		// --- Wiki hand-curated notes ---
		if deps.WikiStore != nil {
			store, err := deps.WikiStore()
			if err != nil {
				resp.Notices = append(resp.Notices, "memory unavailable: "+err.Error())
			} else {
				// Prefer the display name for the query (matches title
				// field in person/company pages); fall back to the raw
				// input if we couldn't parse one out.
				wikiQuery := displayName
				if wikiQuery == "" {
					wikiQuery = raw
				}
				hits, werr := store.Search(ctx, wikiQuery, maxWiki)
				if werr != nil {
					resp.Notices = append(resp.Notices, "memory search failed: "+werr.Error())
				} else {
					for _, h := range hits {
						row := wikiHitOut{Path: h.Path}
						if page, perr := store.ReadPage(h.Path); perr == nil && page != nil {
							row.Title = page.Meta.Title
							row.Summary = page.Meta.Summary
							row.Category = page.Meta.Category
						}
						resp.WikiHits = append(resp.WikiHits, row)
					}
				}
			}
		}

		return rpcutil.RespondOK(req.ID, resp)
	}
}

// parseSender splits "Display Name <email@host>" into ("email@host",
// "Display Name"). Tolerant of bare emails ("a@b.com") and bare names
// ("Alice"). Empty parts are returned as "".
func parseSender(raw string) (email, display string) {
	addr, err := mail.ParseAddress(raw)
	if err == nil {
		return strings.TrimSpace(addr.Address), strings.TrimSpace(addr.Name)
	}
	// Fallback: look for an obvious "<email>" pattern even if
	// mail.ParseAddress refused (e.g., display name has quoting issues).
	if i := strings.Index(raw, "<"); i >= 0 {
		j := strings.Index(raw[i:], ">")
		if j > 0 {
			email = strings.TrimSpace(raw[i+1 : i+j])
			display = strings.TrimSpace(raw[:i])
			display = strings.Trim(display, `"' `)
			return email, display
		}
	}
	// If it looks like a bare email, treat as email; otherwise treat as
	// display name.
	if strings.Contains(raw, "@") && !strings.ContainsAny(raw, " \t") {
		return raw, ""
	}
	return "", raw
}
