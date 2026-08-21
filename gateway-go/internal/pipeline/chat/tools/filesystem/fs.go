package filesystem

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/artifact"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// --- Write tool ---

// ToolWrite builds the workspace file-write tool.
func ToolWrite(defaultDir string) toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		var p struct {
			FilePath string `json:"file_path"`
			Content  string `json:"content"`
		}
		if err := jsonutil.UnmarshalInto("write params", input, &p); err != nil {
			return "", err
		}
		if p.FilePath == "" {
			return "", fmt.Errorf("file_path is required")
		}

		dir := defaultDir
		path, clamped := artifact.ResolvePathContained(p.FilePath, dir)
		// A clamped path means the request pointed outside the workspace and
		// the resolver handed back the workspace ROOT. Reporting anything about
		// that directory against p.FilePath would be a statement about a path
		// the tool never touched — say what actually happened instead.
		if clamped {
			return "", fmt.Errorf("%q is outside this tool's workspace (%s) — write only reaches files under the workspace", p.FilePath, dir)
		}
		// A directory target yields a confusing "rename: is a directory" error
		// from atomicfile below; reject it up front with a clear message.
		if info, statErr := os.Stat(path); statErr == nil && info.IsDir() {
			return "", fmt.Errorf("%q is a directory, not a file — write needs a file path", p.FilePath)
		}
		if err := artifact.CheckProtectedPath(path, "write"); err != nil {
			return "", err
		}

		// Staleness check: reject if the file changed since our last read.
		if fc := toolport.FileCacheFromContext(ctx); fc != nil {
			if err := fc.CheckStaleness(path); err != nil {
				return "", err
			}
			defer fc.UpdateAfterWrite(path)
		}

		// Pre-edit checkpoint so the user can roll back this write.
		toolport.SnapshotBeforeWrite(ctx, path, "write")

		if err := ctx.Err(); err != nil {
			return "", err
		}
		if err := atomicfile.WriteFileContext(ctx, path, []byte(p.Content), nil); err != nil {
			return "", fmt.Errorf("failed to write file: %w", err)
		}
		return fmt.Sprintf("Wrote %s", p.FilePath), nil
	}
}

// --- Edit tool ---

// editParams is the edit tool's input payload.
//
// Canonical names are file_path / old_string / new_string. The Path / OldText /
// NewText / File fields accept the aliases models keep sending (path, old_text,
// new_text, file) so a schema mismatch is not a 100% error-rate tool.
type editParams struct {
	FilePath   string      `json:"file_path"`
	Path       string      `json:"path"`
	File       string      `json:"file"`
	OldString  string      `json:"old_string"`
	OldText    string      `json:"old_text"`
	NewString  string      `json:"new_string"`
	NewText    string      `json:"new_text"`
	ReplaceAll bool        `json:"replace_all"`
	Regex      bool        `json:"regex"`
	Line       int         `json:"line"`
	Anchor     string      `json:"anchor"`
	AnchorEnd  string      `json:"anchor_end"`
	Edits      []batchEdit `json:"edits"`
}

func (p *editParams) normalizeAliases() {
	if p.FilePath == "" {
		p.FilePath = p.Path
	}
	if p.FilePath == "" {
		p.FilePath = p.File
	}
	if p.OldString == "" {
		p.OldString = p.OldText
	}
	if p.NewString == "" {
		p.NewString = p.NewText
	}
}

