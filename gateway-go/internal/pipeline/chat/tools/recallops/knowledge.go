package recallops

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/knowledge"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

const (
	knowledgeFactsDefaultLimit = 10
	knowledgeFactsMaxLimit     = 50
)

// ToolKnowledge wraps the knowledge.Router as a single agent-facing tool over
// the wiki knowledge base. Six ops:
//
//	recall      — federated search across all read backends, merged by score
//	read        — fetch one document by its layered ref ("w:...")
//	record      — write a wiki page (the only writable backend)
//	assert_fact — append a typed claim and resolve its current-state status
//	forget_fact — append a tombstone while preserving history
//	facts       — read active facts or one key's history
//
// The two fact mutations never carry a caller-declared authority. The model
// supplies evidence (subject/key/value/source_refs); the server decides how much
// that evidence is worth, and knowledge.wikiAdapter caps it at agent_confirmed.
// direct_user stays exclusive to authenticated direct-message induction, and
// primary_document/runtime_observation stay exclusive to ingestion paths that
// authenticate their own source. See docs/adr/0005-fact-write-authority.md.

func ToolKnowledge(router *knowledge.Router) toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Op     string `json:"op"`
			Action string `json:"action"`

			// recall
			Query   string   `json:"query"`
			Limit   int      `json:"limit"`
			Sources []string `json:"sources"`
			Scopes  []string `json:"scopes"`

			// read
			Ref string `json:"ref"`

			// record
			Page       string   `json:"page"`
			Title      string   `json:"title"`
			Category   string   `json:"category"`
			Body       string   `json:"body"`
			Summary    string   `json:"summary"`
			Tags       []string `json:"tags"`
			Related    []string `json:"related"`
			Supersedes []string `json:"supersedes"`
			Importance float64  `json:"importance"`

			// assert_fact / forget_fact / facts
			Subject    string   `json:"subject"`
			FactKey    string   `json:"fact_key"`
			Value      string   `json:"value"`
			FactKind   string   `json:"fact_kind"`
			SourceRefs []string `json:"source_refs"`
			Reason     string   `json:"reason"`
		}
		if err := jsonutil.UnmarshalInto("knowledge params", input, &p); err != nil {
			return "", err
		}
		if router == nil {
			return "", fmt.Errorf("knowledge router is not configured")
		}
		op := normalizeKnowledgeOp(p.Op)
		if op == "" {
			op = normalizeKnowledgeOp(p.Action)
		}

		switch op {
		case "recall":
			layers := make([]knowledge.Layer, 0, len(p.Sources))
			for _, source := range p.Sources {
				layer, ok := knowledge.ParseLayerName(source)
				if !ok {
					return "", fmt.Errorf("unknown knowledge source %q (expected wiki|files)", source)
				}
				layers = append(layers, layer)
			}
			return knowledgeRecall(ctx, router, p.Query, p.Limit, knowledge.RecallOptions{Layers: layers, Scopes: p.Scopes})
		case "read":
			return knowledgeRead(ctx, router, p.Ref)
		case "record":
			return knowledgeRecord(ctx, router, knowledge.RecordOptions{
				Page:       p.Page,
				Title:      p.Title,
				Category:   p.Category,
				Body:       p.Body,
				Summary:    p.Summary,
				Tags:       p.Tags,
				Related:    p.Related,
				Supersedes: p.Supersedes,
				Importance: p.Importance,
			})
		case "assert_fact":
			return knowledgeAssertFact(ctx, router, knowledge.FactRecordOptions{
				Subject: p.Subject, Key: p.FactKey, Value: p.Value,
				Kind: p.FactKind, Sources: p.SourceRefs, Reason: p.Reason,
			})
		case "forget_fact":
			return knowledgeForgetFact(ctx, router, knowledge.FactForgetOptions{
				Subject: p.Subject, Key: p.FactKey,
				Sources: p.SourceRefs, Reason: p.Reason,
			})
		case "facts":
			return knowledgeFacts(ctx, router, p.Subject, p.FactKey, p.Limit)
		default:
			return "", fmt.Errorf("unknown knowledge op %q (expected recall|read|record|assert_fact|forget_fact|facts)", op)
		}
	}
}

