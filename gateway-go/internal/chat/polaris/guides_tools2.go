package polaris

const webGuide = `web 도구는 웹 검색, URL 페치, 검색+자동페치 3가지 모드를 제공한다.

## 사용법
- URL 내용 가져오기: web(url:'https://example.com')
- 웹 검색: web(query:'golang error handling')
- 검색 후 상위 결과 자동 페치: web(query:'kubernetes pods', fetch:2)

## 언제 어떤 모드?
- 특정 URL 내용 → url만 지정
- 최신 정보 탐색 → query만 지정
- 리서치 + 종합 → query + fetch:2

## 문서 자동 파싱
PDF, Office(DOCX/XLSX/PPTX), CSV 등 바이너리 문서는 자동으로 텍스트 추출 (LiteParse CLI 필요: npm i -g @llamaindex/liteparse)

## 제한사항
- 다운로드 최대 5MB, 출력 기본 50,000자
- SSRF 보호: 내부 IP 접근 차단
- 봇 차단 사이트: 3단계 스텔스 프로필 자동 에스컬레이션

## YouTube
YouTube URL을 web에 넣으면 자동으로 자막 추출. 또는 youtube_transcript 도구 직접 사용.`

const execGuide = `exec 도구는 셸 명령 실행, process 도구는 백그라운드 프로세스 관리.

## exec 사용법
- 기본: exec(command:'ls -la')
- 타임아웃 지정: exec(command:'make test', timeout:120)
- 백그라운드: exec(command:'npm start', background:true)

## 기본값
- 타임아웃: 30초 (최대 300초/5분)
- 작업 디렉토리: 에이전트 워크스페이스

## 백그라운드 모드
background:true로 실행하면 sessionId를 즉시 반환. 이후 process 도구로 관리:
- process(action:'poll', sessionId:'...') — 상태 확인
- process(action:'log', sessionId:'...') — 전체 로그
- process(action:'kill', sessionId:'...') — 종료

## 주의사항
- 30초 넘는 빌드는 timeout 지정하거나 background:true 사용
- 출력이 길면 pilot으로 감싸서 요약: pilot(task:'결과 요약', exec:'make test')`

const gatewayToolGuide = `gateway 도구는 Deneb 게이트웨이 자체를 관리한다.

## 설정 읽기
gateway(action:'config.get') — 현재 deneb.json 설정 반환

## 설정 변경 (추천)
gateway(action:'config.patch', patch:{agents:{defaults:{model:'anthropic/claude-sonnet-4-20250514'}}})
지정한 키만 업데이트, 나머지는 보존.

## 설정 전체 교체 (주의)
gateway(action:'config.apply', config:{...}) — 전체 덮어씀, 위험

## 설정 스키마 조회
gateway(action:'config.schema.lookup', path:'agents.defaults.model') — 해당 키의 타입/설명 확인

## 재시작
gateway(action:'restart') — SIGUSR1로 그레이스풀 재시작

## 셀프 업데이트
gateway(action:'update.run') — git pull + make all 실행 (2분 타임아웃)`

const mediaGuide = `미디어 도구: 이미지 분석, YouTube 자막, 파일 전송.

## 이미지 분석 (비전)
- 단일: image(image:'/path/to/screenshot.png', prompt:'이 에러 뭐야?')
- 여러 장: image(images:['/img1.png', '/img2.png'], prompt:'차이점 설명')
- URL도 가능: image(image:'https://example.com/photo.jpg')
- 최대 20장, 응답 4096 토큰

## YouTube 자막
youtube_transcript(url:'https://youtube.com/watch?v=...')
동영상 자막을 텍스트로 추출 (90초 타임아웃)

## 파일 전송
send_file(path:'/path/to/report.pdf')
텔레그램으로 파일 전송 (최대 50MB, 자동 MIME 타입 감지)`

const gmailGuide = `Gmail 도구는 OAuth2로 Gmail에 접근한다.

## 빠른 사용법
- 받은편지함: gmail(action:'inbox')
- 검색: gmail(action:'search', query:'from:user@example.com is:unread')
- 읽기: gmail(action:'read', message_id:'...')
- 전송: gmail(action:'send', to:'user@example.com', subject:'제목', body:'내용')
- 답장: gmail(action:'reply', message_id:'...', body:'답변 내용')
- 라벨: gmail(action:'label', label_action:'add', label_name:'중요', message_id:'...')

## 연락처 별칭
한 번 이메일을 보내면 자동으로 별칭이 KV에 저장됨.
이후에는 gmail(action:'send', to:'peter', ...) 처럼 별칭만으로 전송 가능.

## 설정
~/.deneb/credentials/gmail_client.json + gmail_token.json 필요 (OAuth2)`

const dataToolsGuide = `KV 스토어와 HTTP 클라이언트.

## KV 스토어 (영구 저장소)
~/.deneb/kv.json에 JSON으로 저장. 세션 간 데이터 유지.
- 저장: kv(action:'set', key:'my_key', value:'my_value')
- 읽기: kv(action:'get', key:'my_key')
- 삭제: kv(action:'delete', key:'my_key')
- 목록: kv(action:'list', prefix:'gmail.')

활용: 연락처 별칭, 사용자 선호, 캐시된 데이터 등.

## HTTP (API 호출)
- GET: http(url:'https://api.example.com/data')
- POST: http(url:'https://api.example.com/data', method:'POST', json:{key:'value'})
- 커스텀 헤더: http(url:'...', headers:{"Authorization": "Bearer ..."})
- 타임아웃: 기본 30초, 최대 120초
- 응답 최대 5MB, 50,000자`

const sessionToolsGuide = `세션 관리 도구 모음.

## 세션 목록/검색
- sessions_list() — 활성 세션 목록 (kind: main/group/cron/hook 필터 가능)
- sessions_history(sessionKey:'...', limit:10) — 대화 기록 읽기
- sessions_search(query:'검색어') — 모든 세션에서 전문 검색

## 크로스 세션 메시징
sessions_send(sessionKey:'agent:default:main', message:'메시지')
다른 세션에 메시지를 보내고 해당 세션에서 에이전트 실행 트리거.

## 서브 에이전트
- 생성: sessions_spawn(task:'리서치 해줘', label:'research', model:'...')
- 모니터링: subagents(action:'list')
- 조종: subagents(action:'steer', target:'research', message:'추가 지시')
- 종료: subagents(action:'kill', target:'all')

서브 에이전트는 독립 세션에서 실행되므로 메인 대화와 분리됨.

## 현재 세션 상태
session_status() — 세션 키, 종류, 모델, 토큰 사용량 등`

const messageGuide = `메시지 도구로 사용자에게 메시지를 보내거나 반응한다.

## 사용법
- 보내기: message(action:'send', message:'안녕하세요')
- 답장: message(action:'reply', message:'네!', replyTo:'메시지ID')
- 스레드 답장: message(action:'thread-reply', message:'내용', replyTo:'메시지ID')
- 리액션: message(action:'react', emoji:'👍', messageId:'메시지ID')

## 라우팅
- 기본: 현재 대화로 전송
- 다른 채널: channel + to 파라미터 지정
- 다른 세션: sessions_send 도구 사용 (message 도구 아님)

## 무음 응답 (NO_REPLY)
message 도구로 이미 응답을 보냈으면, LLM 응답에 NO_REPLY만 적어서 중복 전달 방지.

## 텔레그램
- 4096자 넘으면 자동 분할
- MarkdownV2 자동 포맷팅`
