---
description: 위키 프로젝트 문서 레이아웃 규약 (프로젝트당 폴더 + 고정 슬롯)
globs:
  - "gateway-go/internal/domain/wiki/**"
  - "gateway-go/internal/runtime/server/wiki_*.go"
  - "gateway-go/internal/pipeline/chat/tools/wiki.go"
  - "gateway-go/internal/pipeline/chat/tools/wiki_ingest.go"
---

# Wiki Project Layout (프로젝트 문서 스키마)

> **단일 진실원: `gateway-go/internal/domain/wiki/project_layout.go`.** "무엇이
> 프로젝트 페이지인가"를 판단하는 코드는 반드시 이 파일의 헬퍼를 쓴다 —
> `strings.Count(p, "/") == 1` 같은 자체 규칙 복제 금지 (2026-07 중복 문서 사태의
> 뿌리가 이 규칙 복제 + 검색 없는 생성이었다).

## 레이아웃 (2026-07 정형화)

```
프로젝트/<프로젝트명>/
├── 대표.md      ← 대표페이지: 현재 상태·개요·핵심 사실 (digest/status/candidate 대상)
├── 로그.md      ← 진행 로그: 사건·회의·결재는 새 페이지가 아니라 여기에 날짜와 함께 append
│                  섹션 제목 문법: '## [YYYY-MM-DD] <op> | <주제>' (op: 회의/결정/발주/이슈/ingest …)
├── 기자재/      ← 케이블·모듈 등 자재 문서
├── 메일분석/    ← 메일 1통 = 1페이지 (시스템 자동 생성; 손으로 만들지 말 것)
├── 자료/        ← 외부 소스(URL·유튜브) 캡처, 소스 1개 = 1페이지 (wiki action="ingest"가 생성;
│                  손으로 만들지 말 것 — 정규화 URL 멱등, frontmatter resource가 키)
└── 회의록/      ← 회의 녹음 분석, 녹음 1개 = 1페이지 (plaud_recordings.go가 생성;
                   손으로 만들지 말 것)

프로젝트/거래/      ← 거래처 단위 원장 (프로젝트 횡단이라 프로젝트 폴더 밖)
프로젝트/메일분석/  ← 프로젝트 미연결 메일 분석 버킷
프로젝트/자료/      ← 프로젝트 미연결 자료 버킷
프로젝트/회의록/    ← 프로젝트 미연결 회의록 버킷
```

- **레거시**: 이관 전 대표페이지는 flat `프로젝트/<이름>.md`, 메일분석은
  `프로젝트/mail-analyses/`. 헬퍼들은 전환기 동안 두 형태를 모두 인식한다.
- **이관 도구**: `cmd/wiki-restructure` (dry-run 기본, `--plan` JSON으로 판단성
  병합/폴딩 지정, `--apply` 실행). **적용 전 게이트웨이 정지 필수** — Store 락은
  프로세스 내부용이고 FTS 인덱스는 메모리 상주(기동 시 재구축)다.

## 헬퍼 (project_layout.go)

| 질문 | 헬퍼 |
|---|---|
| 이 경로가 대표페이지인가 | `IsProjectRepPage(path)` |
| 이 경로의 소유 프로젝트는 | `ProjectNameOf(path)` / `ProjectFolderOf(path)` |
| 대표/로그/메일분석/자료/회의록 경로 생성 | `RepPagePath` / `LogPagePath` / `MailAnalysisPagePath` / `MaterialPagePath` / `MeetingPagePath` |
| 원시 데이터(메일·거래·자료·회의록)인가 | `IsProjectRawDataPath(path)` (자료만: `IsMaterialPath`) |
| flat 프로젝트 경로 정규화 | `NormalizeProjectPagePath(path)` (쓰기 경로에서 호출) |
| 프로젝트 열거 | `Store.KnownProjects()` |

## 현장(sites) 작성 규칙 (2026-07 확정)

프로젝트 대표페이지 머리말 `sites:` — 현장 위치의 **정본 값** (code처럼 이동-안정 정체성).

- **형식 고정**: `광역약칭 시/군 읍/면/동 [리]` — 공백 구분, 번지·도로명·마침표 금지.
  예: `전북 군산시 옥구읍 수산리`, `전남 신안군 비금면`, `충남 당진시 송악읍`
