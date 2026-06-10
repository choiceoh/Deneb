// memory_backup.go — wiring for the daily offsite memory backup task.
//
// The agent's entire memory (wiki, diary, transcripts, polaris, workspace,
// contacts, kv) lives on the gateway host's single disk. The cluster's
// storage node is reachable over ssh only (its NFS export is mounted
// read-only here), so the backup streams a tar.gz through ssh. The task is
// registered only when this process owns the production state dir, keeping
// dev live-test instances from shipping duplicate archives.
package server

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/backup"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
)

// defaultBackupSSHHost is this deployment's storage node (ssh alias). Override
// with DENEB_BACKUP_SSH_HOST; set DENEB_BACKUP_DISABLE=1 to turn backups off.
const defaultBackupSSHHost = "spark4tb"

// registerMemoryBackupTask wires the daily backup into the autonomous service.
func (s *Server) registerMemoryBackupTask(homeDir string) {
	if os.Getenv("DENEB_BACKUP_DISABLE") == "1" {
		s.logger.Info("memory backup disabled via DENEB_BACKUP_DISABLE")
		return
	}
	stateDir := config.ResolveStateDir()
	if homeDir == "" || stateDir != filepath.Join(homeDir, config.DefaultStateDirname) {
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
	if s.wikiStore != nil {
		store := s.wikiStore
		preSnapshot = func(ctx context.Context) {
			store.SnapshotGit(ctx, "daily backup snapshot")
		}
	}

	task, err := backup.NewTask(backup.Config{
		StateDir:      stateDir,
		SSHHost:       host,
		RemoteDir:     strings.TrimSpace(os.Getenv("DENEB_BACKUP_DIR")),
		RetentionDays: retention,
		Logger:        s.logger,
	}, preSnapshot)
	if err != nil {
		s.logger.Error("memory backup task init failed", "error", err)
		return
	}
	s.autonomousSvc.RegisterTask(task)
	s.logger.Info("memory backup task registered", "host", host, "stateDir", stateDir)
}
