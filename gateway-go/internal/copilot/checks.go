package copilot

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// --- Check: sglang health ---

func (s *Service) checkSglangHealth(ctx context.Context) CheckResult {
	name := "sglang_health"

	// Quick HTTP check against /v1/models.
	cmd := exec.CommandContext(ctx, "curl", "-sf", "--max-time", "5",
		s.cfg.SglangBaseURL+"/models")
	out, err := cmd.Output()
	if err != nil {
		return CheckResult{
			Name:    name,
			Status:  "critical",
			Message: "sglang 서버 응답 없음",
			Details: fmt.Sprintf("error: %v", err),
		}
	}

	return CheckResult{
		Name:    name,
		Status:  "ok",
		Message: "sglang 서버 정상",
		Details: truncate(string(out), 500),
	}
}

// --- Check: disk usage ---

func (s *Service) checkDiskUsage(ctx context.Context) CheckResult {
	name := "disk_usage"

	cmd := exec.CommandContext(ctx, "df", "-h", "/")
	out, err := cmd.Output()
	if err != nil {
		return CheckResult{
			Name:    name,
			Status:  "warning",
			Message: "디스크 사용량 확인 실패",
			Details: err.Error(),
		}
	}

	// Parse the usage percentage from df output.
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return CheckResult{Name: name, Status: "ok", Message: "디스크 정상", Details: string(out)}
	}

	fields := strings.Fields(lines[1])
	if len(fields) < 5 {
		return CheckResult{Name: name, Status: "ok", Message: "디스크 정상", Details: string(out)}
	}

	pctStr := strings.TrimSuffix(fields[4], "%")
	pct, _ := strconv.Atoi(pctStr)

	status := "ok"
	msg := fmt.Sprintf("디스크 사용량: %s%%", fields[4])
	if pct >= 95 {
		status = "critical"
		msg = fmt.Sprintf("디스크 거의 가득 참: %s%%", fields[4])
	} else if pct >= 85 {
		status = "warning"
		msg = fmt.Sprintf("디스크 사용량 높음: %s%%", fields[4])
	}

	return CheckResult{
		Name:    name,
		Status:  status,
		Message: msg,
		Details: string(out),
	}
}

// --- Check: GPU status ---

func (s *Service) checkGPUStatus(ctx context.Context) CheckResult {
	name := "gpu_status"

	cmd := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=name,temperature.gpu,utilization.gpu,memory.used,memory.total",
		"--format=csv,noheader,nounits")
	out, err := cmd.Output()
	if err != nil {
		return CheckResult{
			Name:    name,
			Status:  "warning",
			Message: "GPU 상태 확인 실패",
			Details: err.Error(),
		}
	}

	output := strings.TrimSpace(string(out))
	lines := strings.Split(output, "\n")
	status := "ok"
	var messages []string

	for _, line := range lines {
		fields := strings.Split(line, ", ")
		if len(fields) < 5 {
			continue
		}

		gpuName := strings.TrimSpace(fields[0])
		temp, _ := strconv.Atoi(strings.TrimSpace(fields[1]))
		memUsed, _ := strconv.ParseFloat(strings.TrimSpace(fields[3]), 64)
		memTotal, _ := strconv.ParseFloat(strings.TrimSpace(fields[4]), 64)

		memPct := 0.0
		if memTotal > 0 {
			memPct = (memUsed / memTotal) * 100
		}

		if temp >= 90 {
			status = "critical"
			messages = append(messages, fmt.Sprintf("%s: 온도 위험 %d°C", gpuName, temp))
		} else if temp >= 80 {
			if status != "critical" {
				status = "warning"
			}
			messages = append(messages, fmt.Sprintf("%s: 온도 높음 %d°C", gpuName, temp))
		}

		if memPct >= 95 {
			if status != "critical" {
				status = "warning"
			}
			messages = append(messages, fmt.Sprintf("%s: VRAM 사용량 높음 %.0f%%", gpuName, memPct))
		}
	}

	msg := "GPU 상태 정상"
	if len(messages) > 0 {
		msg = strings.Join(messages, "; ")
	}

	return CheckResult{
		Name:    name,
		Status:  status,
		Message: msg,
		Details: output,
	}
}

// --- Check: process health ---

func (s *Service) checkProcessHealth(ctx context.Context) CheckResult {
	name := "process_health"

	// Check that key processes are alive.
	processes := map[string]string{
		"deneb-gateway": "gateway",
		"sglang":        "sglang",
	}

	var missing []string
	for pattern, label := range processes {
		cmd := exec.CommandContext(ctx, "pgrep", "-f", pattern)
		if err := cmd.Run(); err != nil {
			missing = append(missing, label)
		}
	}

	if len(missing) > 0 {
		return CheckResult{
			Name:    name,
			Status:  "critical",
			Message: fmt.Sprintf("프로세스 미발견: %s", strings.Join(missing, ", ")),
		}
	}

	return CheckResult{
		Name:    name,
		Status:  "ok",
		Message: "핵심 프로세스 모두 정상",
	}
}

// --- Check: gateway logs (AI-powered analysis) ---

func (s *Service) checkGatewayLogs(ctx context.Context) CheckResult {
	name := "gateway_logs"

	logPath := "/tmp/deneb-gateway.log"

	// Read last 200 lines.
	data, err := os.ReadFile(logPath)
	if err != nil {
		return CheckResult{
			Name:    name,
			Status:  "ok",
			Message: "게이트웨이 로그 파일 없음 (정상일 수 있음)",
			Details: err.Error(),
		}
	}

	lines := strings.Split(string(data), "\n")
	start := 0
	if len(lines) > 200 {
		start = len(lines) - 200
	}
	recentLogs := strings.Join(lines[start:], "\n")

	// Count ERROR/WARN occurrences first (quick check without LLM).
	errorCount := strings.Count(strings.ToUpper(recentLogs), "ERROR")
	warnCount := strings.Count(strings.ToUpper(recentLogs), "WARN")

	if errorCount == 0 && warnCount == 0 {
		return CheckResult{
			Name:    name,
			Status:  "ok",
			Message: "최근 로그에 오류/경고 없음",
		}
	}

	// Use local LLM to analyze the log patterns.
	system := "You are a system log analyzer. Analyze the given gateway logs and identify:\n" +
		"1. Recurring error patterns\n" +
		"2. Potential issues that need attention\n" +
		"3. Whether errors are transient or persistent\n" +
		"Reply in Korean. Be concise (max 5 lines)."

	prompt := fmt.Sprintf("최근 게이트웨이 로그 (ERROR: %d건, WARN: %d건):\n\n%s",
		errorCount, warnCount, truncate(recentLogs, 8000))

	analysis, err := s.askLocalLLM(ctx, system, prompt)
	if err != nil {
		// Fallback to simple count-based result.
		s.log.Warn("copilot log analysis LLM call failed", "error", err)
		status := "ok"
		if errorCount > 10 {
			status = "warning"
		}
		return CheckResult{
			Name:    name,
			Status:  status,
			Message: fmt.Sprintf("로그: ERROR %d건, WARN %d건 (AI 분석 실패)", errorCount, warnCount),
		}
	}

	status := "ok"
	if errorCount > 10 {
		status = "warning"
	}

	return CheckResult{
		Name:    name,
		Status:  status,
		Message: fmt.Sprintf("로그: ERROR %d건, WARN %d건", errorCount, warnCount),
		Details: analysis,
	}
}

// truncate shortens a string to maxLen, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
