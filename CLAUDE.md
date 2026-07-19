# Deneb

**비서실장형 단일 AI 에이전트** (NVIDIA DGX Spark 플릿 · 단일 사용자 · Korean-first — 게이트웨이·배포·웜홀=srv4, GPU 보조=srv1·srv2). 한 페르소나가 업무분석(깊이)과 업무비서(능동)를 동시에 수행한다 — 분석/비서 *페르소나* 분리 금지 (기기별 반응형 레이아웃은 허용). 좁고 깊게: 완성도·응집도 우선, 옵션보다 opinionated 기본값.

**프로젝트 주 목표(북극성, 2026-07): 에이전트의 재귀적 자가개선** — 개선 절차 자체를 진화 대상으로. 우선순위 판단·설계 결정은 [RSI 로드맵](docs/research/recursive-self-improvement-roadmap.md)(P1 절차 외부화 → P2 slow loop → P3 verifier 공진화 → P4 스킬+도구 번들 → P5 복리화: 수요 생성·케이던스·선제 L4 공급·재귀 표면·효용 접지)을 기준으로 한다.

- `gateway-go/` — Go 게이트웨이 (HTTP+SSE, RPC 200+, 챗/LLM 파이프라인, ~50 내장 도구). **주 런타임.**
- `client-android/` — Kotlin Multiplatform **모바일** 클라 (Android 데일리드라이버 + iOS). Compose Desktop 타깃은 헤드리스 검증용으로만 잔존.
- `andromeda/` — **데스크톱** 워크스테이션 (Tauri 2 + React 19 + Refine + Vite). 자체 게이트/가이드 보유.
- `skills/` — 파일시스템 발견 스킬 플러그인. `docs/` — Mintlify 문서.
- 두 클라 모두 `miniapp.*` RPC (`X-Deneb-Client-Token`) 사용. wire 타입은 Go `//deneb:wire` 구조체에서 Kotlin+TS **양쪽** 생성.
- 모듈 상세는 각 모듈의 `CLAUDE.md` (gateway-go/, client-android/app/, andromeda/, skills/).

## 안전 (항상 적용)

- 레포: https://github.com/choiceoh/Deneb. 챗 응답의 파일 참조는 레포 상대 경로만 (절대경로·`~/...` 금지).
- `~/deneb/` = **프로덕션 전용** (main만 — srv4 의 auto-deploy 타이머가 pull·빌드·핫스왑) — 에이전트는 거기서 브랜치/워크트리/수동 빌드 금지. 개발은 `~/deneb-dev/`.
- 멀티에이전트 안전: `git stash`·워크트리 조작·브랜치 전환은 **명시 요청 시에만**. "push" = rebase 통합 허용, "commit" = 내 변경만. 낯선 파일은 무시하고 내 변경만 커밋.
- **ZCode 자동 워크트리 격리**: ZCode 세션은 SessionStart 훅(`scripts/dev/zcode-worktree-init.sh`)이 `~/.zcode/worktrees/Deneb/<session-id>` 워크트리(`zcode/<session-id>` 브랜치)를 자동 생성. **세션 시작 직후 반드시 `cd ~/.zcode/worktrees/Deneb/<session-id>`로 진입** — SessionStart 안내 메시지의 경로를 따를 것. 메인 체크아웃 편집은 PreToolUse 가드(`zcode-worktree-guard.sh`)가 차단. Stop 훅은 상태만 보고(PR/push 없음). Codex(`~/.codex/worktrees/`)/Trae(`~/.trae/worktrees/`)/Cursor(`~/.cursor/worktrees/Deneb/`, `cursor/<session-id>`)와 경로·브랜치 분리로 충돌 회피. 전체 가이드: [docs/tools/zcode-environment.md](docs/tools/zcode-environment.md).
- **Cursor 자동 워크트리 + CodeGraph**: `.cursor/hooks.json` SessionStart가 `scripts/dev/cursor-worktree-init.sh`로 `~/.cursor/worktrees/Deneb/<session-id>`를 만들고 `active-root`를 기록. `.cursor/mcp.json`은 `cursor-codegraph-serve.sh`로 **active-root 워크트리**에 CodeGraph를 바인딩. 메인 체크아웃 편집은 `cursor-worktree-guard.sh`(`failClosed`)가 차단. 세션 시작 직후 `move_agent_to_root` → 워크트리, 이어서 `git checkout cursor/<session-id>`로 브랜치 복구(MCP 재바인딩 + main 덮어쓰기 취소).
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

### 코드 내비게이션: CodeGraph (소스 코드 그래프)

