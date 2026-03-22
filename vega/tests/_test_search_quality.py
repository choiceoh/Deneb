#!/usr/bin/env python3
"""검색 품질 테스트 — 다양한 쿼리로 실제 검색 결과 검증"""
import sys, os, json, tempfile, shutil
from pathlib import Path

SELF_DIR = Path(__file__).parent
sys.path.insert(0, str(SELF_DIR))

# --- Fixture DB 생성 (test_vega.py와 동일) ---
import config

_TMP = tempfile.mkdtemp(prefix='vega_sq_')
_MD = os.path.join(_TMP, 'projects')
os.makedirs(_MD, exist_ok=True)
os.makedirs(os.path.join(_MD, '거래처'), exist_ok=True)

FIXTURES = {
    '비금도.md': """# 비금도 해상태양광

| 항목 | 내용 |
|------|------|
| **상태** | 시공 중 🟢 |
| **발주처** | 한국전력 |
| **사내 담당** | 고건 팀장 |
| **거래처 담당** | Christina Gu (ZTT) |
| **용량** | 100MW |
| **형태** | 해상태양광 |
| **EPC 계약금액** | 1200억원 |

## 현재 상황
- 해저케이블 154kV 포설 진행 중
- ZTT 케이블 납기 5월 12일 확정
- 모듈 설치 70% 완료

## 다음 예상 액션
- 2026-03-25: ZTT 케이블 선적 확인
- 2026-04-01: 해저 포설 공사 착수

## 이력
- 2026-03-15: CU 헷징 계약 체결
- 2026-03-10: 154kV 해저케이블 공장 검수 완료
- 2026-02-28: 주민 설명회 완료

## 2026-03-19
- **RE: 비금도 케이블 최종 납기 확인** (Christina Gu)
  - 최종 ETD 5월 12일 목표, 선적 서류 준비 중
  - 품질 인증서 다음 주 발송 예정

## 2026-03-15
- **CU 헷징 계약 관련** (신한은행 김대희)
  - 헷징 비율 80% 확정, 잔여분 4월 재검토
""",
    '화성산단.md': """# 화성산단 태양광

| 항목 | 내용 |
|------|------|
| **상태** | 검토 중 🟡 |
| **발주처** | 현대엔지니어링 |
| **사내 담당** | 고건 팀장 |
| **거래처 담당** | 김서현 (현대위아) |
| **용량** | 50MW |
| **형태** | 육상태양광 |
| **EPC 계약금액** | 450억원 |

## 현재 상황
- 기본설계 검토 단계
- 현대위아 지붕 구조 검토 요청
- 경량모듈 적용 여부 결정 필요

## 다음 예상 액션
- 2026-03-28: 지붕 하중 검토 보고서 제출
- 2026-04-05: 모듈 선정 회의

## 이력
- 2026-03-12: 현장 실사 완료
- 2026-02-20: LOI 수령

## 2026-03-18
- **화성산단 지붕태양광 구조검토** (김서현)
  - TPO 브라켓 적용 시 하중 문제 검토 필요
  - 경량모듈 적용하면 해결 가능성 있음
""",
    '제주풍력ESS.md': """# 제주 풍력 ESS

| 항목 | 내용 |
|------|------|
| **상태** | 긴급 대응 중 🔴 |
| **발주처** | 제주에너지공사 |
| **사내 담당** | 이경렬 과장 |
| **거래처 담당** | Alan Zhang (Peak Energy) |
| **용량** | 30MW/60MWh |
| **형태** | ESS |

## 현재 상황
- ESS 배터리 화재 안전 인증 지연
- Peak Energy 납기 2주 지연 통보
- 제주도 환경영향평가 추가 요구

## 다음 예상 액션
- 2026-03-22: 화재 안전 인증 재신청
- 2026-03-30: 환경영향평가 보완 서류 제출

## 이력
- 2026-03-18: Peak Energy 납기 지연 통보
- 2026-03-10: ESS 화재 안전 기준 강화 공지
- 2026-02-15: 환경영향평가 1차 통과

## 2026-03-18
- **RE: ESS Battery Delivery Delay** (Alan Zhang)
  - UL9540A 인증 지연으로 배터리 출하 2주 밀림
  - 대안: 삼성SDI 배터리로 교체 검토 가능
""",
    '거래처/ZTT.md': """# ZTT (중천과기)

## 담당자
- Christina Gu (영업, christina.gu@ztt.com)
- Jay Yu (기술지원)

## 관련 프로젝트
- 비금도 해상태양광 — 154kV 해저케이블
- 석문호 — 22.9kV 솔라케이블

## 최근 이슈
- 2026-03: 비금도 케이블 납기 5월 확정
- 2026-02: 석문호 케이블 공장 출하 완료
""",
}

for fname, content in FIXTURES.items():
    fpath = os.path.join(_MD, fname)
    with open(fpath, 'w', encoding='utf-8') as f:
        f.write(content)

# DB 빌드
config.DB_PATH = os.path.join(_TMP, 'test.db')
config.MD_DIR = _MD
from project_db_v2 import import_files
import_files(_MD, config.DB_PATH)

# --- 검색 테스트 ---
from core import execute

