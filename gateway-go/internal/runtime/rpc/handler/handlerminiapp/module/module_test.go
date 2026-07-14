package module

import (
	"errors"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/nativesync"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
)

type moduleSyncStore struct{}

func (moduleSyncStore) Pull(afterSeq int64, _ int) (nativesync.PullResult, error) {
	return nativesync.PullResult{Cursor: afterSeq}, nil
}

func TestMethodsReturnsNilForEmptyDepsAndSyncPullOnlyWhenSyncConfigured(t *testing.T) {
	got, err := Methods(Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("empty dependencies = %v, want nil", got)
	}

	methods, err := Methods(Dependencies{Sync: SyncDeps{Store: moduleSyncStore{}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(methods) != 1 || methods["miniapp.sync.pull"] == nil {
		t.Fatalf("methods = %v, want only miniapp.sync.pull", methods)
	}
}

func TestOrgDashboardDepsReturnsNonNilRulesAndLanesLoaders(t *testing.T) {
	deps := OrgDashboardDeps(nil, nil)
	if deps.Rules == nil || deps.Lanes == nil {
		t.Fatalf("org dashboard loaders not wired: %+v", deps)
	}
}

func TestMergeMethodSetsReturnsTypedDuplicateError(t *testing.T) {
	handler := rpcutil.HandlerFunc(nil)
	_, err := mergeMethodSets(
		map[string]rpcutil.HandlerFunc{"miniapp.same": handler},
		map[string]rpcutil.HandlerFunc{"miniapp.same": handler},
	)
	var duplicate *DuplicateMethodError
	if !errors.As(err, &duplicate) || duplicate.Method != "miniapp.same" {
		t.Fatalf("error = %#v, want DuplicateMethodError(miniapp.same)", err)
	}
}
