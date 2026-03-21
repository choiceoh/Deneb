# Vega 프로젝트 검색엔진 — AI 사용 가이드

이 시스템은 태양광/풍력/ESS 프로젝트 데이터를 .md 파일로 관리하고,
SQLite DB로 변환하여 검색/분석하는 도구입니다.

## 도구 호출

```bash
python3 vega.py <명령> [인수...]
# 또는
./vega-wrapper.sh <명령> [인수...]
```

모든 응답은 JSON:
```json
{"command": "...", "timestamp": "...", "status": "ok|error", "data": {...}, "summary": "한 줄 요약"}
```

## MCP 도구 (v1.42)

**`ask` 하나로 모든 읽기 질문 해결.** 쓰기만 개별 도구 사용.

| 도구 | 역할 | 예시 |
|------|------|------|
| **`ask`** | 모든 읽기 질문 | `ask "비금도 어떻게 돼?"`, `ask "급한거"`, `ask "비용 얼마"` |
| **`update`** | 상태/필드 변경 | `update project=5 status="완료 🟢"` |
| **`mail-append`** | 메일 기록 | `mail-append subject="..." sender="..."` |
| **`add-action`** | 액션/이력 추가 | `add-action project=5 text="FAT 출장 확정"` |

### ask 입력 관용성 (v1.42)

**개떡같이 써도 찰떡같이 나옵니다:**
- 오타 자동 보정: "비금또" → 비금도 프로젝트 매칭 (difflib 퍼지)
- 프로젝트명만 입력: `ask "비금도"` → 자동으로 brief 제공
- 조사/문장부호 무시: "비금도는 어떻게 돼?" = "비금도 어떻게 돼"
- 검색 0건 → 유사 프로젝트 brief 자동 첨부 (`_auto_brief`)
- 라우팅 키워드: 비용/예산→pipeline, 마감/납기→urgent, 일정/스케줄→timeline

### ask 응답 구조

| 필드 | 설명 |
|------|------|
| `_meta` | 라우팅 정보 (routed_to, confidence, auto_corrected_to) |
| `_ai_hint` | AI 행동 가이드 (상황별 안내) |
| `_bundle` | 다음에 필요할 데이터 (auto_brief, related_projects 등) |
| `match_reasons` | 검색 매칭 사유 (프로젝트명/본문/의미검색/커뮤니케이션) |
| `_auto_brief` | 검색 0건 시 퍼지 매칭된 프로젝트 brief |

`ask` 내부 자동 라우팅: search, brief, show, urgent, pipeline, person, dashboard, timeline, list 등 18개 명령

## Upgrade 명령 (v1.45)

버전 업그레이드 후 단일 명령으로 전체 정비:

```bash
python3 vega.py upgrade          # 증분 (변경분만)
python3 vega.py upgrade --force  # 전체 재빌드
```

수행 단계:
1. DB 스키마 마이그레이션 (`SCHEMA_VERSION` 체크)
2. .md 파일 증분 재파싱 (해시 기반 변경 감지)
3. memory 파일 증분 업데이트
4. 미임베딩 청크 벡터 임베딩
5. FTS 인덱스 정합성 확인

응답에 `models` 섹션 포함 — 감지된 모델 경로 보고.

### 모델 자동 감지 (v1.45)

모델 디렉토리 탐색 순서:
1. `VEGA_MODELS_DIR` 환경변수
2. `~/.vega/models/`
3. `./models` (Vega 소스 옆)

각 모델은 디렉토리 내 glob 패턴으로 자동 매칭:
- embedder: `qwen3-embedding*`, `*embedding*.gguf`
- reranker: `qwen3-reranker*`, `*reranker*.gguf`
- expander: `Qwen3.5-9B*`, `*expander*.gguf`

개별 모델 경로 오버라이드: `VEGA_MODEL_EMBEDDER`, `VEGA_MODEL_RERANKER`, `VEGA_MODEL_EXPANDER`

## Memory Backend (v1.44)

OpenClaw이 Vega를 memory backend로 사용하는 4개 CLI 명령. **기존 Vega 명령과 다른 출력 규약** 사용.

```bash
# 인덱싱 (증분, --force로 전체)
python3 vega.py memory-update [--force]

# 벡터 임베딩 생성
python3 vega.py memory-embed [--force]

# 검색 → bare JSON 배열
python3 vega.py memory-search "키워드" --json --limit 5 [--collection name]

# 상태 → bare JSON 객체
python3 vega.py memory-status --json
```

