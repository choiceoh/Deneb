package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/vega"
)

// healthCheckToolSchema returns the JSON Schema for the health_check tool.
func healthCheckToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"component": map[string]any{
				"type":        "string",
				"enum":        []string{"all", "embedding", "reranker", "sglang"},
				"description": "Component to check (default: all). embedding=Gemini API, reranker=Jina API, sglang=local LLM",
			},
		},
	}
}

// toolHealthCheck creates the health_check ToolFunc.
func toolHealthCheck(deps *CoreToolDeps) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Component string `json:"component"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid health_check params: %w", err)
		}
		if p.Component == "" {
			p.Component = "all"
		}

		// If only sglang is requested and vega backend is not needed, use the
		// cached sglang health check directly.
		if p.Component == "sglang" {
			return formatSglangHealth(), nil
		}

		backend := deps.VegaBackend
		if backend == nil {
			// No vega backend — still report sglang status.
			if p.Component == "all" {
				var sb strings.Builder
				sb.WriteString("## 인프라 상태 점검\n\n")
				sb.WriteString("| 구성요소 | 상태 | 지연시간 | 상세 |\n")
				sb.WriteString("|----------|------|---------|------|\n")
				sb.WriteString("| embedding (Gemini) | ❌ | — | vega backend not configured |\n")
				sb.WriteString("| reranker (Jina) | ❌ | — | vega backend not configured |\n")
				sb.WriteString(formatSglangHealthRow())
				return sb.String(), nil
			}
			return fmt.Sprintf("❌ %s: vega backend not configured", p.Component), nil
		}

		// Type-assert to HealthChecker.
		hc, ok := backend.(vega.HealthChecker)
		if !ok {
			return "❌ health check not supported by this backend", nil
		}

		status := hc.HealthCheck(ctx)

		// Filter to requested component if not "all".
		if p.Component != "all" {
			var filtered []vega.ComponentHealth
			for _, c := range status.Components {
				if matchesComponent(c.Name, p.Component) {
					filtered = append(filtered, c)
				}
			}
			if len(filtered) == 0 {
				return fmt.Sprintf("❌ component %q not found", p.Component), nil
			}
			status.Components = filtered
		} else {
			// Append sglang gateway-level health (chat/compression hooks).
			sglangGw := vega.ComponentHealth{
				Name: "sglang (gateway)",
			}
			if checkSglangHealth() {
				sglangGw.Available = true
				sglangGw.Detail = "chat hooks + compression operational"
			} else {
				sglangGw.Available = false
				sglangGw.Detail = "unreachable at " + defaultSglangBaseURL
			}
			status.Components = append(status.Components, sglangGw)
		}

		return formatHealthStatus(status), nil
	}
}

// matchesComponent checks if a component name matches the filter.
func matchesComponent(name, filter string) bool {
	switch filter {
	case "embedding":
		return strings.Contains(name, "embedding") || strings.Contains(name, "Gemini")
	case "reranker":
		return strings.Contains(name, "reranker") || strings.Contains(name, "Jina")
	case "sglang":
		return strings.Contains(name, "sglang")
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

// formatSglangHealthRow returns a table row for the sglang gateway check.
func formatSglangHealthRow() string {
	healthy := checkSglangHealth()
	icon := "✅"
	detail := "operational"
	if !healthy {
		icon = "❌"
		detail = "unreachable at " + defaultSglangBaseURL
	}
	return fmt.Sprintf("| sglang (gateway) | %s | — | %s |\n", icon, detail)
}
