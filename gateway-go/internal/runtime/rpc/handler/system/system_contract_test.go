package system

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/maintenance"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/usage"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/metrics"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpctest"
)

type staticMethodLister []string

func (m staticMethodLister) Methods() []string { return append([]string(nil), m...) }

func resultStrings(t *testing.T, value any) []string {
	t.Helper()
	raw, ok := value.([]any)
	if !ok {
		t.Fatalf("value type = %T, want []any", value)
	}
	out := make([]string, len(raw))
	for i, item := range raw {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("value[%d] type = %T, want string", i, item)
		}
		out[i] = text
	}
	return out
}

func findCalledMethod(t *testing.T, result map[string]any, method string) map[string]any {
	t.Helper()
	called, ok := result["called"].([]any)
	if !ok {
		t.Fatalf("called type = %T", result["called"])
	}
	for _, item := range called {
		record, ok := item.(map[string]any)
		if ok && record["method"] == method {
			return record
		}
	}
	t.Fatalf("called method %q not found in %#v", method, called)
	return nil
}

func TestHealthMethodsReturnHealthAndSystemInfo(t *testing.T) {
	methods := HealthMethods(HealthDeps{
		SessionCount: func() int { return 7 },
		Version:      "v-test",
	})

	health := rpctest.Call(methods, "health.check", nil)
	rpctest.MustOK(t, health)
	healthResult := rpctest.Result(t, health)
	if healthResult["status"] != "ok" || healthResult["runtime"] != "go" || healthResult["sessions"] != float64(7) {
		t.Fatalf("health result = %#v", healthResult)
	}
	if channels := healthResult["channels"].([]any); len(channels) != 0 {
		t.Fatalf("channels = %#v, want empty", channels)
	}

	info := rpctest.Call(methods, "system.info", nil)
	rpctest.MustOK(t, info)
	infoResult := rpctest.Result(t, info)
	if infoResult["version"] != "v-test" || infoResult["goVersion"] != runtime.Version() {
		t.Fatalf("system info = %#v", infoResult)
	}
	if infoResult["arch"] != runtime.GOARCH || infoResult["numCPU"].(float64) < 1 {
		t.Fatalf("runtime details = %#v", infoResult)
	}
}

func TestHealthMethodsReturnZeroSessionsAndUnknownVersionByDefault(t *testing.T) {
	methods := HealthMethods(HealthDeps{})
	health := rpctest.Result(t, rpctest.Call(methods, "health.check", nil))
	if health["sessions"] != float64(0) {
		t.Fatalf("sessions = %v, want 0", health["sessions"])
	}
	info := rpctest.Result(t, rpctest.Call(methods, "system.info", nil))
	if info["version"] != "unknown" {
		t.Fatalf("version = %v, want unknown", info["version"])
	}
}

func TestIdentityMethodsReturnStableGatewayIdentity(t *testing.T) {
	resp := rpctest.Call(IdentityMethods("v1.2.3"), "gateway.identity.get", nil)
	rpctest.MustOK(t, resp)
	result := rpctest.Result(t, resp)
	if result["version"] != "v1.2.3" || result["runtime"] != "go" || result["arch"] != runtime.GOARCH {
		t.Fatalf("identity = %#v", result)
	}
	for _, key := range []string{"hostname", "stateDir"} {
		if strings.TrimSpace(result[key].(string)) == "" {
			t.Fatalf("identity %s is blank: %#v", key, result)
		}
	}
}

func TestMaintenanceMethodsRejectRunAndSummaryWhenRunnerMissing(t *testing.T) {
	methods := MaintenanceMethods(MaintenanceDeps{})
	rpctest.MustErr(t, rpctest.Call(methods, "maintenance.run", nil))
	rpctest.MustErr(t, rpctest.Call(methods, "maintenance.summary", nil))
	status := rpctest.Call(methods, "maintenance.status", nil)
	rpctest.MustOK(t, status)
	if rpctest.Result(t, status)["hasReport"] != false {
		t.Fatalf("status = %#v", rpctest.Result(t, status))
	}
}

