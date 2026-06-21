// Package backup ships a daily archive of the agent's memory stores to a
// remote host.
//
// Every store the agent's memory depends on (wiki, diary, transcripts,
// polaris, workspace context files, contacts, kv) lives on a single NVMe in
// the gateway host. fsync and atomic writes protect against crashes, but a
// disk failure would erase the agent's entire accumulated memory. The cluster
// has a dedicated storage node reachable over ssh (the NFS mount is read-only
// from the gateway host, so scp/ssh is the only write path).
//
// The archive is built with Go's tar/gzip and streamed straight into
// `ssh <host> "cat > file"` — no local staging copy, no extra disk usage.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DefaultTargets are the state-dir entries that constitute the agent's
// memory. Missing entries are skipped (not every deployment has all stores).
var DefaultTargets = []string{
	"wiki",
	"knowledge", // topic/org knowledge docs (org charts, etc.) — not git-tracked like wiki
	"memory",
	"transcripts",
	"polaris",
	"workspace",
	"contacts.json",
	"kv.json",
}

// runTimeout bounds one backup cycle end to end (archive + ship + prune).
const runTimeout = 15 * time.Minute

// Config configures the backup task. Zero-value fields fall back to the
// documented defaults at construction time.
type Config struct {
	StateDir      string // source root (the production ~/.deneb)
	SSHHost       string // ssh destination (alias or host); required
	RemoteDir     string // remote directory, relative to the remote $HOME
	RetentionDays int    // prune remote archives older than this
	Logger        *slog.Logger
}

// shipFunc streams a finished archive somewhere. Injectable for tests.
type shipFunc func(ctx context.Context, name string, archive io.Reader) error

// Task implements autonomous.PeriodicTask: one memory archive per day.
type Task struct {
	cfg         Config
	preSnapshot func(context.Context) // optional hook (wiki git snapshot)
	ship        shipFunc
	prune       func(context.Context) error
}

