package reply

import (
	"fmt"
	"strings"
	"time"
)

// ResponsePrefixTemplate defines the format for response prefix headers.
type ResponsePrefixTemplate struct {
	Format   string // Go template-like format
	Timezone string
}

// FormatResponsePrefix builds a response prefix string from a template.
// Supported placeholders: {timestamp}, {model}, {provider}, {elapsed}.
func FormatResponsePrefix(tmpl ResponsePrefixTemplate, params ResponsePrefixParams) string {
	if tmpl.Format == "" {
		return ""
	}

	result := tmpl.Format

	if strings.Contains(result, "{timestamp}") {
		ts := FormatEnvelopeTimestamp(params.Timestamp, tmpl.Timezone)
		result = strings.ReplaceAll(result, "{timestamp}", ts)
	}
	if strings.Contains(result, "{model}") {
		result = strings.ReplaceAll(result, "{model}", params.Model)
	}
	if strings.Contains(result, "{provider}") {
		result = strings.ReplaceAll(result, "{provider}", params.Provider)
	}
	if strings.Contains(result, "{elapsed}") {
		elapsed := ""
		if params.ElapsedMs > 0 {
			secs := float64(params.ElapsedMs) / 1000.0
			if secs < 1 {
				elapsed = "<1s"
			} else {
				elapsed = strings.TrimRight(strings.TrimRight(
					fmt.Sprintf("%.1f", secs), "0"), ".") + "s"
			}
		}
		result = strings.ReplaceAll(result, "{elapsed}", elapsed)
	}

	return result
}

// ResponsePrefixParams holds the values for response prefix formatting.
type ResponsePrefixParams struct {
	Timestamp time.Time
	Model     string
	Provider  string
	ElapsedMs int64
}

// FormatEnvelopeTimestamp formats a timestamp for response prefix display.
func FormatEnvelopeTimestamp(t time.Time, timezone string) string {
	if t.IsZero() {
		return ""
	}
	loc := time.Local
	if timezone != "" {
		if l, err := time.LoadLocation(timezone); err == nil {
			loc = l
		}
	}
	return t.In(loc).Format("Mon 15:04")
}
