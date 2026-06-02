// gmail.go — miniapp.gmail.* RPC handlers.
//
// The Mini App webview talks to these to power its Gmail triage UI:
//
//	miniapp.gmail.list_recent  — recent messages matching a Gmail query
//	miniapp.gmail.get          — full message body + headers + attachments
//	miniapp.gmail.mark_read    — remove the UNREAD label
//	miniapp.gmail.archive      — remove the INBOX label
//	miniapp.gmail.trash        — move the message to Gmail's Trash folder
//
// Every method assumes the request already passed client-token verification
// (the HTTP bridge in server_http_miniapp.go enforces that before the
// dispatcher is reached), so handlers only re-check that the client identity is
// actually attached and return UNAUTHORIZED if it is missing.
//
// The handlers depend on a GmailClient interface rather than the concrete
// *gmail.Client so tests can drop in a fake without standing up the OAuth
// flow. Production wiring in method_registry.go passes a closure around
// gmail.DefaultClient().

package handlerminiapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// GmailClient is the subset of *gmail.Client the handlers need.
// Defined here so tests can supply a fake without importing the real
// OAuth client.
type GmailClient interface {
	Search(ctx context.Context, query string, maxResults int) ([]gmail.MessageSummary, error)
	SearchPage(ctx context.Context, query, pageToken string, maxResults int) ([]gmail.MessageSummary, string, error)
	GetMessage(ctx context.Context, messageID string) (*gmail.MessageDetail, error)
	ModifyLabels(ctx context.Context, messageID string, addNames, removeNames []string) error
	Trash(ctx context.Context, messageID string) error
}

// GmailDeps groups the values the handlers need at registration time.
// Client is a lazy factory rather than a *gmail.Client instance because
// DefaultClient() can fail at startup (no OAuth tokens yet) and we want
// the gateway to keep running even then — failures surface per-call as
// UNAVAILABLE responses instead.
type GmailDeps struct {
	Client func() (GmailClient, error)
}

// Default list query and limit applied when the Mini App omits them.
// Tuned for triage: everything in the inbox OR still unread (the latter
// captures auto-archived-yet-unread mail from filter workflows), in
// the last week, single screenful. Gmail uses {} as a logical OR group.
const (
	defaultGmailQuery    = "{in:inbox is:unread} newer_than:7d"
	defaultGmailLimit    = 20
	maxGmailLimit        = 100
	maxGmailBodyChars    = 3000
	labelUnread          = "UNREAD"
	labelInbox           = "INBOX"
	bodyTruncationSuffix = "\n\n...[truncated, total=%d chars]"
	// maxEmptyPageHops bounds the server-side absorption loop for the
	// "Gmail returns 0 messages with a non-empty nextPageToken" case
	// (legitimate response from filter-heavy queries) so the Mini App
	// never sees an empty page that secretly has more results behind
	// it. 5 is enough for plausible filter shapes without blowing
	// out the request budget.
	maxEmptyPageHops = 5
)

// GmailMethods returns the miniapp.gmail.* handler map. Returns nil if deps
// has no Client factory — handler registration in method_registry.go can
// then skip wiring without crashing the server.
func GmailMethods(deps GmailDeps) map[string]rpcutil.HandlerFunc {
	if deps.Client == nil {
		return nil
	}
	// One cache shared across the handlers: list_recent fills it, while
	// archive/trash invalidate it. mark_read intentionally does NOT — it
	// leaves inbox membership intact and the client fires it on every
	// open, so invalidating there would defeat the cache (see
	// gmail_list_cache.go).
	cache := newListCache(listCacheTTL)
	return map[string]rpcutil.HandlerFunc{
		"miniapp.gmail.list_recent": gmailListRecent(deps, cache),
		"miniapp.gmail.get":         gmailGet(deps),
		"miniapp.gmail.mark_read":   gmailMarkRead(deps),
		"miniapp.gmail.archive":     gmailArchive(deps, cache),
		"miniapp.gmail.trash":       gmailTrash(deps, cache),
	}
}