// ToolEdit builds the workspace file-edit tool.
func ToolEdit(defaultDir string) toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		var p editParams
		if err := jsonutil.UnmarshalInto("edit params", input, &p); err != nil {
			return "", err
		}
		p.normalizeAliases()
		if p.FilePath == "" {
			return "", fmt.Errorf("file_path is required")
		}
		if p.OldString == "" && p.Anchor == "" && len(p.Edits) == 0 {
			return "", fmt.Errorf("old_string is required (or use anchor= for a content-hash anchored edit, or edits=[...] for a batch)")
		}

		dir := defaultDir
		path, clamped := artifact.ResolvePathContained(p.FilePath, dir)
		// Same honesty rule as write: a clamped request never touched
		// p.FilePath, so no verdict about the clamp target may be phrased
		// against it. The old message here claimed real skill files were
		// directories and cost the heartbeat 15 straight edit calls
		// (2026-08-02 regression alert).
		if clamped {
			return "", fmt.Errorf("%q is outside this tool's workspace (%s) — edit only reaches files under the workspace", p.FilePath, dir)
		}
		if err := artifact.CheckProtectedPath(path, "edit"); err != nil {
			return "", err
		}

		// Staleness check: reject if the file changed since our last read.
		fc := toolport.FileCacheFromContext(ctx)
		if fc != nil {
			if err := fc.CheckStaleness(path); err != nil {
				return "", err
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			// A directory target yields a confusing "is a directory" read error.
			// edit/write operate on a single file; point the model at read for
			// directory exploration. (Out-of-workspace clamps are rejected
			// above, so reaching here means a genuine directory inside the
			// workspace.)
			if info, statErr := os.Stat(path); statErr == nil && info.IsDir() {
				return "", fmt.Errorf("%q is a directory, not a file — edit targets a single file; use the read tool to list a directory", p.FilePath)
			}
			return "", fmt.Errorf("failed to read file: %w", err)
		}

		// Snapshot the current contents BEFORE any mutation path runs
		// (regex / line-target / substring). Covers every write branch below
		// — SnapshotBeforeWrite is nil-safe and dedupes by SHA-256, so a
		// no-op edit doesn't spam the index.
		toolport.SnapshotBeforeWrite(ctx, path, "edit")

		content := string(data)

		result, err := dispatchEdit(ctx, path, content, p)
		// Update cache after a successful write.
		if err == nil && fc != nil {
			fc.UpdateAfterWrite(path)
		}
		return result, err
	}
}

// dispatchEdit routes one edit invocation to its mode (batch / anchor / regex /
// line-target / plain substring). The caller updates the file cache on success.
func dispatchEdit(ctx context.Context, path, content string, p editParams) (string, error) {
	// Batch mode: N sequential replacements in ONE call — one read, one
	// atomic write, one re-prefill instead of N (the per-edit round-trip is
	// the single biggest coding-turn waste). All-or-nothing: any failing
	// edit aborts before the write, so the file is never left half-edited.
	if len(p.Edits) > 0 {
		return applyBatchEditsContext(ctx, path, p.FilePath, content, p.Edits)
	}

	// Content-hash anchored replacement (opt-in, token-efficient). The
	// model addresses a whole line — or an anchor..anchor_end range — by the
	// short hash surfaced via read(hashes=true), instead of reproducing
	// old_string. Replaces the matched line(s) wholesale with new_string.
	if p.Anchor != "" {
		return editByAnchorContext(ctx, path, p.FilePath, content, p.Anchor, p.AnchorEnd, p.NewString)
	}

	// Regex-based replacement.
	if p.Regex {
		return editWithRegexContext(ctx, path, p.FilePath, content, p.OldString, p.NewString, p.ReplaceAll)
	}

	// Line-targeted replacement.
	if p.Line > 0 {
		return editAtLineContext(ctx, path, p.FilePath, content, p.OldString, p.NewString, p.Line)
	}

	return editBySubstring(ctx, path, content, p)
}

// editBySubstring is the plain (non-regex, non-anchored) old_string →
// new_string replacement, with the whitespace-tolerant fallback.
func editBySubstring(ctx context.Context, path, content string, p editParams) (string, error) {
	count := strings.Count(content, p.OldString)
	if count == 0 {
		// Whitespace-tolerant fallback: a unique line-aligned match that
		// differs only in indentation is applied directly (with the file's
		// indentation) instead of bouncing a "not found" back to the model —
		// the mismatch is almost always tabs-vs-spaces or a copy at the
		// wrong depth, and the retry round-trip is the single biggest
		// coding-turn waste. Ambiguous or partial-line cases still fail
		// with the existing hint so the model can disambiguate.
		if !p.ReplaceAll {
			if result, handled, err := editWhitespaceTolerantContext(ctx, path, p.FilePath, content, p.OldString, p.NewString); handled {
				return result, err
			}
		}
		hint := editFuzzyHint(content, p.OldString)
		return "", fmt.Errorf("old_string not found in file%s", hint)
	}
	if count > 1 && !p.ReplaceAll {
		return "", fmt.Errorf("old_string is not unique in file (%d occurrences). Use replace_all=true to replace all, or use line= to target a specific line", count)
	}

	var newContent string
	if p.ReplaceAll {
		newContent = strings.ReplaceAll(content, p.OldString, p.NewString)
	} else {
		newContent = strings.Replace(content, p.OldString, p.NewString, 1)
	}
	if err := atomicfile.WriteFileContext(ctx, path, []byte(newContent), nil); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}
	if count > 1 {
		return fmt.Sprintf("Edited %s (%d replacements)", p.FilePath, count), nil
	}
	return fmt.Sprintf("Edited %s", p.FilePath), nil
}

