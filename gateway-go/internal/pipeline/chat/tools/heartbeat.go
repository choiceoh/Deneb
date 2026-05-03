package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// HeartbeatFileName is the filename written under <home>/.deneb/.
const HeartbeatFileName = "HEARTBEAT.md"

// HeartbeatBackupName is the 1-generation backup written before each overwrite.
// Lets the user (or agent) recover from an accidental clear by restoring this
// file. Only the most recent prior content is kept — heartbeat updates can be
// frequent and a deeper history would just add maintenance noise.
const HeartbeatBackupName = "HEARTBEAT.md.prev"

// ToolHeartbeatUpdate writes ~/.deneb/HEARTBEAT.md atomically. The path is
// fixed: the heartbeat task reads from this exact location, so a free-form
// path argument would invite mistakes (typos, wrong dir, escaping the
// home dir under fs.write's workspace clamp).
//
// Before each write the prior content is copied to HEARTBEAT.md.prev so an
// accidental clear by the autonomous heartbeat (or user) is recoverable.
//
// Used by the autonomous heartbeat loop to retire completed/cancelled items
// and update progress, breaking the "repeat the same report every 30 minutes"
// failure mode. Also usable from a normal user session ("add this to my
// heartbeat") so the user can self-manage the file without leaving Telegram.
func ToolHeartbeatUpdate() ToolFunc {
	return toolHeartbeatUpdateWithHome("")
}

// toolHeartbeatUpdateWithHome is the testable variant: when homeDir is empty
// it falls back to os.UserHomeDir() for production use; tests pass a tmpdir.
func toolHeartbeatUpdateWithHome(homeDir string) ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Content string `json:"content"`
		}
		if err := jsonutil.UnmarshalInto("heartbeat_update params", input, &p); err != nil {
			return "", err
		}

		home := homeDir
		if home == "" {
			h, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("heartbeat_update: cannot resolve home dir: %w", err)
			}
			home = h
		}
		dir := filepath.Join(home, ".deneb")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("heartbeat_update: cannot create %s: %w", dir, err)
		}
		path := filepath.Join(dir, HeartbeatFileName)
		backup := filepath.Join(dir, HeartbeatBackupName)

		// Best-effort 1-generation backup. A missing/unreadable previous file
		// is fine (first run, or someone manually deleted it); a failed
		// backup write is not — silently overwriting without backup defeats
		// the safety net, so surface the error.
		if prev, err := os.ReadFile(path); err == nil {
			if werr := atomicfile.WriteFile(backup, prev, nil); werr != nil {
				return "", fmt.Errorf("heartbeat_update: backup write failed: %w", werr)
			}
		}

		if err := atomicfile.WriteFile(path, []byte(p.Content), nil); err != nil {
			return "", fmt.Errorf("heartbeat_update: write failed: %w", err)
		}

		if p.Content == "" {
			return fmt.Sprintf("HEARTBEAT.md cleared (%s); prior content saved to %s", path, backup), nil
		}
		return fmt.Sprintf("HEARTBEAT.md updated (%d bytes) at %s", len(p.Content), path), nil
	}
}
