// Package enginecontrol asks the fleet which model its production serves, and
// chooses it.
//
// The fleet decides. stkernel's launchers/st_production.py on the engine's head
// node holds the selection (~/glm53-logs/st-production.json, glm53 when nothing
// chose), and the production supervisor does the switch — waits for the door to
// go quiet, stops the fleet it runs, boots the chosen profile under the
// production lease — and says what it is doing in a state file. The gateway
// never starts or stops a container: it reads that file and writes the
// selection, over ssh, the one path to the head that needs no control plane of
// its own (the backup task ships the same way).
//
// Which roles use the engine's model is the gateway's side of a switch, and it
// follows what production actually serves rather than what was chosen: see
// PlanFollow.
package enginecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// sshTargetEnv names the engine's head as user@host. Unset, the head is the
	// host the engine's /metrics URL names, reached as this process's user;
	// "off" disables the control entirely.
	sshTargetEnv = "DENEB_ENGINE_CONTROL_SSH"
	// scriptEnv points at st_production.py on the head. The default is
	// production's own tree, which is what the supervisor runs from.
	scriptEnv     = "DENEB_ENGINE_CONTROL_SCRIPT"
	defaultScript = "/home/choiceoh/st-engine/launchers/st_production.py"

	// stateTTL bounds how often the engine screen's 15 s poll costs an ssh.
	stateTTL   = 10 * time.Second
	sshTimeout = 8 * time.Second
)

// ErrUnknownProfile is a choice the fleet does not offer.
var ErrUnknownProfile = errors.New("the fleet does not serve that profile")

// profileName keeps a profile word safe on a remote command line. The fleet's
// own names are short lowercase words (glm53, qwen38).
var profileName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// Profile is one model production can serve.
type Profile struct {
	Name      string
	Model     string // the id the engine's door answers to
	Container string
}

// State is the fleet's answer: what was chosen, and what the supervisor is
// doing about it.
type State struct {
	Selected string // the profile production is to serve
	ChosenBy string
	ChosenAt time.Time
	Note     string

	// Supervised is false until a supervisor that knows about the selection has
	// published its state: the chooser then cannot learn whether it took.
	Supervised bool
	Serving    string // the profile the supervisor last saw answer a chat; "" while nothing of production serves
	Phase      string // serving | switching | launching | waiting | held | reverted
	Detail     string
	UpdatedAt  time.Time

	Profiles []Profile
	Default  string
}

// ModelOf is the model id a profile serves; "" for a profile the fleet does
// not list.
func (s State) ModelOf(profile string) string {
	for _, p := range s.Profiles {
		if p.Name == profile {
			return p.Model
		}
	}
	return ""
}

// Runner executes st_production.py on the engine's head with args and returns
// its standard output. Tests replace it; production runs ssh.
type Runner func(ctx context.Context, args ...string) ([]byte, error)

// Controller reads and writes production's model on one fleet.
type Controller struct {
	target string
	run    Runner
	now    func() time.Time

	mu       sync.Mutex
	cached   State
	cachedAt time.Time
}

// New returns a controller that runs st_production.py through run. target
// names the head in errors and logs.
func New(target string, run Runner) *Controller {
	return &Controller{target: target, run: run, now: time.Now}
}

// FromEnv builds the controller for the configured engine, or nil when there
// is none or the control is switched off. engineEndpoints are the engine's
// /metrics URLs (DENEB_ENGINE_METRICS_URL); the first one's host is the head.
func FromEnv(engineEndpoints []string) *Controller {
	target := strings.TrimSpace(os.Getenv(sshTargetEnv))
	if strings.EqualFold(target, "off") {
		return nil
	}
	if target == "" {
		if len(engineEndpoints) == 0 {
			return nil
		}
		u, err := url.Parse(strings.TrimSpace(engineEndpoints[0]))
		if err != nil || u.Hostname() == "" {
			return nil
		}
		target = u.Hostname()
		if me, err := user.Current(); err == nil && me.Username != "" {
			target = me.Username + "@" + target
		}
	}
	script := strings.TrimSpace(os.Getenv(scriptEnv))
	if script == "" {
		script = defaultScript
	}
	return New(target, sshRunner(target, script))
}

