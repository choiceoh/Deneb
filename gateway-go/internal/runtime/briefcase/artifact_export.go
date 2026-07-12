package briefcase

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	casepack "github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase"
)

type artifactExportPlan struct {
	sourcePath          string
	resolvedSourceRoot  string
	destination         string
	resolvedDestination string
}

type selectedArtifact struct {
	id     string
	source string
	target string
}

// ExportRunArtifacts copies only signed, declared artifacts into a durable
// directory and rewrites ArtifactRoot on a detached result. The destination
// must not already exist, preventing stale files from contaminating a score.
func ExportRunArtifacts(ctx context.Context, pack *casepack.Pack, run *RunResult, destination string) (_ *RunResult, returnErr error) {
	return exportRunArtifacts(ctx, pack, run, destination, false)
}

func exportRunArtifacts(ctx context.Context, pack *casepack.Pack, run *RunResult, destination string, allowInsideRunRoot bool) (_ *RunResult, returnErr error) {
	plan, err := planArtifactExport(pack, run, destination, allowInsideRunRoot)
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := createArtifactExportDestination(plan.destination); err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		cleanupFailedArtifactExport(plan.destination, committed, &returnErr)
	}()
	if err := verifyArtifactExportDestination(plan); err != nil {
		return nil, err
	}

	snapshot := cloneRunResult(run)
	if err := exportDeclaredArtifacts(ctx, pack.Manifest.Artifacts, plan); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	recordArtifactExport(snapshot, plan.destination)
	committed = true
	return snapshot, nil
}

func planArtifactExport(pack *casepack.Pack, run *RunResult, destination string, allowInsideRunRoot bool) (artifactExportPlan, error) {
	if pack == nil || run == nil {
		return artifactExportPlan{}, errors.New("briefcase: pack and run result are required for artifact export")
	}
	sourcePath, resolvedSourceRoot, err := resolveArtifactSourceRoot(run.ArtifactRoot)
	if err != nil {
		return artifactExportPlan{}, err
	}
	destination, resolvedDestination, err := resolveArtifactExportDestination(resolvedSourceRoot, destination, allowInsideRunRoot)
	if err != nil {
		return artifactExportPlan{}, err
	}
	return artifactExportPlan{
		sourcePath:          sourcePath,
		resolvedSourceRoot:  resolvedSourceRoot,
		destination:         destination,
		resolvedDestination: resolvedDestination,
	}, nil
}

func resolveArtifactSourceRoot(root string) (string, string, error) {
	if strings.TrimSpace(root) == "" {
		return "", "", errors.New("briefcase: run artifact root is invalid")
	}
	sourcePath, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", "", errors.New("briefcase: run artifact root is invalid")
	}
	resolvedRoot, err := filepath.EvalSymlinks(sourcePath)
	if err != nil {
		return "", "", fmt.Errorf("briefcase: resolve run artifact root: %w", err)
	}
	rootInfo, err := os.Stat(resolvedRoot)
	if err != nil || !rootInfo.IsDir() {
		return "", "", errors.New("briefcase: run artifact root is not a directory")
	}
	return sourcePath, resolvedRoot, nil
}

func resolveArtifactExportDestination(sourceRoot, destination string, allowInsideRunRoot bool) (string, string, error) {
	if strings.TrimSpace(destination) == "" {
		return "", "", errors.New("briefcase: artifact export destination is required")
	}
	destination, err := filepath.Abs(filepath.Clean(strings.TrimSpace(destination)))
	if err != nil {
		return "", "", errors.New("briefcase: artifact export destination is required")
	}
	resolvedDestination, err := resolveProspectivePath(destination)
	if err != nil {
		return "", "", fmt.Errorf("briefcase: resolve artifact export destination: %w", err)
	}
	if pathWithin(sourceRoot, resolvedDestination) {
		return "", "", errors.New("briefcase: artifact export destination must be outside the run root")
	}
	if !allowInsideRunRoot && destinationInsidePlaintextRunRoot(sourceRoot, resolvedDestination) {
		return "", "", errors.New("briefcase: durable artifact export destination must be outside the plaintext RunRoot")
	}
	if err := requireFreshArtifactExportDestination(destination); err != nil {
		return "", "", err
	}
	return destination, resolvedDestination, nil
}

func destinationInsidePlaintextRunRoot(sourceRoot, destination string) bool {
	plaintextRoot := inferredPlaintextRunRoot(sourceRoot)
	return plaintextRoot != "" && pathWithin(plaintextRoot, destination)
}

func requireFreshArtifactExportDestination(destination string) error {
	_, err := os.Lstat(destination)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err == nil {
		return errors.New("briefcase: artifact export destination already exists")
	}
	return fmt.Errorf("briefcase: inspect artifact export destination: %w", err)
}

