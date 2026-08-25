package genesis

import (
	"strings"
	"testing"
)

// The two live mistakes, both a real name from a NEIGHBOURING vocabulary used
// as a bridge attribute — prose cannot tell those apart, which is why nothing
// caught them.
func TestBridgeSurfacePreflightRejectsNewBadCalls(t *testing.T) {
	ok, reason := bridgeSurfacePreflight(
		"원본: deneb.mail_archive(action=\"search\") 로 조회한다.",
		"개선: deneb.deal_ledger() 와 deneb.project_history() 로 조회한다.",
	)
	if ok {
		t.Fatal("candidate introducing unknown bridge calls must be rejected")
	}
	for _, want := range []string{"deneb.deals()", `deneb.mail_archive(action="project_history")`} {
		if !strings.Contains(reason, want) {
			t.Errorf("reason should point at the right call %q: %s", want, reason)
		}
	}
}

// Real surface passes — the guard must not block the calls the bridge does have.
func TestBridgeSurfacePreflightAllowsRealSurface(t *testing.T) {
	body := "deneb.mail_archive(...), deneb.calendar(...), deneb.contacts(...), deneb.deals(...), deneb.wiki(...), deneb.read(f), deneb.write(f, c), deneb.edit(f, a, b)"
	if ok, reason := bridgeSurfacePreflight("원본", body); !ok {
		t.Fatalf("real bridge surface rejected: %s", reason)
	}
}

// A candidate that merely INHERITS an existing bad call is not made worse by
// it. Blocking those would freeze every skill already carrying one — the repair
// could never land.
func TestBridgeSurfacePreflightIgnoresInheritedBadCalls(t *testing.T) {
	original := "deneb.deal_ledger() 로 조회한다."
	candidate := "deneb.deal_ledger() 로 조회한다. 그리고 설명을 더 명확히 했다."
	if ok, reason := bridgeSurfacePreflight(original, candidate); !ok {
		t.Fatalf("inherited bad call must not block a repair: %s", reason)
	}
	// …but ADDING another one on top still fails.
	if ok, _ := bridgeSurfacePreflight(original, candidate+" deneb.nonexistent()"); ok {
		t.Fatal("a newly added bad call must still be rejected")
	}
}
