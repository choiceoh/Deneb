package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/knowledge"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// ToolKnowledge wraps the knowledge.Router as a single agent-facing tool that
// unifies the wiki (curated, writable) and hindsight (auto-retained cross-
// session) memory backends. Three ops:
//
//	recall  — federated search across all read backends, merged by score
//	read    — fetch one document by its layered ref ("w:..." or "h:...")
//	record  — write a wiki page (the only writable backend)
func ToolKnowledge(router *knowledge.Router) toolctx.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Op string `json:"op"`

			// recall
			Query string `json:"query"`
			Limit int    `json:"limit"`

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
			return knowledgeRecall(ctx, router, p.Query, p.Limit)
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
				Importance: p.Importance,
			})
		default:
			return "", fmt.Errorf("unknown knowledge op %q (expected recall|read|record)", p.Op)
		}
	}
}

func knowledgeRecall(ctx context.Context, router *knowledge.Router, query string, limit int) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("query is required for knowledge(op=\"recall\")")
	}
	if limit <= 0 {
		limit = 10
	}

	hits := router.Recall(ctx, query, limit)
	if len(hits) == 0 {
		return fmt.Sprintf("검색 결과 없음: %q (위키·hindsight 모두 빈손)", query), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## 🔍 %q (%d건, layers=%v)\n\n", query, len(hits), router.Layers())
	for i, h := range hits {
		fmt.Fprintf(&sb, "%d. `%s`", i+1, h.Ref.String())
		if h.Time > 0 {
			fmt.Fprintf(&sb, " (%s)", time.UnixMilli(h.Time).Format("2006-01-02"))
		}
		sb.WriteString("\n")
		snippet := strings.TrimSpace(h.Snippet)
		if snippet != "" {
			sb.WriteString("   ")
			sb.WriteString(truncate(snippet, 240))
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}
	sb.WriteString("자세한 내용은 `knowledge(op=\"read\", ref=\"...\")` 로 ref 지정.")
	return sb.String(), nil
}

func knowledgeRead(ctx context.Context, router *knowledge.Router, refStr string) (string, error) {
	refStr = strings.TrimSpace(refStr)
	if refStr == "" {
		return "", fmt.Errorf("ref is required for knowledge(op=\"read\")")
	}
	ref, err := knowledge.ParseRef(refStr)
	if err != nil {
		return "", err
	}
	doc, err := router.Read(ctx, ref)
	if err != nil {
		return "", err
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
