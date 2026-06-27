// memory_curation.go — dreaming-driven curation of the workspace MEMORY.md.
//
// MEMORY.md is the agent's auto-recorded learnings file. It only ever grows
// (176KB observed in production), while the system prompt loads at most a
// fixed budget of it — so the oldest learnings silently rot outside the
// window. The durable home for distilled knowledge is the wiki.
//
// This module closes the loop: each dream cycle feeds the *unconsumed*
// timestamped sections of MEMORY.md into the same LLM synthesis that distills
// diaries into wiki pages, then rewrites MEMORY.md keeping (a) the curated
// category sections at the head verbatim and (b) recent/unconsumed
// timestamped sections. MEMORY.md becomes a bounded hot buffer; the wiki is
// the long-term store.
//
// Safety properties:
//   - Only timestamped ("## YYYY-MM-DD …") sections are ever touched; the
//     category sections (결정사항/선호도/…) are preserved byte-for-byte.
//   - A section is dropped only when it was consumed by synthesis AND is
//     older than memoryKeepDays.
//   - The previous content is saved to MEMORY.md.bak and the rewrite is
//     atomic (tmp+rename).
//   - Optimistic concurrency: if the file changed between scan and rewrite
//     (the recorder appended mid-cycle), the rewrite is skipped this cycle.
package wiki

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	memoryFileName = "MEMORY.md"
	// memoryScanMaxBytes caps how much MEMORY.md content one dream cycle
	// feeds to synthesis (alongside the 30KB diary budget). Large backlogs
	// drain over successive cycles.
	memoryScanMaxBytes = 15_000
	// memoryKeepDays: timestamped sections younger than this are always kept
	// in MEMORY.md, consumed or not — they are the "hot" working memory.
	memoryKeepDays = 14
	// memoryDiskMaxBytes is the HARD on-disk ceiling for MEMORY.md, enforced
	// independently of the dream cycle. curateWorkspaceMemory only runs when a
	// dream cycle consumed sections, so a disabled or lagging dreamer let the
	// file grow unbounded (176KB observed) even though the prompt reads at most
	// maxMemoryFileChars (32KB) of it. This cap is double the read budget so a
	// healthy file is never touched, but a runaway one is bounded regardless of
	// dreaming health. The prompt loader reads head+tail, so enforcement keeps
	// the preamble + category sections (head) and the newest timestamped
	// sections (tail), dropping the oldest timestamped sections in between.
	memoryDiskMaxBytes = 64_000
)

// memoryStampRe matches timestamped section headers like
// "## 2026-04-07 00:39" or "## 2026-04-07". The fixed format makes stamps
// lexicographically comparable.
var memoryStampRe = regexp.MustCompile(`^## (\d{4}-\d{2}-\d{2}(?: \d{2}:\d{2})?)\s*$`)

// memorySection is one "## …" block of MEMORY.md, kept raw so reassembly is
// byte-faithful.
type memorySection struct {
	raw   string // header line + body, exactly as read
	stamp string // "YYYY-MM-DD[ HH:MM]" for timestamped sections, "" otherwise
}

// parseMemorySections splits content into the preamble (everything before the
// first section header) and the ordered section list.
func parseMemorySections(content string) (string, []memorySection) {
	lines := strings.SplitAfter(content, "\n")
	var preamble strings.Builder
	var sections []memorySection
	var cur *memorySection

	flush := func() {
		if cur != nil {
			sections = append(sections, *cur)
			cur = nil
		}
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			flush()
			stamp := ""
			if m := memoryStampRe.FindStringSubmatch(strings.TrimRight(line, "\n")); m != nil {
				stamp = m[1]
			}
			cur = &memorySection{raw: line, stamp: stamp}
			continue
		}
		if cur != nil {
			cur.raw += line
		} else {
			preamble.WriteString(line)
		}
	}
	flush()
	return preamble.String(), sections
}

