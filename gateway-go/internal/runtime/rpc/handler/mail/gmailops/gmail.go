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
package gmailops

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailbody"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailwork"
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

type nativeMailStatusClient interface {
	NativeStatus(ctx context.Context) (mailarchive.NativeStatus, error)
}

// GmailDeps groups the values the handlers need at registration time.
// Client is a lazy factory rather than a *gmail.Client instance because
// DefaultClient() can fail at startup (no OAuth tokens yet) and we want
// the gateway to keep running even then — failures surface per-call as
// UNAVAILABLE responses instead.
type GmailDeps struct {
	Client func() (GmailClient, error)
	// NotifyChanged, when set, fires after an inbox-membership mutation succeeds
	// (archive/trash — NOT mark_read, which keeps membership and the list cache)
	// so other clients force-warm their mail list via the native-sync mirror
	// (mail.changed). Nil disables (tests).
	NotifyChanged func(messageID string)
	// Priority scores one inbox row into a glanceable tier + short hint
	// (domain/mailpriority). Nil disables row priority — rows ship with
	// empty fields and the native inbox renders no marker.
	Priority func(from, subject, snippet string) (tier, hint string)
	// AnalysisCache, when set, lets list rows prefer the per-mail LLM
	// analysis verdict (analysisRecord.Importance) over the heuristic:
	// urgent/attention render the marker with a "분석 판정" hint, routine
	// SUPPRESSES the heuristic (the analysis looked at the full body and
	// judged it FYI), and absent/blank falls through to the heuristic.
	AnalysisCache *AnalysisStore
	// WorkState records Deneb-native workflow status: analysis, feed,
	// calendar-proposal, and to-do state per message ID. Nil disables the
	// overlay so legacy Gmail-only tests and deployments keep working.
	WorkState *mailwork.Store
	// MailStore is a lazy accessor (like Client) for the local mail mirror, which
	// is created in a later init phase than this handler's registration. When it
	// returns a non-nil reader, the get action serves bodies from it before the
	// Gmail API. Labels/stars aren't mirrored, so list keeps using Gmail for
	// authoritative state. nil accessor / nil reader = Gmail only.
	MailStore func() MailStoreReader
}

// MailStoreReader is the subset of the local mailstore the gmail handlers use to
// serve message bodies without a Gmail API round-trip. Satisfied by
// *mailstore.Store.
type MailStoreReader interface {
	Read(messageID, query string, mailboxes []string) (mailarchive.ContextMessage, bool)
}

// detailFromContext maps a stored ContextMessage back to the gmail.MessageDetail
// shape the get handler formats. Labels are intentionally empty — the store does
// not mirror Gmail's mutable label state.
func detailFromContext(m mailarchive.ContextMessage) *gmail.MessageDetail {
	return &gmail.MessageDetail{
		ID:              m.ID,
		From:            m.From,
		To:              m.To,
		CC:              m.CC,
		Subject:         m.Subject,
		Date:            m.Date,
		Body:            m.Body,
		MessageIDHeader: m.MessageID,
		References:      m.References,
		Attachments:     m.Attachments,
	}
}

// Default list query and limit applied when the Mini App omits them.
// Tuned for native triage: everything in the inbox OR still unread (the latter
// captures auto-archived-yet-unread mail from filter workflows), in the last
// month. Gmail uses {} as a logical OR group.
const (
	defaultGmailQuery = "{in:inbox is:unread} newer_than:30d"
	defaultGmailLimit = 60
	maxGmailLimit     = 100
	maxGmailBodyChars = 3000
	// maxGmailFullBodyChars caps the "full" body view requested by the
	// detail screen's 전체 보기 — generous enough for any real mail, bounded
	// so a pathological megabyte body can't be shipped to the phone whole.
	maxGmailFullBodyChars = 200_000
	labelUnread           = "UNREAD"
	labelInbox            = "INBOX"
	bodyTruncationSuffix  = "\n\n...[truncated, total=%d chars]"
	// maxEmptyPageHops bounds the server-side absorption loop for the
	// "Gmail returns 0 messages with a non-empty nextPageToken" case
	// (legitimate response from filter-heavy queries) so the Mini App
	// never sees an empty page that secretly has more results behind
	// it. 5 is enough for plausible filter shapes without blowing
	// out the request budget.
	maxEmptyPageHops = 5
	// maxWorkFilterPageHops bounds extra pages scanned for Deneb-native
	// workflow filters. These filters are applied after Gmail/archive search,
	// so the first backend page can legitimately contain zero matching rows.
	maxWorkFilterPageHops = 5
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
		"miniapp.gmail.list_recent":   gmailListRecent(deps, cache),
		"miniapp.gmail.get":           gmailGet(deps),
		"miniapp.gmail.mark_read":     gmailMarkRead(deps),
		"miniapp.gmail.archive":       gmailArchive(deps, cache),
		"miniapp.gmail.trash":         gmailTrash(deps, cache),
		"miniapp.gmail.native_status": gmailNativeStatus(deps),
	}
}

