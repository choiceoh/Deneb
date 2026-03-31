package polaris

const memoryGuide = `메모리는 대화 간 정보를 영구 보존하는 통합 시스템이다. 팩트 저장소(DB) + 파일 검색 두 레이어.

## 사용법
- 검색: memory(action:'search', query:'사용자 선호')
- 저장: memory(action:'set', query:'사용자는 다크모드 선호', category:'preference')
- 조회: memory(action:'get', fact_id:42)
- 삭제: memory(action:'forget', fact_id:42)
- 상태: memory(action:'status')

## 두 레이어
1. **팩트 저장소**: SQLite DB. 중요도 점수(0-1), 카테고리, 임베딩 벡터 검색
2. **파일 검색**: MEMORY.md, memory/*.md 파일에서 키워드 매칭

검색하면 두 레이어 결과를 합쳐서 랭킹.

## 자동 메모리 저장
컴팩션 직전에 대화의 중요 정보를 자동 저장 (메모리 플러시).
설정: agents.defaults.compaction.memoryFlush.enabled (기본 true)

## 수동 메모리 편집
MEMORY.md 파일을 직접 write/edit 도구로 수정 가능.

## 문제 해결
- "기억 못 해요" → memory(action:'search')로 검색하거나 aurora_grep으로 대화 기록 검색
- "임베더 없으면?" → 키워드 매칭만 동작, 중요도 0.7 이상만 반환
- "파일 캐시" → 메모리 파일은 5분 캐시. 수정 직후 검색하면 반영 안 될 수 있음`

const sessionsGuide = `세션은 개별 대화 단위. 라이프사이클 관리와 격리를 제공한다.

## 세션 키 형식
- DM: agent:<agentId>:main
- 그룹: agent:<agentId>:<channel>:group:<chatId>
- 포럼: ...group:<chatId>:topic:<topicId>
- 크론: cron:<jobId>
- 서브에이전트: <parentKey>:<label>:<unixMs>

## 라이프사이클
IDLE → RUNNING → DONE / FAILED / KILLED / TIMEOUT
- 같은 세션에서 동시 실행 불가 (직렬 큐)
- 상태 전이는 검증됨 (DONE→RUNNING 불가)

## 세션 리셋
- 매일 새벽 4시 자동 리셋 (dailyResetHour 설정)
- /new 또는 /reset 명령으로 수동 리셋
- 유휴 리셋: idleResetMinutes 설정 (선택)

## 세션 도구
- sessions_list() — 활성 세션 목록
- sessions_history(sessionKey:'...', limit:10) — 대화 기록
- sessions_search(query:'검색어') — 전문 검색
- sessions_send(sessionKey:'...', message:'...') — 다른 세션에 메시지
- sessions_spawn(task:'...', label:'research') — 서브에이전트 생성
- session_status() — 현재 세션 정보

## 큐 모드
메시지가 실행 중에 들어오면:
- collect: 모아서 한 번에 처리
- steer: 실행 중인 에이전트에 주입
- followup: 현재 끝나면 다음으로 처리

## 문제 해결
- "매일 아침 대화 초기화됨" → 새벽 4시 자동 리셋. dailyResetHour 설정으로 변경
- "동시에 두 요청이 안 돼요" → 세션 내 직렬 처리. 서브에이전트로 병렬화 가능
- "DONE→RUNNING 안 됨" → 새 실행을 시작해야 함. 이전 실행 재시작 불가`