// Target is the head this controller asks, for display.
func (c *Controller) Target() string {
	if c == nil {
		return ""
	}
	return c.target
}

// sshRunner runs `python3 <script> <args>` on target. ssh joins the remote
// arguments into one shell command line, so every argument is quoted.
func sshRunner(target, script string) Runner {
	return func(ctx context.Context, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(ctx, sshTimeout)
		defer cancel()
		remote := []string{"python3", shellQuote(script)}
		for _, a := range args {
			remote = append(remote, shellQuote(a))
		}
		cmd := exec.CommandContext(ctx, "ssh", "-n", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5", //nolint:gosec // G204 — the target comes from operator config, every remote argument is quoted; ssh is the designed transport
			target, strings.Join(remote, " "))
		var stderr strings.Builder
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("ssh %s: %w (%s)", target, err, strings.TrimSpace(stderr.String()))
		}
		return out, nil
	}
}

// shellQuote wraps s in single quotes for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// State returns the fleet's answer, at most stateTTL old.
func (c *Controller) State(ctx context.Context) (State, error) {
	c.mu.Lock()
	if !c.cachedAt.IsZero() && c.now().Sub(c.cachedAt) < stateTTL {
		s := c.cached
		c.mu.Unlock()
		return s, nil
	}
	c.mu.Unlock()
	return c.Refresh(ctx)
}

// Refresh asks the fleet now.
func (c *Controller) Refresh(ctx context.Context) (State, error) {
	out, err := c.run(ctx, "show")
	if err != nil {
		return State{}, err
	}
	s, err := parseShow(out)
	if err != nil {
		return State{}, err
	}
	c.mu.Lock()
	c.cached, c.cachedAt = s, c.now()
	c.mu.Unlock()
	return s, nil
}

// Select chooses the profile production serves. The supervisor picks it up on
// its next cycle; the returned state is the fleet's answer right after the
// write. Choosing what is already chosen writes nothing.
func (c *Controller) Select(ctx context.Context, profile, by, note string) (State, error) {
	s, err := c.Refresh(ctx)
	if err != nil {
		return State{}, err
	}
	if !profileName.MatchString(profile) || s.ModelOf(profile) == "" {
		return s, fmt.Errorf("%w: %q", ErrUnknownProfile, profile)
	}
	if s.Selected == profile {
		return s, nil
	}
	if _, err := c.run(ctx, "select", profile, "--by", by, "--note", note); err != nil {
		return s, err
	}
	return c.Refresh(ctx)
}

// showDoc is st_production.py's `show`.
type showDoc struct {
	Selected  string `json:"selected"`
	Default   string `json:"default"`
	Selection struct {
		By   string  `json:"by"`
		At   float64 `json:"at"`
		Note string  `json:"note"`
	} `json:"selection"`
	State struct {
		Serving string  `json:"serving"`
		Wanted  string  `json:"wanted"`
		Phase   string  `json:"phase"`
		Detail  string  `json:"detail"`
		At      float64 `json:"at"`
	} `json:"state"`
	Profiles map[string]struct {
		Model     string `json:"model"`
		Container string `json:"container"`
	} `json:"profiles"`
}

func parseShow(raw []byte) (State, error) {
	var doc showDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return State{}, fmt.Errorf("st_production.py show: %w", err)
	}
	if doc.Selected == "" || len(doc.Profiles) == 0 {
		return State{}, errors.New("st_production.py show: no selection or no profiles")
	}
	s := State{
		Selected: doc.Selected, ChosenBy: doc.Selection.By, Note: doc.Selection.Note,
		ChosenAt: epoch(doc.Selection.At), Default: doc.Default,
		Supervised: doc.State.Phase != "", Serving: doc.State.Serving, Phase: doc.State.Phase,
		Detail: doc.State.Detail, UpdatedAt: epoch(doc.State.At),
	}
	for name, p := range doc.Profiles {
		s.Profiles = append(s.Profiles, Profile{Name: name, Model: p.Model, Container: p.Container})
	}
	sort.Slice(s.Profiles, func(i, j int) bool { return s.Profiles[i].Name < s.Profiles[j].Name })
	return s, nil
}

func epoch(sec float64) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(int64(sec * 1000))
}