QUERIES = [
    # 기본 프로젝트명 검색
    ("비금도", "비금도가 1위여야 함"),
    ("화성산단", "화성산단이 1위여야 함"),
    ("제주", "제주 풍력 ESS가 나와야 함"),
    # 자연어 질문
    ("비금도 케이블 납기 언제야", "비금도 + 케이블 납기 정보"),
    ("급한 프로젝트 뭐 있어", "urgent로 라우팅 or 제주ESS가 상위"),
    ("고건 담당 프로젝트", "비금도+화성산단 모두 나와야 함"),
    # 거래처/인물 검색
    ("Christina", "비금도 관련 결과"),
    ("ZTT", "비금도 + ZTT 거래처 정보"),
    ("Peak Energy", "제주 ESS 관련"),
    # 복합 조건
    ("케이블 납기 지연", "비금도/제주ESS 관련 — 납기 키워드"),
    ("ESS 화재", "제주 풍력 ESS가 1위"),
    ("경량모듈 적용", "화성산단이 1위"),
    # 의미 검색 패턴
    ("해저케이블 설계 어떻게", "비금도 관련 — 의미검색"),
    ("배터리 안전 인증 문제", "제주 ESS 관련"),
    # 한국어 조사 포함
    ("비금도에서 케이블은", "비금도가 매칭되어야 함"),
    ("화성산단의 현황", "화성산단 매칭"),
    # 엣지케이스
    ("", "빈 쿼리 — 에러 없이 처리"),
    ("ㅎㅎ", "무의미 입력 — 결과 0건 OK"),
    ("154kV", "특수문자 포함 — 비금도 매칭"),
    ("1200억", "금액 검색 — 비금도 매칭"),
    ("UL9540A", "기술 규격 검색 — 제주ESS"),
]

print("=" * 80)
print("검색 품질 테스트 결과")
print("=" * 80)

issues = []
for i, (query, expected) in enumerate(QUERIES, 1):
    r = execute('search', {'query': query, 'sub_args': query.split() if query else []})
    data = r.get('data', {})
    status = r.get('status', 'error')
    projects = data.get('projects', [])
    comms = data.get('communications', [])
    rc = data.get('result_count', {})
    matched_kw = data.get('matched_keywords', [])
    meta = data.get('search_meta', {})
    
    top_names = [p['name'] for p in projects[:3]]
    
    # 문제 감지
    problem = None
    if query == "":
        if status == 'error':
            problem = "빈 쿼리에서 에러 발생"
    elif query == "ㅎㅎ":
        pass  # 0건 OK
    elif "비금도" in query and projects and "비금도" not in projects[0].get('name', ''):
        problem = f"비금도 쿼리인데 1위가 {projects[0].get('name', '?')}"
    elif "화성산단" in query and projects and "화성산단" not in projects[0].get('name', ''):
        problem = f"화성산단 쿼리인데 1위가 {projects[0].get('name', '?')}"
    elif "급한" in query and not projects:
        problem = "급한 프로젝트 쿼리인데 결과 0건"
    elif "ESS" in query and "제주" not in str(top_names):
        problem = f"ESS 쿼리인데 제주 없음: {top_names}"
    elif "해저케이블" in query and projects and "비금도" not in projects[0].get('name', ''):
        problem = f"해저케이블 쿼리인데 1위가 {projects[0].get('name', '?')} (비금도여야 함)"
    elif "케이블" in query.lower() and not projects:
        problem = "케이블 쿼리인데 결과 0건"
    elif "고건" in query and len(projects) < 2:
        problem = f"고건 담당 2개 프로젝트인데 {len(projects)}개만 반환"
    elif "Christina" in query and not projects:
        problem = "Christina 검색 결과 0건"
    elif "ZTT" in query and not projects:
        problem = "ZTT 검색 결과 0건"
    elif "Peak" in query and not projects:
        problem = "Peak Energy 검색 결과 0건"
    elif "154kV" in query and not projects:
        problem = "154kV 검색 결과 0건"
    elif "1200억" in query and not projects:
        problem = "1200억 검색 결과 0건"
    elif "UL9540A" in query and not projects:
        problem = "UL9540A 검색 결과 0건"
    elif "경량모듈" in query and projects and "화성산단" not in projects[0].get('name', ''):
        problem = f"경량모듈인데 1위가 {projects[0].get('name', '?')}"
    elif "화재" in query and projects and "제주" not in projects[0].get('name', ''):
        problem = f"ESS 화재인데 1위가 {projects[0].get('name', '?')}"
    elif ("에서" in query or "의 " in query or query.endswith("은")) and projects and not matched_kw:
        problem = "조사 포함 쿼리인데 matched_keywords가 빈 배열"
    
    flag = "FAIL" if problem else "OK"
    if problem:
        issues.append((i, query, problem))
    
    print(f"\n[{i:2d}] [{flag:4s}] q=\"{query}\"")
    print(f"     기대: {expected}")
    print(f"     결과: {len(projects)}개 프로젝트, {len(comms)}건 커뮤, route={meta.get('route','?')}")
    print(f"     상위: {top_names}")
    print(f"     키워드: {matched_kw}")
    if problem:
        print(f"     *** 문제: {problem}")

print("\n" + "=" * 80)
print(f"결과: {len(QUERIES)}건 중 {len(issues)}건 실패")
for idx, q, prob in issues:
    print(f"  [{idx}] \"{q}\" → {prob}")

# Cleanup
shutil.rmtree(_TMP, ignore_errors=True)