### 출력 규약 (execute() 우회)

- **stdout**: bare JSON (envelope 없음, `{command, status, data}` 래핑 없음)
- **stderr**: 에러 메시지
- **exit code**: 0=성공, 1=에러
- `core.py main()`에서 `memory-*` 명령은 `_run_memory_command()`로 바이패스

### memory-search 응답

```json
[{"path": "memory/note.md", "startLine": 1, "endLine": 5, "score": 0.85, "snippet": "...", "source": "memory"}]
```

### memory-status 응답

```json
{"files": 45, "chunks": 320, "embedded": 315, "model": "qwen3-embedding-8b-q4_k_m.gguf", "dbPath": "/path/to/projects.db"}
```

### 설정 (환경변수)

- `VEGA_MEMORY_PATHS`: 콤마 구분 경로 (기본: `MEMORY.md,memory,projects`)
- `VEGA_MEMORY_WORKSPACE`: workspace 루트 (기본: MD_DIR 부모)

### 스키마

- `projects.source_type`: `'project'`(기본) 또는 `'memory'`
- `chunks.start_line`, `chunks.end_line`: heading 기반 라인 번호
- `file_hashes.source_file`: memory 파일은 `memory:` 접두사 + 상대경로

## mail-append 사용법

메일 내용을 프로젝트 .md 파일에 자동 삽입합니다.

```bash
# 방법 1: 개별 인수 (권장 — 이스케이핑 불필요)
python3 vega.py mail-append \
  --subject "비금도 케이블 납기 건" \
  --sender "Christina Gu" \
  --date "2026-03-19" \
  --body "최종 ETD 5월 12일 목표" \
  --project "비금도"

# 방법 2: JSON 문자열
python3 vega.py mail-append '{"subject":"...", "sender":"...", "date":"...", "project":"..."}'
```

### mail-append 응답 해석

| status | 의미 | 다음 행동 |
|--------|------|----------|
| `ok` | 삽입 완료 | 없음 |
| `skipped` | 이미 존재 | 없음 |
| `candidates` | 프로젝트 매칭 불확실 | `candidates` 배열에서 선택 → `--project`로 재실행 |
| `no_match` | 매칭 실패 | 사용자에게 프로젝트 확인 요청 |
| `error` | 오류 | `error` 필드 확인 |

**중요**: 발신자가 여러 프로젝트에 관여하면 매칭이 불확실할 수 있습니다.
이 경우 항상 `--project "프로젝트명"`을 지정하세요.

## cross 명령 응답

`cross`는 하위 명령에 따라 응답 형태가 다릅니다:

```bash
cross vendors    → data.vendors: {거래처별 프로젝트 목록}
cross materials  → data.materials: {자재별 프로젝트 목록}
cross personnel  → data.personnel: {인력 충돌 목록}
cross schedule   → data.schedule: {일정 충돌}
cross synergy    → data.synergy: {시너지 기회}
cross all        → 위 5개 전부 포함
cross project 5  → data.project: {특정 프로젝트의 교차 정보}
```

## 오류 복구

### "DB 자동 재빌드 실패"
1. `health` 명령으로 상태 확인
2. MD_DIR 경로에 .md 파일이 있는지 확인
3. `python3 project_db_v2.py import <MD_DIR> --db projects.db` 수동 실행

### "매칭되는 프로젝트가 없습니다"
- `candidates` 필드가 있으면 후보에서 선택
- 없으면 `list` 명령으로 프로젝트 목록 확인 후 `--project`로 재시도

### 검색 결과 없음
- FTS5 예약어 (AND, OR, NOT)는 자동 이스케이프됨
- 특수문자 (O&M, 154kV)도 안전하게 처리됨
- 결과 없으면 키워드를 줄여서 재검색

## .md 파일 구조

```markdown
# 프로젝트명

| 항목 | 내용 |
|------|------|
| **상태** | 진행 중 🟢 |
| **발주처** | 고객사명 |
| **사내 담당** | 이름 직급 |
| **거래처 담당** | 이름 (회사) |

## 현재 상황
- 현재 진행 내용

## 이력
- 2026-03-19: 이벤트 설명

## 2026-03-19
- **메일 제목** (발신자)
  - 메일 요약
```

날짜 섹션은 최신이 위, `- **제목** (발신자)` 형식입니다.
mail-append는 이 형식으로 자동 삽입합니다.

