package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func logToolExecution(runLog *agentlog.RunLogger, turn int, tc llm.ContentBlock, block llm.ContentBlock, elapsed time.Duration, fileEffects []agentlog.ToolFileEffect) {
	if runLog == nil {
		return
	}
	td := agentlog.TurnToolData{
		Turn:        turn + 1,
		Name:        tc.Name,
		ToolUseID:   tc.ID,
		DurationMs:  elapsed.Milliseconds(),
		InputBytes:  len(tc.Input),
		InputHash:   provenanceHash(tc.Input),
		OutputLen:   len(block.Content),
		OutputHash:  provenanceHashString(block.Content),
		Targets:     toolTargetHints(tc.Input),
		FileEffects: fileEffects,
		IsError:     block.IsError,
	}
	if block.IsError {
		td.Error = block.Content
	}
	runLog.LogTurnTool(td)
}

const maxProvenanceFileBytes = 1_000_000

var mutatingToolPathKeys = map[string][]string{
	"edit":  {"file_path"},
	"write": {"file_path"},
}

type fileSnapshot struct {
	Path    string
	AbsPath string
	Exists  bool
	Hash    string
	Bytes   int64
	Lines   int
	ModTime int64
	Content []byte
	Error   string
}

type toolProvenanceRootProvider interface {
	ToolProvenanceRoot() string
}

func toolProvenanceRoot(tools ToolExecutor) string {
	if tools == nil {
		return ""
	}
	p, ok := tools.(toolProvenanceRootProvider)
	if !ok {
		return ""
	}
	return strings.TrimSpace(p.ToolProvenanceRoot())
}

func captureToolFileSnapshots(root, toolName string, input json.RawMessage) []fileSnapshot {
	paths := toolFileEffectPaths(root, toolName, input)
	if len(paths) == 0 {
		return nil
	}
	out := make([]fileSnapshot, 0, len(paths))
	for _, path := range paths {
		out = append(out, snapshotFileForProvenance(path.DisplayPath, path.AbsPath))
	}
	return out
}

type toolFileEffectPath struct {
	DisplayPath string
	AbsPath     string
}

func toolFileEffectPaths(root, toolName string, input json.RawMessage) []toolFileEffectPath {
	keys := mutatingToolPathKeys[toolName]
	root = strings.TrimSpace(root)
	if root == "" || len(keys) == 0 || len(input) == 0 {
		return nil
	}
	absRoot, err := filepath.Abs(root)
	if err != nil || absRoot == "" {
		return nil
	}
	absRoot = filepath.Clean(absRoot)
	var args map[string]json.RawMessage
	if err := json.Unmarshal(input, &args); err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []toolFileEffectPath
	for _, key := range keys {
		for _, path := range rawTargetHints(args[key]) {
			resolved := resolveSnapshotPath(absRoot, path)
			if resolved.AbsPath == "" {
				continue
			}
			if _, ok := seen[resolved.AbsPath]; ok {
				continue
			}
			seen[resolved.AbsPath] = struct{}{}
			out = append(out, resolved)
			if len(out) >= 8 {
				return out
			}
		}
	}
	return out
}

func resolveSnapshotPath(root, path string) toolFileEffectPath {
	path = strings.TrimSpace(path)
	if path == "" {
		return toolFileEffectPath{}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return toolFileEffectPath{}
	}
	absPath = filepath.Clean(absPath)
	rel, err := filepath.Rel(root, absPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return toolFileEffectPath{}
	}
	if rel == "." {
		rel = filepath.Base(root)
	}
	return toolFileEffectPath{
		DisplayPath: sanitizeTargetHint(rel),
		AbsPath:     absPath,
	}
}

func snapshotFileForProvenance(displayPath, absPath string) fileSnapshot {
	snap := fileSnapshot{Path: sanitizeTargetHint(displayPath), AbsPath: absPath}
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return snap
		}
		snap.Error = "stat: " + err.Error()
		return snap
	}
	snap.Exists = true
	snap.Bytes = info.Size()
	snap.ModTime = info.ModTime().UnixMilli()
	if !info.Mode().IsRegular() {
		snap.Error = "not a regular file"
		return snap
	}
	if info.Size() > maxProvenanceFileBytes {
		snap.Error = "file too large for provenance hash"
		return snap
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		snap.Error = "read: " + err.Error()
		return snap
	}
	snap.Content = data
	snap.Hash = provenanceHash(data)
	snap.Lines = countLines(data)
	return snap
}

