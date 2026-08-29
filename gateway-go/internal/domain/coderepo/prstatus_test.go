package coderepo

import (
	"context"
	"errors"
	"testing"
)

// stubGH swaps the CLI for a canned reply and restores it afterwards.
func stubGH(t *testing.T, out string, err error) {
	t.Helper()
	prev := runGH
	runGH = func(context.Context, string, ...string) ([]byte, error) { return []byte(out), err }
	t.Cleanup(func() { runGH = prev })
}

// ★The distinction this whole surface rests on. "gh is broken" must NOT read as
// "your branch has no pull request" — that would tell the operator their work is
// untracked while it might be failing CI.
func TestUnreachableGHIsUnknownNotNone(t *testing.T) {
	stubGH(t, "", errors.New(`exec: "gh": executable file not found`))
	if got := PullRequestFor(context.Background(), "/repo", "deneb/x").State; got != PRStateUnknown {
		t.Errorf("state = %q, want %q", got, PRStateUnknown)
	}

	stubGH(t, "not json", nil)
	if got := PullRequestFor(context.Background(), "/repo", "deneb/x").State; got != PRStateUnknown {
		t.Errorf("malformed output state = %q, want %q", got, PRStateUnknown)
	}
}

// An empty list is a real answer: asked, and there is no pull request.
func TestEmptyListIsNone(t *testing.T) {
	stubGH(t, "[]", nil)
	if got := PullRequestFor(context.Background(), "/repo", "deneb/x").State; got != PRStateNone {
		t.Errorf("state = %q, want %q", got, PRStateNone)
	}
}

func TestMissingInputsCannotBeAnswered(t *testing.T) {
	stubGH(t, "[]", nil)
	if got := PullRequestFor(context.Background(), "", "deneb/x").State; got != PRStateUnknown {
		t.Errorf("no repo path: %q, want unknown", got)
	}
	if got := PullRequestFor(context.Background(), "/repo", "").State; got != PRStateUnknown {
		t.Errorf("no branch: %q, want unknown", got)
	}
}

func TestStatesMapFromTheCheckRollup(t *testing.T) {
	for _, tc := range []struct {
		name string
		json string
		want PRState
	}{
		{
			"all green is passing",
			`[{"number":1,"state":"OPEN","statusCheckRollup":[{"status":"COMPLETED","conclusion":"SUCCESS"}]}]`,
			PRStatePassing,
		},
		{
			"a running check is running",
			`[{"number":1,"state":"OPEN","statusCheckRollup":[{"status":"IN_PROGRESS","conclusion":""}]}]`,
			PRStateRunning,
		},
		{
			"merged outranks its checks",
			`[{"number":1,"state":"MERGED","statusCheckRollup":[{"status":"COMPLETED","conclusion":"FAILURE"}]}]`,
			PRStateMerged,
		},
		{"closed is closed", `[{"number":1,"state":"CLOSED","statusCheckRollup":[]}]`, PRStateClosed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubGH(t, tc.json, nil)
			if got := PullRequestFor(context.Background(), "/repo", "b").State; got != tc.want {
				t.Errorf("state = %q, want %q", got, tc.want)
			}
		})
	}
}

// ★A red check must win over a still-running one. Reporting "running" while
// something already failed delays exactly the signal this surface delivers.
func TestFailingOutranksPending(t *testing.T) {
	stubGH(t, `[{"number":7,"state":"OPEN","statusCheckRollup":[
		{"status":"COMPLETED","conclusion":"FAILURE"},
		{"status":"IN_PROGRESS","conclusion":""}]}]`, nil)

	got := PullRequestFor(context.Background(), "/repo", "b")
	if got.State != PRStateFailing {
		t.Errorf("state = %q, want %q", got.State, PRStateFailing)
	}
	if got.Failing != 1 || got.Pending != 1 || got.Total != 2 {
		t.Errorf("counts = %+v, want 1 failing / 1 pending / 2 total", got)
	}
}

// Skipped, neutral and cancelled are not defects. This repo skips several lanes
// on every pull request and cancels superseded runs — counting those as red
// would paint nearly every healthy pull request red.
func TestSkippedNeutralAndCancelledAreNotFailures(t *testing.T) {
	stubGH(t, `[{"number":8,"state":"OPEN","statusCheckRollup":[
		{"status":"COMPLETED","conclusion":"SUCCESS"},
		{"status":"COMPLETED","conclusion":"SKIPPED"},
		{"status":"COMPLETED","conclusion":"NEUTRAL"},
		{"status":"COMPLETED","conclusion":"CANCELLED"}]}]`, nil)

	got := PullRequestFor(context.Background(), "/repo", "b")
	if got.State != PRStatePassing {
		t.Errorf("state = %q, want %q", got.State, PRStatePassing)
	}
	if got.Failing != 0 {
		t.Errorf("failing = %d, want 0", got.Failing)
	}
}

// Legacy commit statuses report in `state` rather than `conclusion`.
func TestLegacyCommitStatusFieldIsRead(t *testing.T) {
	stubGH(t, `[{"number":9,"state":"OPEN","statusCheckRollup":[{"state":"FAILURE"}]}]`, nil)
	if got := PullRequestFor(context.Background(), "/repo", "b").State; got != PRStateFailing {
		t.Errorf("state = %q, want %q", got, PRStateFailing)
	}
}

func TestIdentityFieldsSurviveForLinking(t *testing.T) {
	stubGH(t, `[{"number":4932,"state":"MERGED","title":"제목","url":"https://x/4932","statusCheckRollup":[]}]`, nil)
	got := PullRequestFor(context.Background(), "/repo", "b")
	if got.Number != 4932 || got.Title != "제목" || got.URL != "https://x/4932" {
		t.Errorf("got %+v, want the pull request's identity carried through", got)
	}
}
