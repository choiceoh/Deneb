# Vega 변경 이력 (Changelog)

## vega v1.48 — Deneb(OpenClaw) 호환성 강화

### 핵심: 상위 프로젝트 Deneb와의 연결성 및 호환성 강화. 버전 협상, 검색 모드 제어, 기능 프로빙 지원.

### 신규 기능

| #   | 항목                                                    | 파일                        |
| --- | ------------------------------------------------------- | --------------------------- |
| 1   | `memory-version` 명령 추가 — 경량 버전/기능 프로빙     | `commands/memory.py`        |
| 2   | `memory-status` 확장 — 버전, capabilities, 모델 가용성  | `commands/memory.py`        |
| 3   | `memory-search --mode` 지원 — search/vsearch/query 모드 | `commands/memory.py`        |
| 4   | `config.VERSION`, `config.PROTOCOL_VERSION` 추가        | `config.py`                 |
| 5   | MCP 스키마에 `version` 도구 추가                        | `mcp-vega.json`             |

### memory-version 응답

```json
{
  "version": "1.48",
  "protocolVersion": 1,
  "capabilities": {
    "semanticSearch": true,
    "reranking": true,
    "searchModes": ["search", "vsearch", "query"],
    "memoryCommands": ["memory-search", "memory-update", "memory-embed", "memory-status", "memory-version"]
  }
}
```

### memory-search --mode

| 모드       | 동작                              | 속도   | 재현율 |
| ---------- | --------------------------------- | ------ | ------ |
| `search`   | FTS5만 사용                       | 빠름   | 낮음   |
| `vsearch`  | 벡터 검색만 사용                  | 보통   | 보통   |
| `query`    | FTS5 + 벡터 + 리랭킹 (기본)      | 느림   | 높음   |

### Deneb(OpenClaw) 측 변경

| #   | 항목                                                              | 파일                          |
| --- | ----------------------------------------------------------------- | ----------------------------- |
| 1   | `VegaMemoryManager` — 초기화 시 `memory-version` 프로빙          | `src/memory/vega-manager.ts`  |
| 2   | `VegaMemoryManager.search()` — `--mode` 플래그 전달              | `src/memory/vega-manager.ts`  |
| 3   | `VegaMemoryManager` — 사용자 env 변수 서브프로세스 전달          | `src/memory/vega-manager.ts`  |
| 4   | `ResolvedVegaConfig` — `searchMode`, `env` 필드 추가             | `src/memory/backend-config.ts`|
| 5   | `MemoryVegaConfig` 타입 — `searchMode`, `env` 필드 추가          | `src/config/types.memory.ts`  |
| 6   | Zod 스키마 — `searchMode`, `env` 검증 추가                       | `src/config/zod-schema.ts`    |
| 7   | `VegaCapabilities`, `VegaVersionInfo` 타입 export                | `src/memory/vega-manager.ts`  |
| 8   | MCP 스키마에 `version` 도구 추가                                 | `mcp-vega.json`               |

### 하위 호환성

- `memory-version` 미지원 Vega(v1.47 이하)에 대해 `memory-status`로 폴백
- `--mode` 플래그는 이전 버전에서 무시됨 (기존 동작 유지)
- `memory-status` 응답에 추가된 필드는 선택적 — 기존 Deneb 코드도 정상 동작
- SCHEMA_VERSION 변경 없음 (여전히 6)

---

## vega v1.47 — QMD 외부 의존성 완전 제거

### 핵심: QMD(외부 Node.js 의미 검색 도구) 흔적 완전 제거, 로컬 모델(LocalAdapter) 전용 아키텍처로 전환

v1.4에서 LocalAdapter가 QMD를 대체했으나 코드/설정/스크립트에 QMD 참조가 남아 있었음. 이번 버전에서 모두 제거.

### 제거된 항목

| #   | 항목                                                                    | 파일                   |
| --- | ----------------------------------------------------------------------- | ---------------------- |
| 1   | `commands/qmd_ops.py` 전체 삭제 (qmd-status, qmd-index 명령)            | `commands/qmd_ops.py`  |
| 2   | `QMDAdapter` 클래스 전체 삭제 (~200줄)                                  | `router.py`            |
| 3   | `qmd/` 디렉토리 삭제 (qmd-wrapper.sh, config/, cache/, models/)         | `qmd/`                 |
| 4   | `QMD_BIN`, `QMD_WRAPPER`, `get_qmd_executable()`, `_check_executable()` | `config.py`, `core.py` |
| 5   | `_find_qmd_wrapper()` 셸 함수                                           | `_lib.sh`              |
| 6   | bootstrap.sh QMD wrapper 경로 확인 단계                                 | `bootstrap.sh`         |
| 7   | sync-db.sh `qmd_sync()` 함수 (QMD 인덱스 동기화)                        | `sync-db.sh`           |
| 8   | install.sh QMD 설정/wrapper 복사 전체                                   | `install.sh`           |
| 9   | setup.sh QMD 연결 확인 단계 + QMD wrapper 교체 안내                     | `setup.sh`             |
| 10  | mcp-vega.json `QMD_WRAPPER` 환경변수                                    | `mcp-vega.json`        |
| 11  | `TestQmdOpsNewActions` 테스트 클래스                                    | `test_vega.py`         |

### 이름 변경 (source label 통일)

| 이전                                               | 이후                                              | 영향                                              |
| -------------------------------------------------- | ------------------------------------------------- | ------------------------------------------------- |
| `source='qmd'`                                     | `source='semantic'`                               | `router.py`, `commands/search.py`, `test_vega.py` |
| `self.qmd`                                         | `self.semantic`                                   | `SearchRouter` 인스턴스 변수                      |
| `qmd_available/qmd_used/qmd_count`                 | `semantic_available/semantic_used/semantic_count` | `search_meta` 키                                  |
| `route='qmd'`                                      | `route='semantic'`                                | `analyze_query()` 반환값                          |
| `_infer_qmd_project_name`                          | `_infer_semantic_project_name`                    | `router.py` 내부 함수                             |
| `_score_qmd_results`                               | `_score_semantic_results`                         | `router.py` 내부 함수                             |
| `_qmd_items_to_unified`                            | `_semantic_items_to_unified`                      | `router.py` 내부 함수                             |
| `INFERENCE_BACKEND: 'local'\|'qmd'\|'sqlite_only'` | `'local'\|'sqlite_only'`                          | `config.py`                                       |

### 경로 정리

| 이전                                                          | 이후                                            |
| ------------------------------------------------------------- | ----------------------------------------------- |
| `~/.openclaw/agents/main/qmd/xdg-data/qmd/knowledge/projects` | `~/.openclaw/agents/main/knowledge/projects`    |
| `~/.cache/qmd/models` (모델 탐색 후보)                        | 제거 (환경변수 → `~/.vega/models` → `./models`) |

### 하위 호환성

- 외부 API 응답의 `source` 필드가 `'qmd'` → `'semantic'`으로 변경 (MCP 소비자 영향 가능)
- `INFERENCE_BACKEND='qmd'` 설정은 더 이상 작동하지 않음 (`'local'` 또는 `'sqlite_only'` 사용)
- SCHEMA_VERSION 변경 없음 (여전히 6)

---

## vega v1.46 — 기술 부채 정리

### 버그 수정 (v1.45 핫픽스 포함)

| #   | 수정                                                     | 파일                  |
| --- | -------------------------------------------------------- | --------------------- |
| 1   | `_auto_correct_depth` import-by-value → 모듈 참조로 수정 | `commands/ask.py`     |
| 2   | `upgrade` 명령에서 중복 `init_db` 호출 제거              | `commands/upgrade.py` |
| 3   | `_find_model` wildcard fallback 경로 오류 수정           | `config.py`           |
| 4   | `search` match_reasons 과잉 매칭 (`len >= 2` 가드)       | `commands/search.py`  |
| 5   | `core._ensure_db` 빈 person 문자열 처리                  | `core.py`             |

### 리팩토링

| #   | 변경                                                                                                                                 | 영향 파일                                                                            |
| --- | ------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ |
| 1   | **DB_PATH 모듈 레벨 바인딩 제거** — `from config import DB_PATH` 대신 `config.DB_PATH` 호출 시점 참조로 통일 (테스트 fixture 안정성) | `md_editor.py`, `mail_to_md.py`, `project_db_v2.py`, `router.py`                     |
| 2   | **커넥션 누수 수정** — `import_files()`, `import_incremental()` try/finally 래핑                                                     | `project_db_v2.py`                                                                   |
| 3   | **import 표준화** — 함수 내 `import config` 5건 → 모듈 레벨 `import config as _cfg` 통합                                             | `models.py`                                                                          |
| 4   | **공유 upsert 로직 추출** — `upsert_md_file()`, `delete_project_by_source()` 헬퍼로 ~160줄 중복 제거                                 | `project_db_v2.py`, `commands/upgrade.py`                                            |
| 5   | **silent error handler 로깅 추가** — 실패 원인 추적을 위해 `except: pass` → `logging.warning/debug`                                  | `core.py`, `mail_to_md.py`, `md_editor.py`, `project_db_v2.py`, `commands/memory.py` |

### 하위 호환성

- 외부 API/CLI 변경 없음
- SCHEMA_VERSION 변경 없음 (여전히 6)
- 모든 함수 시그니처 하위 호환 (`db_path=None` 기본값은 기존 `DB_PATH`와 동일하게 동작)

---

## vega v1.45 — Upgrade 명령 + 모델 자동 감지

### 새 기능

| #   | 기능               | 설명                                                                                                                           |
| --- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------ |
| 1   | **upgrade 명령**   | `python vega.py upgrade [--force]` — 스키마 마이그레이션 + .md 재파싱 + memory 갱신 + 임베딩 + FTS 정합성을 단일 명령으로 처리 |
| 2   | **모델 자동 감지** | `VEGA_MODELS_DIR` 환경변수 → `~/.vega/models/` → `~/.cache/qmd/models/` → `./models` 순으로 GGUF 모델 자동 탐색                |
| 3   | **모델 패턴 매칭** | 정확한 파일명 없어도 `*embedding*.gguf`, `*reranker*.gguf` 등 glob 패턴으로 호환 모델 자동 발견                                |

### 설정

- `VEGA_MODELS_DIR`: 모델 디렉토리 경로 (환경변수, 최우선)
- 기존 `VEGA_MODEL_EMBEDDER`, `VEGA_MODEL_RERANKER`, `VEGA_MODEL_EXPANDER` 환경변수도 계속 동작
- upgrade 결과에 `models` 섹션 포함 (감지된 모델 경로 + 디렉토리)

---

## vega v1.44 — Memory Backend for OpenClaw

### 핵심: OpenClaw이 Vega를 memory backend로 사용할 수 있는 4개 CLI 명령 추가

### 새 기능

| #   | 기능                      | 설명                                                                             |
| --- | ------------------------- | -------------------------------------------------------------------------------- |
| 1   | **memory-search**         | 하이브리드 검색 (FTS5 + 벡터 + 리랭킹) → bare JSON 배열 (`MemorySearchResult[]`) |
| 2   | **memory-update**         | workspace의 .md 파일 스캔 → 증분 인덱싱 (해시 기반), `--force` 전체 재인덱싱     |
| 3   | **memory-embed**          | 미임베딩 청크에 벡터 임베딩 생성, `--force` 재임베딩                             |
| 4   | **memory-status**         | memory 파일/청크/임베딩 카운트 + 모델 정보 반환                                  |
| 5   | **heading 기반 .md 파서** | `_parse_memory_md()` — h1~h3 heading 기준 청크 분할, 라인 번호 포함              |

### 출력 규약 (기존 Vega 명령과 다름)

- **stdout**: bare JSON (envelope 없음)
- **stderr**: 에러 메시지
- **exit code**: 0=성공, 1=에러
- memory-search → `[{path, startLine, endLine, score, snippet, source}]`
- memory-status → `{files, chunks, embedded, model, dbPath}`
- memory-update/embed → exit code만 (성공 시 stdout 없음 가능)