// batchEdit is one entry of the edit tool's edits=[...] batch mode.
type batchEdit struct {
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

// applyBatchEditsContext applies the edits sequentially in memory (each edit
// sees the previous edits' result), then writes once. Any failure aborts
// BEFORE the write — the file is never left half-edited — and names the
// failing index so the model can fix just that entry.
func applyBatchEditsContext(ctx context.Context, path, displayPath, content string, edits []batchEdit) (string, error) {
	cur := content
	total := 0
	for i, e := range edits {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if e.OldString == "" {
			return "", fmt.Errorf("edits[%d]: old_string is required (file unchanged)", i)
		}
		count := strings.Count(cur, e.OldString)
		if count == 0 {
			hint := editFuzzyHint(cur, e.OldString)
			return "", fmt.Errorf("edits[%d]: old_string not found — batch is all-or-nothing, file unchanged%s", i, hint)
		}
		if count > 1 && !e.ReplaceAll {
			return "", fmt.Errorf("edits[%d]: old_string is not unique (%d occurrences) — set replace_all=true on that entry or disambiguate (file unchanged)", i, count)
		}
		if e.ReplaceAll {
			cur = strings.ReplaceAll(cur, e.OldString, e.NewString)
			total += count
		} else {
			cur = strings.Replace(cur, e.OldString, e.NewString, 1)
			total++
		}
	}
	if err := atomicfile.WriteFileContext(ctx, path, []byte(cur), nil); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}
	return fmt.Sprintf("Edited %s (%d edits, %d replacements)", displayPath, len(edits), total), nil
}

func editWithRegexContext(ctx context.Context, path, displayPath, content, pattern, replacement string, replaceAll bool) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex pattern: %w", err)
	}

	matches := re.FindAllStringIndex(content, -1)
	if len(matches) == 0 {
		return "", fmt.Errorf("regex pattern not found in file")
	}
	if len(matches) > 1 && !replaceAll {
		return "", fmt.Errorf("regex pattern matches %d times. Use replace_all=true to replace all", len(matches))
	}

	var newContent string
	if replaceAll {
		newContent = re.ReplaceAllString(content, replacement)
	} else {
		// Replace only the first match.
		loc := matches[0]
		newContent = content[:loc[0]] + re.ReplaceAllString(content[loc[0]:loc[1]], replacement) + content[loc[1]:]
	}

	if err := atomicfile.WriteFileContext(ctx, path, []byte(newContent), nil); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}
	return fmt.Sprintf("Edited %s (regex, %d matches)", displayPath, len(matches)), nil
}

func editAtLineContext(ctx context.Context, path, displayPath, content, oldStr, newStr string, lineNum int) (string, error) {
	lines := strings.Split(content, "\n")
	if lineNum > len(lines) {
		return "", fmt.Errorf("line %d out of range (file has %d lines)", lineNum, len(lines))
	}

	idx := lineNum - 1
	if !strings.Contains(lines[idx], oldStr) {
		return "", fmt.Errorf("old_string not found on line %d: %q", lineNum, lines[idx])
	}

	lines[idx] = strings.Replace(lines[idx], oldStr, newStr, 1)
	newContent := strings.Join(lines, "\n")

	if err := atomicfile.WriteFileContext(ctx, path, []byte(newContent), nil); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}
	return fmt.Sprintf("Edited %s (line %d)", displayPath, lineNum), nil
}

// editByAnchor replaces a whole line — or an inclusive line range
// (anchor..anchorEnd) — addressed by content-hash anchors from
// read(hashes=true) with newStr. newStr may span multiple lines. Anchors that
// match zero or multiple lines are rejected so the model can disambiguate
// (re-read for fresh anchors, or fall back to line=/old_string).
func editByAnchor(path, displayPath, content, anchor, anchorEnd, newStr string) (string, error) {
	return editByAnchorContext(context.Background(), path, displayPath, content, anchor, anchorEnd, newStr)
}