// NewTask builds the daily backup task. preSnapshot (optional) runs before
// archiving — used to commit a wiki git snapshot so the archive carries the
// full version history.
func NewTask(cfg Config, preSnapshot func(context.Context)) (*Task, error) {
	if strings.TrimSpace(cfg.StateDir) == "" {
		return nil, fmt.Errorf("backup: StateDir required")
	}
	if strings.TrimSpace(cfg.SSHHost) == "" {
		return nil, fmt.Errorf("backup: SSHHost required")
	}
	if cfg.RemoteDir == "" {
		cfg.RemoteDir = "deneb-backups"
	}
	if cfg.RetentionDays <= 0 {
		cfg.RetentionDays = 30
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	// The remote dir and archive name are interpolated into a remote shell
	// command line; refuse anything that needs quoting rather than escape it.
	if strings.ContainsAny(cfg.RemoteDir, " '\"`$;|&<>") {
		return nil, fmt.Errorf("backup: RemoteDir must not contain shell metacharacters: %q", cfg.RemoteDir)
	}
	// The host is passed as a single argv to ssh (no local shell), but a value
	// starting with '-' would be parsed as an ssh option (e.g. -oProxyCommand).
	if strings.HasPrefix(cfg.SSHHost, "-") || strings.ContainsAny(cfg.SSHHost, " '\"`$;|&<>") {
		return nil, fmt.Errorf("backup: invalid SSHHost: %q", cfg.SSHHost)
	}
	t := &Task{cfg: cfg, preSnapshot: preSnapshot}
	t.ship = t.sshShip
	t.prune = t.sshPrune
	return t, nil
}

// Name implements autonomous.PeriodicTask.
func (t *Task) Name() string { return "memory-backup" }

// Interval implements autonomous.PeriodicTask. Daily: memory changes are
// incremental and the wiki git history inside the archive covers intra-day
// granularity.
func (t *Task) Interval() time.Duration { return 24 * time.Hour }

// Run implements autonomous.PeriodicTask: snapshot → archive → ship → prune.
func (t *Task) Run(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()
	start := time.Now()

	if t.preSnapshot != nil {
		t.preSnapshot(ctx)
	}

	name := "deneb-memory-" + time.Now().Format("20060102") + ".tar.gz"

	pr, pw := io.Pipe()
	archiveErr := make(chan error, 1)
	go func() {
		err := writeArchive(pw, t.cfg.StateDir, DefaultTargets)
		pw.CloseWithError(err)
		archiveErr <- err
	}()

	shipErr := t.ship(ctx, name, pr)
	// Drain the pipe so the archive goroutine always finishes.
	pr.CloseWithError(shipErr)
	werr := <-archiveErr

	if shipErr != nil || werr != nil {
		err := shipErr
		if err == nil {
			err = werr
		}
		// A dead backup is an operator-must-know event: if this stays silent
		// the next disk failure erases the agent's entire memory.
		t.cfg.Logger.Error("memory backup failed", "archive", name, "host", t.cfg.SSHHost, "error", err)
		return err
	}

	if err := t.prune(ctx); err != nil {
		t.cfg.Logger.Warn("memory backup: retention prune failed", "error", err)
	}

	t.cfg.Logger.Info("memory backup shipped",
		"archive", name, "host", t.cfg.SSHHost, "remoteDir", t.cfg.RemoteDir,
		"elapsed", time.Since(start).Round(time.Second).String())
	return nil
}

// sshShip streams the archive into `ssh host "cat > dir/name.partial && mv"`.
// The temp-then-rename keeps a half-shipped archive from masquerading as a
// good backup. BatchMode prevents an interactive prompt from hanging the task.
func (t *Task) sshShip(ctx context.Context, name string, archive io.Reader) error {
	dst := t.cfg.RemoteDir + "/" + name
	remote := fmt.Sprintf("mkdir -p %s && cat > %s.partial && mv %s.partial %s",
		t.cfg.RemoteDir, dst, dst, dst)
	cmd := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", t.cfg.SSHHost, remote) //nolint:gosec // G204 — host and dir are validated in NewTask; ssh is the designed transport
	cmd.Stdin = archive
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ssh ship: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// sshPrune deletes remote archives older than the retention window.
func (t *Task) sshPrune(ctx context.Context) error {
	remote := fmt.Sprintf("find %s -maxdepth 1 -name 'deneb-memory-*.tar.gz' -mtime +%d -delete",
		t.cfg.RemoteDir, t.cfg.RetentionDays)
	cmd := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", t.cfg.SSHHost, remote) //nolint:gosec // G204 — host and dir are validated in NewTask; ssh is the designed transport
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ssh prune: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// writeArchive tars the given state-dir entries (gzip-compressed) into w.
// Missing entries are skipped; temp/lock artifacts are excluded; only regular
// files and directories are stored.
func writeArchive(w io.Writer, stateDir string, targets []string) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)

	for _, target := range targets {
		root := filepath.Join(stateDir, target)
		info, err := os.Lstat(root)
		if err != nil {
			continue // store not present in this deployment
		}
		if !info.IsDir() {
			if info.Mode().IsRegular() {
				if err := addFile(tw, root, target, info); err != nil {
					return err
				}
			}
			continue
		}
		walkErr := filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
			if err != nil {
				return nil //nolint:nilerr // skip unreadable entries; backup the rest
			}
			rel, rerr := filepath.Rel(stateDir, path)
			if rerr != nil {
				return nil //nolint:nilerr // outside stateDir; cannot happen in practice
			}
			name := filepath.ToSlash(rel)
			switch {
			case fi.IsDir():
				hdr := &tar.Header{Name: name + "/", Mode: 0o755, Typeflag: tar.TypeDir, ModTime: fi.ModTime()}
				return tw.WriteHeader(hdr)
			case fi.Mode().IsRegular():
				if strings.HasSuffix(name, ".tmp") || strings.HasSuffix(name, ".lock") || strings.HasSuffix(name, ".partial") {
					return nil
				}
				return addFile(tw, path, name, fi)
			default:
				return nil // sockets, symlinks, devices: not memory
			}
		})
		if walkErr != nil {
			return walkErr
		}
	}

	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func addFile(tw *tar.Writer, path, name string, fi os.FileInfo) error {
	f, err := os.Open(path)
	if err != nil {
		return nil //nolint:nilerr // file vanished mid-walk; skip
	}
	defer f.Close()
	hdr := &tar.Header{
		Name:    name,
		Mode:    int64(fi.Mode().Perm()),
		Size:    fi.Size(),
		ModTime: fi.ModTime(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	// A file appended to mid-copy (transcripts) would corrupt the tar stream:
	// copy exactly the size recorded in the header.
	_, err = io.CopyN(tw, f, fi.Size())
	if errors.Is(err, io.EOF) {
		return fmt.Errorf("backup: %s truncated while archiving", name)
	}
	return err
}
