package ai.deneb.deneb

// Compiled-in changelog — the record of what the native client shipped over time.
//
// This is deliberately separate from [UpdateManifest.notes]: that field describes a
// *newer* build fetched from the gateway's version.json and only appears when an
// update is available. These notes are the changelog for builds up to and including
// the one the user is running, so they work offline and survive the gateway being
// down. The settings "버전" card surfaces them on demand (no auto-popup).
//
// Newest first. There is no per-entry version label: the app has no semantic
// versionName anymore (it is identified by versionCode alone), so the sheet is a
// flat reverse-chronological changelog headed by "현재 빌드 N". When you ship a
// release with user-facing changes, prepend a new entry here with its highlights.

/** One released build and the user-facing highlights it introduced. */
data class DenebPatchNote(
    val highlights: List<String>,
)

val DENEB_PATCH_NOTES: List<DenebPatchNote> = listOf(
    DenebPatchNote(
        highlights = listOf(
            "Dropbox 연동 시 실제로는 연결됐는데 '인증 코드 교환에 실패했습니다'가 뜨던 문제 수정 — 승인 코드가 두 번 처리되며 생기던 거짓 오류를 제거",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "챗봇 모드에서는 대화 글자 크기와 줄간격을 키워 더 편하게 읽을 수 있습니다 — 업무 모드는 기존 크기 그대로",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "내 메시지 말풍선이 긴 글에서도 화면 폭의 약 80%까지만 차지하도록 제한했습니다 — 왼쪽 끝까지 늘어나지 않고 항상 오른쪽에 단정하게 붙습니다",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "설정 > 연동에서 Dropbox 계정을 앱에서 직접 연결 — App key 입력 → 승인 페이지 열기 → 코드 붙여넣기로 끝납니다(호스트 명령어 불필요). 연결 후 채팅의 dropbox 도구·백업이 동작합니다",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "대화를 스크롤하는 동안 '맨 아래로' 버튼이 부드럽게 사라졌다가, 스크롤을 멈추면 다시 나타납니다 — 움직이는 화면을 가리지 않도록",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "내 메시지 말풍선을 테두리 없는 회색 배경으로 — 파란 강조 워시와 외곽선을 빼고 차분한 중립 회색으로 정돈",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "챗봇 모드에서는 하단 탭바가 통째로 사라져 채팅에 집중할 수 있습니다 — 상단의 챗봇/업무 토글로만 전환하고, 업무 모드로 돌아오면 탭바가 다시 나타납니다",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "응답을 생성하는 동안 화면 위쪽에서 색이 천천히 한 바퀴 순환하는(파랑→청록→…→파랑) 은은한 빛이 차오르고, 답변이 시작되면 검정으로 사라집니다 — 가장자리 글로우를 대신하는 '생성 중' 배경",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "더보기 화면을 설정과 같은 그룹 카드 양식으로 통일 — 검색·할일·일기·카테고리가 아이콘·제목·한 줄 설명·화살표가 있는 같은 행 모양으로 정돈됩니다",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "챗봇 모드에서는 업무 데이터 화면(메일·달력·검색·카테고리·플릿)을 네비게이션에서 숨겨 깨끗한 일반 대화 공간이 됩니다 — 업무 모드로 돌아오면 다시 나타납니다",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "채팅 입력창을 더 작고 단정하게 — 여백과 보내기 아이콘을 줄여, 마지막 메시지가 입력창 바로 위까지 붙어 보입니다",
            "맨 아래가 아닐 때 '맨 아래로' 버튼이 제대로 뜨고, 눌렀을 때 긴 메시지의 진짜 끝까지 내려갑니다 (GPT·클로드처럼)",
            "답변을 쓰는 동안 메시지 영역 가장자리에 은은한 오로라 빛이 흐릅니다 — 응답이 끝나면 사라집니다",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "챗봇 대화 기록을 개별 대화 목록으로 정리 — 고정 '챗봇' 홈 없이 각 대화가 독립 세션(전부 삭제 가능). 챗봇 모드로 돌아오면 마지막 대화를 복원하고, 없으면 새 대화로 시작",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "설정 > 모델에 '챗봇' 역할 추가 — 챗봇 모드 대화에 업무 모드와 다른(더 가벼운) 모델을 지정할 수 있습니다. 미설정이면 메인 모델을 그대로 사용",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "채팅 입력창 안내 문구를 '메시지를 입력하세요'로 정돈하고, 입력칸에 커서가 들어가면 안내 문구가 사라지도록 변경",
            "빈 대화 환영 화면을 모드별로 분리 — 업무는 시간대별 개인 맞춤 인사, 챗봇은 가벼운 대화 인사로 다르게 표시",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "하단 탭바가 채팅·메일·달력·설정으로 — 자주 쓰는 설정이 더보기 안쪽 대신 탭으로 올라오고, 카테고리는 더보기로 이동",
            "더보기에서 플릿 항목 제거 — 플릿은 설정 > 라우팅·인프라 안에 있어 한 곳에서만 보입니다",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "앱을 완전히 종료했거나 절전(Doze) 상태여도 능동 보고 알림이 도착 — 그동안 앱이 켜져 있을 때만 뜨던 메일 분석·아침 브리핑·예약 작업 알림을 푸시(FCM)로 받습니다",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "설정 > 스킬 > 진화 내역의 항목을 누르면 그 자리에서 펼쳐짐 — 잘렸던 전체 사유와 판단 근거, 정확한 시각, 리뷰 판정까지. 스킬이 지정된 항목은 '스킬 보기'로 해당 스킬 상세로 이동",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "메일 분석 접기 카드의 이중 테두리 제거 — 카드 속 카드로 좁아졌던 본문이 좌우로 넓어져 표·목록이 덜 잘림",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "대기 칩이 생각 내용을 실시간 중계 — \"깊이 생각 중: …발신인 이력을 대조\"처럼 지금 무슨 생각을 하는지 보임",
            "도구 작업이 끝나고 다음 단계로 넘어가는 동안 칩이 일반 스피너로 되돌아가지 않고 \"결과 검토 중…\"으로 이어짐",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "설정 > 스킬에서 스킬을 누르면 상세 화면이 열림 — 전체 설명·생성/사용/진화 이력과 SKILL.md 문서 본문, 그 스킬만의 진화 내역까지",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "업무 피드에서 메일 분석·리포트 카드를 열면 별도 대화 대신 업무 채팅의 해당 카드 위치로 이동 — 카드는 펼쳐진 채로 보여 바로 읽고 그 자리에서 이어서 질문 가능",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "모든 화면의 머리글이 하나로 — 메일·검색·카테고리·사람·위키·일기·크론·설정도 일정 화면과 같은 뒤로(←) + 큰 제목 프레임을 사용",
            "메일·카테고리의 다중 선택 하단 바가 그림자 떠 있는 패널 대신 본문과 같은 평면(헤어라인 구분선)으로",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "메일 분석이 업무 채팅에 접힌 카드로 도착 — 제목 한 줄만 보이고, 누르면 그 자리에서 전체 분석이 펼쳐짐 (다른 화면으로 이동하지 않음)",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "진행 표시가 대상까지 말해줌 — '메일 확인 중: 아르고에너지'처럼 무엇을 찾는지 표시, 실패한 단계는 '~ 실패'로 솔직하게",
            "답변 아래 작업 내역 한 줄 — 이번 답을 만들며 어떤 단계를 거쳤는지(메일 확인 ×2 · 웹 검색) 남음",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "답변을 기다리는 동안 비서가 지금 뭘 하는지 보여줌 — '생각 중…' 대신 '메일 확인 중', '기억 검색 중', '깊이 생각 중…' 같은 실제 진행 상황 표시",
            "10초 넘게 걸리는 작업은 경과 시간(· 32초)이 함께 표시 — 오래 걸려도 멈춘 게 아님을 알 수 있음",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "메일 검색창은 평소 숨겨두고, 목록을 아래로 끌어내리면 나타나도록 — 받은 메일이 화면 맨 위부터 보임 (계속 당기면 새로고침은 그대로)",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "인터랙티브 폼에 날짜·시간 선택 — 마감일·미팅 시각을 타이핑 대신 네이티브 피커로",
            "답변이 흐르는 동안 만들어지는 중인 인터랙티브 화면은 '화면 구성 중…'으로 차분하게 — 반쪽짜리 폼이 깜빡이던 것 해소",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "사람과 인물 위키를 한 화면으로 통합 — 최근 연락(메일 빈도)과 인물 문서가 한 목록에, 서로 아는 사람은 위키 요약이 함께 표시",
            "통합된 '사람'은 카테고리 화면 상단 진입점으로 이동 — 메뉴의 people 항목과 카테고리 목록의 '인물' 항목은 정리",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "메일 분석 접힘 카드의 미리보기가 제목 대신 본문 첫 줄을 표시 — 'AI 분석 · 이메일 분석' 같은 제목 반복 제거",
            "설정 모델 탭에서 키가 만료된 제공자를 '인증 만료'로 구분 표시 (응답 없음과 별개)",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "채팅 답변 속 제목(#·##·###)이 위키·분석 화면과 같은 글꼴 사다리로 통일 — 화면 어디서나 제목 위계가 한 가지 언어로 읽힘",
            "메일·사람·카테고리·세션 목록의 행을 누르는 동안 살짝 눌리는 촉감 피드백 — 할일·업무피드와 같은 감각으로 통일",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "인터랙티브 폼에 필수 입력 검증 — 빈 필수 항목이 있으면 제출이 막히고 해당 칸이 빨갛게 표시",
            "숫자·이메일·전화 입력 칸은 알맞은 키보드가 바로 열림, 선택 상자엔 안내 문구(placeholder) 지원",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "메일 본문 속 링크가 바로 탭 가능해지고, 본문 텍스트를 길게 눌러 복사 가능",
            "잘린 긴 메일은 '전체 보기'로 끝까지 읽기",
            "HTML 표 메일의 칸이 붙어 나오던 것 교정(품명·단가 구분), 목록은 • 글머리표, 인용은 > 표시로",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "메일 상세의 AI 분석이 기본 접힌 카드로 — 첫 줄 미리보기만 보이고 탭하면 펼쳐져, 긴 분석이 본문을 밀어내지 않음",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "메일 검색 추가 — 키워드·발신자(from:)로 전체 메일함을 바로 검색",
            "이미지 첨부는 메일 안에서 바로 미리보기 (영수증·사진을 열지 않고 확인)",
            "메일 상세 화면 정돈 — 휴지통과 AI 분석만 남기고 단순화",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "설정에서 '토픽문서' 탭 제거 — 업무 배경지식 문서는 채팅에서 \"배경지식에 추가해줘\"라고 부탁하면 에이전트가 직접 관리",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "데스크톱 채팅이 넓은 창에서도 읽기 좋은 가운데 칼럼으로 — 메시지·입력창이 한 폭으로 정렬",
            "데스크톱 사이드바 다듬기 — 줄 전체가 클릭되고, 마우스를 올리면 밝아지며, 클릭 후 남던 회색 상자 제거",
            "데스크톱에서 Ctrl(맥은 Cmd)+1~7로 채팅·메일·일정·검색·사람·카테고리·설정 바로 전환",
            "데스크톱에선 사이드바가 곧 내비게이션 — 일정의 '←', 검색·설정·사람·카테고리의 '닫기' 정리",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "화면 전반의 글꼴 위계를 데네브 타입 시스템으로 통일 — 페이지 제목은 크고 가볍게, 카드·섹션 제목은 또렷하게",
            "메뉴 서랍이 미니앱 시절의 큰 타이포 메뉴로 — 항목이 더 크고 시원하게",
            "대화 목록·위키 본문 등 한글이 너무 가늘게 보이던 곳의 두께를 읽기 좋게 교정",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "받은 메일을 Deneb 디자인으로 새 단장 — 울트라라이트 제목, 전체 폭 구분선, 오늘·어제·이번 주 시간 구분, 가까운 메일은 시각·요일로 표시",
            "내 메시지 말풍선에 오로라 색감 적용(다크), 도구 실행 표시가 OLED에서 투명한 외곽선 스타일로",
            "입력창 안내 문구를 '무엇이든 물어보세요'로",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "설정에 '화면' 탭 추가 — 테마(시스템·라이트·다크·OLED 블랙)와 화면 배율을 직접 조절, 즉시 반영",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "열이 많은 표는 가로 스크롤로 — 칸이 한 글자씩 찌그러지지 않고 전부 읽힘",
            "답변 속 이메일 주소가 바로 탭할 수 있는 링크로 — <주소> 꺾쇠 표기와 ***굵은 기울임***도 올바르게 표시",
            "H₂O·m² 같은 위·아래 첨자와 <b> 등 HTML 표기, [텍스트][1] 참조형 링크 지원",
            "문장 바로 아래 붙은 표, '2026. 6. 9.' 같은 날짜 줄, HTML 문자(&amp; 등) 렌더링 교정",
            "번호 목록이 10번을 넘어도 번호 칸이 밀리지 않게 정렬",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "설정 '모델' 탭을 제공자별로 묶어 정리하고, 메인·경량·폴백 역할이 어느 모델에 배정됐는지 요약으로 표시",
            "모델 역할이 헷갈릴 땐 '?'를 눌러 각 역할이 무슨 일을 하는지 설명 보기",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "설정에 '관찰' 탭 추가 — 게이트웨이가 스스로 무엇을 했는지(실행 횟수, 도구별 사용량·오류율)와 최근 경고·오류 로그를 읽기 전용으로 한눈에",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "데스크톱에서 메일 목록과 본문을 좌우로 나란히 보기 — 목록에서 고르면 오른쪽에 바로 펼쳐짐",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "데스크톱에 고정 좌측 사이드바 — 넓은 화면에서 메뉴를 항상 띄워두고 더 빠르게 이동",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "설정에 '스킬' 탭 추가 — 에이전트가 쓸 수 있는 스킬을 이름·설명·분류와 함께 확인(직접 부를 수 있는 스킬은 앞에 / 표시), 읽기 전용",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "할 일(To-do) 추가 — 마감일을 정하거나 날짜 없이도 등록, 체크로 완료 처리",
            "마감일이 있는 할 일은 달력 그 날짜에 함께 표시 (일정 아래 '할 일' 묶음)",
            "달력 상단 '할 일' 버튼으로 전체 목록 열기 — 진행 중·완료가 나뉘고 지난 마감은 빨갛게 강조",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "달력을 좌우로 쓸어 이전·다음 달로 바로 넘기기 (위쪽 화살표는 그대로)",
            "일정 없는 날에 '이 날 일정 추가' 버튼이 떠 바로 그 날짜로 등록",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "채팅 답변에서 자주 쓰는 표기를 제대로 인식 — • 글머리표를 목록으로, ━━━/─── 구분선을 가로줄로, ①②③ 동그라미 숫자를 번호 목록으로(이전엔 밋밋한 문단으로만 보이던 것)",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "달력 월 보기 고정 — 일정이 많은 날을 눌러도 위쪽 달력이 사라지지 않고, 아래 일정 목록만 따로 스크롤",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "달력 월 보기 정리 — 짧은 일정은 점, 종일·여러 날 일정은 띠로 한눈에 구분",
            "일정 추가가 더 간결하게 — 하루 일정은 날짜 하나만, '여러 날'을 켜면 종료 날짜 입력",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "채팅 답변 속 맨 URL을 자동으로 링크 — 주소를 그냥 적어도 탭하면 바로 열림",
            "별표 곱셈 오인 수정 — \"3 * 4 * 5\" 같은 표현이 기울임꼴로 잘못 변하지 않음",
            "체크리스트(✓)와 표 안 줄바꿈(<br>) 표시 개선",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "여러 날에 걸친 일정 추가 — 출장·전시·워크숍처럼 며칠짜리 일정의 시작·종료 날짜를 따로 지정",
            "월 보기에서 여러 날 일정을 걸친 모든 날에 이어진 막대로 표시 (주가 바뀌어도 연결), 어느 날을 눌러도 그 날에 걸친 일정이 목록에 표시",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "채팅 답변 줄 간격을 메신저(텔레그램·카톡) 수준으로 더 촘촘하게 — 한 화면에 더 많이 담기되 읽기 흐름은 유지",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "채팅 답변 가독성 개선 — 본문 줄 간격과 문단 사이를 넉넉하게, 글자 크기는 한 단계 정돈해 긴 한국어 답변이 한눈에 읽히도록",
            "내가 보낸 말풍선을 또렷한 색으로 구분 (특히 밝은 테마에서 잘 보이도록)",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "긴 마크다운 메시지를 스크롤 진입 전에 미리 측정해 스크롤을 더 부드럽게 (R8 릴리스 최적화)",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "긴 대화를 빠르게 스크롤할 때 이미지·서식이 매번 다시 그려지지 않도록 캐시 — 더 부드러운 스크롤",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "유지보수 및 안정화 빌드 — 더 이상 쓰지 않는 텔레그램 잔재 정리",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "우측 세션 드로어에서 크론·시스템뿐 아니라 모든 자동(기계) 세션을 한 그룹으로 접어 목록을 깔끔하게",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "마크다운 본문 파싱 결과를 스크롤 중에도 캐시 — 순수 스크롤이 더 매끄럽게",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "응답 스트리밍을 생동감보다 부드러움 쪽으로 미세 조정 — 화면 떨림 감소",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "스트리밍 토큰 갱신을 초당 약 30회로 묶어 답변이 흐를 때 화면 부하를 낮춤",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "답변이 흐르는 동안 변하지 않은 이전 메시지는 다시 그리지 않도록 최적화 — 더 가벼운 스트리밍",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "화면이 지원하면 가장 빠른 표시 모드(120Hz)를 요청해 스크롤을 더 부드럽게",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "업무 피드를 아이콘만 있는 간결한 빠른 동작으로 정돈",
            "스트리밍 중 마크다운 재파싱을 약 16fps로 제한해 답변이 흐를 때 더 가볍게",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "채팅 마크다운 렌더링 대폭 개선 — 코드 구문 강조, 체크박스형 작업 목록, 코드 블록 복사 확인, 취소선·둥근 이미지",
            "메일 분석 상세를 표 포함 완전한 마크다운으로 표시",
            "스트리밍 스크롤·커서·간격 다듬기, 홈 위젯에 최근 메일 표시",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "유지보수 빌드 — 2.9.37 내용을 그대로 재배포(인앱 업데이트 정상화)",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "인앱 업데이트가 앱 안에서 바로 내려받고 설치까지 — 브라우저로 빠져나가 직접 찾을 필요 없이",
            "최근 일기 타임라인 — '요즘 내 주변에 무슨 일이 있었나'를 최신순으로 한눈에",
            "예약·시스템·부팅 세션을 키에서 이름을 뽑아 표시(예: '예약 · 메일 분석')하고 드로어에서 한 그룹으로 접기",
            "업무 피드 시트 정리 — 상대 시간(방금/N분 전), 동작별 아이콘, 닫기 버튼; 내용 없는 능동 보고는 걸러냄",
            "설정에서 클라이언트 토큰을 가리고 보기/숨기기 토글 추가",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "유지보수 빌드 — 본문 글자 크기 조정 반영 및 인앱 업데이트 정상화",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "업무 피드를 상단 알림 종 아이콘 뒤로 — 안 읽은 개수 배지를 누르면 시트로 열려 채팅 영역을 가리지 않음",
            "채팅 본문 글자 크기·줄 간격을 살짝 줄여 더 차분하고 조밀한 읽기 화면",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "설정 탭을 좌우로 밀어서 전환",
            "은퇴한 토픽이 세션 드로어에 남던 문제 수정",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "업무 피드(액션 인박스) — 처리할 일을 앱에서 모아 보고 바로 후속·완료",
            "설정에서 OpenAI 호환 모델(로컬 vLLM·LM Studio 등)을 직접 추가·삭제",
            "메일 시각을 기기 현지 시간대로 표시, 인앱 업데이트를 게이트웨이를 통해 받도록 정리",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "우측 세션 드로어 열기 스와이프를 화면 오른쪽 절반(가운데~오른쪽)에서 왼쪽으로 밀면 되도록 넓힘",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "우측 세션 드로어 열기 제스처를 화면 오른쪽 끝 '살짝 안쪽'에서 왼쪽으로 미는 방식으로 — 안드로이드 뒤로가기 제스처가 맨 끝을 먹어서 안 되던 문제 우회",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "모닝레터·개별 메일분석 등 능동형 리포트가 네이티브 앱에 제대로 도착 — 앱을 켜면 업무 홈으로 바로 들어가고, 켜둔 상태에서도 새 리포트가 실시간 반영(다른 대화를 보고 있으면 '새 업무 리포트' 배너로 안내)",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "우측 세션 드로어를 화면 오른쪽 끝에서 왼쪽으로 밀어서 열기 — 중첩 드로어 제스처 충돌로 안 먹던 문제 수정",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "안 쓰는 화상회의(Meet) 참가 버튼과 일정 목록의 Meet 배지 제거",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "일정 상세 화면을 새 타이포그래픽 디자인으로 정돈 — 큰 제목·섹션 라벨·깔끔한 정보 행 (다른 화면도 순차 적용 예정)",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "상세 화면(메일·크론·사람·일정·문서·카테고리)의 로딩 실패에 '다시 시도' 버튼 — 한 번 실패해도 빠져나갈 필요 없이 바로 재시도",
            "조용히 실패하던 동작 교정 — 크론 삭제 확인 + 실패 알림, 메일 보관·삭제·모델 전환 실패 표시",
            "접근성 — 알림 캡처 체크박스·설정 탭·삭제 버튼에 스크린리더 라벨과 더 큰 터치 영역, 본문 마크다운의 들여쓴/번호 목록 표시 수정",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "메일·일정·사람·검색 화면의 로딩·오류·빈 상태 정리 — 스켈레톤 로딩, 실패 시 '다시 시도' 버튼, 내용이 없을 땐 안내 문구",
            "메일을 읽음·보관·삭제하면 목록에서 부드럽게 사라지고, 탭·길게누르기 햅틱을 통일",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "메일 분석 리포트에서 모델의 추론 과정이 그대로 노출되던 문제 수정",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "모델 아이콘 추가 — Gemma 전용 마크와 MiniMax 실제 로고를 모델 전환기에 표시",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "모델 아이콘 추가 — Qwen·StepFun·Xiaomi MiMo 브랜드 마크가 모델 전환기에 표시",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "설정 모델 탭에 응답 상태 색상 점 — 초록=응답 가능, 빨강=응답 없음, 노랑=미확인 (채움=현재 선택)",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "모델 전환기에 모델별 실제 브랜드 아이콘(흑백) — Claude·GPT·Gemini·Kimi·DeepSeek 등을 한눈에 구분",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "토픽 전환을 오른쪽 드로어로 — 상단바 해시태그(#)를 누르면 업무·잡담·코딩을 한눈에 보고 고를 수 있어요",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "알림 주입 방식 선택 — 도착 즉시 자동 주입(기본)과 탭해서 보내는 수동 주입을 설정에서 전환",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "캡처한 알림(카톡·메일 등)을 즉시 처리 — 60초 폴링 대기 없이 바로 트리아지",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "능동 알림을 탭하면 보고가 있는 업무 토픽으로 바로 이동",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "알림 캡처 — 설정에서 받을 앱을 직접 고를 수 있어요(비우면 전체)",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "능동 알림 — 모닝레터·메일분석을 게이트웨이가 만든 즉시 푸시(주기 대기 없이)",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "버그 수정 — 답변을 위키에 기록할 때 스트리밍된 본문이 사라지던 문제",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "재생성(regen) 버튼 수정 — 마지막 답변을 다시 생성하도록 동작",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "모닝레터·메일분석이 업무 토픽에도 표시 (텔레그램과 함께)",
            "좌측 드로어를 미니앱식 타이포 메뉴로 정리",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "접근성 — 입력바 아이콘(보내기·중지·첨부)에 TalkBack 라벨",
            "설정 탭 목록(사람·크론·토픽문서)도 부드럽게 등장하는 모션",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "유지보수 빌드 — 최신 변경 반영 및 안정화",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "답변이 생성될 때 깜빡이는 타이핑 커서 — 스트리밍이 한눈에",
            "드로어·목록·일정·검색 탭에 햅틱 — 손끝 피드백 통일",
            "사람 목록 항목이 부드럽게 나타나는 모션",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "UI 폴리싱 — 브랜드 블루 컬러 일관화, Pretendard 한글 자간 정리",
            "불러올 때 스켈레톤(시머) 표시 — 빈 화면 대신 부드럽게 채워짐",
            "전송·탭에 햅틱, 따뜻해진 오류 카드, 시간대 인사",
            "브랜드 테두리를 잔잔한 오로라 스윕으로 통일",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "음성 캡처 앱 단축키 — 홈 화면 단축키로 바로 말해서 Deneb에 받아쓰기",
            "토픽 전환 버튼(업무·잡담·코딩)과 좌측 내비게이션 드로어",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "이미지 캡처 — 사진·스크린샷을 Deneb에 공유하면 게이트웨이가 OCR로 텍스트를 읽어 처리",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "채팅 응답 토큰 단위 스트리밍 — 답변이 실시간으로 흘러나옴",
            "알림 캡처 탭 — 다른 앱 알림을 읽어와 탭으로 분류·처리",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "공유 시트 캡처 — 다른 앱에서 텍스트를 공유하면 바로 Deneb 채팅으로",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "역할별 모델 선택 — 메인·경량·폴백 모델을 각각 지정",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "크론 상세 화면 — 일정·지시·배달·상태 확인, 활성화·실행·삭제",
        ),
    ),
    DenebPatchNote(
        highlights = listOf(
            "캘린더 심화 + 위키 페이지 메타데이터 편집",
        ),
    ),
)
