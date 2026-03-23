"""Section classification and tag extraction."""

import re


# ──────────────────────────────────────────────
# 4. 섹션 유형 분류
# ──────────────────────────────────────────────

def classify_section(heading, content):
    h = (heading or "").lower()
    c = (content or "").lower()

    if any(k in h for k in ['현재 상황', '현재상황', '개요', '프로젝트 개요']):
        return 'status'
    if any(k in h for k in ['다음 예상', '액션']):
        return 'next_action'
    if any(k in h for k in ['이력']):
        return 'history'
    if any(k in h for k in ['로그 20']):
        return 'comm_log'
    if any(k in h for k in ['기술', '사양', '사업 기본']):
        return 'technical'
    if any(k in h for k in ['이슈', '리스크', '문제', '화재']):
        return 'issue'
    if any(k in h for k in ['일정', '마일스톤', '공정']):
        return 'schedule'
    if any(k in h for k in ['경제성', '투자비', '공사비', '운영비', '재무']):
        return 'financial'
    if any(k in h for k in ['인허가', '규제']):
        return 'permit'
    if any(k in h for k in ['결론', '요약', '종합']):
        return 'summary'
    if any(k in h for k in ['관련 메일', '메일']):
        return 'comm_log'
    if any(k in h for k in ['첨부', '자료']):
        return 'attachment'

    # 본문 기반
    if any(k in c for k in ['견적', '계약서', '공사도급']):
        return 'contract'
    if any(k in c for k in ['화재', '소손', '클레임']):
        return 'issue'

    return 'other'


# ──────────────────────────────────────────────
# 5. 태그 추출 (태양광 EPC 도메인)
# ──────────────────────────────────────────────

def extract_tags(meta, sections):
    tags = set()

    # 메타 기반
    if meta.get('client'):
        tags.add(f"고객:{meta['client']}")
    if meta.get('person_internal'):
        for p in re.split(r'[,/·]', meta['person_internal']):
            p = p.strip()
            if p:
                tags.add(f"담당:{p}")
    if meta.get('status'):
        tags.add(f"상태:{meta['status']}")

    all_text = ' '.join(body for _, body, _ in sections).lower()
    all_text += ' ' + (meta.get('name', '') or '').lower()
    all_text += ' ' + (meta.get('biz_type', '') or '').lower()

    # 테이블 메타 _ 접두어 필드 → 태그 (v1.34)
    for key, val in meta.items():
        if key.startswith('_') and val:
            tag_name = key.lstrip('_')
            tags.add(f"기술:{tag_name}")

    # 태양광 EPC 도메인 키워드
    tech_kw = {
        'EPC': ['epc', '시공'],
        'O&M': ['o&m', '운영관리', '유지관리', '유지보수'],
        'PPA': ['ppa', '직접전력거래', '전력거래'],
        '설비리스': ['설비리스', '리스사업', '임대차'],
        'ESS': ['ess', 'bess', '에너지저장'],
        '해저케이블': ['해저케이블', 'submarine cable', '154kv'],
        '모듈': ['모듈', 'module', '진코', 'jinko', 'ja solar', '트리나', '한화'],
        '인버터': ['인버터', 'inverter', '화웨이', 'huawei', 'pcs'],
        '구조검토': ['구조검토', '구조계산', '구조물'],
        'TPO방수': ['tpo', '방수', '현대l&c'],
        '환경공단': ['환경공단', '탄소중립', '감축설비', '지원사업'],
        'PF금융': ['pf', '팩토링', '대출', '금융조건', '펀드'],
        'CU헷징': ['헷징', 'hedging', 'lme'],
        'MC4화재': ['mc4', '커넥터 화재', '소손'],
        '해상풍력': ['해상풍력', '풍력', '풍황'],
        '수상태양광': ['수상태양광', '수상'],
        '접속단': ['접속단', '계통연계', 'kepco', '한전'],
        'REC': ['rec', 'rec 가중치', 'rps'],
        'SMP': ['smp'],
    }
    for tag, keywords in tech_kw.items():
        if any(kw in all_text for kw in keywords):
            tags.add(f"기술:{tag}")

    # 프로젝트 유형
    type_kw = {
        '지붕태양광': ['지붕', '루프탑', 'rooftop'],
        '주차장태양광': ['주차장', '캐노피'],
        '수상태양광': ['수상', '석문호'],
        '지상태양광': ['토지', '지상'],
        '해상풍력': ['해상풍력'],
        'BESS': ['bess', 'ess 발전'],
    }
    for tag, keywords in type_kw.items():
        if any(kw in all_text for kw in keywords):
            tags.add(f"유형:{tag}")

    # 현대차그룹 관련
    hyundai = ['현대', '기아', '모비스', '글로비스', '위아']
    if any(kw in all_text for kw in hyundai):
        tags.add("그룹:현대차")

    return tags