// requireAuth enforces that a native client identity has been attached
// upstream by the HTTP bridge. All Mini App handlers share this guard.
func requireAuth(ctx context.Context, reqID string) *protocol.ResponseFrame {
	if clientauth.FromContext(ctx) == nil {
		return rpcerr.New(protocol.ErrUnauthorized, "miniapp request missing client identity context").Response(reqID)
	}
	return nil
}

// gmailClientOrErr resolves the lazy client factory, mapping the err to an
// UNAVAILABLE response so the Mini App can show a "Gmail not configured"
// banner instead of a generic failure.
func gmailClientOrErr(deps GmailDeps, reqID string) (GmailClient, *protocol.ResponseFrame) {
	client, err := deps.Client()
	if err != nil {
		return nil, rpcerr.WrapUnavailable("gmail client unavailable", err).Response(reqID)
	}
	return client, nil
}

// --- list_recent ---------------------------------------------------------

func gmailListRecent(deps GmailDeps, cache *listCache) rpcutil.HandlerFunc {
	type params struct {
		Query     string `json:"query,omitempty"`
		Limit     int    `json:"limit,omitempty"`
		PageToken string `json:"pageToken,omitempty"`
	}
	type rowOut struct {
		ID       string   `json:"id"`
		ThreadID string   `json:"threadId"`
		From     string   `json:"from"`
		Subject  string   `json:"subject"`
		Snippet  string   `json:"snippet"`
		Date     string   `json:"date"`
		IsUnread bool     `json:"isUnread"`
		Labels   []string `json:"labels"`
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
		query := strings.TrimSpace(p.Query)
		if query == "" {
			query = defaultGmailQuery
		}
		limit := p.Limit
		if limit <= 0 {
			limit = defaultGmailLimit
		}
		if limit > maxGmailLimit {
			limit = maxGmailLimit
		}

		// Serve a recent identical page from cache so re-entering the
		// inbox (back from a mail, tab switch) is instant. Keyed by the
		// exact query/limit/page so pagination and custom queries each
		// cache independently. A nil cache (tests) makes get a no-op.
		cacheKey := query + "|" + itoa(limit) + "|" + p.PageToken
		now := time.Now()
		if payload, ok := cache.get(cacheKey, now); ok {
			return rpcutil.RespondOK(req.ID, payload)
		}

		client, errResp := gmailClientOrErr(deps, req.ID)
		if errResp != nil {
			return errResp
		}
		results, nextPageToken, err := client.SearchPage(ctx, query, p.PageToken, limit)
		if err != nil {
			// Route through mapGmailError so 403 (Gmail OAuth scope
			// missing) and 404 stay distinguishable from transient
			// outages — the client can surface different remediation
			// hints. Matches get/mark_read/archive's behavior.
			return mapGmailError(req.ID, "gmail search failed", err)
		}
		// Absorb the "empty page + token" case server-side: Gmail can
		// legitimately return 0 messages with a non-empty
		// nextPageToken when server-side filtering eats a chunk, and
		// the Mini App can't tell the difference between that and a
		// truly empty inbox. Hop forward up to maxEmptyPageHops times
		// until we get at least one message or run out of pages.
		for hops := 0; hops < maxEmptyPageHops && len(results) == 0 && nextPageToken != ""; hops++ {
			results, nextPageToken, err = client.SearchPage(ctx, query, nextPageToken, limit)
			if err != nil {
				return mapGmailError(req.ID, "gmail search failed", err)
			}
		}

		out := make([]rowOut, 0, len(results))
		for _, m := range results {
			out = append(out, rowOut{
				ID:       m.ID,
				ThreadID: m.ThreadID,
				From:     m.From,
				Subject:  m.Subject,
				Snippet:  m.Snippet,
				Date:     normalizeDate(m.Date),
				IsUnread: hasUnreadLabel(m.Labels),
				Labels:   m.Labels,
			})
		}
		payload := map[string]any{
			"messages":      out,
			"nextPageToken": nextPageToken,
		}
		cache.put(cacheKey, payload, now)
		return rpcutil.RespondOK(req.ID, payload)
	}
}