const architectureGuide = `Deneb은 Go + Rust 두 런타임이 협력하는 AI 게이트웨이다.

## 두 런타임
1. **Go 게이트웨이** (메인): HTTP/WS 서버, RPC, 세션, 채팅, 채널, 인증, 크론, 스킬
2. **Rust 코어** (CGo FFI): 프로토콜 검증, 보안, 미디어 감지, 마크다운, 메모리 검색, 컨텍스트 엔진, 컴팩션

## 통신 방식
- Go ↔ Rust: CGo FFI (인프로세스, 오버헤드 없음)
- CLI ↔ 게이트웨이: 웹소켓
- 공유 타입: proto/ 디렉토리의 Protobuf 스키마

## 빌드 순서
Proto 스키마 → Rust 코어 (정적 라이브러리) → Go 게이트웨이 (Rust를 CGo로 링크)

## 주요 서브시스템
- **서버**: HTTP/WS, RPC 130+ 메서드
- **세션**: 라이프사이클 상태 머신, 큐잉
- **채널**: 플러그인 레지스트리 (텔레그램이 주력)
- **채팅**: 시스템 프롬프트, 도구, 에이전트 루프
- **인증**: HMAC-SHA256 토큰, 역할(operator/viewer/agent/probe)

## 하드웨어
DGX Spark: 로컬 GPU 추론, 10 동시성, CUDA 지원

## 문제 해결
- "빌드 에러" → make rust 먼저, 그 다음 make go (순서 중요)
- "FFI 에러 -99" → Rust 패닉. 로그에서 원인 확인
- "RPC 큐잉" → 워커 풀은 CPU×2 (4~64). 초과하면 대기`

const channelsGuide = `채널은 외부 메시징 플랫폼과 Deneb을 연결하는 플러그인이다.

## 현재 지원
텔레그램이 유일한 운영 채널. 프로젝트 철학상 단일 플랫폼 최적화.

## 채널 라우팅 흐름
1. 채널이 외부 메시지 수신
2. dmScope + 채팅 타입에 따라 세션 키 결정
3. 세션 큐에 메시지 추가
4. 에이전트 실행 → 응답을 같은 채널로 전달

## 기능 선언 (Capabilities)
채널별로 지원하는 기능을 선언: 텍스트, 미디어, 리액션, 타이핑, 스레드, 포럼, 인라인 키보드, 파일 업로드

## 그룹 처리
- 일반 그룹: 하나의 공유 세션
- 포럼 그룹: 토픽별 개별 세션
- groupAllowFrom: 그룹별 발신자 필터 (없으면 allowFrom으로 폴백)

## 문제 해결
- "그룹에서 봇이 응답 안 함" → groupAllowFrom 또는 allowFrom 설정 확인
- "포럼 토픽마다 대화가 따로임" → 정상. 포럼은 토픽별 세션 격리
- "채널 추가하고 싶어요" → 텔레그램만 지원. Plugin 인터페이스로 확장 가능하나 비권장`

const telegramGuide = `텔레그램은 Deneb의 주력 채널. 커스텀 Go Bot API 클라이언트 사용.

## 설정
- 봇 토큰: channels.telegram.botToken 또는 TELEGRAM_BOT_TOKEN 환경변수
- BotFather에서 /newbot으로 생성

## 접근 제어
- DM 정책: pairing(기본), allowlist, open, disabled
- 그룹 정책: open, allowlist(기본), disabled
- 사용자 ID: 숫자 (예: 8734062810)

## 메시지 제약
- 4096자 제한 (자동 분할)
- MarkdownV2 형식
- 파일 업로드 50MB 제한

## 상태 이모지
👀 대기 → 🤔 생각 → 🔥 도구 실행 → ⚡ 웹 검색 → 👍 완료 → 😱 에러

## 포럼 토픽
포럼 그룹에서는 토픽별로 독립 세션. 일반 그룹은 하나의 공유 세션.

## 폴링 vs 웹훅
- 롱 폴링 (기본): 설정 간단, 서버 없이 동작
- 웹훅 (선택): 낮은 지연, HTTPS 엔드포인트 필요

## 프라이버시 모드
- 기본: 그룹에서 /명령만 수신 (관리자 아닌 경우)
- /setprivacy 변경 시 봇 제거 후 재추가 필요

## 문제 해결
- "메시지가 잘려요" → 4096자 자동 분할. 포맷이 깨질 수 있음
- "그룹에서 반응 없음" → 프라이버시 모드 확인. 관리자 권한 또는 /setprivacy 끄기
- "MarkdownV2 깨짐" → 특수문자(_, *, [, ] 등) 자동 이스케이프 처리됨`

