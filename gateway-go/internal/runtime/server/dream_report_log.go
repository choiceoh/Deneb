package server

// dream-reports.jsonl rollup — the weekly memory digest's source of truth
// (docs/research/improvement-ideas.md 4.9). DreamReport counters were
// previously ephemeral: one feed card per cycle, then gone. The rollup keeps
// every completed cycle auditable and gives the digest (and any future
// dreamer regression analysis) real data to read.

import (
	"path/filepath"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

// dreamReportEntry timestamps one persisted DreamReport.
type dreamReportEntry struct {
	AtMs   int64                  `json:"atMs"`
	Report autonomous.DreamReport `json:"report"`
}

func dreamReportsPath() string {
	return filepath.Join(config.ResolveStateDir(), "data", "dream-reports.jsonl")
}

// appendDreamReport persists one completed dream cycle. Best-effort: a failed
// append must never affect the dream cycle itself.
func (s *Server) appendDreamReport(r *autonomous.DreamReport) {
	if s == nil || r == nil {
		return
	}
	entry := dreamReportEntry{AtMs: time.Now().UnixMilli(), Report: *r}
	if err := jsonlstore.Append(dreamReportsPath(), entry); err != nil {
		s.logger.Warn("dream report rollup append failed", "error", err)
	}
}

// loadDreamReportsSince returns rollup entries recorded after sinceMs.
func loadDreamReportsSince(sinceMs int64) ([]dreamReportEntry, error) {
	entries, err := jsonlstore.Load[dreamReportEntry](dreamReportsPath())
	if err != nil {
		return nil, err
	}
	out := make([]dreamReportEntry, 0, len(entries))
	for _, e := range entries {
		if e.AtMs > sinceMs {
			out = append(out, e)
		}
	}
	return out, nil
}
