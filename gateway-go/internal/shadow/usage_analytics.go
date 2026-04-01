package shadow

import (
	"fmt"
	"strings"
	"time"
)

// UsageAnalytics tracks session patterns and usage trends.
type UsageAnalytics struct {
	svc *Service

	// State (guarded by svc.mu).
	sessionRuns     []sessionRun     // recent session runs for pattern analysis
	hourlyActivity  [24]int          // message count per hour (0-23)
	dailyTokens     map[string]int64 // date string → total tokens
	topicFrequency  map[string]int   // topic → count
	failurePatterns []failureRecord  // for error pattern learning
}

type sessionRun struct {
	Key       string `json:"key"`
	StartedAt int64  `json:"startedAt"`
	EndedAt   int64  `json:"endedAt"`
	Status    string `json:"status"` // "done", "failed", "timeout", "killed"
	DurationMs int64 `json:"durationMs"`
}

type failureRecord struct {
	Key       string `json:"key"`
	Ts        int64  `json:"ts"`
	Status    string `json:"status"`
}

// UsageReport is the analytics snapshot returned via RPC.
type UsageReport struct {
	TotalSessions    int            `json:"totalSessions"`
	SuccessRate      float64        `json:"successRate"`     // percentage
	AvgDurationMs    int64          `json:"avgDurationMs"`
	PeakHours        []int          `json:"peakHours"`       // top 3 most active hours
	TopTopics        []TopicStat    `json:"topTopics"`       // most frequent topics
	RecentFailures   int            `json:"recentFailures"`  // failures in last 24h
	DailyTokens      map[string]int64 `json:"dailyTokens,omitempty"`
}

// TopicStat is a topic + frequency pair.
type TopicStat struct {
	Topic string `json:"topic"`
	Count int    `json:"count"`
}

func newUsageAnalytics(svc *Service) *UsageAnalytics {
	return &UsageAnalytics{
		svc:            svc,
		dailyTokens:    make(map[string]int64),
		topicFrequency: make(map[string]int),
	}
}

// RecordSessionRun records a completed session run.
func (ua *UsageAnalytics) RecordSessionRun(key, status string, startedAt, endedAt int64) {
	run := sessionRun{
		Key:        key,
		StartedAt:  startedAt,
		EndedAt:    endedAt,
		Status:     status,
		DurationMs: endedAt - startedAt,
	}

	ua.svc.mu.Lock()
	defer ua.svc.mu.Unlock()

	ua.sessionRuns = append(ua.sessionRuns, run)
	// Keep last 500 runs.
	if len(ua.sessionRuns) > 500 {
		ua.sessionRuns = ua.sessionRuns[len(ua.sessionRuns)-500:]
	}

	if status == "failed" || status == "timeout" {
		ua.failurePatterns = append(ua.failurePatterns, failureRecord{
			Key: key, Ts: endedAt, Status: status,
		})
		if len(ua.failurePatterns) > 200 {
			ua.failurePatterns = ua.failurePatterns[len(ua.failurePatterns)-200:]
		}
	}
}

// RecordActivity logs activity at the current hour.
func (ua *UsageAnalytics) RecordActivity() {
	hour := time.Now().Hour()
	ua.svc.mu.Lock()
	ua.hourlyActivity[hour]++
	ua.svc.mu.Unlock()
}

// RecordTopic logs a topic occurrence.
func (ua *UsageAnalytics) RecordTopic(topic string) {
	if topic == "" {
		return
	}
	ua.svc.mu.Lock()
	ua.topicFrequency[topic]++
	ua.svc.mu.Unlock()
}

// GetReport generates a usage analytics report.
func (ua *UsageAnalytics) GetReport() UsageReport {
	ua.svc.mu.Lock()
	defer ua.svc.mu.Unlock()

	// Copy the map to avoid data race after lock release.
	dailyTokensCopy := make(map[string]int64, len(ua.dailyTokens))
	for k, v := range ua.dailyTokens {
		dailyTokensCopy[k] = v
	}
	report := UsageReport{
		TotalSessions: len(ua.sessionRuns),
		DailyTokens:   dailyTokensCopy,
	}

	if len(ua.sessionRuns) == 0 {
		return report
	}

	// Success rate.
	var successes int
	var totalDuration int64
	now := time.Now().UnixMilli()
	twentyFourHoursAgo := now - 24*60*60*1000

	for _, r := range ua.sessionRuns {
		if r.Status == "done" {
			successes++
		}
		totalDuration += r.DurationMs
	}
	report.SuccessRate = float64(successes) / float64(len(ua.sessionRuns)) * 100
	report.AvgDurationMs = totalDuration / int64(len(ua.sessionRuns))

	// Recent failures (24h).
	for _, f := range ua.failurePatterns {
		if f.Ts > twentyFourHoursAgo {
			report.RecentFailures++
		}
	}

	// Peak hours (top 3).
	type hourCount struct {
		hour  int
		count int
	}
	var hours []hourCount
	for h, c := range ua.hourlyActivity {
		if c > 0 {
			hours = append(hours, hourCount{h, c})
		}
	}
	// Simple sort for top 3.
	for i := 0; i < len(hours) && i < 3; i++ {
		for j := i + 1; j < len(hours); j++ {
			if hours[j].count > hours[i].count {
				hours[i], hours[j] = hours[j], hours[i]
			}
		}
	}
	for i := 0; i < len(hours) && i < 3; i++ {
		report.PeakHours = append(report.PeakHours, hours[i].hour)
	}

	// Top topics.
	type topicCount struct {
		topic string
		count int
	}
	var topics []topicCount
	for t, c := range ua.topicFrequency {
		topics = append(topics, topicCount{t, c})
	}
	for i := 0; i < len(topics) && i < 5; i++ {
		for j := i + 1; j < len(topics); j++ {
			if topics[j].count > topics[i].count {
				topics[i], topics[j] = topics[j], topics[i]
			}
		}
	}
	for i := 0; i < len(topics) && i < 5; i++ {
		report.TopTopics = append(report.TopTopics, TopicStat{
			Topic: topics[i].topic,
			Count: topics[i].count,
		})
	}

	return report
}

// FormatDailyDigest generates a Korean daily analytics summary.
func (ua *UsageAnalytics) FormatDailyDigest() string {
	report := ua.GetReport()
	var parts []string

	parts = append(parts, "📊 일일 사용 분석")
	parts = append(parts, fmt.Sprintf("  세션: %d회 (성공률 %.0f%%)", report.TotalSessions, report.SuccessRate))

	if report.AvgDurationMs > 0 {
		avgSec := report.AvgDurationMs / 1000
		parts = append(parts, fmt.Sprintf("  평균 소요: %d초", avgSec))
	}

	if len(report.PeakHours) > 0 {
		hourStrs := make([]string, len(report.PeakHours))
		for i, h := range report.PeakHours {
			hourStrs[i] = fmt.Sprintf("%d시", h)
		}
		parts = append(parts, fmt.Sprintf("  활발한 시간: %s", strings.Join(hourStrs, ", ")))
	}

	if len(report.TopTopics) > 0 {
		topicStrs := make([]string, 0, len(report.TopTopics))
		for _, t := range report.TopTopics {
			topicStrs = append(topicStrs, fmt.Sprintf("%s(%d)", t.Topic, t.Count))
		}
		parts = append(parts, fmt.Sprintf("  주요 주제: %s", strings.Join(topicStrs, ", ")))
	}

	if report.RecentFailures > 0 {
		parts = append(parts, fmt.Sprintf("  24시간 실패: %d건", report.RecentFailures))
	}

	return strings.Join(parts, "\n")
}
