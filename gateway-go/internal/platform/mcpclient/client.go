// Package mcpclient implements a minimal client for the Model Context
// Protocol (MCP) stdio transport: it spawns a configured server command and
// speaks newline-delimited JSON-RPC 2.0 over the child's stdin/stdout.
//
// Scope is deliberately narrow (mirror of the mcpapi package's minimalism on
// the serving side): handshake, tools/list, tools/call, liveness probe. No
// resources/prompts/sampling — server-initiated requests are answered with
// "method not found" so a well-behaved server degrades gracefully.
//
// Two protocol eras are spoken, decided per server at initialization:
// 2026-07-28 ("MCP 2.0") is stateless — no handshake, per-request `_meta` —
// and everything older uses the initialize handshake. A `server/discover`
// probe tells them apart; see stateless.go.
//
// The stdio transport (not streamable HTTP) is the deliberate choice for
// external servers like Plaud's: their npx wrapper owns the OAuth dance and
// token cache, which a headless gateway cannot do against a browser-redirect
// flow. Remote-only servers ride a stdio bridge (`npx -y mcp-remote <url>`).
//
// One Client is meant to be shared by every consumer of its server (chat
// tools, ingest tasks, …): calls are safe to run concurrently, and the
// initialize handshake runs once in the background with its own timeout —
// waiters block cancelably on their own ctx, never on a mutex.
//
// Operational conveniences beyond the bare protocol:
//   - the child runs in its own PROCESS GROUP and teardown is tiered
//     (stdin close → SIGTERM group → grace → SIGKILL group), so npx-style
//     wrappers can flush state and their grandchildren never outlive them;
//   - a ring of recent child stderr is kept and folded into initialization
//     errors — a failed first-run OAuth surfaces its auth URL in the error
//     itself, not just somewhere in the log;
//   - Stats() exposes a snapshot (spawns, call counters, last error, recent
//     stderr) for health/observe surfaces.
package mcpclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

// handshakeProtocolVersion is the revision offered in an initialize handshake.
// It is deliberately NOT the newest revision this client speaks: 2026-07-28
// has no initialize, so reaching this constant already means the server is in
// the handshake era (see stateless.go).
const handshakeProtocolVersion = "2025-06-18"

// restartBackoff is the minimum interval between respawn attempts after the
// child process dies — prevents a crash-looping server from being re-exec'd
// on every tool call.
const restartBackoff = 30 * time.Second

// initTimeout bounds one spawn+handshake attempt. It derives from lifeCtx,
// not from any caller's ctx: a caller giving up early must not abort the
// initialization other callers are waiting for. Generous because a cold npx
// run downloads the package before the server even starts.
const initTimeout = 3 * time.Minute

// writeTimeout bounds a single stdin write. A child that stops draining its
// stdin (wedged event loop) would otherwise block the writer forever once
// the pipe buffer fills; hitting this deadline kills the child and routes
// recovery through the normal respawn path.
const writeTimeout = 30 * time.Second

// stopGrace is how long a stopped child gets to exit after SIGTERM before
// its process group is SIGKILLed. Polite first: npx wrappers may need to
// persist token caches and reap their own children.
const stopGrace = 3 * time.Second

// maxLineBytes caps a single JSON-RPC message from the server (tool results
// can embed whole meeting transcripts; 16 MiB is generous without being
// unbounded).
const maxLineBytes = 16 << 20

// stderrRingSize / stderrLineCap bound the retained child stderr (diagnosis
// ring — see Stats and init-error enrichment).
const (
	stderrRingSize = 20
	stderrLineCap  = 400
)

// ToolInfo is one entry from tools/list.
type ToolInfo struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	InputSchema jsonObject `json:"inputSchema"`
}

