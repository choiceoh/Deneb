package observe

import (
	"strconv"
	"strings"
)

// Prometheus text-format parsing shared by the engine scrapers
// (FetchEngineCounters, the engine diagnostics). Line-at-a-time on purpose: an
// engine's /metrics runs to hundreds of KB and only a handful of series matter.

// parseVllmCounter matches one Prometheus text-format sample line against an
// exact metric name and returns its model_name label and value. Comments,
// other metrics (including longer names sharing the prefix, e.g. *_created),
// and malformed samples return ok=false. An unlabeled sample (single-model
// server) returns model="".
func parseVllmCounter(line, name string) (model string, value float64, ok bool) {
	rest, found := strings.CutPrefix(line, name)
	if !found || rest == "" {
		return "", 0, false
	}
	switch rest[0] {
	case '{':
		end := strings.IndexByte(rest, '}')
		if end < 0 {
			return "", 0, false
		}
		model = promLabel(rest[1:end], "model_name")
		rest = rest[end+1:]
	case ' ', '\t':
		// no label set
	default:
		return "", 0, false // longer metric name sharing this prefix
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", 0, false
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return "", 0, false
	}
	return model, v, true
}

// promLabel extracts one label's value from a Prometheus label body
// `a="x",b="y"`. Served model names never contain escaped quotes, so a plain
// quote scan is enough here. Key matches are anchored at a label boundary so
// e.g. a hypothetical "engine_model_name" label cannot shadow "model_name".
func promLabel(body, key string) string {
	needle := key + `="`
	for idx := strings.Index(body, needle); idx >= 0; {
		if idx == 0 || body[idx-1] == ',' || body[idx-1] == ' ' {
			rest := body[idx+len(needle):]
			if end := strings.IndexByte(rest, '"'); end >= 0 {
				return rest[:end]
			}
			return ""
		}
		next := strings.Index(body[idx+1:], needle)
		if next < 0 {
			return ""
		}
		idx += 1 + next
	}
	return ""
}