func editByAnchorContext(ctx context.Context, path, displayPath, content, anchor, anchorEnd, newStr string) (string, error) {
	lines := strings.Split(content, "\n")

	findUnique := func(target string) (int, error) {
		idx, matches := -1, 0
		for i, line := range lines {
			if lineAnchorHash(line) == target {
				matches++
				idx = i
			}
		}
		if matches == 0 {
			return -1, fmt.Errorf("anchor %q not found — re-read the file with hashes=true to get current anchors", target)
		}
		if matches > 1 {
			return -1, fmt.Errorf("anchor %q matches %d lines (identical content). Use line= to target one, or old_string with surrounding context", target, matches)
		}
		return idx, nil
	}

	startIdx, err := findUnique(anchor)
	if err != nil {
		return "", err
	}
	endIdx := startIdx
	if anchorEnd != "" {
		endIdx, err = findUnique(anchorEnd)
		if err != nil {
			return "", err
		}
		if endIdx < startIdx {
			return "", fmt.Errorf("anchor_end (line %d) is before anchor (line %d)", endIdx+1, startIdx+1)
		}
	}

	// Splice: replace lines[startIdx..endIdx] (inclusive) with newStr.
	newLines := make([]string, 0, len(lines))
	newLines = append(newLines, lines[:startIdx]...)
	newLines = append(newLines, strings.Split(newStr, "\n")...)
	newLines = append(newLines, lines[endIdx+1:]...)
	newContent := strings.Join(newLines, "\n")

	if err := atomicfile.WriteFileContext(ctx, path, []byte(newContent), nil); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}
	if endIdx > startIdx {
		return fmt.Sprintf("Edited %s (anchor lines %d-%d)", displayPath, startIdx+1, endIdx+1), nil
	}
	return fmt.Sprintf("Edited %s (anchor line %d)", displayPath, startIdx+1), nil
}

// editWhitespaceTolerant applies old_string→new_string when the file contains
// exactly one LINE-ALIGNED match that differs from old_string only in
// per-line leading/trailing whitespace. handled=false → not applicable (no
// line-aligned near-match; the caller falls through to the not-found error).
// handled=true with err → applicable but ambiguous (multiple matches), which
// is reported precisely instead of the generic not-found.
//
// Conservative by design: each old_string line must equal a file line after
// TrimSpace (internal whitespace still matters — it can be semantic inside
// string literals), a whitespace-only old_string never matches, and
// new_string is re-indented by the first-line indent delta so the block lands
// at the file's actual depth, preserving relative indentation.
func editWhitespaceTolerantContext(ctx context.Context, path, displayPath, content, oldStr, newStr string) (result string, handled bool, err error) {
	if strings.Contains(content, "\r") {
		// CR/CRLF file: the tolerant splice joins with bare \n, so the
		// replaced block (and any new lines) would silently switch line
		// endings inside a Windows-style file. Exact matching still works —
		// leave those files to it.
		return "", false, nil
	}
	oldLines := strings.Split(oldStr, "\n")
	allBlank := true
	oldTrimmed := make([]string, len(oldLines))
	for i, l := range oldLines {
		oldTrimmed[i] = strings.TrimSpace(l)
		if oldTrimmed[i] != "" {
			allBlank = false
		}
	}
	if allBlank {
		return "", false, nil // whitespace-only target — too dangerous to guess
	}

	lines := strings.Split(content, "\n")
	if len(oldLines) > len(lines) {
		return "", false, nil
	}
	var starts []int
	for i := 0; i+len(oldLines) <= len(lines); i++ {
		if err := ctx.Err(); err != nil {
			return "", true, err
		}
		ok := true
		for j := range oldLines {
			if strings.TrimSpace(lines[i+j]) != oldTrimmed[j] {
				ok = false
				break
			}
		}
		if ok {
			starts = append(starts, i)
		}
	}
	switch len(starts) {
	case 0:
		return "", false, nil
	case 1:
		// fall through to apply
	default:
		shown := starts
		if len(shown) > 3 {
			shown = shown[:3]
		}
		lineNos := make([]string, len(shown))
		for i, s := range shown {
			lineNos[i] = fmt.Sprintf("%d", s+1)
		}
		return "", true, fmt.Errorf("old_string not found exactly, and %d whitespace-tolerant matches exist (lines %s). Add surrounding context or use line= to target one", len(starts), strings.Join(lineNos, ", "))
	}

	start := starts[0]
	// Re-indent new_string so the block lands at the file's actual depth AND
	// style (tabs vs spaces). The matched window gives an exact per-level
	// translation table — old_string line j's indent maps to the file line's
	// indent — and new lines almost always reuse indent levels present in the
	// block they replace. Unseen levels fall back to translating whole
	// repetitions of the base (first line) indent, which preserves relative
	// depth without mixing whitespace styles. Blank lines pass.
	indentMap := make(map[string]string, len(oldLines))
	for j := range oldLines {
		if strings.TrimSpace(oldLines[j]) == "" {
			continue
		}
		k, v := leadingWhitespace(oldLines[j]), leadingWhitespace(lines[start+j])
		if prev, ok := indentMap[k]; ok && prev != v {
			// The same source indent maps to two different file depths (the
			// model flattened a nested block). Any replacement choice would
			// corrupt indentation-sensitive files — refuse and ask for exact
			// text instead of guessing.
			return "", true, fmt.Errorf("old_string not found exactly; a whitespace-tolerant match exists at line %d but old_string's indentation is ambiguous (identical source indents map to different file depths). Copy the exact text from read, including indentation", start+1)
		}
		indentMap[k] = v
	}
	oldIndent := leadingWhitespace(oldLines[0])
	fileIndent := leadingWhitespace(lines[start])
	newLines := strings.Split(newStr, "\n")
	reindented := false
	for i, l := range newLines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		ind := leadingWhitespace(l)
		if mapped, ok := indentMap[ind]; ok {
			if mapped != ind {
				newLines[i] = mapped + l[len(ind):]
				reindented = true
			}
			continue
		}
		if newInd, ok := translateUnseenIndent(ind, oldIndent, fileIndent); ok {
			newLines[i] = newInd + l[len(ind):]
			reindented = true
		}
	}

	spliced := append([]string{}, lines[:start]...)
	spliced = append(spliced, newLines...)
	spliced = append(spliced, lines[start+len(oldLines):]...)
	if err := atomicfile.WriteFileContext(ctx, path, []byte(strings.Join(spliced, "\n")), nil); err != nil {
		return "", true, fmt.Errorf("failed to write file: %w", err)
	}
	note := ""
	if reindented {
		note = ", indentation adapted"
	}
	return fmt.Sprintf("Edited %s (whitespace-tolerant match at line %d%s)", displayPath, start+1, note), true, nil
}

