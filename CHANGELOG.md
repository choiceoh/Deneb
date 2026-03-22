# Deneb Changelog

## v3.2 — 2026-03-21

ACP (Claude Code) 연동 활성화, 코드 구조 리팩토링.

### 변경 사항

- **ACP/Claude Code 연동** — acpx 플러그인 활성화, `acp.allowedAgents` 설정
- **코드 리팩토링** — 대형 파일에서 9개 전용 모듈 추출 (PR #22)

## v3.160 — 2026-03-21

Deneb 독립 포크. upstream 동기화 종료.

### 포크 기반

- upstream v3.141 기반
- Deneb 리브랜딩 완료
- upstream 509커밋 (v2026.3.7) 미반영 — 독립 개발

### 신규 기능

- **Aurora Memory Module** — AI-agent-first 메모리 파일 관리 (memory-md-manager)
- **Vega QMD 백엔드 통합** — VegaMemoryManager, LCM 네이티브화
- **Vega CLI 래퍼** — bin/vega wrapper + install.sh 개선
- **Telegram custom apiRoot** 지원 (upstream cherry-pick)
- **컨텍스트 엔진** — transcript maintenance 기능 (upstream cherry-pick)

### 개선

- Telegram 전용 빌드 (미사용 채널 어댑터 제거)
- 에러 리질리언스 계층 추가
- Rolldown 빌드 안정화 (stale .js 정리, clean:true)
- Subagent 타임아웃 시 부분 진행 결과 포함
- JSONL 세션 로그 트렁케이션 (디스크 과다 사용 방지)
- Compaction 후 세션 JSONL 자동 트렁케이션

### 버그 수정

- Telegram 스트리밍 미리보기 종료 시 message:sent hook 발행
- Telegram dmPolicy pairing 경고
- 시퀀스 갭 브로드캐스트 스킵
- 잘못된 형식의 replay tool call sanitize
- Thread binding 직렬화
- Telegram accountId 누락 시 잘못된 봇 라우팅 방지