func knowledgeFacts(ctx context.Context, router *knowledge.Router, subject, key string, limit int) (string, error) {
	facts, err := router.Facts(ctx, subject, key)
	if err != nil {
		return "", err
	}
	if len(facts) == 0 {
		return "현행/이력 사실 없음.", nil
	}
	if limit <= 0 {
		limit = knowledgeFactsDefaultLimit
	} else if limit > knowledgeFactsMaxLimit {
		limit = knowledgeFactsMaxLimit
	}
	if len(facts) > limit {
		if strings.TrimSpace(key) != "" {
			// One-key history is revision-ordered oldest-first. Keep the latest
			// bounded tail while the canonical ledger retains every revision.
			facts = facts[len(facts)-limit:]
		} else {
			// Active facts are newest-first, so keep the bounded head.
			facts = facts[:limit]
		}
	}
	raw, err := json.MarshalIndent(facts, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func knowledgeRecall(ctx context.Context, router *knowledge.Router, query string, limit int, options knowledge.RecallOptions) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("query is required for knowledge(op=\"recall\")")
	}
	if limit <= 0 {
		limit = 10
	}

	started := time.Now()
	packet := router.RecallPacket(ctx, query, limit, options)
	hits, notes := packet.Results, packet.Notes
	planSources := make([]string, 0, len(packet.Plan.Sources))
	for _, source := range packet.Plan.Sources {
		planSources = append(planSources, source.Source.Name)
	}
	slog.Info(
		"knowledge tool recall",
		"query_len", len(query),
		"limit", limit,
		"hit_count", len(hits),
		"layers", planSources,
		"scopes", packet.Plan.Scopes,
		"notes", len(notes),
		"total_ms", time.Since(started).Milliseconds(),
	)
	if len(hits) == 0 {
		msg := fmt.Sprintf("검색 결과 없음: %q (sources=%v)", query, planSources)
		if len(notes) > 0 {
			msg += "\n참고: " + strings.Join(notes, "; ")
		}
		return msg, nil
	}

	var sb strings.Builder
	sb.WriteString(toolport.RecallHeader(query, len(hits), fmt.Sprintf("sources=%v", planSources)))
	for _, n := range notes {
		sb.WriteString("참고: ")
		sb.WriteString(n)
		sb.WriteByte('\n')
	}
	for i, h := range hits {
		metaParts := make([]string, 0, 2)
		if startLine := h.Provenance.Locator.StartLine; startLine > 0 {
			lineRef := fmt.Sprintf("L%d", startLine)
			if endLine := h.Provenance.Locator.EndLine; endLine > startLine {
				lineRef += fmt.Sprintf("-L%d", endLine)
			}
			metaParts = append(metaParts, lineRef)
		}
		if h.Time > 0 {
			metaParts = append(metaParts, time.UnixMilli(h.Time).Format("2006-01-02"))
		}
		sb.WriteString(toolport.RecallRow(i+1, h.Ref.String(), strings.Join(metaParts, " · "), h.Snippet))
		if contextText := strings.TrimSpace(h.Context); contextText != "" {
			lineRef := ""
			if start := h.Provenance.Locator.ContextStartLine; start > 0 {
				lineRef = fmt.Sprintf(" L%d", start)
				if end := h.Provenance.Locator.ContextEndLine; end > start {
					lineRef += fmt.Sprintf("-L%d", end)
				}
			}
			fmt.Fprintf(&sb, "   ↳ 인접 문맥%s: %s\n\n", lineRef, truncateKnowledgeContext(contextText, 800))
		}
	}
	sb.WriteString("자세한 내용은 `knowledge(op=\"read\", ref=\"...\")` 로 ref 지정.")
	return sb.String(), nil
}

func truncateKnowledgeContext(text string, maxRunes int) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "..."
}

