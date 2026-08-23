package knowledge

import (
	"context"
	"fmt"
	"strings"
	"time"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
)

// wikiAdapter exposes the curated wiki Store as a knowledge backend.
type wikiAdapter struct {
	store *wiki.Store
}

// NewWikiAdapter wraps an initialized wiki.Store. Returns nil when store is
// nil so Router can ignore an unconfigured backend.
func NewWikiAdapter(store *wiki.Store) Adapter {
	if store == nil {
		return nil
	}
	return &wikiAdapter{store: store}
}

// Layer identifies the knowledge layer served by the adapter.
func (a *wikiAdapter) Layer() Layer { return LayerWiki }

func (a *wikiAdapter) Descriptor() SourceDescriptor {
	return SourceDescriptor{
		Layer:       LayerWiki,
		Name:        "wiki",
		Description: "curated wiki pages with lexical, semantic, graph, and rerank retrieval",
		Capabilities: []Capability{
			CapabilityLexical, CapabilitySemantic, CapabilityStructured,
			CapabilityGraph, CapabilityRerank, CapabilityLateContext,
		},
		Cost: 2,
		Sync: SyncContract{
			StableID: "workspace-relative page path", Cursor: "write-through index generation",
			ChangeDetection: "page content hash", DeletionDetection: "store delete plus audit tombstone",
			FreshnessTargetMillis: millis(180 * 24 * time.Hour), AuthorizationBoundary: "workspace wiki root",
		},
	}
}

// Recall searches the adapter for knowledge relevant to the query.
// Uses the full wiki pipeline (BM25/semantic/graph RRF + validity + gated
// model rerank). applyModelRerank only runs when top scores are ambiguous
// (or ForceRerank), so clear winners skip the ≤800ms cross-encoder.
func (a *wikiAdapter) Recall(ctx context.Context, query string, limit int) ([]Result, error) {
	report, err := a.store.SearchWithOptions(ctx, query, limit, wiki.QueryOptions{})
	if err != nil {
		return nil, err
	}
	hits := report.Results
	out := make([]Result, 0, len(hits))
	for _, h := range hits {
		meta := map[string]string{}
		originRefs := append([]string(nil), h.OriginRefs...)
		if h.FactID != "" {
			meta["factId"] = h.FactID
			meta["factKey"] = h.FactKey
			meta["factPlane"] = "current"
		}
		if h.SubjectID != "" {
			meta["subjectId"] = h.SubjectID
		}
		if h.Line > 0 {
			meta["startLine"] = fmt.Sprintf("%d", h.Line)
			meta["endLine"] = fmt.Sprintf("%d", h.EndLine)
		}
		if h.FactID == "" {
			if page, pageErr := a.store.ReadPage(h.Path); pageErr == nil && page != nil {
				originRefs = append([]string(nil), page.Meta.Sources...)
				if page.Meta.SubjectID != "" {
					meta["subjectId"] = page.Meta.SubjectID
				}
				// 인물 페이지의 emails는 메일 From과 비교할 식별자라 Recall Meta에
				// 싣는다. mail_archive가 Read(회상-사용 원장) 없이 불일치를 표시하게.
				if strings.HasPrefix(h.Path, "인물/") && len(page.Meta.Emails) > 0 {
					meta["emails"] = safeDocumentMetaValues(page.Meta.Emails)
				}
			}
		}
		out = append(out, Result{
			Ref:     Ref{Layer: LayerWiki, ID: h.Path},
			Snippet: h.Content,
			Context: h.ExpandedContent,
			Score:   h.Score,
			Provenance: Provenance{
				Locator: Locator{
					StartLine: h.Line, EndLine: h.EndLine,
					ContextStartLine: h.ExpandedLine, ContextEndLine: h.ExpandedEndLine,
				},
				Hierarchy:  append([]string(nil), h.Context...),
				OriginRefs: originRefs,
			},
			Meta: meta,
			Time: h.UpdatedAt,
		})
	}
	return out, nil
}

