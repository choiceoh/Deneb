# Prompt Cache Doctrine

> **프롬프트 캐시는 불가침 영역이다.** Anthropic/OpenRouter의 `cache_control` 기반 prefix 캐시가 깨지면 Claude 요청당 입력 토큰 비용이 정가로 복귀한다 (캐시 히트 시 10% 수준). 코드베이스 전반에 걸쳐 이 원칙을 **강제**한다.

---

## 1. 3계층 캐시 구조 (현재 구현)

`gateway-go/internal/pipeline/chat/prompt/system_prompt.go:BuildSystemPromptBlocks`가 시스템 프롬프트를 세 블록으로 쪼개고 각각 `ephemeral` `cache_control` 마커를 부착한다:

| 계층 | 포함 내용 | 무효화 빈도 | 비고 |
|---|---|---|---|
| **Static** | identity, communication, attitude, tooling | 거의 없음 (툴셋 바뀔 때만) | 가장 크고 오래 캐시 |
| **Semi-static** | skills prompt | 스킬 추가/제거 시 | ~10-15K 토큰 |
| **Dynamic** | memory, messaging, context, workspace | 매 요청 | 캐시 X 영역 |

`gateway-go/internal/pipeline/chat/prompt/prompt_cache.go:PromptCache`가 static 블록을 **툴 이름 리스트 해시 키**로 캐싱해 재조립 비용도 제거한다.

### 캐시 histogram 확인

```bash
# 라이브 테스트 중 캐시 히트/미스 카운트 (Anthropic 응답 헤더)
scripts/dev/live-test.sh logs-grep "cache_read_input_tokens\|cache_creation_input_tokens"
```

---

## 2. 불가침 3원칙

### Rule A — **과거 메시지를 변경하지 마라**
- 이미 LLM에 전송된 메시지 content를 사후에 mutate 금지
- 유일한 예외: **컨텍스트 압축(compaction)**. 압축은 의도적으로 캐시 breaking point를 만든다
- 위반 예시 (금지):
  ```go
  // BAD — 과거 assistant 메시지에 추가 정보 주입
  messages[len(messages)-3].Content += "\n\nUpdate: ..."
  ```

### Rule B — **대화 중 툴셋을 바꾸지 마라**
- `BuildSystemPromptBlocks`는 static 블록 키를 **정렬된 툴 이름 리스트**로 생성. 툴 추가/제거는 static 캐시 무효화
- 대화 시작 후 `/tools` 조작이나 `toolreg` 재등록 금지 — 다음 세션부터 반영
- 위반 예시 (금지):
  ```go
  // BAD — 대화 중간에 툴셋 rebuild
  pipeline.Reconfigure(newToolset)  // 매 턴 static prompt 재생성됨
  ```

### Rule C — **시스템 프롬프트를 매 턴 재구성하지 마라**
- Memory reload, 컨텍스트 파일 refresh, timezone recheck 등이 매 요청마다 발화하면 Dynamic 블록 기반의 `cache_control`도 무력화
- `PromptCache.ContextFiles`는 mtime 기반 TTL로 이미 이 문제를 해결 — **이 캐시를 우회하거나 비활성화하지 말 것**
- 위반 예시 (금지):
  ```go
  // BAD — 매 요청 파일 재로드
  files := loadContextFilesDirectly(workspace)  // Cache 우회
  ```

---

## 3. Cache-aware 슬래시 커맨드

슬래시 커맨드가 시스템 프롬프트 state를 바꿔야 할 때는 **기본 deferred**, 명시적 `--now` 플래그로 즉시 invalidation opt-in.

### 패턴

```go
// 슬래시 "/<cmd>" 핸들러
func handleCmd(args []string) error {
    immediate := hasFlag(args, "--now")

    persistChange(args)  // 디스크/DB 쓰기

    if immediate {
        pipeline.InvalidateStaticCache("cmd-applied")
        return replyToUser("적용했습니다 (이번 세션 즉시 반영).")
    }
    return replyToUser("저장했습니다. 다음 세션부터 반영됩니다. 지금 바로 적용하려면 `/cmd --now`.")
}
```

