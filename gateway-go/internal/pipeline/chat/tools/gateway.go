package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// gatewayStartTime is captured at process start so `status` can report uptime.
// Approximate (a few microseconds off from the true gateway start) but
// accurate enough for agent-facing reporting.
var gatewayStartTime = time.Now()

// secretPathPattern rejects config paths that look like credentials.
// `config_set` refuses to write to these (agents should NEVER manage tokens).
var secretPathPattern = regexp.MustCompile(`(?i)(token|apikey|api_key|password|secret|credential)`)

// GatewayVersion is set by the bootstrap package at runtime so tools can
// report the build version without importing cmd/. Package-level for tests.
var GatewayVersion = "dev"

// CommandRunner abstracts external command execution so tests can inject
// fakes instead of shelling out to real git/make.
type CommandRunner interface {
	Run(ctx context.Context, dir, name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// ProcessSignaller abstracts process signalling so tests can verify the
// restart path without actually sending SIGUSR1 to the test process.
type ProcessSignaller interface {
	Signal(sig os.Signal) error
	PID() int
}

type selfSignaller struct{}

func (selfSignaller) Signal(sig os.Signal) error {
	proc, err := os.FindProcess(os.Getpid())
	if err != nil {
		return err
	}
	return proc.Signal(sig)
}

func (selfSignaller) PID() int { return os.Getpid() }

// GatewayDeps bundles the injectable dependencies for the gateway tool.
// When all fields are zero values, sensible defaults are wired (real git,
// real SIGUSR1 to self). Tests pass fakes.
type GatewayDeps struct {
	Runner     CommandRunner
	Signaller  ProcessSignaller
	ConfigPath string // override for config file path (empty ⇒ config.ResolveConfigPath)
	Now        func() time.Time
}

func (d GatewayDeps) runner() CommandRunner {
	if d.Runner != nil {
		return d.Runner
	}
	return execRunner{}
}

func (d GatewayDeps) signaller() ProcessSignaller {
	if d.Signaller != nil {
		return d.Signaller
	}
	return selfSignaller{}
}

func (d GatewayDeps) configPath() string {
	if d.ConfigPath != "" {
		return d.ConfigPath
	}
	return config.ResolveConfigPath()
}

func (d GatewayDeps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// ToolGateway returns the gateway self-management tool. Backward compatible
// with the legacy action names (config.get / config.patch / config.apply /
// config.schema.lookup / restart / restart.confirmed / update.run).
//
// New actions (Korean-first, agent-initiated, approval-gated):
//   - status        — dashboard of version, uptime, PID, port
//   - config_get    — read a dotted path from deneb.json
//   - config_set    — write a leaf value at a dotted path (approval required)
//   - update        — git pull + rebuild + restart (approval required; clean main only)
//
// Destructive actions (restart/update/config_set) return a structured
// `needs_approval` envelope on first call; the agent must relay confirmation
// to the user and then invoke the `.confirmed` variant with the same
// `action_token` to execute.
func ToolGateway(repoDir string) ToolFunc {
	return ToolGatewayWithDeps(repoDir, GatewayDeps{})
}

// ToolGatewayWithDeps is the injectable constructor used by tests.
func ToolGatewayWithDeps(repoDir string, deps GatewayDeps) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action      string         `json:"action"`
			Path        string         `json:"path"`
			Value       any            `json:"value"`
			Patch       map[string]any `json:"patch"`
			Config      map[string]any `json:"config"`
			ActionToken string         `json:"action_token"`
			Reason      string         `json:"reason"`
		}
		if err := jsonutil.UnmarshalInto("gateway params", input, &p); err != nil {
			return "", err
		}

		switch p.Action {
		// ── New agent-friendly actions ─────────────────────────────────────

		case "status":
			return gatewayStatus(deps)

		case "config_get":
			return gatewayConfigGet(deps, p.Path)

		case "config_set":
			return gatewayConfigSet(deps, p.Path, p.Value, false)

		case "config_set.confirmed":
			return gatewayConfigSet(deps, p.Path, p.Value, true)

		case "update":
			return gatewayUpdate(ctx, deps, repoDir, false)

		case "update.confirmed":
			return gatewayUpdate(ctx, deps, repoDir, true)

		// ── Legacy / existing actions (preserved) ──────────────────────────

		case "config.get":
			snapshot, err := config.LoadConfig(deps.configPath())
			if err != nil {
				return fmt.Sprintf("설정 파일 로드에 실패했습니다: %s", err.Error()), nil
			}
			result := map[string]any{
				"path":   snapshot.Path,
				"exists": snapshot.Exists,
				"valid":  snapshot.Valid,
				"hash":   snapshot.Hash,
				"config": snapshot.Config,
			}
			data, _ := json.MarshalIndent(result, "", "  ")
			return string(data), nil

		case "config.schema.lookup":
			node := config.LookupSchema(p.Path)
			if node == nil {
				return fmt.Sprintf("경로 %q에 대한 스키마를 찾을 수 없습니다.", p.Path), nil
			}
			data, _ := json.MarshalIndent(node, "", "  ")
			return string(data), nil

		case "config.patch":
			if p.Patch == nil {
				return "", fmt.Errorf("config.patch에는 patch 객체가 필요합니다")
			}
			return gatewayConfigPatch(deps, p.Patch)

		case "config.apply":
			if p.Config == nil {
				return "", fmt.Errorf("config.apply에는 config 객체가 필요합니다")
			}
			return gatewayConfigApply(deps, p.Config)

		case "restart":
			token := newActionToken()
			slog.Info("gateway tool: restart requested, awaiting approval",
				"reason", p.Reason, "token", token)
			return approvalEnvelope(
				token,
				"restart",
				"게이트웨이를 재시작합니다. 진행 중인 세션이 중단됩니다.",
				"재시작",
			)

		case "restart.confirmed":
			slog.Info("gateway tool: restart confirmed, sending SIGUSR1",
				"pid", deps.signaller().PID(), "token", p.ActionToken)
			if err := deps.signaller().Signal(syscall.SIGUSR1); err != nil {
				slog.Error("gateway tool: restart signal failed", "error", err)
				return fmt.Sprintf("재시작 신호 전송 실패: %s. CLI에서 `deneb gateway restart`를 사용하세요.", err.Error()), nil
			}
			return "게이트웨이 재시작 신호를 전송했습니다 (SIGUSR1). 곧 재시작됩니다.", nil

		case "update.run":
			// Legacy path — behaves like the confirmed update. Kept for
			// backward compatibility with older system prompts.
			return gatewayUpdate(ctx, deps, repoDir, true)

		default:
			return fmt.Sprintf("알 수 없는 gateway action: %q. 지원 action: status, config_get, config_set, update, restart, config.get, config.schema.lookup, config.patch, config.apply.", p.Action), nil
		}
	}
}