func TestMaintenanceMethodsRunCreatesAndSummaryReusesReport(t *testing.T) {
	runner := maintenance.NewRunner(t.TempDir())
	methods := MaintenanceMethods(MaintenanceDeps{Runner: runner})

	initial := rpctest.Result(t, rpctest.Call(methods, "maintenance.status", nil))
	if initial["hasReport"] != false {
		t.Fatalf("initial status = %#v", initial)
	}

	run := rpctest.Call(methods, "maintenance.run", map[string]any{"dryRun": true})
	rpctest.MustOK(t, run)
	if rpctest.Result(t, run)["dryRun"] != true {
		t.Fatalf("run result = %#v", rpctest.Result(t, run))
	}

	status := rpctest.Result(t, rpctest.Call(methods, "maintenance.status", nil))
	if status["hasReport"] != true || status["summary"] == nil || status["report"] == nil {
		t.Fatalf("status = %#v", status)
	}

	summary := rpctest.Result(t, rpctest.Call(methods, "maintenance.summary", nil))
	if summary["summary"] == nil || summary["report"] == nil {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestMaintenanceSummaryCreatesDryRunWhenNoReportExists(t *testing.T) {
	runner := maintenance.NewRunner(t.TempDir())
	methods := MaintenanceMethods(MaintenanceDeps{Runner: runner})
	result := rpctest.Result(t, rpctest.Call(methods, "maintenance.summary", nil))
	report := result["report"].(map[string]any)
	if report["dryRun"] != true {
		t.Fatalf("report = %#v, want dry run", report)
	}
	if runner.LastReport() == nil || !runner.LastReport().DryRun {
		t.Fatal("summary must retain the generated dry-run report")
	}
}

func TestUsageMethodsReturnEmptyShapeWithoutTracker(t *testing.T) {
	methods := UsageMethods(UsageDeps{})
	status := rpctest.Result(t, rpctest.Call(methods, "usage.status", nil))
	if status["uptime"] != "0s" || len(status["providers"].(map[string]any)) != 0 {
		t.Fatalf("status = %#v", status)
	}
	cost := rpctest.Result(t, rpctest.Call(methods, "usage.cost", nil))
	if cost["totalCalls"] != float64(0) || len(cost["providers"].(map[string]any)) != 0 {
		t.Fatalf("cost = %#v", cost)
	}
}

func TestUsageMethodsReturnRecordedCallsAndTokenTotals(t *testing.T) {
	tracker := usage.New()
	tracker.RecordCall("openai")
	tracker.RecordCall("openai")
	tracker.RecordCall("local")
	tracker.RecordTokens("openai", 100, 25, 40, 5)
	methods := UsageMethods(UsageDeps{Tracker: tracker})

	status := rpctest.Result(t, rpctest.Call(methods, "usage.status", nil))
	providers := status["providers"].(map[string]any)
	openAI := providers["openai"].(map[string]any)
	if openAI["calls"] != float64(2) || openAI["tokens"].(map[string]any)["cacheRead"] != float64(40) {
		t.Fatalf("openai status = %#v", openAI)
	}
	if strings.TrimSpace(status["startedAt"].(string)) == "" {
		t.Fatalf("startedAt is blank: %#v", status)
	}

	cost := rpctest.Result(t, rpctest.Call(methods, "usage.cost", nil))
	if cost["totalCalls"] != float64(3) {
		t.Fatalf("cost = %#v", cost)
	}
}

func TestMonitoringMethodsReturnStableEmptyChannelSurface(t *testing.T) {
	methods := MonitoringMethods(MonitoringDeps{})
	channels := rpctest.Result(t, rpctest.Call(methods, "monitoring.channel_health", nil))["channels"].([]any)
	if len(channels) != 0 {
		t.Fatalf("channels = %#v", channels)
	}
	zero := rpctest.Result(t, rpctest.Call(methods, "monitoring.rpc_zero_calls", nil))
	if zero["total"] != float64(0) || len(zero["zeroCalls"].([]any)) != 0 {
		t.Fatalf("nil-dispatcher result = %#v", zero)
	}
}

func TestMonitoringRPCZeroCallsReturnsZeroAndCalledMethodCounts(t *testing.T) {
	const calledMethod = "system.contract.called"
	const zeroMethod = "system.contract.zero"
	metrics.RPCRequestsTotal.Inc(calledMethod, "ok", "")
	metrics.RPCRequestsTotal.Inc(calledMethod, "ok", "")
	metrics.RPCRequestsTotal.Inc(calledMethod, "error", "E_TIMEOUT")
	metrics.RPCRequestsTotal.Inc(calledMethod, "error", "E_UNAVAILABLE")

	methods := MonitoringMethods(MonitoringDeps{Dispatcher: staticMethodLister{zeroMethod, calledMethod}})
	result := rpctest.Result(t, rpctest.Call(methods, "monitoring.rpc_zero_calls", nil))
	if got := resultStrings(t, result["zeroCalls"]); len(got) != 1 || got[0] != zeroMethod {
		t.Fatalf("zero calls = %#v", got)
	}
	called := findCalledMethod(t, result, calledMethod)
	if called["ok"] != float64(2) || called["error"] != float64(2) {
		t.Fatalf("called counts = %#v", called)
	}
	if result["zeroCount"] != float64(1) || result["calledCount"] != float64(1) || result["totalMethods"] != float64(2) {
		t.Fatalf("summary counts = %#v", result)
	}
}

func TestRPCMethodCountsIgnoreOtherMethodsAndUnknownStatuses(t *testing.T) {
	counts := map[string]int64{
		"target\x00ok\x00":              3,
		"target\x00error\x00E_ONE":      2,
		"target\x00error\x00E_TWO":      4,
		"target\x00in_progress\x00":     99,
		"target-child\x00ok\x00":        50,
		"unrelated\x00error\x00E_OTHER": 60,
	}
	ok, errs := rpcMethodCounts(counts, "target")
	if ok != 3 || errs != 6 {
		t.Fatalf("rpcMethodCounts = (%d, %d), want (3, 6)", ok, errs)
	}
}

func TestLogsTailRejectsMissingLogDirectoryAndNonLogFiles(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	rpctest.MustErr(t, rpctest.Call(LogsMethods(LogsDeps{LogDir: missing}), "logs.tail", nil))

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a log"), 0o600); err != nil {
		t.Fatal(err)
	}
	rpctest.MustErr(t, rpctest.Call(LogsMethods(LogsDeps{LogDir: dir}), "logs.tail", nil))
}

func TestLogsTailReturnsLatestDatedLogFileByDefault(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"deneb-2026-07-09.log": "old\n",
		"deneb-2026-07-11.log": "new\n",
		"deneb-2026-07-10.log": "middle\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	result := rpctest.Result(t, rpctest.Call(LogsMethods(LogsDeps{LogDir: dir}), "logs.tail", nil))
	if result["file"] != "deneb-2026-07-11.log" {
		t.Fatalf("file = %v", result["file"])
	}
	if got := resultStrings(t, result["lines"]); len(got) != 1 || got[0] != "new" {
		t.Fatalf("lines = %#v", got)
	}
}

func TestLogsTailPaginatesWithoutDroppingBoundaryLine(t *testing.T) {
	dir := t.TempDir()
	body := "one\ntwo\nthree\nfour\n"
	if err := os.WriteFile(filepath.Join(dir, "deneb.log"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	methods := LogsMethods(LogsDeps{LogDir: dir})
	first := rpctest.Result(t, rpctest.Call(methods, "logs.tail", map[string]any{"limit": 2}))
	if got := resultStrings(t, first["lines"]); strings.Join(got, ",") != "one,two" {
		t.Fatalf("first page = %#v", got)
	}
	if first["truncated"] != true {
		t.Fatalf("first page must be truncated: %#v", first)
	}

	cursor := int64(first["cursor"].(float64))
	second := rpctest.Result(t, rpctest.Call(methods, "logs.tail", map[string]any{"cursor": cursor, "limit": 2}))
	if got := resultStrings(t, second["lines"]); strings.Join(got, ",") != "three,four" {
		t.Fatalf("second page = %#v", got)
	}
	if second["reset"] != false {
		t.Fatalf("second page unexpectedly reset: %#v", second)
	}
}

func TestLogsTailPaginatesCRLFWithoutCursorDrift(t *testing.T) {
	dir := t.TempDir()
	body := "one\r\ntwo\r\nthree\r\n"
	if err := os.WriteFile(filepath.Join(dir, "deneb.log"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	methods := LogsMethods(LogsDeps{LogDir: dir})
	first := rpctest.Result(t, rpctest.Call(methods, "logs.tail", map[string]any{"limit": 1}))
	if first["cursor"] != float64(len("one\r\n")) {
		t.Fatalf("first cursor = %v, want %d", first["cursor"], len("one\r\n"))
	}
	second := rpctest.Result(t, rpctest.Call(methods, "logs.tail", map[string]any{
		"cursor": int64(first["cursor"].(float64)),
		"limit":  1,
	}))
	if got := resultStrings(t, second["lines"]); len(got) != 1 || got[0] != "two" {
		t.Fatalf("second page = %#v", got)
	}
}

func TestLogsTailByteLimitTruncatesAtBoundaryAndResumesAtNextLine(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "deneb.log"), []byte("abcdefghij\nsecond\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	methods := LogsMethods(LogsDeps{LogDir: dir})
	first := rpctest.Result(t, rpctest.Call(methods, "logs.tail", map[string]any{"maxBytes": 5}))
	if got := resultStrings(t, first["lines"]); len(got) != 1 || got[0] != "abcde" {
		t.Fatalf("first page = %#v", got)
	}
	if first["cursor"] != float64(5) || first["truncated"] != true {
		t.Fatalf("first page metadata = %#v", first)
	}
	second := rpctest.Result(t, rpctest.Call(methods, "logs.tail", map[string]any{
		"cursor": int64(first["cursor"].(float64)),
	}))
	if got := resultStrings(t, second["lines"]); len(got) != 1 || got[0] != "second" {
		t.Fatalf("second page = %#v", got)
	}
}

func TestLogsTailReadsOnlyFullLinesAfterMidLineCursor(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "deneb.log"), []byte("alpha\nbeta\ngamma\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := rpctest.Result(t, rpctest.Call(LogsMethods(LogsDeps{LogDir: dir}), "logs.tail", map[string]any{
		"cursor": 2,
		"limit":  2,
	}))
	if got := resultStrings(t, result["lines"]); strings.Join(got, ",") != "beta,gamma" {
		t.Fatalf("lines = %#v", got)
	}
}

func TestLogsTailRestartsAfterCursorExceedsRotatedFileSize(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "deneb.log"), []byte("fresh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := rpctest.Result(t, rpctest.Call(LogsMethods(LogsDeps{LogDir: dir}), "logs.tail", map[string]any{
		"cursor": 9999,
	}))
	if result["reset"] != true || int64(result["cursor"].(float64)) != int64(len("fresh\n")) {
		t.Fatalf("rotation result = %#v", result)
	}
	if got := resultStrings(t, result["lines"]); len(got) != 1 || got[0] != "fresh" {
		t.Fatalf("lines = %#v", got)
	}
}

