package handlers

import "fmt"

func resolveFirstRestToken(restTokens []string) string {
	if len(restTokens) == 0 {
		return ""
	}
	return restTokens[0]
}

func computeRunRuntime(run SubagentRunRecord, now int64) (string, int64) {
	if run.StartedAt <= 0 {
		return "n/a", 0
	}
	end := run.EndedAt
	if end == 0 {
		end = now
	}
	runtimeMs := end - run.StartedAt
	return FormatDurationCompact(runtimeMs), runtimeMs
}

func buildRunListLine(index int, run SubagentRunRecord, runtime string, taskMaxChars int) string {
	task := TruncateLine(run.Task, taskMaxChars)
	label := FormatRunLabel(run)

	depthTag := ""
	if run.SpawnDepth > 1 {
		depthTag = fmt.Sprintf(" d%d", run.SpawnDepth)
	}
	modelTag := ""
	if run.Model != "" {
		modelTag = fmt.Sprintf(" [%s]", run.Model)
	}
	descendantTag := ""
	if run.PendingDescendants > 0 {
		descendantTag = fmt.Sprintf(" +%d pending", run.PendingDescendants)
	}
	status := FormatRunStatus(run)

	return fmt.Sprintf("#%d %s [%s] (%s%s%s%s) %s", index, label, status, runtime, depthTag, modelTag, descendantTag, task)
}
