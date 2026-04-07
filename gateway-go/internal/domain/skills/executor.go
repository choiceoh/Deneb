// executor.go provides execution logic for local and system skill types.
//
// Local skills run a shell command and return stdout.
// System skills dispatch to registered gateway-internal handlers.
package skills

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// SystemHandler is a function that handles a system skill invocation.
type SystemHandler func(args string) (string, error)

// systemHandlers is the registry of system skill handlers.
var (
	systemHandlers   = make(map[string]SystemHandler)
	systemHandlersMu sync.RWMutex
)

// RegisterSystemHandler registers a named handler for system-type skills.
func RegisterSystemHandler(name string, handler SystemHandler) {
	systemHandlersMu.Lock()
	systemHandlers[name] = handler
	systemHandlersMu.Unlock()
}

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

// ExecuteSystemSkill dispatches to a registered system handler.
func ExecuteSystemSkill(entry SkillEntry, args string) (string, error) {
	handlerName := ""
	if entry.Metadata != nil {
		handlerName = entry.Metadata.SystemHandler
	}
	if handlerName == "" {
		handlerName = entry.Skill.Name
	}

	systemHandlersMu.RLock()
	handler, ok := systemHandlers[handlerName]
	systemHandlersMu.RUnlock()

	if !ok {
		return "", fmt.Errorf("no system handler registered for %q", handlerName)
	}
	return handler(args)
}