// Client manages one MCP server child process. Calls may run concurrently;
// a dead child is respawned lazily on the next call (restartBackoff-gated).
//
// Lock hierarchy (acquire in this order; never reverse):
//
//	Client.mu (process/init state)
//	  → Client.pendingMu (in-flight request table; reader goroutine takes it alone)
//	  → Client.stderrMu (stderr ring; also taken alone by the stderr goroutine)
//	Client.writeMu (stdin write serialization — independent: never held
//	together with any other lock, so a deadline-bounded slow write can only
//	stall other WRITERS, never state readers or the response router)
//
// No lock is ever held while waiting for the child: initialization waiters
// select on initDone/ctx, request waiters select on their pending channel.
type Client struct {
	argv   []string
	logger *slog.Logger
	// lifeCtx bounds the child process's lifetime (exec.CommandContext):
	// typically the server shutdown ctx, so the child dies with the gateway.
	lifeCtx context.Context

	writeMu sync.Mutex

	mu         sync.Mutex
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	ready      bool // process running AND initialize handshake completed
	lastStart  time.Time
	closed     bool
	generation int // bumped per spawn AND per stop; stale goroutines no-op
	spawns     int // spawn count (monotonic, for Stats)
	// initDone is closed when the in-flight background init attempt settles
	// (success or failure); nil when no attempt is in flight or pending
	// retry. initErr carries the last attempt's failure.
	initDone chan struct{}
	initErr  error
	// Handshake-reported identity (ServerInfo accessor).
	serverName    string
	serverVersion string
	// negotiatedProtocol is the revision settled on at initialization. Empty
	// means the handshake era; protocolVersion2026 means every request must
	// carry its own `_meta` (see stateless.go).
	negotiatedProtocol string
	listChangedLogged  bool // one restart-hint log per spawn, spam-proof
	lastError          string
	lastErrorAt        time.Time

	calls      atomic.Uint64
	callErrors atomic.Uint64

	stderrMu   sync.Mutex
	stderrGen  int      // spawn generation the ring belongs to (stale writers drop)
	stderrTail []string // ring of recent child stderr lines, newest last

	pendingMu sync.Mutex
	pending   map[int64]chan rpcResponse
	nextID    int64
}

type rpcResponse struct {
	Result json.RawMessage
	Err    error
}