func createArtifactExportDestination(destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return fmt.Errorf("briefcase: create artifact export parent: %w", err)
	}
	if err := os.Mkdir(destination, 0o700); err != nil {
		return fmt.Errorf("briefcase: create artifact export destination: %w", err)
	}
	return nil
}

func verifyArtifactExportDestination(plan artifactExportPlan) error {
	resolvedCreatedDestination, err := filepath.EvalSymlinks(plan.destination)
	if err != nil || resolvedCreatedDestination != plan.resolvedDestination {
		return errors.New("briefcase: artifact export destination changed during creation")
	}
	return nil
}

func cleanupFailedArtifactExport(destination string, committed bool, returnErr *error) {
	if committed {
		return
	}
	if cleanupErr := os.RemoveAll(destination); cleanupErr != nil {
		*returnErr = errors.Join(*returnErr, fmt.Errorf("briefcase: clean failed artifact export: %w", cleanupErr))
	}
}

func exportDeclaredArtifacts(ctx context.Context, declarations []casepack.Artifact, plan artifactExportPlan) error {
	for _, declaration := range declarations {
		artifact, present, err := selectCompletedArtifact(ctx, declaration, plan)
		if err != nil {
			return err
		}
		if !present {
			continue
		}
		if err := writeSelectedArtifact(ctx, artifact); err != nil {
			return fmt.Errorf("briefcase: snapshot completed artifact %q: %w", artifact.id, err)
		}
	}
	return nil
}

func selectCompletedArtifact(ctx context.Context, declaration casepack.Artifact, plan artifactExportPlan) (selectedArtifact, bool, error) {
	if err := ctx.Err(); err != nil {
		return selectedArtifact{}, false, err
	}
	source, info, present, err := inspectCompletedArtifact(declaration, plan)
	if err != nil || !present {
		return selectedArtifact{}, present, err
	}
	limit := declaration.MaxBytes
	if limit <= 0 {
		limit = casepack.MaxArtifactBytesV1
	}
	if info.Size() > limit {
		return selectedArtifact{}, false, fmt.Errorf("briefcase: completed artifact %q exceeds its signed size limit", declaration.ID)
	}
	return selectedArtifact{
		id:     declaration.ID,
		source: source,
		target: filepath.Join(plan.destination, filepath.FromSlash(declaration.Path)),
	}, true, nil
}

func inspectCompletedArtifact(declaration casepack.Artifact, plan artifactExportPlan) (string, os.FileInfo, bool, error) {
	source := filepath.Join(plan.sourcePath, filepath.FromSlash(declaration.Path))
	info, err := os.Lstat(source)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil, false, nil
	}
	if err != nil {
		return "", nil, false, fmt.Errorf("briefcase: inspect completed artifact %q: %w", declaration.ID, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", nil, false, fmt.Errorf("briefcase: completed artifact %q is not a regular file", declaration.ID)
	}
	resolvedSource, err := filepath.EvalSymlinks(source)
	if err != nil || !pathWithin(plan.resolvedSourceRoot, resolvedSource) {
		return "", nil, false, fmt.Errorf("briefcase: completed artifact %q escapes the run root", declaration.ID)
	}
	return source, info, true, nil
}

func writeSelectedArtifact(ctx context.Context, artifact selectedArtifact) error {
	input, err := os.Open(artifact.source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(artifact.target), 0o700); err != nil {
		return err
	}
	output, err := os.OpenFile(artifact.target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		_ = output.Close()
		if !committed {
			_ = os.Remove(artifact.target)
		}
	}()
	if err := serializeArtifactPayload(ctx, input, output); err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	committed = true
	return ctx.Err()
}

func serializeArtifactPayload(ctx context.Context, input io.Reader, output io.Writer) error {
	buffer := make([]byte, 64*1024)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := input.Read(buffer)
		if n > 0 {
			written, err := output.Write(buffer[:n])
			if err != nil {
				return err
			}
			if written != n {
				return io.ErrShortWrite
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func recordArtifactExport(snapshot *RunResult, destination string) {
	snapshot.ArtifactRoot = destination
}

func resolveProspectivePath(target string) (string, error) {
	target, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return "", err
	}
	current := target
	var suffix []string
	for {
		if _, err := os.Lstat(current); err == nil {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			for index := len(suffix) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, suffix[index])
			}
			return filepath.Clean(resolved), nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("no existing artifact destination ancestor")
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func inferredPlaintextRunRoot(sourceRoot string) string {
	switch {
	case filepath.Base(sourceRoot) == "workspace":
		return filepath.Dir(sourceRoot)
	case filepath.Base(filepath.Dir(sourceRoot)) == "artifacts":
		return filepath.Dir(filepath.Dir(sourceRoot))
	default:
		return ""
	}
}