// ── Action implementations ────────────────────────────────────────────────

func gatewayStatus(deps GatewayDeps) (string, error) {
	snap, err := config.LoadConfig(deps.configPath())
	port := config.DefaultGatewayPort
	if err == nil && snap != nil && snap.Config.Gateway != nil && snap.Config.Gateway.Port != nil {
		port = *snap.Config.Gateway.Port
	}

	uptime := deps.now().Sub(gatewayStartTime)
	result := map[string]any{
		"version":    GatewayVersion,
		"pid":        deps.signaller().PID(),
		"port":       port,
		"uptime":     formatGatewayUptime(uptime),
		"uptime_sec": int64(uptime.Seconds()),
		"go_version": runtime.Version(),
		"config":     snap.Path,
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	return string(data), nil
}

func gatewayConfigGet(deps GatewayDeps, path string) (string, error) {
	snap, err := config.LoadConfig(deps.configPath())
	if err != nil {
		return fmt.Sprintf("설정 파일 로드에 실패했습니다: %s", err.Error()), nil
	}
	// Re-parse Raw as a map so dotted paths resolve against the on-disk
	// shape (not the defaulted Go struct).
	var rootMap map[string]any
	if snap.Raw != "" {
		if err := json.Unmarshal([]byte(snap.Raw), &rootMap); err != nil {
			return fmt.Sprintf("설정 파싱 실패: %s", err.Error()), nil
		}
	} else {
		rootMap = map[string]any{}
	}

	if strings.TrimSpace(path) == "" {
		data, _ := json.MarshalIndent(rootMap, "", "  ")
		return string(data), nil
	}

	val, ok := dottedGet(rootMap, path)
	if !ok {
		return fmt.Sprintf("설정 경로를 찾을 수 없습니다: %q", path), nil
	}
	data, _ := json.MarshalIndent(map[string]any{"path": path, "value": val}, "", "  ")
	return string(data), nil
}

func gatewayConfigSet(deps GatewayDeps, path string, value any, confirmed bool) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("config_set: path는 필수입니다 (예: \"model.main\")")
	}
	if secretPathPattern.MatchString(path) {
		slog.Warn("gateway tool: config_set blocked (secret path)", "path", path)
		return fmt.Sprintf("거부: 경로 %q는 비밀 값으로 보입니다. 에이전트는 토큰/비밀번호/API 키를 관리하지 않습니다.", path), nil
	}
	// Reject object replacement — must be a leaf scalar or array of scalars.
	if _, isMap := value.(map[string]any); isMap {
		return fmt.Sprintf("거부: %q 경로에 객체를 쓰려고 합니다. config_set은 leaf 값 전용입니다. 복합 변경은 config.patch를 사용하세요.", path), nil
	}

	if !confirmed {
		token := newActionToken()
		slog.Info("gateway tool: config_set awaiting approval",
			"path", path, "token", token)
		summary := fmt.Sprintf("설정 변경: `%s` = %s", path, formatValueForSummary(value))
		return approvalEnvelope(token, "config_set", summary, "설정 변경")
	}

	// Load raw, mutate, write atomically.
	cfgPath := deps.configPath()
	rootMap, err := loadRawConfigMap(cfgPath)
	if err != nil {
		return fmt.Sprintf("설정 로드 실패: %s", err.Error()), nil
	}
	if err := dottedSet(rootMap, path, value); err != nil {
		return fmt.Sprintf("설정 경로 적용 실패: %s", err.Error()), nil
	}
	data, err := json.MarshalIndent(rootMap, "", "  ")
	if err != nil {
		return fmt.Sprintf("설정 직렬화 실패: %s", err.Error()), nil
	}
	if err := atomicfile.WriteFile(cfgPath, data, &atomicfile.Options{Perm: 0o644, Backup: true}); err != nil {
		slog.Error("gateway tool: config_set write failed", "path", path, "error", err)
		return fmt.Sprintf("설정 파일 저장 실패: %s", err.Error()), nil
	}
	slog.Info("gateway tool: config_set applied", "path", path, "file", cfgPath)
	return fmt.Sprintf("설정을 저장했습니다: `%s` = %s. 변경 사항을 반영하려면 재시작이 필요할 수 있습니다.", path, formatValueForSummary(value)), nil
}

