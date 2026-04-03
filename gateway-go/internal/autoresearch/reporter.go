package autoresearch

import (
	"context"
	"fmt"
)

// sendCompletionReport generates and sends the final experiment summary
// and chart to the notifier (Telegram). Called from the loop's defer on
// any exit — max iterations, manual stop, cancel, or panic.
func (r *Runner) sendCompletionReport(workdir string, reason string) {
	ctx := context.Background()

	cfg, err := LoadConfig(workdir)
	if err != nil {
		r.notify(ctx, fmt.Sprintf("Autoresearch %s (no config found for report).", reason))
		return
	}

	// Skip report if no iterations were run.
	if cfg.TotalIterations == 0 {
		r.notify(ctx, fmt.Sprintf("Autoresearch %s (no iterations completed).", reason))
		return
	}

	// Send text summary.
	summary := Summary(workdir, cfg)
	r.notify(ctx, fmt.Sprintf("Autoresearch %s\n\n%s", reason, summary))

	// Generate and send chart.
	rows, err := ParseResults(workdir)
	if err != nil || len(rows) == 0 {
		r.logger.Warn("skipping chart: no results to render", "error", err)
		return
	}
	png, err := RenderChart(rows, cfg)
	if err != nil {
		r.logger.Error("failed to render completion chart", "error", err)
		return
	}
	// Also save to disk for later reference.
	if _, saveErr := SaveChart(workdir, rows, cfg); saveErr != nil {
		r.logger.Warn("failed to save chart to disk", "error", saveErr)
	}

	caption := fmt.Sprintf("%s — %d iterations", cfg.MetricName, cfg.TotalIterations)
	if cfg.BestMetric != nil {
		caption = fmt.Sprintf("%s — %d iterations, best: %.6f",
			cfg.MetricName, cfg.TotalIterations, *cfg.BestMetric)
		if cfg.BaselineMetric != nil && *cfg.BaselineMetric != 0 {
			improvement := (*cfg.BaselineMetric - *cfg.BestMetric) / *cfg.BaselineMetric * 100
			if cfg.MetricDirection == "maximize" {
				improvement = (*cfg.BestMetric - *cfg.BaselineMetric) / *cfg.BaselineMetric * 100
			}
			caption += fmt.Sprintf(" (%.2f%%)", improvement)
		}
	}
	r.notifyPhoto(ctx, png, caption)

	// Inject completion summary into the triggering session's transcript
	// so the LLM has context about the results on its next turn.
	r.injectResultToTranscript(reason, summary)
}

// injectResultToTranscript appends the autoresearch completion summary to the
// triggering session's transcript as a system note.
func (r *Runner) injectResultToTranscript(reason, summary string) {
	r.mu.Lock()
	fn := r.transcriptAppendFn
	key := r.sessionKey
	r.mu.Unlock()

	if fn == nil || key == "" {
		return
	}

	note := fmt.Sprintf("[Autoresearch %s]\n\n%s", reason, summary)
	// Truncate to avoid bloating the transcript.
	const maxLen = 4000
	if len(note) > maxLen {
		note = note[:maxLen] + "\n... (truncated)"
	}

	if err := fn(key, note); err != nil {
		r.logger.Warn("failed to inject autoresearch result into transcript",
			"sessionKey", key,
			"error", err,
		)
	} else {
		r.logger.Info("injected autoresearch result into session transcript",
			"sessionKey", key,
			"summaryLen", len(note),
		)
	}
}

// notify sends a message via the notifier if one is configured.
func (r *Runner) notify(ctx context.Context, msg string) {
	r.mu.Lock()
	n := r.notifier
	r.mu.Unlock()
	if n != nil {
		if err := n.Notify(ctx, msg); err != nil {
			r.logger.Error("notification failed", "error", err)
		}
	}
}

func (r *Runner) notifyPhoto(ctx context.Context, png []byte, caption string) {
	r.mu.Lock()
	n := r.notifier
	r.mu.Unlock()
	if n != nil {
		if err := n.NotifyPhoto(ctx, png, caption); err != nil {
			r.logger.Error("photo notification failed", "error", err)
		}
	}
}
