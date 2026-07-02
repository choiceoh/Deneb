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

- 프로젝트 밑 **flat `.md` 신규 생성 금지** — 드리머(`dreamer_apply.go`)와 위키
  도구(`tools/wiki.go`)가 `NormalizeProjectPagePath`로 강제한다. 새 쓰기 경로를
  추가하면 같은 정규화를 통과시킬 것.
- 사건·이벤트는 페이지 증식이 아니라 해당 프로젝트 `로그.md`에 append.
- 페이지 이동은 `Store.MovePage` (인바운드 related 재지향 포함), 병합은
  `Store.MergePage`. 파일을 직접 mv/rm 하지 말 것.

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
   archived)도 이 태스크가 수행.
3. **드림 verify** (`verify.go` Phase 5) — 정규화 제목 일치 자동 병합, 유사 제목
   advisory, 30일 방치 superseded 자동 아카이브, **90일 지난 메일분석 자동
   아카이브**(보존 정책). 사이클당 fix 15건 상한.