// memoryScanResult carries one cycle's MEMORY.md contribution to synthesis.
type memoryScanResult struct {
	Content         string // labeled chunk appended to the synthesis input
	ConsumedThrough string // new high-water stamp once the cycle completes
	Sections        int
	fileSize        int64
	fileMod         time.Time
}

// scanWorkspaceMemory collects timestamped sections newer than
// consumedThrough, oldest first, up to memoryScanMaxBytes. Returns nil when
// there is nothing to consume (no workspace, no file, no new sections).
func (wd *WikiDreamer) scanWorkspaceMemory(consumedThrough string) *memoryScanResult {
	if wd.workspaceDir == "" {
		return nil
	}
	path := filepath.Join(wd.workspaceDir, memoryFileName)
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	_, sections := parseMemorySections(string(data))

	var sb strings.Builder
	high := consumedThrough
	count := 0
	for _, sec := range sections {
		if sec.stamp == "" || sec.stamp <= consumedThrough {
			continue
		}
		if sb.Len()+len(sec.raw) > memoryScanMaxBytes && count > 0 {
			break // budget reached; the rest drains next cycle
		}
		sb.WriteString(sec.raw)
		if sec.stamp > high {
			high = sec.stamp
		}
		count++
		if sb.Len() >= memoryScanMaxBytes {
			break
		}
	}
	if count == 0 {
		return nil
	}
	return &memoryScanResult{
		Content: "\n\n=== 워크스페이스 MEMORY.md 자동기록 (다이어리와 동일하게 위키 페이지로 증류할 것) ===\n" +
			sb.String(),
		ConsumedThrough: high,
		Sections:        count,
		fileSize:        info.Size(),
		fileMod:         info.ModTime(),
	}
}