## update 명령

프로젝트의 메타 필드(상태, 담당자 등)를 변경합니다. .md와 DB를 동시에 업데이트하고 이력에 자동 기록합니다.

```bash
update 5 --status "완료 🟢"
update "비금도" --status "긴급 대응 중 🔴"
update 5 --field "사내 담당" "고건 팀장"
```

## urgent 명령

관심이 필요한 프로젝트를 우선순위별로 보여줍니다:
- `critical`: 🔴 상태 프로젝트
- `overdue`: 다음 예상 액션의 기한이 지난 프로젝트
- `stale`: 30일 이상 커뮤니케이션 없는 활성 프로젝트
- `overloaded`: 5개 이상 프로젝트를 담당하는 인원

매일 업무 시작 시 `urgent`를 먼저 실행하세요.

## person 명령

특정 인물의 전체 프로젝트 포트폴리오와 최근 커뮤니케이션을 조회합니다.

```bash
person "고건"        → 고건의 내부/외부 담당 프로젝트 + 최근 메일
person "Christina"   → Christina의 관련 프로젝트 + 발신 이력
```

## add-action 명령

프로젝트의 "다음 예상 액션" 또는 "이력" 섹션에 항목을 추가합니다.

```bash
add-action 5 "FAT 출장 일정 확정 필요"
add-action "비금도" "2차 대금 결제 확인"
add-action 5 --history "CU 헷징 계약 체결 완료"
```

## 일일 워크플로우 (권장 순서)

1. `urgent` — 오늘 관심이 필요한 항목 확인
2. 메일 처리 → `mail-append`로 프로젝트에 삽입
3. `update` — 상태 변경이 필요한 프로젝트 업데이트
4. `add-action` — 다음 액션 항목 추가
5. `weekly` 또는 `dashboard` — 전체 현황 확인

## 개발자용: 패치 가이드

### 새 명령 추가

**방법 1: 일반 명령** — `commands/` 디렉토리에 파일 추가:

```python
# commands/my_command.py
from core import register_command

@register_command('my-cmd',
    summary_fn=lambda d: f"결과: {d.get('count',0)}건")
def _exec_my_cmd(params):
    sub_args = params.get('sub_args', [])
    return {'count': 42}
```

**방법 2: 애드온 브릿지** — `addon_command()` 한 줄로 등록:

```python
# commands/my_addon_cmd.py
from addons import MyAddon
from core import addon_command

addon_command('my-addon', MyAddon,
              summary_fn=lambda d: f"완료: {d.get('total',0)}건")
```

**방법 3: 프로젝트 ID 필요 명령** — `require_project()` 사용:

```python
from core import register_command, require_project, get_db_connection

@register_command('my-project-cmd',
    summary_fn=lambda d: f"[{d.get('id','')}] {d.get('name','')}")
def _exec(params):
    pid = require_project(params, usage_hint='프로젝트를 지정해주세요.')
    if isinstance(pid, dict):  # error dict
        return pid
    # pid는 int — DB 조회 진행
```

자동으로:
- `_load_commands()`가 파일을 발견하고 import
- EXPLICIT_COMMANDS에 등록
- execute()에서 라우팅
- 에러 핸들링 적용
- `summary_fn`이 있으면 자동 요약 생성 (core.py 수정 불필요)
- `summary_fn` 없으면 `data['summary']` 폴백

자연어 라우팅이 필요하면 core.py의 NL_ROUTES에 패턴 추가:
```python
NL_ROUTES.append((r'(내 패턴)', 'my-cmd'))
```

### 새 애드온 추가

`addons/` 패키지에 파일 추가:

```python
# addons/my_addon.py
from addons._base import BaseAddon

class MyAddon(BaseAddon):
    name = 'my-addon'
    description = '한 줄 설명'

    def run(self, cmd, args, ctx):
        data = {'result': 'hello'}
        ctx.output(data)

    def api(self, cmd, args, ctx):
        return {'result': 'hello', 'summary': '완료'}
```

그리고 `addons/__init__.py`의 `ADDONS` 리스트에 추가.