// rpcError is the JSON-RPC 2.0 error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// New creates a client for the given command line (argv[0] = binary). The
// process is not spawned until Start or the first call. lifeCtx bounds the
// child process's lifetime (pass the server shutdown ctx); Close also kills it.
func New(lifeCtx context.Context, argv []string, logger *slog.Logger) (*Client, error) {
	if len(argv) == 0 {
		return nil, errors.New("mcpclient: empty command")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if lifeCtx == nil {
		lifeCtx = context.Background()
	}
	return &Client{
		argv:    argv,
		logger:  logger,
		lifeCtx: lifeCtx,
		pending: make(map[int64]chan rpcResponse),
	}, nil
}

// Start spawns the server process and waits for the initialize handshake.
// ctx bounds the wait only; the process itself lives until Close (and the
// handshake attempt keeps running on its own initTimeout even if ctx expires).
func (c *Client) Start(ctx context.Context) error {
	return c.ensureReady(ctx)
}

// Ping round-trips a cheap liveness probe for health surfaces and future
// consumers. 2026-07-28 removed the `ping` method — a POST that returns is
// liveness enough — so a stateless server is probed with server/discover, the
// cheapest method every 2.0 server MUST implement.
func (c *Client) Ping(ctx context.Context) error {
	if err := c.ensureReady(ctx); err != nil {
		return err
	}
	if c.protocol() == protocolVersion2026 {
		_, err := c.roundTrip(ctx, "server/discover", withStatelessMeta(nil, protocolVersion2026))
		return err
	}
	_, err := c.roundTrip(ctx, "ping", nil)
	return err
}

// ServerInfo returns the name/version the server reported at initialize
// (empty until the first successful handshake).
func (c *Client) ServerInfo() (name, version string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.serverName, c.serverVersion
}

// ListTools returns the server's full tool catalog (follows pagination).
func (c *Client) ListTools(ctx context.Context) ([]ToolInfo, error) {
	if err := c.ensureReady(ctx); err != nil {
		return nil, err
	}
	protocol := c.protocol()
	var out []ToolInfo
	cursor := ""
	seenCursors := map[string]bool{}
	for {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		params = withStatelessMeta(params, protocol)
		raw, err := c.roundTrip(ctx, "tools/list", params)
		if err != nil {
			return nil, err
		}
		var page struct {
			Tools      []ToolInfo `json:"tools"`
			NextCursor string     `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, fmt.Errorf("mcpclient: tools/list result: %w", err)
		}
		out = append(out, page.Tools...)
		if page.NextCursor == "" || len(page.Tools) == 0 {
			return out, nil
		}
		if seenCursors[page.NextCursor] {
			return nil, fmt.Errorf("mcpclient: tools/list repeated cursor %q", page.NextCursor)
		}
		seenCursors[page.NextCursor] = true
		cursor = page.NextCursor
	}
}

// CallTool invokes a named tool and renders the result for agent
// consumption (see renderToolResult for the fidelity rules).
func (c *Client) CallTool(ctx context.Context, name string, args rawJSON) (string, error) {
	if err := c.ensureReady(ctx); err != nil {
		return "", err
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	params := withStatelessMeta(map[string]any{"name": name, "arguments": args}, c.protocol())
	raw, err := c.roundTrip(ctx, "tools/call", params)
	if err != nil {
		return "", err
	}
	return renderToolResult(name, raw)
}

// Close terminates the server process and permanently disables the client.
// Safe to call more than once.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.stopLocked()
}

// CloseProcess kills the current child WITHOUT closing the client: the next
// call respawns it (restartBackoff-gated). Use when reclaiming an idle or
// unusable process while keeping the client available to later consumers —
// e.g. after a failed boot-time discovery, when the operator may complete
// the server's OAuth step and retry without a gateway restart.
func (c *Client) CloseProcess() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopLocked()
}

// ensureReady makes sure the child is spawned and initialized. The handshake
// runs at most once per spawn, in a background goroutine bounded by
// initTimeout (derived from lifeCtx). Callers wait on the shared initDone
// future with their OWN ctx — a caller giving up early neither aborts the
// attempt nor delays other callers, and no mutex is held while waiting.
func (c *Client) ensureReady(ctx context.Context) error {
	c.mu.Lock()
	if c.ready {
		c.mu.Unlock()
		return nil
	}
	if c.initDone == nil {
		if err := c.spawnLocked(); err != nil {
			c.mu.Unlock()
			return err
		}
		done := make(chan struct{})
		c.initDone = done
		c.initErr = nil
		gen := c.generation
		// safego owns panic recovery (outermost); the inner defer still
		// settles the future first during any unwind, so waiters never hang.
		safego.GoWithSlog(c.logger, "mcp-init", func() {
			defer close(done)
			c.initRun(gen)
		})
	}
	done := c.initDone
	c.mu.Unlock()

	select {
	case <-done:
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.ready {
			return nil
		}
		if c.closed {
			return errors.New("mcpclient: client closed")
		}
		if c.initErr != nil {
			return c.initErr
		}
		return errors.New("mcpclient: server initialization did not complete")
	case <-ctx.Done():
		return fmt.Errorf("mcpclient: waiting for server init: %w", ctx.Err())
	}
}

// initRun performs the initialize handshake for one spawn generation and
// settles the client's init state; the caller's wrapper closes the future
// and safego handles panic recovery.
func (c *Client) initRun(gen int) {
	ctx, cancel := context.WithTimeout(c.lifeCtx, initTimeout)
	defer cancel()
	res, err := c.handshake(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.generation != gen {
		return // superseded (deliberate stop or newer spawn) — state isn't ours
	}
	if err != nil {
		// Fold recent child stderr into the error: for the dominant failure
		// mode (first-run OAuth pending) this puts the auth URL in the error
		// the consumer sees, not just somewhere in the operator log.
		if snip := c.stderrSnippet(3); snip != "" {
			err = fmt.Errorf("%w — recent stderr: %s", err, snip)
		}
		c.initErr = err
		c.noteErrorLocked(err)
		c.stopLocked() // resets initDone so a later attempt can respawn
		return
	}
	if c.cmd != nil {
		c.ready = true
		c.initErr = nil
		// Settled successfully: reset the future so stopLocked's
		// "stopped during initialization" branch can never fire for a
		// later RUNTIME death of this fully-initialized server (waiters
		// have ready=true's fast path; nothing reads initDone once ready).
		c.initDone = nil
		c.serverName = res.serverName
		c.serverVersion = res.serverVersion
		c.negotiatedProtocol = res.protocol
	}
}

// spawnLocked starts the child process. Caller holds c.mu.
func (c *Client) spawnLocked() error {
	if c.closed {
		return errors.New("mcpclient: client closed")
	}
	if c.cmd != nil {
		return nil // already running (handshake pending or done)
	}
	if !c.lastStart.IsZero() && time.Since(c.lastStart) < restartBackoff {
		return fmt.Errorf("mcpclient: server process down, respawn backoff active (%s)", restartBackoff)
	}
	c.lastStart = time.Now()

	cmd := exec.CommandContext(c.lifeCtx, c.argv[0], c.argv[1:]...) //nolint:gosec // G204 — argv is operator config (DENEB_MCP_SERVERS env), not request input.
	// Allowlisted environment only: the gateway's own env carries provider
	// keys and mail credentials, and a third-party npx package (or any of its
	// transitive dependencies) must not inherit them. Servers that NEED a
	// custom variable get it explicitly via the command line: `env KEY=val
	// npx …` (argv[0]=env — no code path required).
	cmd.Env = childEnv()
	// Own process group: npx-style wrappers spawn the real server as a
	// grandchild, and signaling only the wrapper would orphan it. All kill
	// paths below signal the group (-pgid).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// lifeCtx cancel (gateway shutdown): polite TERM to the group so
	// wrappers can flush token caches. Deliberately NO delayed SIGKILL here:
	// an unguarded timer would race pid reuse (child exits fast, OS recycles
	// the pgid, late Kill(-pid) hits an innocent group). KILL escalation
	// lives solely in stopLocked's reaper, whose timer is stopped after
	// Wait. Residual: a child that ignores SIGTERM during gateway shutdown
	// can outlive the gateway — acceptable vs. collateral-killing a stranger.
	cmd.Cancel = func() error {
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
	cmd.WaitDelay = stopGrace

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("mcpclient: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("mcpclient: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("mcpclient: stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("mcpclient: start %q: %w", c.argv[0], err)
	}
	c.cmd = cmd
	c.stdin = stdin
	c.generation++
	c.spawns++
	c.listChangedLogged = false
	gen := c.generation
	// Fresh diagnosis ring per spawn: the ring answers "what did THIS child
	// say", and a predecessor lingering in its TERM grace must not write
	// into it (recordStderr drops stale generations).
	c.stderrMu.Lock()
	c.stderrGen = gen
	c.stderrTail = nil
	c.stderrMu.Unlock()
	c.logger.Info("mcp server process started", "cmd", c.argv[0], "pid", cmd.Process.Pid)

	// Reader goroutine: routes responses to pending calls. Exits on stdout
	// EOF (process death or Close), so it always has a termination path.
	// The inner defer runs onProcessExit even during a panic unwind, before
	// safego's outer recovery logs it.
	safego.GoWithSlog(c.logger, "mcp-reader", func() {
		defer c.onProcessExit(gen)
		c.readLoop(stdout)
	})

	// Stderr goroutine: surfaces the server's own diagnostics — first-run
	// OAuth instructions from wrappers like Plaud's npx package arrive here,
	// so they must reach both the operator log and the diagnosis ring that
	// initialization errors quote. Exits on stderr EOF alongside the process.
	// Lines are tagged with this spawn's generation so a dying predecessor
	// cannot contaminate a successor's diagnosis ring during the TERM grace.
	safego.GoWithSlog(c.logger, "mcp-stderr", func() {
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 64*1024), 256*1024)
		for sc.Scan() {
			if line := strings.TrimSpace(sc.Text()); line != "" {
				c.recordStderr(gen, line)
				c.logger.Info("mcp server stderr", "cmd", c.argv[0], "line", line)
			}
		}
	})

	return nil
}

// handshake establishes what this server speaks and returns its identity.
// State recording is the caller's job (initRun), under its generation check —
// a stale handshake must not overwrite a newer spawn's ServerInfo.
//
// 2026-07-28 deleted the handshake this is named after, so the era has to be
// detected rather than assumed: server/discover answers on a 2.0 server and is
// "method not found" on every older one.
func (c *Client) handshake(ctx context.Context) (handshakeResult, error) {
	if res, ok := c.discover(ctx); ok {
		return res, nil
	}
	return c.initializeHandshake(ctx)
}

// initializeHandshake is the pre-2026-07-28 path: initialize +
// notifications/initialized, pinning the version for the whole connection.
func (c *Client) initializeHandshake(ctx context.Context) (handshakeResult, error) {
	params := map[string]any{
		"protocolVersion": handshakeProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": clientName, "version": clientVersion},
	}
	raw, err := c.roundTrip(ctx, "initialize", params)
	if err != nil {
		return handshakeResult{}, fmt.Errorf("mcpclient: initialize: %w", err)
	}
	var init struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(raw, &init); err != nil {
		return handshakeResult{}, fmt.Errorf("mcpclient: initialize result: %w", err)
	}
	if err := c.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); err != nil {
		return handshakeResult{}, err
	}
	c.logger.Info("mcp server initialized",
		"server", init.ServerInfo.Name,
		"serverVersion", init.ServerInfo.Version,
		"protocolVersion", init.ProtocolVersion)
	return handshakeResult{
		serverName:    init.ServerInfo.Name,
		serverVersion: init.ServerInfo.Version,
	}, nil
}

// stopLocked is the single teardown path: it signals the child's process
// group (TERM now, KILL after stopGrace), resets the init future (so the
// next call can respawn instead of waiting on a settled channel), bumps the
// generation (so the old spawn's reader/init goroutines become stale and
// cannot double-handle the exit), and fails all in-flight calls. Caller
// holds c.mu; pendingMu is taken under it (documented order).
func (c *Client) stopLocked() {
	if c.stdin != nil {
		_ = c.stdin.Close() // MCP stdio shutdown step 1: give the child EOF first
		c.stdin = nil
	}
	if c.cmd != nil && c.cmd.Process != nil {
		pid := c.cmd.Process.Pid
		cmd := c.cmd
		if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
		}
		// Reap in the background with KILL escalation; Wait must not run
		// under c.mu because the exiting reader goroutine's onProcessExit
		// also takes it. The timer is stopped after Wait so a reaped pid
		// can never receive a late group-SIGKILL (pid-reuse safety) — this
		// reaper is the ONLY kill-escalation point (cmd.Cancel is TERM-only).
		safego.GoWithSlog(c.logger, "mcp-reaper", func() {
			t := time.AfterFunc(stopGrace, func() {
				if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
					_ = cmd.Process.Kill()
				}
			})
			_ = cmd.Wait()
			t.Stop()
		})
	}
	c.cmd = nil
	c.ready = false
	// A waiter mid-flight on the init future must get a concrete reason,
	// not the generic fallthrough (initRun's own failure path has already
	// set a more specific error before calling here). This branch — child
	// died mid-handshake — is where a pending first-run OAuth lands, so the
	// stderr ring (carrying the auth URL) is folded in here too; initRun
	// cannot do it for this path because the stop bumps the generation and
	// its late completion becomes stale.
	if c.initDone != nil && c.initErr == nil {
		err := errors.New("mcpclient: server process stopped during initialization")
		if snip := c.stderrSnippet(3); snip != "" {
			err = fmt.Errorf("%w — recent stderr: %s", err, snip)
		}
		c.initErr = err
		c.noteErrorLocked(err)
	}
	c.initDone = nil
	c.generation++

	err := errors.New("mcp server process stopped")
	c.pendingMu.Lock()
	for id, ch := range c.pending {
		ch <- rpcResponse{Err: err} // buffered; never blocks
		delete(c.pending, id)
	}
	c.pendingMu.Unlock()
}

