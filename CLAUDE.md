# Deneb

**비서실장형 단일 AI 에이전트** (NVIDIA DGX Spark 플릿 · 단일 사용자 · Korean-first — 게이트웨이·배포·웜홀=srv4, GPU 보조=srv1·srv2). 한 페르소나가 업무분석(깊이)과 업무비서(능동)를 동시에 수행한다 — 분석/비서 *페르소나* 분리 금지 (기기별 반응형 레이아웃은 허용). 좁고 깊게: 완성도·응집도 우선, 옵션보다 opinionated 기본값.

- `gateway-go/` — Go 게이트웨이 (HTTP+SSE, RPC 150+, 챗/LLM 파이프라인, 도구 통합). **주 런타임.**
- `client-android/` — Kotlin Multiplatform **모바일** 클라 (Android 데일리드라이버 + iOS). Compose Desktop 타깃은 헤드리스 검증용으로만 잔존.
- `andromeda/` — **데스크톱** 워크스테이션 (Tauri 2 + React 18 + Refine + Vite). 자체 게이트/가이드 보유.
- `skills/` — 파일시스템 발견 스킬 플러그인. `docs/` — Mintlify 문서.
- 두 클라 모두 `miniapp.*` RPC (`X-Deneb-Client-Token`) 사용. wire 타입은 Go `//deneb:wire` 구조체에서 Kotlin+TS **양쪽** 생성.
- 모듈 상세는 각 모듈의 `CLAUDE.md` (gateway-go/, client-android/app/, andromeda/, skills/).

## 안전 (항상 적용)

- 레포: https://github.com/choiceoh/Deneb. 챗 응답의 파일 참조는 레포 상대 경로만 (절대경로·`~/...` 금지).
- `~/deneb/` = **프로덕션 전용** (main만 — srv4 의 auto-deploy 타이머가 pull·빌드·핫스왑) — 에이전트는 거기서 브랜치/워크트리/수동 빌드 금지. 개발은 `~/deneb-dev/`.
- 멀티에이전트 안전: `git stash`·워크트리 조작·브랜치 전환은 **명시 요청 시에만**. "push" = rebase 통합 허용, "commit" = 내 변경만. 낯선 파일은 무시하고 내 변경만 커밋.
- **생성 코드(`DO NOT EDIT` 헤더) 직접 수정 금지** — 소스 오브 트루스 수정 후 make 타깃으로 재생성 ([generated-code](docs/agent-rules/generated-code.md)). `//deneb:wire` 변경 = `make kotlin-models` **와** `pnpm gen:wire` 둘 다.
- 보안 CODEOWNERS 경로(`.github/dependabot.yml`, `codeql.yml`, `gateway-go/internal/infra/{clientauth,secret,config}/`)는 소유자가 명시 요청할 때만 수정.
- 실 전화번호·크리덴셜·라이브 설정값 커밋 금지. 버전 번호 변경·release/publish는 운영자 명시 승인. baseline/snapshot/expected-failure 파일로 실패를 침묵시키지 말 것. 의존성 패치는 명시 승인 필요.
- 질문에는 코드로 검증한 고신뢰 답변만. 이슈/PR 작업 시 마지막에 전체 URL 출력.

## 게이트 (푸시 전)

- **스코프가 명확한 diff(한 레인만 변경)는 `make ci/fast`(경로 게이트) 통과로 푸시 가능** — CI가 어차피 전체를 재검증한다. 레인 경계가 애매하거나 공유 표면(Makefile·CI 워크플로·생성기·wire)을 건드리면 전체 `make ci`.
- client-android 변경 시 Kotlin 레인 필수 (`make ci ARGS=--kotlin`). andromeda는 **별도 레인**: `cd andromeda && pnpm verify`. Go만은 `make check`.
- 게이트웨이 동작 변경은 라이브 검증까지: `scripts/dev/live-test.sh restart && smoke` (+ 관련 `quality`) — 로그에 에러/경고 없음까지가 완료 ([live-testing](docs/agent-rules/live-testing.md)).
- 실패한 빌드/테스트 상태로 커밋·푸시 금지.