- 광역은 약칭 고정(전북·전남·경북·경남·충북·충남·강원·경기·제주·서울·부산·대구·인천·광주·대전·울산·세종) — 파서가 전체명을 자동 축약(`page.go:normalizeSiteName`).
- 복수 현장(물류센터 N개소)은 배열로. 확인 안 된 단계는 아는 데까지만, **추측 금지**.
- **소비처**: 회상 프로젝트 앵커(`project_anchor.go:bestProjectKeyIn` — 현장 전체형 + 말단 행정단위가 매칭 키)와 미팅 하베스트가 "수산리 현장" 류 장소 지칭을 프로젝트로 해석하는 근거.
- 기입 주체: 위키 도구 `sites` 파라미터·드리머·리서치 태스크 (전부 규칙이 프롬프트에 내장).

## 특성(kinds) 작성 규칙 (2026-07 확정 — 실데이터 분포 기반)

대표페이지 머리말 `kinds:` — 프로젝트가 사업적으로 무엇인지. **2단 고정 체계** `1차` 또는 `1차/2차` (복수 허용, 2026-07-06 운영자 확정):

| 1차 | 2차 | 뜻 |
|---|---|---|
| 태양광 | 토지 · 루프탑 · 수상 · ESS | 발전소 사업 — **구 시공·개발 1차 통합** (2차=설치·플랜트 유형; **ESS 사업도 태양광**) |
| 기자재 | 모듈 · 인버터 · 케이블 · 기타 | 기자재 공급 (2차=품목) |
| 풍력 | 육상 · 해상 | 풍력 사업 |
| 기타 | 용역 · 협력 | 서비스(안전관리대행 등)·NDA·제휴 |

- 2차를 모르면 1차만 기입, 확인되면 세분화. 자식이 있으면 맨부모는 자동 제거(자식이 부모를 함의 — 상위 질의는 접두 매칭으로).
- 어휘 밖 값은 파싱·렌더에서 **드롭**(`page.go:normalizeKinds`). 동의어·구세대 flat 값은 자동 승격(모듈→기자재/모듈, 루프탑→태양광/루프탑, BESS→태양광/ESS, 용역→기타/용역, 시공·개발→태양광) — 기존 저장 데이터는 손대지 않아도 읽기 시점에 새 체계로 수렴.
- **단계어 가드**: 구 1차 시공·개발(및 epc·턴키·dev·인허가)은 발전원을 말하지 않으므로 기본 태양광으로 접히되, 같은 목록에 풍력이 명시돼 있으면 드롭된다 — 풍력 개발 프로젝트([풍력, 개발])가 유령 태양광을 얻지 않게(`page.go:kindStageWords`).
- code의 거래타입 세그먼트(dev/epc/mod/…)와 의미 정합 — kinds가 사람이 읽는 정본.

## 불변식

- 프로젝트 밑 **flat `.md` 신규 생성 금지** — 드리머(`dreamer_apply.go`)와 위키
  도구(`tools/wiki.go`)가 `NormalizeProjectPagePath`로 강제한다. 새 쓰기 경로를
  추가하면 같은 정규화를 통과시킬 것.
- **깊이 강제**: 같은 함수가 스키마 초과 깊이를 파일명으로 접는다 — LLM 제목의
  날짜 슬래시("6/25 회의")가 유령 중첩 폴더를 만들던 실사고(2026-07-02, 드리머)
  차단. 허용 형태: `프로젝트/<이름>/<파일>` 또는 `프로젝트/<이름>/<기자재|메일분석>/<파일>`.
- **회상 앵커**: 질의가 활성 프로젝트를 이름으로 언급하면 recall이 그 대표페이지
  (현재 상태 포함)를 키워드 순위와 무관하게 최상위 증거로 주입한다
  (`chat/recall_evidence.go` + `Store.MatchProjectsInText` — 결정적, 3자 미만
  이름 가드, 종결 프로젝트 제외).
- **미분류 메일 재분류**: `프로젝트/메일분석/`의 미연결 메일은 리뷰어가 매 사이클
  결정적 신호(related가 프로젝트를 가리킴 / 제목이 정확히 한 프로젝트를 지목)로
  해당 프로젝트 메일분석 슬롯에 소급 파일링한다 (모호하면 잔류, 사이클당 10건).
