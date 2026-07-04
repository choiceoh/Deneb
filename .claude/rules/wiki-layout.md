---
description: 위키 프로젝트 문서 레이아웃 규약 (프로젝트당 폴더 + 고정 슬롯)
globs:
  - "gateway-go/internal/domain/wiki/**"
  - "gateway-go/internal/runtime/server/wiki_*.go"
  - "gateway-go/internal/pipeline/chat/tools/wiki.go"
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
├── 기자재/      ← 케이블·모듈 등 자재 문서
└── 메일분석/    ← 메일 1통 = 1페이지 (시스템 자동 생성; 손으로 만들지 말 것)

프로젝트/거래/      ← 거래처 단위 원장 (프로젝트 횡단이라 프로젝트 폴더 밖)
프로젝트/메일분석/  ← 프로젝트 미연결 메일 분석 버킷
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
| 대표/로그/메일분석 경로 생성 | `RepPagePath` / `LogPagePath` / `MailAnalysisPagePath` |
| 원시 데이터(메일·거래)인가 | `IsProjectRawDataPath(path)` |
| flat 프로젝트 경로 정규화 | `NormalizeProjectPagePath(path)` (쓰기 경로에서 호출) |
| 프로젝트 열거 | `Store.KnownProjects()` |

## 불변식

- 프로젝트 밑 **flat `.md` 신규 생성 금지** — 드리머(`dreamer_apply.go`)·위키
  도구(`tools/wiki.go`)·knowledge 어댑터(`knowledge/adapter_wiki.go`)·wiki RPC
  write(`handler/wiki/wiki.go`)·미니앱 create(`handlerminiapp/memory_write.go`)가
  `NormalizeProjectPagePath`로 강제한다 (**신규 생성만** — 기존 레거시 flat
  페이지의 업데이트는 제자리 유지). 새 쓰기 경로를 추가하면 같은 정규화를
  통과시킬 것.
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
  보존기한으로 **archived 된 메일은 재분류 대상에서 제외**되고, 재분류는 메타데이터
  수리라 **Updated 를 재스탬프하지 않는다** (90일 보존 시계·휴면 시계 왜곡 방지 —
  아카이브 처리(`archivePage`)도 같은 원칙으로 Updated 를 보존한다).
- **메일 1통 = 1페이지 (전역)**: 메일분석 쓰기는 msgID 로 전 버킷을 조회해
  (`Store.FindMailAnalysisPage`) 기존 페이지가 있으면 그 자리에 갱신한다 — 분석
  캐시가 우회/유실돼 관련 프로젝트가 다르게 풀려도 두 번째 페이지를 만들지 않는다.
  드림 verify 의 중복 병합도 메일분석·원시데이터·로그 슬롯·archived 페이지를
  후보에서 제외하므로 (리뷰어와 동일 제외), 같은 제목의 스레드 메일("RE: 견적")이
  자동 병합으로 삭제되는 일이 없다.
- 사건·이벤트는 페이지 증식이 아니라 해당 프로젝트 `로그.md`에 append.
- 페이지 이동은 `Store.MovePage` (인바운드 related 재지향 포함), 병합은
  `Store.MergePage`. 파일을 직접 mv/rm 하지 말 것.

## 프로젝트 생애주기 (종결/재개)

- **종결** = `Store.CloseProject` (도구: `wiki(action="close", query=이름, content=결과)`).
  대표페이지 `## 종결` 섹션에 날짜·결과 기록 + **폴더 전체 Archived 플래그**.
  파일 이동·삭제 없음(경로 안정성). `knownProjects`가 archived 대표를 건너뛰므로
  **한 플래그로 활성 무대 전체**(메일 연결 후보·모아보기·리서치·리뷰어 코드신호)에서
  동시 퇴장. 검색은 뒷순위로만 밀림. stale-deadline 감지도 archived를 건너뜀.
- **재개** = `Store.ReopenProject` (`action="reopen"`) — 폴더 전체 복원, 종결 이력은
  섹션에 남음. ★**재개 식별자는 폴더명 또는 대표페이지 경로**: 표시 제목 폴백은
  `knownProjects`(활성 전용) 기반이라 **종결된 프로젝트는 표시 제목으로 못 찾는다**
  (예: 폴더 `기아-화성`, 제목 "기아 화성"이면 reopen 은 `기아-화성`으로).
