package serverport

import (
	"path/filepath"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
)

// ProductionStateDir reports the resolved state dir and whether this process
// owns the production state dir (homeDir/.deneb). Autonomous tasks that mutate
// or ship shared state (offsite backup, wiki research) gate on this so a
// dev/live-test instance (DENEB_STATE_DIR=/tmp/...) never touches production
// data. Shared here (rather than duplicated per composition-root package)
// since servermail, serverauto, and runtime/server all need the same check.
func ProductionStateDir(homeDir string) (string, bool) {
	stateDir := config.ResolveStateDir()
	return stateDir, homeDir != "" && stateDir == filepath.Join(homeDir, config.DefaultStateDirname)
}