// gmailClientOrErr resolves the lazy client factory, mapping the err to an
// UNAVAILABLE response so the Mini App can show a "Gmail not configured"
// banner instead of a generic failure.
func gmailClientOrErr(deps GmailDeps, reqID string) (GmailClient, *protocol.ResponseFrame) {
	client, err := deps.Client()
	if err != nil {
		return nil, rpcerr.WrapUnavailable("mail client unavailable", err).Response(reqID)
	}
	return client, nil
}

func gmailNativeStatus(deps GmailDeps) rpcutil.HandlerFunc {
	return authenticated(func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		client, err := deps.Client()
		if err != nil || client == nil {
			out := mailNativeStatusOut{
				Source:    "unavailable",
				Available: false,
				Pipeline:  mailPipelineStatusOut(deps.WorkState),
			}
			if err != nil {
				out.Error = err.Error()
			}
			return rpcutil.RespondOK(req.ID, out)
		}
		native, ok := client.(nativeMailStatusClient)
		if !ok {
			return rpcutil.RespondOK(req.ID, mailNativeStatusOut{
				Source:         "gmail",
				Available:      true,
				OfflineCapable: false,
				Pipeline:       mailPipelineStatusOut(deps.WorkState),
			})
		}
		status, err := native.NativeStatus(ctx)
		out := nativeStatusOut(status)
		out.Pipeline = mailPipelineStatusOut(deps.WorkState)
		if err != nil {
			out.Available = false
			out.Error = err.Error()
		}
		return rpcutil.RespondOK(req.ID, out)
	})
}

func nativeStatusOut(status mailarchive.NativeStatus) mailNativeStatusOut {
	out := mailNativeStatusOut{
		Source:         status.Source,
		Available:      status.Available,
		OfflineCapable: status.OfflineCapable,
		Mailboxes:      make([]mailNativeMailboxOut, 0, len(status.Mailboxes)),
		Overlay: mailNativeOverlayOut{
			Messages: status.Overlay.Messages,
			Read:     status.Overlay.Read,
			Archived: status.Overlay.Archived,
			Trashed:  status.Overlay.Trashed,
		},
	}
	if !status.GeneratedAt.IsZero() {
		out.GeneratedAt = status.GeneratedAt.UTC().Format(time.RFC3339)
	}
	for _, m := range status.Mailboxes {
		out.Mailboxes = append(out.Mailboxes, mailNativeMailboxOut{
			Name:              m.Name,
			Total:             m.Total,
			Unread:            m.Unread,
			LocallyRead:       m.LocallyRead,
			LocallyArchived:   m.LocallyArchived,
			LocallyTrashed:    m.LocallyTrashed,
			LatestUID:         m.LatestUID,
			AttachmentCapable: m.AttachmentCapable,
		})
	}
	return out
}