### 스키마 변경 (v6)

- `chunks` 테이블: `start_line INTEGER`, `end_line INTEGER` 추가
- `projects` 테이블: `source_type TEXT DEFAULT 'project'` 추가
- 기존 데이터는 자동 마이그레이션 (NULL/`'project'` 기본값)

### 설정

- `VEGA_MEMORY_PATHS`: 콤마 구분 경로 목록 (기본: `MEMORY.md,memory,projects`)
- `VEGA_MEMORY_WORKSPACE`: workspace 루트 (기본: MD_DIR 부모)
- `vector_search()`에 `source_type` 필터 추가 (하위 호환)

### 하위 호환성

- 기존 커맨드 동작 변경 없음
- `vector_search()` 기존 호출은 `source_type=None`으로 전체 검색 유지
- SCHEMA_VERSION 5→6 자동 마이그레이션

---

## vega v1.42 — "개떡같이 써도 찰떡같이" AI 입력 관용성 대폭 강화

### 핵심: AI가 대충 질문해도 항상 유용한 응답을 반환하도록 퍼지 매칭, 스마트 라우팅, 자동 복구 레이어 추가

### 새 기능

| #   | 기능                   | 설명                                                                                               |
| --- | ---------------------- | -------------------------------------------------------------------------------------------------- |
| 1   | **퍼지 프로젝트 매칭** | `_fuzzy_find_project()` — LIKE 실패 시 difflib.SequenceMatcher로 폴백. "비금또"→"비금도" 자동 보정 |
| 2   | **스마트 라우팅**      | `_smart_route()` — 신뢰도 낮은 search 라우팅에서 프로젝트명 감지 시 brief로 자동 전환              |
| 3   | **쿼리 정규화**        | `_normalize_query()` — 의미없는 접미사(해줘/알려줘/뭐야), 문장부호 자동 제거                       |
| 4   | **검색 0건 자동 복구** | 검색 결과 없을 때 fuzzy 매칭으로 `_auto_brief` 자동 첨부                                           |
| 5   | **match_reasons**      | 검색 결과에 매칭 사유 표시 (프로젝트명/본문/의미검색/커뮤니케이션)                                 |
| 6   | **NL_ROUTES 확장**     | 비용/예산→pipeline, 마감/납기→urgent, 일정/스케줄→timeline 등 7개 패턴 추가                        |
| 7   | **\_ai_hint 확장**     | show, list, dashboard, timeline, pipeline, fuzzy_matched 상황 가이드 추가                          |
| 8   | **\_bundle 확장**      | urgent→top brief, show→같은 발주처 프로젝트 번들                                                   |
| 9   | **auto-correct 확장**  | 패턴 4: \_auto_brief→brief 전환, 패턴 5: timeline fuzzy 재시도                                     |

### 변경사항

- `ask` 명령이 `_smart_route()` 사용 (기존 `route_input()` 대신)
- `brief` 명령에 fuzzy 프로젝트 매칭 폴백 추가
- `require_project()`에 `fuzzy=True` 옵션 추가
- `_find_project_id_in_text()`에 fuzzy 폴백 추가
- MCP 도구 description 간소화 ("자연어로 아무거나 물어보세요")
- CLAUDE.md 입력 관용성 섹션 추가

### 하위 호환성

- 기존 커맨드 삭제 없음 (15개 전부 유지)
- 기존 응답 필드 변경 없음 (새 필드만 추가)
- `_find_project_id()` 기존 동작 그대로 유지 (fuzzy는 별도 함수)

---

## vega v1.41 — 로컬 모델 안정성 강화 + 엣지케이스 버그 수정

### 핵심: v1.4 로컬 AI 모델의 안정성 및 router.py 호환성 개선, 37개 엣지케이스 테스트 추가

### 버그 수정

| #   | 버그                                                                                  | 수정                                                          |
| --- | ------------------------------------------------------------------------------------- | ------------------------------------------------------------- |
| 1   | `LocalAdapter.search()` 임베딩 실패 시 `None` 반환 → router.py에서 NoneType 에러      | `[]` (빈 리스트) 반환으로 통일                                |
| 2   | `_results_to_items()` metadata에 `uri`, `best_chunk_pos`, `filter_bypassed` 필드 누락 | router.py `_qmd_items_to_unified()`가 기대하는 전체 필드 추가 |
| 3   | `_call_embed()` 빈 리스트/빈 내부 리스트 반환 시 silent failure                       | RuntimeError 발생으로 명시적 실패                             |
| 4   | `_blob_to_vector()` 빈/손상 BLOB 크래시                                               | 빈 배열 반환 + 4바이트 정렬 처리                              |
| 5   | `vector_search()` NaN/Inf query_vec 크래시                                            | `np.isfinite()` 검증 추가                                     |
| 6   | `vector_search()` 벡터 파싱 실패 시 로깅 없음                                         | `_log.debug()` 추가                                           |
| 7   | `LocalReranker.rerank()` query=None/빈 문자열 크래시                                  | 빈 쿼리 → 전부 0.0 점수 반환                                  |
| 8   | `_extract_yes_logprob()` 비숫자 logprob → TypeError                                   | `isinstance(lp, (int, float))` 검증                           |
| 9   | `status()` TOCTOU (os.path.isfile → os.path.getsize 사이 파일 삭제)                   | try/except OSError 래핑                                       |
| 10  | `ModelManager.__init__()` lock 없이 `_initialized` 체크                               | `_lock` 내부로 이동                                           |
| 11  | `LocalExpander.expand()` 매우 긴 쿼리 → 프롬프트 오버플로우                           | 500자 제한                                                    |
| 12  | `embed_all_chunks()` batch_size=0/음수 → 무한루프                                     | 기본값 32로 폴백                                              |
| 13  | `embed_single()` 빈 배열 반환 시 `.size` 미검증                                       | `vec.size > 0` 추가                                           |
| 14  | `LocalAdapter.search()` `project_filter`에 None 포함 시 에러                          | None 요소 필터링                                              |
| 15  | `rerank()` 프롬프트에 query/doc 길이 제한 없음                                        | query 1000자, doc 1500자 제한                                 |

### 테스트 추가 (37개 신규, 총 188개)

| 클래스                        | 테스트 수 | 내용                                                              |
| ----------------------------- | --------- | ----------------------------------------------------------------- |
| `TestCallEmbedEdgeCases`      | 4         | 빈 리스트, 빈 내부 리스트, 빈 data, 빈 벡터                       |
| `TestBlobEdgeCases`           | 5         | 빈/None/3바이트/비정렬 BLOB, 리스트 입력                          |
| `TestVectorSearchEdgeCases`   | 4         | NaN/Inf/빈 query, 손상 BLOB in DB                                 |
| `TestLocalRerankerEdgeCases`  | 5         | None/빈 query, 비숫자 logprob, 긴 query                           |
| `TestLocalExpanderEdgeCases`  | 3         | 긴 query, 비문자열, 유니코드/특수문자 키워드                      |
| `TestLocalEmbedderEdgeCases`  | 3         | 영벡터, embed 실패, 유니코드 텍스트                               |
| `TestLocalAdapterEdgeCases`   | 6         | None vs [], metadata 완전성, 빈 필터, None 필터                   |
| `TestModelManagerEdgeCases`   | 4         | 알 수 없는 role, 모델 없는 status, double reset, 없는 role unload |
| `TestEmbedAllChunksEdgeCases` | 2         | batch_size=0, batch_size=-1                                       |

---

## vega v1.4 — 로컬 AI 모델 내장 (쿼리확장 + 리랭커 + 임베딩)

### 핵심: 세 개의 GGUF 모델을 로컬에서 직접 로드하여 QMD 외부 의존성 제거

### 사용 모델

| 모델               | 용도                                | 양자화 |
| ------------------ | ----------------------------------- | ------ |
| Qwen3.5-9B         | 쿼리 확장 (동의어/관련 키워드 생성) | Q4_K_M |
| Qwen3-Reranker-4B  | cross-encoder 리랭킹                | 기본   |
| Qwen3-Embedding-8B | 벡터 임베딩 (코사인 유사도 검색)    | Q4_K_M |

### 새 파일

| 파일        | 역할                                                                                                     |
| ----------- | -------------------------------------------------------------------------------------------------------- |
| `models.py` | ModelManager, LocalEmbedder, LocalReranker, LocalExpander, LocalAdapter, vector_search, embed_all_chunks |

### 변경 파일

| 파일                  | 변경                                                                                                  |
| --------------------- | ----------------------------------------------------------------------------------------------------- |
| `config.py`           | MODELS_DIR, MODEL_EXPANDER/EMBEDDER/RERANKER, MODEL_UNLOAD_TTL, INFERENCE_BACKEND, SCHEMA_VERSION 4→5 |
| `project_db_v2.py`    | chunk_embeddings 테이블 + chunks_emb_ad 삭제 트리거 + v5 마이그레이션                                 |
| `router.py`           | SearchRouter.**init**: INFERENCE_BACKEND에 따라 LocalAdapter/QMDAdapter/NullAdapter 선택              |
| `commands/qmd_ops.py` | embed-local (로컬 모델로 임베딩 일괄 생성), model-status (모델 파일/로드 상태 조회) 액션 추가         |
| `test_vega.py`        | 52개 모델 관련 테스트 추가 (총 151개)                                                                 |

### 아키텍처

- `INFERENCE_BACKEND` 설정: `local` (기본, 로컬 모델) / `qmd` (기존 QMD) / `sqlite_only` (벡터 검색 없음)
- ModelManager: 싱글톤, lazy loading, TTL 자동 해제 (기본 300초), thread-safe
- LocalAdapter: QMDAdapter와 동일 인터페이스 (drop-in replacement)
- chunk_embeddings: BLOB 저장, chunks 삭제 시 트리거로 자동 정리
- 벡터 검색: numpy 코사인 유사도 (< 10K chunks 규모에 충분)
- 의존성: `llama-cpp-python`, `numpy` (없으면 graceful fallback)

### v1.342 대비 버그 수정 (10건)

1. `_vector_to_blob`/`_blob_to_vector`: struct.pack(4096 args) → numpy tobytes/frombuffer
2. `_call_embed()`: embed() API 호환 레이어 추가 (0.2.x/0.3.x 모두 지원)
3. `LocalReranker.rerank()`: logprobs=True → logprobs=1 (int로 명시)
4. `LocalAdapter.search(mode='search')`: 빈 결과 반환 → 실제 벡터 검색 수행
5. `embed_all_chunks()`: connection leak → try/finally
6. `ModelManager.reset()`: 테스트 격리용 싱글톤 리셋 추가
7. `LocalExpander.expand()`: 빈/공백 쿼리 guard 추가
8. `LocalExpander._parse_keywords()`: 중복 제거, 가비지 필터, 원본 쿼리 제외
9. `vector_search()`: 차원 불일치 감지 + try/finally
10. `ModelManager.get_model()`: 이미 로드된 모델은 \_HAS_LLAMA 체크 우회 (mock 호환)
11. `LocalReranker.rerank()`: 빈 docs 체크를 모델 로드보다 앞으로 이동

---

## vega v1.341 — Vega 리브랜딩 (탑솔라 → Vega 통일)

### 핵심: 코드베이스 전체에서 "탑솔라/topsolar" 명칭을 "Vega/vega"로 통일

### 파일 이름 변경

| 기존                  | 변경              |
| --------------------- | ----------------- |
| `topsolar.py`         | `vega.py`         |
| `test_topsolar.py`    | `test_vega.py`    |
| `topsolar-wrapper.sh` | `vega-wrapper.sh` |
| `mcp-topsolar.json`   | `mcp-vega.json`   |

### 내용 변경 (20개 파일)

