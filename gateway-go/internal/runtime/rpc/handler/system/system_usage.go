// system_usage.go — usage.* and logs.* RPC handlers.
package system

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/usage"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// UsageDeps holds dependencies for usage RPC methods.
type UsageDeps struct {
	Tracker *usage.Tracker
}

// UsageMethods returns the usage.status and usage.cost handlers.
func UsageMethods(deps UsageDeps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"usage.status": usageStatus(deps),
		"usage.cost":   usageCost(deps),
	}
}

func usageStatus(deps UsageDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.Tracker == nil {
			resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
				"uptime":    "0s",
				"providers": map[string]any{},
			})
			return resp
		}
		resp, _ := protocol.NewResponseOK(req.ID, deps.Tracker.Status())
		return resp
	}
}

func usageCost(deps UsageDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.Tracker == nil {
			resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
				"totalCalls": 0,
				"providers":  map[string]any{},
			})
			return resp
		}
		resp, _ := protocol.NewResponseOK(req.ID, deps.Tracker.Cost())
		return resp
	}
}

// LogsDeps holds dependencies for log-related RPC methods.
type LogsDeps struct {
	// LogDir is the directory containing rolling log files.
	// Defaults to ~/.deneb/logs/ if empty.
	LogDir string
}

// LogsMethods returns the logs.tail handler.
func LogsMethods(deps LogsDeps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"logs.tail": logsTail(deps),
	}
}

const (
	defaultLogLimit = 500
	maxLogLimit     = 5000
	defaultMaxBytes = 250 * 1024  // 250 KB
	maxMaxBytes     = 1024 * 1024 // 1 MB
)

func logsTail(deps LogsDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Cursor   *int64 `json:"cursor"`
			Limit    int    `json:"limit"`
			MaxBytes int    `json:"maxBytes"`
		}
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &p)
		}
		if p.Limit <= 0 || p.Limit > maxLogLimit {
			p.Limit = defaultLogLimit
		}
		if p.MaxBytes <= 0 || p.MaxBytes > maxMaxBytes {
			p.MaxBytes = defaultMaxBytes
		}

		logDir := deps.LogDir
		if logDir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return rpcerr.WrapUnavailable("cannot determine home directory", err).Response(req.ID)
			}
			logDir = filepath.Join(home, ".deneb", "logs")
		}

		// Find the most recent log file.
		logFile, err := findLatestLogFile(logDir)
		if err != nil {
			return rpcerr.Newf(protocol.ErrNotFound, "no log files found: %v", err).Response(req.ID)
		}

		f, err := os.Open(logFile)
		if err != nil {
			return rpcerr.WrapUnavailable("cannot open log file", err).Response(req.ID)
		}
		defer f.Close()

		info, err := f.Stat()
		if err != nil {
			return rpcerr.WrapUnavailable("cannot stat log file", err).Response(req.ID)
		}

		fileSize := info.Size()
		var cursor int64
		reset := false
		skipPartialLine := false

		if p.Cursor != nil {
			cursor = *p.Cursor
			// Detect log rotation or an invalid external cursor and reset to start.
			if cursor < 0 || cursor > fileSize {
				cursor = 0
				reset = true
			}
		}

		// Seek to cursor position.
		if cursor > 0 {
			// Cursors returned by this handler point immediately after a complete
			// newline and must resume at the next line. Only discard a prefix when
			// an external caller supplied a cursor in the middle of a line.
			var previous [1]byte
			if _, err := f.ReadAt(previous[:], cursor-1); err == nil {
				skipPartialLine = previous[0] != '\n'
			}
			if _, err := f.Seek(cursor, io.SeekStart); err != nil {
				return rpcerr.WrapUnavailable("seek failed", err).Response(req.ID)
			}
		}

		// Read up to maxBytes.
		reader := io.LimitReader(f, int64(p.MaxBytes))
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

		var lines []string
		truncated := false
		bytesRead := int64(0)

		// If resuming mid-file, skip first partial line.
		if skipPartialLine && !reset {
			if scanner.Scan() {
				bytesRead += scannedLineWidth(f, cursor+bytesRead, fileSize, scanner.Bytes())
			}
		}

		for scanner.Scan() {
			if len(lines) >= p.Limit {
				truncated = true
				break
			}
			line := scanner.Text()
			bytesRead += scannedLineWidth(f, cursor+bytesRead, fileSize, scanner.Bytes())
			lines = append(lines, line)
		}

		newCursor := cursor + bytesRead
		if newCursor < fileSize {
			truncated = true
		}

		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"cursor":    newCursor,
			"size":      fileSize,
			"lines":     lines,
			"truncated": truncated,
			"reset":     reset,
			"file":      filepath.Base(logFile),
		})
		return resp
	}
}

// scannedLineWidth returns the source bytes consumed for a ScanLines token,
// including LF or CRLF delimiters stripped by the scanner. When a byte limit
// ends mid-line, no delimiter is added, so the returned cursor never jumps
// past unread log bytes.
func scannedLineWidth(file *os.File, offset, fileSize int64, token []byte) int64 {
	width := int64(len(token))
	separatorAt := offset + width
	if separatorAt >= fileSize {
		return width
	}
	var separator [2]byte
	n, _ := file.ReadAt(separator[:], separatorAt)
	if n == 0 {
		return width
	}
	switch separator[0] {
	case '\n':
		return width + 1
	case '\r':
		if n > 1 && separator[1] == '\n' {
			return width + 2
		}
		// ScanLines drops a terminal carriage return even without LF.
		return width + 1
	default:
		return width
	}
}

// findLatestLogFile returns the most recently modified log file in the directory.
func findLatestLogFile(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	var logFiles []os.DirEntry
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".log") {
			logFiles = append(logFiles, e)
		}
	}
	if len(logFiles) == 0 {
		return "", os.ErrNotExist
	}

	// Sort by name descending (deneb-YYYY-MM-DD.log sorts chronologically).
	sort.Slice(logFiles, func(i, j int) bool {
		return logFiles[i].Name() > logFiles[j].Name()
	})

	return filepath.Join(dir, logFiles[0].Name()), nil
}