func mailPipelineStatusOut(store *mailwork.Store) mailNativePipelineOut {
	if store == nil {
		return mailNativePipelineOut{}
	}
	s, err := store.SummaryWithError()
	out := mailNativePipelineOut{
		Messages:           s.Messages,
		Analyzed:           s.Analyzed,
		Analyzing:          s.Analyzing,
		Failed:             s.Failed,
		FeedCreated:        s.FeedCreated,
		FeedMissing:        s.FeedMissing,
		CalendarCandidates: s.CalendarCandidates,
		TodoCandidates:     s.TodoCandidates,
	}
	if s.UpdatedAtMs > 0 {
		out.UpdatedAt = time.UnixMilli(s.UpdatedAtMs).UTC().Format(time.RFC3339)
	}
	if err != nil {
		out.Error = "state_load_failed"
	}
	return out
}

func gmailListRecent(deps GmailDeps, cache *listCache) rpcutil.HandlerFunc {
	type params struct {
		Query     string `json:"query,omitempty"`
		Limit     int    `json:"limit,omitempty"`
		PageToken string `json:"pageToken,omitempty"`
	}
	return bindOptional(func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		rawQuery := strings.TrimSpace(p.Query)
		query, workFilter := parseMailWorkQuery(rawQuery)
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
		cacheQueryKey := rawQuery
		if cacheQueryKey == "" {
			cacheQueryKey = query
		}
		cacheKey := cacheQueryKey + "|" + itoa(limit) + "|" + p.PageToken + "|" + itoa64(mailWorkCacheRevision(deps))
		now := time.Now()
		if payload, ok := cache.get(cacheKey, now); ok {
			return rpcutil.RespondOK(req.ID, payload)
		}

		fetchPage := func(fctx context.Context) (map[string]any, *protocol.ResponseFrame) {
			client, errResp := gmailClientOrErr(deps, req.ID)
			if errResp != nil {
				return nil, errResp
			}
			searchLimit := limit
			if workFilter != "" {
				searchLimit = maxGmailLimit
			}
			results, nextPageToken, err := client.SearchPage(fctx, query, p.PageToken, searchLimit)
			if err != nil {
				// Route through mapGmailError so 403 (Gmail OAuth scope
				// missing) and 404 stay distinguishable from transient
				// outages — the client can surface different remediation
				// hints. Matches get/mark_read/archive's behavior.
				return nil, MapGmailError(req.ID, "mail search failed", err)
			}
			// Absorb the "empty page + token" case server-side: Gmail can
			// legitimately return 0 messages with a non-empty
			// nextPageToken when server-side filtering eats a chunk, and
			// the Mini App can't tell the difference between that and a
			// truly empty inbox. Hop forward up to maxEmptyPageHops times
			// until we get at least one message or run out of pages.
			for hops := 0; hops < maxEmptyPageHops && len(results) == 0 && nextPageToken != ""; hops++ {
				results, nextPageToken, err = client.SearchPage(fctx, query, nextPageToken, searchLimit)
				if err != nil {
					return nil, MapGmailError(req.ID, "mail search failed", err)
				}
			}

			out := appendMailRows(make([]mailRowOut, 0, len(results)), deps, results, workFilter, limit)
			for hops := 0; workFilter != "" && len(out) < limit && nextPageToken != "" && hops < maxWorkFilterPageHops; hops++ {
				results, nextPageToken, err = client.SearchPage(fctx, query, nextPageToken, searchLimit)
				if err != nil {
					return nil, MapGmailError(req.ID, "mail search failed", err)
				}
				out = appendMailRows(out, deps, results, workFilter, limit)
			}
			return map[string]any{
				"messages":      out,
				"nextPageToken": nextPageToken,
			}, nil
		}

		// Stale-while-revalidate: a page past TTL (≤5 min) is served
		// instantly and refreshed in the background, so re-opening the mail
		// tab never eats the ~2.5s cold fetch. Single-flight per key.
		if stale, ok, refresh, refreshGen := cache.getStale(cacheKey, now); ok {
			if refresh {
				go func() {
					defer cache.refreshDone(cacheKey)
					defer func() {
						if r := recover(); r != nil {
							slog.Error("mail list background refresh panic", "panic", r)
						}
					}()
					// Detached from the request ctx (it dies when this response
					// is written) but bounded — a hung refresh must not leak.
					rctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					if payload, errResp := fetchPage(rctx); errResp == nil {
						cache.putIfGeneration(cacheKey, payload, time.Now(), refreshGen)
					}
				}()
			}
			return rpcutil.RespondOK(req.ID, stale)
		}

		payload, errResp := fetchPage(ctx)
		if errResp != nil {
			return errResp
		}
		cache.put(cacheKey, payload, now)
		return rpcutil.RespondOK(req.ID, payload)
	})
}