| 파일                      | 변경                                                          |
| ------------------------- | ------------------------------------------------------------- |
| `vega.py`                 | 모듈 docstring "탑솔라" → "Vega"                              |
| `config.py`               | docstring "탑솔라" → "Vega", topsolar.py 참조 → vega.py       |
| `core.py`                 | docstring "탑솔라" → "Vega"                                   |
| `router.py`               | docstring "탑솔라 통합 검색 라우터" → "Vega 통합 검색 라우터" |
| `project_db_v2.py`        | docstring + argparse 설명 "탑솔라" → "Vega"                   |
| `mail_to_md.py`           | 사용법 주석 topsolar.py → vega.py                             |
| `test_vega.py`            | docstring + 실행 명령 test_topsolar → test_vega               |
| `_test_search_quality.py` | test_topsolar.py 참조 → test_vega.py                          |
| `commands/write.py`       | 사용법 안내 topsolar.py → vega.py                             |
| `commands/cross.py`       | 주석 "topsolar의 역할" → "core의 역할"                        |
| `addons/__init__.py`      | docstring + 출력 "탑솔라 애드온" → "Vega 애드온"              |
| `addons/_base.py`         | docstring "topsolar.py가 호출" → "vega.py가 호출"             |
| `addons/dashboard.py`     | HTML 타이틀/제목 "탑솔라 대시보드" → "Vega 대시보드"          |
| `addons/contacts.py`      | 노이즈 필터 '탑솔라' → 'Vega'                                 |
| `_lib.sh`                 | 주석 "탑솔라 셸 스크립트" → "Vega 셸 스크립트"                |
| `vega-wrapper.sh`         | 전면 재작성: TOPSOLAR→VEGA_CLI, 모든 참조 vega.py             |
| `bootstrap.sh`            | topsolar.py health → vega.py health                           |
| `setup.sh`                | 설치 안내/필수 파일/MCP 가이드 전체 vega 통일                 |
| `install.sh`              | alias topsolar.py → vega.py                                   |
| `mcp-vega.json`           | 서버명 "topsolar" → "vega", wrapper 참조 통일                 |
| `CLAUDE.md`               | 전면 재작성: 모든 참조 vega.py/vega-wrapper.sh/mcp-vega.json  |

### 유지 사항

- CHANGELOG.md의 과거 버전 기록은 당시 실제 파일명을 보존 (역사적 정확성)
- 코드 로직 변경 없음 — 순수 이름/문서 변경만

### 테스트

- 98개 단위 테스트 통과 (test_vega.py)
- 21개 검색 품질 테스트 통과

---

## vega v1.34 — 검색 품질 종합 개선 (라우팅 + 랭킹 + 사용성)

### 핵심: 21개 품질 테스트 + 18개 실무 시나리오 → 6건 버그 수정 + 11건 개선

### Phase 1: 버그 수정 (6건)

#### Fix 1: "급한 프로젝트" 라우팅 + 검색 실패

- `analyze_query()`의 status 패턴에 "급한", "위급" 누락 → 추가
- 상태 필터에 동의어 확장: "급한" → "긴급"도 함께 LIKE 검색
- "급한 프로젝트 뭐 있어" → 제주ESS (긴급 대응 중 🔴) 정상 반환

#### Fix 2: 의미 패턴 + 키워드 동시 존재 시 QMD 단독 라우팅

- "해저케이블 설계 어떻게" → "어떻게"가 의미 패턴 매칭 → QMD 전용 라우팅
- SQLite 구조 데이터를 활용 못해 비금도(해저케이블 프로젝트)가 3위로 밀림
- **수정**: 의미 패턴만 매칭 + 키워드 있으면 → hybrid (SQLite + QMD 병행)

#### Fix 3: 한국어 조사 포함 쿼리의 matched_keywords 빈 배열

- "비금도에서 케이블은" → 라우터가 조사 제거 후 "비금도", "케이블"로 검색
- search.py는 원본 "비금도에서", "케이블은"으로 키워드 매칭 → 불일치 → 빈 배열
- **수정**: search.py에서 라우터의 전처리된 모든 추출 토큰 (keywords + clients + statuses 등) 사용

#### Fix 4: BM25 랭킹에서 구체적 키워드 & 다빈도 프로젝트 순위 개선

- 긴 키워드일수록 높은 가중치: `4.0 + (len-2) × 2.0` (예: "해저케이블" 5글자 = +10.0)
- 매칭 청크 수 보너스: 여러 청크에 키워드가 등장하는 프로젝트 우선 (`+3.0 × (count-1)`)
- 비금도가 "해저케이블 설계 어떻게"에서 정상 1위 (다빈도 + 긴 키워드 보너스)

#### Fix 5: 부분 이름 매칭 시 거래처가 실제 프로젝트보다 상위

- "비금" 검색 → ZTT(거래처)가 1위, 비금도(실제 프로젝트)가 2위
- **수정**: 검색어가 프로젝트명에 포함되면 +20.0 보너스 → 실제 프로젝트 우선

#### Fix 6: 시간/금액 기반 쿼리의 0건 방치

- "이번 달 할일", "500억 이상" 등 search로 풀 수 없는 쿼리 → 0건 + 무응답
- **수정**: NL_ROUTES에 시간 기반 패턴 추가 (`이번 달|할일|해야` → urgent)
- **수정**: 0건 시 쿼리 유형 분석 → `urgent`/`pipeline` 등 대안 명령 안내

### Phase 2: 품질 개선 (11건)

#### 개선 1: 스톱워드 축소 — 의도 표현 보존

- 기존: `담당`, `진행중`, `관련`, `어떻게` 등이 스톱워드로 제거 → 의도 소실
- **수정**: 순수 필러만 제거, 실무 의도 표현 28개 보존
- 효과: "담당자 관련" 검색 시 "담당"이 키워드로 작동

#### 개선 2: 커뮤니케이션 정렬 + 표시 제한

- 최신순 정렬 (날짜 DESC) + 상위 10건 제한
- 10건 초과 시 `communications_total`, `communications_note` 표시
- 효과: AI가 최신 메일부터 파악, 토큰 낭비 방지

#### 개선 3: 인물 → person 명령 NL 라우팅

- `뭐 하고 있|맡은 거|담당하는|포트폴리오|업무 현황` → `person` 자동 라우팅
- `이번 달|할 일|액션 아이템|해야` → `urgent` 자동 라우팅
- 효과: "고건 뭐 하고 있어?" → person 명령으로 직접 연결

#### 개선 4: 의미 검색 패턴 확장 (4그룹 추가)

- 시간/기간: `지난달|지난주|최근\d`
- 문서/산출물: `계약서|도면|인증서|보고서|견적`
- 이해관계자: `발주처|고객|협력사|외주|하도급`
- 소통: `의견|회의|논의|합의|피드백|회신`
- 효과: "발주처 피드백 있어?" 같은 쿼리가 hybrid 라우팅

#### 개선 5: FTS broad 임계값 — 활성 프로젝트 기준

- 기존: strict FTS 결과 < 3건이면 broad FTS 실행
- **수정**: 활성 프로젝트(완료/취소 제외) < 5건이면 broad FTS 실행
- 효과: 완료 프로젝트만 매칭되어도 broad 탐색으로 활성 프로젝트 발굴

#### 개선 6: 비표준 커뮤니케이션 파싱

- `- 일반 텍스트` (bold/발신자 없음) → 독립 커뮤니케이션 항목으로 파싱
- 5글자 이상의 plain bullet → subject로 인식, sender 없이 저장
- 효과: 비정형 메모/기록도 검색 가능

#### 개선 7: 커뮤니케이션 검색 → project_id 필터

- 기존: 커뮤니케이션 FTS가 전체 DB 대상 → 무관한 프로젝트 메일도 반환
- **수정**: 프로젝트 검색 결과의 project_ids로 커뮤니케이션 필터링
- 효과: 검색어와 관련된 프로젝트의 메일만 반환

#### 개선 8: 맥락별 후속 명령 힌트

- 인물 검색 → `person {이름}` 힌트
- 커뮤니케이션만 매칭 → 프로젝트 상세 안내
- 다수 결과(5+) → `--min-score` 필터 안내
- 효과: AI가 자연스럽게 다음 단계 안내

#### 개선 9: 프로젝트명 직접 매칭 보너스 정밀화

- 정확 매칭 (프로젝트명 전체 또는 첫 단어): +30.0
- 부분 매칭 (프로젝트명에 포함): +20.0
- 효과: "비금" → 비금도 해상태양광이 ZTT보다 확실하게 상위

#### 개선 10: 테이블 메타 `_` 접두어 → 태그 자동 생성

- `.md` 테이블의 `_해저케이블`, `_CU헷징` 등 `_` 접두어 항목 → `기술:해저케이블` 태그
- 효과: 기술 키워드가 태그로 인덱싱되어 검색 정밀도 향상

#### 개선 11: 동적 패턴 캐시 TTL 추가

- 기존: mtime 변경 시에만 캐시 무효화 → 파일 변경 없으면 영원히 캐시
- **수정**: 60초 TTL 추가 — 주기적으로 캐시 갱신
- 효과: DB 외부 변경(수동 편집 등) 시에도 60초 내 반영

### 테스트

- 21개 검색 품질 쿼리 전수 통과
- 18개 실무 시나리오 테스트
- 98개 단위 테스트 (v1.339 대비 +1)

---

## vega v1.339 — v1.338 코드 리뷰 반영 (정규식 오탐 + 페널티 스태킹 + 안정성)

### 핵심: v1.338 코드 리뷰에서 발견된 6건 버그 수정

### Fix 1: `source` 필드 패턴 불일치

- `_parse_output`의 `source` 필드가 `item.get('displayPath', item.get('file', ''))` (구패턴) 사용
- `or` 패턴으로 통일: `item.get('displayPath') or item.get('file', '')`

### Fix 2: `_extract_project_from_path` trailing slash 엣지케이스

- `qmd://collection/` (trailing slash)일 때 `parts[1]`이 빈 문자열 → 빈 프로젝트명
- `strip('/')` 후 빈 문자열이면 collection name으로 폴백

### Fix 3: 페널티 스태킹 — 0.3 x 0.3 x 0.1 = 0.009x 과도

- 기존: `filter_bypassed(0.3)` × `_NON_PROJECT_RE(0.3)` × `_BACKUP_DIR_RE(0.1)` 곱셈 스태킹
- 수정: `min()` 방식으로 가장 낮은 단일 페널티만 적용 (최악 0.1x)
- 백업 디렉토리 내 비프로젝트 파일: 0.009x → 0.1x

### Fix 4: `_BACKUP_DIR_RE` 정규식 오탐

- 기존 `-v\d+[-./]` 패턴이 정당한 프로젝트명(`서비스-v2`, `nova-v3.md`) 매칭
- 제거: 범용 `-v\d+` 패턴 삭제, `vega-v\d+[-/.]` + `tools-backup` + `backup/bak/old` 리터럴만 유지
- 경로 경계 앵커 추가: `(?:^|/)` 접두어

### Fix 5: `_write_qmdignore` 안정성

- `os.listdir()` try-except 추가 (디렉토리 접근 실패 시 크래시 방지)
- 파일 쓰기 실패 시 `ValueError` raise (caller에서 처리)
- 부모 디렉토리 스캔 제거 (루트 디렉토리 스캔 위험 + relpath 로직 혼란)

### Fix 6: 정규식 중복 정의 해소

- `_BACKUP_DIR_RE` (router.py)와 `_EXCLUDE_DIR_RE` (qmd_ops.py) 동일 정규식 → 양쪽 동시 수정
- 순환 import 방지를 위해 각 파일에서 정의하되 패턴 동기화

### 변경 파일

