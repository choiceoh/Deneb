package meeting

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
)

// parsePlaudList decodes plaud_list_files output: {"type":"list","data":[...]}
// with id/name/start_at/duration(ms) rows. Timestamps are naive UTC
// ("2026-07-06T01:05:35", sometimes fractional).
func parsePlaudList(raw string) ([]plaudFile, error) {
	var envelope struct {
		Data []struct {
			ID       string  `json:"id"`
			Name     string  `json:"name"`
			StartAt  string  `json:"start_at"`
			Duration float64 `json:"duration"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return nil, fmt.Errorf("list envelope: %w", err)
	}
	out := make([]plaudFile, 0, len(envelope.Data))
	for _, d := range envelope.Data {
		out = append(out, plaudFile{
			ID:       strings.TrimSpace(d.ID),
			Name:     strings.TrimSpace(d.Name),
			StartAt:  parsePlaudTime(d.StartAt),
			Duration: time.Duration(d.Duration) * time.Millisecond,
		})
	}
	return out, nil
}

func parsePlaudTime(s string) time.Time {
	for _, layout := range []string{"2006-01-02T15:04:05.999999", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, strings.TrimSpace(s)); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// parsePlaudTranscript flattens get_transcript output into "speaker: text"
// lines. The payload is a JSON array of segments whose data_content is itself
// a JSON-encoded array of utterances; field names beyond "content" are not
// contractual, so unknown shapes degrade to the raw text (the synthesis model
// tolerates it) rather than failing the recording.
func parsePlaudTranscript(raw string) string {
	var segments []struct {
		DataContent string `json:"data_content"`
	}
	if err := json.Unmarshal([]byte(raw), &segments); err != nil {
		// The MCP tool result may wrap the JSON array in extra text (the
		// production first run archived a raw-JSON excerpt because of this) —
		// retry on the outermost bracketed span before giving up.
		s, e := strings.Index(raw, "["), strings.LastIndex(raw, "]")
		if s < 0 || e <= s || json.Unmarshal([]byte(raw[s:e+1]), &segments) != nil {
			return strings.TrimSpace(raw)
		}
	}
	var b strings.Builder
	for _, seg := range segments {
		if strings.TrimSpace(seg.DataContent) == "" {
			continue
		}
		var utterances []map[string]any
		if err := json.Unmarshal([]byte(seg.DataContent), &utterances); err != nil {
			b.WriteString(strings.TrimSpace(seg.DataContent))
			b.WriteString("\n")
			continue
		}
		for _, u := range utterances {
			content, _ := u["content"].(string)
			if strings.TrimSpace(content) == "" {
				continue
			}
			if sp := utteranceSpeaker(u); sp != "" {
				b.WriteString(sp)
				b.WriteString(": ")
			}
			b.WriteString(strings.TrimSpace(content))
			b.WriteString("\n")
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return strings.TrimSpace(raw)
	}
	return out
}

func utteranceSpeaker(u map[string]any) string {
	for _, key := range []string{"speaker", "speaker_name", "speaker_label", "speaker_id"} {
		switch v := u[key].(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		case float64:
			return fmt.Sprintf("화자%d", int(v))
		}
	}
	return ""
}

// splitRelatedProjects pops the trailing "관련프로젝트:" line off the report and
// resolves it against the offered candidates — the model may only link paths
// it was shown (anti-hallucination), capped at plaudRelatedProjectCap.
func splitRelatedProjects(report string, cands []mailanalysis.ProjectCandidate) (string, []string) {
	lines := strings.Split(strings.TrimSpace(report), "\n")
	idx := -1
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-4; i-- {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "관련프로젝트:") {
			idx = i
			break
		}
	}
	if idx < 0 {
		return report, nil
	}
	value := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[idx]), "관련프로젝트:"))
	body := strings.TrimSpace(strings.Join(append(append([]string{}, lines[:idx]...), lines[idx+1:]...), "\n"))
	if value == "" || value == "없음" {
		return body, nil
	}
	known := make(map[string]bool, len(cands))
	for _, c := range cands {
		known[strings.TrimSpace(c.Path)] = true
	}
	var related []string
	for _, p := range strings.Split(value, ",") {
		p = strings.TrimSpace(p)
		if p == "" || !known[p] {
			continue
		}
		related = append(related, p)
		if len(related) >= plaudRelatedProjectCap {
			break
		}
	}
	return body, related
}