// onProcessExit runs when the reader goroutine sees stdout EOF. A stale
// generation means the stop was deliberate (Close/CloseProcess/init failure
// already ran stopLocked, which bumps the generation) or a newer spawn owns
// the state — either way there is nothing to do and nothing to log. A
// current generation here is a genuinely unexpected child death.
func (c *Client) onProcessExit(gen int) {
	c.mu.Lock()
	if gen != c.generation {
		c.mu.Unlock()
		return
	}
	c.stopLocked()
	c.mu.Unlock()

	if c.lifeCtx.Err() == nil {
		// Not a lifeCtx (gateway shutdown) kill: the toolset silently
		// degrades until the next call respawns the child — operator-visible
		// per logging.md. Deliberate stops never reach here (stale gen).
		c.logger.Error("mcp server process exited unexpectedly", "cmd", c.argv[0])
	}
}

// readLoop parses newline-delimited JSON-RPC messages from stdout.
func (c *Client) readLoop(stdout io.Reader) {
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), maxLineBytes)
	for sc.Scan() {
		line := sc.Bytes()
		// bytes.TrimSpace: a string() conversion here would heap-copy up to
		// maxLineBytes per message just for a blank check.
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		// ID stays raw: our own requests use numeric ids, but a
		// server-initiated request may carry a string id that must be echoed
		// verbatim in the reply (else a server blocking on that reply stalls).
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  *rpcError       `json:"error"`
		}
		if err := json.Unmarshal(line, &msg); err != nil {
			c.logger.Warn("mcp server sent unparseable line", "error", err, "bytes", len(line))
			continue
		}
		hasID := len(msg.ID) > 0 && string(msg.ID) != "null"
		switch {
		case msg.Method != "" && hasID:
			// Server-initiated request (sampling/roots/…) — out of scope.
			c.logger.Debug("mcp server request unsupported", "method", msg.Method)
			_ = c.send(map[string]any{
				"jsonrpc": "2.0",
				"id":      msg.ID,
				"error":   rpcError{Code: -32601, Message: "method not supported by deneb-gateway client"},
			})
		case msg.Method != "":
			c.onNotification(msg.Method)
		case hasID:
			var id int64
			if err := json.Unmarshal(msg.ID, &id); err != nil {
				// Responses can only answer requests WE sent, and ours are
				// always numeric — anything else is unroutable.
				c.logger.Warn("mcp response with non-numeric id ignored", "id", string(msg.ID))
				continue
			}
			var resp rpcResponse
			if msg.Error != nil {
				resp.Err = fmt.Errorf("mcp server error %d: %s", msg.Error.Code, msg.Error.Message)
			} else {
				resp.Result = append(json.RawMessage(nil), msg.Result...)
			}
			c.pendingMu.Lock()
			ch, ok := c.pending[id]
			if ok {
				delete(c.pending, id)
			}
			c.pendingMu.Unlock()
			if ok {
				ch <- resp // buffered; never blocks the reader
			}
		}
	}
	if err := sc.Err(); err != nil {
		c.logger.Warn("mcp stdout read ended", "error", err)
	}
}

