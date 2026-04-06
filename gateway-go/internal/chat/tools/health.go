package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// LocalAIProbe provides local AI health-check functions that live outside this
// package (in chat/). Injected to avoid a circular import.
type LocalAIProbe struct {
	// CheckHealth returns true if the local AI server is reachable.
	CheckHealth func() bool
	// BaseURL returns the base URL for the lightweight model.
	BaseURL func() string
}

// ComponentHealth describes the health of a single backend component.
type ComponentHealth struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Latency   string `json:"latency,omitempty"` // e.g. "123ms"
	Detail    string `json:"detail,omitempty"`  // model name, endpoint, or error message
}

// HealthStatus is the aggregated health report.
type HealthStatus struct {
	Components []ComponentHealth `json:"components"`
}

// ToolHealthCheck creates the health_check ToolFunc.
func ToolHealthCheck(d *toolctx.VegaDeps, localAI LocalAIProbe) ToolFunc {
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

		// Single-component shortcuts.
		switch p.Component {
		case "localai":
			return formatLocalAIHealth(localAI), nil
		case "memory":
			return formatMemoryHealth(ctx, d), nil
		}

		var rows []ComponentHealth

		if p.Component != "all" {
			if p.Component == "memory" {
				rows = append(rows, checkMemoryComponent(ctx, d))
			} else {
				return fmt.Sprintf("❌ component %q not found", p.Component), nil
			}
			return formatHealthStatus(HealthStatus{Components: rows}), nil
		}

		// Append gateway-level local AI health.
		localAIGw := ComponentHealth{Name: "localai (gateway)"}
		if localAI.CheckHealth() {
			localAIGw.Available = true
			localAIGw.Detail = "chat hooks + compression operational"
		} else {
			localAIGw.Detail = "unreachable at " + localAI.BaseURL()
		}
		rows = append(rows, localAIGw)

		// Append aurora-memory health.
		rows = append(rows, checkMemoryComponent(ctx, d))

		return formatHealthStatus(HealthStatus{Components: rows}), nil
	}
}

// --- Memory health ---

// checkMemoryComponent probes the aurora-memory store and returns a ComponentHealth.
func checkMemoryComponent(ctx context.Context, d *toolctx.VegaDeps) ComponentHealth {
	ch := ComponentHealth{Name: "aurora-memory"}
	if d.MemoryStore == nil {
		ch.Detail = "not configured"
		return ch
	}

	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	start := time.Now()
	count, err := d.MemoryStore.ActiveFactCount(probeCtx)
	elapsed := time.Since(start)
	ch.Latency = elapsed.Round(time.Millisecond).String()

	if err != nil {
		ch.Detail = "DB error: " + err.Error()
		return ch
	}

	ch.Available = true

	// Load embeddings count for richer status.
	embCount := 0
	if embeddings, err := d.MemoryStore.LoadEmbeddings(probeCtx); err == nil {
		embCount = len(embeddings)
	}

	ch.Detail = fmt.Sprintf("facts=%d, embeddings=%d", count, embCount)
	return ch
}

// formatMemoryHealth returns a standalone aurora-memory health report.
func formatMemoryHealth(ctx context.Context, d *toolctx.VegaDeps) string {
	ch := checkMemoryComponent(ctx, d)
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

// formatHealthStatus renders a HealthStatus as a Korean markdown table.
func formatHealthStatus(status HealthStatus) string {
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

// formatLocalAIHealth returns a standalone local AI health report.
func formatLocalAIHealth(localAI LocalAIProbe) string {
	healthy := localAI.CheckHealth()
	icon := "✅"
	baseURL := localAI.BaseURL()
	detail := "operational at " + baseURL
	if !healthy {
		icon = "❌"
		detail = "unreachable at " + baseURL
	}
	return fmt.Sprintf("## LocalAI 상태\n\n%s %s\n%s", icon, "localai", detail)
}
