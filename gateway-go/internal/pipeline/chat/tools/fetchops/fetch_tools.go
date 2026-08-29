// fetch_tools.go — Meta-tool that activates deferred tools mid-run.
//
// Deferred tools have their name+description visible in the system prompt but
// full JSON schemas are not sent in the initial Tools array. When the LLM
// needs a deferred tool, it calls fetch_tools to:
//  1. Get the full schema description (returned as text).
//  2. Signal DeferredActivation so the executor injects schemas on the next turn.
package fetchops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/embedindex"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/toolpreset"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/toolmeta"
)

// FetchToolsRegistry is the subset of ToolRegistry needed by fetch_tools.
type FetchToolsRegistry interface {
	DeferredToolDef(name string) (toolport.ToolDef, bool)
	DeferredSummaries() []toolport.DeferredToolSummary
	// HasTool reports whether the name is registered at all, deferred or not.
	// Without it a fetch for an eager tool is indistinguishable from a fetch
	// for a tool that does not exist — see resolveFetchTool.
	HasTool(name string) bool
}

type fetchToolsCatalogRevisioner interface {
	DeferredCatalogRevision() uint64
}

// FetchToolReranker is an optional cross-encoder used only after lexical/dense
// retrieval has admitted a bounded candidate set. Every failure is fail-open.
type FetchToolReranker interface {
	Rerank(ctx context.Context, query string, documents []string) ([]float64, error)
}

// ToolFetchTools returns a tool that activates deferred tools and returns their
// schemas. An optional embedder adds semantic ranking inside this explicit
// meta-tool; it never changes the base tool array or prompt-cache boundary.
func ToolFetchTools(registry FetchToolsRegistry, embedders ...embedindex.Embedder) toolport.ToolFunc {
	var embedder embedindex.Embedder
	if len(embedders) > 0 {
		embedder = embedders[0]
	}
	return ToolFetchToolsWithReranker(registry, embedder, nil)
}

// ToolFetchToolsWithReranker adds a bounded cross-encoder pass to query-based
// discovery. Explicit tool names bypass both semantic search and reranking.
func ToolFetchToolsWithReranker(registry FetchToolsRegistry, embedder embedindex.Embedder, reranker FetchToolReranker) toolport.ToolFunc {
	semantic := newFetchToolSemanticSearch(embedder)
	catalog := newFetchToolSearchCatalog(registry)
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		return runFetchTools(ctx, input, registry, catalog, semantic, reranker)
	}
}

func runFetchTools(ctx context.Context, input json.RawMessage, registry FetchToolsRegistry, catalog *fetchToolSearchCatalog, semantic *fetchToolSemanticSearch, reranker FetchToolReranker) (string, error) {
	if err := validateFetchToolsContext(ctx); err != nil {
		return "", err
	}
	request, err := parseFetchToolsRequest(input)
	if err != nil {
		return "", err
	}

	access := fetchToolAccessFromContext(ctx)
	names := selectFetchToolNames(ctx, request, catalog, access, semantic, reranker)
	if request.selectsByQuery() && len(names) == 0 {
		return fmt.Sprintf("No deferred tools match query %q.", request.Query), nil
	}

	activation := toolport.DeferredActivationFromContext(ctx)
	report, err := buildFetchToolsReport(ctx, names, registry, access, activation, func(name string) []string {
		return suggestFetchToolNames(ctx, name, catalog, access, semantic, reranker)
	})
	if err != nil {
		return "", err
	}
	return report.finalize(ctx, activation)
}

type fetchToolsRequest struct {
	Names []string `json:"names"`
	Query string   `json:"query"`
}

func validateFetchToolsContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("fetch_tools requires a context")
	}
	return ctx.Err()
}

func parseFetchToolsRequest(input json.RawMessage) (fetchToolsRequest, error) {
	var request fetchToolsRequest
	if err := jsonutil.UnmarshalInto("fetch_tools params", input, &request); err != nil {
		return fetchToolsRequest{}, err
	}
	request.Query = strings.TrimSpace(request.Query)
	if len(request.Names) == 0 && request.Query == "" {
		return fetchToolsRequest{}, fmt.Errorf("names or query is required")
	}
	return request, nil
}

func (r fetchToolsRequest) selectsByQuery() bool {
	return r.Query != "" && len(r.Names) == 0
}