// --- get -----------------------------------------------------------------

func gmailGet(deps GmailDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
		// Full requests the untruncated body (still bounded by
		// maxGmailFullBodyChars). The default keeps the 3000-char cap so the
		// list->detail flow stays light; the detail screen refetches with
		// full=true when the user asks for the rest.
		Full bool `json:"full,omitempty"`
	}
	return bind(func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		if strings.TrimSpace(p.ID) == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}

		// Serve the body from the local mailstore when present (no API round-trip).
		// get is a body-open — the list already showed authoritative label/star
		// state, which the store doesn't mirror. Fall back to Gmail on a miss or
		// when the store only has an empty-body review stub.
		var msg *gmail.MessageDetail
		if deps.MailStore != nil {
			if ms := deps.MailStore(); ms != nil {
				if cm, ok := ms.Read(p.ID, "", nil); ok && strings.TrimSpace(cm.Body) != "" {
					msg = detailFromContext(cm)
				}
			}
		}
		if msg == nil {
			client, errResp := gmailClientOrErr(deps, req.ID)
			if errResp != nil {
				return errResp
			}
			fetched, err := client.GetMessage(ctx, p.ID)
			if err != nil {
				return MapGmailError(req.ID, "mail get failed", err)
			}
			if fetched == nil {
				return rpcerr.NotFound("message " + rpcutil.TruncateForError(p.ID)).Response(req.ID)
			}
			msg = fetched
		}

		bodyLimit := maxGmailBodyChars
		if p.Full {
			bodyLimit = maxGmailFullBodyChars
		}
		cleaned := mailbody.CleanForDisplay(msg.Body)
		displayBody := cleaned.Body
		if strings.TrimSpace(displayBody) == "" && strings.TrimSpace(msg.Body) != "" {
			displayBody = strings.TrimSpace(msg.Body)
		}
		body, total := truncateBody(displayBody, bodyLimit)
		bodyCleaned := mailBodyWasCleaned(cleaned, msg.Body)
		rawBody := ""
		rawTotal := 0
		if bodyCleaned {
			rawBody, rawTotal = truncateBody(msg.Body, bodyLimit)
		}
		atts := make([]mailAttachmentOut, 0, len(msg.Attachments))
		for _, a := range msg.Attachments {
			atts = append(atts, mailAttachmentOut{
				ID:        a.AttachmentID,
				Filename:  a.Filename,
				MimeType:  a.MimeType,
				Size:      a.Size,
				Truncated: a.Truncated,
			})
		}
		out := mailMessageOut{
			ID:                   msg.ID,
			ThreadID:             msg.ThreadID,
			From:                 msg.From,
			To:                   msg.To,
			CC:                   msg.CC,
			Subject:              msg.Subject,
			Date:                 NormalizeDate(msg.Date),
			IsUnread:             hasUnreadLabel(msg.Labels),
			Body:                 body,
			BodyTotal:            total,
			RawBody:              rawBody,
			RawBodyTotal:         rawTotal,
			BodyCleaned:          bodyCleaned,
			BodyHiddenBlockCount: len(cleaned.HiddenBlocks),
			BodyHiddenLineCount:  mailHiddenLineCount(cleaned.HiddenBlocks),
			Labels:               nonNilLabels(msg.Labels),
			Attachments:          atts,
			RelatedProjects:      mailRelatedProjects(deps, msg.ID),
		}
		applyMailWorkMessage(&out, mailWorkStateForDetail(deps, msg))
		return rpcutil.RespondOK(req.ID, out)
	})
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
		// Full requests the untruncated body (still bounded by
		// maxGmailFullBodyChars). The default keeps the 3000-char cap so the
		// list->detail flow stays light; the detail screen refetches with
		// full=true when the user asks for the rest.
		Full bool `json:"full,omitempty"`
	}
	return bind(func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		if strings.TrimSpace(p.ID) == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}

		client, errResp := gmailClientOrErr(deps, req.ID)
		if errResp != nil {
			return errResp
		}
		if err := client.Trash(ctx, p.ID); err != nil {
			return MapGmailError(req.ID, "mail trash failed", err)
		}
		// Trashing removes the message from the inbox — drop the cached
		// list so the next fetch no longer includes it, and mirror the
		// membership change to other clients.
		cache.invalidate()
		if deps.NotifyChanged != nil {
			deps.NotifyChanged(p.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{"ok": true})
	})
}