func buildToolFileEffects(before, after []fileSnapshot) []agentlog.ToolFileEffect {
	if len(before) == 0 && len(after) == 0 {
		return nil
	}
	byPath := map[string]struct {
		before *fileSnapshot
		after  *fileSnapshot
	}{}
	order := make([]string, 0, len(before)+len(after))
	ensure := func(path string) {
		if _, ok := byPath[path]; !ok {
			byPath[path] = struct {
				before *fileSnapshot
				after  *fileSnapshot
			}{}
			order = append(order, path)
		}
	}
	for i := range before {
		path := before[i].Path
		ensure(path)
		entry := byPath[path]
		entry.before = &before[i]
		byPath[path] = entry
	}
	for i := range after {
		path := after[i].Path
		ensure(path)
		entry := byPath[path]
		entry.after = &after[i]
		byPath[path] = entry
	}

	out := make([]agentlog.ToolFileEffect, 0, len(order))
	for _, path := range order {
		entry := byPath[path]
		var b, a fileSnapshot
		if entry.before != nil {
			b = *entry.before
		} else {
			b.Path = path
		}
		if entry.after != nil {
			a = *entry.after
		} else {
			a.Path = path
		}
		effect := agentlog.ToolFileEffect{
			Path:         path,
			ExistsBefore: b.Exists,
			ExistsAfter:  a.Exists,
			BeforeHash:   b.Hash,
			AfterHash:    a.Hash,
			BeforeBytes:  b.Bytes,
			AfterBytes:   a.Bytes,
			BeforeLines:  b.Lines,
			AfterLines:   a.Lines,
			Changed:      fileSnapshotChanged(b, a),
			Error:        joinSnapshotErrors(b.Error, a.Error),
		}
		switch {
		case !b.Exists && a.Exists:
			effect.AddedLines = a.Lines
		case b.Exists && !a.Exists:
			effect.RemovedLines = b.Lines
		case len(b.Content) > 0 || len(a.Content) > 0:
			effect.AddedLines, effect.RemovedLines = changedLineStats(splitLines(b.Content), splitLines(a.Content))
		}
		out = append(out, effect)
	}
	return out
}

func fileSnapshotChanged(before, after fileSnapshot) bool {
	if before.Exists != after.Exists {
		return true
	}
	if before.Hash != "" || after.Hash != "" {
		return before.Hash != after.Hash
	}
	return before.Bytes != after.Bytes || before.ModTime != after.ModTime
}

func joinSnapshotErrors(before, after string) string {
	switch {
	case before != "" && after != "":
		return "before: " + before + "; after: " + after
	case before != "":
		return "before: " + before
	case after != "":
		return "after: " + after
	default:
		return ""
	}
}

func countLines(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	n := bytes.Count(data, []byte{'\n'})
	if data[len(data)-1] != '\n' {
		n++
	}
	return n
}

func splitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	lines := strings.SplitAfter(string(data), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func changedLineStats(before, after []string) (added, removed int) {
	prefix := 0
	for prefix < len(before) && prefix < len(after) && before[prefix] == after[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(before)-prefix && suffix < len(after)-prefix &&
		before[len(before)-1-suffix] == after[len(after)-1-suffix] {
		suffix++
	}
	removed = len(before) - prefix - suffix
	added = len(after) - prefix - suffix
	return added, removed
}

func provenanceHash(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func provenanceHashString(s string) string {
	if s == "" {
		return ""
	}
	return provenanceHash([]byte(s))
}

var targetHintKeys = map[string]struct{}{
	"file_path":   {},
	"path":        {},
	"workdir":     {},
	"target_path": {},
	"source_path": {},
	"dest_path":   {},
	"dst_path":    {},
	"src_path":    {},
}

func toolTargetHints(input json.RawMessage) []string {
	if len(input) == 0 {
		return nil
	}
	var args map[string]json.RawMessage
	if err := json.Unmarshal(input, &args); err != nil {
		return nil
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	seen := map[string]struct{}{}
	var out []string
	for _, key := range keys {
		if _, ok := targetHintKeys[key]; !ok {
			continue
		}
		for _, hint := range rawTargetHints(args[key]) {
			hint = sanitizeTargetHint(hint)
			if hint == "" {
				continue
			}
			if _, ok := seen[hint]; ok {
				continue
			}
			seen[hint] = struct{}{}
			out = append(out, hint)
			if len(out) >= 8 {
				return out
			}
		}
	}
	return out
}

func rawTargetHints(raw json.RawMessage) []string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return []string{s}
	}
	var ss []string
	if err := json.Unmarshal(raw, &ss); err == nil {
		return ss
	}
	return nil
}

func sanitizeTargetHint(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.Join(strings.Fields(s), " ")
	s = filepath.Clean(s)
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		cleanHome := filepath.Clean(home)
		switch {
		case s == cleanHome:
			s = "~"
		case strings.HasPrefix(s, cleanHome+string(os.PathSeparator)):
			s = "~" + strings.TrimPrefix(s, cleanHome)
		}
	}
	return truncateRunes(s, 160)
}

func truncateRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