func TestLogsTailRestartsWhenCursorIsNegative(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "deneb.log"), []byte("first\nsecond\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := rpctest.Result(t, rpctest.Call(LogsMethods(LogsDeps{LogDir: dir}), "logs.tail", map[string]any{
		"cursor": -10,
	}))
	if result["reset"] != true {
		t.Fatalf("negative cursor did not reset: %#v", result)
	}
	if got := resultStrings(t, result["lines"]); strings.Join(got, ",") != "first,second" {
		t.Fatalf("lines = %#v", got)
	}
}

func TestConfigAdvancedMethodRosterAndValidationFailures(t *testing.T) {
	methods := ConfigAdvancedMethods(ConfigAdvancedDeps{})
	for _, name := range []string{
		"config.get",
		"config.set",
		"config.apply",
		"config.patch",
		"config.schema",
		"config.schema.lookup",
	} {
		if methods[name] == nil {
			t.Errorf("missing %s", name)
		}
	}
	rpctest.MustErr(t, rpctest.Call(methods, "config.set", map[string]any{"raw": ""}))
	rpctest.MustErr(t, rpctest.Call(methods, "config.apply", map[string]any{"raw": "{"}))
	rpctest.MustErr(t, rpctest.Call(methods, "config.patch", map[string]any{"raw": "not-json"}))
	rpctest.MustErr(t, rpctest.Call(methods, "config.schema.lookup", map[string]any{"path": ""}))
	rpctest.MustOK(t, rpctest.Call(methods, "config.schema", nil))
}