func gatewayConfigPatch(deps GatewayDeps, patch map[string]any) (string, error) {
	cfgPath := deps.configPath()
	rootMap, err := loadRawConfigMap(cfgPath)
	if err != nil {
		return fmt.Sprintf("현재 설정 파싱 실패: %s", err.Error()), nil
	}
	for k, v := range patch {
		rootMap[k] = v
	}
	merged, err := json.MarshalIndent(rootMap, "", "  ")
	if err != nil {
		return fmt.Sprintf("패치된 설정 직렬화 실패: %s", err.Error()), nil
	}
	if err := atomicfile.WriteFile(cfgPath, merged, &atomicfile.Options{Perm: 0o644, Backup: true}); err != nil {
		return fmt.Sprintf("설정 저장 실패: %s", err.Error()), nil
	}
	return fmt.Sprintf("설정을 패치했습니다. 저장 경로: %s", cfgPath), nil
}

func gatewayConfigApply(deps GatewayDeps, cfg map[string]any) (string, error) {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Sprintf("설정 직렬화 실패: %s", err.Error()), nil
	}
	cfgPath := deps.configPath()
	if err := atomicfile.WriteFile(cfgPath, data, &atomicfile.Options{Perm: 0o644, Backup: true}); err != nil {
		return fmt.Sprintf("설정 저장 실패: %s", err.Error()), nil
	}
	return fmt.Sprintf("설정을 적용했습니다. 저장 경로: %s", cfgPath), nil
}

func gatewayUpdate(ctx context.Context, deps GatewayDeps, repoDir string, confirmed bool) (string, error) {
	dir := repoDir
	if dir == "" {
		dir, _ = os.Getwd()
	}
	runner := deps.runner()

	updateCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	// Safety: branch must be main (or main-tracking).
	branchOut, err := runner.Run(updateCtx, dir, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return fmt.Sprintf("현재 브랜치 확인 실패: %s\n%s", err.Error(), strings.TrimSpace(string(branchOut))), nil
	}
	branch := strings.TrimSpace(string(branchOut))
	if branch != "main" {
		return fmt.Sprintf("거부: 현재 브랜치가 %q입니다. update는 main 브랜치에서만 허용됩니다. 먼저 main으로 체크아웃하세요.", branch), nil
	}

	// Safety: worktree must be clean.
	statusOut, err := runner.Run(updateCtx, dir, "git", "status", "--porcelain")
	if err != nil {
		return fmt.Sprintf("git status 실패: %s\n%s", err.Error(), strings.TrimSpace(string(statusOut))), nil
	}
	if strings.TrimSpace(string(statusOut)) != "" {
		return fmt.Sprintf("거부: 작업 디렉토리에 커밋되지 않은 변경이 있습니다. update 전에 먼저 정리하세요.\n\n```\n%s\n```", strings.TrimSpace(string(statusOut))), nil
	}

	if !confirmed {
		token := newActionToken()
		slog.Info("gateway tool: update awaiting approval",
			"branch", branch, "dir", dir, "token", token)
		summary := fmt.Sprintf("업데이트: `git pull --rebase origin main` → `make go` → 재시작 (현재 브랜치: %s)", branch)
		return approvalEnvelope(token, "update", summary, "업데이트")
	}

	// Step 1: git pull --rebase origin main
	pullOut, err := runner.Run(updateCtx, dir, "git", "pull", "--rebase", "origin", "main")
	if err != nil {
		slog.Error("gateway tool: update pull failed", "error", err)
		return fmt.Sprintf("update 실패 (git pull): %s\n%s", err.Error(), strings.TrimSpace(string(pullOut))), nil
	}

	// Step 2: build
	buildOut, err := runner.Run(updateCtx, dir, "make", "go")
	if err != nil {
		slog.Error("gateway tool: update build failed", "error", err)
		return fmt.Sprintf("update 실패 (빌드): %s\n%s", err.Error(), strings.TrimSpace(string(buildOut))), nil
	}

	// Sentinel for operator inspection.
	home, _ := os.UserHomeDir()
	if home != "" {
		sentinelPath := home + "/.deneb/.update-sentinel"
		sentinel := map[string]any{
			"updatedAt": deps.now().Format(time.RFC3339),
			"branch":    branch,
		}
		sentinelData, _ := json.Marshal(sentinel)
		_ = atomicfile.WriteFile(sentinelPath, sentinelData, &atomicfile.Options{Perm: 0o644})
	}

	slog.Info("gateway tool: update complete, triggering restart", "branch", branch, "dir", dir)
	if err := deps.signaller().Signal(syscall.SIGUSR1); err != nil {
		return fmt.Sprintf("업데이트는 성공했지만 재시작 신호 실패: %s. CLI에서 `deneb gateway restart`를 실행하세요.\n\n%s", err.Error(), strings.TrimSpace(string(pullOut))), nil
	}
	return fmt.Sprintf("업데이트 완료 — 곧 재시작됩니다.\n\n- git pull: %s\n- 빌드: OK", strings.TrimSpace(string(pullOut))), nil
}

