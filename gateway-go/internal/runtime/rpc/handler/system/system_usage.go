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

	"github.com/choiceoh/deneb/gateway-go/internal/domain/usage"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
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
				return rpcerr.Unavailable("cannot determine home directory: " + err.Error()).Response(req.ID)
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
			return rpcerr.Unavailable("cannot open log file: " + err.Error()).Response(req.ID)
		}
		defer f.Close()

		info, err := f.Stat()
		if err != nil {
			return rpcerr.Unavailable("cannot stat log file: " + err.Error()).Response(req.ID)
		}

		fileSize := info.Size()
		var cursor int64
		reset := false

		if p.Cursor != nil {
			cursor = *p.Cursor
			// Detect log rotation: if cursor exceeds file size, reset to start.
			if cursor > fileSize {
				cursor = 0
				reset = true
			}
		}

		// Seek to cursor position.
		if cursor > 0 {
			if _, err := f.Seek(cursor, io.SeekStart); err != nil {
				return rpcerr.Unavailable("seek failed: " + err.Error()).Response(req.ID)
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
		if cursor > 0 && !reset {
			if scanner.Scan() {
				bytesRead += int64(len(scanner.Bytes())) + 1 // +1 for newline
			}
		}

		for scanner.Scan() {
			if len(lines) >= p.Limit {
				truncated = true
				break
			}
			line := scanner.Text()
			bytesRead += int64(len(line)) + 1
			lines = append(lines, line)
		}

		newCursor := cursor + bytesRead

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
