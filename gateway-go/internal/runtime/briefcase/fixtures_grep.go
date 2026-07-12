package briefcase

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
)

const (
	defaultPureGrepResults = 100
	maxPureGrepResults     = 500
)

var errPureGrepLimit = errors.New("briefcase: grep result limit reached")

type pureGrepRequest struct {
	Pattern    string `json:"pattern"`
	Path       string `json:"path"`
	IgnoreCase bool   `json:"ignoreCase"`
	MaxResults int    `json:"maxResults"`
}

// pureGrepPlan is the authorized, side-effect-free search decision consumed by
// the filesystem walker. Keeping resolution here makes the execution path deal
// only in workspace-relative names accepted by os.Root.
type pureGrepPlan struct {
	pattern    *regexp.Regexp
	scope      string
	maxResults int
}

func pureGrep(workspace string, policy *Policy) chat.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		plan, err := planPureGrep(workspace, policy, input)
		if err != nil {
			return "", err
		}
		return executePureGrep(ctx, workspace, plan)
	}
}

func planPureGrep(workspace string, policy *Policy, input json.RawMessage) (pureGrepPlan, error) {
	if err := rejectUnknownFixtureFields(input, "pattern", "path", "ignoreCase", "maxResults"); err != nil {
		return pureGrepPlan{}, err
	}
	var request pureGrepRequest
	if err := json.Unmarshal(input, &request); err != nil {
		return pureGrepPlan{}, err
	}
	if request.Pattern == "" {
		return pureGrepPlan{}, errors.New("pattern is required")
	}
	pattern := request.Pattern
	if request.IgnoreCase {
		pattern = "(?i)" + pattern
	}
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return pureGrepPlan{}, fmt.Errorf("invalid grep pattern: %w", err)
	}

	root := workspace
	if request.Path != "" {
		root, err = resolveWorkspaceMember(workspace, request.Path)
		if err != nil {
			return pureGrepPlan{}, err
		}
	}
	if err := policy.CheckRead(root); err != nil {
		return pureGrepPlan{}, err
	}
	rootRel, err := filepath.Rel(workspace, root)
	if err != nil {
		return pureGrepPlan{}, err
	}
	return pureGrepPlan{
		pattern:    compiled,
		scope:      rootRel,
		maxResults: normalizePureGrepLimit(request.MaxResults),
	}, nil
}

func normalizePureGrepLimit(limit int) int {
	if limit <= 0 {
		return defaultPureGrepResults
	}
	if limit > maxPureGrepResults {
		return maxPureGrepResults
	}
	return limit
}

// executePureGrep owns all filesystem effects. The plan has already passed
// lexical resolution and policy authorization; os.Root and the walker preserve
// that boundary against symlink or file-type changes during traversal.
func executePureGrep(ctx context.Context, workspace string, plan pureGrepPlan) (string, error) {
	workspaceRoot, err := os.OpenRoot(workspace)
	if err != nil {
		return "", err
	}
	defer workspaceRoot.Close()

	walker := pureGrepWalker{
		ctx:        ctx,
		root:       workspaceRoot,
		pattern:    plan.pattern,
		maxResults: plan.maxResults,
	}
	if err := walker.walk(plan.scope); err != nil && !errors.Is(err, errPureGrepLimit) {
		return "", err
	}
	sort.Strings(walker.matches)
	return strings.Join(walker.matches, "\n"), nil
}

type pureGrepWalker struct {
	ctx        context.Context
	root       *os.Root
	pattern    *regexp.Regexp
	maxResults int
	matches    []string
}

func (w *pureGrepWalker) walk(path string) error {
	if err := w.stopReason(); err != nil {
		return err
	}
	info, err := w.root.Lstat(path)
	if err != nil {
		return err
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("briefcase: grep encountered symlink %q", path)
	case info.IsDir():
		return w.walkDirectory(path)
	case !info.Mode().IsRegular():
		return fmt.Errorf("briefcase: grep encountered special file %q", path)
	default:
		return w.scanFile(path)
	}
}

func (w *pureGrepWalker) walkDirectory(path string) error {
	dir, err := w.root.Open(path)
	if err != nil {
		return err
	}
	entries, readErr := dir.ReadDir(-1)
	closeErr := dir.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if err := w.ctx.Err(); err != nil {
			return err
		}
		child := entry.Name()
		if path != "." {
			child = filepath.Join(path, child)
		}
		if err := w.walk(child); err != nil {
			return err
		}
	}
	return nil
}

func (w *pureGrepWalker) scanFile(path string) error {
	file, err := w.root.Open(path)
	if err != nil {
		return err
	}
	openedInfo, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return err
	}
	if !openedInfo.Mode().IsRegular() {
		_ = file.Close()
		return fmt.Errorf("briefcase: grep path changed to a non-regular file %q", path)
	}

	scanner := bufio.NewScanner(file)
	line := 0
	for scanner.Scan() {
		if err := w.ctx.Err(); err != nil {
			_ = file.Close()
			return err
		}
		line++
		if !w.pattern.MatchString(scanner.Text()) {
			continue
		}
		w.matches = append(w.matches, fmt.Sprintf("%s:%d:%s", filepath.ToSlash(path), line, scanner.Text()))
		if len(w.matches) >= w.maxResults {
			break
		}
	}
	scanErr := scanner.Err()
	closeErr := file.Close()
	if scanErr != nil {
		return scanErr
	}
	return closeErr
}

func (w *pureGrepWalker) stopReason() error {
	if err := w.ctx.Err(); err != nil {
		return err
	}
	if len(w.matches) >= w.maxResults {
		return errPureGrepLimit
	}
	return nil
}

func pureGrepFixtureSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern":    map[string]any{"type": "string"},
			"path":       map[string]any{"type": "string"},
			"ignoreCase": map[string]any{"type": "boolean"},
			"maxResults": map[string]any{"type": "integer", "minimum": 1, "maximum": maxPureGrepResults},
		},
		"required":             []string{"pattern"},
		"additionalProperties": false,
	}
}