// --- get -----------------------------------------------------------------

func gmailGet(deps GmailDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	type attachmentOut struct {
		ID       string `json:"id"`
		Filename string `json:"filename"`
		MimeType string `json:"mimeType"`
		Size     int    `json:"size"`
	}
	type messageOut struct {
		ID          string          `json:"id"`
		ThreadID    string          `json:"threadId"`
		From        string          `json:"from"`
		To          string          `json:"to"`
		CC          string          `json:"cc,omitempty"`
		Subject     string          `json:"subject"`
		Date        string          `json:"date"`
		Body        string          `json:"body"`
		BodyTotal   int             `json:"bodyTotal"`
		Labels      []string        `json:"labels"`
		Attachments []attachmentOut `json:"attachments"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[params](req)
		if errResp != nil {
			return errResp
		}
		if strings.TrimSpace(p.ID) == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}

		client, errResp := gmailClientOrErr(deps, req.ID)
		if errResp != nil {
			return errResp
		}
		msg, err := client.GetMessage(ctx, p.ID)
		if err != nil {
			return mapGmailError(req.ID, "gmail get failed", err)
		}
		if msg == nil {
			return rpcerr.NotFound("message " + rpcutil.TruncateForError(p.ID)).Response(req.ID)
		}

		body, total := truncateBody(msg.Body, maxGmailBodyChars)
		atts := make([]attachmentOut, 0, len(msg.Attachments))
		for _, a := range msg.Attachments {
			atts = append(atts, attachmentOut{
				ID:       a.AttachmentID,
				Filename: a.Filename,
				MimeType: a.MimeType,
				Size:     a.Size,
			})
		}
		return rpcutil.RespondOK(req.ID, messageOut{
			ID:          msg.ID,
			ThreadID:    msg.ThreadID,
			From:        msg.From,
			To:          msg.To,
			CC:          msg.CC,
			Subject:     msg.Subject,
			Date:        normalizeDate(msg.Date),
			Body:        body,
			BodyTotal:   total,
			Labels:      msg.Labels,
			Attachments: atts,
		})
	}
}

// --- mark_read / archive --------------------------------------------------

func gmailMarkRead(deps GmailDeps) rpcutil.HandlerFunc {
	// nil cache: marking read leaves inbox membership unchanged, so a
	// cached list stays valid (the client updates the read dot
	// optimistically). See gmail_list_cache.go for why this must not
	// invalidate.
	return modifyLabelsHandler(deps, nil, []string{labelUnread})
}

func gmailArchive(deps GmailDeps, cache *listCache) rpcutil.HandlerFunc {
	// Archive drops the message from the inbox, so any cached list is now
	// stale — invalidate so the next list reflects the removal.
	return modifyLabelsHandler(deps, cache, []string{labelInbox})
}

// gmailTrash moves a message to Trash via Gmail's dedicated /trash
// endpoint (rather than ModifyLabels add=TRASH) so we skip a label-ID
// lookup round-trip and stay aligned with how the Gmail web client
// performs deletes — recoverable from the user's Trash UI for ~30 days.
func gmailTrash(deps GmailDeps, cache *listCache) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[params](req)
		if errResp != nil {
			return errResp
		}
		if strings.TrimSpace(p.ID) == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}

		client, errResp := gmailClientOrErr(deps, req.ID)
		if errResp != nil {
			return errResp
		}
		if err := client.Trash(ctx, p.ID); err != nil {
			return mapGmailError(req.ID, "gmail trash failed", err)
		}
		// Trashing removes the message from the inbox — drop the cached
		// list so the next fetch no longer includes it.
		cache.invalidate()
		return rpcutil.RespondOK(req.ID, map[string]any{"ok": true})
	}
}

// modifyLabelsHandler builds a handler that removes the given labels from
// the message identified by params.id and returns the resulting label set
// so the Mini App can update its row without a follow-up fetch.
func modifyLabelsHandler(deps GmailDeps, cache *listCache, removeLabels []string) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[params](req)
		if errResp != nil {
			return errResp
		}
		if strings.TrimSpace(p.ID) == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}

		client, errResp := gmailClientOrErr(deps, req.ID)
		if errResp != nil {
			return errResp
		}
		if err := client.ModifyLabels(ctx, p.ID, nil, removeLabels); err != nil {
			return mapGmailError(req.ID, "gmail modify labels failed", err)
		}
		// Invalidate when this action changes inbox membership (archive
		// passes a cache; mark_read passes nil, a no-op).
		cache.invalidate()
		// Re-fetch metadata for the updated label list. Skipped silently
		// on failure — the action itself succeeded.
		labels := []string{}
		if msg, err := client.GetMessage(ctx, p.ID); err == nil && msg != nil {
			labels = msg.Labels
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"ok":     true,
			"labels": labels,
		})
	}
}

// --- helpers --------------------------------------------------------------

// hasUnreadLabel reports whether labels contains the Gmail UNREAD system
// label. Inline (rather than a generic hasLabel(labels, target) helper)
// because every production caller wants the same target — lint's unparam
// check rightly flags any single-call-target helper as suspicious.
func hasUnreadLabel(labels []string) bool {
	for _, l := range labels {
		if l == labelUnread {
			return true
		}
	}
	return false
}

// normalizeDate parses Gmail's RFC 2822 Date header into ISO 8601 / RFC 3339.
// On parse failure it returns the raw input — the client renders whatever it
// gets, so a malformed header is better than an empty cell.
func normalizeDate(raw string) string {
	if raw == "" {
		return ""
	}
	t, err := mail.ParseDate(raw)
	if err != nil {
		return raw
	}
	return t.UTC().Format(time.RFC3339)
}

// truncateBody clips the body to maxChars runes (not bytes — Korean and
// emoji count as one each) and appends a marker stating the original length.
// Returns the trimmed body plus the original char count so the client can
// show "1234 / 3000+ chars" hints if it wants.
func truncateBody(body string, maxChars int) (trimmed string, totalChars int) {
	runes := []rune(body)
	totalChars = len(runes)
	if totalChars <= maxChars {
		return body, totalChars
	}
	return string(runes[:maxChars]) + suffixFor(totalChars), totalChars
}

func suffixFor(total int) string {
	return strings.NewReplacer("%d", itoa(total)).Replace(bodyTruncationSuffix)
}

// itoa avoids strconv import for the single integer-to-decimal we need.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	negative := false
	if n < 0 {
		negative = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// mapGmailError classifies a Gmail client error into an RPC error response.
// Gmail returns HTTP-shaped errors via the client; we map well-known ones
// to NOT_FOUND / FORBIDDEN and lump the rest under UNAVAILABLE so the
// Mini App can choose between "retry" and "show the operator".
func mapGmailError(reqID, msg string, err error) *protocol.ResponseFrame {
	if err == nil {
		return rpcerr.Unavailable(msg).Response(reqID)
	}
	text := err.Error()
	switch {
	case errors.Is(err, errGmailNotFound) || strings.Contains(text, "404") || strings.Contains(strings.ToLower(text), "not found"):
		return rpcerr.NotFound(msg).Response(reqID)
	case strings.Contains(text, "403") || strings.Contains(strings.ToLower(text), "forbidden"):
		return rpcerr.New(protocol.ErrForbidden, msg+": "+text).Response(reqID)
	case strings.Contains(text, "400") || strings.Contains(strings.ToLower(text), "invalid"):
		// Most commonly: a stale or malformed pageToken sent by the
		// Mini App. Map to INVALID_REQUEST so the client surfaces
		// "reset to first page" instead of looping on "retry".
		return rpcerr.InvalidParams(fmt.Errorf("%s: %s", msg, text)).Response(reqID)
	default:
		return rpcerr.WrapUnavailable(msg, err).Response(reqID)
	}
}

// errGmailNotFound is a sentinel callers may wrap to force the NOT_FOUND
// branch in mapGmailError; primarily exposed for tests.
var errGmailNotFound = errors.New("gmail: message not found")
