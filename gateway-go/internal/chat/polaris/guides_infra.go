package polaris

const providerGuide = `프로바이더는 LLM 제공자 플러그인 시스템이다. 모델 검색, 인증, 기능 감지를 관리한다.

## 모델 사용 흐름
1. 사용자/설정에서 provider/model 지정
2. 프로바이더 ID 정규화 (z.ai→zai, qwen→qwen-portal 등)
3. 모델 해석 → 인증 준비 → 기능 감지 (스트리밍, 캐싱, 도구 지원)

## 프로바이더 설정 (ConnectorConfig)
- BaseURL: API 엔드포인트
- APIKey: 인증 키
- AuthMode: api_key, bearer, oauth, token, none
- Headers: 커스텀 헤더 (${VAR} 확장 지원)

## 모델 카탈로그
각 모델 엔트리에 포함되는 정보:
- Provider, ModelID, Label
- ContextWindow, Reasoning 지원 여부
- APIType (openai, anthropic 등)

## 프로바이더 기능 확장
플러그인 인터페이스로 다양한 훅 제공:
- DynamicModelResolver: 커스텀 모델 ID 조회
- ThinkingPolicyProvider: 사고 모드 지원 여부
- CatalogAugmenter: 추가 모델 항목 주입
- CapabilitiesProvider: 스트리밍/캐싱/도구 지원 플래그

## 문제 해결
- "모델을 못 찾아요" → 프로바이더 ID 정규화 규칙 확인 (z.ai→zai 등)
- "인증 실패" → APIKey, AuthMode 설정 확인. AuthDoctorProvider가 진단 힌트 제공
- "모델 변경" → gateway(action:'config.patch', patch:{agents:{defaults:{model:'provider/model-id'}}})`

const liteparseGuide = `LiteParse는 PDF, Office 문서 등 바이너리 파일에서 텍스트를 추출한다.

## 지원 형식
- PDF
- Office: DOCX, XLSX, PPTX (+ 레거시 DOC, XLS, PPT)
- OpenDocument: ODT, ODS, ODP
- CSV

## 사용 방식
web 도구로 문서 URL을 가져오면 자동으로 LiteParse가 텍스트 추출.
별도 호출 불필요 — MIME 타입 감지 후 자동 처리.

## 제한
- 입력 파일 최대 50MB
- 출력 텍스트 최대 200KB
- 파싱 타임아웃 60초

## 설치
npm i -g @llamaindex/liteparse
설치 안 되어 있으면 문서 파싱이 자동 스킵됨.

## 문제 해결
- "문서 파싱 실패" → lit CLI 설치 여부 확인: which lit
- "출력이 잘려요" → 200KB 제한. 큰 문서는 핵심 부분만 추출됨
- "지원 안 되는 형식" → 위 목록의 형식만 지원. 이미지/영상은 불가`

const metricsGuide = `Prometheus 호환 메트릭 시스템. /metrics 엔드포인트에서 수집.

## 확인 방법
curl http://localhost:18789/metrics

## 주요 메트릭
### RPC
- deneb_rpc_requests_total: 요청 수 (method, status 라벨)
- deneb_rpc_duration_seconds: 응답 시간 분포

### LLM
- deneb_llm_request_duration_seconds: LLM 호출 시간 (provider, model 라벨)
- deneb_llm_tokens_total: 토큰 사용량 (direction, model 라벨)

### 세션
- deneb_active_sessions: 활성 세션 수
- deneb_websocket_clients: 웹소켓 연결 수

## 메트릭 타입
- Counter: 증가만 (요청 수 등)
- Gauge: 증감 (활성 세션 수 등)
- Histogram: 분포 (응답 시간 등)

모두 atomic 연산으로 lock-free, 동시성 안전.

## 문제 해결
- "메트릭이 안 나와요" → /metrics 경로 확인, 게이트웨이 포트 확인
- "Prometheus 연동" → scrape_configs에 localhost:18789 추가
- "특정 메트릭 찾기" → curl ... | grep deneb_`

const transcriptGuide = `트랜스크립트는 세션 대화 기록을 JSONL 파일로 영구 저장한다.

## 저장 형식
~/.deneb/agents/<agentId>/sessions/<sessionId>.jsonl
- 첫 줄: 세션 헤더 (type, version, id, timestamp)
- 이후: 메시지 (role, content, timestamp, tokenCount)
- 컴팩션 후: 요약 메시지 (compacted: true)

## 주요 동작
- 메시지 추가: 원자적 쓰기 (append-only)
- 메시지 읽기: 전체 또는 최근 N개 미리보기
- 세션 삭제: 파일 삭제
- 실시간 업데이트: 콜백 리스너 등록 가능

## 컴팩션 연동
- 토큰 사용률 75% 초과 시 트리거
- 오래된 메시지를 요약으로 교체
- 최근 8개 메시지는 항상 보존
- 원자적 파일 교체 (크래시 안전)

## 제한
- 줄당 최대 버퍼: 10MB (큰 도구 출력 처리용)
- 초기 버퍼: 512KB

## 문제 해결
- "대화 기록이 사라짐" → 컴팩션으로 요약된 것. 원본은 요약으로 대체됨
- "파일이 너무 커요" → /compact 수동 실행으로 크기 줄이기
- "기록 검색" → sessions_search(query:'...') 또는 aurora_grep 사용`