func TestPersistValidatedConfigRejectsMissingAndMalformedInputBeforeIO(t *testing.T) {
	if _, _, err := persistValidatedConfig("", ""); err == nil {
		t.Fatal("missing raw config must fail")
	}
	if _, _, err := persistValidatedConfig("{", ""); err == nil {
		t.Fatal("malformed raw config must fail")
	}
}

func TestPersistValidatedConfigCreatesPrivateAtomicConfig(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private", "nested")
	path := filepath.Join(dir, "deneb.json")
	t.Setenv("DENEB_CONFIG_PATH", path)
	raw := `{"gateway":{"bind":"loopback"}}`
	_, hash, err := persistValidatedConfig(raw, "")
	if err != nil {
		t.Fatal(err)
	}
	if hash != config.HashString(raw) {
		t.Fatalf("hash = %q, want %q", hash, config.HashString(raw))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != raw {
		t.Fatalf("config = %q, want %q", data, raw)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("config mode = %o, want 600", got)
	}
	if matches, err := filepath.Glob(path + ".*.tmp"); err != nil || len(matches) != 0 {
		t.Fatalf("temporary files = %#v, err=%v", matches, err)
	}
}

func TestConfigSetBroadcastsAndRejectsStaleBaseHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deneb.json")
	t.Setenv("DENEB_CONFIG_PATH", path)
	var event string
	var payload map[string]any
	methods := ConfigAdvancedMethods(ConfigAdvancedDeps{Broadcaster: func(gotEvent string, gotPayload events.EventPayload) (int, []error) {
		event = gotEvent
		_ = json.Unmarshal(gotPayload.Bytes(), &payload)
		return 1, nil
	}})
	raw := `{"gateway":{"bind":"loopback"}}`
	set := rpctest.Call(methods, "config.set", map[string]any{"raw": raw})
	rpctest.MustOK(t, set)
	result := rpctest.Result(t, set)
	if event != "config.changed" || payload["hash"] != result["hash"] {
		t.Fatalf("broadcast event=%q payload=%#v result=%#v", event, payload, result)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stale := rpctest.Call(methods, "config.set", map[string]any{
		"raw":      `{"gateway":{"bind":"lan"}}`,
		"baseHash": "stale-hash",
	})
	rpctest.MustErr(t, stale)
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("stale write changed config: before=%q after=%q", before, after)
	}
}