func knowledgeRead(ctx context.Context, router *knowledge.Router, refStr string) (string, error) {
	refStr = strings.TrimSpace(refStr)
	if refStr == "" {
		return "", fmt.Errorf("ref is required for knowledge(op=\"read\")")
	}
	// This tool is the model-driven read surface: attribute the read to the
	// chat session so the wiki utility ledger can join it against the inject
	// exposure that surfaced the page (unattributed reads are not recorded).
	ctx = knowledge.WithReadAttribution(ctx, toolport.SessionKeyFromContext(ctx))
	// Guidance strings instead of raw Go errors: a bad ref or a missing doc is
	// an expected state the model should recover from (search first), not a
	// hard failure to surface to the user.
	ref, err := knowledge.ParseRef(refStr)
	if err != nil {
		return fmt.Sprintf("ref 형식이 잘못됐습니다 (%s). `knowledge(op=\"recall\", query=...)`로 먼저 검색해 결과의 ref를 그대로 사용하세요.", err), nil
	}
	doc, err := router.Read(ctx, ref)
	if err != nil {
		return fmt.Sprintf("문서를 열 수 없습니다: `%s` (%s). ref가 오래됐을 수 있으니 `knowledge(op=\"recall\", query=...)`로 다시 검색하세요.", ref.String(), err), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## 📄 `%s`\n\n", ref.String())
	if doc.Title != "" {
		fmt.Fprintf(&sb, "**제목:** %s\n", doc.Title)
	}
	for k, v := range doc.Meta {
		fmt.Fprintf(&sb, "**%s:** %s\n", k, v)
	}
	if doc.Time > 0 {
		fmt.Fprintf(&sb, "**시간:** %s\n", time.UnixMilli(doc.Time).Format("2006-01-02 15:04"))
	}
	sb.WriteString("\n---\n\n")
	sb.WriteString(doc.Content)
	return sb.String(), nil
}

func knowledgeRecord(ctx context.Context, router *knowledge.Router, opts knowledge.RecordOptions) (string, error) {
	if strings.TrimSpace(opts.Page) == "" {
		return "", fmt.Errorf("page is required for knowledge(op=\"record\")")
	}
	// The record path replaces an existing page's body just like the wiki
	// tool's write action does (knowledge/adapter_wiki.go: page.Body =
	// opts.Body). Read what is there first so the result can name what the
	// write destroyed — the wiki tool has said so since #4797, and a page
	// overwritten through this door was still silent.
	previous := existingWikiBody(ctx, router, opts.Page)
	ref, err := router.Record(ctx, opts)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("✏️ 기록됨: `%s`", ref.String()) +
		wiki.ReplacedBodyNotice(previous, opts.Body), nil
}

// existingWikiBody returns the page body a record is about to overwrite, or ""
// when the page is new or unreadable. Best-effort: a read failure must not stop
// the write the operator asked for.
func existingWikiBody(ctx context.Context, router *knowledge.Router, page string) string {
	page = strings.TrimSpace(page)
	if router == nil || page == "" {
		return ""
	}
	doc, err := router.Read(ctx, knowledge.Ref{Layer: knowledge.LayerWiki, ID: page})
	if err != nil || doc == nil {
		return ""
	}
	return doc.Content
}

// knowledgeAssertFact records a model-supplied claim as evidence, never as an
// authority. Source refs are mandatory: an agent claim that cannot name where it
// came from is exactly the kind of unverifiable assertion the fact plane exists
// to keep out of answers.
func knowledgeAssertFact(ctx context.Context, router *knowledge.Router, opts knowledge.FactRecordOptions) (string, error) {
	if strings.TrimSpace(opts.Key) == "" || strings.TrimSpace(opts.Value) == "" {
		return "", fmt.Errorf("fact_key and value are required for knowledge(op=\"assert_fact\")")
	}
	if err := requireFactSourceRefs(opts.Sources); err != nil {
		return "", err
	}
	result, err := router.RecordFact(ctx, opts)
	if err != nil {
		return "", err
	}
	if !result.Committed {
		return "", fmt.Errorf("fact assertion was not committed (resolution=%s)", result.Resolution)
	}
	prefix := "사실 mutation 커밋됨"
	switch result.Status {
	case "current":
		prefix = "현행 사실 반영됨"
	case "conflicted":
		prefix = "상충하는 사실로 기록됨"
	case "superseded":
		prefix = "사실 주장은 이력에 기록됐지만 현행 사실은 변경되지 않음"
	}
	message := fmt.Sprintf("%s: revision=%d status=%s resolution=%s claim=%s", prefix,
		result.Revision, result.Status, result.Resolution, result.ClaimID)
	// Tell the model whether its citation actually holds up. This changes no
	// authority (the server fixes that), but a ref that does not open — or opens
	// on a page that never states this value — is a bad citation the model should
	// fix rather than keep repeating.
	switch {
	case result.VerifiedSource != "":
		message += fmt.Sprintf("\n근거 확인됨 — `%s`가 이 값을 담고 있다(권위는 %s 그대로).",
			result.VerifiedSource, result.Authority)
	case result.SourceNote != "":
		message += fmt.Sprintf("\n근거 미확인(%s) — 권위 %s로 기록됨. 인용한 ref가 열리는지, 그 페이지가 이 값을 담고 있는지 확인하라.",
			result.SourceNote, result.Authority)
	}
	if result.ProjectionError != "" {
		message += "\n⚠ 정본은 커밋됐지만 호환 projection 갱신이 지연됨: " + result.ProjectionError
	}
	return message, nil
}

// knowledgeForgetFact retires an identity the agent itself established. The
// store refuses to retire a stronger claim (a direct-user fact in particular),
// so a rejected delete comes back as a recorded but ignored lifecycle event.
func knowledgeForgetFact(ctx context.Context, router *knowledge.Router, opts knowledge.FactForgetOptions) (string, error) {
	if strings.TrimSpace(opts.Key) == "" {
		return "", fmt.Errorf("fact_key is required for knowledge(op=\"forget_fact\")")
	}
	result, err := router.ForgetFact(ctx, opts)
	if err != nil {
		return "", err
	}
	if !result.Committed {
		return "", fmt.Errorf("fact tombstone was not committed (resolution=%s)", result.Resolution)
	}
	prefix := "사실을 소프트 삭제함"
	switch {
	case result.Resolution == "ignored_lower_authority":
		prefix = "삭제 요청은 이력에 남았지만 더 높은 권위의 현행 사실은 유지됨 (사용자 직접 발화만 철회 가능)"
	case result.Resolution == "already_absent":
		prefix = "이미 현행 사실이 없어 변화 없음"
	}
	message := fmt.Sprintf("%s: revision=%d resolution=%s claim=%s", prefix,
		result.Revision, result.Resolution, result.ClaimID)
	if result.ProjectionError != "" {
		message += "\n⚠ 정본은 커밋됐지만 호환 projection 갱신이 지연됨: " + result.ProjectionError
	}
	return message, nil
}

func requireFactSourceRefs(sources []string) error {
	for _, source := range sources {
		if strings.TrimSpace(source) != "" {
			return nil
		}
	}
	return fmt.Errorf("source_refs is required for knowledge(op=\"assert_fact\"): name the ref/document this claim came from")
}