// modifyLabelsHandler builds a handler that removes the given labels from
// the message identified by params.id and returns the resulting label set
// so the Mini App can update its row without a follow-up fetch.
func modifyLabelsHandler(deps GmailDeps, cache *listCache, removeLabels []string) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
		// Full requests the untruncated body (still bounded by
		// maxGmailFullBodyChars). The default keeps the 3000-char cap so the
		// list->detail flow stays light; the detail screen refetches with
		// full=true when the user asks for the rest.
		Full bool `json:"full,omitempty"`
	}
	return bind(func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		if strings.TrimSpace(p.ID) == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}

		client, errResp := gmailClientOrErr(deps, req.ID)
		if errResp != nil {
			return errResp
		}
		if err := client.ModifyLabels(ctx, p.ID, nil, removeLabels); err != nil {
			return MapGmailError(req.ID, "mail modify labels failed", err)
		}
		// Invalidate when this action changes inbox membership (archive
		// passes a cache; mark_read passes nil, a no-op). The same membership
		// change is mirrored to other clients via mail.changed.
		cache.invalidate()
		if cache != nil && deps.NotifyChanged != nil {
			deps.NotifyChanged(p.ID)
		}
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
	})
}

// --- helpers --------------------------------------------------------------

func mailSnippetForDisplay(snippet string) string {
	cleaned := mailbody.CleanForDisplay(snippet).Body
	if strings.TrimSpace(cleaned) == "" {
		cleaned = strings.TrimSpace(snippet)
	}
	cleaned = strings.TrimSpace(strings.Join(strings.Fields(cleaned), " "))
	const max = 360
	runes := []rune(cleaned)
	if len(runes) <= max {
		return cleaned
	}
	return string(runes[:max]) + "..."
}

func mailBodyWasCleaned(cleaned mailbody.CleanResult, raw string) bool {
	return len(cleaned.HiddenBlocks) > 0 && strings.TrimSpace(cleaned.Body) != strings.TrimSpace(raw)
}

func mailHiddenLineCount(blocks []mailbody.HiddenBlock) int {
	n := 0
	for _, block := range blocks {
		n += block.Lines
	}
	return n
}

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

// NormalizeDate parses Gmail's RFC 2822 Date header into ISO 8601 / RFC 3339.
// On parse failure it returns the raw input — the client renders whatever it
// gets, so a malformed header is better than an empty cell. Exported so the
// sibling analyzebind package can normalize dates identically.
func NormalizeDate(raw string) string {
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

func itoa64(n int64) string {
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

// MapGmailError classifies a Gmail client error into an RPC error response.
// Gmail returns HTTP-shaped errors via the client; we map well-known ones
// to NOT_FOUND / FORBIDDEN and lump the rest under UNAVAILABLE so the
// Mini App can choose between "retry" and "show the operator". Exported so
// the sibling analyzebind package maps Gmail errors identically.
func MapGmailError(reqID, msg string, err error) *protocol.ResponseFrame {
	if err == nil {
		return rpcerr.Unavailable(msg).Response(reqID)
	}
	text := err.Error()
	switch {
	case errors.Is(err, ErrGmailNotFound) || strings.Contains(text, "404") || strings.Contains(strings.ToLower(text), "not found"):
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

// ErrGmailNotFound is a sentinel callers may wrap to force the NOT_FOUND
// branch in MapGmailError; primarily exposed for tests and for the
// analyzebind package's workflow-state bookkeeping.
var ErrGmailNotFound = errors.New("gmail: message not found")