- 사건·이벤트는 페이지 증식이 아니라 해당 프로젝트 `로그.md`에 append.
- **외부 소스 캡처는 `wiki(action="ingest", query=URL)`** (`tools/wiki_ingest.go`): 웹/유튜브를
  lightweight 요약(fail-open — 실패 시 발췌로 캡처 보존)+바운드 발췌와 함께 자료 페이지로 영속화.
  같은 URL은 멱등(트래킹 파라미터 제거·유튜브 변형 정규화), 갱신은 `force=true`. `project=` 연결은
  대표페이지 존재를 검증(오타 유령 폴더 방지 — 없으면 전역 버킷)하고 로그에 `ingest` op 섹션을 남긴다.
- **출처 규율 (부유 주장 금지)**: 소스가 있는 주장은 그 [[페이지]]/ref를 병기하고, 소스 없는 합성
  추론은 `> 합성:` 인용으로 시작한다 — 드리머 프롬프트와 wiki write 도구 설명이 같은 규율을 강제.
- 페이지 이동은 `Store.MovePage` (인바운드 related 재지향 포함), 병합은
  `Store.MergePage`. 파일을 직접 mv/rm 하지 말 것.

## 프로젝트 생애주기 (종결/재개)

- **종결** = `Store.CloseProject` (도구: `wiki(action="close", query=이름, content=결과)`).
  대표페이지 `## 종결` 섹션에 날짜·결과 기록 + **폴더 전체 Archived 플래그**.
  파일 이동·삭제 없음(경로 안정성). `knownProjects`가 archived 대표를 건너뛰므로
  **한 플래그로 활성 무대 전체**(메일 연결 후보·모아보기·리서치·리뷰어 코드신호)에서
  동시 퇴장. 검색은 뒷순위로만 밀림. stale-deadline 감지도 archived를 건너뜀.
- **재개** = `Store.ReopenProject` (`action="reopen"`) — 폴더 전체 복원, 종결 이력은
  섹션에 남음.
- **졸음 감지** = `Store.FlagDormantProjects` (wiki-review 태스크가 사이클당 최대 2건):
  120일(`DormantAfterDays`) 무활동 활성 프로젝트의 대표페이지 현재 상태에 "종결 검토
  제안" 불릿 1개 (분기당 1회 멱등, ref=`dormant:YYYYQn`). **자동 종결은 절대 없음.**
- 주의: 종결된 프로젝트로 오는 새 메일은 자동 연결되지 않는다(미분류 버킷행) —
  뒤늦은 정산 메일이 예상되면 종결을 늦추거나 재개로 되살린다.

## 중복 방어 3겹 (모두 `FindSimilarPages` 공유 — ID·코드·슬러그·FTS 제목 신호)

1. **쓰기 전 가드** — 위키 도구 write가 신규 생성 시 유사 문서를 찾으면 생성을
   거부하고 기존 경로를 안내 (`force=true`로만 강행). 드리머의 create-dedup도
   같은 프리미티브.
2. **위키 리뷰어** (`runtime/server/wiki_review_task.go`, 2h) — 최근 쓰인 문서의
   근사중복을 analysis 역할 단일 JSON 판정으로 사후 검수. **기본은 관찰 모드**
   (판정만 state 파일 `observed`에 기록) — 판정 품질 확인 후
   `DENEB_WIKI_REVIEW_AUTOMERGE=1`로 자동 병합을 무장한다 (사이클당 3건, git
   스냅샷 선행). 같은 프로젝트 폴더의 대표/로그/상세와 로그 슬롯은 후보 제외.
   로그 회전(`RotateProjectLog`: 로그.md 최신 20섹션 유지, 초과분 → 로그-보관.md
   archived)과 죽은 링크 정리(`PruneDeadRelatedLinks`: related의 죽은 참조를
   결정적으로 복구{정확경로→레거시flat→유일 basename→유일 제목→유일 ID} 또는
   제거, 모호하면 추측 없이 제거, Updated 미갱신)도 이 태스크가 수행.
3. **드림 verify** (`verify.go` Phase 5) — 정규화 제목 일치 자동 병합, 유사 제목
   advisory, 30일 방치 superseded 자동 아카이브, **90일 지난 메일분석 자동
   아카이브**(보존 정책). 사이클당 fix 15건 상한.