// curateWorkspaceMemory rewrites MEMORY.md after a successful cycle: sections
// consumed by synthesis (stamp <= consumedThrough) AND older than
// memoryKeepDays are dropped — their substance now lives in the wiki. Returns
// the number of dropped sections.
func (wd *WikiDreamer) curateWorkspaceMemory(scan *memoryScanResult) (int, error) {
	if wd.workspaceDir == "" || scan == nil {
		return 0, nil
	}
	path := filepath.Join(wd.workspaceDir, memoryFileName)
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	// Optimistic concurrency: the recorder may have appended during the
	// (minutes-long) dream cycle. A rewrite from stale bytes would silently
	// drop that append — skip and curate next cycle instead.
	if info.Size() != scan.fileSize || !info.ModTime().Equal(scan.fileMod) {
		slog.Info("memory-curation: MEMORY.md changed during the cycle; rewrite skipped")
		return 0, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	preamble, sections := parseMemorySections(string(data))

	cutoff := time.Now().AddDate(0, 0, -memoryKeepDays).Format("2006-01-02 15:04")
	var out strings.Builder
	out.WriteString(preamble)
	dropped := 0
	for _, sec := range sections {
		consumed := sec.stamp != "" && sec.stamp <= scan.ConsumedThrough
		old := sec.stamp != "" && sec.stamp < cutoff
		if consumed && old {
			dropped++
			continue
		}
		out.WriteString(sec.raw)
	}
	if dropped == 0 {
		return 0, nil
	}

	// Keep the previous full content recoverable, then rewrite atomically.
	if err := os.WriteFile(path+".bak", data, 0o644); err != nil { //nolint:gosec // G703 — path is workspaceDir/MEMORY.md, wired at startup, not user input
		return 0, fmt.Errorf("memory-curation: backup write: %w", err)
	}
	tmp := path + ".curate.tmp"
	if err := writeFileSync(tmp, []byte(out.String()), 0o644); err != nil {
		return 0, fmt.Errorf("memory-curation: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return 0, fmt.Errorf("memory-curation: rename: %w", err)
	}
	slog.Info("memory-curation: MEMORY.md rewritten",
		"droppedSections", dropped, "beforeBytes", len(data), "afterBytes", out.Len())
	return dropped, nil
}

// enforceMemoryDiskCap bounds MEMORY.md on disk to memoryDiskMaxBytes,
// independently of the dream cycle. It is a safety net for when dreaming is
// disabled or lagging: curateWorkspaceMemory only fires after synthesis
// consumes sections, so without this the file grows without bound.
//
// Unlike curation (which drops only consumed+aged sections), this is a pure
// size guard: the preamble and all non-timestamped category sections are kept
// verbatim (they are the curated head the prompt loader reads), and the OLDEST
// timestamped sections are dropped — oldest first, in file order — until the
// result fits the cap. The newest timestamped sections (the tail the loader
// also reads) are preserved. Dropped content is appended to MEMORY.md.bak so it
// stays recoverable, and the rewrite is atomic (tmp+rename via writeFileSync).
//
// Returns the number of timestamped sections dropped. A no-op (file absent,
// under cap, or nothing droppable) returns 0.
func (wd *WikiDreamer) enforceMemoryDiskCap() (int, error) {
	if wd.workspaceDir == "" {
		return 0, nil
	}
	path := filepath.Join(wd.workspaceDir, memoryFileName)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if info.Size() <= memoryDiskMaxBytes {
		return 0, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	if len(data) <= memoryDiskMaxBytes {
		return 0, nil
	}
	preamble, sections := parseMemorySections(string(data))

	// Fixed cost: preamble + every non-timestamped (category) section is always
	// kept. Timestamped sections are the droppable budget.
	fixed := len(preamble)
	for _, sec := range sections {
		if sec.stamp == "" {
			fixed += len(sec.raw)
		}
	}

	// Walk timestamped sections oldest-first (file order is chronological),
	// dropping until the projected total fits the cap. Stop as soon as it fits
	// so the maximum number of newest sections survives.
	total := len(data)
	drop := make(map[int]bool)
	dropped := 0
	var droppedRaw strings.Builder
	for i, sec := range sections {
		if total <= memoryDiskMaxBytes {
			break
		}
		if sec.stamp == "" {
			continue // never drop category sections
		}
		drop[i] = true
		droppedRaw.WriteString(sec.raw)
		total -= len(sec.raw)
		dropped++
	}
	if dropped == 0 {
		// Over the cap but nothing droppable (all content is category sections
		// or the timestamped tail alone already exceeds the cap). Leave the file
		// intact rather than corrupt the curated head; surface it for the operator.
		slog.Warn("memory-curation: MEMORY.md over disk cap but no droppable sections",
			"sizeBytes", len(data), "capBytes", memoryDiskMaxBytes, "fixedBytes", fixed)
		return 0, nil
	}

	var out strings.Builder
	out.WriteString(preamble)
	for i, sec := range sections {
		if drop[i] {
			continue
		}
		out.WriteString(sec.raw)
	}

	// Append the dropped sections to the backup so nothing is lost on disk. Use
	// O_APPEND because curateWorkspaceMemory may also write MEMORY.md.bak; both
	// are recovery aids, and appending keeps the older rotated content too.
	if bak, ferr := os.OpenFile(path+".bak", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); ferr == nil { //nolint:gosec // G304/G302 — path is workspaceDir/MEMORY.md.bak, wired at startup
		_, _ = bak.WriteString(droppedRaw.String())
		_ = bak.Close()
	} else {
		// Backup is best-effort, but if we cannot preserve the dropped content
		// at all, do not destroy it — abort the rewrite.
		return 0, fmt.Errorf("memory-curation: disk-cap backup write: %w", ferr)
	}

	tmp := path + ".diskcap.tmp"
	if err := writeFileSync(tmp, []byte(out.String()), 0o644); err != nil {
		return 0, fmt.Errorf("memory-curation: disk-cap write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return 0, fmt.Errorf("memory-curation: disk-cap rename: %w", err)
	}
	slog.Info("memory-curation: MEMORY.md disk-capped",
		"droppedSections", dropped, "beforeBytes", len(data), "afterBytes", out.Len(), "capBytes", memoryDiskMaxBytes)
	return dropped, nil
}
