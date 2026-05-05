// Package secretref resolves external secret references used in Deneb config.
package secretref

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	onePasswordPrefix = "op://"
	defaultTimeout    = 5 * time.Second
)

// Runner abstracts command execution for tests.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Output()
}

// Resolver resolves supported secret reference strings.
type Resolver struct {
	Runner  Runner
	Timeout time.Duration
}

// DefaultResolver returns a resolver backed by the local command environment.
func DefaultResolver() Resolver {
	return Resolver{
		Runner:  execRunner{},
		Timeout: defaultTimeout,
	}
}

// IsReference reports whether value uses a supported secret reference scheme.
func IsReference(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), onePasswordPrefix)
}

// ResolveRequired resolves a secret reference. It returns an error when value
// is empty or does not use a supported reference scheme.
func (r Resolver) ResolveRequired(ctx context.Context, value string) (string, error) {
	ref := strings.TrimSpace(value)
	if ref == "" {
		return "", errors.New("secret reference is empty")
	}
	if !strings.HasPrefix(ref, onePasswordPrefix) {
		return "", fmt.Errorf("unsupported secret reference scheme")
	}
	if strings.ContainsAny(ref, "\x00\r\n") {
		return "", fmt.Errorf("secret reference contains control characters")
	}

	timeout := r.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	runner := r.Runner
	if runner == nil {
		runner = execRunner{}
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	out, err := runner.Run(runCtx, "op", "read", ref)
	if runCtx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("1Password secret reference resolution timed out")
	}
	if err != nil {
		return "", fmt.Errorf("1Password secret reference resolution failed: %w", err)
	}

	secret := strings.TrimRight(string(out), "\r\n")
	if secret == "" {
		return "", fmt.Errorf("1Password secret reference resolved to an empty value")
	}
	return secret, nil
}

// ResolveRequired resolves a secret reference using DefaultResolver.
func ResolveRequired(ctx context.Context, value string) (string, error) {
	return DefaultResolver().ResolveRequired(ctx, value)
}
