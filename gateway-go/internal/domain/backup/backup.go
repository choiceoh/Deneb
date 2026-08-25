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
	"path"
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
	// network: infrastructure config snapshots that live nowhere else — the
	// CRS812 fabric switch's exported config (scripts/deploy/crs812-config-backup.sh).
	// Jumbo l2mtu, fan tuning, and the NTP client exist only on that device;
	// without a shipped copy a dead unit means rebuilding them from memory.
	"network",
	"contacts.json",
	"kv.json",
	// The recall gold sets: the ground truth every retrieval measurement is
	// scored against. They live outside the repo on purpose (business data),
	// which left them as a single unbacked copy — losing them would cost the
	// only reference the search quality bench has, and they are hand-curated
	// over months. Glob because new sets get added (meeting, facet, traffic…);
	// the ".bak-*" copies are deliberately out of pattern.
	"wiki-qa-gold*.jsonl",
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
	// shipped reports whether the named archive already exists remotely — the
	// same-day catch-up check (see Interval/Run). Injectable for tests.
	shipped func(ctx context.Context, name string) (bool, error)
	now     func() time.Time // injectable clock for date-stamped archive names
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
	if strings.ContainsAny(cfg.RemoteDir, " \t\r\n'\"`$;|&<>") {
		return nil, fmt.Errorf("backup: RemoteDir must not contain shell metacharacters: %q", cfg.RemoteDir)
	}
	// The host is passed as a single argv to ssh (no local shell), but a value
	// starting with '-' would be parsed as an ssh option (e.g. -oProxyCommand).
	if strings.HasPrefix(cfg.SSHHost, "-") || strings.ContainsAny(cfg.SSHHost, " \t\r\n'\"`$;|&<>") {
		return nil, fmt.Errorf("backup: invalid SSHHost: %q", cfg.SSHHost)
	}
	t := &Task{cfg: cfg, preSnapshot: preSnapshot, now: time.Now}
	t.ship = t.sshShip
	t.prune = t.sshPrune
	t.shipped = t.sshShipped
	return t, nil
}

// Name implements autonomous.PeriodicTask.
func (t *Task) Name() string { return "memory-backup" }

// Interval implements autonomous.PeriodicTask. Hourly RETRY cadence for one
// DAILY archive: Run no-ops once today's archive exists remotely, so the extra
// ticks cost one cheap ssh probe each.
//
// Why not a 24h interval: the scheduler stamps LastRunAt on every run, success
// or failure, so a failed run's next attempt was a full day later — and the
// archive name is date-stamped, so that day's backup was lost for good. That
// is not hypothetical: 2026-08-06 has no archive because one ssh timeout hit
// the single daily attempt. Hourly + a remote existence check turns a
// transient outage into a delay instead of a hole.
func (t *Task) Interval() time.Duration { return time.Hour }

