package autoreply

import (
	"strings"
	"time"
)

// TemplateVars holds variables available for message template interpolation.
type TemplateVars struct {
	AgentID     string
	SessionKey  string
	Channel     string
	From        string
	To          string
	Timestamp   time.Time
	IsGroup     bool
	Model       string
	Provider    string
	CurrentTime string
}

// ApplyTemplate interpolates {{variable}} placeholders in a template string.
func ApplyTemplate(template string, vars TemplateVars) string {
	if template == "" || !strings.Contains(template, "{{") {
		return template
	}

	replacements := map[string]string{
		"{{agentId}}":     vars.AgentID,
		"{{sessionKey}}":  vars.SessionKey,
		"{{channel}}":     vars.Channel,
		"{{from}}":        vars.From,
		"{{to}}":          vars.To,
		"{{model}}":       vars.Model,
		"{{provider}}":    vars.Provider,
		"{{currentTime}}": vars.CurrentTime,
		"{{timestamp}}":   formatTemplateTimestamp(vars.Timestamp),
		"{{isGroup}}":     boolToStr(vars.IsGroup),
	}

	result := template
	for key, value := range replacements {
		if strings.Contains(result, key) {
			result = strings.ReplaceAll(result, key, value)
		}
	}
	return result
}

func formatTemplateTimestamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func boolToStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// ResolveCurrentTimeString returns the current time formatted for templates.
func ResolveCurrentTimeString(timezone string) string {
	t := time.Now()
	if timezone != "" && timezone != "utc" {
		if loc, err := time.LoadLocation(timezone); err == nil {
			t = t.In(loc)
		}
	}
	return t.Format("2006-01-02 15:04:05 MST")
}
