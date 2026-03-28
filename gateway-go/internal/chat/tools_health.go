package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/vega"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// healthCheckToolSchema returns the JSON Schema for the health_check tool.

// toolHealthCheck creates the health_check ToolFunc.
func toolHealthCheck(deps *CoreToolDeps) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Component string `json:"component"`
		}
		if err := jsonutil.UnmarshalInto("health_check params", input, &p); err != nil {
			return "", err
		}
		if p.Component == "" {
			p.Component = "all"
		}

		// Single-component shortcuts that don't need vega backend.
		switch p.Component {
		case "sglang":
			return formatSglangHealth(), nil
		case "memory":
			return formatMemoryHealth(ctx, deps), nil
		}

		// Collect all component rows for "all" or vega-related components.
		var rows []vega.ComponentHealth

		// Vega-backed components (embedding, reranker, sglang via expander).
		backend := deps.VegaBackend
		if backend != nil {
			if hc, ok := backend.(vega.HealthChecker); ok {
				status := hc.HealthCheck(ctx)
				if p.Component == "all" {
					rows = append(rows, status.Components...)
				} else {
					for _, c := range status.Components {
						if matchesComponent(c.Name, p.Component) {
							rows = append(rows, c)
						}
					}
				}
			}
		} else if p.Component == "all" {
			rows = append(rows,
				vega.ComponentHealth{Name: "embedding (Gemini)", Detail: "vega backend not configured"},
				vega.ComponentHealth{Name: "reranker (Jina)", Detail: "vega backend not configured"},
			)
		} else if p.Component == "embedding" || p.Component == "reranker" {
			return fmt.Sprintf("❌ %s: vega backend not configured", p.Component), nil
		}

		if p.Component != "all" {
			if len(rows) == 0 {
				return fmt.Sprintf("❌ component %q not found", p.Component), nil
			}
			return formatHealthStatus(vega.HealthStatus{Components: rows}), nil
		}

		// Append gateway-level sglang health.
		sglangGw := vega.ComponentHealth{Name: "sglang (gateway)"}
		if checkSglangHealth() {
			sglangGw.Available = true
			sglangGw.Detail = "chat hooks + compression operational"
		} else {
			sglangGw.Detail = "unreachable at " + defaultSglangBaseURL
		}
		rows = append(rows, sglangGw)

		// Append aurora-memory health.
		rows = append(rows, checkMemoryComponent(ctx, deps))

		return formatHealthStatus(vega.HealthStatus{Components: rows}), nil
	}
}

// --- Memory health ---

// checkMemoryComponent probes the aurora-memory store and returns a ComponentHealth.
func checkMemoryComponent(ctx context.Context, deps *CoreToolDeps) vega.ComponentHealth {
	ch := vega.ComponentHealth{Name: "aurora-memory"}
	if deps.MemoryStore == nil {
		ch.Detail = "not configured"
		return ch
	}

	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	start := time.Now()
	count, err := deps.MemoryStore.ActiveFactCount(probeCtx)
	elapsed := time.Since(start)
	ch.Latency = elapsed.Round(time.Millisecond).String()

	if err != nil {
		ch.Detail = "DB error: " + err.Error()
		return ch
	}

	ch.Available = true

	// Load embeddings count for richer status.
	embCount := 0
	if embeddings, err := deps.MemoryStore.LoadEmbeddings(probeCtx); err == nil {
		embCount = len(embeddings)
	}

	ch.Detail = fmt.Sprintf("facts=%d, embeddings=%d", count, embCount)
	return ch
}

// formatMemoryHealth returns a standalone aurora-memory health report.
func formatMemoryHealth(ctx context.Context, deps *CoreToolDeps) string {
	ch := checkMemoryComponent(ctx, deps)
	icon := "✅"
	if !ch.Available {
		icon = "❌"
	}
	latency := ch.Latency
	if latency == "" {
		latency = "—"
	}
	return fmt.Sprintf("## Aurora-Memory 상태\n\n%s %s (latency: %s)\n%s", icon, ch.Name, latency, ch.Detail)
}

// --- Shared helpers ---

// matchesComponent checks if a component name matches the filter.
func matchesComponent(name, filter string) bool {
	switch filter {
	case "embedding":
		return strings.Contains(name, "embedding") || strings.Contains(name, "Gemini")
	case "reranker":
		return strings.Contains(name, "reranker") || strings.Contains(name, "Jina")
	case "sglang":
		return strings.Contains(name, "sglang")
	case "memory":
		return strings.Contains(name, "memory")
	default:
		return false
	}
}

// formatHealthStatus renders a HealthStatus as a Korean markdown table.
func formatHealthStatus(status vega.HealthStatus) string {
	var sb strings.Builder
	sb.WriteString("## 인프라 상태 점검\n\n")
	sb.WriteString("| 구성요소 | 상태 | 지연시간 | 상세 |\n")
	sb.WriteString("|----------|------|---------|------|\n")

	for _, c := range status.Components {
		icon := "✅"
		if !c.Available {
			icon = "❌"
		}
		latency := c.Latency
		if latency == "" {
			latency = "—"
		}
		detail := c.Detail
		if detail == "" {
			detail = "—"
		}
		fmt.Fprintf(&sb, "| %s | %s | %s | %s |\n", c.Name, icon, latency, detail)
	}

	return sb.String()
}

// formatSglangHealth returns a standalone sglang health report.
func formatSglangHealth() string {
	healthy := checkSglangHealth()
	icon := "✅"
	detail := "operational at " + defaultSglangBaseURL
	if !healthy {
		icon = "❌"
		detail = "unreachable at " + defaultSglangBaseURL
	}
	return fmt.Sprintf("## SGLang 상태\n\n%s %s\n%s", icon, "sglang", detail)
}