// Run implements autonomous.PeriodicTask: snapshot → archive → ship → prune,
// skipped entirely when today's archive already shipped.
func (t *Task) Run(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()
	start := time.Now()

	name := "deneb-memory-" + t.now().Format("20060102") + ".tar.gz"

	// Same-day catch-up gate. The REMOTE is the source of truth (not an
	// in-process flag): the gateway restarts often, and a restart must not
	// re-ship an archive that already landed. A probe error means "unknown" —
	// fall through and attempt, so a probe outage can never suppress a backup.
	if t.shipped != nil {
		if done, err := t.shipped(ctx, name); err != nil {
			t.cfg.Logger.Debug("memory backup: remote probe failed; attempting anyway",
				"archive", name, "error", err)
		} else if done {
			return nil
		}
	}

	if t.preSnapshot != nil {
		t.preSnapshot(ctx)
	}

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
// good backup — but rename alone is not enough: when the GATEWAY side dies
// gracefully mid-stream (deploy hot-swap, operator restart), the remote cat
// sees a clean EOF, exits 0, and && promotes a truncated archive under the
// final name. The date-stamped name then makes the hourly catch-up probe
// report "already shipped", so the corrupt file silently stands for the whole
// day (2026-08-25: a 00:03 restart left a 584MB torso of an 1.5GB archive,
// gzip: unexpected end of file). gzip -t before the rename closes that hole:
// truncation always breaks the gzip stream, the .partial never promotes, and
// the next hourly tick re-ships. BatchMode prevents an interactive prompt
// from hanging the task.
func (t *Task) sshShip(ctx context.Context, name string, archive io.Reader) error {
	cmd := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", t.cfg.SSHHost, sshShipCommand(t.cfg.RemoteDir, name)) //nolint:gosec // G204 — host and dir are validated in NewTask; ssh is the designed transport
	cmd.Stdin = archive
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ssh ship: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// sshShipped reports whether the named archive already exists on the remote.
// A non-zero ssh exit with no transport error means "absent" (test -f failed);
// a transport failure surfaces as an error so the caller can treat it as
// unknown rather than as "absent" or "present".
func (t *Task) sshShipped(ctx context.Context, name string) (bool, error) {
	dst := t.cfg.RemoteDir + "/" + name
	// Echo a sentinel instead of trusting the exit code alone: ssh itself exits
	// non-zero on transport failure too, and conflating the two would make a
	// network outage look like "already shipped" — the exact silent-hole class
	// this gate exists to close.
	remote := fmt.Sprintf("test -f %s && echo PRESENT || echo ABSENT", dst)
	cmd := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", t.cfg.SSHHost, remote) //nolint:gosec // G204 — host and dir are validated in NewTask; ssh is the designed transport
	out, err := cmd.Output()
	answer := strings.TrimSpace(string(out))
	switch answer {
	case "PRESENT":
		return true, nil
	case "ABSENT":
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("ssh probe: %w", err)
	}
	return false, fmt.Errorf("ssh probe: unexpected answer %q", answer)
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

	for _, target := range expandTargets(stateDir, targets) {
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
				if isRegenerableCache(name) {
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

// regenerableCaches are embedding caches: derived entirely from the pages plus
// the embedder, and rebuilt automatically on the first warm after a restore.
//
// They dominate the archive without protecting anything — measured 2026-08-23,
// the wiki tarred to 117MB of which 69MB was one vector cache, while the pages
// themselves were 1.6MB. Shipping that daily across the retention window costs
// transfer and storage on the backup host for bytes a restore does not need.
// The tradeoff is explicit: after a restore the first boot re-embeds the corpus
// (minutes, search degrades to BM25 meanwhile) instead of reading a cache.
var regenerableCaches = []string{
	".semantic-cache.json",
	".semantic-cache.f32",
	".diary-semantic-cache.json",
	".diary-semantic-cache.f32",
	"semantic-index.json",
	"semantic-index.f32",
	"workfeed.semantic.json",
	"workfeed.semantic.f32",
}

// isRegenerableCache reports whether a state-dir-relative path is one of them.
func isRegenerableCache(name string) bool {
	base := path.Base(name)
	for _, c := range regenerableCaches {
		if base == c {
			return true
		}
	}
	return false
}

// expandTargets resolves glob patterns in the target list to the state-dir
// entries they match, leaving plain names untouched. A pattern that matches
// nothing simply contributes nothing — same as a missing store.
func expandTargets(stateDir string, targets []string) []string {
	out := make([]string, 0, len(targets))
	for _, target := range targets {
		if !strings.ContainsAny(target, "*?[") {
			out = append(out, target)
			continue
		}
		matches, err := filepath.Glob(filepath.Join(stateDir, target))
		if err != nil {
			continue // malformed pattern: skip rather than abort the backup
		}
		for _, m := range matches {
			rel, rerr := filepath.Rel(stateDir, m)
			if rerr != nil {
				continue
			}
			out = append(out, filepath.ToSlash(rel))
		}
	}
	return out
}

// sshShipCommand builds the remote shell line for one archive ship. Every
// stage is load-bearing: mkdir for first contact, .partial so a dead transport
// never leaves the final name, gzip -t so a clean-EOF truncation (graceful
// sender death) never promotes, mv as the atomic commit.
func sshShipCommand(remoteDir, name string) string {
	dst := remoteDir + "/" + name
	return fmt.Sprintf("mkdir -p %s && cat > %s.partial && gzip -t %s.partial && mv %s.partial %s",
		remoteDir, dst, dst, dst, dst)
}