// FilterRecallResults applies one atomic fact-lifecycle snapshot to the full
// federated result set. The guard intentionally runs in Router after all
// adapters return, so an old files hit cannot bypass a wiki correction.
func (a *wikiAdapter) FilterRecallResults(results []Result) []Result {
	if a == nil || a.store == nil || len(results) == 0 {
		return results
	}
	snapshot := a.store.RecallFactSnapshot()
	activeIDs := make(map[string]struct{}, len(snapshot.Active))
	for _, claim := range snapshot.Active {
		activeIDs[claim.ID] = struct{}{}
	}
	texts := make([]string, len(results))
	for index, result := range results {
		texts[index] = result.Snippet + "\n" + result.Context
	}
	allowed := a.store.FactLifecycleTextsAllowed(texts, snapshot)
	filtered := results[:0]
	for index, result := range results {
		if result.Meta["factPlane"] == "current" {
			if _, current := activeIDs[result.Meta["factId"]]; current {
				filtered = append(filtered, result)
			}
			continue
		}
		if allowed[index] {
			filtered = append(filtered, result)
		}
	}
	return filtered
}

// Read returns the wiki document identified by id.
func (a *wikiAdapter) Read(_ context.Context, id string) (*Document, error) {
	factRef := strings.HasPrefix(strings.Trim(strings.ReplaceAll(strings.TrimSpace(id), "\\", "/"), "/"), "@facts/")
	page, err := a.store.ReadPage(id)
	if err != nil {
		return nil, fmt.Errorf("read wiki page %q: %w", id, err)
	}
	// 효용 접지: Router.Read's only caller is the chat knowledge tool, so this
	// is a model-driven page open — observed USE for the recall-utility ledger
	// (bridge-evidence adoption). No session at this layer (the domain cannot
	// read chat context keys); path-level attribution is what the scoring uses.
	// Best-effort derived telemetry by ledger contract; must never fail a read.
	if !factRef {
		_ = a.store.RecordRecallEvents([]wiki.RecallEvent{{Path: id, Event: wiki.RecallEventRead}})
	}
	meta := map[string]string{}
	if factRef {
		meta["factPlane"] = "current"
		meta["factId"] = safeDocumentMetaValue(page.Meta.ID)
		meta["subjectId"] = safeDocumentMetaValue(page.Meta.SubjectID)
	}
	if page.Meta.Category != "" {
		meta["category"] = safeDocumentMetaValue(page.Meta.Category)
	}
	if page.Meta.Type != "" {
		meta["type"] = safeDocumentMetaValue(page.Meta.Type)
	}
	if page.Meta.Summary != "" {
		meta["summary"] = safeDocumentMetaValue(page.Meta.Summary)
	}
	if len(page.Meta.Tags) > 0 {
		meta["tags"] = safeDocumentMetaValues(page.Meta.Tags)
	}
	if len(page.Meta.Related) > 0 {
		meta["related"] = safeDocumentMetaValues(page.Meta.Related)
	}
	if page.Meta.Updated != "" {
		meta["updated"] = safeDocumentMetaValue(page.Meta.Updated)
	}
	if len(page.Meta.Emails) > 0 {
		meta["emails"] = safeDocumentMetaValues(page.Meta.Emails)
	}
	if len(page.Meta.Sources) > 0 {
		meta["sources"] = safeDocumentMetaValues(page.Meta.Sources)
	}
	return &Document{
		Ref:     Ref{Layer: LayerWiki, ID: id},
		Title:   safeDocumentMetaValue(page.Meta.Title),
		Content: page.Body,
		Meta:    meta,
	}, nil
}

func safeDocumentMetaValues(values []string) string {
	safe := make([]string, 0, len(values))
	for _, value := range values {
		if value = safeDocumentMetaValue(value); value != "" {
			safe = append(safe, value)
		}
	}
	return strings.Join(safe, ", ")
}

func safeDocumentMetaValue(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	return strings.NewReplacer("<", "‹", ">", "›", "`", "'").Replace(value)
}