// onNotification handles server notifications. tools/list_changed matters
// operationally: the chat toolset is frozen per process (prompt-cache Rule B),
// so an upstream toolset change needs a gateway restart to be picked up —
// surface that once per spawn at Info (once, so a hostile server cannot spam
// the log). Everything else stays at Debug.
func (c *Client) onNotification(method string) {
	if method == "notifications/tools/list_changed" {
		c.mu.Lock()
		logged := c.listChangedLogged
		c.listChangedLogged = true
		c.mu.Unlock()
		if !logged {
			c.logger.Info("mcp server toolset changed upstream — restart the gateway to re-discover",
				"cmd", c.argv[0])
		}
		return
	}
	c.logger.Debug("mcp server notification ignored", "method", method)
}

// roundTrip sends a request and waits for its response or ctx expiry.
func (c *Client) roundTrip(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return c.request(ctx, method, params, false)
}

// probe is roundTrip for a call whose failure is an expected ANSWER rather
// than a fault — the 2.0 era detection against a handshake-era server. Its
// "method not found" is kept out of Stats' last-error, which operators read as
// "this server is broken".
func (c *Client) probe(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return c.request(ctx, method, params, true)
}

func (c *Client) request(ctx context.Context, method string, params any, quiet bool) (json.RawMessage, error) {
	c.calls.Add(1)
	c.pendingMu.Lock()
	c.nextID++
	id := c.nextID
	// Buffered so a late response after ctx expiry never blocks the reader.
	ch := make(chan rpcResponse, 1)
	c.pending[id] = ch
	c.pendingMu.Unlock()

	req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	note := c.noteCallError
	if quiet {
		note = func(err error) error { return err }
	}
	if err := c.send(req); err != nil {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, note(err)
	}

	select {
	case resp := <-ch:
		if resp.Err != nil {
			return nil, note(fmt.Errorf("mcpclient: %s: %w", method, resp.Err))
		}
		return resp.Result, nil
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, note(fmt.Errorf("mcpclient: %s: %w", method, ctx.Err()))
	}
}

