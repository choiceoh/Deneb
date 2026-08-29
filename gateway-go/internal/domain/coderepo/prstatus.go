package coderepo

// Pull-request status for a conversation's branch.
//
// A conversation bound to a repository works on its own branch (BranchFor), so
// that branch name is the key linking a chat thread to its pull request. This
// asks GitHub — through the `gh` CLI the operator is already logged into — what
// happened to the work after it left the agent: are checks running, did they
// fail, did it land.
//
// Why an EXTERNAL source: the agent reports its own success, and this surface
// exists precisely because that report can be wrong. GitHub's check rollup is
// not something the agent can talk its way past.

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"
)

// PRState is what to show for a branch.
type PRState string

const (
	// PRStateUnknown means we could not ask — `gh` missing, not logged in, or
	// the call failed. ★Deliberately distinct from PRStateNone: an auth failure
	// rendered as "no pull request" would quietly tell the operator their work
	// is untracked when it may in fact be failing CI.
	PRStateUnknown PRState = "unknown"
	// PRStateNone means we asked successfully and the branch has no PR.
	PRStateNone    PRState = "none"
	PRStateRunning PRState = "running"
	PRStateFailing PRState = "failing"
	PRStatePassing PRState = "passing"
	PRStateMerged  PRState = "merged"
	PRStateClosed  PRState = "closed"
)

// PRStatus is one branch's pull-request state.
type PRStatus struct {
	State   PRState `json:"state"`
	Number  int     `json:"number,omitempty"`
	Title   string  `json:"title,omitempty"`
	URL     string  `json:"url,omitempty"`
	Failing int     `json:"failing,omitempty"`
	Pending int     `json:"pending,omitempty"`
	Total   int     `json:"total,omitempty"`
}

// ghTimeout bounds the CLI call. This sits behind a UI poll, so a hung network
// must surface as "unknown" rather than hold the request open.
const ghTimeout = 15 * time.Second

type ghCheck struct {
	Status     string `json:"status"`     // QUEUED / IN_PROGRESS / COMPLETED
	Conclusion string `json:"conclusion"` // SUCCESS / FAILURE / SKIPPED / …
	State      string `json:"state"`      // legacy commit statuses report here
}

type ghPR struct {
	Number int       `json:"number"`
	State  string    `json:"state"` // OPEN / MERGED / CLOSED
	Title  string    `json:"title"`
	URL    string    `json:"url"`
	Rollup []ghCheck `json:"statusCheckRollup"`
}

// runGH is swapped in tests. It returns raw stdout so the parsing stays here
// rather than inside a fake.
var runGH = func(ctx context.Context, repoPath string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, ghTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = repoPath // gh resolves the repository from its working directory
	return cmd.Output()
}

// PullRequestFor reports the pull-request state of a branch.
//
// It never returns an error: the caller is a status icon, and every failure
// mode collapses to "we could not tell", which is itself the honest thing to
// show. Nothing is logged here — an unreachable `gh` is an optional-feature
// failure the state already communicates, not a user-visible outage.
func PullRequestFor(ctx context.Context, repoPath, branch string) PRStatus {
	if strings.TrimSpace(repoPath) == "" || strings.TrimSpace(branch) == "" {
		return PRStatus{State: PRStateUnknown}
	}
	out, err := runGH(ctx, repoPath,
		"pr", "list", "--head", branch, "--state", "all", "--limit", "1",
		"--json", "number,state,title,url,statusCheckRollup")
	if err != nil {
		return PRStatus{State: PRStateUnknown}
	}
	var prs []ghPR
	if err := json.Unmarshal(out, &prs); err != nil {
		return PRStatus{State: PRStateUnknown}
	}
	if len(prs) == 0 {
		// Asked and answered: this branch genuinely has no pull request.
		return PRStatus{State: PRStateNone}
	}
	return statusOf(prs[0])
}

func statusOf(pr ghPR) PRStatus {
	s := PRStatus{Number: pr.Number, Title: pr.Title, URL: pr.URL}
	for _, c := range pr.Rollup {
		s.Total++
		concl := strings.ToUpper(strings.TrimSpace(c.Conclusion))
		if concl == "" {
			concl = strings.ToUpper(strings.TrimSpace(c.State))
		}
		switch concl {
		case "SUCCESS", "NEUTRAL", "SKIPPED":
			// Not failures. Counting skipped/neutral as red would paint most
			// healthy pull requests red — this repo skips several lanes per PR.
		case "FAILURE", "TIMED_OUT", "ERROR", "STARTUP_FAILURE", "ACTION_REQUIRED":
			s.Failing++
		case "CANCELLED":
			// Usually a superseded re-push, not a defect.
		default:
			// QUEUED / IN_PROGRESS / PENDING / empty / anything unrecognised:
			// still running rather than an invented verdict.
			if strings.ToUpper(strings.TrimSpace(c.Status)) != "COMPLETED" {
				s.Pending++
			}
		}
	}

	switch strings.ToUpper(pr.State) {
	case "MERGED":
		s.State = PRStateMerged
	case "CLOSED":
		s.State = PRStateClosed
	default:
		// ★Failing outranks pending: a red check is what the operator must act
		// on, and showing "running" while something already failed would delay
		// exactly the signal this surface exists to deliver.
		switch {
		case s.Failing > 0:
			s.State = PRStateFailing
		case s.Pending > 0:
			s.State = PRStateRunning
		default:
			s.State = PRStatePassing
		}
	}
	return s
}
