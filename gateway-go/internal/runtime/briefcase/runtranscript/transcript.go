package runtranscript

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/tokenest"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/transcript"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
)

// Paths is the RunRoot path pair required to own a local transcript stack.
// Callers pass RunRoot.Paths values; this package stays free of a briefcase import
// so the parent can depend on runtranscript without a cycle.
type Paths struct {
	Root  string
	State string
}

// RunTranscript owns the production transcript stack for one Briefcase
// RunRoot: a JSONL transcript plus a Polaris store and the concrete Bridge that
// chat's context assembly recognizes. All durable state stays below State.
//
// HandlerConfig.Transcript must receive Bridge(), not a wrapper around it. The
// chat pipeline intentionally type-asserts *polaris.Bridge before assembling
// prior turns into the next model request.
type RunTranscript struct {
	bridge *polaris.Bridge
	store  *polaris.Store

	closeOnce sync.Once
	closeErr  error
}

// NewRunTranscript creates a RunRoot-local transcript stack. paths must be the
// unmodified value returned by RunRoot.Paths; accepting an arbitrary State path
// would let a benchmark caller reconnect the executor to an operator transcript
// or Polaris store.
func NewRunTranscript(paths Paths, logger *slog.Logger) (*RunTranscript, error) {
	if err := validateTranscriptPaths(paths); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	transcriptDir := filepath.Join(paths.State, "transcripts")
	polarisDir := filepath.Join(paths.State, "polaris")
	for _, dir := range []string{
		transcriptDir,
		polarisDir,
		filepath.Join(polarisDir, "messages"),
		filepath.Join(polarisDir, "summaries"),
	} {
		if err := ensurePrivateLocalDir(dir); err != nil {
			return nil, err
		}
	}

	store, err := polaris.NewStoreWithTokenEstimator(polarisDir, tokenest.EstimateUncalibrated)
	if err != nil {
		return nil, fmt.Errorf("briefcase: create RunRoot-local Polaris transcript store: %w", err)
	}
	legacy := transcript.NewCachedTranscriptStore(transcript.NewFileTranscriptStore(transcriptDir), 0)
	return &RunTranscript{
		bridge: polaris.NewBridgeWithOptions(legacy, store, logger, polaris.BridgeOptions{
			StrictPersistence: true,
		}),
		store: store,
	}, nil
}

// Bridge returns the exact concrete transcript value expected by production
// chat context assembly. The returned pointer is owned by RunTranscript and is
// valid until the enclosing harness is closed.
func (t *RunTranscript) Bridge() *polaris.Bridge {
	if t == nil {
		return nil
	}
	return t.bridge
}

// Close releases the owned Polaris store. Files are deliberately retained;
// RunRoot owns their eventual removal, and a closed-loop attempt may inspect
// them until RunRoot cleanup.
func (t *RunTranscript) Close() error {
	if t == nil {
		return nil
	}
	t.closeOnce.Do(func() {
		if t.store != nil {
			t.closeErr = t.store.Close()
		}
	})
	return t.closeErr
}

func validateTranscriptPaths(paths Paths) error {
	root := filepath.Clean(paths.Root)
	state := filepath.Clean(paths.State)
	if paths.Root == "" || paths.State == "" || !filepath.IsAbs(root) || !filepath.IsAbs(state) {
		return errors.New("briefcase: transcript requires absolute RunRoot and State paths")
	}
	if state != filepath.Join(root, "state") {
		return errors.New("briefcase: transcript State path is not owned by its RunRoot")
	}
	for _, item := range []struct {
		label string
		path  string
	}{{"RunRoot", root}, {"State", state}} {
		info, err := os.Lstat(item.path)
		if err != nil {
			return fmt.Errorf("briefcase: inspect transcript %s: %w", item.label, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("briefcase: transcript %s must be a real directory", item.label)
		}
	}
	return nil
}

func ensurePrivateLocalDir(path string) error {
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("briefcase: transcript path must be a real directory: %s", path)
		}
	case errors.Is(err, os.ErrNotExist):
		if err := os.Mkdir(path, 0o700); err != nil {
			return fmt.Errorf("briefcase: create local transcript directory: %w", err)
		}
	default:
		return fmt.Errorf("briefcase: inspect local transcript directory: %w", err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("briefcase: secure local transcript directory: %w", err)
	}
	return nil
}
