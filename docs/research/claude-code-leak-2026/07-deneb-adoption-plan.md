# Deneb Adoption Plan — Claude Code 기법 도입

> Claude Code 유출 분석에서 추출한 기법 중 Deneb에 도입할 항목 정리.
> 현재 Deneb 아키텍처와 비교하여 갭 분석 포함.

---

## 비교 요약: Deneb vs Claude Code

| 영역 | Deneb 현재 | Claude Code | 갭 |
|------|-----------|-------------|-----|
| 프롬프트 캐시 | 3블록 분리 + ephemeral marker | Dynamic boundary + tool schema cache | 소 |
| 메모리 관리 | 수동 MEMORY.md | autoDream 자동 통합 | **대** |
| 서브에이전트 | 기본 spawn/subagent | Coordinator 4단계 + scratchpad + task notification | **대** |
| 컨텍스트 오버플로 | 20K/파일 잘라내기 | Disk spillover + dedup + autocompaction | 중 |
| 프롬프트 모듈화 | .claude/rules/* glob 기반 | 110+ 조각 + 토큰 예산 | 중 |
| 도구 시스템 | 30+ 도구 | 40+ 도구 + LSP + worktree | 소 |
| 권한 모델 | 단일 사용자 (기본 허용) | 4단계 + ML classifier | 소 (불필요) |
| 감정 감지 | 없음 | Regex frustration detection | 소 |
| 세션 메모리 | 세션 상태 머신 | 구조화 마크다운 (10개 섹션) | 중 |
| Proactive Agent | 없음 | KAIROS (항시 실행) | 대 (미래) |

---

## 도입 우선순위

### P0 — 즉시 도입 (높은 영향 + 낮은 난이도)

#### 1. Tool Result Spillover
**현재**: 도구 결과가 크면 잘라냄
**목표**: 결과 > 임계값이면 디스크 저장 + 프리뷰만 컨텍스트에
**구현**:
- `gateway-go/internal/chat/tools/` 공통 래퍼에 spillover 로직 추가
- 임계값: 8,000 chars (대략 2K tokens)
- 저장 위치: `/tmp/deneb-results/{session_id}/{tool_call_id}`
- 프리뷰: 첫 500자 + `[Full result: /tmp/deneb-results/...]`

#### 2. File-Read Deduplication
**현재**: 매번 파일 전체 읽기
**목표**: 세션 내 동일 파일 mtime 미변경 시 캐시 반환
**구현**:
- `gateway-go/internal/chat/tools/fs.go` read 도구에 세션 캐시 추가
- 캐시 키: filepath + mtime
- 캐시 스코프: 세션 단위 (세션 종료 시 GC)

#### 3. Structured Session Memory
**현재**: 세션 상태만 (IDLE/RUNNING/DONE)
**목표**: 세션별 구조화 메모리 파일
**구현**:
- 세션 시작 시 구조화 메모리 초기화:
  ```
  # Session: [auto-generated title]
  ## Task / ## Files / ## Workflow / ## Errors / ## Learnings
  ```
- 세션 종료 시 learnings를 MEMORY.md에 전파

---

### P1 — 단기 도입 (높은 영향 + 중간 난이도)

#### 4. autoDream 메모리 통합
**현재**: MEMORY.md 수동 관리
**목표**: 자동 메모리 정리/통합/정리
**구현**:
- 새 패키지: `gateway-go/internal/dream/`
- 3-gate 트리거: 24시간 + 5세션 + 잠금
- 4-phase: Orient → Gather → Consolidate → Prune
- 200줄/25KB 제한
- LLM 호출로 통합 수행 (lightweight 모델 사용)
- 기존 cron 시스템에 dream job 등록

#### 5. 토큰 예산 시스템
**현재**: 하드코딩된 char 제한 (20K/파일, 150K 총)
**목표**: 프롬프트 조각별 토큰 수 추적 + 우선순위 기반 조합
**구현**:
- 각 프롬프트 조각에 estimated_tokens 메타데이터
- 총 예산 내에서 우선순위 순으로 조각 포함
- 예산 초과 시 낮은 우선순위부터 제거 (현재 스킬의 binary search와 유사)

---

### P2 — 중기 도입 (중간 영향 + 높은 난이도)

#### 6. Coordinator Mode
**현재**: 기본 서브에이전트 spawn
**목표**: Research→Synthesis→Implementation→Verification 파이프라인
**구현**:
- 세션 시스템에 coordinator 모드 추가
- Research: 병렬 워커로 코드베이스 탐색
- Synthesis: 메인 에이전트가 결과 종합
- Implementation: 워커에게 구체적 구현 지시
- Verification: 워커가 빌드/테스트 검증
- Shared scratchpad: `/tmp/deneb-scratchpad/{session_id}/`

#### 7. LSP 통합
**현재**: grep/find 기반
**목표**: gopls/rust-analyzer 연동
**구현**:
- 새 도구: `lsp` in `gateway-go/internal/chat/tools/lsp.go`
- Go: gopls (이미 시스템에 설치됨)
- Rust: rust-analyzer
- 지원: definition, references, call hierarchy, hover
- JSON-RPC over stdio

---

### P3 — 장기 / 참고만

#### 8. KAIROS (Proactive Agent)
Deneb의 단일 사용자 환경에서 매우 유용할 수 있으나 복잡도 높음.
참고만 하고 향후 별도 이슈로 진행.

#### 9. Buddy System
재미 요소로 고려 가능하나 우선순위 낮음.

#### 10. Anti-Distillation
단일 사용자 환경에서 불필요.

---

## Deneb에서 이미 더 잘하고 있는 것

1. **조건부 규칙 로딩** (`.claude/rules/*.md` + frontmatter globs)
   → Claude Code의 110+ 하드코딩 조각보다 유연함

2. **Vibe Coder 최적화** (코딩 채널 zero-code-exposure)
   → Claude Code에는 이런 사용자 특화가 없음

3. **세션 스냅샷** (context files frozen per session)
   → Claude Code도 유사하지만 Deneb의 구현이 더 명시적

4. **프롬프트 캐시 3블록 분리**
   → Claude Code와 동등 수준, 이미 잘 구현됨

5. **Rust FFI 코어** (성능 크리티컬 부분 Rust)
   → Claude Code는 전체 TypeScript, Deneb은 하이브리드로 성능 우위

---

## 구현 순서 (추천)

```
Week 1-2: P0 (Spillover + Dedup + Structured Memory)
Week 3-4: P1 (autoDream + Token Budget)
Week 5-8: P2 (Coordinator + LSP)
Beyond:   P3 (KAIROS 별도 계획)
```