// deadlineWriter is the subset of *os.File send uses to bound writes.
// exec.Cmd.StdinPipe returns an *os.File, whose pipe deadlines work on Linux.
type deadlineWriter interface {
	SetWriteDeadline(t time.Time) error
}

// send marshals and writes one newline-terminated message to the child's
// stdin. Writes are deadline-bounded: a child that stops draining stdin
// would otherwise block the caller forever once the 64KB pipe buffer fills —
// on any write failure (deadline included) the child is killed so recovery
// goes through the normal respawn path.
//
// The write happens under writeMu with NO other lock held: a wedged child
// stalls only competing writers for up to writeTimeout, never Stats/
// ensureReady/response routing. The generation captured with the stdin ref
// keeps the failure path from killing a newer process than the one written to.
func (c *Client) send(msg map[string]any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("mcpclient: marshal: %w", err)
	}
	data = append(data, '\n')

	c.mu.Lock()
	stdin := c.stdin
	gen := c.generation
	c.mu.Unlock()
	if stdin == nil {
		return errors.New("mcpclient: server process not running")
	}

	c.writeMu.Lock()
	if dw, ok := stdin.(deadlineWriter); ok {
		_ = dw.SetWriteDeadline(time.Now().Add(writeTimeout)) // best effort
	}
	_, err = stdin.Write(data)
	c.writeMu.Unlock()

	if err != nil {
		// Broken/wedged pipe — kill this generation so recovery goes through
		// the normal respawn path. This is an UNEXPECTED child failure and
		// stopLocked's generation bump makes the reader's onProcessExit
		// stale (silent), so the operator-visible Error lives here.
		c.logger.Error("mcp server stdin write failed — stopping process", "cmd", c.argv[0], "error", err)
		c.mu.Lock()
		if c.generation == gen {
			c.stopLocked()
		}
		c.mu.Unlock()
		return fmt.Errorf("mcpclient: write: %w", err)
	}
	return nil
}
