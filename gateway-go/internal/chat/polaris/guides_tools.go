package polaris

const toolsGuide = `Deneb 에이전트는 42개 이상의 도구를 사용할 수 있다.

## 도구 카테고리
- **File**: read, write, edit, grep, find — 파일 읽기/쓰기/검색
- **Code**: multi_edit, tree, diff, analyze, test — 코드 작업
- **Git**: git — 버전 관리
- **Exec**: exec, process — 명령 실행, 백그라운드 프로세스
- **AI**: pilot (로컬 sglang 분석), polaris (시스템 지식)
- **Web**: web (검색+페치), http (API 호출)
- **Memory**: memory (통합 메모리), vega (프로젝트 검색)
- **System**: cron (스케줄링), message (메시지 전송), gateway (설정 관리)
- **Sessions**: sessions_list/history/search/send/spawn, subagents
- **Media**: image (비전 분석), youtube_transcript, send_file
- **Data**: gmail, kv (키-값 저장소)
- **Utilities**: batch_read, search_and_read, inspect, apply_patch, health_check

## 실용 팁
- 독립적인 도구는 **병렬 호출**하면 더 빠름
- 도구 출력이 크면 "compress": true로 자동 요약 (로컬 sglang 사용)
- grep/find/tree 결과는 실행 내에서 캐싱됨 — 같은 패턴 반복 호출 불필요
- 도구 출력이 64K자 넘으면 자동 트리밍 (앞뒤 보존)

## 도구 연쇄 ($ref)
한 턴에서 도구 간 결과 전달:
{"$ref": "tool_use_id"} → 다음 도구에 _ref_content로 주입
예: grep으로 찾은 파일을 pilot으로 분석

## 자주 쓰는 패턴
- 파일 구조 파악: tree → analyze(action:'outline')
- 코드 수정: edit (단일) 또는 multi_edit (여러 파일 동시)
- 변경 검증: diff → test(action:'run') → git(action:'commit')
- 큰 출력 분석: exec → pilot(task:'결과 요약')

## 주의사항
- 도구 이름은 대소문자 구분 ("exec" ≠ "Exec")
- $ref는 30초 타임아웃 — 느린 도구에 의존하면 실패할 수 있음
- grep은 200줄, find는 500개 항목까지 자동 요약`

const systemPromptGuide = `시스템 프롬프트는 LLM에 주입되는 에이전트 지시문이다. 에이전트의 성격, 도구 사용법, 행동 규칙을 정의한다.

## 구조 (3개 블록)
1. **정적 블록** (캐싱): 도구 목록, 사용법, 안전 규칙, CLI 레퍼런스 — 서버 시작 후 거의 안 변함
2. **준정적 블록** (캐싱): 스킬 목록 — 스킬 추가/제거 시에만 변경
3. **동적 블록** (매 요청): 메모리, 워크스페이스, 날짜, 컨텍스트 파일, 런타임 정보

Anthropic API 사용 시 정적/준정적 블록이 캐싱되어 입력 토큰 비용이 절반으로 줄어든다.

## 컨텍스트 파일
워크스페이스에서 자동 로드: CLAUDE.md, SOUL.md, TOOLS.md, IDENTITY.md, USER.md, MEMORY.md
- 파일당 최대 20K자, 전체 최대 150K자
- 넘치면 앞 70% + 뒤 20% 보존, 중간 생략

## 응답 스타일
- 기본 언어: 한국어
- 텔레그램 4096자 제한에 맞춰 간결하게
- 이모지 자연스럽게 사용

## 바이브 코더 모드
텔레그램 코딩 채널용 별도 프롬프트:
- 코드 직접 노출 금지, 한국어로 설명
- 📝 변경 요약 → 🔨 빌드 → 🧪 테스트 형식
- 버튼으로 다음 액션 제안

## 설정 변경
- 에이전트 성격: SOUL.md 파일 수정
- 추가 지시: CLAUDE.md 파일 수정
- 프롬프트 자체 수정: gateway-go/internal/chat/prompt/system_prompt.go`
