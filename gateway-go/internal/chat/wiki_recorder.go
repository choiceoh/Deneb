// wiki_recorder.go — Post-response wiki recording via 2 parallel RLM loops.
//
// After each successful agent run, two RLM loops fire in parallel:
//   - RLM-Diary: conversation turn loaded as REPL context → summarizes → diary_log()
//   - RLM-Curator: conversation turn + wiki index as context → wiki_search/wiki_write()
//
// Both run as background goroutines and never block the main response path.
// Architecture follows the RLM paper (Zhang et al. 2025): input is externalized
// into the REPL environment as a variable, and the LLM writes Starlark code to
// interact with it via wiki builtins.
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/rlm"
	"github.com/choiceoh/deneb/gateway-go/internal/rlm/repl"
	"github.com/choiceoh/deneb/gateway-go/internal/wiki"
)

// wikiRecorderDeps holds the dependencies for wiki recording RLMs.
type wikiRecorderDeps struct {
	store    *wiki.Store
	registry *modelrole.Registry
	logger   *slog.Logger
}

// spawnWikiRecorders fires two parallel RLM loops after a successful response.
// Called from handleRunSuccess; runs in a background goroutine.
func spawnWikiRecorders(ctx context.Context, rd wikiRecorderDeps, userMsg, assistantResp string, toolNames []string) {
	if rd.store == nil || rd.registry == nil {
		return
	}

	// Resolve model: try lightweight (local vLLM), then fallback.
	client, model := resolveRecorderModel(rd.registry)
	if client == nil || model == "" {
		rd.logger.Warn("wiki-recorder: no model available for recording, skipping")
		return
	}

	// Build the conversation turn data that gets loaded into the REPL as context.
	turnData := buildTurnData(userMsg, assistantResp, toolNames)

	var wg sync.WaitGroup
	wg.Add(2)

	// RLM-1: Diary recorder.
	go func() {
		defer wg.Done()
		runDiaryRLM(ctx, rd, client, model, turnData)
	}()

	// RLM-2: Wiki curator.
	go func() {
		defer wg.Done()
		runCuratorRLM(ctx, rd, client, model, turnData)
	}()

	wg.Wait()
	rd.logger.Debug("wiki-recorder: both RLM loops completed")
}

// buildTurnData formats the conversation turn into a structured string
// that gets loaded as a REPL variable.
func buildTurnData(userMsg, assistantResp string, toolNames []string) string {
	var sb strings.Builder
	sb.WriteString("## 사용자 메시지\n")
	sb.WriteString(truncateStr(userMsg, 3000))
	sb.WriteString("\n\n## 응답\n")
	sb.WriteString(truncateStr(assistantResp, 5000))
	if len(toolNames) > 0 {
		sb.WriteString("\n\n## 사용된 도구\n")
		sb.WriteString(strings.Join(toolNames, ", "))
	}
	return sb.String()
}

// runDiaryRLM executes the diary recording RLM loop.
// The LLM reads the turn data from the REPL context variable,
// summarizes it, and calls diary_log() to append to today's diary.
func runDiaryRLM(ctx context.Context, rd wikiRecorderDeps, client *llm.Client, model, turnData string) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Build REPL with turn data as a context variable and diary_log builtin.
	wikiFuncs := buildWikiFuncs(rd.store)
	replEnv := repl.NewEnv(ctx, repl.EnvConfig{
		Messages: []repl.MessageEntry{
			{Seq: 0, Role: "system", Content: turnData},
		},
		Wiki:    wikiFuncs,
		Timeout: 10 * time.Second,
	})

	system := llm.SystemString(diarySystemPrompt)

	userPrompt := fmt.Sprintf(
		"context 변수에 대화 턴 데이터가 있다. 길이: %d자.\n"+
			"1. context를 읽어서 핵심을 파악하라\n"+
			"2. diary_log()로 1~3줄 한국어 요약을 기록하라\n"+
			"3. FINAL('done')으로 종료하라",
		len(turnData))

	result, err := rlm.RunLoop(ctx, rlm.LoopConfig{
		Client:          client,
		Model:           model,
		System:          system,
		MaxTokens:       1024,
		MaxIter:         3,
		MaxConsecErrors: 2,
		FallbackEnabled: false,
		REPLEnv:         replEnv,
		Logger:          rd.logger.With("rlm", "diary"),
	}, userPrompt)

	if err != nil {
		rd.logger.Warn("wiki-recorder: diary RLM failed", "error", err)
		return
	}

	rd.logger.Debug("wiki-recorder: diary RLM completed",
		"iterations", result.Iterations,
		"stop_reason", result.StopReason)
}