func TestConfigPatchMergesTopLevelObjectsAndBroadcastsContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deneb.json")
	t.Setenv("DENEB_CONFIG_PATH", path)
	initial := `{"gateway":{"bind":"loopback"}}`
	if err := writeConfigBytes(path, []byte(initial)); err != nil {
		t.Fatal(err)
	}
	var event string
	var payload map[string]any
	methods := ConfigAdvancedMethods(ConfigAdvancedDeps{Broadcaster: func(gotEvent string, gotPayload events.EventPayload) (int, []error) {
		event = gotEvent
		_ = json.Unmarshal(gotPayload.Bytes(), &payload)
		return 1, nil
	}})
	resp := rpctest.Call(methods, "config.patch", map[string]any{
		"raw":        `{"agents":{"defaults":{"workspace":"~/workspace"}}}`,
		"sessionKey": "client:main",
		"note":       "change workspace",
	})
	rpctest.MustOK(t, resp)
	if event != "config.patched" || payload["sessionKey"] != "client:main" || payload["note"] != "change workspace" {
		t.Fatalf("broadcast event=%q payload=%#v", event, payload)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var merged map[string]any
	if err := json.Unmarshal(data, &merged); err != nil {
		t.Fatal(err)
	}
	if merged["gateway"] == nil || merged["agents"] == nil {
		t.Fatalf("merged config = %#v", merged)
	}
}

