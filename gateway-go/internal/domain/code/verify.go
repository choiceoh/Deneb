package code

// verify.go — deterministic build/test verification for a session's worktree.
//
// "Did it work?" is the vibe coder's only real signal — they can't read a diff.
// This detects the project's toolchain by marker files, runs its build + test,
// and reports a deterministic pass/fail that flips the session status. The
// agent-driven self-heal loop and the Korean summary that wrap this live in the
// orchestration layer (they need the model); this file is the deterministic
// core: detect → run → pass/fail. Keeping it deterministic means the status is
// trustworthy even when the agent's own exec attempts are flaky.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ProjectKind is the detected toolchain of a worktree.
type ProjectKind string

const (
	KindGo      ProjectKind = "go"
	KindNode    ProjectKind = "node"
	KindRust    ProjectKind = "rust"
	KindPython  ProjectKind = "python"
	KindMake    ProjectKind = "make"
	KindUnknown ProjectKind = "unknown"
)

// VerifyStep is one command run during verification.
type VerifyStep struct {
	Label  string `json:"label"`            // 빌드 | 테스트 | 설치 | 문법 검사
	Cmd    string `json:"cmd"`              // human-readable command, e.g. "go build ./..."
	OK     bool   `json:"ok"`               // command exited 0
	Output string `json:"output,omitempty"` // bounded captured stdout+stderr (or the error)
}

// VerifyResult is the outcome of verifying a worktree.
type VerifyResult struct {
	Kind   ProjectKind  `json:"kind"`
	Passed bool         `json:"passed"` // every planned step ran and succeeded (false for unknown toolchain)
	Steps  []VerifyStep `json:"steps,omitempty"`
}

type plannedStep struct {
	label string
	name  string
	args  []string
}

func (p plannedStep) cmd() string {
	if len(p.args) == 0 {
		return p.name
	}
	return p.name + " " + strings.Join(p.args, " ")
}

// detectKind inspects the worktree for toolchain marker files. exists checks a
// path relative to the worktree root; injectable so detection is unit-testable.
func detectKind(exists func(rel string) bool) ProjectKind {
	switch {
	case exists("go.mod"):
		return KindGo
	case exists("package.json"):
		return KindNode
	case exists("Cargo.toml"):
		return KindRust
	case exists("pyproject.toml"), exists("requirements.txt"), exists("setup.py"):
		return KindPython
	case exists("Makefile"):
		return KindMake
	default:
		return KindUnknown
	}
}

// verifyPlan is the ordered build/test steps for a kind (empty for unknown).
func verifyPlan(kind ProjectKind) []plannedStep {
	switch kind {
	case KindGo:
		return []plannedStep{
			{"빌드", "go", []string{"build", "./..."}},
			{"테스트", "go", []string{"test", "./..."}},
		}
	case KindNode:
		return []plannedStep{
			// --ignore-scripts: don't run the repo's pre/post-install lifecycle
			// scripts during a verify — they execute as the gateway user, unsandboxed.
			{"설치", "npm", []string{"install", "--no-audit", "--no-fund", "--ignore-scripts"}},
			{"빌드", "npm", []string{"run", "build", "--if-present"}},
		}
	case KindRust:
		return []plannedStep{
			{"빌드", "cargo", []string{"build"}},
			{"테스트", "cargo", []string{"test"}},
		}
	case KindPython:
		return []plannedStep{
			{"문법 검사", "python3", []string{"-m", "compileall", "-q", "."}},
		}
	case KindMake:
		return []plannedStep{
			{"빌드", "make", nil},
		}
	default:
		return nil
	}
}

// Verify detects the worktree's toolchain and runs its build + test, returning a
// deterministic pass/fail. Each command runs with dir as the working directory.
// It stops at the first failing step (the agent fixes it, then re-verifies).
func (m *Manager) Verify(ctx context.Context, dir string) (VerifyResult, error) {
	if dir == "" {
		return VerifyResult{}, fmt.Errorf("verify: empty dir")
	}
	kind := detectKind(func(rel string) bool {
		_, err := os.Stat(filepath.Join(dir, rel))
		return err == nil
	})
	plan := verifyPlan(kind)
	res := VerifyResult{Kind: kind}
	if len(plan) == 0 {
		return res, nil // unknown toolchain — nothing to run, can't claim pass
	}

	res.Passed = true
	for _, ps := range plan {
		out, err := m.Runner.Run(ctx, dir, ps.name, ps.args...)
		res.Steps = append(res.Steps, VerifyStep{
			Label:  ps.label,
			Cmd:    ps.cmd(),
			OK:     err == nil,
			Output: boundedOutput(out, err),
		})
		if err != nil {
			res.Passed = false
			break
		}
	}
	return res, nil
}

// boundedOutput returns the command output capped to a rune-safe length, or the
// error text when the command produced nothing.
func boundedOutput(out []byte, err error) string {
	const maxRunes = 4000
	r := []rune(string(out))
	s := string(r)
	if len(r) > maxRunes {
		// Build/test errors land at the END, so keep head + tail and drop the
		// middle rather than head-truncating away the actual failure.
		head := maxRunes / 3
		tail := maxRunes - head
		s = string(r[:head]) + "\n…[중략]…\n" + string(r[len(r)-tail:])
	}
	if strings.TrimSpace(s) == "" && err != nil {
		return err.Error()
	}
	return s
}
