package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
	Latency   string `json:"latency,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// HealthStatus is the aggregated health report.
type HealthStatus struct {
	Components []ComponentHealth `json:"components"`
}

// ToolHealthCheck creates the health_check ToolFunc.
func ToolHealthCheck(localAI LocalAIProbe) ToolFunc {
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

		switch p.Component {
		case "localai":
			return formatLocalAIHealth(localAI), nil
		case "memory":
			return "memory store replaced by wiki", nil
		}

		var rows []ComponentHealth

		if p.Component != "all" {
			return fmt.Sprintf("❌ component %q not found", p.Component), nil
		}

		// Local AI health.
		localAIGw := ComponentHealth{Name: "localai (gateway)"}
		if localAI.CheckHealth() {
			localAIGw.Available = true
			localAIGw.Detail = "chat hooks + compression operational"
		} else {
			localAIGw.Detail = "unreachable at " + localAI.BaseURL()
		}
		rows = append(rows, localAIGw)

		return formatHealthStatus(HealthStatus{Components: rows}), nil
	}
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
func formatLocalAIHealth(probe LocalAIProbe) string {
	icon := "❌"
	detail := "unreachable"
	if probe.CheckHealth() {
		icon = "✅"
		detail = "operational"
	}
	return fmt.Sprintf("## Local AI 상태\n\n%s localai (%s)\nBase URL: %s",
		icon, detail, probe.BaseURL())
}
