package serverauto

// curriculumEnvDigest — the composition-root wiring for CurriculumTask's
// EnvDigest field (RSI P5-1). The digest FORMAT and source orchestration live
// in runtime/curriculumenv; serverauto only injects its (nil-tolerant,
// late-bound) stores. Passing a nil concrete store as a nil interface (not a
// typed-nil) so the digest's nil checks hold.

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/curriculumenv"
)

func (m *Manager) curriculumEnvDigest(_ context.Context) string {
	src := curriculumenv.Sources{}
	if feed := m.Host.WorkFeedStore(); feed != nil {
		src.Feed = feed
	}
	if wikiStore := m.Host.WikiStore(); wikiStore != nil {
		src.Wiki = wikiStore
	}
	if m.AgentLogWriter != nil {
		src.AgentLog = m.AgentLogWriter
	}
	return curriculumenv.Digest(src)
}