// fetchToolAccess keeps preset filtering identical across catalog search and
// explicit-name activation. A nil allow-list means an unrestricted run.
type fetchToolAccess struct {
	allowed map[string]struct{}
}

func fetchToolAccessFromContext(ctx context.Context) fetchToolAccess {
	preset := toolpreset.Preset(toolport.ToolPresetFromContext(ctx))
	return fetchToolAccess{allowed: toolpreset.AllowedTools(preset)}
}

func (a fetchToolAccess) allows(name string) bool {
	if a.allowed == nil {
		return true
	}
	_, ok := a.allowed[name]
	return ok
}

// selectFetchToolNames gives explicit names precedence. Query selection ranks
// whole-token matches, then appends substring-only matches as a recall floor.
func selectFetchToolNames(ctx context.Context, request fetchToolsRequest, catalog *fetchToolSearchCatalog, access fetchToolAccess, semantic *fetchToolSemanticSearch, reranker FetchToolReranker) []string {
	if !request.selectsByQuery() {
		return request.Names
	}
	docs := filterFetchToolDocs(catalog.docs(), access)
	poolLimit := searchResultLimit
	if reranker != nil {
		poolLimit = fetchToolRerankCandidateLimit
	}
	lexical := appendSubstringMatches(bm25RankLimit(request.Query, docs, poolLimit), request.Query, docs)
	lexical = clipFetchToolNames(lexical, poolLimit)
	candidates := lexical
	if semantic != nil {
		if dense, ok := semantic.rank(ctx, request.Query, docs); ok {
			candidates = fuseFetchToolRanksLimitCalibrated(lexical, dense, poolLimit, semantic.admissionFloor())
		}
	}
	if reranked, ok := rerankFetchToolNames(ctx, request.Query, candidates, docs, reranker); ok {
		candidates = reranked
	}
	return clipFetchToolNames(candidates, searchResultLimit)
}

func clipFetchToolNames(names []string, limit int) []string {
	if limit > 0 && len(names) > limit {
		return names[:limit]
	}
	return names
}

type fetchToolSearchCatalog struct {
	registry FetchToolsRegistry

	mu          sync.RWMutex
	ready       bool
	revision    uint64
	fingerprint string
	entries     []searchDoc
}

func newFetchToolSearchCatalog(registry FetchToolsRegistry) *fetchToolSearchCatalog {
	return &fetchToolSearchCatalog{registry: registry}
}

func (c *fetchToolSearchCatalog) docs() []searchDoc {
	if c == nil || c.registry == nil {
		return nil
	}
	if revision, ok := fetchToolCatalogRevision(c.registry); ok {
		if docs, ok := c.docsForRevision(revision); ok {
			return docs
		}
		return c.rebuildVersioned(revision)
	}

	summaries := c.registry.DeferredSummaries()
	fingerprint := fetchToolSummariesFingerprint(summaries)
	if docs, ok := c.docsForFingerprint(fingerprint); ok {
		return docs
	}
	return c.rebuildUnversioned(fingerprint, summaries)
}

func (c *fetchToolSearchCatalog) docsForRevision(revision uint64) ([]searchDoc, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.ready || c.revision != revision {
		return nil, false
	}
	return c.entries, true
}

func (c *fetchToolSearchCatalog) docsForFingerprint(fingerprint string) ([]searchDoc, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.ready || c.fingerprint != fingerprint {
		return nil, false
	}
	return c.entries, true
}

func (c *fetchToolSearchCatalog) rebuildVersioned(revision uint64) []searchDoc {
	summaries := c.registry.DeferredSummaries()
	docs := c.buildDocs(summaries)
	if latest, ok := fetchToolCatalogRevision(c.registry); ok && latest != revision {
		revision = latest
		summaries = c.registry.DeferredSummaries()
		docs = c.buildDocs(summaries)
	}

	c.mu.Lock()
	c.ready = true
	c.revision = revision
	c.fingerprint = ""
	c.entries = docs
	c.mu.Unlock()
	return docs
}

func (c *fetchToolSearchCatalog) rebuildUnversioned(fingerprint string, summaries []toolport.DeferredToolSummary) []searchDoc {
	docs := c.buildDocs(summaries)
	c.mu.Lock()
	c.ready = true
	c.revision = 0
	c.fingerprint = fingerprint
	c.entries = docs
	c.mu.Unlock()
	return docs
}

