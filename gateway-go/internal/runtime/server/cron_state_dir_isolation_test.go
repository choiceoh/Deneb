package server

import (
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/cron"
)

// The cron store used to be built from $HOME, so a dev or live-test gateway
// read and WROTE the operator's real schedule. Measured 2026-08-26: a
// puppet-mode `cron add` test replaced the production morning-letter job
// (08:00 `/morning` → 22:00 with a test command) and the letter fired that
// night at 22:00.
//
// The state dir is the isolation boundary the workspace already uses (#4693).
func TestCronStoreFollowsTheStateDir(t *testing.T) {
	t.Setenv("DENEB_STATE_DIR", "/tmp/deneb-dev-state")
	dev := cron.DefaultCronStorePath(config.ResolveStateDir())
	if want := filepath.Join("/tmp/deneb-dev-state", "cron", "jobs.json"); dev != want {
		t.Fatalf("dev cron store = %q, want %q", dev, want)
	}
	if filepath.Dir(filepath.Dir(dev)) != "/tmp/deneb-dev-state" {
		t.Fatalf("dev cron store escaped the state dir: %q", dev)
	}
}

// Production must be unchanged byte-for-byte — the default state dir IS
// $HOME/.deneb, so there is nothing to migrate.
func TestCronStoreProductionPathUnchanged(t *testing.T) {
	t.Setenv("DENEB_STATE_DIR", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	got := cron.DefaultCronStorePath(config.ResolveStateDir())
	want := filepath.Join(home, ".deneb", "cron", "jobs.json")
	if got != want {
		t.Fatalf("production cron store = %q, want %q", got, want)
	}
}