// leadingWhitespace returns the run of spaces/tabs at the start of s.
func leadingWhitespace(s string) string {
	for i, r := range s {
		if r != ' ' && r != '\t' {
			return s[:i]
		}
	}
	return s
}

// translateUnseenIndent maps a new_string indent level absent from the
// matched block's translation table: whole repetitions of the old base
// indent become repetitions of the file base indent (a deeper level in
// new_string keeps its relative depth in the file's style), and any
// remainder must not mix whitespace styles with the translated prefix —
// concatenating a tab-file base with a space remainder is exactly the mixed
// indentation that breaks Python/YAML, so mixing returns ok=false and the
// line is left untouched.
func translateUnseenIndent(ind, oldIndent, fileIndent string) (string, bool) {
	if oldIndent == fileIndent {
		return "", false // no depth shift — nothing to translate
	}
	if oldIndent == "" {
		// Flush-left old block: deepen by the file's base indent.
		if mixesWhitespaceStyles(fileIndent, ind) {
			return "", false
		}
		return fileIndent + ind, true
	}
	n := 0
	rest := ind
	for strings.HasPrefix(rest, oldIndent) {
		n++
		rest = rest[len(oldIndent):]
	}
	if n == 0 {
		return "", false // shallower than the base — leave as written
	}
	if mixesWhitespaceStyles(fileIndent, rest) {
		return "", false
	}
	return strings.Repeat(fileIndent, n) + rest, true
}

// mixesWhitespaceStyles reports whether joining the two indent fragments
// would put tabs and spaces in one indentation run.
func mixesWhitespaceStyles(a, b string) bool {
	tabs := strings.Contains(a, "\t") || strings.Contains(b, "\t")
	spaces := strings.Contains(a, " ") || strings.Contains(b, " ")
	return tabs && spaces
}

// editFuzzyHint provides a hint when old_string is not found.
func editFuzzyHint(content, oldStr string) string {
	// Check if it's a whitespace issue.
	normalized := strings.Join(strings.Fields(oldStr), " ")
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		normalizedLine := strings.Join(strings.Fields(line), " ")
		if strings.Contains(normalizedLine, normalized) {
			return fmt.Sprintf(". Possible whitespace mismatch on line %d", i+1)
		}
	}

	// Check first line of old_string for partial match.
	firstLine := strings.Split(oldStr, "\n")[0]
	if firstLine != "" {
		for i, line := range lines {
			if strings.Contains(line, strings.TrimSpace(firstLine)) {
				return fmt.Sprintf(". Similar text found on line %d — check for whitespace or trailing characters", i+1)
			}
		}
	}

	return ""
}

// --- Grep tool ---