func (c *fetchToolSearchCatalog) buildDocs(summaries []toolport.DeferredToolSummary) []searchDoc {
	docs := make([]searchDoc, 0, len(summaries))
	for _, summary := range summaries {
		docs = append(docs, deferredToolSearchDoc(c.registry, summary))
	}
	return docs
}

func fetchToolCatalogRevision(registry FetchToolsRegistry) (uint64, bool) {
	revisioner, ok := registry.(fetchToolsCatalogRevisioner)
	if !ok {
		return 0, false
	}
	return revisioner.DeferredCatalogRevision(), true
}

func fetchToolSummariesFingerprint(summaries []toolport.DeferredToolSummary) string {
	var b strings.Builder
	for _, summary := range summaries {
		b.WriteString(summary.Name)
		b.WriteByte('\x00')
		b.WriteString(summary.Description)
		b.WriteByte('\n')
	}
	return b.String()
}

func filterFetchToolDocs(docs []searchDoc, access fetchToolAccess) []searchDoc {
	if access.allowed == nil {
		return docs
	}
	filtered := make([]searchDoc, 0, len(docs))
	for _, doc := range docs {
		if access.allows(doc.name) {
			filtered = append(filtered, doc)
		}
	}
	return filtered
}

func deferredToolSearchDoc(registry FetchToolsRegistry, summary toolport.DeferredToolSummary) searchDoc {
	tokens := append(tokenize(summary.Name), tokenize(summary.Description)...)
	parameterNames := []string(nil)
	if def, ok := registry.DeferredToolDef(summary.Name); ok {
		parameterNames = extractParamNames(def.InputSchema)
		for _, parameterName := range parameterNames {
			tokens = append(tokens, tokenize(parameterName)...)
		}
	}
	semanticText := summary.Name + "\n" + summary.Description
	if len(parameterNames) > 0 {
		semanticText += "\nParameters: " + strings.Join(parameterNames, ", ")
	}
	return searchDoc{
		name:     summary.Name,
		tokens:   tokens,
		fallback: strings.ToLower(summary.Name + " " + summary.Description),
		semantic: semanticText,
	}
}

func appendSubstringMatches(names []string, query string, docs []searchDoc) []string {
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		seen[name] = true
	}
	query = strings.ToLower(query)
	for _, doc := range docs {
		if !seen[doc.name] && strings.Contains(doc.fallback, query) {
			names = append(names, doc.name)
			seen[doc.name] = true
		}
	}
	return names
}

// fetchToolSuggestionLimit bounds the "closest available" hint on an unknown
// name. Three is enough to redirect and short enough to stay one line.
const fetchToolSuggestionLimit = 3

type fetchToolDecision uint8

const (
	fetchToolUnavailable fetchToolDecision = iota
	fetchToolNotFound
	fetchToolAlreadyOnWire
	fetchToolAlreadyActive
	fetchToolActivate
)

type fetchToolResolution struct {
	name     string
	def      toolport.ToolDef
	decision fetchToolDecision
}

func resolveFetchTool(
	name string,
	registry FetchToolsRegistry,
	access fetchToolAccess,
	activation *toolport.DeferredActivation,
) fetchToolResolution {
	if !access.allows(name) {
		return fetchToolResolution{name: name, decision: fetchToolUnavailable}
	}
	def, ok := registry.DeferredToolDef(name)
	if !ok {
		// An eager tool is already in the tools array. Reporting it as "not
		// found" reads as "no such tool" and talks the model out of a surface it
		// already holds — measured across the transcript history: mail_archive
		// was fetched 30 times, watch 15, write 3, edit 2, all eager throughout.
		if registry.HasTool(name) {
			return fetchToolResolution{name: name, decision: fetchToolAlreadyOnWire}
		}
		return fetchToolResolution{name: name, decision: fetchToolNotFound}
	}
	// The active snapshot only advances between turns. A same-turn duplicate
	// deliberately returns its schema again, while a prior-turn activation does not.
	if activation != nil && activation.IsActive(name) {
		return fetchToolResolution{name: name, decision: fetchToolAlreadyActive}
	}
	return fetchToolResolution{name: name, def: def, decision: fetchToolActivate}
}