const skillsGuide = `스킬은 에이전트 기능을 확장하는 모듈. 각 스킬은 SKILL.md 파일로 정의.

## 스킬 우선순위 (나중이 우선)
1. extra (설정의 extra-dirs)
2. bundled (~/.deneb/bundled-skills)
3. managed (~/.deneb/skills)
4. agents-personal (~/.agents/skills)
5. agents-project (워크스페이스/.agents/skills)
6. workspace (워크스페이스/skills)

## SKILL.md 형식
프론트매터(YAML):
- name: 스킬 이름 (필수)
- description: 한 줄 설명 (필수)
- always: true면 자격 검사 건너뜀
- requires: {bins: [], env: [], config: []} — 실행 조건

본문: 에이전트가 따를 마크다운 지시문.

## 에이전트의 스킬 사용 방식
1. 시스템 프롬프트의 <available_skills>에서 스킬 목록 확인
2. 사용자 요청과 스킬 이름/설명 매칭
3. read 도구로 SKILL.md 로드
4. 지시문에 따라 작업 수행
명시적 호출 API 없음 — 에이전트가 자율 판단.

## 제한
- 시스템 프롬프트에 최대 150개, 30,000자
- SKILL.md 최대 256KB

## 문제 해결
- "스킬이 안 보여요" → config.skills.entries[key].enabled 확인, requires 조건 충족 여부 확인
- "스킬이 너무 많아 프롬프트에 안 들어감" → 150개/30K자 제한. 불필요한 스킬 비활성화
- "항상 로드하고 싶으면" → metadata.always: true 설정`

const pilotGuide = `Pilot은 로컬 sglang(경량 AI)으로 도구 출력을 분석하는 도구.

## 언제 쓰나?
- exec/test/diff 같은 긴 출력을 요약할 때
- 여러 소스를 합쳐서 분석할 때
- 연쇄 조회(결과→다음 도구)가 필요할 때

## 사용법
- 파일 분석: pilot(task:'구조 설명', file:'path/to/file.go')
- 검색 분석: pilot(task:'패턴 분석', grep:'pattern', path:'src/')
- 명령 결과 요약: pilot(task:'결과 요약', exec:'make test')

## Pilot vs 직접 도구
- 원시 출력만 필요 → 직접 도구 (read, grep, exec)
- 출력 분석/요약 필요 → pilot
- 복잡한 추론 → 메인 모델 (pilot 아님)

## 제한
- 전체 타임아웃: 2분
- 소스 최대 10개, 소스당 30초
- 입력 최대 24,000자 (자동 잘림)

## sglang 설정
- URL: SGLANG_BASE_URL (기본 http://127.0.0.1:30000/v1)
- 모델: SGLANG_MODEL (기본 Qwen/Qwen3.5-35B-A3B)
- sglang 없으면 → 원시 결과만 반환 (LLM 분석 없음)

## 문제 해결
- "pilot 결과가 raw로 나와요" → sglang이 꺼져있음. 서비스 상태 확인
- "타임아웃" → 소스가 많거나 느림. 소스 수 줄이거나 timeout 확인
- "분석이 얕아요" → 로컬 경량 모델 한계. 깊은 분석은 메인 모델 사용`

const cronGuide = `크론은 예약 작업을 관리한다. 정해진 시간에 에이전트 턴을 자동 실행.

## 사용법
- 상태: cron(action:'status')
- 목록: cron(action:'list')
- 추가: cron(action:'add', name:'일일리포트', schedule:'0 9 * * *', command:'리포트 생성')
- 수정: cron(action:'update', jobId:'...', schedule:'0 10 * * *')
- 삭제: cron(action:'remove', jobId:'...')
- 즉시 실행: cron(action:'run', jobId:'...')

## 스케줄 형식
- 크론 표현식: "0 9 * * *" (매일 오전 9시)
- 인터벌: "every 5m", "every 1h"

## 세션 모드
- main: 메인 세션에서 실행
- isolated: 매 실행마다 새 세션 (깔끔)
- current: 현재 세션에서 실행

## 전달 방식
- none: 결과 저장만 (무음)
- announce: 알림 채널로 전송
- webhook: URL로 POST

## 실패 처리
- N번 연속 실패 후 알림 (기본 3회)
- 쿨다운으로 알림 스팸 방지
- deleteAfterRun: true면 1회 실행 후 자동 삭제

## 문제 해결
- "작업이 안 돌아요" → cron(action:'list')로 enabled 상태 확인
- "연속 실패 알림" → 에러 원인 확인. 최소 2초 간격으로 재시도됨
- "일회성 작업" → deleteAfterRun: true 설정`