| 파일                  | 변경                                                            |
| --------------------- | --------------------------------------------------------------- |
| `router.py`           | source `or` 통일, trailing slash, 정규식 정밀화, min() 페널티   |
| `commands/qmd_ops.py` | 정규식 동기화, 에러 핸들링, 부모 디렉토리 스캔 제거             |
| `test_topsolar.py`    | v1.339 테스트 3개 추가 (오탐 방지, 스태킹 방지, trailing slash) |
| `CHANGELOG.md`        | v1.339 섹션                                                     |

### 테스트

- 94 + 3 = **97개 테스트 케이스**

---

## vega v1.338 — QMD 통합 버그 수정 (프로젝트 매칭 + 인덱스 노이즈)

### 핵심: QMD 검색 결과의 프로젝트 매핑 실패 + 인덱스 노이즈 해결

DGX 환경에서 QMD 연동 코드가 갖춰져 있으나 실제 통합이 안 되는 2가지 문제 수정.

### Bug 1: `_parse_output` — file 필드 미매핑

**문제**: QMD JSON 결과에 `"file": "qmd://projects-dir-main/비금도.md"` 있지만 `displayPath`가 없을 때, `project_name` 추출이 `item.get('displayPath', '')` → 빈 문자열을 `_extract_project_from_path()`에 전달 → 프로젝트명 추출 실패 → `_infer_qmd_project_name()` 빈 문자열 반환 → 프로젝트 매칭 실패.

**수정**:

- `item.get('displayPath', '')` → `item.get('displayPath') or item.get('file', '')` (project_name, filepath 모두)
- `displayPath`가 None/빈 문자열이면 `file` 필드로 폴백

### Bug 2: `_extract_project_from_path` — qmd:// URI 미처리

**문제**: `qmd://projects-dir-main/tools/claude.md` 같은 QMD URI에서 프로젝트명 추출 불가. 또한 `CLAUDE.md`, `README.md` 등 비프로젝트 파일이 프로젝트명으로 반환됨.

**수정**:

- `qmd://` 접두어 파싱: `qmd://collection-name/path/file.md` → `file.md` → stem 추출
- 비프로젝트 파일 (`_NON_PROJECT_RE` 매칭) → 빈 문자열 반환 (노이즈 방지)

### Bug 3: QMD 인덱스 노이즈 — 백업/이전 버전 디렉토리

**문제**: `tools-backup-v1-21/`, `vega-v1-21/`, `vega-v1-32/` 등 백업 디렉토리가 QMD에 인덱싱되어, CLAUDE.md 예시 코드가 실제 프로젝트(비금도.md)보다 높은 점수를 받음.

**수정**:

- `_BACKUP_DIR_RE` 정규식 추가: 백업/이전 버전 경로 패턴 감지
- `_score_qmd_results()`에서 백업 경로 결과에 0.1× 페널티 적용
- `qmd-index clean` 신규 액션: `.qmdignore` 파일 자동 생성 + 인덱스 재빌드
- `qmd-index setup`에서도 `.qmdignore` 자동 생성

### 변경 파일

| 파일                  | 변경                                                                                                                   |
| --------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| `router.py`           | `_parse_output` file 폴백, `_extract_project_from_path` qmd:// URI + 비프로젝트 필터, `_BACKUP_DIR_RE` + 스코어 페널티 |
| `commands/qmd_ops.py` | `_write_qmdignore()`, `qmd-index clean` 액션, setup에 .qmdignore 자동 생성                                             |
| `test_topsolar.py`    | v1.338 테스트 7개 추가                                                                                                 |
| `CHANGELOG.md`        | v1.338 섹션 추가                                                                                                       |

### 테스트

- 87 + 7 = **94개 테스트 케이스**

### QMD 인덱스 정리 방법

```bash
# 방법 1: vega 명령
python3 topsolar.py qmd-index clean

# 방법 2: 재설정
python3 topsolar.py qmd-index setup
```

---

## vega v1.337 — 패치 자동화 (Claude 최적화)

### 핵심: 새 명령 추가 시 core.py 수정 완전 제거

기존 문제: 새 명령 추가마다 (1) commands/ 파일 작성, (2) `_generate_summary`에 if/elif 추가, (3) 프로젝트 ID 추출 보일러플레이트 복붙 — 3곳 수정 필요. CLAUDE.md는 72% 변경이력(789줄)으로 비대.

### Phase 1: `summary_fn` — 요약 함수 자기 등록

**`_generate_summary` 20개 if/elif 체인 → 명령별 `summary_fn` 자기 등록**

- `register_command`에 `summary_fn` 파라미터 추가
- 각 명령 파일이 자신의 요약 로직을 데코레이터에 선언
- `_generate_summary`에서 레지스트리의 `summary_fn` 우선 호출, 없으면 `data['summary']` 폴백
- core.py의 100줄 if/elif → 8줄 디스패치 (ask 특수 케이스만 잔존)
- **효과: 새 명령 추가 시 core.py 수정 불필요**

```python
@register_command('my-cmd',
    summary_fn=lambda d: f"결과: {d.get('count',0)}건")
def _exec_my_cmd(params):
    return {'count': 42}
```

### Phase 2: `addon_command()` — 애드온 브릿지 한 줄 등록

**5개 애드온 브릿지 × 3-9줄 보일러플레이트 → 1줄**

- `addon_command(name, addon_class, summary_fn=...)` 헬퍼 추가
- 내부에서 Ctx 생성, safe_api 호출, 레지스트리 등록 자동 처리
- 적용: dashboard(9줄→2줄), changelog(5줄→2줄)

```python
from addons import Dashboard
from core import addon_command
addon_command('dashboard', Dashboard,
              summary_fn=lambda d: f"활성 {d.get('active_projects',0)}개")
```

### Phase 3: `require_project()` — 프로젝트 ID 추출 통합

**show/timeline/기타 명령의 프로젝트 ID 추출 패턴 통합**

- `require_project(params, usage_hint)` → int(성공) 또는 dict(에러)
- params에서 `id`, `sub_args`, `query` 순서로 탐색
- 적용: show.py의 timeline, show 명령 (각 5-7줄 → 3줄)

### Phase 4: CLAUDE.md 분리

**1089줄 → 303줄 (가이드) + 792줄 (CHANGELOG.md)**

- 운영 가이드(300줄)와 변경이력(790줄)을 분리
- CLAUDE.md가 가벼워져 AI 컨텍스트 로딩 속도 향상
- 변경이력 참조: `CHANGELOG.md` 링크

### Phase 5: 기존 명령 이관

`summary_fn`으로 이관된 명령 (15개):

- search, dashboard, contacts, pipeline, weekly, changelog
- timeline, show, list, brief, recent, cross
- urgent, person, health

### 변경 파일

| 파일                    | 변경                                                                                    |
| ----------------------- | --------------------------------------------------------------------------------------- |
| `core.py`               | `summary_fn` 파라미터, `addon_command()`, `require_project()`, `_generate_summary` 축소 |
| `commands/dashboard.py` | 9줄 → 4줄 (`addon_command` 사용)                                                        |
| `commands/weekly.py`    | `summary_fn` 추가, changelog → `addon_command`                                          |
| `commands/contacts.py`  | `summary_fn` 추가                                                                       |
| `commands/pipeline.py`  | `summary_fn` 추가                                                                       |
| `commands/cross.py`     | `summary_fn` 추가 (별도 함수로 분리)                                                    |
| `commands/show.py`      | `require_project` + `summary_fn` 적용 (3개 명령)                                        |
| `commands/search.py`    | `summary_fn` 추가 (복잡 로직 별도 함수)                                                 |
| `commands/brief.py`     | `summary_fn` 추가 (brief, recent 각각)                                                  |
| `commands/urgent.py`    | `summary_fn` 추가                                                                       |
| `commands/person.py`    | `summary_fn` 추가                                                                       |
| `commands/system.py`    | health `summary_fn` 추가                                                                |
| `CLAUDE.md`             | 개발자 가이드 갱신, 변경이력 → CHANGELOG.md 분리                                        |
| `CHANGELOG.md`          | 신규 파일 (변경이력 전체)                                                               |

### 미래 패치 영향도

| 작업             | v1.336                                             | v1.337                  |
| ---------------- | -------------------------------------------------- | ----------------------- |
| 새 명령 추가     | commands/ 1파일 + core.py `_generate_summary` 수정 | commands/ 1파일만       |
| 새 애드온 브릿지 | commands/ 1파일 (5-9줄 보일러플레이트)             | `addon_command()` 1줄   |
| 프로젝트 ID 필요 | 5-7줄 추출 로직 복붙                               | `require_project()` 1줄 |
| CLAUDE.md 읽기   | 1089줄 전체 로드                                   | 303줄 (72% 절감)        |

### 테스트

- **87개 테스트 케이스** (v1.336과 동일 수, 코드 변경만)

---

## vega v1.31 — QMD 2.0.1 통합 (의미 검색 엔진)

### 핵심: SQLite 구조화 검색 + QMD 2.0 의미 검색 하이브리드