type fetchToolsReport struct {
	output        strings.Builder
	activated     []string
	alreadyActive []string
	// suggest offers the closest catalog entries for a name that matches no
	// registered tool. The model invents tool names it wishes existed
	// (autoresearch, analyze in the transcript history); a dead end there costs
	// a turn, a near match costs none. Injected so the report stays free of the
	// search stack; nil simply omits the hint.
	suggest func(string) []string
	// inlineSchemas writes each activated tool's full JSON schema into the tool
	// RESULT text. That is only useful when the tools array cannot be changed
	// mid-turn (no DeferredActivation in context) — when activation works, the
	// same schema arrives in the tools array and the result copy is dead
	// weight that also persists in the transcript for every later turn.
	// Measured 2026-08-25 in puppet mode: one query-based fetch activated 5
	// tools and shipped 22.4KB of schema twice.
	inlineSchemas bool
}

func buildFetchToolsReport(
	ctx context.Context,
	names []string,
	registry FetchToolsRegistry,
	access fetchToolAccess,
	activation *toolport.DeferredActivation,
	suggest func(string) []string,
) (*fetchToolsReport, error) {
	report := &fetchToolsReport{inlineSchemas: activation == nil, suggest: suggest}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		report.add(resolveFetchTool(name, registry, access, activation))
	}
	return report, nil
}

func (r *fetchToolsReport) add(resolution fetchToolResolution) {
	switch resolution.decision {
	case fetchToolUnavailable:
		fmt.Fprintf(&r.output, "- %s: not available under the current tool preset\n", resolution.name)
	case fetchToolNotFound:
		fmt.Fprintf(&r.output, "- %s: not found or not a deferred tool", resolution.name)
		if r.suggest != nil {
			if hints := r.suggest(resolution.name); len(hints) > 0 {
				fmt.Fprintf(&r.output, " — closest available: %s", strings.Join(hints, ", "))
			}
		}
		r.output.WriteString("\n")
	case fetchToolAlreadyOnWire:
		fmt.Fprintf(&r.output, "- %s: already in your tools array — call it directly, no fetch needed\n", resolution.name)
	case fetchToolAlreadyActive:
		r.alreadyActive = append(r.alreadyActive, resolution.name)
	case fetchToolActivate:
		r.activated = append(r.activated, resolution.name)
		r.writeSchema(resolution.def)
	}
}

func (r *fetchToolsReport) writeSchema(def toolport.ToolDef) {
	fmt.Fprintf(&r.output, "## %s\n%s\n", def.Name, def.Description)
	if def.InputSchema != nil && r.inlineSchemas {
		schemaJSON, _ := json.MarshalIndent(def.InputSchema, "", "  ")
		fmt.Fprintf(&r.output, "```json\n%s\n```\n", schemaJSON)
	}
	r.output.WriteString("\n")
}

// suggestFetchToolNames runs the ordinary catalog search over an unknown name,
// dropping the name itself so a partial match cannot suggest what was asked for.
func suggestFetchToolNames(ctx context.Context, name string, catalog *fetchToolSearchCatalog, access fetchToolAccess, semantic *fetchToolSemanticSearch, reranker FetchToolReranker) []string {
	hits := selectFetchToolNames(ctx, fetchToolsRequest{Query: name}, catalog, access, semantic, reranker)
	out := make([]string, 0, fetchToolSuggestionLimit)
	for _, hit := range hits {
		if hit == name {
			continue
		}
		out = append(out, hit)
		if len(out) == fetchToolSuggestionLimit {
			break
		}
	}
	return out
}

func (r *fetchToolsReport) finalize(ctx context.Context, activation *toolport.DeferredActivation) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	r.publishActivation(ctx, activation)
	r.appendActivationNotices()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return r.output.String(), nil
}

func (r *fetchToolsReport) publishActivation(ctx context.Context, activation *toolport.DeferredActivation) {
	if activation != nil && len(r.activated) > 0 {
		activation.Activate(r.activated)
	}
	// Structured replay evidence is the code-only half of the text notices.
	// Already-active names re-anchor state after older evidence is summarized.
	if names := r.replayEvidence(); len(names) > 0 {
		toolmeta.Set(ctx, "activatedTools", names)
	}
}

func (r *fetchToolsReport) replayEvidence() []string {
	names := append([]string{}, r.activated...)
	return append(names, r.alreadyActive...)
}

func (r *fetchToolsReport) appendActivationNotices() {
	if len(r.alreadyActive) > 0 {
		r.output.WriteString(toolport.FormatAlreadyActiveNotice(r.alreadyActive))
		r.output.WriteString("\n")
	}
	if len(r.activated) > 0 {
		r.output.WriteString(toolport.FormatFetchActivationNotice(r.activated))
	}
}