// Record persists a document through the adapter.
func (a *wikiAdapter) Record(_ context.Context, opts RecordOptions) (Ref, error) {
	if strings.TrimSpace(opts.Page) == "" {
		return Ref{}, fmt.Errorf("page is required for knowledge.record")
	}
	path := opts.Page

	title := strings.TrimSpace(opts.Title)
	if title == "" {
		// Derive from the last path segment.
		if i := strings.LastIndexByte(path, '/'); i >= 0 {
			title = path[i+1:]
		} else {
			title = path
		}
	}

	existing, _ := a.store.ReadPage(path)
	var page *wiki.Page
	now := time.Now().Format("2006-01-02")
	if existing != nil {
		page = existing
		page.Meta.Title = title
		if opts.Summary != "" {
			page.Meta.Summary = opts.Summary
		}
		if len(opts.Tags) > 0 {
			page.Meta.Tags = opts.Tags
		}
		if len(opts.Related) > 0 {
			page.Meta.Related = opts.Related
		}
		if opts.Importance > 0 {
			page.Meta.Importance = opts.Importance
		}
		if opts.Category != "" {
			page.Meta.Category = opts.Category
		}
		page.Meta.Updated = now
		if opts.Body != "" {
			page.Body = opts.Body
		}
	} else {
		page = wiki.NewPage(title, opts.Category, opts.Tags)
		page.Meta.Summary = opts.Summary
		page.Meta.Related = opts.Related
		if opts.Importance > 0 {
			page.Meta.Importance = opts.Importance
		}
		if opts.Body != "" {
			page.Body = opts.Body
		} else {
			page.Body = fmt.Sprintf("# %s\n\n## 요약\n\n\n## 핵심 사실\n\n\n## 변경 이력\n- %s: 페이지 생성\n",
				title, now)
		}
	}

	if err := a.store.WritePage(path, page); err != nil {
		return Ref{}, fmt.Errorf("write wiki page %q: %w", path, err)
	}
	for _, old := range opts.Supersedes {
		old = strings.TrimSpace(old)
		if old == "" {
			continue
		}
		if err := a.store.MarkSuperseded(old, path); err != nil {
			return Ref{}, fmt.Errorf("mark superseded %q -> %q: %w", old, path, err)
		}
	}
	return Ref{Layer: LayerWiki, ID: path}, nil
}

func (a *wikiAdapter) RecordFact(_ context.Context, opts FactRecordOptions) (FactMutationResult, error) {
	kind, err := parseFactKind(opts.Kind)
	if err != nil {
		return FactMutationResult{}, err
	}
	authority, err := parseFactAuthority(opts.Authority)
	if err != nil {
		return FactMutationResult{}, err
	}
	basisAt, err := parseFactBasisAt(opts.BasisAt)
	if err != nil {
		return FactMutationResult{}, err
	}
	if err := validateFactEvidence(authority, opts.Sources); err != nil {
		return FactMutationResult{}, err
	}
	if authority == wiki.FactAuthorityPrimaryDoc &&
		(kind == wiki.FactKindAmount || kind == wiki.FactKindDeadline || kind == wiki.FactKindContract) && basisAt.IsZero() {
		return FactMutationResult{}, fmt.Errorf("basis_at is required for primary_document %s facts", kind)
	}
	actor := strings.TrimSpace(opts.Actor)
	if actor == "" {
		actor = "knowledge-tool"
	}
	result, err := a.store.UpsertFact(wiki.FactInput{
		Subject: opts.Subject, Key: opts.Key, Value: opts.Value,
		Kind: kind, Authority: authority, Sources: opts.Sources,
		BasisAt: basisAt, Actor: actor, Reason: opts.Reason,
	})
	return adaptFactMutationResult(result), err
}