// runCuratorRLM executes the wiki curation RLM loop.
// The LLM reads the turn data, checks the wiki index for existing pages,
// and creates or updates wiki pages for structured knowledge.
func runCuratorRLM(ctx context.Context, rd wikiRecorderDeps, client *llm.Client, model, turnData string) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	// Build REPL with turn data as context and full wiki builtins.
	wikiFuncs := buildWikiFuncs(rd.store)
	replEnv := repl.NewEnv(ctx, repl.EnvConfig{
		Messages: []repl.MessageEntry{
			{Seq: 0, Role: "system", Content: turnData},
		},
		Wiki:    wikiFuncs,
		Timeout: 15 * time.Second,
	})

	system := llm.SystemString(curatorSystemPrompt)

	userPrompt := fmt.Sprintf(
		"context 변수에 대화 턴 데이터가 있다. 길이: %d자.\n"+
			"1. context를 읽어서 장기 보존할 지식이 있는지 판단하라\n"+
			"2. wiki_index()로 기존 위키 페이지 목록을 확인하라\n"+
			"3. 새 지식이 있으면 wiki_write()로 기록하라 (기존 페이지와 중복 금지)\n"+
			"4. 없으면 바로 FINAL('no_new_knowledge')\n"+
			"카테고리: 사람, 프로젝트, 기술, 업무, 결정, 선호",
		len(turnData))

	result, err := rlm.RunLoop(ctx, rlm.LoopConfig{
		Client:          client,
		Model:           model,
		System:          system,
		MaxTokens:       2048,
		MaxIter:         5,
		MaxConsecErrors: 2,
		FallbackEnabled: false,
		REPLEnv:         replEnv,
		Logger:          rd.logger.With("rlm", "curator"),
	}, userPrompt)

	if err != nil {
		rd.logger.Warn("wiki-recorder: curator RLM failed", "error", err)
		return
	}

	rd.logger.Debug("wiki-recorder: curator RLM completed",
		"iterations", result.Iterations,
		"stop_reason", result.StopReason)
}

// appendDiaryEntry appends a timestamped entry to today's diary file.
func appendDiaryEntry(diaryDir, content string) error {
	if content == "" {
		return fmt.Errorf("empty diary content")
	}
	if err := os.MkdirAll(diaryDir, 0o755); err != nil {
		return fmt.Errorf("diary dir: %w", err)
	}

	today := time.Now().Format("2006-01-02")
	path := filepath.Join(diaryDir, "diary-"+today+".md")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("diary open: %w", err)
	}
	defer f.Close()

	now := time.Now().Format("15:04")
	entry := fmt.Sprintf("\n## %s\n\n%s\n", now, content)
	_, err = f.WriteString(entry)
	return err
}

// resolveRecorderModel picks the best available model for wiki recording.
// Tries lightweight (vLLM) first with auto-detection, then falls back.
func resolveRecorderModel(reg *modelrole.Registry) (*llm.Client, string) {
	// Try lightweight model (local vLLM).
	client := reg.Client(modelrole.RoleLightweight)
	model := reg.Model(modelrole.RoleLightweight)
	if client != nil && model != "" {
		// Probe vLLM for actual available model (the configured model name
		// may not match what's loaded on the GPU).
		if detected := probeVllmModel(reg.BaseURL(modelrole.RoleLightweight)); detected != "" {
			return client, detected
		}
	}

	// Fallback to the fallback role (e.g., Gemini).
	client = reg.Client(modelrole.RoleFallback)
	model = reg.Model(modelrole.RoleFallback)
	if client != nil && model != "" {
		return client, model
	}

	return nil, ""
}

// probeVllmModel queries the vLLM /v1/models endpoint to find the actual loaded model.
func probeVllmModel(baseURL string) string {
	if baseURL == "" {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/models", nil)
	if err != nil {
		return ""
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ""
	}
	if len(result.Data) > 0 {
		return result.Data[0].ID
	}
	return ""
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// --- System Prompts ---

const diarySystemPrompt = `너는 대화 일지 기록자다. REPL 환경에서 Starlark 코드를 실행한다.

환경:
- context: 대화 턴 데이터 (사용자 메시지 + 응답 + 도구 사용)
- diary_log(content): 오늘 일지에 timestamped 엔트리를 추가하는 함수
- FINAL(answer): 작업 완료 시 호출

절차:
1. context 변수에서 대화 내용을 확인
2. 핵심을 1~3줄 한국어로 요약
3. diary_log("요약 내용")으로 기록
4. FINAL("done")

코드를 ` + "```repl" + ` 블록으로 작성하라. 한 번에 끝내라.`

const curatorSystemPrompt = `너는 위키 큐레이터다. REPL 환경에서 Starlark 코드를 실행하여 대화에서 장기 보존할 지식을 추출하고 위키에 기록한다.

환경:
- context: 대화 턴 데이터
- wiki_index(category=""): 기존 위키 인덱스 조회 (카테고리별 또는 전체)
- wiki_search(query, limit=10): 위키 전문 검색
- wiki_read(path): 위키 페이지 읽기
- wiki_write(path, content): 위키 페이지 생성/업데이트
- FINAL(answer): 작업 완료 시 호출

카테고리: 사람, 프로젝트, 기술, 업무, 결정, 선호

절차:
1. context를 읽어서 장기 보존할 지식 판별
2. 잡담, 인사, 단순 질의응답 → FINAL("no_new_knowledge")
3. 새 지식이 있으면 wiki_index()로 기존 페이지 확인
4. 같은 주제 페이지가 있으면 wiki_read() → 내용 병합 → wiki_write()
5. 없으면 wiki_write("카테고리/제목.md", "frontmatter + 내용")
6. FINAL("recorded: 경로")

페이지 포맷:
---
title: 제목
category: 카테고리
tags: [태그1, 태그2]
importance: 0.5
---
# 제목

내용...

중복 페이지를 만들지 마라. 하나의 주제는 하나의 페이지.
코드를 ` + "```repl" + ` 블록으로 작성하라.`