### 유틸리티 (core.py에서 import)
- `find_project_id(ref)` — ID/이름으로 프로젝트 ID 반환
- `require_project(params)` — params에서 프로젝트 ID 추출 (에러 dict 또는 int 반환)
- `addon_command(name, cls)` — 애드온 브릿지 한 줄 등록
- `get_flag(args, flag)` — CLI 인수에서 --flag VALUE 추출
- `extract_days(params)` — --days N 또는 자연어에서 일수 추출
- `extract_bullets(text)` — 마크다운 리스트 항목 추출
- `escape_like(s)` — SQL LIKE 와일드카드 이스케이프
- `build_single_brief(pid)` — 단일 프로젝트 브리프 데이터 생성
- `VegaError(msg, usage)` — 사용자 에러 던지기

### 테스트 작성

```python
class TestMyFeature(VegaTestCase):
    def test_something(self):
        r = self._exec('my-cmd')
        data = self._assert_ok(r)  # status='ok' 검증 + data 반환

    def test_search(self):
        r = self._search('키워드')  # search 명령 숏컷
        self._assert_ok(r)
```

테스트 실행:
```bash
python3 -m unittest test_vega -v
```

### 주의사항
- DB 경로는 반드시 `config.DB_PATH` (호출 시점 참조) 또는 `get_db_connection()` 사용
- `from config import DB_PATH`로 모듈 레벨에 바인딩 금지 (테스트 fixture 깨짐)
- addons에서 config 값 접근: `import config` → `config.DB_PATH`, `config.KNOWN_VENDORS` 등

## MCP 통합 참고

vega-wrapper.sh는 MCP 프로토콜과 호환됩니다:
- stdin으로 JSON 파라미터를 받아 모든 명령에 전달 가능
- `update`, `add-action`, `person`, `weekly`, `mail-append` 등은 MCP params에서 직접 인수 추출
- mcp-vega.json에 도구 스키마 정의

## 호환성

- **Windows/Unix**: 파일 읽기 시 `\r\n` → `\n` 자동 정규화, 경로 구분자 자동 처리
- **Python 경로**: `sys.executable` 사용 (python3 하드코딩 아님)
- **Shell scripts**: `python3` → `python` 자동 폴백 (Windows/다양한 환경 대응)
- **DB 경로**: `source_file` 필드에 절대 경로 저장 → `find_md_path()` 안정성 확보
- **FTS5**: 예약어/특수문자 자동 이스케이프, 실패 시 LIKE 폴백

## 복원력

- **컨테이너 재시작**: DB 없으면 자동 재빌드 (`_ensure_db()`), `.snapshot.json` 없으면 첫 스냅샷 자동 생성
- **MD_DIR 동적 탐색**: 기본 경로 없으면 OpenClaw 경로 자동 탐색 (버전별 glob 포함)
- **로컬 모델 미설치**: `config.py`에서 모델 디렉토리 자동 탐색, 못 찾으면 SQLite 폴백 (크래시 없음)
- **DB 자동 재빌드**: `Ctx.get_conn()`, `_ensure_db()`, `vega-wrapper.sh`, `bootstrap.sh` 4중 안전장치
- **증분 업데이트**: `file_hashes` 테이블로 변경 파일만 재파싱, 전체경로/파일명 호환
- **bootstrap.sh**: 세션 시작 시 DB 복구 + sync-db 데몬 재시작 + 헬스체크

## 안정성

- **DB 동시접근 안전**: 모든 모듈이 `get_db_connection()` 사용 — WAL 모드 + `busy_timeout=5000ms`
- **스키마 버전 관리**: `PRAGMA user_version`으로 DB 버전 추적 (`SCHEMA_VERSION = 6`)
- **즉시 DB 반영**: `mail-append`, `add-action`, `update` 후 .md ↔ DB 즉시 동기화 (sync-db 대기 불필요)
- **중복 방지**: chunks 테이블 INSERT 시 기존 동일 항목 DELETE 후 재삽입
- **Atomic DB 재빌드**: `sync-db.sh`에서 임시 파일 → mv 패턴 + `trap` 정리
- **빈 검색어 처리**: 빈 query에 사용법 안내 반환, 크래시 없음
- **JSON 파싱 에러**: `mail-append`에서 잘못된 JSON 입력 시 명확한 에러 + 올바른 형식 안내
- **매칭 키워드**: `search` 결과에 `matched_keywords` 필드 포함 — 어떤 키워드가 매칭됐는지 확인 가능
- **프로젝트 조회 통합**: `_find_project_id()` — ID/이름 모두 지원, 중복 코드 제거

## 변경 이력

상세 변경 이력은 [CHANGELOG.md](CHANGELOG.md)를 참조하세요.