- **졸음 감지** = `Store.FlagDormantProjects` (wiki-review 태스크가 사이클당 최대 2건):
  120일(`DormantAfterDays`) 무활동 활성 프로젝트의 대표페이지 현재 상태에 "종결 검토
  제안" 불릿 1개 (분기당 1회 멱등, ref=`dormant:YYYYQn`). **자동 종결은 절대 없음.**
- 주의: 종결된 프로젝트로 오는 새 메일은 자동 연결되지 않는다(미분류 버킷행) —
  뒤늦은 정산 메일이 예상되면 종결을 늦추거나 재개로 되살린다.
- **활성 거래처 판정은 archived 를 안 본다(의도)**: `ActiveCounterpartyDomains`는
  경로 형태(프로젝트 연결 메일분석) + Created 컷오프만 쓰는 인덱스 스캔이라, 종결
  직후에도 최근 60일 내 메일 도메인은 컷오프가 지날 때까지 부스트가 유지된다 —
  방금 끝난 거래처의 정산·후속 메일이 곧바로 강등되지 않는 것이 글랜스에 맞다.

## 중복 방어 3겹 (모두 `FindSimilarPages` 공유 — ID·코드·슬러그·FTS 제목 신호)

1. **쓰기 전 가드** — 위키 도구 write가 신규 생성 시 유사 문서를 찾으면 생성을
   거부하고 기존 경로를 안내 (`force=true`로만 강행). 드리머의 create-dedup도
   같은 프리미티브.
2. **위키 리뷰어** (`runtime/server/wiki_review_task.go`, 2h) — 최근 쓰인 문서의
   근사중복을 analysis 역할 단일 JSON 판정으로 사후 검수. **기본은 관찰 모드**
   (판정만 state 파일 `observed`에 기록) — 판정 품질 확인 후
   `DENEB_WIKI_REVIEW_AUTOMERGE=1`로 자동 병합을 무장한다 (사이클당 3건, git
   스냅샷 선행). ★관찰 모드가 멈추는 것은 **LLM 판정 병합뿐** — 결정적 유지보수
   (로그 회전·죽은 링크 정리·미분류 메일 재분류·휴면 플래그·flat 레이아웃 수리)는
   관찰 모드와 무관하게 **매 사이클 항상 실행**된다. 같은 프로젝트 폴더의
   대표/로그/상세와 로그 슬롯, **archived 페이지**는 후보 제외.
   로그 회전(`RotateProjectLog`: 로그.md 최신 20섹션 유지, 초과분 → 로그-보관.md
   archived)과 죽은 링크 정리(`PruneDeadRelatedLinks`: related의 죽은 참조를
   결정적으로 복구{정확경로→레거시flat→유일 basename→유일 제목→유일 ID} 또는
   제거, 모호하면 추측 없이 제거, Updated 미갱신 — ★슬롯 파일명(대표.md/로그.md/
   로그-보관.md)은 basename 신원에서 제외: 프로젝트가 하나뿐일 때 죽은 슬롯 참조가
   무관한 프로젝트 슬롯으로 재지향되던 것 차단)도 이 태스크가 수행.
3. **드림 verify** (`verify.go` Phase 5) — 정규화 제목 일치 자동 병합, 유사 제목
   advisory, 30일 방치 superseded 자동 아카이브, **90일 지난 메일분석 자동
   아카이브**(보존 정책, Updated 보존). 사이클당 fix 15건 상한, fix 적용 직전
   git 스냅샷 선행(첫 사이클도 pre-state 확보). ★중복 후보에서 **메일분석·
   원시데이터·로그 슬롯·archived 제외**(메일 1통=1페이지 재보장), ★**서로 다른
   프로젝트 폴더의 대표페이지 쌍은 advisory 로 강등**(자동 병합 없음 — 하위 문서
   재부모화가 필요해 auto-fix 범위 밖; `FoldDuplicate` 자체도 이 쌍을 거부한다.
   운영자 병합은 미니앱 `MergePage` 경로 사용). keeper 선택은 archived 가 항상
   패배(살아있는 페이지가 보관 페이지 속으로 접히지 않게).