// ── Helpers ──────────────────────────────────────────────────────────────

// approvalEnvelope returns a structured JSON response signalling that the
// action is destructive and requires user confirmation via a button.
// The pipeline / channel layer interprets `confirm_button` and renders an
// inline keyboard in Telegram; the agent relays the Korean summary as plain
// text if no button UI is available.
func approvalEnvelope(token, action, summary, buttonLabel string) (string, error) {
	envelope := map[string]any{
		"needs_approval": true,
		"action_token":   token,
		"action":         action,
		"summary":        summary,
		"confirm_button": map[string]any{
			"text":   buttonLabel,
			"action": action + ".confirmed",
			"token":  token,
		},
		"user_message": fmt.Sprintf("다음 작업에 대한 확인이 필요합니다: %s\n\n승인하려면 버튼을 누르거나 '응/확인/진행'이라고 답하세요.", summary),
	}
	data, _ := json.MarshalIndent(envelope, "", "  ")
	return string(data), nil
}

func newActionToken() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("tok_%d", time.Now().UnixNano())
	}
	return "tok_" + hex.EncodeToString(b)
}

// loadRawConfigMap reads the config file as a generic map (preserving the
// on-disk shape without defaults). Missing file ⇒ empty map.
func loadRawConfigMap(cfgPath string) (map[string]any, error) {
	raw, err := os.ReadFile(cfgPath) //nolint:gosec // G304 — path is config-resolved, trusted
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

// dottedGet walks `root` along a dotted path like "model.main".
func dottedGet(root map[string]any, path string) (any, bool) {
	parts := strings.Split(path, ".")
	var cur any = root
	for _, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[p]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// dottedSet writes a leaf value at a dotted path, creating intermediate
// maps as needed. The final segment is the leaf; earlier segments must be
// maps (or missing, in which case they are created).
func dottedSet(root map[string]any, path string, value any) error {
	parts := strings.Split(path, ".")
	if len(parts) == 0 || parts[0] == "" {
		return fmt.Errorf("empty path")
	}
	cur := root
	for i, p := range parts[:len(parts)-1] {
		nxt, ok := cur[p]
		if !ok {
			nm := map[string]any{}
			cur[p] = nm
			cur = nm
			continue
		}
		m, ok := nxt.(map[string]any)
		if !ok {
			return fmt.Errorf("path conflict at %q: expected object, got %T",
				strings.Join(parts[:i+1], "."), nxt)
		}
		cur = m
	}
	cur[parts[len(parts)-1]] = value
	return nil
}

// formatValueForSummary renders a value for the Korean approval summary.
// Strings get quotes; everything else uses JSON.
func formatValueForSummary(v any) string {
	if s, ok := v.(string); ok {
		return fmt.Sprintf("%q", s)
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(data)
}

// formatGatewayUptime renders a duration as "Nd Nh Nm" style.
func formatGatewayUptime(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	days := int(d / (24 * time.Hour))
	hours := int((d % (24 * time.Hour)) / time.Hour)
	minutes := int((d % time.Hour) / time.Minute)
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	default:
		return fmt.Sprintf("%dm", minutes)
	}
}
