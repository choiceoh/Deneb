package briefcase

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	casepack "github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase"
)

var ErrPolicyDenied = errors.New("briefcase runtime policy denied")

type Operation string

const (
	OperationRead    Operation = "read"
	OperationWrite   Operation = "write"
	OperationDevice  Operation = "device"
	OperationNetwork Operation = "network"
	OperationExec    Operation = "exec"
)

type PolicyRequest struct {
	Operation  Operation
	Path       string
	DeviceKind string
}

type PolicyOptions struct {
	AllowedDeviceKinds []string
	// WritableArtifacts maps manifest-relative output paths to signed byte
	// limits. A non-nil map restricts mutations to exactly these paths.
	WritableArtifacts map[string]int64
}

type DenialError struct {
	Operation Operation
	Resource  string
	Reason    string
}

func (e *DenialError) Error() string {
	if e == nil {
		return ErrPolicyDenied.Error()
	}
	return fmt.Sprintf("%s: operation=%s resource=%q reason=%s", ErrPolicyDenied, e.Operation, e.Resource, e.Reason)
}

func (e *DenialError) Unwrap() error { return ErrPolicyDenied }

// Policy authorizes only local reads/writes inside its RunRoot and explicitly
// allowlisted DeviceTwin action kinds. Network, process execution, malformed
// requests, and unknown operations are always denied.
type Policy struct {
	root               *RunRoot
	readRoots          []string
	writeRoots         []string
	allowedDeviceKinds map[string]struct{}
	writeLimits        map[string]int64
	restrictWrites     bool
	mutationMu         sync.Mutex
}