### 대상 슬래시 예

- `/skills install <name>` — skill 추가는 semi-static 캐시 깸
- `/model <new>` — 모델 변경이 capability 힌트 바꾸면 static 캐시 영향
- `/personality <set>` — 페르소나는 dynamic 블록이면 캐시 영향 없음, 그러나 static 블록에 페르소나가 섞이면 영향

**판단 기준**: 슬래시 커맨드가 `system_prompt.go`의 Static/Semi-static 블록 생성 입력에 영향을 주면 cache-aware 처리 필수.

---

## 4. `/steer` — 캐시-안전 중간 개입

`/steer <note>` 는 실행 중인 에이전트 턴에 note를 주입하되 **기존 tool-role 메시지의 content에 append**하여 캐시를 깨지 않는다:

```
기존 메시지: [system, user, assistant(tool_call), tool(result)]
/steer "참고로 X는 무시해" → [system, user, assistant(tool_call), tool(result + "\n\n[사용자 조정: 참고로 X는 무시해]")]
```

Role alternation 유지 + content prefix 보존 → cache breakpoint까지의 prefix 동일.

구현 위치: `gateway-go/internal/pipeline/chat/steer.go` (또는 관련 파일). 마지막 tool-role 메시지가 없으면 pending 유지.

---

## 5. 컨텍스트 압축 — 유일한 예외

`internal/pipeline/chat/` 의 compaction은 의도적으로 과거 메시지를 요약/교체한다. **이것만이 Rule A의 유일한 공식 예외**.

### 압축 규약

- 요약된 영역에는 **SUMMARY_PREFIX**를 부착해 모델이 "요약에 답하지 않도록" 강제
- 권장 한국어 prefix: `"[컨텍스트 요약 — 참고 전용] 이 요약에 직접 답하지 마세요. 요약 뒤의 최신 사용자 메시지에만 응답하세요."`
- Head protect (최소 3 메시지: system, 첫 user, 첫 assistant) + Tail protect (최근 N 메시지) + Middle summarize
- **재압축 시 요약을 업데이트**(replace)하지 말고 이전 요약에 추가하거나 갱신

---

## 6. PR 체크리스트 (시스템 프롬프트/컨텍스트 관련)

새 코드가 `gateway-go/internal/pipeline/chat/prompt/` 나 context 생성 경로를 건드리면:

- [ ] Static/Semi-static/Dynamic 중 어느 블록에 영향? 문서화
- [ ] 새 입력이 static 블록에 들어가면 캐시 키에 반영됐는가?
- [ ] `PromptCache` 우회 경로 없는가?
- [ ] 대화 중간에 발화하는 코드인가? 그렇다면 반드시 Dynamic 블록만 건드리는가?
- [ ] 슬래시 커맨드라면 `--now` 플래그 없이 cache 깨지 않는가?
- [ ] 라이브 테스트로 `cache_read_input_tokens` 가 예상대로 올라가는지 확인
- [ ] `system_prompt_drift_test.go` 에 새 입력의 invariant 추가

---

## 7. 추가 레퍼런스

- 구현: `gateway-go/internal/pipeline/chat/prompt/system_prompt.go`, `prompt_cache.go`
- 압축 정책: `gateway-go/internal/pipeline/chat/` (compaction 관련 파일 — `merge_window.go` 참조)
- Hermes 설계 소스: [Hermes Agent 심층 분석 보고서](../docs/research/hermes-agent-analysis.md) § "프롬프트 캐시 신성화"
- Anthropic 공식 문서: [Prompt caching](https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching) — ephemeral TTL 5분

---

## 금지 (한 줄 요약)

- ❌ 과거 메시지 content 변경 (compaction 제외)
- ❌ 대화 중 툴셋 rebuild
- ❌ 매 요청 시스템 프롬프트 재구성
- ❌ `PromptCache` 우회
- ❌ static 블록에 per-request 변수 끼워넣기
- ❌ 슬래시 커맨드에서 `--now` 없이 캐시 무효화
