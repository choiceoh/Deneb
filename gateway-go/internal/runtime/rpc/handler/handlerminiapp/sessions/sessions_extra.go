package sessions

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/minibind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

const (
	defaultSessionSearchLimit = 20
	maxSessionSearchLimit     = 50
	sessionSearchSnippetRunes = 120
)

func sessionsPin(deps SessionsDeps) rpcutil.HandlerFunc {
	type params struct {
		SessionKey string `json:"sessionKey"`
		Pinned     bool   `json:"pinned"`
	}
	return minibind.BindOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		key := strings.TrimSpace(p.SessionKey)
		if key == "" {
			return rpcerr.MissingParam("sessionKey").Response(req.ID)
		}
		if deps.Manager.Get(key) == nil {
			return rpcutil.RespondOK(req.ID, map[string]any{"pinned": false, "ok": false})
		}
		pinned := p.Pinned
		s := deps.Manager.Patch(key, session.PatchFields{Pinned: &pinned})
		return rpcutil.RespondOK(req.ID, map[string]any{"ok": s != nil, "pinned": pinned})
	})
}

func sessionsFocus(deps SessionsDeps) rpcutil.HandlerFunc {
	type params struct {
		SessionKey string `json:"sessionKey"`
	}
	return minibind.BindOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		key := strings.TrimSpace(p.SessionKey)
		if key != "" && deps.SetFocus != nil {
			if err := deps.SetFocus(key); err != nil {
				return rpcerr.WrapUnavailable("session focus persist failed", err).Response(req.ID)
			}
		}
		current := key
		if deps.Focus != nil {
			current = deps.Focus()
		}
		return rpcutil.RespondOK(req.ID, sessionFocusResult{SessionKey: current})
	})
}

func sessionsSearch(deps SessionsDeps) rpcutil.HandlerFunc {
	type params struct {
		Query      string `json:"query"`
		MaxResults int    `json:"maxResults,omitempty"`
	}
	return minibind.BindOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		query := strings.TrimSpace(p.Query)
		if query == "" {
			return rpcerr.MissingParam("query").Response(req.ID)
		}
		limit := p.MaxResults
		if limit <= 0 {
			limit = defaultSessionSearchLimit
		}
		if limit > maxSessionSearchLimit {
			limit = maxSessionSearchLimit
		}

		hits := searchSessionLabels(deps.Manager, query, limit)
		seen := make(map[string]struct{}, len(hits))
		for _, hit := range hits {
			seen[hit.SessionKey] = struct{}{}
		}

		store, err := deps.Transcripts()
		if err != nil && len(hits) == 0 {
			return rpcerr.WrapUnavailable("transcript store unavailable", err).Response(req.ID)
		}
		if err == nil && store != nil && len(hits) < limit {
			results, searchErr := store.Search(query, limit)
			if searchErr != nil && len(hits) == 0 {
				return rpcerr.WrapUnavailable("transcript search failed", searchErr).Response(req.ID)
			}
			if searchErr != nil {
				results = nil
			}
			for _, result := range results {
				if len(hits) >= limit {
					break
				}
				if _, dup := seen[result.SessionKey]; dup {
					continue
				}
				seen[result.SessionKey] = struct{}{}
				hits = append(hits, sessionSearchHitOut{
					SessionKey: result.SessionKey,
					Snippet:    snippetFromSearch(result),
					Label:      sessionLabel(deps.Manager, result.SessionKey),
				})
			}
		}
		return rpcutil.RespondOK(req.ID, sessionSearchResult{Hits: hits})
	})
}

func searchSessionLabels(mgr SessionsLister, query string, limit int) []sessionSearchHitOut {
	if mgr == nil {
		return nil
	}
	needle := strings.ToLower(query)
	var hits []sessionSearchHitOut
	for _, s := range mgr.List() {
		if s == nil {
			continue
		}
		if len(hits) >= limit {
			break
		}
		label := strings.TrimSpace(s.Label)
		haystack := strings.ToLower(label + " " + s.Key)
		if !strings.Contains(haystack, needle) {
			continue
		}
		hits = append(hits, sessionSearchHitOut{
			SessionKey: s.Key,
			Label:      label,
		})
	}
	return hits
}

func sessionLabel(mgr SessionsLister, key string) string {
	if mgr == nil {
		return ""
	}
	if s := mgr.Get(key); s != nil {
		return s.Label
	}
	return ""
}

func snippetFromSearch(result toolport.SearchResult) string {
	if len(result.Matches) == 0 {
		return ""
	}
	text := strings.TrimSpace(result.Matches[0].Message.TextContent())
	return truncateRunes(text, sessionSearchSnippetRunes)
}

func truncateRunes(s string, max int) string {
	if max <= 0 || s == "" {
		return s
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}