func (a *wikiAdapter) ForgetFact(_ context.Context, opts FactForgetOptions) (FactMutationResult, error) {
	authority, err := parseFactAuthority(opts.Authority)
	if err != nil {
		return FactMutationResult{}, err
	}
	if err := validateFactEvidence(authority, opts.Sources); err != nil {
		return FactMutationResult{}, err
	}
	actor := strings.TrimSpace(opts.Actor)
	if actor == "" {
		actor = "knowledge-tool"
	}
	result, err := a.store.TombstoneFact(wiki.FactTombstoneInput{
		Subject: opts.Subject, Key: opts.Key, Authority: authority,
		Sources: opts.Sources, Actor: actor, Reason: opts.Reason,
	})
	return adaptFactMutationResult(result), err
}

func (a *wikiAdapter) Facts(_ context.Context, subject, key string) ([]FactView, error) {
	if strings.TrimSpace(subject) == "" {
		subject = "self"
	}
	var claims []wiki.FactClaim
	if strings.TrimSpace(key) != "" {
		claims = a.store.FactHistory(subject, key)
	} else {
		claims = a.store.ActiveFacts(subject)
	}
	out := make([]FactView, 0, len(claims))
	for _, claim := range claims {
		out = append(out, FactView{
			ID: claim.ID, Subject: claim.Subject, Key: claim.Key, Value: claim.Value,
			Kind: string(claim.Kind), Authority: string(claim.Authority), Status: string(claim.Status),
			Sources: append([]string(nil), claim.Sources...), Actor: claim.Actor, Reason: claim.Reason,
			RecordedAtMs: claim.RecordedAtMs,
			BasisAtMs:    claim.BasisAtMs, Revision: uint64(claim.Revision),
		})
	}
	return out, nil
}

func parseFactKind(raw string) (wiki.FactKind, error) {
	switch wiki.FactKind(strings.ToLower(strings.TrimSpace(raw))) {
	case "", wiki.FactKindGeneric:
		return wiki.FactKindGeneric, nil
	case wiki.FactKindPreference:
		return wiki.FactKindPreference, nil
	case wiki.FactKindIdentity:
		return wiki.FactKindIdentity, nil
	case wiki.FactKindAmount:
		return wiki.FactKindAmount, nil
	case wiki.FactKindDeadline:
		return wiki.FactKindDeadline, nil
	case wiki.FactKindContract:
		return wiki.FactKindContract, nil
	case wiki.FactKindSystemState:
		return wiki.FactKindSystemState, nil
	default:
		return "", fmt.Errorf("unknown fact kind %q", raw)
	}
}

func parseFactAuthority(raw string) (wiki.FactAuthority, error) {
	switch wiki.FactAuthority(strings.ToLower(strings.TrimSpace(raw))) {
	case "", wiki.FactAuthorityAgent:
		return wiki.FactAuthorityAgent, nil
	case wiki.FactAuthorityDirectUser:
		// Only the trusted direct-message induction path writes this authority
		// straight to the fact Store; the knowledge adapter is model-callable.
		return "", fmt.Errorf("fact authority %q is reserved for trusted direct-message induction", raw)
	case wiki.FactAuthorityPrimaryDoc:
		return wiki.FactAuthorityPrimaryDoc, nil
	case wiki.FactAuthorityRuntime:
		return wiki.FactAuthorityRuntime, nil
	case wiki.FactAuthorityInference:
		return wiki.FactAuthorityInference, nil
	default:
		return "", fmt.Errorf("unknown fact authority %q", raw)
	}
}

func validateFactEvidence(authority wiki.FactAuthority, sources []string) error {
	if authority != wiki.FactAuthorityPrimaryDoc && authority != wiki.FactAuthorityRuntime {
		return nil
	}
	for _, source := range sources {
		if strings.TrimSpace(source) != "" {
			return nil
		}
	}
	return fmt.Errorf("source_refs is required for authority=%s", authority)
}

func parseFactBasisAt(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("basis_at must be RFC3339 or YYYY-MM-DD")
}

func adaptFactMutationResult(result wiki.FactWriteResult) FactMutationResult {
	return FactMutationResult{
		Revision: uint64(result.Revision), ClaimID: result.ClaimID,
		Status: string(result.Status), Resolution: result.Resolution,
		Committed: result.Committed, ProjectionError: result.ProjectionError,
	}
}
