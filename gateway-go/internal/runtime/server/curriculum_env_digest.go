package server

// curriculumEnvDigest — the composition-root wiring for CurriculumTask's
// EnvDigest field (RSI P5-1). The digest FORMAT and source orchestration live
// in runtime/curriculumenv; server only injects its (nil-tolerant, late-bound)
// stores. Passing a nil concrete store as a nil interface (not a typed-nil) so
// the digest's nil checks hold.

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/svcops"
)

func (s *Server) curriculumEnvDigest(_ context.Context) string {
	src := svcops.Sources{}
	if s.workFeedStore != nil {
		src.Feed = s.workFeedStore
	}
	if s.wikiStore != nil {
		src.Wiki = s.wikiStore
	}
	if s.agentLogWriter != nil {
		src.AgentLog = s.agentLogWriter
	}
	return svcops.Digest(src)
}
