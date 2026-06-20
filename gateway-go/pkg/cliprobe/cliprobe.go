// Package cliprobe verifies that an external CLI is not just present on $PATH
// but actually runnable.
//
// exec.LookPath answers "is there a file named X on PATH" — it does NOT answer
// "does X actually run". A broken venv shim (the classic fallout of a system
// Python upgrade) stays on PATH and passes LookPath, then explodes at the real
// invocation with exit 126/127. That failure surfaces deep inside an unrelated
// code path (a yt-dlp download error) with no hint that the tool itself is the
// problem.
//
// Probe runs the tool's `--version` under a short timeout and classifies the
// outcome into one of three states, each carrying an operator-actionable
// remediation hint:
//
//	StatusMissing — not found on PATH (install it)
//	StatusBroken  — found but won't run (reinstall it)
//	StatusOK      — found and runnable
//
// It is intentionally tiny and dependency-free so any caller that shells out to
// a CLI (yt-dlp, ffmpeg, tesseract, …) can swap a bare LookPath for a probe that
// tells the operator what to fix.
package cliprobe

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

// Status classifies the runnability of a CLI tool.
type Status int

const (
	// StatusOK means the tool was found on PATH and `--version` ran cleanly.
	StatusOK Status = iota
	// StatusMissing means the tool is not on PATH (LookPath failed).
	StatusMissing
	// StatusBroken means the tool is on PATH but failed to execute — a broken
	// shim, a missing interpreter, or a non-zero/127/126 exit on `--version`.
	StatusBroken
)

func (s Status) String() string {
	switch s {
	case StatusOK:
		return "ok"
	case StatusMissing:
		return "missing"
	case StatusBroken:
		return "broken"
	default:
		return "unknown"
	}
}

// Result is the outcome of probing a single CLI tool.
type Result struct {
	// Name is the command that was probed (e.g. "yt-dlp").
	Name string
	// Status is the runnability classification.
	Status Status
	// Path is the resolved absolute path when found on PATH (empty when missing).
	Path string
	// Err is the underlying error for missing/broken outcomes (nil when OK).
	Err error
	// Hint is an operator-actionable remediation string for missing/broken
	// outcomes (empty when OK). Example: "pip install --force-reinstall yt-dlp".
	Hint string
}

// OK reports whether the tool is found and runnable.
func (r Result) OK() bool { return r.Status == StatusOK }

// defaultProbeTimeout bounds a `--version` invocation. `--version` is a
// fast local operation; a tool that can't print its version in a couple of
// seconds is effectively broken for our purposes.
const defaultProbeTimeout = 3 * time.Second

// Probe checks whether name is present on PATH and actually executable by
// running `name --version` under a short timeout. The optional hint is returned
// in Result.Hint for missing/broken outcomes (it should describe how to install
// or repair the tool, e.g. "pip install --force-reinstall yt-dlp"). Pass "" to
// use a generic fallback hint.
//
// Probe never returns an error itself; the outcome is fully described by the
// Result (callers branch on Result.Status / Result.OK).
func Probe(ctx context.Context, name, hint string) Result {
	res := Result{Name: name, Hint: hint}

	path, err := exec.LookPath(name)
	if err != nil {
		res.Status = StatusMissing
		res.Err = err
		if res.Hint == "" {
			res.Hint = "install `" + name + "` and ensure it is on PATH"
		}
		return res
	}
	res.Path = path

	probeCtx, cancel := context.WithTimeout(ctx, defaultProbeTimeout)
	defer cancel()

	// Use the resolved path so we exec exactly what LookPath found.
	cmd := exec.CommandContext(probeCtx, path, "--version")
	runErr := cmd.Run()
	if runErr != nil {
		res.Status = StatusBroken
		res.Err = runErr
		if res.Hint == "" {
			res.Hint = "`" + name + "` is on PATH but failed to run — reinstall it"
		}
		return res
	}

	res.Status = StatusOK
	return res
}

// brokenExitCodes are exit codes that indicate the shell could not execute the
// command (127 = command not found / interpreter missing, 126 = found but not
// executable). These are the classic broken-shim signatures. Exposed so callers
// can recognize them, though Probe already treats any non-zero exit as broken.
var brokenExitCodes = map[int]struct{}{126: {}, 127: {}}

// IsBrokenExit reports whether err is an *exec.ExitError whose code is a
// shell "cannot execute" signal (126/127). Useful for callers that already
// shelled out and want to distinguish a broken tool from a normal task failure.
func IsBrokenExit(err error) bool {
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return false
	}
	_, ok := brokenExitCodes[ee.ExitCode()]
	return ok
}

// FormatProblem renders a one-line, log-friendly description of a non-OK probe
// result, including the remediation hint. Returns "" for an OK result so callers
// can `if msg := FormatProblem(r); msg != "" { ... }` without a status check.
func FormatProblem(r Result) string {
	if r.OK() {
		return ""
	}
	var b strings.Builder
	b.WriteString(r.Name)
	b.WriteString(": ")
	b.WriteString(r.Status.String())
	if r.Err != nil {
		b.WriteString(" (")
		b.WriteString(r.Err.Error())
		b.WriteString(")")
	}
	if r.Hint != "" {
		b.WriteString(" — ")
		b.WriteString(r.Hint)
	}
	return b.String()
}