func TestUpdateResultWritesSentinelAndRestartDirectiveOnlyOnSuccess(t *testing.T) {
	dir := t.TempDir()
	success := updateResult(updateResultOpts{
		reqID:          "success",
		ok:             true,
		status:         "ok",
		mode:           "make",
		beforeSHA:      "before",
		afterSHA:       "after",
		steps:          []updateStep{{Name: "build", OK: true}},
		startTime:      time.Now().Add(-time.Second),
		denebDir:       dir,
		restartDelayMs: 250,
	})
	rpctest.MustOK(t, success)
	result := rpctest.Result(t, success)
	if result["restart"].(map[string]any)["delayMs"] != float64(250) {
		t.Fatalf("restart = %#v", result["restart"])
	}
	sentinel := result["sentinel"].(map[string]any)
	data, err := os.ReadFile(sentinel["path"].(string))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["beforeSHA"] != "before" || payload["afterSHA"] != "after" {
		t.Fatalf("sentinel payload = %#v", payload)
	}

	failureDir := t.TempDir()
	failure := updateResult(updateResultOpts{
		reqID:          "failure",
		ok:             false,
		status:         "error",
		mode:           "git",
		startTime:      time.Now(),
		denebDir:       failureDir,
		restartDelayMs: 250,
	})
	rpctest.MustOK(t, failure)
	failureResult := rpctest.Result(t, failure)
	if failureResult["restart"] != nil || failureResult["sentinel"] != nil {
		t.Fatalf("failure result = %#v", failureResult)
	}
	if _, err := os.Stat(filepath.Join(failureDir, ".update-sentinel")); !os.IsNotExist(err) {
		t.Fatalf("failure sentinel stat error = %v", err)
	}
}

func TestRunStepCapturesSuccessFailureAndOutput(t *testing.T) {
	success := runStep(t.Context(), t.TempDir(), "say hello", "sh", "-c", "printf hello")
	if !success.OK || success.Log != "hello" || success.Command != "sh -c printf hello" {
		t.Fatalf("success step = %#v", success)
	}
	failure := runStep(t.Context(), t.TempDir(), "fail", "sh", "-c", "printf problem >&2; exit 7")
	if failure.OK || !strings.Contains(failure.Log, "problem") {
		t.Fatalf("failure step = %#v", failure)
	}
}

func TestRunGitRevReturnsBlankOutsideRepository(t *testing.T) {
	if got := runGitRev(t.TempDir()); got != "" {
		t.Fatalf("runGitRev = %q, want blank", got)
	}
}
