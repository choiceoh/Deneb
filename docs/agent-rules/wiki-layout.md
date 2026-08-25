---
description: 위키 프로젝트 문서 레이아웃 규약 (프로젝트당 폴더 + 고정 슬롯)
globs:
  - "gateway-go/internal/domain/wiki/**"
  - "gateway-go/internal/runtime/server/wiki_*.go"
  - "gateway-go/internal/runtime/wikiwork/**"
  - "gateway-go/internal/pipeline/chat/tools/wikitool/wiki.go"
  - "gateway-go/internal/pipeline/chat/tools/wikitool/wiki_ingest.go"
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
│                  섹션 제목 문법: '## [YYYY-MM-DD] <op> | <주제>' (op: 회의/결정/발주/이슈/ingest/질문해결/질문폐기 …)
├── 기자재/      ← 케이블·모듈 등 자재 문서
├── 메일분석/    ← 메일 1통 = 1페이지 (시스템 자동 생성; 손으로 만들지 말 것)
├── 자료/        ← 외부 소스(URL·유튜브) 캡처, 소스 1개 = 1페이지 (wiki action="ingest"가 생성;
│                  손으로 만들지 말 것 — 정규화 URL 멱등, frontmatter resource가 키)
├── 회의록/      ← 회의 녹음 분석, 녹음 1개 = 1페이지 (runtime/meeting/plaud_recordings.go가 생성;
│                  손으로 만들지 말 것)
└── 현장/        ← **폐지**. 현장 지도(#4517 제거)의 데이터 모델이었고, 지도가 사라진
                   뒤 프로젝트 대표페이지의 "## 현장" 표 + sites[] 로 접었다(#4745).
                   새로 만들지 말 것 — 현장 정보는 대표페이지에 쓴다.
                   손으로 정리한 표가 이미 있던 4개 프로젝트만 페이지가 남아 있다.

프로젝트/거래/      ← 거래처 단위 원장 (프로젝트 횡단이라 프로젝트 폴더 밖)
│                  거래처 없는 반복 경비(법인카드 주유비 등)는 `경비-<카테고리>.md`
│                  카테고리 원장으로 파일링 (결재 비용 캡처 — wiki_approval_deal.go)
프로젝트/메일분석/  ← 프로젝트 미연결 메일 분석 버킷
프로젝트/자료/      ← 프로젝트 미연결 자료 버킷
프로젝트/회의록/    ← 프로젝트 미연결 회의록 버킷
```

- **레거시**: 이관 전 대표페이지는 flat `프로젝트/<이름>.md`, 메일분석은
  `프로젝트/mail-analyses/`. 헬퍼들은 전환기 동안 두 형태를 모두 인식한다.
- **이관 도구**: `cmd/wiki-restructure` (dry-run 기본, `--plan` JSON으로 판단성
  병합/폴딩 지정, `--apply` 실행). **적용 전 게이트웨이 정지 필수** — Store 락은
  프로세스 내부용이고 FTS 인덱스는 메모리 상주(기동 시 재구축)다.

## 카테고리 계약 (프로젝트 외 5종 — 2026-08-25 확정)

카테고리는 코드에 고정된 6종이다 (`store.go`의 `Categories`). 프로젝트 레이아웃만
규약이 있고 나머지 다섯은 "무엇이 여기 들어가는가"가 어디에도 없어서, 실제로
시스템 문서가 기타로, 운영자 환경 사실이 기타로 흘러갔다 (2026-08-25 실측 6장).
경계는 **문서가 무엇에 관한 것인가**로 가른다 — 누가 썼는지도, 어떤 톤인지도 아니다.

| 카테고리 | 들어가는 것 | 들어가지 않는 것 |
|---|---|---|
| `프로젝트/` | 특정 프로젝트에 귀속되는 모든 것 (위 레이아웃) | 프로젝트 횡단 지식 → `업무/` |
| `인물/` | 사람 한 명 = 한 페이지. 신원키(`emails`)·소속·관계 | 그 사람이 보낸 메일 분석 → 프로젝트 슬롯 |
| `업무/` | 프로젝트 횡단 업무 지식: 거래처·제품·법규·시장·전략·회사 소개 | 특정 프로젝트 건 → 그 프로젝트 폴더 |
| `시스템/` | **Deneb 자신과 그 인프라**: 모델·라우팅·벤치, 연동 기기, 네트워크 장비, 에이전트 운영 규칙·지표 | 업무용 제품 지식 → `업무/` |
| `사용자/` | **운영자 개인에 관한 사실**: 선호·취향·일정·개인 환경(집/차 네트워크 등) | 일반 레퍼런스 → `기타/` |
| `기타/` | 위 어디에도 속하지 않는 참고 지식 (일반 용어, 시사, 개인 관심 주제) | 분류를 미루는 용도 — 나머지 다섯을 먼저 시도할 것 |

- `기타/`는 **분류 실패 버킷이 아니라 잔여 카테고리**다. 새 페이지를 쓸 때 기타를
  고르기 전에 다섯 중 하나가 맞는지 먼저 본다.
- 카테고리를 옮길 때는 **경로와 프런트매터 `category:` 를 함께** 바꾼다. 둘이
  어긋나면 검색 필터와 드리머가 서로 다른 답을 본다.
- 카테고리 집합 자체를 늘리지 않는다. 새 축이 필요해 보이면 그것은 대개 태그이거나
  프로젝트 슬롯이다.

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

## 단계(stage) 작성 규칙 (2026-07-19 확정)

프로젝트 대표페이지 머리말 `stage:` — 사업 생애 단계, 고정 어휘 하나.

- **어휘 고정**: `제안 → 견적 → 입찰 → 개발 → 계약협의 → 시공/납품 → 운영` (진행 순) + `종결`/`유실` (말단). `개발`=자체개발 인허가·부지(2026-07-20 추가), `시공`/`납품`은 병렬 트랙 — 현장 사업은 시공, **기자재(조달) 건의 계약 이행은 납품** (운영자: "기자재 전용을 나눠"). 어휘 밖 값은 파스/렌더에서 버려짐(`page.go:normalizeStage`).
- 현장 페이지의 `status:`(후보/계약/개설/준공 — 현장 지도용 per-site 생애)와 **별개 축**: stage는 딜의 단계, 프로젝트당 하나.
- **현장 문서 게이트**: 현장 상세 섹션·write-site 현장 페이지는 `개발` 또는 `계약협의` 이상부터 (기존 O&M 자산 예외 — 자체개발은 부지가 본체라 개발 단계부터). 영업 단계(제안·견적·입찰)는 `sites:` 메타데이터 한 줄까지만 — 운영자 확정 "최소 계약서 협의 수준은 되어야지".
- 기입 주체: 위키 도구 `stage` 파라미터·드리머(메일 신호로 진행 확인 시 갱신)·백필 오디터. 불확실하면 생략(추측 금지).

## 사업군(program) 작성 규칙 (2026-07-19 확정)

프로젝트 대표페이지 머리말 `program:` — 한 벤처의 워크스트림인 형제 프로젝트들을 묶는 축.

- 예: 비금 130.9MW = 케이블 조달(ZTT)·커넥터(SunKean)·EPC가 별개 폴더 → 셋 다 `program: 비금-130mw`.
- client(거래처) 아래의 **중간 우산**: client는 상대 회사로, program은 벤처로 묶는다. 값 문자열 자체가 그룹 키이므로 합류 시 기존 표기를 그대로 재사용(검색으로 확인). 단독 딜은 생략.
- 소비처: 회상·모아보기 그룹핑(향후), 수리 워크리스트의 형제 판정.

## 폴더명=코드 전환 원칙 (2026-07-19 운영자 확정)

- **방향**: 프로젝트 폴더명은 동결 코드(`프로젝트/nde-ztt-cbl-001/`)로, **사람 표면에는 한글 별칭**(= 대표.md title)을 보여준다 — 설명형 폴더명은 사업이 진화하면 낡고, 개명은 모든 참조를 깨기 때문.
- 별칭 정본 = 대표.md `title:` (2차 소스 금지). 표시 헬퍼 `Store.ProjectDisplayAlias`/`DisplayPath` (`display_alias.go`).
- **title은 프로젝트의 이름이다 — 이벤트·상태·날짜 금지** (2026-07-21 확정): "세창스틸 1,2공장 태양광 — 6/29 견적 재송부 (재검토)" 같은 상태 꼬리는 ①별칭이 즉시 낡고 ②상태 어휘("재송부")가 검색·골드 앵커를 오염시킨다 (실측: 무관 메일 13건이 세창스틸로 오라벨). 상태는 summary·로그.md의 몫. 정리 시 현장/지역 토큰은 보존한다 ("제이티에너지 청도 풍각면 화산리 턴키"에서 지역부를 잘라내지 말 것).
- **대표페이지는 회상 큐(cues:)를 채운다**: 실사용 어휘(메일 제목·회의가 이 프로젝트를 부르는 말) 중 페이지에 없는 토큰. 결정적 백필 도구 `scripts/audit/wiki_cue_backfill.py`(dry-run 기본, 타 프로젝트 정체성 토큰 자동 기각) — 2026-07-21 보급률 8/66에서 백필(2026-08-23 현재 19/73 — 상시 채움 주체는 없다).
- 이관은 파일럿(1건)→벤치 검증→전체 순. 이관 전 신규 프로젝트도 코드 폴더로 생성 가능(코드 민팅 규약은 code: 필드 문법과 동일).
- 검색·앵커는 제목·client·sites 키로 동작하므로 폴더명 코드화의 회상 영향은 제한적(2026-07-19 실측 설계).

## 공급 딜 파일링 규약 (2026-07-19 확정)

- **현장에 종속된 공급 딜**(특정 발전소용 케이블/모듈 발주)은 그 현장 프로젝트의 `기자재/` 슬롯에 파일링 — 별도 공급 폴더를 만들지 않는다.
- **현장 횡단 공급 원장**(남도에코 케이블 연간 거래 등)만 독립 폴더 또는 `프로젝트/거래/` 원장으로 유지 — 이때 client는 공급 상대가 아니라 **발주처**를 기입한다.
- 한 딜의 내용이 공급 폴더·거래 원장·현장 프로젝트 3곳에 분산되는 것을 금지 — 정본 1곳 + 나머지는 [[링크]].

## 거래처(client) 작성 규칙 (2026-07-07 운영자 확정)

프로젝트 대표페이지 머리말 `client:` — 프로젝트 위계의 **최상단 그룹핑 축** (거래처가 1단계, 프로젝트가 2단계).

- **형식 고정**: 계열사 단위 정식명 **1개** (스칼라) — `기아`, `현대차`, `LG전자`, `금호타이어`.
  그룹명(현대차그룹) 금지, ㈜ 등 법인 접미어 금지, 거래 원장(`프로젝트/거래/`) 표기와 일치 지향.
- 발주처/계약 상대가 확인되면 기입. 자체 개발 등 거래처 없는 사업은 **비워둠** (추측 금지).
- **소비처**: ① 안드로메다 프로젝트 홈·오늘 마감 레이더(`miniapp.project.digests`)가
  이 값으로 프로젝트를 묶고, ② 회상 프로젝트 앵커
  (`project_anchor.go:bestProjectKeyIn`)의 매칭 키 — "금호타이어 근황?"이 소속 프로젝트 전부를
  앵커한다. 정확히-하나 소비처(미팅 하베스트·메일 재분류)는 `UniqueProjectInText` 특이도 규칙을
  쓴다: 거래처만 언급하면 형제 프로젝트끼리 동률(잔류), 구체 제목은 자기 이름 키가 이겨서 파일링.
- 기입 주체: 위키 도구 `client` 파라미터 · 드리머(신규 기입, 기존 값 덮어쓰기 금지) ·
  리서치 태스크(빈 값 백필) · `wiki-restructure --plan`의 `set-client` op (일괄 백필, Updated 무변경).
- 세부 사업 단위(예: 기아 화성 국유지)는 3단계 폴더가 아니라 **프로젝트 내부 구조**(상세 페이지·섹션)로
  두고, 별도 계약으로 독립하면 그때 프로젝트로 승격한다 (케이스별 운영자 판단).

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
  도구(`tools/wikitool/wiki.go`)가 `NormalizeProjectPagePath`로 강제한다. 새 쓰기 경로를
  추가하면 같은 정규화를 통과시킬 것.
- **깊이 강제**: 같은 함수가 스키마 초과 깊이를 파일명으로 접는다 — LLM 제목의
  날짜 슬래시("6/25 회의")가 유령 중첩 폴더를 만들던 실사고(2026-07-02, 드리머)
  차단. 허용 형태: `프로젝트/<이름>/<파일>` 또는
  `프로젝트/<이름>/<기자재|메일분석|자료|회의록|현장>/<파일>`.
- **회상 앵커**: 질의가 활성 프로젝트를 이름으로 언급하면 recall이 그 대표페이지
  (현재 상태 포함)를 키워드 순위와 무관하게 최상위 증거로 주입한다
  (`chat/recall/recall_evidence.go` + `Store.MatchProjectsInText` — 결정적, 3자 미만
  이름 가드, 종결 프로젝트 제외).
- **미분류 메일 재분류**: `프로젝트/메일분석/`의 미연결 메일은 리뷰어가 매 사이클
  결정적 신호로 해당 프로젝트 메일분석 슬롯에 소급 파일링한다 (모호하면 잔류,
  사이클당 10건). 신호 정의와 가드는 아래 "메일 재분류 신호" 표.
- 사건·이벤트는 페이지 증식이 아니라 해당 프로젝트 `로그.md`에 append.
- **외부 소스 캡처는 `wiki(action="ingest", query=URL)`** (`tools/wikitool/wiki_ingest.go`): 웹/유튜브를
  lightweight 요약(fail-open — 실패 시 발췌로 캡처 보존)+바운드 발췌와 함께 자료 페이지로 영속화.
  같은 URL은 멱등(트래킹 파라미터 제거·유튜브 변형 정규화), 갱신은 `force=true`. `project=` 연결은
  대표페이지 존재를 검증(오타 유령 폴더 방지 — 없으면 전역 버킷)하고 로그에 `ingest` op 섹션을 남긴다.
- **출처 규율 (부유 주장 금지)**: 소스가 있는 주장은 그 [[페이지]]/ref를 병기하고, 소스 없는 합성
  추론은 `> 합성:` 인용으로 시작한다 — 드리머 프롬프트와 wiki write 도구 설명이 같은 규율을 강제.
- 페이지 이동은 `Store.MovePage` (인바운드 related 재지향 포함), 병합은
  `Store.MergePage`. 파일을 직접 mv/rm 하지 말 것.

## 현행 사실 plane (정정·삭제 가능한 기억)

변할 수 있는 한 줄 사실(선호, 이름, 금액, 기한, 계약 상태, 런타임 상태)은 일반
위키 본문을 덮어쓰는 방식으로 관리하지 않는다 (근거: [ADR-0004](../adr/0004-canonical-fact-plane.md)). 정본은 위키 루트의 append-only
`.fact-mutations.jsonl`이며, `(subject, fact_key)`별 현재값은 사실 종류·출처 권위·
기준 시각을 함께 해석해 결정한다.

- **쓰기 계약**: 에이전트가 근거를 확인한 사실/정정은 `knowledge(op="assert_fact")`,
  의도적 삭제는 `knowledge(op="forget_fact")`를 쓴다. `forget_fact`는 과거를 지우지 않고
  tombstone을 남겨 약한 추론이 삭제값을 되살리지 못하게 한다.
  **권위는 호출자가 고르는 값이 아니라 경로가 정한다** ([ADR-0005](../adr/0005-fact-write-authority.md)):
  모델 도구 스키마에는 `authority`·`basis_at` 필드 자체가 없고 어댑터가 `agent_confirmed`로
  고정한다(`assert_fact`는 `source_refs` 필수). `direct_user`는 인증된 네이티브 직접 발화
  induction만, `primary_document`/`runtime_observation`은 자기 출처를 인증하는 내부 ingestion만
  발급한다. 그래서 도구 호출은 사용자가 직접 말한 사실을 덮거나 지울 수 없고, 시도는
  `ignored_lower_authority`로 이력에만 남는다. `source_refs`는 서버가 열어보고 그 페이지가
  주장 값을 담고 있는지 도구 응답으로 **보고**하지만(`Store.VerifyFactSource`), 그것으로
  권위가 올라가지는 않는다 — 모델이 자기가 쓴 페이지를 인용할 수 있어 승격은 페이지
  provenance가 생긴 뒤로 막아뒀다. promptware로 오염된 턴에서는 두 mutation op이
  비가역 도구 게이트에 걸려 아예 실행되지 않는다.
- **저널 세그먼트**: 저널은 컴팩션하지 않는다(영구 이력). 활성 세그먼트가 임계치를
  넘으면 스냅샷을 durable하게 만든 뒤 `.fact-mutations.<revision>.jsonl` 아카이브로
  rename하고 새 세그먼트를 연다. 기동은 스냅샷 seed + 활성 세그먼트 replay이며,
  스냅샷을 못 읽을 때만 아카이브 전체로 복구한다. 저널(또는 최신 아카이브)이 스냅샷
  watermark에 못 미치면 fail-closed로 기동을 거부한다.
- **커밋 경계**: 저널 append+fsync가 먼저다. `.fact-state.json`,
  `사용자/현행-사실.md`, 워크스페이스 `MEMORY.md`/`USER.md`는 재생성 가능한 호환 뷰다.
  projection 오류가 나도 이미 커밋된 mutation을 재시도하지 말고
  `FactProjectionStatus`의 degraded 상태로 수리한다. 기존 수동 파일은 최초 전환 전에
  `.legacy` 백업으로 byte-preserving 보존한다.
- **회상 현행성**: 채팅 회상은 한 revision의 `RecallFactSnapshot`에서 current/stale/
  tombstone 규칙을 함께 읽는다. 현행 값은 최신 턴의 `<current-facts>`에 live로 붙이고,
  과거 위키·일기·대화·파일 증거의 정정 전 값은 최종 증거에서 제거한다. 타 주체 사실은
  질의나 직전 문맥으로 명시적으로 일치한 subject만 허용한다. 사실 mutation은 Tier1,
  prompt snapshot, recall cache를 같은 generation 경계에서 무효화한다. typed 폐기값은
  `(subject, fact_key)`가 해소된 증거에만 적용한다 — `A`, `80` 같은 짧은 값을 전역 문자열
  deny로 쓰면 무관한 페이지까지 지워진다. 주체·키가 없는 레거시 superseded-page 줄은
  충돌 가능성이 낮은 긴 문장만 전역 차단하고, 짧게 변하는 값은 반드시 사실 plane으로
  이관한다.
- **검색 plane**: 일반 검색은 현행 사실을 `@facts/<claim-id>.md` 읽기 전용 결과로 보여줄
  수 있다. 이 namespace와 `사용자/현행-사실.md`는 write/update/delete/move/merge/fold
  대상이 아니며, 클릭할 때 반드시 `ReadPage`로 현재 claim을 재검증한다. 캘린더·메일
  보강·인물 dossier·채팅 page evidence·ingest 멱등 검사처럼 실제 위키 파일이 필요한
  소비자는 `QueryOptions.ExcludeFactResults`를 검색 시점에 사용한다. 결과를 받은 뒤
  `FactID`만 버리면 fact가 top-K 슬롯을 이미 밀어냈으므로 금지한다.
- **평가 plane**: 기존 path/content 위키 골드는 page-only 결과만 평가한다. synthetic
  fact 결과를 같은 scorer에 섞으면 맞는 사실도 MISS가 되므로, fact lifecycle은
  A→B 정정 뒤 A=0/B=1, forget 뒤 A=B=0을 실제 knowledge→recall 경로에서 별도 측정한다.

자동 생성 파일의 marker나 `@facts/` 경로를 일반 위키 페이지로 가장하지 말 것. 사용자가
직접 관리하는 설명·근거·사건 서술은 기존 `knowledge(op="record")`/wiki 페이지에 남기고,
현재값으로 판정해야 하는 좁은 주장만 사실 plane에 기록한다.

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

## 운영자 위키 브리프 (워크스페이스 WIKI.md)

- **워크스페이스 `WIKI.md`** (USER.md·MEMORY.md 옆) = 운영자가 직접 편집하는 위키 유지보수 조향 파일 <!-- docref:ignore -->
  (OpenWiki의 wiki-brief 패턴 도입, 2026-07). 드리머 합성·위키 리서치·스카우트·노티
  다이제스트·슈퍼노트 다이제스트 프롬프트(소비자 5곳)가 **매 사이클 재읽기**로 주입한다 (`wiki/brief.go`: `LoadWikiBrief`+`WikiBriefSection` — 렌더러 단일).
- 용도: "무엇에 집중/무시할지"의 조향만. **레이아웃 불변식(카테고리·슬롯·추측 금지)은 브리프가
  못 뒤집는다** — 주입 섹션에 우선순위가 명시돼 있다. 파일 없음/빈 파일 = 조향 없음(내장 규칙만).
- 예산 2,000룬 초과분은 잘리고 마커가 남는다 — 브리프는 문단 수준으로 짧게.
- **챗 컨텍스트 파일이 아니다**: `contextFileNames`에 없으므로 세션 프롬프트·프롬프트 캐시 경로에
  절대 들어가지 않는다 (백그라운드 유지보수 프롬프트 전용). 컨텍스트 파일로 승격 금지.

## 위키 스카우트 (외부 능동 수집 — wiki-research의 쌍둥이)

- **wiki-scout 태스크** (`runtime/wikiwork/wiki_scout_task.go`, 12h): 대표페이지들의 **외부성
  미해결 질문**(2일 이상 경과, 사이클당 최대 3건, 시도 후 3일 쿨다운)과 WIKI.md 브리프 관심 주제를
  대상으로 웹 접근 포함 바운드 턴 1회. 질문도 브리프도 없으면 사이클 스킵. OpenWiki의 능동 커넥터
  수집 패턴 도입(2026-07). **즉시 트리거**: wiki-research 턴이 오늘 날짜 미해결 질문을 새로 쓰면
  (내부 소스로 못 닫았다는 뜻) 리서치 직후 `TriggerForPage`가 나이 게이트 없이 바로 1회 스카우트.
- **`PresetWikiScout`는 의도적으로 좁다** (`web`·`wiki`·`fetch_tools`뿐): 스카우트 컨텍스트에는
  신뢰 불가 웹 페이지가 실리므로 개인 메모리 표면(mail_archive·contacts·polaris·graphify)과 파일
  읽기가 같은 턴에서 닿으면 주입 시 유출 표면이 된다 — researcher 표면 재사용 금지.
- **쓰기 표면 3개 고정** (웹 텍스트가 정본 상태에 직접 스미지 않게): ① 근거 URL → `wiki ingest`
  자료 페이지(주입 방어·멱등 그대로 적용), ② 로그.md `질문해결` op append, ③ 대표페이지 미해결
  질문 **불릿 제거만**. 대표페이지 본문(현재 상태 등) 통합은 내부 wiki-research 몫 — 스카우트가
  본문을 만지면 안 된다. wiki-research가 웹을 뺀 이유(큐레이션 오염)는 이 분리로 해소된다.
- 게이트: 프로덕션 상태 디렉토리 전용, `DENEB_WIKI_SCOUT_DISABLE=1`로 비활성, 사용자 활동 중 스킵.
  상태는 `wiki-scout-state.json`(질문별 마지막 시도 시각, 60일 프룬). 위키 날짜 비교·표기는 <!-- docref:ignore -->
  `dentime.Now()`(KST 정본 시계) — 서버 로컬 시계 금지.
- **공유 유지보수 락**(`scout.MaintenanceLock()` → `research.SetMaintenanceLock`): scout와 research가
  같은 `*sync.Mutex`를 TryLock으로 잡아 **scout-vs-scout + scout-vs-research** 쓰기 경합을 막는다
  (전이 락이 아니면 두 턴이 같은 대표페이지를 stale read 후 full-body 재작성해 서로의 미해결 질문
  편집을 덮어씀). research는 자기 턴 앞뒤로 락을 잡고, post-research 트리거 **전에 반드시 해제**
  (scout가 같은 락을 재획득 — 안 그러면 데드락). 락을 못 잡은 사이클은 스킵(다음 회차 복귀).
- **도구 표면 좁힘**(`PresetWikiScout` = `web`·`wiki`·`fetch_tools`만): 신뢰 불가 웹 페이지가 컨텍스트에
  실리므로 개인 메모리 도구 배제. `wiki(action="ingest")` 페치는 **SSRF-safe 다이얼러**
  (`media.SSRFSafeDialer` — loopback/LAN/link-local 거부, 리다이렉트·DNS 리바인딩 포함)로만.
- **주입 방어 계층**: 요약 프롬프트 가드 + 저장 원문 발췌/fail-open 블록쿼트 펜싱 + `GateUntrustedTools`
  (exec류 차단, wiki 쓰기는 허용) — scout·noti-digest 둘 다 무장.

## 노티 다이제스트 (휴대폰 알림 → 기억)

- **레저** (`domain/phoneledger/ledger.go`): 판단 경로(휴대폰 이벤트 판단 턴은 의도적 ephemeral)와
  무관하게 notification/sms 이벤트를 redact+바운드 후 `<state>/phone-events/YYYY-MM-DD.jsonl`에
  append. tiny 게이트 **앞**에서 기록 — 게이트는 푸시 가치 기준이지 기억 가치 기준이 아니다
  (카톡 "발주 밀림"은 NO_REPLY가 정답이지만 프로젝트 로그감). 보존 30일 자동 프룬.
  **OTP·보안코드 제외**: 인증번호류 키워드+짧은 숫자코드가 함께 있으면 레저에 적재하지 않음
  (append가 tiny 게이트 앞이라 온디바이스 블록리스트를 통과한 OTP가 raw로 남는 걸 방지).
- **noti-digest 태스크** (`runtime/wikiwork/noti_digest_task.go`, 12h): 미소비 레저 테일(런 단위
  16K룬 예산, 라인 경계 오프셋 커밋)을 **wiki 전용 프리셋**(`PresetNotiDigest` = `wiki`·`fetch_tools`만
  — 웹 없음 + 개인 메모리 스토어 없음: 악성 알림이 사적 데이터를 읽어 wiki 쓰기로 영속화하는 경로
  차단)의 턴 1회로 위키에 소화. 알림 텍스트는 프롬프트 조립 전 **라인 펜싱**(delimiter 무력화·개행
  평탄화)으로 데이터 블록 탈출 방지. 쓰기 표면: 프로젝트 로그.md op append + 기존 인물 페이지
  update만 — 새 페이지·대표페이지 본문 수정 금지. 광고·OTP·잡담은 버림.
- 오프셋은 **성공한 턴 뒤에만** 전진(일시 장애로 내용 유실 금지), 연속 3회 실패 시 강제 전진(독배치가
  파이프라인을 못 박게). 게이트: 프로덕션 전용, `DENEB_NOTI_DIGEST_DISABLE=1`, 사용자 활동 중 스킵.

## 슈퍼노트 다이제스트 (손글씨 노트 → 기억)

- **수집 경로**: 슈퍼노트 만타(태블릿)가 손글씨 페이지를 온디바이스 필기인식으로 **검색가능 PDF**로
  내보내 **Google Drive 폴더에 자동동기화** → Deneb가 그 폴더를 폴링. 기기 필기인식이라 Deneb는 OCR
  불필요(PDF 텍스트 레이어를 뽑기만). 만타에 텍스트만 올라오면 `.txt`도 처리.
- **Drive 클라이언트** (`platform/googledrive/client.go`): 읽기 전용, `~/.deneb/credentials/drive_*.json`
  (gmail 패턴 미러). **Drive 스코프 크리덴셜은 운영자 1회 설정** — 게이트웨이는 토큰을 새로 발급하지
  않고 refresh만 하므로, Drive-scoped 동의로 받은 `drive_client.json`+`drive_token.json`을 놓아야 함. <!-- docref:ignore -->
- **supernote-digest 태스크** (`runtime/wikiwork/supernote_digest_task.go`, 6h): 설정 폴더의 신규
  파일(modifiedTime 커서 + 처리 ID 셋으로 dedup, 사이클당 최대 5건)을 다운로드→`document.ExtractText`로
  텍스트화→**내부 리서치 프리셋**(`PresetWikiResearch` — 웹 없음, 메모리 스토어 허용)의 턴 1회로 위키에
  소화. 노티와 달리 **사용자 본인의 1차 자료**(신뢰됨)라 untrusted 게이트 미무장 — 프로젝트 매칭에
  mail_archive/contacts/graphify 활용(드리머가 일지 소화하듯).
- 쓰기 표면: 회의 필기·진행 → 프로젝트 로그.md op append(회의/메모/결정/이슈), 할 일 → 로그·커밋먼트,
  인물 사실 → 기존 인물 페이지 update. **새 대표페이지 생성·대표 본문 재작성 금지.** 필기 오인식은
  확정 사실로 쓰지 않음. 커서는 성공 시에만 전진(읽을 수 없는 배치는 재다운로드 방지 위해 커서만 전진).
- 게이트: 프로덕션 전용, `DENEB_SUPERNOTE_DISABLE=1`로 비활성, `DENEB_SUPERNOTE_DRIVE_FOLDER_ID` 미설정
  또는 Drive 크리덴셜 부재면 **1회 로그 후 휴면**(안전 no-op), 사용자 활동 중 스킵. 상태는
  `supernote-digest-state.json`. <!-- docref:ignore -->

## 위치 → 현장 방문 기록 (프라이버시 경계 강함)

- **리코더** (`runtime/wikiwork/site_visit.go`): 휴대폰 `location_update`의 **온디바이스
  역지오코딩 place**(`전라북도 군산시 옥구읍 수산리` 류)를 프로젝트 `sites:`와 대조해 매칭될 때만
  `로그.md`에 `방문` op를 남긴다. 매칭 안 되면 **아무것도 안 씀** — Deneb는 전체 위치 궤적이 아니라
  이미 추적 중인 현장 방문 사실만 기억한다(원좌표·비매칭 장소는 어디에도 안 남음).
- **매칭은 sites 전용**(`Store.MatchProjectSite`): 프로젝트 *이름*이 아니라 `sites:`에만 매칭 —
  "군산" 이름 프로젝트가 군산 근처를 지날 때 오탐 방문하지 않게. 프로젝트별·날짜별 dedup(정지된
  폰이 몇 분마다 핑해도 하루 1회). 클라가 `place`를 안 보내면(구버전) 조용히 no-op.
- **Android**: `readCurrentLocation`이 온디바이스 `Geocoder`(API 키 불필요)로 `place` 필드를 붙인다.

## 회의 참석 기록 (미팅 하베스트 보강)

- 하베스트는 "○○ 어떻게 됐어요?" 질문만 던졌다 — 답을 안 하면 회의 사실이 증발. 이제 매칭된 회의가
  끝나면 **묻는 것과 무관하게**(ask cap·응답 여부 무관) 프로젝트 로그에 `회의 | <제목> — 참석` op를
  조용히 남긴다(`meeting_attendance.go`의 `RecordMeetingAttendanceByPath`, 이벤트 키로 dedup·영속).
- **프로젝트 매칭만**: 타깃이 거래처만 매칭되면(단일 프로젝트 없음) skip(질문은 여전히 발송). 답변으로
  들어오는 **결과**는 기존 흐름대로 그 위에 얹힌다.

## 중복 방어 3겹 (모두 `FindSimilarPages` 공유 — ID·코드·슬러그·FTS 제목 신호)

1. **쓰기 전 가드** — 위키 도구 write가 신규 생성 시 유사 문서를 찾으면 생성을
   거부하고 기존 경로를 안내 (`force=true`로만 강행). 드리머의 create-dedup도
   같은 프리미티브.
2. **위키 리뷰어** (`runtime/wikiwork/wiki_review_task.go`, 2h) — 최근 쓰인 문서의
   근사중복을 analysis 역할 단일 JSON 판정으로 사후 검수. **기본은 관찰 모드**
   (판정만 state 파일 `observed`에 기록) — 판정 품질 확인 후
   `DENEB_WIKI_REVIEW_AUTOMERGE=1`로 자동 병합을 무장한다 (사이클당 3건, git
   스냅샷 선행). 같은 프로젝트 폴더의 대표/로그/상세와 로그 슬롯은 후보 제외.
   로그 회전(`RotateProjectLog`: 로그.md 최신 20섹션 유지, 초과분 → 로그-보관.md
   archived)과 죽은 링크 정리(`PruneDeadRelatedLinks`: related의 죽은 참조를
   결정적으로 복구{정확경로→레거시flat→유일 basename→유일 제목→유일 ID} 또는
   제거, 모호하면 추측 없이 제거, Updated 미갱신 · `PruneDeadWikiLinks`: 본문
   `[[경로]]` 링크도 같은 사다리로 재지정, 평문형 `[[이름]]`과 미해결 링크는
   보존하고 카운트만)도 이 태스크가 수행. 미연결 메일 재분류(신호 1~3)도 여기서
   돈다 — 아래 "메일 재분류 신호" 참조.
3. **드림 verify** (`verify.go` Phase 5) — 아래 5a~5f 표. 사이클당 fix 15건 상한,
   그중 **이동은 3건**(`maxAutoMovesPerCycle`)까지.

### 드림 verify 단계 (5a~5f)

| 단계 | 무엇 | 자동 적용 | 가드 |
|---|---|---|---|
| 5a 중복 | 정규화 제목 일치 · 동일 ID | 병합(`FoldDuplicate`) | **동일 ID는 제목까지 같을 때만** 자동(그 외 advisory `동일한 ID(제목 상이)`) · 형제 딜(`siblingDeals`) 제외 · CJK는 정규화-동일만 · `FoldDuplicate`가 마지막 관문: 메일 Message-ID 상이·같은 프로젝트 폴더의 두 in-folder 페이지·서로 다른 **코드** 폴더의 대표 쌍은 거부 |
| 5b 오분류 | 인덱스 전체를 LLM에 주고 카테고리 오류 판정 | confidence=high면 이동 | **레이아웃 소유 경로는 이동 금지**(`IsLayoutManagedPath`: 프로젝트/ 하위·거래 원장·메일분석·자료) · `type: deal` 금지 · 스냅샷에 없는 경로 금지 · 서브폴더 민팅 금지(타깃은 `<카테고리>/<파일>.md`) · 목적지 프로젝트 금지 · 이동 3건/사이클 · 사유(`reason`) 로깅 |
| 5c 마감 | `due` 경과 | advisory | — |
| 5c-2 제목 규칙 | 대표 title에 상태·날짜 꼬리 | advisory(`title_rule`) | 제목 편집은 랭킹 입력이라 사람 판단 |
| 5d superseded | 30일 방치 superseded | 아카이브 | 대표/로그 슬롯·원장·메일분석은 **스킵**(잘못 찍힌 플래그는 수리 대상) |
| 5e 메일 보존 | 90일 지난 메일분석 | 아카이브 | 데이터 나이상 첫 발화는 ~2026-09 |
| 5e-2 인물 스텁 | 마커 스텁 30일+무회상 | 아카이브 | 큐레이션된 인물 페이지는 제외 |
| 5f 미회상 콜드 | 오래된 저중요·무회상 | advisory | 사용자·시스템·인물 카테고리 제외 |

### 정체성·supersede 불변식 (쓰기 경로)

- **`id`는 fill-only**: `mergeDreamUpdate`·wikitool write 모두 기존 id를 덮어쓰지
  않는다. id는 `similar.go` 리타겟 1순위·verify 동일-ID 병합·`link_prune` 복구·
  그래프 시드·graphify 노드 id가 해석하는 **정체성 키**이지 업데이트별 슬러그가
  아니다.
- **리타겟된 update는 본문만 붙인다**: 제안 경로가 유사 페이지로 바뀌면
  (`retargetedFrom`) summary/type/confidence/resource는 대상 페이지 것을 유지.
  리타겟 자체도 **슬롯 종류 일치**(대표↔대표·로그↔로그)와 `title` 사유일 때
  **제목 토큰 겹침**을 요구한다 — 아니면 제안 경로에 그대로 쓴다.
- **대표 전용 필드**(client·sites·kinds·stage·program)는 `IsProjectRepPage`인
  경로에만 기록한다.
- **`MarkSuperseded` 전제조건**: old가 대표/로그 슬롯·메일분석·거래 원장이면
  거부한다. supersede는 "같은 주제의 낡은 값 대체" 전용이고, 플래그가 붙으면
  검색 ×0.15·거래처 앵커 제외·30일 뒤 아카이브로 이어져 지식이 회상에서 사라진다.
- **빈 본문 생성 금지**: update-on-missing에서 content가 비면 페이지를 만들지 않는다.
- **메타-전용 수리는 `updated`를 보존**한다(`Store.UpdatePageMetaOnly`; 본문을
  바꾸면 에러). `updated`는 신선도·마감·졸음 감지의 입력이라 캡 스윕 같은
  메타 정리가 날짜를 밀면 신호가 죽는다.

### 메일 재분류 신호 (미연결 `프로젝트/메일분석/`)

| 신호 | 근거 | 가드 |
|---|---|---|
| 1 related | related가 가리키는 **대표페이지** | 메일↔메일 엣지는 소유 증거 아님 · 두 프로젝트면 모호(잔류)이고 도메인 신호로 내려가지 않음 |
| 2 title | 제목이 정확히 한 프로젝트를 지목(`uniqueProjectIn`) | **자사명(탑솔라)** client 키는 후보에서 제외 |
| 3 sender domain | 이미 파일링된 같은 도메인 메일 ≥3, 만장일치 | `DENEB_MAIL_RECLASS_DOMAIN=1`로만 무장(기본 관찰) · 이동 3건/사이클 |

공통: archived/superseded 페이지는 후보 제외, 사이클당 총 10건, 모호하면 잔류.

### 거래 원장 위치 (단일 정본)

`type: deal` 페이지는 **`프로젝트/거래/<slug>.md` 한 곳**에만 산다 —
`UpsertDealPage`가 쓰는 경로이자 `knownCounterparties`가 읽는 유일한 폴더라,
다른 카테고리로 옮기면 회상 거래처 앵커에서 빠지고 다음 결재 캡처가 같은
거래처를 원위치에 다시 만든다. 프로젝트 폴더 안의 원장(현장 횡단 공급 원장 등)은
폴더가 정본이라 예외. 검사는 `scripts/audit/wiki_deal_ledger_lint.py`.