[QMD 2.0](https://github.com/tobi/qmd) — 로컬 마크다운 검색 엔진 by Tobi Lutke (Shopify).
BM25 + 벡터(Qwen3-Embedding-8B Q4) + LLM 리랭킹(qwen3-reranker) + 쿼리 확장(LFM2-1.2B-Q4_K_M).
모든 모델 자동 다운로드. QMD 미설치 시 SQLite 전용 모드 자동 폴백.

### QMD 2.0 주요 변경 (vs 1.x)

- **통합 `search()` API**: `query`/`search`/`structuredSearch` 분리 → 단일 `search()` 메서드
- **`intent` 파라미터**: 의미 모호성 해소 — 확장, 리랭킹, 청크 선택, 스니펫 추출 전 파이프라인에 적용
- **Query documents**: typed sub-queries (`lex:`, `vec:`, `hyde:`) 조합
- **기본 모델**: `embeddinggemma-300M` (영어), `Qwen3-Embedding-8B Q4` (한국어/CJK)
- **`qmd skill install`**: Claude Code 플러그인 원클릭 설치
- **`--explain`**: 검색 점수 추적 (RRF 기여도, 리랭커 점수, 블렌딩)
- **Stable SDK**: `createStore()` → `QMDStore` 인터페이스 (Node.js/Bun 라이브러리)

### QMD 감지 (config.py)

- **3단계 탐색**: `QMD_BIN` (직접 바이너리) → `QMD_WRAPPER` (wrapper.sh) → None (SQLite 폴백)
- **`get_qmd_executable()`**: 실행 가능한 QMD 경로를 반환하는 통합 함수
- **탐색 우선순위**: 환경변수 > 로컬 qmd/ > VEGA_HOME > ~/.vega > OpenClaw > PATH

### 하이브리드 검색 라우터 (router.py)

- **쿼리 분석기**: 구조화 필드(고객사, 담당자) vs 의미 패턴(어떻게, 왜) 자동 판별
- **3가지 라우팅**: SQLite 우선 / QMD 우선 / 하이브리드 (양쪽 동시)
- **QMD 모드 자동 선택**: query(하이브리드) / search(BM25) / vsearch(벡터)
- **intent 자동 추출**: 의미 질문의 고객사+키워드를 intent로 QMD에 전달
- **JSON 파싱**: QMD 2.0 `HybridQueryResult` 필드 매핑 (bestChunk, docid, displayPath)
- **결과 퓨전**: SQLite + QMD 결과를 프로젝트 단위로 점수 합산 + 재정렬

### 신규 명령

- **`qmd-status`**: QMD 설치 상태, 버전, 컬렉션 목록, 인덱스 상태 조회
- **`qmd-index`**: QMD 인덱스 관리
  - `qmd-index setup` — 전체 초기 설정 (컬렉션 등록 + 컨텍스트 + 인덱스)
  - `qmd-index update` — 인덱스 갱신 (기본)
  - `qmd-index add [name]` — 컬렉션 수동 등록
  - `qmd-index embed` — 벡터 임베딩 생성
  - `qmd-index skill-install` — Claude Code 플러그인 설치
  - `qmd-index status` — qmd-status로 리다이렉트

### QMD 초기 설정 가이드

```bash
# 방법 1: vega 명령으로 한 번에
python3 topsolar.py qmd-index setup

# 방법 2: 수동
npm install -g @tobilu/qmd
qmd collection add /path/to/projects --name projects
qmd context add qmd://projects "태양광/풍력/ESS 프로젝트 데이터"
qmd embed  # 선택 — 의미 검색용

# Claude Code 플러그인 (선택)
qmd skill install
```

### QMD 2.0 검색 파이프라인

```
쿼리 → LLM 쿼리 확장 → [원본(×2), lex 변형, vec 변형, hyde 변형]
  각각 → BM25 + 벡터 검색
    → RRF 퓨전 (k=60, top-rank 보너스)
      → 청크 선택 (intent 가중)
        → LLM 리랭킹 (qwen3-reranker)
          → Position-Aware 블렌딩 (1-3위: 75% RRF, 4-10위: 60%, 11+: 40%)
```

### 배포 파일

```
vega-v1.32/
├── qmd/
│   ├── config/settings.toml    # QMD 검색 설정 (모델 자동 다운로드)
│   ├── qmd-wrapper.sh          # 한국어 임베딩 모델 설정 wrapper
│   ├── cache/                   # QMD 캐시 (자동 생성)
│   └── models/                  # 임베딩 모델 캐시
├── install.sh                   # 통합 설치 스크립트
├── commands/qmd_ops.py          # qmd-status, qmd-index 명령
└── (기존 v1.21 파일들)
```

### 테스트

- **TestQMDIntegration**: 6+3개 테스트 — 명령 등록, 메타데이터, config export, 폴백 안전성, 통합 형식 검증
- 48 + 9 = **57개 테스트 케이스**

### v1.31 리팩토링 — Vega 중심 QMD 통합

**설계 원칙**: QMD는 Vega의 하위 모듈. Vega의 데이터 모델·명명 규칙이 기준.

- **router.py 순수 라이브러리화**: CLI 코드(`main()`, `format_results()`) 제거 (~190줄), QMD 1.x 텍스트 파싱(`_parse_qmd_block()`) 제거 (~120줄)
- **Vega 통합 결과 형식**: `_make_result()` 팩토리 — SQLite/QMD 양쪽 결과를 동일한 canonical dict로 정규화
  - 필드: `project_id`, `project_name`, `client`, `status`, `person`, `content`, `heading`, `score`, `source`('sqlite'|'qmd'), `entry_date`, `chunk_type`, `metadata`
  - `_sqlite_rows_to_unified()`, `_qmd_items_to_unified()` 변환 함수
  - `SearchRouter.search()` 반환값에 `results['unified']` 추가
- **commands/search.py 단순화**: 포맷 브릿징 코드 제거, 통합 형식 직접 소비
- **config import 정리**: `sys.path` 해킹 제거, `from config import` 직접 사용
- **버그 수정**: `QMDAdapter._run()` `try:/else:` 구문 오류 → `if self.use_binary:/else:` 수정
- **임베딩 모델**: `Qwen3-Embedding-8B Q4` (한국어/CJK), `embeddinggemma-300M` (영어)

## vega v1.336 — 구조 개선 (개발자 생산성)

### 핵심: 대규모 패치를 빠르고 안전하게 적용할 수 있는 코드베이스 구조

### Phase 1: addons.py 패키지 분리 (1348줄 → 10파일)

**addons.py 모놀리스 → addons/ 패키지**

- 기존: 8개 애드온 클래스가 1348줄 단일 파일에 집약
- 수정: 클래스별 독립 파일로 분리, `__init__.py`에서 re-export

```
addons/
├── __init__.py      # re-export + ADDONS 리스트 + main()
├── __main__.py      # python -m addons 지원
├── _base.py         # Ctx, BaseAddon, _load_projects, _extract, _json_default
├── cross.py         # CrossAnalysis
├── dashboard.py     # Dashboard
├── changelog.py     # Changelog
├── sync_back.py     # SyncBack
├── contacts.py      # Contacts
├── weekly.py        # WeeklyReport
├── pipeline.py      # Pipeline
└── template.py      # Template
```

- 효과: Pipeline 수정 시 pipeline.py만 열면 됨. 다른 애드온 코드와 충돌 없음
- **하위 호환**: `from addons import Pipeline, Ctx` 기존 import 전부 유지

### Phase 2: addons import-time 바인딩 버그 수정

**`from config import DB_PATH, MD_DIR` → `import config` (v1.33과 동일 패턴)**

- 기존: `Ctx.__init__(self, db_path=DB_PATH)` — import 시점의 값이 기본값으로 고정
- 수정: `Ctx.__init__(self, db_path=None)` → `self.db_path = db_path if db_path is not None else config.DB_PATH`
- 효과: 테스트 fixture에서 `config.DB_PATH` 패치가 addons에도 전파됨

### Phase 3: core.py 공개 별칭 통합

**21개 `alias = _func  # public alias` 산재 → `_publish()` 일괄 등록**

- 기존: 매 함수 정의 직후 `find_project_id = _find_project_id  # public alias` 패턴 반복
- 수정: `_publish()` 헬퍼로 모든 별칭을 한 곳에서 등록

```python
_publish({
    'find_project_id': _find_project_id,
    'get_flag': _get_flag,
    'escape_like': _escape_like,
    # ... 20개 더
})
```

- 효과: 새 유틸리티 추가 시 `_publish` dict에 한 줄만 추가. 별칭 관리가 한 곳으로 집중

### Phase 4: 테스트 인프라 — VegaTestCase 베이스 클래스

**4개 테스트 클래스의 `_exec()` 중복 제거**

- 기존: `TestResponseStructure`, `TestNewCommands`, `TestAsk`, `TestEdgeCases` 각각 `_exec()` 메서드 중복
- 수정: `VegaTestCase(unittest.TestCase)` 베이스 클래스 도입

```python
class VegaTestCase(unittest.TestCase):
    def _exec(self, cmd, params=None):
        return execute(cmd, params or {})

    def _assert_ok(self, response, msg=None):
        """status=='ok' 검증 + data 반환"""
        ...

    def _search(self, query, **extra_params):
        """search 명령 숏컷"""
        ...
```

- 효과: 새 테스트 클래스 작성 시 `VegaTestCase` 상속만으로 헬퍼 사용. 보일러플레이트 제거

### 변경 파일

| 파일                    | 변경                                                     |
| ----------------------- | -------------------------------------------------------- |
| `addons.py` → `addons/` | 1348줄 모놀리스 → 10파일 패키지, import-time 바인딩 수정 |
| `core.py`               | `_publish()` 헬퍼, 21개 별칭 통합                        |
| `test_topsolar.py`      | VegaTestCase 추가, 4클래스 상속 전환, v1.336 테스트 3개  |
| `CLAUDE.md`             | v1.336 섹션 추가                                         |

### 테스트

- 84 + 3 = **87개 테스트 케이스**

### 미래 패치 가이드 (v1.336+)

**새 애드온 추가**: `addons/my_addon.py` 생성 → `addons/__init__.py`의 `ADDONS` 리스트에 추가
**새 명령 추가**: `commands/my_cmd.py` 생성 (자동 등록)
**새 유틸리티 추가**: `core.py`에 `_my_func()` 구현 → `_publish`에 한 줄 추가 → `__all__`에 추가
**새 테스트 추가**: `class TestMyFeature(VegaTestCase)` — `self._exec()`, `self._assert_ok()`, `self._search()` 즉시 사용

---

## vega v1.335 — 안정성 개선 + 성능 최적화 + 코드 품질

### 핵심: 버그 수정, DB 쿼리 최적화, 복잡 함수 분해, 테스트 보강

### Phase 1: 버그 수정

**1.1 mail_to_md.py `_auto_sync_db()` — 실패 무시 버그**

- 기존: chunks 갱신 실패 시 `except Exception: pass` → 로깅 없이 DB 불일치 발생
- 수정: comm_log INSERT 직후 별도 `conn.commit()`, chunks 실패 시 `logging.warning()` 추가
- 효과: comm_log는 항상 보존, chunks 실패 원인 추적 가능

**1.2 addons.py Pipeline `_extract_amount()` — 이중 매칭 방지**

- 기존: 같은 줄에서 '원' 패턴과 '억원' 패턴이 겹치면 동일 금액 두 번 후보 등록
- 수정: `matched_spans` 위치 추적으로 겹치는 매칭 건너뛰기
- 효과: 정확한 금액 추출

### Phase 2: 성능 개선

**2.1 addons.py `_load_projects()` — N+1 쿼리 → 벌크 쿼리**

- 기존: 프로젝트 N개에 대해 chunks/comm_log/tags 각각 쿼리 (3N+1회)
- 수정: 3개 벌크 쿼리 + defaultdict 조합 (4회)
- 효과: 50+ 프로젝트 DB에서 체감 속도 향상

**2.2 md_editor.py `find_md_path()` — rglob 캐싱**

- 기존: 매 호출마다 `rglob('*.md')` 전체 스캔
- 수정: mtime 기반 `{stem: full_path}` 모듈 캐시
- 효과: update/add-action 반복 호출 시 불필요한 파일 탐색 제거

**2.3 project_db_v2.py — FTS 리빌드 조건부 실행**

- 기존: `import_incremental()` 후 FTS 3테이블 무조건 rebuild
- 수정: 변경 10개 초과 시에만 rebuild, `rebuild_fts()` 분리
- 효과: 소량 증분 업데이트 시 불필요한 FTS rebuild 제거

### Phase 3: 코드 품질 리팩토링

**3.1 config.py — 경로 탐색 함수 통합**

- 기존: `_find_md_dir()`, `_find_qmd_binary()`, `_find_qmd_wrapper()` 3개 유사 함수
- 수정: 공통 `_find_path()` 헬퍼로 통합 (~40줄 절감)

**3.2 router.py `_rerank_fusion()` — 함수 분해**

- 기존: 90줄짜리 단일 함수
- 수정: `_score_sqlite_chunks()`, `_score_qmd_results()`, `_apply_ranking()` 3개로 분해
- 효과: 각 함수 독립 테스트 가능, 스코어링 로직 수정 용이

### Phase 4: 테스트 보강

- `TestPipelineAmountV1335`: 이중 매칭, 부정 컨텍스트, 만원/USD 변환, 소액 무시 (5개)
- `TestRerankFusionV1335`: 분해된 함수 동작 검증 (2개)
- `TestFTSRebuildV1335`: rebuild_fts 호출 가능 검증 (1개)
- 76 + 8 = **84개 테스트 케이스**

### 변경 파일

| 파일               | 변경                                                  |
| ------------------ | ----------------------------------------------------- |
| `mail_to_md.py`    | \_auto_sync_db 커밋 분리 + 로깅                       |
| `addons.py`        | \_extract_amount span 추적, \_load_projects 벌크 쿼리 |
| `project_db_v2.py` | FTS 조건부 rebuild, rebuild_fts() 분리                |
| `md_editor.py`     | find_md_path rglob 캐싱                               |
| `config.py`        | \_find_path() 공통 헬퍼                               |
| `router.py`        | \_rerank_fusion 3함수 분해                            |
| `test_topsolar.py` | v1.335 테스트 8개 추가                                |
| `CLAUDE.md`        | v1.335 섹션 추가                                      |

---

## vega v1.334 — QMD 자연 융합 + AI 사용성 개선

### 핵심: QMD가 Vega에 자연스럽게 녹아들고, AI가 하나의 깔끔한 응답만 소비

기존 문제: QMD 결과가 별도 `qmd_results[]` 배열로 분리되어 AI가 수동 합쳐야 했고, `analysis` dict에 라우팅 디테일이 노출되어 노이즈 발생. 코드 구조도 3-way 분기로 복잡.

### Part 1: 코드 구조 리팩토링 (router.py)

**1A. SearchRouter.search() — 3-way 분기 → 선형 흐름**

- 기존: `if route=='sqlite' / elif route=='qmd' / elif route=='hybrid'` — 3개 분기, 각각 SQLite/QMD 호출 조합이 다름
- 수정: SQLite 항상 먼저 실행, QMD는 route에 따라 조건부 실행
- 효과: 코드가 단순해지고, 각 route가 "QMD를 어떻게 보강할지"만 결정

**1B. `_qmd_items_to_unified()` — DB 메타데이터 보강**

- 기존: project_name만 추론, project_id/client/status/person 전부 빈 값
- 수정: `_load_project_lookup(db_path)` 추가 — projects 테이블에서 메타데이터 1회 로드
- QMD 결과의 project_name으로 조회 → project_id/client/status/person 채움
- 효과: QMD 결과가 SQLite 결과와 동일한 메타데이터를 가짐 → 진짜 통합 가능

**1C. `_rerank_fusion()` — 고아 QMD 결과 복구**

- 기존: QMD가 SQLite에 없던 프로젝트를 발견해도 pid=None → 무시
- 수정: pid가 None이면 DB에서 project_id 조회 (1B의 같은 캐시 재사용)
- 효과: QMD가 SQLite에 없던 프로젝트를 발견하면 결과에 포함

**1D. `search_meta` 추가**

- AI가 소비하는 깔끔한 메타데이터: `route`, `qmd_available`, `qmd_used`, `sqlite_count`, `qmd_count`, `rerank_mode`
- `comms`를 별도 전달 (sqlite raw에서 분리)

### Part 2: AI 사용성 개선 (search.py, core.py)

**2A. search.py — 단일 `projects[]`로 통합**

- 기존: `projects[]` (SQLite만) + `qmd_results[]` (QMD만) 분리
- 수정: 모든 unified items를 project_id 기반으로 단일 grouped dict에 합침
- `projects[].sources`: `['sqlite']`, `['qmd']`, 또는 `['sqlite', 'qmd']`
- `projects[].sections[].source`: 각 섹션의 출처 표시
- `qmd_results[]` 배열 완전 제거
- project_id 없는 QMD 결과만 `qmd_extra[]`로 최소 분리

**2B. search.py — `analysis` 제거 → `search_meta`**

- `analysis`, `route`, `route_reason` 필드 제거
- `search_meta`만 전달 — AI가 불필요한 패턴 매칭 디테일을 보지 않음

**2C. core.py — AI 힌트 QMD 가이드**

- `qmd_used=True` → "QMD 의미 검색이 N건 추가 결과를 포함합니다"
- `qmd_available=False` → "QMD 미연결 — 키워드 매칭만 수행"

**2D. core.py — summary QMD 반영**

- QMD 기여도 포함: "검색 결과: 비금도, 화성산단 (3개 프로젝트, QMD 2건 포함)"

### 응답 형식 변화

**Before (v1.333)**:

```json
{
  "projects": [{"id": 5, "name": "비금도", "sections": [...]}],
  "qmd_results": [{"source": "비금도.md", "content": "케이블 납기..."}],
  "analysis": {"route": "hybrid", "extracted": {...}, "reason": "..."},
  "route": "hybrid",
  "route_reason": "구조화(4) + 의미(2) → 혼합 검색"
}
```

**After (v1.334)**:

```json
{
  "projects": [
    {"id": 5, "name": "비금도", "sources": ["sqlite", "qmd"],
     "sections": [
       {"heading": "현재 상황", "source": "sqlite", ...},
       {"heading": "케이블 납기 이슈", "source": "qmd", ...}
     ]}
  ],
  "search_meta": {"route": "hybrid", "qmd_used": true, "qmd_count": 2}
}
```

### 변경 파일

| 파일                 | 변경                                                             |
| -------------------- | ---------------------------------------------------------------- |
| `router.py`          | 1A: 선형 흐름, 1B: QMD 메타 보강, 1C: 고아 복구, 1D: search_meta |
| `commands/search.py` | 2A: 통합 그룹핑, 2B: analysis→search_meta                        |
| `core.py`            | 2C: AI 힌트, 2D: summary                                         |
| `test_topsolar.py`   | v1.334 테스트 11개 추가                                          |
| `CLAUDE.md`          | v1.334 섹션 추가                                                 |

### 테스트

- 65 + 11 = **76개 테스트 케이스**

## vega v1.333 — 검색 품질 버그 수정

### 핵심: QMD 결과 노이즈 제거 + 날짜 정렬 수정 + 속도 개선

v1.332에서 QMD 라우팅이 개선되었으나, 실제 검색 시 INDEX.md/README 등 비프로젝트 문서가 결과에 섞이고, 날짜 정렬이 오래된 순서로 나오며, 순수 구조화 쿼리에서 불필요한 QMD 호출(3-5초)이 발생하는 문제 확인. 6건 버그 수정 + 1건 모드 최적화.

### 변경 사항

**1. `_negate_date_str()` 날짜 정렬 수정** (Bug 1)

- 기존: ISO 날짜 오름차순 → "2024-01-01" < "2026-03-20" (오래된 항목 먼저)
- 수정: 각 숫자를 9에서 빼서 반전 (최신 우선), NULL은 'z'로 맨 뒤

**2. QMD 보충 조건 강화 + 모드 고정** (Bug 2)

- 기존: SQLite <5건이면 무조건 QMD 보충 (순수 구조화 쿼리도 3-5초 낭비)
- 수정: 키워드/의미 패턴 있을 때만 보충, 모드는 `'search'`(BM25, ~1초) 고정

**3. QMD 필터 매칭 최적화** (Bug 3)

- 기존: O(n²) — 필터×필드 이중 루프
- 수정: 필드를 단일 문자열로 join, 필터당 1회 `in` 검색

**4. confidence 계산 통일** (Bug 4)

- 기존: `score/(total+1)` → 매칭 1건에 0.67, hybrid는 하드코딩 0.8
- 수정: `0.7 + score * 0.05` (최대 0.95)

**5. `_infer_qmd_project_name` 빈 문자열 반환** (Bug 5)

- 기존: `return ''` → 빈 문자열이 truthy하지 않지만 caller가 `if not project_name`으로 체크하므로 재호출 루프
- 수정: `return None` + caller에서 `or ''` 방어

**6. 비프로젝트 문서 노이즈 페널티** (Bug 6)

- QMD 결과의 source/filepath에서 비프로젝트 패턴 감지 → 점수 0.3× 페널티
- 패턴: `(INDEX|README|CLAUDE|CHANGELOG|TODO|LICENSE|\.github)` (대소문자 무관)

### 변경 파일

| 파일               | 변경                                             |
| ------------------ | ------------------------------------------------ |
| `router.py`        | 6건 버그 수정 + QMD 보충 모드 'search' 고정      |
| `test_topsolar.py` | `_negate_date_str` + 비프로젝트 패턴 테스트 추가 |
| `CLAUDE.md`        | v1.333 섹션 추가                                 |

### 테스트

- 58 + 7 = **65개 테스트 케이스**

## vega v1.332 — QMD 라우팅 개선

### 핵심: QMD 벡터 검색의 실질적 활용도 향상

기존 문제: 테스트 쿼리 대부분이 SQLite로만 라우팅되어 QMD가 거의 안 쓰임. FTS5 키워드 매칭이 3+건 반환하면 QMD 보충 검색도 발동 안 됨.

### 변경 사항

**1. SEMANTIC_PATTERNS 대폭 확장** (5개 → 10개 그룹)

- 기존: 질문형(어떻게/왜), 설명형(자세히/배경), 기술형, 리스크, 전략 — 총 5개 그룹
- 추가: 사건/사고(화재/파손/고장), 변경/교체(교체/보수/재시공), 상황/경과(경위/사례), 조건/제약(규제/인허가/주민반대), 일정/지연(납기/딜레이/공기차질)
- 효과: "화재 발생으로 모듈 교체" 같은 의미 쿼리가 QMD로 라우팅됨

**2. 라우팅 로직 개선** (`analyze_query()`)

- 기존: 패턴 매칭 없으면 무조건 sqlite, 구조 필드만 있어도 sqlite
- 변경: 키워드가 있으면 패턴 매칭 없어도 hybrid, 구조 필드 + 키워드 → hybrid
- 효과: 대부분의 자연어 질문이 SQLite + QMD 병행 검색으로 처리

**3. QMD 보충 검색 임계값 완화**

- 기존: SQLite 결과 `< 3`건일 때만 QMD 보충
- 변경: `< 5`건으로 상향
- 효과: SQLite가 3~4건 반환해도 QMD가 의미 유사 결과 추가 가능

### 변경 파일

| 파일        | 변경                                                                          |
| ----------- | ----------------------------------------------------------------------------- |
| `router.py` | SEMANTIC_PATTERNS 확장, analyze_query() 라우팅 개선, QMD 보충 임계값 5로 상향 |
| `CLAUDE.md` | v1.332 섹션 추가                                                              |

### 라우팅 비교 (Before → After)

| 쿼리 예시                 | v1.331 라우트        | v1.332 라우트           |
| ------------------------- | -------------------- | ----------------------- |
| "비금도"                  | sqlite               | sqlite (구조 필드만)    |
| "화재 발생으로 모듈 교체" | sqlite (패턴 미매칭) | hybrid (사건+교체 패턴) |
| "케이블 납기 지연"        | sqlite               | hybrid (지연 패턴)      |
| "최근에 사고난 현장"      | sqlite               | hybrid (사고 패턴)      |
| "비금도 케이블 현황"      | sqlite (구조 필드)   | hybrid (구조+키워드)    |

## vega v1.331 — 리랭킹 토글

### 핵심: 리랭킹 on/off를 설정 하나로 전환

리랭킹 파이프라인이 2단계(QMD 내부 리랭커 + Vega 퓨전 스코어링)로 구성되어 있어, 검색 품질 실험이나 디버깅 시 각 단계를 독립적으로 끄고 켤 수 있어야 합니다.

### 사용법

```python
# config.py에서 직접 변경
RERANK_MODE = 'full'        # 기본: 전체 리랭킹
RERANK_MODE = 'vega_only'   # Vega 퓨전만, QMD 리랭커 OFF
RERANK_MODE = 'none'        # 리랭킹 완전 OFF

# 또는 환경변수로 오버라이드
VEGA_RERANK=none python3 topsolar.py search "비금도"
VEGA_RERANK=vega_only python3 topsolar.py search "비금도"
```

### 모드별 동작

| 모드        | QMD 리랭커          | Vega 퓨전 (`_rerank_fusion`) | 결과 순서                       |
| ----------- | ------------------- | ---------------------------- | ------------------------------- |
| `full`      | qwen3-reranker 활성 | 토큰 매칭 + 복합 스코어링    | 퓨전 점수 기준                  |
| `vega_only` | `--no-rerank` 전달  | 토큰 매칭 + 복합 스코어링    | 퓨전 점수 기준                  |
| `none`      | `--no-rerank` 전달  | 스킵                         | SQLite BM25 원본 + QMD 네이티브 |

### 변경 파일

| 파일        | 변경                                                                              |
| ----------- | --------------------------------------------------------------------------------- |
| `config.py` | `RERANK_MODE` 설정 추가 (환경변수 `VEGA_RERANK` 오버라이드)                       |
| `router.py` | `RERANK_MODE` import, QMD `--no-rerank` 조건부 전달, `_rerank_fusion` 조건부 스킵 |

### 검색 결과에 모드 표시

`results['rerank_mode']` 필드로 현재 적용된 리랭킹 모드를 확인할 수 있습니다.

## vega v1.33 — 크래시 방어 + 테스트 인프라 정비

### A. 크래시 버그 수정 (5건)

- **A1. router.py `_rerank_fusion` entry_date 정렬**: `-(is not None)` 불리언 로직 반전 + int/str 혼합 비교 TypeError. `0 if truthy else 1` + `str()` 래퍼로 수정. QMD 정렬에 `_qmd_name_cache` dict 도입 (반복 호출 제거)
- **A2. show.py content NULL**: `c['content'][:800]` → `(c['content'] or '')[:800]`
- **A3. ask.py 대명사 치환 NULL**: `row['name']`이 None일 때 `.replace()` TypeError. `name = row['name'] or ''`로 방어
- **A4. core.py `person.split()[0]` IndexError**: 빈 문자열 `.split()` → `[]` → `[0]` 크래시. `if person and person.split()` 가드 추가
- **A5. write.py `fetchone()[0]` NULL**: `_row = conn.execute(...).fetchone()` → `new_cid = _row[0] if _row else None` + 가드

### B. 안전성 개선 (3건)

- **B6-B8. router.py silent exception → logging.debug**: 3곳의 `except Exception: pass` → `except Exception as e: _log.debug("... 실패: %s", e)`. `import logging` + `_log = logging.getLogger(__name__)` 추가
- **router.py `_row_value` 추가 방어**: `_sqlite_rows_to_unified` 내 `project_id=_row_value(r, 'project_id', '')`, `_qmd_items_to_unified` 내 `str()` 래퍼

### D. 코드 품질 (3건)

- **D9. 검색 limit 상수화**: `_CHUNK_LIMIT=50`, `_LIKE_LIMIT=30`, `_COMM_LIMIT=15` — 하드코딩 매직넘버 제거
- **D10. \_rerank_fusion `_row_value` 적용**: `row['project_id']`/`row['name']` 직접 접근 → `_row_value(row, ...)` 안전 접근
- **D11. QMD 정렬 `_qmd_name_cache`**: `id()` 기반 dict 캐시로 `_infer_qmd_project_name()` 반복 호출 제거

### E. 테스트 인프라 정비 — DB_PATH import-time 바인딩 해소

- **원인**: `from core import DB_PATH`가 모듈 로드 시점에 값을 캡처 → 테스트 픽스처의 `config.DB_PATH` 패치가 전파 안 됨 (22/61 실패)
- **수정**: 모든 commands에서 `from core import DB_PATH` 제거 → `import config` + `config.DB_PATH` 호출 시점 참조로 전환
- **대상 파일 (10개)**: search.py, contacts.py, compare.py, cross.py, dashboard.py, pipeline.py, weekly.py, system.py, write.py, qmd_ops.py
- **write.py**: `DB_PATH`/`MD_DIR` import만 있고 사용처 없음 → import만 제거
- **system.py**: `DB_PATH` 6회 + `MD_DIR` 6회 → 모두 `config.DB_PATH`/`config.MD_DIR`로 전환
- **설계 원칙**: DB 경로는 `config.DB_PATH`(호출 시점 참조) 또는 `get_db_connection()`(내부에서 config 참조) 경유. 모듈 레벨 바인딩 금지

### 테스트

- 58개 테스트 케이스 (v1.32와 동일 수)
- 테스트 픽스처 호환성 복구 (22개 실패 → 0개)

## vega v1.32 — 버그 수정 + 코드 리뷰

### 버그 수정

- **`_row_value()` SQL NULL 크래시**: `row[key]`가 None(SQL NULL)을 반환할 때 default 대신 None이 통과되어, 이후 문자열 연결 시 TypeError 발생 가능. `val if val is not None else default` 패턴으로 수정 — 양쪽 분기(try/except) 모두 적용
- **미사용 import 제거**: router.py에서 `get_qmd_executable` import 제거 (사용처 없음)

### 테스트

- **`test_row_value_none_safety`**: `_row_value`의 None 안전성 검증 — None row, None value, actual value, missing key 4가지 케이스
- 57 + 1 = **58개 테스트 케이스**

## vega v1.21 — 테스트 실패 수정

### 버그 수정

- **compare.py `sqlite3.Row`에 `.get()` 호출**: Row 객체는 dict가 아니라 `row['key']`로 접근해야 함. `(fetchone() or {}).get('c', 0)` → `row['c'] if row else 0` 패턴으로 3곳 수정 (stats 명령의 total, active, comm_total)
- **write.py 쓰기 명령 `read_only` 기본값**: `mail-append`, `update`, `add-action`이 `read_only=True`(기본값)로 등록되어 TestCommandMetadata 실패. `read_only=False, category='write'` 명시 추가
- **write.py `urgent`/`person` 중복 등록**: `urgent.py`, `person.py`에 올바른 메타데이터로 이미 존재하는데, `write.py`에도 중복 등록되어 나중에 로드되면서 메타데이터를 덮어씀. write.py에서 160줄 분량의 중복 코드 제거
- **write.py 미사용 import 정리**: `re`, `datetime`, `_escape_like` 제거 (urgent/person 코드와 함께 불필요해짐)

### 테스트 결과

- 48/48 통과 (이전: 46/48)

## vega v1.2 — 개발자 인프라 (패치 속도 + 안전성)

### 공개 API 경계 (core.py)

- **`__all__` 선언**: commands/가 import할 수 있는 이름 목록 명시. 비공개 함수 변경 시 commands/ 영향 즉시 파악
- **공개 함수명 정리**: `_find_project_id` → `find_project_id`, `_get_flag` → `get_flag` 등 22개 함수에 공개 별칭 추가
- **하위 호환**: 기존 `_` 접두어 이름도 계속 작동 (별칭 패턴)

### 명령 파일 정리

- **write.py 분리**: urgent, person을 별도 파일로 분리 → 읽기/쓰기 명령 명확히 구분
- **파일 구조**: write.py(3개 쓰기), urgent.py, person.py, 나머지 읽기 명령 각자 파일

### register_command 메타데이터

```python
@register_command('urgent', read_only=True, category='query')
@register_command('update', read_only=False, category='write')
@register_command('health', read_only=True, category='system')
@register_command('ask', read_only=True, category='ai')
```

- **read_only**: 데이터 변경 여부. 테스트에서 쓰기 명령 자동 분리
- **category**: query/write/system/ai. 문서 자동 생성, 권한 제어에 활용

### 테스트 강화

- **TestCommandMetadata**: 모든 명령에 메타데이터 존재 확인, 읽기/쓰기 구분 검증
- **43 → 46 테스트 케이스**

### 미래 패치 시 작업 흐름

1. `commands/새파일.py` 생성 (자동 등록)
2. 공개 유틸리티는 `from core import find_project_id, get_flag` (밑줄 없음)
3. `python3 -m unittest test_topsolar -v` 로 회귀 확인
4. TestCommandMetadata가 메타데이터 누락 자동 탐지

## vega v1.19 — 모놀리스 분리 (유지보수성 아키텍처)

### 핵심: topsolar.py (2442줄) → core.py + commands/\*.py + topsolar.py (30줄)

명령 하나를 수정해도 다른 명령에 영향이 없는 구조. 새 명령 추가 시 `commands/` 에 파일만 추가하면 자동 등록.

### 파일 구조

```
vega-v1.19/
├── core.py              # 핵심 인프라 (~900줄)
│   ├── execute(), register_command, route_input
│   ├── _find_project_id, _get_flag, _extract_days 등 유틸리티
│   ├── _apply_depth, _build_ai_hint, _build_bundle (AI 헬퍼)
│   ├── _generate_summary, _apply_format
│   ├── 세션 관리, 오류 자동 교정
│   └── _load_commands() — commands/ 자동 디스커버리
├── commands/            # 명령 핸들러 (파일당 1~3개 명령)
│   ├── __init__.py
│   ├── search.py        # search
│   ├── dashboard.py     # dashboard
│   ├── brief.py         # brief, recent, _build_single_brief
│   ├── show.py          # show, list, timeline
│   ├── cross.py         # cross
│   ├── pipeline.py      # pipeline
│   ├── contacts.py      # contacts
│   ├── person.py        # person, urgent
│   ├── weekly.py        # weekly, changelog
│   ├── compare.py       # compare, stats
│   ├── ask.py           # ask (통합 엔드포인트)
│   ├── write.py         # update, add-action, mail-append
│   └── system.py        # health, template, sync-back
├── topsolar.py          # CLI 진입점 (~30줄) — core.main() 호출
├── config.py            # 설정 (변경 없음)
├── addons.py            # 플러그인 (변경 없음)
├── router.py            # 검색 라우터 (변경 없음)
├── project_db_v2.py     # DB 파서 (변경 없음)
├── mail_to_md.py        # 메일 삽입 (변경 없음)
├── md_editor.py         # .md 편집 (변경 없음)
└── test_topsolar.py     # 테스트 (core에서 import)
```

### 새 명령 추가 방법 (v1.19+)

```python
# commands/my_command.py 파일 생성만 하면 됨
from core import register_command, get_db_connection

@register_command('my-cmd')
def _exec_my_cmd(params):
    return {'result': 'hello', 'summary': '완료'}
```

파일 저장 → 자동 등록 → 즉시 사용 가능. 다른 파일 수정 불필요.

### 하위 호환성

- `topsolar-wrapper.sh` → `topsolar.py` → `core.main()` 체인 유지
- 기존 MCP 도구 스키마 변경 없음
- 테스트 스위트: `from topsolar import execute` → `from core import execute`

## vega v1.18 — 테스트 인프라 + 미래 안전성

### 종합 테스트 스위트 (test_topsolar.py 전면 재작성)

- **fixture DB 자동 생성**: 더 이상 `@skipUnless(DB 존재)` 불필요. `setUpModule`에서 임시 .md → SQLite 자동 빌드
- **10개 테스트 클래스, 40+ 테스트 케이스**: 모든 명령, NL 라우팅, 엣지케이스, ask, config, rglob 검증
- **DB 없이도 실행 가능한 테스트**: Korean NLP, FTS sanitize, 금액 추출, Config, 라우팅
- **rglob 재귀 스캔 검증**: 하위 디렉토리 .md 파일이 실제로 DB에 들어가는지 확인
- **ask 순환 참조 테스트**: `ask "ask something"` → search 폴백 검증
- **v1.13~v1.17 버그 회귀 테스트**: min_score, 중복 ID, 빈 쿼리, 존재하지 않는 프로젝트

### 테스트 실행

```bash
python3 -m unittest test_topsolar -v
```

수정 후 이 명령 한 번이면 전체 회귀 테스트 완료.

## vega v1.17 — 심층 버그 수정 (보안 + 엣지케이스 + 데이터 안전성)

### 보안 수정

- **SQL LIKE 와일드카드 인젝션**: `_` (단일문자) 이스케이프 — `_escape_like()` 헬퍼 추가. list, person, \_find_project_id에 적용
- **`_exec_ask` 공백 쿼리**: 공백만 있는 query `"   "` 통과 방지 — `.strip()` 추가

### 엣지케이스 안전성

- **`_apply_depth` compare/stats 누락**: brief 모드에서 compare, stats 명령 처리 추가
- **`_exec_compare` 중복 프로젝트 ID**: `seen_pids` set으로 중복 제거
- **`_build_bundle` projects[0] 안전**: `.get('id')` None 체크, `or []` 패턴
- **`_generate_summary` None 안전**: `data.get('projects') or []` 패턴 일괄 적용

### 데이터 안전성

- **세션 파일 원자적 쓰기**: `tempfile.mkstemp` → `os.replace` — Windows 호환 동시접근 안전
- **BOM 처리 일괄 적용**: project_db_v2, mail_to_md, SyncBack 모두 `utf-8-sig`
- **symlink 루프 방지**: `rglob("*.md")`에 `is_symlink()` 필터 (project_db_v2, mail_to_md)
- **FTS5 컬럼필터 `:` 이스케이프**: `project_name:비금도` FTS 구문 오해석 방지
- **mail_to_md 본문 2000자 제한**: 초장문 메일 .md 삽입 크기 제어

### 로직 수정

- **search 0건 auto-correct**: status=ok + 결과 0건일 때도 작동
- **multi-brief summary 도달**: single-brief에 가려지던 문제 수정
- **weekly/changelog 매칭**: `_route_confidence`에서 `weekly_or_changelog` 처리
- **CrossAnalysis.\_schedule() None 날짜**: start/end 모두 None 체크
- **mail_to_md 동점 처리**: project ID 기준 결정적 정렬
- **Pipeline 금액 중복 집계**: 3개 겹치는 `억` 패턴 통합 — 이중 카운팅 방지
- **전화번호 패턴 개선**: 유선(02-xxxx, 031-xxx) 추가, 긴 숫자열 오매칭 방지
- **\_preprocess_korean 타입 안전**: 비문자열 → 빈 리스트

## vega v1.16 — 버그 수정

### 재귀 스캔 버그 수정 (Critical)

- **`.glob("*.md")` → `.rglob("*.md")`**: projects/ 하위 디렉토리의 .md 파일을 못 찾던 문제 수정
  - `project_db_v2.py`: DB 임포트 시 하위 폴더 .md 파일 누락 (2곳)
  - `mail_to_md.py`: 메일 프로젝트 매칭 시 하위 폴더 .md 파일 누락
  - `md_editor.py`: .md 파일 경로 찾기 시 하위 폴더 누락
  - `topsolar.py health`: .md 파일 개수 집계 시 하위 폴더 누락

### 커넥션 누수 수정

- **topsolar.py**: 여러 명령 핸들러에서 `conn.close()`가 `try/finally`로 보호되지 않던 곳 수정
- **addons.py \_load_projects()**: 예외 발생 시 커넥션 누수 방지 (`try/finally` 추가)
- **router.py sqlite_search()**: 커넥션 관리 안전성 강화
- **md_editor.py update_db_field()**: 커넥션 누수 방지 (`try/finally` 추가)
- **mail_to_md.py**: 프로젝트 인덱스 로드 시 커넥션 안전성 강화

### mail_to_md.py 버그 수정

- **`import sys` 누락**: CLI 실행 시 NameError 크래시 — sys 임포트 추가
- **None 필드 `.strip()` 크래시**: mail_data의 None 값에서 AttributeError — `(... or '').strip()` 패턴
- **`_auto_sync_db()` 커넥션 누수**: `try/finally` 래핑
- **잘못된 날짜 허용**: `datetime.strptime()` 검증 추가

### md_editor.py 버그 수정

- **`update_db_field()` 커넥션 누수**: `try/finally` 래핑
- **.md 파일 백업 추가**: 덮어쓰기 전 `_backup_file()` — 실패 시 복구 가능

### 기타 버그 수정

- addons.py `_json_default`: sqlite3 미임포트 시 NameError 방지
- topsolar.py `_exec_ask`: 순환 재귀 가드 강화
- topsolar.py `_try_auto_correct`: 무한 루프 방지 depth 가드

## vega v1.15 — AI 소비자 최적화

### 핵심 변화: 도구 19개 → 4개

MCP 도구를 `ask`(읽기 통합) + `update` + `mail-append` + `add-action`(쓰기 3개)로 재설계.
AI의 도구 선택 복잡도가 O(19) → O(4)로 감소. 기존 19개 명령은 내부에서 그대로 작동.

### AI 행동 가이드

#### 도구 선택 원칙

- 읽기 질문 → `ask` (하나로 통일)
- "바꿔줘/변경해줘" → `update`
- "기록해줘/넣어줘" (메일) → `mail-append`
- "추가해줘/할 일에 넣어줘" → `add-action`
- 도구 선택이 불확실하면 → `ask`

#### 응답 깊이 판단 (depth)

- "어때?" "상태?" "진행 중이야?" → `depth=brief`
- "자세히" "전부" "다 알려줘" → `depth=full`
- 기본 → `depth=normal`

#### 응답 내 특수 필드 활용

- `_ai_hint`: 상황별 AI 행동 가이드. 검색 결과가 많으면 "상위 3개만 언급", 긴급 항목이 있으면 "먼저 언급" 등
- `_bundle`: 후속 질문 대비 선제 데이터. brief/person/search 결과에 포함. 추가 호출 없이 답변 가능
- `_meta`: 라우팅 정보 (routed_to, confidence). 자동 교정 여부 확인

#### 흔한 대화 패턴

| 사용자 발화          | AI 행동                                               |
| -------------------- | ----------------------------------------------------- |
| "오늘 뭐 급해?"      | `ask(query, depth=brief)`                             |
| "비금도 어떻게 돼?"  | `ask(query, depth=normal)`                            |
| "자세히 알려줘"      | `ask(query, depth=full)` — 이전 프로젝트 context 전달 |
| "상태 완료로 바꿔줘" | `update(project=이전 맥락, status="완료 🟢")`         |
| "이 메일 기록해"     | `mail-append(subject, sender, ...)`                   |
| "그 프로젝트"        | `ask(query, context={recent_project_ids: [ID]})`      |

#### 응답 구성 원칙

- `_ai_hint`에 `has_critical`이 있으면 → 긴급 항목 먼저, 경고 톤
- `_ai_hint`에 `has_risks`이 있으면 → 리스크 별도 강조
- `_ai_hint`에 `too_many_results`이 있으면 → 상위 3개만 언급, "더 보실래요?"
- `_ai_hint`에 `no_actions`이 있으면 → 액션 추가 제안
- `_bundle`에 데이터가 있으면 → 후속 질문에 추가 호출 없이 답변
- `_meta.auto_corrected_to`가 있으면 → 자동 교정되었음을 짧게 언급

### 신규 기능

- **ask 통합 엔드포인트 (E-1)**: 자연어 그대로 전달 → 내부 NL 라우팅 → 최적 명령 자동 실행. `_meta.routed_to`로 어떤 명령이 실행됐는지 확인
- **depth 파라미터 (E-2)**: brief/normal/full. brief는 핵심 필드만 (토큰 50-80% 절약). show의 sections/comms, search의 content 등 제거
- **\_ai_hint (E-3)**: 응답 내 상황별 AI 행동 가이드. search 0건/1건/5+건, urgent critical/all_clear, brief risks/no_actions 등
- **\_bundle 선제 번들링 (E-4)**: brief→urgency+recent_3d+related, person→this_week+urgent_projects, search 단일→auto_brief. 연쇄 호출 제거
- **세션 컨텍스트 (E-5)**: .session.json에 최근 조회 프로젝트 기록. "그 프로젝트"/"거기" 등 대명사를 세션에서 해석. context 파라미터로 AI가 명시 전달도 가능
- **MCP 4개 도구 (E-6)**: ask(읽기 통합) + update + mail-append + add-action. 기존 19개 내부 명령은 그대로 유지
- **오류 자동 교정 (E-7)**: show/brief 프로젝트 못 찾음→search 전환, person에 프로젝트명→brief 전환, search 0건+suggestions→첫 후보 brief

## vega v1.13

### 신규 기능

- **compare 명령**: 2개 이상 프로젝트 나란히 비교 — 공유/고유 거래처·자재·인력, 상태·용량 비교. `compare 5 7` 또는 `compare "비금도" "화성산단"`
- **stats 명령**: 포트폴리오 통계 분석 — 커뮤니케이션 빈도, 월별 트렌드, 담당자 부하, 미활동 프로젝트. `stats`
- **list 필터링**: `--status`, `--person`, `--client` 필터 지원. `list --status "진행 중"`, `list --person "고건"`
- **멀티 브리프**: `brief 5 7 12` 또는 `projects` 배열로 여러 프로젝트 한 번에 브리프
- **--format 출력 모드**: summary(기본), detail, markdown(표), ids(ID만) — `search "케이블" --format markdown`
- **--min-score 필터**: 검색 결과에서 관련도 낮은 노이즈 제거. `search "모듈" --min-score 30`
- **match_methods 투명성**: 검색 결과에 매칭 방식 표시 (fts5_strict/fts5_broad/trigram/like_fallback)
- **audit_log 테이블**: update, add-action 변경 시 누가(user/ai/auto) 무엇을 바꿨는지 자동 추적
- **MCP 도구 18개**: compare, stats 추가. list/search/brief 스키마 확장

### 검색·출력 품질

- **summary에 프로젝트명 포함**: search, urgent 등의 summary에 상위 프로젝트명 3개 표시. AI가 한 번에 맥락 파악 가능
- **커뮤니케이션 날짜순 정렬**: comm 검색 결과를 날짜 우선 정렬 (기존: BM25 우선). 최신 메일이 항상 위
- **list 필터 summary**: 필터 적용 시 "3개 프로젝트 (필터: status=진행 중)" 형태

### 코드 품질

- **VegaError 예외 클래스**: 에러 응답 형태 통일 ({error, error_type, usage}). 기존 dict 기반 에러와 공존
- **db_session() 컨텍스트 매니저**: `with db_session() as conn:` — conn.close() 누락 방지. 기존 get_db_connection()도 유지
- **Changelog/Dashboard 중복 제거**: `_compute_changes()`, `_compute()` 공통 메서드 추출. run()/api() 코드 50% 감소
- **\_load_projects() mtime 캐시**: DB 파일 변경 시에만 재로드. router.py의 \_pattern_cache와 동일 패턴
- **write_audit_log() 유틸리티**: config.py에 공용 함수. 실패해도 메인 동작 미차단
- **스키마 버전 4**: PRAGMA user_version = 4. audit_log 테이블 추가. 기존 DB 자동 마이그레이션

## vega v1.12

### 신규 기능

- **brief 명령**: 프로젝트 원페이지 브리프 — 상태, 다음 액션, 리스크, 최근 커뮤니케이션을 한눈에. show보다 간결하고 액션 중심
- **recent 명령**: 최근 활동 피드 — N일간 커뮤니케이션 + 변경을 시간순으로. `--days`, `--limit` 지원, 프로젝트 필터 가능
- **검색 제안**: 검색 결과 0건일 때 프로젝트/고객사/담당자 후보를 fuzzy matching으로 제안 (`suggestions` 필드)
- **follow_up_hint**: 검색 결과에 다음 명령 제안 (show/brief/timeline)
- **자연어 라우팅**: "한눈에 요약"→brief, "최근 활동"→recent 자동 매칭
- **\_find_project_id_in_text()**: 자연어 안에서 프로젝트명을 fuzzy 매칭하여 ID 반환
- **\_row_value()**: sqlite3.Row/dict 양쪽에 안전한 필드 접근 유틸리티
- **MCP 스키마**: brief, recent 도구 추가 (교차 참조 description 포함)

### 안정성 (v1.11 병합)

- **"도" 조사 버그 수정**: 비금도/진도/완도 등 지명 보존. "도"를 복합조사(에서도/까지도/만도)로만 처리
- **어미 제거 보강**: `_TRAILING_ENDINGS`에 `중인`, `된` 추가 — "공사중인"→"공사", "완료된"→"완료"
- **stopwords 보강**: 설계중인/시공중인/검토중인 + 어떻게/어떤/무슨/대해서 등
- **기계적 2글자 분할 제거**: `_preprocess_korean()`에서 노이즈 분할 삭제 — trigram FTS에 위임
- **QMD 필터 recall 복구**: 필터 매칭 실패 시 빈 결과 대신 unfiltered 반환 + `filter_bypassed` 플래그. fusion에서 0.3배 하향
- **add-action 태그 재연결**: chunk DELETE→INSERT 후 `extract_tags()` 재실행하여 `chunk_tags` 매핑 보존
- **fetchone() None 안전성**: `config.py check_schema_version()`, `topsolar.py health` 분류 통계
- **conn.close() try/finally**: timeline, show, list, add-action에 적용 — 예외 시 커넥션 누수 방지
- **QMD 필터 우회 표기**: `analysis.reason`에 "QMD 프로젝트 필터 우회됨" 표시
- **pyc 캐시 미포함 배포**