## Git / PR

- **Conventional Commit 필수**: `feat(chat): …` (type: feat|fix|perf|refactor|docs|test|chore|ci|build · scope: 모듈명). 커밋은 `scripts/committer "<msg>" <files…>` 로 (스테이징 스코프 유지).
- PR 본문: Summary / Changes / Verification 3섹션 + 푸터 `🤖 Generated with [Claude Code](https://claude.com/claude-code)` — 상세 [git-pr](docs/agent-rules/git-pr.md).
- 머지는 **`scripts/dev/pr.sh land <pr>`** — 체크 그린 감시→스쿼시→랜딩 검증(`merge-base --is-ancestor`, MERGED ≠ 랜딩)→브랜치 정리를 한 번에. main에 머지커밋 푸시 금지 — 리베이스.
- 네이티브 사용자 표시 패치노트는 `client-android/app/changelog.d/` 조각 파일로 (사용자 영향 변경만). 루트 CHANGELOG.md는 release-please 자동 생성 — 직접 편집 금지.

## 스타일

- Korean-first (UI 텍스트·사용자 응답·PR 본문). 코드·주석·문서는 미국 영어. 제품명 **Deneb** / CLI·경로 `deneb`.
- Go: gofumpt + vet (`make fmt`). Kotlin: spotless + detekt. TS: eslint + prettier. 파일 ~700 LOC 이하 권장.
- vibe coding — 다음 AI 세션을 위해 까다로운 로직에만 짧은 주석·충분한 컨텍스트. 단순 순차 처리 > 동시성.

## 에이전트 운용 (토큰·속도)

- 독립적인 도구 호출은 한 응답에 **병렬로**. 긴 명령(CI watch·빌드·배포)은 **background 실행 + 완료 통지** — 반복 폴링 금지.
- **시끄러운 출력을 컨텍스트에 담지 말 것**: 진행바 명령은 `2>&1 | tail -N`, 대량 리포트는 `--format json` + 필터, 로그는 grep. 파일은 필요한 범위만 Read (offset/limit).
- 넓은 탐색은 저비용 탐색 에이전트로. 환경 특이사항(머신별 함정)은 메모리에 저장해 재진단을 없앤다.
- 세션 시작 시 관례적 빌드/테스트 금지 — 작업에 필요할 때만 (`make go` · `make test` · `make go-dev` · `./scripts/check-dev-env.sh`).

## 룰 인덱스 — 필요할 때 Read (조건부 로딩)

> 상세 규칙은 `docs/agent-rules/`에 있고 **자동 주입되지 않는다**. 대상 경로를 처음 Edit/Write 하면 훅(`scripts/dev/claude-rules-gate.py`)이 1회 차단하며 해당 룰을 안내한다 — 안내된 룰 중 작업과 관련된 것을 Read 후 재시도하라. 룰 추가/수정 규약은 [docs/agent-rules/README.md](docs/agent-rules/README.md).

| 파일 (docs/agent-rules/) | 언제 읽나 |
|---|---|
| architecture.md | 구조/모듈맵 오리엔테이션이 필요할 때 |
| go-gateway.md · live-testing.md · concurrency.md · logging.md | gateway-go 코드를 만질 때 |
| prompt-cache.md | 챗 prompt/캐시/컴팩션 경로 (불가침 원칙 — 위반 금지) |
| hub-wiring.md | method_registry·GatewayHub 배선 |
| model-roles.md · sidecar-models.md | LLM 역할 배치 · 로컬 모델/wormhole 운영 |
| wiki-layout.md | 위키 도메인 (project_layout.go 규약) |
| native-design-system.md · native-live-app.md | client-android UI · 실앱 라이브 검증 |
| denebui.md | deneb-ui 카드 (라벨 HTML) — 3구현 동기·계약 위치·검증 사슬 |
| generated-code.md | 생성 파일 재생성 방법 |
| release-and-deploy.md | 배포 · APK 발행/서명 · OTA |
| git-pr.md · testing.md · docs.md · build-status.md · collaboration.md · optimization.md | 각 주제 상세 |
