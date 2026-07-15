// memory_backup.go — wiring for the daily offsite memory backup task.
//
// The agent's entire memory (wiki, diary, transcripts, polaris, workspace,
// contacts, kv) lives on the gateway host's single disk. The cluster's
// storage node is reachable over ssh only (its NFS export is mounted
// read-only here), so the backup streams a tar.gz through ssh. The task is
// registered only when this process owns the production state dir, keeping
// dev live-test instances from shipping duplicate archives.
package servermail

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/backup"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/serverport"
)

// defaultBackupSSHHost is this deployment's storage node — the Tailscale machine
// name "srv2" (MagicDNS-resolvable). Override with DENEB_BACKUP_SSH_HOST; set
// DENEB_BACKUP_DISABLE=1 to turn backups off. (Previously "spark4tb", which is
// not a resolvable Tailscale name, so every backup logged "could not resolve
// hostname spark4tb".)
const defaultBackupSSHHost = "srv2"

// RegisterMemoryBackupTask wires the daily backup into the autonomous service.
func (m *Manager) RegisterMemoryBackupTask(homeDir string) {
	if os.Getenv("DENEB_BACKUP_DISABLE") == "1" {
		m.Host.Logger().Info("memory backup disabled via DENEB_BACKUP_DISABLE")
		return
	}
	stateDir, ok := serverport.ProductionStateDir(homeDir)
	if !ok {
		// Non-production state dir (dev live-test) — never ship backups.
		return
	}

	host := strings.TrimSpace(os.Getenv("DENEB_BACKUP_SSH_HOST"))
	if host == "" {
		host = defaultBackupSSHHost
	}
	retention := 0
	if v := os.Getenv("DENEB_BACKUP_RETENTION_DAYS"); v != "" {
		if d, err := strconv.Atoi(v); err == nil {
			retention = d
		}
	}

	// Pre-snapshot: commit the wiki git history so the archive carries it.
	var preSnapshot func(context.Context)
	if m.WikiStore != nil {
		store := m.WikiStore
		preSnapshot = func(ctx context.Context) {
			store.SnapshotGit(ctx, "daily backup snapshot")
		}
	}

	task, err := backup.NewTask(backup.Config{
		StateDir:      stateDir,
		SSHHost:       host,
		RemoteDir:     strings.TrimSpace(os.Getenv("DENEB_BACKUP_DIR")),
		RetentionDays: retention,
		Logger:        m.Host.Logger(),
	}, preSnapshot)
	if err != nil {
		m.Host.Logger().Error("memory backup task init failed", "error", err)
		return
	}
	m.Host.AutonomousSvc().RegisterTask(task)
	m.Host.Logger().Info("memory backup task registered", "host", host, "stateDir", stateDir)
}
