package polaris

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	compact "github.com/choiceoh/deneb/gateway-go/internal/pipeline/compaction"
)

// condensing tracks in-flight condensation goroutines to prevent double-scheduling.
var condensing sync.Map // session_key → true

// Condense merges uncondensed summary nodes into higher-level condensed nodes.
//
// For each level starting from 1, it groups uncondensed nodes into batches of
// CondenseFanIn and merges each batch via the summarizer. The process recurses
// up to MaxCondensationDepth. Circuit breaker prevents infinite retries.
func (e *Engine) Condense(ctx context.Context, sessionKey string, summarizer compact.Summarizer) error {
	if summarizer == nil {
		return nil
	}

	// Prevent concurrent condensation for the same session.
	if _, loaded := condensing.LoadOrStore(sessionKey, true); loaded {
		return nil
	}
	defer condensing.Delete(sessionKey)

	// Circuit breaker check.
	if !e.circuit.Allow(sessionKey) {
		return nil
	}

	for level := 1; level < e.cfg.MaxCondensationDepth; level++ {
		nodes, err := e.store.LoadUncondensedNodes(sessionKey, level)
		if err != nil {
			return fmt.Errorf("polaris: load uncondensed level %d: %w", level, err)
		}

		// Need at least CondenseFanIn nodes to merge.
		if len(nodes) < e.cfg.CondenseFanIn {
			break
		}

		// Process complete batches.
		for i := 0; i+e.cfg.CondenseFanIn <= len(nodes); i += e.cfg.CondenseFanIn {
			batch := nodes[i : i+e.cfg.CondenseFanIn]

			if err := e.condenseBatch(ctx, sessionKey, batch, level+1, summarizer); err != nil {
				e.circuit.RecordFailure(sessionKey)
				e.logger.Warn("polaris: condensation failed",
					"session", sessionKey, "level", level, "error", err)
				return err
			}
			e.circuit.RecordSuccess(sessionKey)
		}
	}

	return nil
}

// condenseBatch merges a batch of summary nodes into one condensed node.
func (e *Engine) condenseBatch(
	ctx context.Context,
	sessionKey string,
	batch []SummaryNode,
	newLevel int,
	summarizer compact.Summarizer,
) error {
	// Serialize batch contents for summarization.
	text := serializeSummaryBatch(batch)

	maxOutput := 4096
	summary, err := summarizer.Summarize(ctx, condensationPrompt, text, maxOutput)
	if err != nil {
		return fmt.Errorf("summarize batch: %w", err)
	}
	if summary == "" {
		return fmt.Errorf("empty summary from condensation")
	}

	// Compute the range covered by this condensed node.
	msgStart := batch[0].MsgStart
	msgEnd := batch[len(batch)-1].MsgEnd

	condensedID, err := e.store.InsertSummary(SummaryNode{
		SessionKey: sessionKey,
		Level:      newLevel,
		Content:    summary,
		TokenEst:   compact.EstimateTokens(summary),
		CreatedAt:  time.Now().UnixMilli(),
		MsgStart:   msgStart,
		MsgEnd:     msgEnd,
	})
	if err != nil {
		return fmt.Errorf("insert condensed: %w", err)
	}

	// Mark source nodes as absorbed (provenance).
	ids := make([]int64, len(batch))
	for i, n := range batch {
		ids[i] = n.ID
	}
	if err := e.store.UpdateParentID(ids, condensedID); err != nil {
		return fmt.Errorf("update parent: %w", err)
	}

	e.logger.Info("polaris: condensed summaries",
		"session", sessionKey,
		"level", newLevel,
		"sources", len(batch),
		"range", [2]int{msgStart, msgEnd},
		"tokens", compact.EstimateTokens(summary))
	return nil
}

// serializeSummaryBatch converts summary nodes into text for condensation.
func serializeSummaryBatch(nodes []SummaryNode) string {
	var sb strings.Builder
	for i, n := range nodes {
		sb.WriteString(fmt.Sprintf("--- 요약 %d (메시지 %d-%d, level %d) ---\n%s\n\n",
			i+1, n.MsgStart, n.MsgEnd, n.Level, n.Content))
	}
	return sb.String()
}

// condensationPrompt instructs the summarizer to merge multiple summaries into one.
const condensationPrompt = `아래 여러 요약을 하나로 통합하라.

- 모든 구체적 사실(이름, 숫자, 날짜, 경로, 에러코드)을 빠짐없이 보존하라
- 중복 정보는 최신 값만 유지, 진행 상황은 최신 상태만 기록
- 한국어로 작성 (고유명사/코드는 원문 유지)
- 불릿 위주로 간결하게, 사실 누락 금지`
