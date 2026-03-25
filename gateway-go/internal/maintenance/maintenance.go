// Package maintenance implements filesystem cleanup for sessions and logs.
//
// Cleanup thresholds:
//   - Session files older than 30 days are removed.
//   - Log files older than 14 days or total log size > 50 MB are removed (oldest first).
package maintenance

import (
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	sessionMaxAge = 30 * 24 * time.Hour
	logMaxAge     = 14 * 24 * time.Hour
	logMaxTotalMB = 50
)

// CleanedFile records a file that was (or would be) removed.
type CleanedFile struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	Reason  string `json:"reason"`
	Removed bool   `json:"removed"`
}

// Report holds the result of a maintenance run.
type Report struct {
	RanAt        string        `json:"ranAt"`
	DryRun       bool          `json:"dryRun"`
	Sessions     []CleanedFile `json:"sessions"`
	Logs         []CleanedFile `json:"logs"`
	TotalFreedMB float64       `json:"totalFreedMB"`
}

// Summary is a compact summary of the last report.
type Summary struct {
	RanAt          string  `json:"ranAt"`
	SessionsCleaned int    `json:"sessionsCleaned"`
	LogsCleaned     int    `json:"logsCleaned"`
	FreedMB         float64 `json:"freedMB"`
	DryRun          bool   `json:"dryRun"`
}

// Runner manages maintenance operations.
type Runner struct {
	mu         sync.Mutex
	denebDir   string
	lastReport *Report
}

// NewRunner creates a maintenance runner for the given deneb config directory.
func NewRunner(denebDir string) *Runner {
	return &Runner{denebDir: denebDir}
}

// Run executes maintenance cleanup. If dryRun is true, files are not actually removed.
func (r *Runner) Run(dryRun bool) *Report {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	report := &Report{
		RanAt:  now.Format(time.RFC3339),
		DryRun: dryRun,
	}

	// Clean sessions.
	sessDir := filepath.Join(r.denebDir, "sessions")
	report.Sessions = cleanOldFiles(sessDir, sessionMaxAge, now, dryRun)

	// Clean logs.
	logDir := filepath.Join(r.denebDir, "logs")
	report.Logs = cleanLogFiles(logDir, logMaxAge, logMaxTotalMB*1024*1024, now, dryRun)

	// Calculate total freed.
	var freed int64
	for _, f := range report.Sessions {
		if f.Removed || dryRun {
			freed += f.Size
		}
	}
	for _, f := range report.Logs {
		if f.Removed || dryRun {
			freed += f.Size
		}
	}
	report.TotalFreedMB = float64(freed) / (1024 * 1024)

	r.lastReport = report
	return report
}

// LastReport returns the last maintenance report, or nil if none.
func (r *Runner) LastReport() *Report {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastReport
}

// SummarizeReport creates a compact summary from a report.
func SummarizeReport(report *Report) *Summary {
	if report == nil {
		return nil
	}
	return &Summary{
		RanAt:           report.RanAt,
		SessionsCleaned: len(report.Sessions),
		LogsCleaned:     len(report.Logs),
		FreedMB:         report.TotalFreedMB,
		DryRun:          report.DryRun,
	}
}

// cleanOldFiles removes files older than maxAge from dir.
func cleanOldFiles(dir string, maxAge time.Duration, now time.Time, dryRun bool) []CleanedFile {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var cleaned []CleanedFile
	cutoff := now.Add(-maxAge)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			path := filepath.Join(dir, e.Name())
			cf := CleanedFile{
				Path:   path,
				Size:   info.Size(),
				Reason: "older than " + maxAge.String(),
			}
			if !dryRun {
				if err := os.Remove(path); err == nil {
					cf.Removed = true
				}
			}
			cleaned = append(cleaned, cf)
		}
	}
	return cleaned
}

type fileEntry struct {
	name    string
	size    int64
	modTime time.Time
}

// cleanLogFiles removes logs older than maxAge, then removes oldest logs
// until total size is under maxTotalBytes.
func cleanLogFiles(dir string, maxAge time.Duration, maxTotalBytes int64, now time.Time, dryRun bool) []CleanedFile {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var files []fileEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileEntry{name: e.Name(), size: info.Size(), modTime: info.ModTime()})
	}

	// Sort oldest first.
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.Before(files[j].modTime)
	})

	cutoff := now.Add(-maxAge)
	removed := make(map[string]bool)
	var cleaned []CleanedFile

	// Phase 1: remove files older than maxAge.
	for _, f := range files {
		if f.modTime.Before(cutoff) {
			path := filepath.Join(dir, f.name)
			cf := CleanedFile{Path: path, Size: f.size, Reason: "older than " + maxAge.String()}
			if !dryRun {
				if err := os.Remove(path); err == nil {
					cf.Removed = true
				}
			}
			cleaned = append(cleaned, cf)
			removed[f.name] = true
		}
	}

	// Phase 2: if total remaining size > maxTotalBytes, remove oldest first.
	var totalSize int64
	for _, f := range files {
		if !removed[f.name] {
			totalSize += f.size
		}
	}

	for _, f := range files {
		if totalSize <= maxTotalBytes {
			break
		}
		if removed[f.name] {
			continue
		}
		path := filepath.Join(dir, f.name)
		cf := CleanedFile{Path: path, Size: f.size, Reason: "total log size exceeds budget"}
		if !dryRun {
			if err := os.Remove(path); err == nil {
				cf.Removed = true
			}
		}
		cleaned = append(cleaned, cf)
		totalSize -= f.size
	}

	return cleaned
}