이 레포에는 **CodeGraph** — 소스 코드의 심볼 그래프(함수·클래스·호출·임포트·상속)를 인덱싱하는 코드 인텔리전스 — 가 **MCP 서버로 배선**돼 있다(`.mcp.json`). 에이전트는 `codegraph_explore` 툴을 **네이티브로** 받는다. **관계·구조 질문(누가 부르나·바꾸면 뭐 깨지나·이 흐름이 어떻게 엮이나·이 심볼 뭐하나)은 grep으로 이름 검색 후 여러 파일 Read 하지 말고 CodeGraph 한 번으로 해결한다.** 메서드 리시버는 물론 **동적 디스패치(인터페이스→구현체·콜백·이벤트)까지 추적** — grep이 못 따라가는 것.

**언제 쓰나 (구조·관계·흐름 질문의 기본값 = CodeGraph. grep은 리터럴 텍스트 — 문자열·주석·로그·설정값 — 에만):**

| 상황 (이러면 grep/Read 전에) | 쓰는 것 |
|---|---|
| 낯선 코드/서브시스템 이해 착수 · "어디서 X 하나 / 어떻게 엮이나" | `codegraph_explore` 영역 조사 (다중 토큰·짧은 설명, `maxFiles` 3–6) |
| **알려진 심볼 하나** (정의·멤버·트레일) | `codegraph node SYMBOL` / MCP `codegraph_node` — **정확한 심볼엔 explore보다 우선** |
| **소스 심볼 편집 착수 전** — 바꾸면 뭐 깨지나 | `codegraph impact SYMBOL` (블래스트 반경) |
| "이 함수/타입 누가 쓰나" (리팩터·시그니처 변경) | `codegraph callers SYMBOL` |
| 이름 조각으로 정의 찾기 | `codegraph query NAME` (`--kind`로 좁히기) |

원칙: **모르는 영역은 explore, 이미 이름이 있는 심볼은 node/callers/impact**. explore에 단일 PascalCase만 넣으면 camelCase 분해(`GatewayHub`→`Hub`/`GatewayTab`)로 노이즈가 섞일 수 있다 — 그 경우 node로 핀. (런타임 에이전트의 `codegraph_explore`는 단일 특정 심볼이면 게이트웨이 브리지가 **자동으로 node 리라우트**해 이 노이즈를 없앤다; dev CLI/MCP는 리라우트 없으니 수동으로 node를 골라라.) **크로스모듈 노이즈**(게이트웨이 질문에 client-android Kotlin·andromeda TS가 섞임)는 explore가 경로 필터가 없어 랭킹으로만 좁혀지니 — 쿼리에 모듈/파일 토큰(`gateway-go`, 파일명)을 더해 좁혀라. (런타임 에이전트의 codegraph explore/node 결과엔 검색된 파일이 속한 폴더의 **CLAUDE.md 서브트리 맵이 자동 첨부**된다 — 구조(codegraph)+폴더 의도(맵)를 한 번에. dev 하네스는 파일 접근 시 서브트리 맵을 이미 주입하므로 별개.) 심볼 이름을 grep하려는 순간이면 CodeGraph(훅이 유도).

- **MCP 툴** (`CODEGRAPH_MCP_TOOLS=explore,node,search,impact,callers,callees`): 영역 조사=`explore`, 심볼 핀=`node`/`search`, 변경 영향=`impact`/`callers`.
- **CLI**(같은 그래프, 셸에서 쓸 때):

```bash
codegraph node     SYMBOL    # 정의+멤버+트레일 (심볼 핀 — 기본 선택)
codegraph callers  SYMBOL    # 호출자 (동적 디스패치 포함)
codegraph callees  SYMBOL    # 이 심볼이 부르는 것
codegraph impact   SYMBOL    # 변경 블래스트 반경(영향 심볼)
codegraph query    NAME      # 이름 검색 (정확; explore보다 덜 퍼짐)
codegraph explore  "영역..."  # 지형 one-shot (다중 토큰; --max-files 낮게)
```