func NewPolicy(root *RunRoot, opts PolicyOptions) (*Policy, error) {
	paths, err := root.Paths()
	if err != nil {
		return nil, fmt.Errorf("briefcase: create runtime policy: %w", err)
	}
	owned := []string{paths.Home, paths.State, paths.Files, paths.Workspace, paths.Artifacts, paths.Logs, paths.Temp}
	for _, dir := range append([]string{paths.Root}, owned...) {
		info, err := os.Lstat(dir)
		if err != nil {
			return nil, fmt.Errorf("briefcase: validate policy root %s: %w", dir, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
			return nil, fmt.Errorf("briefcase: insecure policy root %s (mode %s)", dir, info.Mode())
		}
	}
	allowed := make(map[string]struct{}, len(opts.AllowedDeviceKinds))
	for _, kind := range opts.AllowedDeviceKinds {
		kind = strings.TrimSpace(kind)
		if kind == "" {
			return nil, errors.New("briefcase: empty allowed device kind")
		}
		allowed[kind] = struct{}{}
	}
	writeLimits := make(map[string]int64, len(opts.WritableArtifacts))
	for relative, limit := range opts.WritableArtifacts {
		absolute := filepath.Clean(filepath.Join(paths.Workspace, filepath.FromSlash(relative)))
		if !pathWithin(filepath.Join(paths.Workspace, "output"), absolute) {
			return nil, fmt.Errorf("briefcase: writable artifact path escapes output: %q", relative)
		}
		if limit <= 0 || limit > int64(casepack.MaxArtifactBytesV1) {
			return nil, fmt.Errorf("briefcase: invalid writable artifact limit for %q", relative)
		}
		writeLimits[absolute] = limit
	}
	return &Policy{
		root:               root,
		readRoots:          append([]string(nil), owned...),
		writeRoots:         append([]string(nil), owned...),
		allowedDeviceKinds: allowed,
		writeLimits:        writeLimits,
		restrictWrites:     opts.WritableArtifacts != nil,
	}, nil
}

func (p *Policy) Authorize(req PolicyRequest) error {
	if p == nil || p.root == nil || p.root.isClosed() {
		return deny(req.Operation, requestResource(req), "run root is unavailable")
	}
	switch req.Operation {
	case OperationRead:
		if req.DeviceKind != "" {
			return deny(req.Operation, req.Path, "read request contains a device kind")
		}
		return p.authorizePath(req.Operation, req.Path, p.readRoots)
	case OperationWrite:
		if req.DeviceKind != "" {
			return deny(req.Operation, req.Path, "write request contains a device kind")
		}
		return p.authorizePath(req.Operation, req.Path, p.writeRoots)
	case OperationDevice:
		if req.Path != "" {
			return deny(req.Operation, req.DeviceKind, "device request contains a filesystem path")
		}
		kind := strings.TrimSpace(req.DeviceKind)
		if kind == "" {
			return deny(req.Operation, kind, "device kind is required")
		}
		if _, ok := p.allowedDeviceKinds[kind]; !ok {
			return deny(req.Operation, kind, "device kind is not allowlisted")
		}
		return nil
	case OperationNetwork:
		return deny(req.Operation, requestResource(req), "network is disabled in Briefcase runs")
	case OperationExec:
		return deny(req.Operation, requestResource(req), "process execution is disabled in Briefcase runs")
	default:
		return deny(req.Operation, requestResource(req), "unknown operation")
	}
}

func (p *Policy) CheckRead(path string) error {
	return p.Authorize(PolicyRequest{Operation: OperationRead, Path: path})
}

func (p *Policy) CheckWrite(path string) error {
	if err := p.Authorize(PolicyRequest{Operation: OperationWrite, Path: path}); err != nil {
		return err
	}
	if p.restrictWrites {
		if _, ok := p.writeLimits[path]; !ok {
			return deny(OperationWrite, path, "path is not a declared artifact")
		}
	}
	return nil
}

func (p *Policy) WriteLimit(path string) (int64, error) {
	if err := p.CheckWrite(path); err != nil {
		return 0, err
	}
	if limit, ok := p.writeLimits[path]; ok {
		return limit, nil
	}
	return int64(casepack.MaxArtifactBytesV1), nil
}

func (p *Policy) LockMutation() func() {
	p.mutationMu.Lock()
	return p.mutationMu.Unlock
}

func (p *Policy) CheckDevice(kind string) error {
	return p.Authorize(PolicyRequest{Operation: OperationDevice, DeviceKind: kind})
}

func (p *Policy) CheckNetwork() error {
	return p.Authorize(PolicyRequest{Operation: OperationNetwork})
}

func (p *Policy) CheckExec() error {
	return p.Authorize(PolicyRequest{Operation: OperationExec})
}

func (p *Policy) authorizePath(operation Operation, path string, roots []string) error {
	if strings.TrimSpace(path) == "" {
		return deny(operation, path, "path is required")
	}
	if !filepath.IsAbs(path) {
		return deny(operation, path, "path must be absolute")
	}
	abs := filepath.Clean(path)
	if abs != path {
		return deny(operation, path, "path must already be clean")
	}
	for _, root := range roots {
		ok, err := securelyWithin(root, abs)
		if err != nil {
			return deny(operation, path, err.Error())
		}
		if ok {
			return nil
		}
	}
	return deny(operation, path, "path is outside the run root")
}

// securelyWithin performs both a lexical containment check and a symlink-aware
// check of the deepest existing ancestor. This also protects writes to paths
// that do not exist yet but sit below an escaping symlink.
func securelyWithin(root, target string) (bool, error) {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false, fmt.Errorf("cannot compare path to allowed root: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return false, nil
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false, fmt.Errorf("cannot resolve allowed root: %w", err)
	}
	ancestor := target
	for {
		_, statErr := os.Lstat(ancestor)
		if statErr == nil {
			break
		}
		if !os.IsNotExist(statErr) {
			return false, fmt.Errorf("cannot inspect path: %w", statErr)
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return false, errors.New("no existing path ancestor")
		}
		ancestor = parent
	}
	resolvedAncestor, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return false, fmt.Errorf("cannot resolve path ancestor: %w", err)
	}
	resolvedRel, err := filepath.Rel(resolvedRoot, resolvedAncestor)
	if err != nil {
		return false, fmt.Errorf("cannot compare resolved path to allowed root: %w", err)
	}
	if resolvedRel == ".." || strings.HasPrefix(resolvedRel, ".."+string(filepath.Separator)) || filepath.IsAbs(resolvedRel) {
		return false, nil
	}
	return true, nil
}

func deny(operation Operation, resource, reason string) error {
	return &DenialError{Operation: operation, Resource: resource, Reason: reason}
}

func requestResource(req PolicyRequest) string {
	if req.Path != "" {
		return req.Path
	}
	return req.DeviceKind
}
