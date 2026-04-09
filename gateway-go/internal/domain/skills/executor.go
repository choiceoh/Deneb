// executor.go provides execution logic for local skill types.
//
// Local skills run a shell command and return stdout.
package skills

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ExecuteLocalSkill runs a local-type skill command and returns its output.
func ExecuteLocalSkill(entry SkillEntry, args string) (string, error) {
	if entry.Metadata == nil || entry.Metadata.LocalExec == nil {
		return "", fmt.Errorf("skill %q has no localExec config", entry.Skill.Name)
	}
	le := entry.Metadata.LocalExec

	timeoutMs := le.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 30_000
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	cmdArgs := make([]string, 0, len(le.Args)+len(strings.Fields(args)))
	cmdArgs = append(cmdArgs, le.Args...)
	cmdArgs = append(cmdArgs, strings.Fields(args)...)
	cmd := exec.CommandContext(ctx, le.Command, cmdArgs...) //nolint:gosec // G204 — command comes from trusted skill definitions
	cmd.Dir = entry.Skill.Dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return "", fmt.Errorf("skill %q failed: %w\n%s", entry.Skill.Name, err, stderr.String())
		}
		return "", fmt.Errorf("skill %q failed: %w", entry.Skill.Name, err)
	}
	return strings.TrimRight(stdout.String(), "\n"), nil
}

// ExecuteSystemSkill is a placeholder for gateway-internal skill dispatch.
// No system handlers are currently registered; this always returns an error.
func ExecuteSystemSkill(entry SkillEntry, _ string) (string, error) {
	return "", fmt.Errorf("system skill %q: no handler registered (type:system is not supported)", entry.Skill.Name)
}