- **인덱스는 로컬 `.codegraph/`**(SQLite, gitignore됨)에 저장, **PostToolUse 훅(`zcode-codegraph-sync.sh`)이 편집 후 백그라운드에서 `codegraph sync` 실행**(<0.5s) — 수동 명령 불필요, 항상 신선. Claude·ZCode 양쪽에 배선. Go·Kotlin·TypeScript·Rust 등 전부 인덱싱.
- **새 워크트리는 SessionStart 훅(`codegraph-autoindex.py`)이 자동 준비** — 형제 워크트리 인덱스를 복사+`sync`(<1s)하거나 없으면 풀 init, 백그라운드라 세션 지연 0. 즉 워크트리마다 손수 `codegraph init` 할 필요 없다. ZCode 워크트리도 `zcode-worktree-init.sh`가 메인 체크아웃의 인덱스를 복사+`sync`로 동일하게 준비.
- 설치/재빌드: `npm i -g @colbymchenry/codegraph@1.4.1` → `codegraph init` (정밀도·explore NL 수정 포함). 재인덱싱은 `codegraph index`, MCP 재배선은 `codegraph install`. GPU·컴파일 불필요(aarch64 네이티브). 상세는 메모리 [[codegraph-adoption]] 참조.
- 문자열-키 간접참조(RPC 메서드명·툴명·이벤트명 → 핸들러/이벤트 타입)는 CodeGraph가 못 잇는 엣지(static-analysis frontier). **`scripts/dev/rpcmap.py`가 결정적으로 채운다**: `rpcmap <메서드명|툴명|이벤트명>` → 핸들러+파일:라인+`codegraph node` 힌트 (예: `rpcmap miniapp.people.list`→`peopleList (people.go:91)`, `rpcmap wiki`→`ToolWiki`, `rpcmap chat.delivery_failed`→`ChatDeliveryFailedEvent`). 역방향 `rpcmap --handler <이름>`, 전체는 `rpcmap --list`. 핸들러를 얻으면 `codegraph node <핸들러>`로 소스+호출자/피호출자. (점 있는 메서드명을 grep하면 훅이 rpcmap으로 유도한다.)
- **개념(시맨틱) 검색**: 심볼 이름을 모르는 "무엇을 하는 코드가 어딨나" 질의는 `make codesearch Q="재시도 백오프"` — Nemotron 임베딩(로컬 :8002) dense 검색과 CodeGraph FTS를 RRF 융합, 한국어 질의는 도메인 동의어 확장으로 영문 코드에 닿는다. 인덱스는 `make codesearch-index`(CodeGraph 노드 기반 증분, `.codegraph/semantic-code.*`), 품질 회귀는 `make codesearch-bench`(골드셋 P@5).
- 주의: CodeGraph는 **소스 코드 전용**. 위키/업무 지식 그래프는 별개 도구(`graphify` 챗 툴 → `~/.deneb/wiki-graph`)이며 이걸로 대체 불가.

## 룰 인덱스 — 필요할 때 Read (조건부 로딩)

> 상세 규칙은 `docs/agent-rules/`에 있고 **자동 주입되지 않는다**. 대상 경로를 처음 Edit/Write 하면 훅(`scripts/dev/claude-rules-gate.py`)이 1회 차단하며 해당 룰을 안내한다 — 안내된 룰 중 작업과 관련된 것을 Read 후 재시도하라. 룰 추가/수정 규약은 [docs/agent-rules/README.md](docs/agent-rules/README.md).

| 파일 (docs/agent-rules/) | 언제 읽나 |
|---|---|
| architecture.md | 구조/모듈맵 오리엔테이션이 필요할 때 |
| go-gateway.md · live-testing.md · concurrency.md · logging.md | gateway-go 코드를 만질 때 |
| prompt-cache.md | 챗 prompt/캐시/컴팩션 경로 (불가침 원칙 — 위반 금지) |
| hub-wiring.md | method_registry·GatewayHub 배선 |
| self-improvement.md | genesis 자가개선 루프 (스킬 생성·진화·큐레이션·자기교정 캡처·게이트) |
| model-roles.md · sidecar-models.md | LLM 역할 배치 · 로컬 모델/wormhole 운영 |
| wiki-layout.md | 위키 도메인 (project_layout.go 규약) |
| native-design-system.md · native-live-app.md | client-android UI · 실앱 라이브 검증 |
| denebui.md | deneb-ui 카드 (라벨 HTML) — 3구현 동기·계약 위치·검증 사슬 |
| generated-code.md | 생성 파일 재생성 방법 |
| release-and-deploy.md | 배포 · APK 발행/서명 · OTA |
| codebase-health-v2.md | Health Bench 2.0 점수·finding·baseline 변경 |
| codebase-health-v3.md | Health Bench 3.0 도메인·점수·baseline·RSI 배선 |
| rsi-bench.md | RSI Bench 과정·효용 점수·baseline·ratchet |
| git-pr.md · testing.md · docs.md · build-status.md · collaboration.md · optimization.md | 각 주제 상세 |

**도구 가이드** (`docs/tools/`): [zcode-environment.md](docs/tools/zcode-environment.md) (워크트리 격리·CodeGraph·훅 파이프라인·헬퍼 스크립트) · [creating-skills.md](docs/tools/creating-skills.md) (스킬 작성).
