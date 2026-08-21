package recallops

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/knowledge"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// ToolKnowledge wraps the knowledge.Router as a single agent-facing tool over
// the wiki knowledge base. Three ops:
//
//	recall  — federated search across all read backends, merged by score
//	read    — fetch one document by its layered ref ("w:...")
//	record  — write a wiki page (the only writable backend)
func ToolKnowledge(router *knowledge.Router) toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Op string `json:"op"`

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
		}
		if err := jsonutil.UnmarshalInto("knowledge params", input, &p); err != nil {
			return "", err
		}
		if router == nil {
			return "", fmt.Errorf("knowledge router is not configured")
		}

		switch p.Op {
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
		default:
			return "", fmt.Errorf("unknown knowledge op %q (expected recall|read|record)", p.Op)
		}
	}
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
	ref, err := router.Record(ctx, opts)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("✏️ 기록됨: `%s`", ref.String()), nil
}
