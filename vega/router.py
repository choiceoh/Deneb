"""
Vega 통합 검색 라우터 (내부 라이브러리)

SQLite(구조화 검색)와 QMD(의미 검색)를 자동으로 라우팅합니다.
Vega 통합 결과 형식으로 반환합니다.
"""

import sqlite3
import re
import os
import json
import logging
from pathlib import Path

_log = logging.getLogger(__name__)

# 검색 결과 제한 상수
_CHUNK_LIMIT = 50       # FTS/trigram 청크 결과 최대 수
_LIKE_LIMIT = 30        # LIKE 폴백 결과 최대 수
_COMM_LIMIT = 15        # 커뮤니케이션 로그 최대 수

from config import DB_PATH, QMD_WRAPPER, QMD_BIN, get_db_connection, RERANK_MODE


# ──────────────────────────────────────────────
# 1. 쿼리 분석기
# ──────────────────────────────────────────────

# 정적 패턴 (폴백 — DB 미연결 시 사용)
_STATIC_PATTERNS = {
    'client': [
        r'(금호타이어|기아|현대[가-힣]*|대한전선|롯데[가-힣]*|무림[가-힣]*|비금도|석문호|'
        r'썬탑|양명|옹진|인하|하이트|글로비스|위아|화성산단|한화[가-힣]*|쿠팡|카카오|'
        r'ZTT|jinko|진코)',
    ],
    'person': [
        r'(김대희|고건|이시연|박민수|강민수|김유영|임은진|박종원|제용범|조은실|'
        r'백종태|강동민|이영민|김세미|장현정|Sara)',
        r'(누가|담당자|담당)',
    ],
    'status': [
        r'(진행중|진행\s?중|완료|준공|설계|시공|계약|검토|대기|마무리|긴급|급한|위급)',
        r'(상태가|현재\s?상황|현황)',
    ],
    'tag': [
        r'(현대차\s?그룹|현대차그룹)',
        r'(환경공단|탄소중립|지원사업)',
        r'(EPC|O&M|PPA|설비리스|PF|팩토링)',
    ],
}

# DB 기반 동적 패턴 캐시 (v1.34: TTL 추가)
import time as _time
_pattern_cache = {'patterns': None, 'mtime': 0, 'ts': 0}
_PATTERN_CACHE_TTL = 60  # 초 — mtime 같아도 60초마다 재확인

def _build_dynamic_patterns(db_path=DB_PATH):
    """DB에서 프로젝트명/고객사/담당자를 읽어 패턴을 동적 생성"""
    import copy
    patterns = copy.deepcopy(_STATIC_PATTERNS)

    if not os.path.isfile(db_path):
        return patterns

    try:
        conn = get_db_connection(db_path)
        try:
            rows = conn.execute("SELECT name, client, person_internal, person_external FROM projects").fetchall()
        finally:
            conn.close()
    except Exception:
        return patterns

    # 프로젝트명 → client 패턴에 추가
    names = set()
    clients = set()
    persons = set()

    for r in rows:
        # 프로젝트명에서 2글자 이상 키워드 추출
        if r[0]:
            for word in re.split(r'[\s/·,]+', r[0]):
                word = word.strip()
                if len(word) >= 2:
                    names.add(re.escape(word))
        # 고객사
        if r[1]:
            for word in re.split(r'[\s/·,()]+', r[1]):
                word = word.strip()
                if len(word) >= 2:
                    clients.add(re.escape(word))
        # 담당자 (내부 + 외부)
        for field in (r[2], r[3]):
            if field:
                for name in re.split(r'[\s/·,]+', field):
                    name = re.sub(r'(팀장|대리|과장|부장|사원|매니저|이사|차장|주임|책임)', '', name).strip()
                    if 2 <= len(name) <= 5 and re.match(r'^[가-힣A-Za-z]+$', name):
                        persons.add(re.escape(name))

    # 동적 패턴 추가 (기존 정적 패턴 뒤에)
    if names | clients:
        dynamic_client = '(' + '|'.join(sorted(names | clients, key=len, reverse=True)) + ')'
        patterns['client'].append(dynamic_client)
    if persons:
        dynamic_person = '(' + '|'.join(sorted(persons, key=len, reverse=True)) + ')'
        patterns['person'].append(dynamic_person)

    return patterns

def _get_structural_patterns(db_path=DB_PATH):
    """캐시된 동적 패턴 반환. DB 변경 시 자동 갱신."""
    try:
        mtime = os.path.getmtime(db_path) if os.path.isfile(db_path) else 0
    except OSError:
        mtime = 0

    now = _time.time()
    cache_expired = (now - _pattern_cache['ts']) > _PATTERN_CACHE_TTL
    if _pattern_cache['patterns'] is None or mtime > _pattern_cache['mtime'] or cache_expired:
        _pattern_cache['patterns'] = _build_dynamic_patterns(db_path)
        _pattern_cache['mtime'] = mtime
        _pattern_cache['ts'] = now

    return _pattern_cache['patterns']

STRUCTURAL_PATTERNS = _STATIC_PATTERNS  # 폴백 (모듈 레벨 참조 호환)

# 의미/내용 검색 패턴 (QMD가 잘하는 것)
# v1.332: 대폭 확장 — 사건/상황/인과/경과 패턴 추가로 QMD 활용도 향상
SEMANTIC_PATTERNS = [
    # 질문형 (기존)
    r'(어떻게|왜|방법|이유|원인|차이|비교)',
    r'(관련\s?내용|자세히|설명|배경)',
    # 기술/공법
    r'(기술적|공법|방식|구조|설계|사양|스펙)',
    # 리스크/문제
    r'(리스크|위험|문제점|이슈|해결|대응|대책|조치)',
    # 전략/분석
    r'(전략|방향|검토|분석|판단|의견|평가)',
    # 사건/사고 (v1.332 신규)
    r'(화재|사고|피해|파손|고장|결함|하자|민원|분쟁)',
    # 변경/교체 (v1.332 신규)
    r'(교체|변경|수정|보수|보강|개선|철거|재시공)',
    # 상황/경과 (v1.332 신규)
    r'(경위|경과|과정|경험|사례|전말|추이)',
    # 조건/제약 (v1.332 신규)
    r'(조건|제약|규제|인허가|환경영향|주민\s?반대|민원)',
    # 일정/지연 (v1.332 신규)
    r'(지연|납기|딜레이|공기|일정\s?차질|늦어|밀려)',
    # 시간/기간 (v1.34 신규)
    r'(지난달|지난주|다음주|다음달|어제|금주|금월|요번주|최근\s?\d)',
    # 문서/산출물 (v1.34 신규)
    r'(문서|계약서|서류|도면|인증서|보고서|시방서|견적)',
    # 이해관계자/소통 (v1.34 신규)
    r'(발주처|고객|클라이언트|협력사|외주|하도급)',
    r'(의견|회의|토의|논의|합의|피드백|회신|답변)',
]

_QUERY_STOPWORDS = {
    # 순수 필러만 제거 — 실무 의도 표현은 보존
    '프로젝트', '검색', '찾아', '찾아줘', '보여', '보여줘', '알려', '알려줘',
    '문의', '내용', '알아봐', '알아봐줘', '인가', '인가요', '정리',
    '대해', '대해서', '관해', '관해서', '좀', '그', '뭐', '뭐가',
    '어떤', '무슨', '몇', '개',
    # 주의: 아래는 의도적으로 제외 (v1.34 — 실무 의도 표현이므로 보존)
    # '담당', '담당자' → person 라우팅에 활용
    # '진행', '진행중', '설계중', '시공중', '검토중' → 상태 검색에 활용
    # '상태', '상황', '현재', '이번' → 맥락 키워드
    # '관련' → "관련 프로젝트" 등에서 유용
    # '어떻게' → semantic 패턴으로 처리
    # '언제', '언제야' → 시간 의도 표현
}

_TRAILING_PARTICLES = re.compile(
    # 주의: '도'를 단독으로 넣으면 비금도/진도/완도 같은 지명이 잘림
    # '도'는 복합조사(에서도, 까지도, 만도, 라도, 이라도)로만 처리
    r'(은|는|이|가|을|를|의|에|에서|으로|로|와|과|만|까지|부터|에게|한테|께|처럼|같이|에서도|까지도|만도|부터도|라도|이라도|라고|이라고)$'
)
_TRAILING_ENDINGS = re.compile(
    r'(하는지|했는지|되는지|해줘|해줘요|해주세요|해라|한다|하는|하기|하다|했던|되고|되는|되어|됐다|된|중인|있던|있는|있고|있어|있음|이야|야|인가요|인가|인지|임)$'
)


def _normalize_query(query):
    """쿼리 전처리: 의미없는 접미사/조사/문장부호 제거."""
    q = (query or '').strip()
    if not q:
        return q
    # 의미없는 접미 표현 제거 (문장 끝)
    q = re.sub(r'(해줘|알려줘|보여줘|뭐야|좀|요)\s*$', '', q).strip()
    # 물음표/마침표/느낌표 제거
    q = re.sub(r'[?？！!.。]+$', '', q).strip()
    return q


def _dedupe_keep_order(items):
    seen = set()
    out = []
    for item in items:
        if item and item not in seen:
            seen.add(item)
            out.append(item)
    return out


def _normalize_keyword(term):
    if not term:
        return ''
    term = re.sub(r'^[^가-힣A-Za-z0-9]+|[^가-힣A-Za-z0-9]+$', '', term.strip())
    if not term:
        return ''
    prev = None
    while prev != term:
        prev = term
        term = _TRAILING_ENDINGS.sub('', term)
        term = _TRAILING_PARTICLES.sub('', term)
    return term.strip()


def _extract_keywords(query, structured_terms):
    structured_lower = {t.lower() for t in structured_terms if t}
    keywords = []
    for raw in re.findall(r'[가-힣A-Za-z0-9&+/.-]+', query):
        if raw.lower() in structured_lower:
            continue
        normalized = _normalize_keyword(raw)
        if len(normalized) <= 1:
            continue
        if normalized.lower() in structured_lower:
            continue
        if normalized in _QUERY_STOPWORDS:
            continue
        keywords.append(normalized)
    return _dedupe_keep_order(keywords)


def _is_strong_term(term):
    if not term:
        return False
    if re.search(r'[A-Za-z0-9]', term):
        return True
    return len(term) >= 3


def _build_fts_queries(terms):
    safe_terms = [_sanitize_fts_single(t) for t in terms]
    safe_terms = [t for t in safe_terms if t]
    if not safe_terms:
        return None, None
    strong_terms = [t for t in safe_terms if _is_strong_term(t.strip('"'))]
    if len(strong_terms) >= 2:
        strict = ' AND '.join(strong_terms[:4])
    elif safe_terms:
        strict = safe_terms[0]
    else:
        strict = None
    broad = ' OR '.join(safe_terms)
    return strict, broad


def analyze_query(query):
    """
    쿼리를 분석하여 라우팅 결정을 내립니다.
    
    Returns:
        {
            'route': 'sqlite' | 'qmd' | 'hybrid',
            'confidence': float,
            'extracted': {
                'clients': [...],
                'persons': [...],
                'statuses': [...],
                'tags': [...],
                'keywords': [...],  # QMD용 키워드
            },
            'reason': str
        }
    """
    query_lower = query.lower()
    extracted = {
        'clients': [],
        'persons': [],
        'statuses': [],
        'tags': [],
        'keywords': [],
    }

    structural_score = 0
    semantic_score = 0

    # 구조화 필드 매칭 (DB 기반 동적 패턴 사용)
    for field, patterns in _get_structural_patterns().items():
        for pattern in patterns:
            matches = re.findall(pattern, query, re.IGNORECASE)
            if matches:
                structural_score += len(matches) * 2
                if field == 'client':
                    extracted['clients'].extend(matches)
                elif field == 'person':
                    extracted['persons'].extend([m for m in matches if m not in ('누가', '담당자', '담당')])
                elif field == 'status':
                    extracted['statuses'].extend([m for m in matches if m not in ('상태가', '현재상황', '현재 상황', '현황')])
                elif field == 'tag':
                    extracted['tags'].extend(matches)

    # 의미 패턴 매칭
    for pattern in SEMANTIC_PATTERNS:
        if re.search(pattern, query_lower):
            semantic_score += 2

    # 순수 키워드 (양쪽 다 사용)
    # 구조화 필드에 매칭되지 않은 나머지를 QMD/FTS 키워드로
    all_structural = []
    for vals in extracted.values():
        all_structural.extend(vals)

    extracted['keywords'] = _extract_keywords(query, all_structural)

    # 라우팅 결정 (v1.332: QMD 활용도 향상)
    total = structural_score + semantic_score
    has_keywords = bool(extracted.get('keywords'))

    if total == 0:
        if has_keywords:
            # 키워드는 있지만 패턴 매칭 없음 → hybrid로 QMD도 활용
            route = 'hybrid'
            confidence = 0.6
            reason = "키워드 감지 → SQLite + QMD 병행 검색"
        else:
            route = 'sqlite'
            confidence = 0.5
            reason = "특정 패턴 없음 → SQLite 전문검색으로 처리"
    elif structural_score > 0 and semantic_score > 0:
        route = 'hybrid'
        confidence = 0.8
        reason = f"구조화({structural_score}) + 의미({semantic_score}) → 혼합 검색"
    elif structural_score > 0 and semantic_score == 0:
        if has_keywords:
            # 구조 필드 + 비구조 키워드 → hybrid (QMD가 키워드 맥락 보충)
            route = 'hybrid'
            confidence = 0.7
            reason = f"구조화({structural_score}) + 키워드 → 혼합 검색"
        else:
            route = 'sqlite'
            confidence = min(0.7 + structural_score * 0.05, 0.95)
            reason = f"구조화 필드 감지({structural_score}) → SQLite 우선"
    else:
        # 의미 패턴만 매칭 — 키워드가 있으면 hybrid (SQLite 구조 데이터도 활용)
        if has_keywords:
            route = 'hybrid'
            confidence = 0.75
            reason = f"의미({semantic_score}) + 키워드 → SQLite + QMD 병행"
        else:
            route = 'qmd'
            confidence = min(0.7 + semantic_score * 0.05, 0.95)
            reason = f"의미 검색 패턴({semantic_score}) → QMD 우선"

    return {
        'route': route,
        'confidence': confidence,
        'extracted': extracted,
        'reason': reason,
    }


# ──────────────────────────────────────────────
# 2. SQLite 검색 엔진
# ──────────────────────────────────────────────

# 한국어 조사 제거 (간이 형태소 처리)
_KO_JOSA = re.compile(
    r'(은|는|이|가|을|를|의|에|에서|으로|로|와|과|만|까지|부터|에게|한테|께|보다|처럼|같이|에서도|까지도|만도|부터도|라고|이라고|이란)$'
)

def _preprocess_korean(query):
    """한국어 검색어 전처리: 조사/어미 제거 (복합어 분리는 trigram FTS에 위임)"""
    if not isinstance(query, str):
        query = str(query) if query is not None else ''
    processed = []
    for raw in re.findall(r'[가-힣A-Za-z0-9&+/.-]+', query):
        cleaned = _normalize_keyword(_KO_JOSA.sub('', raw))
        if cleaned:
            processed.append(cleaned)
    return _dedupe_keep_order(processed)

# FTS5 예약어 (쿼리에 들어가면 크래시)
_FTS_RESERVED = {'AND', 'OR', 'NOT', 'NEAR'}

def _sanitize_fts_single(term):
    """단일 검색어를 FTS5 안전하게 변환"""
    if not term or not term.strip():
        return None
    t = term.strip()
    # 예약어 → 따옴표 감싸기
    if t.upper() in _FTS_RESERVED:
        return f'"{t}"'
    # FTS5 column filter (e.g., "project_name:비금도") → 따옴표 감싸기
    if ':' in t:
        return f'"{t}"'
    # 특수문자 포함 (O&M, 154kV 등) → 따옴표 감싸기
    if re.search(r'[&|!@#$%^*()\-+=\[\]{}<>?/\\~`]', t):
        return f'"{t}"'
    # 빈 문자열이나 공백만
    if not re.search(r'[가-힣a-zA-Z0-9]', t):
        return None
    return t

def _sanitize_fts(terms):
    """복수 검색어를 FTS5 OR 쿼리로 결합"""
    safe = [_sanitize_fts_single(t) for t in terms]
    safe = [s for s in safe if s]
    if not safe:
        return None
    return " OR ".join(safe)


def sqlite_search(query, extracted, db_path=DB_PATH):
    """구조화 + 전문검색 실행"""
    conn = get_db_connection(db_path, row_factory=True)
    try:
        return _sqlite_search_impl(conn, query, extracted)
    finally:
        conn.close()

def _sqlite_search_impl(conn, query, extracted):
    # --- 섹션 검색 ---
    conditions = []
    params = []
    sql = """
        SELECT DISTINCT
            c.id as chunk_id, p.id as project_id,
            p.name, p.client, p.status,
            p.person_internal, p.capacity,
            c.section_heading, c.content, c.chunk_type, c.entry_date
        FROM chunks c
        JOIN projects p ON c.project_id = p.id
    """

    # 고객사 필터
    if extracted['clients']:
        client_conds = []
        for cl in extracted['clients']:
            client_conds.append("(p.client LIKE ? OR p.name LIKE ?)")
            params.extend([f"%{cl}%", f"%{cl}%"])
        conditions.append("(" + " OR ".join(client_conds) + ")")

    # 담당자 필터
    if extracted['persons']:
        person_conds = []
        for p in extracted['persons']:
            person_conds.append("(p.person_internal LIKE ? OR c.content LIKE ?)")
            params.extend([f"%{p}%", f"%{p}%"])
        conditions.append("(" + " OR ".join(person_conds) + ")")

    # 상태 필터 (동의어 확장: "급한" → "긴급" 등)
    _STATUS_SYNONYMS = {
        '급한': ['긴급'], '위급': ['긴급'], '긴급': ['급한'],
    }
    if extracted['statuses']:
        status_conds = []
        for s in extracted['statuses']:
            synonyms = [s] + _STATUS_SYNONYMS.get(s, [])
            for syn in synonyms:
                status_conds.append("p.status LIKE ?")
                params.append(f"%{syn}%")
        conditions.append("(" + " OR ".join(status_conds) + ")")

    # 키워드 전문검색 (FTS5, strict soft-AND 후 broad OR 폴백)
    fts_terms = extracted['keywords']
    strict_fts = broad_fts = None
    if fts_terms:
        strict_fts, broad_fts = _build_fts_queries(fts_terms)
    elif not any([extracted['clients'], extracted['persons'], extracted['statuses']]):
        sanitized = _sanitize_fts_single(query)
        strict_fts = sanitized
        broad_fts = sanitized

    def _run_chunk_query(fts_query=None):
        local_sql = sql
        local_conditions = list(conditions)
        local_params = list(params)
        fts_joined = False

        if fts_query:
            local_sql += " JOIN chunks_fts fts ON fts.rowid = c.id"
            local_conditions.append("chunks_fts MATCH ?")
            local_params.append(fts_query)
            fts_joined = True
        elif fts_terms:
            for term in fts_terms:
                if term.strip():
                    local_conditions.append("c.content LIKE ?")
                    local_params.append(f"%{term}%")
        elif query.strip() and not any([extracted['clients'], extracted['persons'], extracted['statuses']]):
            local_conditions.append("c.content LIKE ?")
            local_params.append(f"%{query}%")

        if extracted['tags']:
            tag_conds = []
            for tag in extracted['tags']:
                tag_conds.append("(c.content LIKE ? OR p.name LIKE ?)")
                local_params.extend([f"%{tag}%", f"%{tag}%"])
            local_conditions.append("(" + " OR ".join(tag_conds) + ")")

        if local_conditions:
            local_sql += " WHERE " + " AND ".join(local_conditions)
        if fts_joined:
            local_sql += f""" ORDER BY
                CASE WHEN p.status LIKE '%완료%' OR p.status LIKE '%취소%' THEN 1 ELSE 0 END,
                bm25(chunks_fts, 5.0, 3.0, 2.0, 1.0),
                c.entry_date DESC NULLS LAST
                LIMIT {_CHUNK_LIMIT}"""
        else:
            local_sql += f""" ORDER BY
                CASE WHEN p.status LIKE '%완료%' OR p.status LIKE '%취소%' THEN 1 ELSE 0 END,
                c.entry_date DESC NULLS LAST, p.id DESC
                LIMIT {_CHUNK_LIMIT}"""

        try:
            return conn.execute(local_sql, local_params).fetchall()
        except Exception:
            return []

    chunk_results = _run_chunk_query(strict_fts)
    match_methods = []
    if chunk_results:
        match_methods.append('fts5_strict')
    # broad 폴백: 활성 프로젝트 기준으로 판단 (완료/취소 제외)
    _active_count = sum(1 for r in chunk_results
                        if not any(k in (_row_value(r, 'status', '') or '') for k in ('완료', '취소')))
    if _active_count < 5 and broad_fts and broad_fts != strict_fts:
        existing_ids = {r['chunk_id'] for r in chunk_results}
        before_count = len(chunk_results)
        for row in _run_chunk_query(broad_fts):
            if row['chunk_id'] not in existing_ids:
                chunk_results.append(row)
                existing_ids.add(row['chunk_id'])
        if len(chunk_results) > before_count:
            match_methods.append('fts5_broad')

    # unicode61 FTS 결과 부족 시 trigram 보충 (부분 문자열 매칭)
    if len(chunk_results) < 3 and query.strip():
        try:
            tri_sql = f"""
                SELECT DISTINCT c.id as chunk_id, p.id as project_id,
                    p.name, p.client, p.status, p.person_internal, p.capacity,
                    c.section_heading, c.content, c.chunk_type, c.entry_date
                FROM chunks c JOIN projects p ON c.project_id = p.id
                JOIN chunks_fts_trigram tri ON tri.rowid = c.id
                WHERE chunks_fts_trigram MATCH ?
                LIMIT {_CHUNK_LIMIT}
            """
            existing_ids = {r['chunk_id'] for r in chunk_results}
            before_count = len(chunk_results)
            tri_results = conn.execute(tri_sql, [f'"{query}"']).fetchall()
            chunk_results = list(chunk_results) + [r for r in tri_results if r['chunk_id'] not in existing_ids]
            if len(chunk_results) > before_count:
                match_methods.append('trigram')
        except Exception as e:
            _log.debug("trigram FTS 실패: %s", e)

    # trigram도 부족하면 LIKE 폴백
    if len(chunk_results) < 3 and query.strip():
        try:
            like_sql = """
                SELECT DISTINCT c.id as chunk_id, p.id as project_id,
                    p.name, p.client, p.status, p.person_internal, p.capacity,
                    c.section_heading, c.content, c.chunk_type, c.entry_date
                FROM chunks c JOIN projects p ON c.project_id = p.id
            """
            # 한국어 전처리된 키워드 + 원본 쿼리
            ko_terms = _preprocess_korean(query)
            all_terms = list(set([query] + ko_terms))
            like_conds = [f"c.content LIKE ?" for _ in all_terms]
            like_params = [f"%{t}%" for t in all_terms]
            existing_ids = {r['chunk_id'] for r in chunk_results}
            if existing_ids:
                like_sql += f" WHERE ({' OR '.join(like_conds)}) AND c.id NOT IN ({','.join('?' * len(existing_ids))})"
                like_params += list(existing_ids)
            else:
                like_sql += f" WHERE {' OR '.join(like_conds)}"
            like_sql += f" LIMIT {_LIKE_LIMIT}"
            before_count = len(chunk_results)
            chunk_results = list(chunk_results) + list(conn.execute(like_sql, like_params).fetchall())
            if len(chunk_results) > before_count:
                match_methods.append('like_fallback')
        except Exception as e:
            _log.debug("LIKE 폴백 실패: %s", e)

    # 프로젝트 ID 목록 (comm 필터 + hybrid용)
    project_ids = list(set(r['project_id'] for r in chunk_results))

    # --- 커뮤니케이션 로그 검색 ---
    comm_results = []
    comm_terms = extracted['keywords'] or [query]
    comm_fts_query = _sanitize_fts(comm_terms) if len(comm_terms) > 1 else _sanitize_fts_single(comm_terms[0] if comm_terms else '')
    if comm_fts_query:
        try:
            comm_sql = """
                SELECT cl.id, p.id as project_id, p.name,
                       cl.log_date, cl.sender, cl.subject, cl.summary
                FROM comm_log cl
                JOIN projects p ON cl.project_id = p.id
                JOIN comm_fts cf ON cf.rowid = cl.id
                WHERE comm_fts MATCH ?
            """
            comm_params = [comm_fts_query]

            # chunks에서 매칭된 프로젝트가 있으면 해당 프로젝트 우선 필터
            if project_ids:
                placeholders = ','.join('?' * len(project_ids))
                comm_sql += f" AND cl.project_id IN ({placeholders})"
                comm_params.extend(project_ids)
            elif extracted['clients']:
                cl_conds = []
                for cl in extracted['clients']:
                    cl_conds.append("p.name LIKE ?")
                    comm_params.append(f"%{cl}%")
                comm_sql += " AND (" + " OR ".join(cl_conds) + ")"

            comm_sql += f" ORDER BY cl.log_date DESC, bm25(comm_fts, 3.0, 2.0, 2.0, 1.0) LIMIT {_COMM_LIMIT}"
            comm_results = conn.execute(comm_sql, comm_params).fetchall()
        except Exception as e:
            _log.debug("comm FTS 실패: %s", e)

    return {
        'chunks': chunk_results,
        'comms': comm_results,
        'project_ids': project_ids,
        'project_names': list(set(r['name'] for r in chunk_results)),
        'match_methods': match_methods,
    }


# ──────────────────────────────────────────────
# 2b. Vega 통합 결과 형식
# ──────────────────────────────────────────────

def _make_result(*, project_id="", project_name="", client="", status="",
                 person="", content="", heading="", score=0.0,
                 source="sqlite", entry_date="", chunk_type="section",
                 metadata=None):
    """
    Vega canonical 검색 결과 dict.

    SQLite/QMD 양쪽 결과를 이 형식으로 정규화하여
    commands/search.py가 단일 형식만 소비하도록 합니다.
    """
    return {
        'project_id': project_id,
        'project_name': project_name,
        'client': client,
        'status': status,
        'person': person,
        'content': content,
        'heading': heading,
        'score': score,
        'source': source,          # 'sqlite' | 'qmd'
        'entry_date': entry_date,
        'chunk_type': chunk_type,
        'metadata': metadata or {},
    }


def _sqlite_rows_to_unified(chunk_results):
    """sqlite3.Row 리스트 → Vega 통합 형식 리스트"""
    return [
        _make_result(
            project_id=_row_value(r, 'project_id', ''),
            project_name=_row_value(r, 'name', ''),
            client=_row_value(r, 'client', ''),
            status=_row_value(r, 'status', ''),
            person=_row_value(r, 'person_internal', ''),
            content=_row_value(r, 'content', ''),
            heading=_row_value(r, 'section_heading', ''),
            score=0.0,
            source='sqlite',
            entry_date=_row_value(r, 'entry_date', ''),
            chunk_type=_row_value(r, 'chunk_type', 'section'),
        )
        for r in chunk_results
    ]


def _load_project_lookup(db_path):
    """projects 테이블에서 {name_lower: (id, name, client, status, person)} dict 로드."""
    if not db_path:
        return {}
    try:
        conn = get_db_connection(db_path, row_factory=True)
        try:
            rows = conn.execute(
                'SELECT id, name, client, status, person_internal FROM projects'
            ).fetchall()
            return {
                (row['name'] or '').strip().lower(): {
                    'id': row['id'],
                    'name': row['name'] or '',
                    'client': row['client'] or '',
                    'status': row['status'] or '',
                    'person': row['person_internal'] or '',
                }
                for row in rows if row['name']
            }
        finally:
            conn.close()
    except Exception as e:
        _log.debug("_load_project_lookup 실패: %s", e)
        return {}


def _qmd_items_to_unified(qmd_results, db_path=None):
    """QMD 파싱 결과 리스트 → Vega 통합 형식 리스트 (DB 메타데이터 보강)."""
    proj_lookup = _load_project_lookup(db_path) if db_path else {}
    unified = []
    for item in (qmd_results or []):
        meta = item.get('metadata', {})
        if meta.get('error'):
            continue
        project_name = (meta.get('project_name') or '').strip()
        if not project_name:
            project_name = _infer_qmd_project_name(item) or ''
        # DB 메타데이터 보강
        proj_info = proj_lookup.get(project_name.lower(), {})
        unified.append(_make_result(
            project_id=proj_info.get('id', ''),
            project_name=proj_info.get('name', '') or project_name,
            client=proj_info.get('client', ''),
            status=proj_info.get('status', ''),
            person=proj_info.get('person', ''),
            content=str(item.get('content') or '')[:500],
            heading=meta.get('title', ''),
            score=float(item.get('score') or 0.0),
            source='qmd',
            chunk_type='qmd_chunk',
            metadata={
                'uri': meta.get('uri', ''),
                'filepath': meta.get('filepath', ''),
                'context': meta.get('context', ''),
                'docid': meta.get('docid', ''),
                'best_chunk_pos': meta.get('best_chunk_pos'),
                'filter_bypassed': meta.get('filter_bypassed', False),
            },
        ))
    return unified


# ──────────────────────────────────────────────
# 3. QMD 어댑터 (CLI: qmd-wrapper.sh)
# ──────────────────────────────────────────────

class QMDAdapter:
    """
    QMD CLI 어댑터.
    
    서브명령:
      query  "검색어"  → 하이브리드 (BM25 + 벡터 + 쿼리 확장)
      search "검색어"  → BM25만 (빠름)
      vsearch "검색어" → 벡터만
    
    wrapper.sh는 환경변수 세팅 + --no-rerank 패스스루 역할.
    """

    def __init__(self, config=None):
        self.config = config or {}
        self.wrapper_path = self.config.get('wrapper', QMD_WRAPPER)
        self.binary_path = self.config.get('binary', QMD_BIN)
        self.use_binary = not self.wrapper_path and bool(self.binary_path)
        self.available = False
        self._check_availability()

    def _check_availability(self):
        """wrapper.sh 또는 qmd 바이너리가 사용 가능한지 확인"""
        if self.wrapper_path:
            wrapper = Path(self.wrapper_path)
            if wrapper.exists() and os.access(str(wrapper), os.X_OK):
                self.available = True
            else:
                self.available = wrapper.exists()
        elif self.binary_path:
            self.available = True

    def _run(self, subcmd, query, extra_args=None):
        """QMD CLI 실행. wrapper 또는 직접 바이너리 지원."""
        import subprocess
        if self.use_binary:
            cmd = [self.binary_path, subcmd, query, '--json']
        else:
            cmd = [self.wrapper_path, subcmd, query, '--json']
        if extra_args:
            cmd.extend(extra_args)

        try:
            result = subprocess.run(
                cmd,
                capture_output=True,
                text=True,
                timeout=30,
                env={**os.environ}
            )
            return result.stdout, result.stderr, result.returncode
        except subprocess.TimeoutExpired:
            return "", "QMD 타임아웃 (30초)", -1
        except FileNotFoundError:
            return "", f"QMD 실행 파일 없음: {self.wrapper_path or self.binary_path}", -2
        except Exception as e:
            return "", str(e), -3

    def _parse_output(self, stdout):
        """
        QMD 2.0 JSON 출력 파싱.

        QMD 2.0 HybridQueryResult 필드:
          file, displayPath, title, body, bestChunk, bestChunkPos,
          score, context, docid, explain?
        """
        if not stdout.strip():
            return []

        try:
            data = json.loads(stdout)
            if isinstance(data, list):
                return [
                    {
                        'source': item.get('displayPath') or item.get('file', ''),
                        'content': item.get('bestChunk', '') or (item.get('body', '') or '')[:500],
                        'score': item.get('score'),
                        'metadata': {
                            'uri': item.get('docid', ''),
                            'title': item.get('title', ''),
                            'context': item.get('context', ''),
                            'project_name': self._extract_project_from_path(
                                item.get('displayPath') or item.get('file', '')),
                            'filepath': item.get('displayPath') or item.get('file', ''),
                            'docid': item.get('docid', ''),
                            'best_chunk_pos': item.get('bestChunkPos'),
                        }
                    }
                    for item in data
                ]
        except (json.JSONDecodeError, ValueError):
            pass

        # JSON 파싱 실패 시 raw 결과 반환
        if stdout.strip():
            return [{
                'source': 'qmd',
                'content': stdout.strip()[:2000],
                'score': None,
                'metadata': {'raw': True}
            }]
        return []

    def _extract_project_from_path(self, path):
        """파일 경로에서 프로젝트명 추출.

        qmd:// URI (예: qmd://projects-dir-main/tools/claude.md) 및
        일반 파일 경로 모두 지원. 비프로젝트 파일(CLAUDE.md 등)은
        빈 문자열을 반환하여 노이즈를 방지한다.
        """
        if not path:
            return ''
        # qmd:// URI에서 경로 부분 추출
        clean = path
        if clean.startswith('qmd://'):
            # qmd://collection-name/path/to/file.md → path/to/file.md
            after = clean[6:]
            parts = after.split('/', 1)
            clean = parts[1].strip('/') if len(parts) > 1 and parts[1].strip('/') else parts[0]
        fname = clean.split('/')[-1] if '/' in clean else clean
        if fname.endswith('.md'):
            stem = fname[:-3]
            # 비프로젝트 파일 제외 (CLAUDE.md, README.md 등)
            if _NON_PROJECT_RE.search(stem):
                return ''
            return stem
        return ''

    def search(self, query, project_filter=None, mode='query', intent=None):
        """
        QMD 검색 실행.
        
        Args:
            query: 검색 질의
            project_filter: 프로젝트명 리스트 (SQLite에서 좁힌 결과)
            mode: 'query' (하이브리드) | 'search' (BM25) | 'vsearch' (벡터)
        
        Returns:
            파싱된 결과 리스트, 또는 실패 시 None
        """
        if not self.available:
            return None

        extra = []
        if intent:
            extra.extend(['--intent', intent])
        # RERANK_MODE: 'full' 이외에는 QMD 내부 리랭커 비활성
        if RERANK_MODE != 'full':
            extra.append('--no-rerank')
        stdout, stderr, rc = self._run(mode, query, extra_args=extra if extra else None)

        if rc != 0:
            # 실패 시 stderr 정보 포함하여 반환
            if stderr:
                return [{'source': 'qmd-error', 'content': stderr[:500],
                         'score': None, 'metadata': {'error': True, 'rc': rc}}]
            return None

        results = self._parse_output(stdout)

        # 프로젝트 필터 적용 (hybrid 모드)
        if project_filter and results:
            filter_lower = [pf.lower() for pf in project_filter]
            strict_filtered = []
            loose_filtered = []
            for r in results:
                meta = r.get('metadata', {})
                strict_hay = '\0'.join([
                    meta.get('project_name', '').lower(),
                    meta.get('filepath', '').lower(),
                    meta.get('title', '').lower(),
                    r.get('source', '').lower(),
                ])
                loose_hay = strict_hay + '\0' + '\0'.join([
                    meta.get('context', '').lower(),
                    r.get('content', '')[:500].lower(),
                ])
                if any(pf in strict_hay for pf in filter_lower):
                    strict_filtered.append(r)
                elif any(pf in loose_hay for pf in filter_lower):
                    loose_filtered.append(r)

            if strict_filtered:
                results = strict_filtered
            elif loose_filtered:
                results = loose_filtered
            else:
                # 필터 매칭 실패 → 원본 반환하되 플래그 표시 (recall 보존)
                for r in results:
                    r.setdefault('metadata', {})['filter_bypassed'] = True

        return results

    def search_fast(self, query, project_filter=None):
        """BM25 전용 검색 (빠름)"""
        return self.search(query, project_filter, mode='search')

    def search_semantic(self, query, project_filter=None):
        """벡터 전용 검색"""
        return self.search(query, project_filter, mode='vsearch')


# ──────────────────────────────────────────────
# 4. 결과 재정렬 / 퓨전
# ──────────────────────────────────────────────


def _row_value(row, key, default=''):
    if row is None:
        return default
    try:
        val = row[key]
        return val if val is not None else default
    except Exception:
        val = row.get(key, default) if hasattr(row, 'get') else default
        return val if val is not None else default


def _negate_date_str(date_str):
    """ISO 날짜 문자열을 반전하여 최신 우선 정렬 키 생성.

    각 숫자를 9에서 뺌: '2026-03-20' → '7973-96-79'
    빈 문자열/None → 'z' (맨 뒤로)
    """
    if not date_str:
        return 'z'
    return ''.join(str(9 - int(c)) if c.isdigit() else c for c in str(date_str))


def _infer_qmd_project_name(result):
    meta = result.get('metadata', {})
    project_name = (meta.get('project_name') or '').strip()
    if project_name:
        return project_name
    title = (meta.get('title') or '').strip()
    if title:
        return re.split(r'[:\-–|/]', title)[0].strip()
    source = result.get('source', '')
    m = re.search(r'/([^/]+?)\.md(?::\d+)?$', source)
    if m:
        return m.group(1).strip()
    return None


_NON_PROJECT_RE = re.compile(
    r'(INDEX|README|CLAUDE|CHANGELOG|TODO|LICENSE|\.github)',
    re.IGNORECASE,
)

# 백업/이전 버전 디렉토리 패턴 — QMD 인덱스 노이즈 필터링
# 주의: 정규식이 정당한 프로젝트명(서비스-v2 등)을 오탐하지 않도록
# vega- 접두어와 backup/tools-backup은 리터럴 매칭만 사용
_BACKUP_DIR_RE = re.compile(
    r'(?:^|[-/])(?:backup|bak|old)(?:[-/]|$)|'     # backup/bak/old 디렉토리
    r'(?:^|/)vega-v\d+[-/.]|'                        # vega-v1.xx 버전 디렉토리
    r'(?:^|/)tools-backup',                           # tools-backup 디렉토리
    re.IGNORECASE,
)


def _score_sqlite_chunks(chunks, extracted):
    """SQLite 청크 결과에서 프로젝트별 스코어 산출. Returns (scores, name_by_id, id_by_name)."""
    project_scores = {}
    project_chunk_count = {}   # 프로젝트별 매칭 청크 수 (다빈도 = 관련도 높음)
    project_name_by_id = {}
    project_id_by_name = {}

    for rank, row in enumerate(chunks, start=1):
        pid = _row_value(row, 'project_id', '')
        name = _row_value(row, 'name', '')
        if not pid:
            continue
        project_name_by_id[pid] = name
        if name:
            project_id_by_name[name] = pid
        score = max(0.0, 60.0 - rank)
        haystack = ' '.join([
            _row_value(row, 'name', '') or '', _row_value(row, 'client', '') or '', _row_value(row, 'status', '') or '',
            _row_value(row, 'person_internal', '') or '', _row_value(row, 'section_heading', '') or '', _row_value(row, 'content', '') or ''
        ]).lower()
        for group in ('clients', 'persons', 'statuses', 'tags', 'keywords'):
            for token in extracted.get(group, []):
                if token and token.lower() in haystack:
                    if group != 'keywords':
                        score += 8.0
                    else:
                        # 긴 키워드일수록 높은 가중치 (구체적 매칭 우선)
                        score += 4.0 + max(0, len(token) - 2) * 2.0
        project_scores[pid] = max(project_scores.get(pid, 0.0), score)
        project_chunk_count[pid] = project_chunk_count.get(pid, 0) + 1

    # 다빈도 보너스: 매칭 청크가 많을수록 관련도 높음 (2번째 청크부터 +3.0)
    for pid, count in project_chunk_count.items():
        if count > 1 and pid in project_scores:
            project_scores[pid] += (count - 1) * 3.0

    # 프로젝트명 직접 매칭 보너스 (v1.34: 정확 > 부분 구분)
    all_tokens = list(extracted.get('keywords', []))
    for g in ('clients', 'persons'):
        all_tokens.extend(extracted.get(g, []))
    for pid, name in project_name_by_id.items():
        if not name or pid not in project_scores:
            continue
        name_lower = name.lower()
        # 프로젝트명의 핵심어 추출 (첫 단어 = 가장 구별적)
        name_words = [w for w in re.split(r'\s+', name_lower) if len(w) >= 2]
        best_bonus = 0.0
        for token in all_tokens:
            if not token:
                continue
            tl = token.lower()
            if tl == name_lower or (name_words and tl == name_words[0]):
                # 정확 매칭: "비금도" == "비금도" or 첫 단어 매칭
                best_bonus = max(best_bonus, 30.0)
            elif tl in name_lower:
                # 부분 매칭: "비금" in "비금도 해상태양광"
                best_bonus = max(best_bonus, 20.0)
        project_scores[pid] += best_bonus

    return project_scores, project_name_by_id, project_id_by_name


def _score_qmd_results(qmd_results, project_scores, project_id_by_name, project_name_by_id, db_path=None):
    """QMD 결과를 프로젝트 스코어에 반영. 고아 QMD 복구 포함."""
    _db_lookup = None

    for rank, item in enumerate(qmd_results, start=1):
        if item.get('metadata', {}).get('error'):
            continue
        project_name = _infer_qmd_project_name(item)
        if not project_name:
            continue
        pid = project_id_by_name.get(project_name)
        # 고아 QMD 복구: SQLite 결과에 없는 프로젝트를 DB에서 조회
        if pid is None and db_path and project_name:
            if _db_lookup is None:
                _db_lookup = _load_project_lookup(db_path)
            proj_info = _db_lookup.get(project_name.lower())
            if proj_info:
                pid = proj_info['id']
                project_id_by_name[project_name] = pid
                project_name_by_id[pid] = proj_info['name']
        base = float(item.get('score') or 0.0)
        base = base if base > 0 else max(0.0, 30.0 - rank)
        # 페널티 — 최소 계수(min)로 적용하여 과도한 스태킹 방지
        penalty = 1.0
        if item.get('metadata', {}).get('filter_bypassed'):
            penalty = min(penalty, 0.3)
        _src = item.get('source', '') or item.get('metadata', {}).get('filepath', '')
        if _BACKUP_DIR_RE.search(_src):
            penalty = min(penalty, 0.1)
        elif _NON_PROJECT_RE.search(_src):
            penalty = min(penalty, 0.3)
        base *= penalty
        if pid is not None:
            project_scores[pid] = project_scores.get(pid, 0.0) + base * 0.6 + 10.0


def _apply_ranking(sqlite_results, qmd_results, project_scores, project_name_by_id, project_id_by_name):
    """프로젝트 스코어 기반으로 SQLite/QMD 양쪽 결과를 정렬."""
    ranked_project_ids = [pid for pid, _ in sorted(project_scores.items(), key=lambda kv: kv[1], reverse=True)]
    if ranked_project_ids:
        order = {pid: idx for idx, pid in enumerate(ranked_project_ids)}
        sqlite_results['chunks'] = sorted(
            sqlite_results.get('chunks', []),
            key=lambda r: (
                order.get(_row_value(r, 'project_id', ''), 10**9),
                _negate_date_str(_row_value(r, 'entry_date', '')),
                _row_value(r, 'chunk_id', 0),
            )
        )
        sqlite_results['project_ids'] = ranked_project_ids
        sqlite_results['project_names'] = [project_name_by_id[pid] for pid in ranked_project_ids if pid in project_name_by_id]
        _qmd_name_cache = {id(r): _infer_qmd_project_name(r) for r in qmd_results}
        qmd_results = sorted(
            qmd_results,
            key=lambda r: (
                order.get(project_id_by_name.get(_qmd_name_cache.get(id(r), ''), None), 10**9),
                -(float(r.get('score') or 0.0))
            )
        )

    sqlite_results['project_scores'] = [
        {'project_id': pid, 'project_name': project_name_by_id.get(pid, ''), 'score': round(project_scores[pid], 2)}
        for pid in ranked_project_ids
    ]
    return sqlite_results, qmd_results


def _rerank_fusion(sqlite_results, qmd_results, extracted, db_path=None):
    """SQLite + QMD 결과를 퓨전 스코어링으로 통합 정렬."""
    sqlite_results = sqlite_results or {'chunks': [], 'comms': [], 'project_ids': [], 'project_names': []}
    qmd_results = qmd_results or []

    # 1) SQLite 스코어링
    project_scores, project_name_by_id, project_id_by_name = _score_sqlite_chunks(
        sqlite_results.get('chunks', []), extracted
    )

    # 2) QMD 스코어링 (고아 복구 포함)
    _score_qmd_results(qmd_results, project_scores, project_id_by_name, project_name_by_id, db_path)

    # 3) 최종 정렬 적용
    return _apply_ranking(sqlite_results, qmd_results, project_scores, project_name_by_id, project_id_by_name)


# ──────────────────────────────────────────────
# 5. 통합 라우터
# ──────────────────────────────────────────────

class SearchRouter:

    def __init__(self, db_path=DB_PATH, qmd_config=None):
        self.db_path = db_path
        # v1.4: INFERENCE_BACKEND에 따라 어댑터 선택
        import config as _cfg
        backend = _cfg.INFERENCE_BACKEND
        if backend == 'local':
            try:
                from models import LocalAdapter
                self.qmd = LocalAdapter()
                _log.info("검색 어댑터: LocalAdapter (로컬 모델)")
            except Exception as e:
                _log.warning("LocalAdapter 로드 실패 → QMDAdapter 폴백: %s", e)
                self.qmd = QMDAdapter(qmd_config)
        elif backend == 'qmd':
            self.qmd = QMDAdapter(qmd_config)
            _log.info("검색 어댑터: QMDAdapter (외부 QMD)")
        else:
            # sqlite_only — 어댑터 비활성
            self.qmd = type('NullAdapter', (), {'available': False, 'search': lambda *a, **kw: None})()
            _log.info("검색 어댑터: 없음 (sqlite_only)")

    def search(self, query):
        """
        통합 검색 실행.
        
        1. 쿼리 분석 → 라우팅 결정
        2. 해당 엔진(들) 실행
        3. 결과 통합 반환
        
        QMD 모드 선택:
          - 의미 질문 (어떻게/왜/방식) → vsearch (벡터)
          - 키워드 특정 (JOCA, MC4 등) → search (BM25, 빠름)
          - 혼합/기본 → query (하이브리드)
        """
        # 0. 쿼리 정규화 (의미없는 접미사/문장부호 제거)
        query = _normalize_query(query)
        if not query:
            return {'query': '', 'analysis': {'route': 'sqlite', 'reason': 'empty'},
                    'sqlite': None, 'qmd': None, 'unified': [], 'comms': [],
                    'search_meta': {'route': 'sqlite', 'qmd_available': self.qmd.available,
                                    'qmd_used': False, 'sqlite_count': 0, 'qmd_count': 0,
                                    'rerank_mode': RERANK_MODE}}

        # 1. 분석
        analysis = analyze_query(query)
        route = analysis['route']
        extracted = analysis['extracted']

        # QMD 모드 결정
        has_semantic = any(re.search(p, query.lower()) for p in SEMANTIC_PATTERNS)
        has_specific_kw = bool(extracted['keywords']) and not has_semantic
        if has_semantic and not has_specific_kw:
            qmd_mode = 'vsearch'   # 순수 의미 → 벡터
        elif has_specific_kw and not has_semantic:
            qmd_mode = 'search'    # 특정 키워드 → BM25 (빠름)
        else:
            qmd_mode = 'query'     # 혼합 → 하이브리드

        # QMD 2.0 intent 파라미터 — 의미 패턴에서 맥락 추출
        qmd_intent = None
        if has_semantic:
            # 의미 질문에서 핵심 키워드를 intent로 전달
            kw = extracted.get('keywords', [])
            cl = extracted.get('clients', [])
            if kw or cl:
                qmd_intent = ' '.join(cl + kw)

        results = {
            'query': query,
            'analysis': analysis,
            'sqlite': None,
            'qmd': None,
            'qmd_mode': qmd_mode,
        }

        # 2. 선형 실행: SQLite 항상 먼저, QMD는 route에 따라 조건부
        results['sqlite'] = sqlite_search(query, extracted, self.db_path)

        if self.qmd.available:
            if route == 'qmd':
                # 순수 의미 검색 → 필터 없이 전체 QMD
                results['qmd'] = self.qmd.search(query, mode=qmd_mode, intent=qmd_intent)
            elif route == 'hybrid':
                # 혼합 → SQLite 프로젝트명으로 QMD 필터
                project_names = results['sqlite'].get('project_names', [])
                results['qmd'] = self.qmd.search(
                    query,
                    project_filter=project_names if project_names else None,
                    mode=qmd_mode,
                    intent=qmd_intent,
                )
            elif route == 'sqlite' and (has_semantic or bool(extracted.get('keywords'))):
                # 보충 → 결과 부족할 때만 BM25 빠른 모드
                if len(results['sqlite'].get('chunks', [])) < 5:
                    results['qmd'] = self.qmd.search(query, mode='search', intent=qmd_intent)
                    results['analysis']['reason'] += " → QMD 보충 검색(BM25)"
        elif route == 'qmd':
            # QMD 미연결 → SQLite 폴백 (이미 위에서 실행됨)
            results['analysis']['reason'] += " (QMD 미연결 → SQLite 폴백)"

        # 리랭킹 적용 (RERANK_MODE 토글)
        results['rerank_mode'] = RERANK_MODE
        if RERANK_MODE != 'none' and (results.get('sqlite') or results.get('qmd')):
            results['sqlite'], results['qmd'] = _rerank_fusion(results.get('sqlite'), results.get('qmd'), extracted, db_path=self.db_path)

        # QMD 필터 우회 표기
        if results.get('qmd') and any(r.get('metadata', {}).get('filter_bypassed') for r in results['qmd']):
            results['analysis']['reason'] += ' (QMD 프로젝트 필터 우회됨 — 결과 신뢰도 낮음)'

        # Vega 통합 결과 형식 생성
        unified = []
        if results.get('sqlite') and results['sqlite'].get('chunks'):
            unified.extend(_sqlite_rows_to_unified(results['sqlite']['chunks']))
        if results.get('qmd'):
            unified.extend(_qmd_items_to_unified(results['qmd'], db_path=self.db_path))
        # project_scores 반영 (fusion 활성 시에만 존재)
        score_map = {}
        if results.get('sqlite') and results['sqlite'].get('project_scores'):
            score_map = {s['project_id']: s['score'] for s in results['sqlite']['project_scores']}
        for item in unified:
            if item['source'] == 'sqlite' and item['project_id'] in score_map:
                item['score'] = score_map[item['project_id']]
        results['unified'] = unified

        # comms 별도 전달 (search.py에서 사용)
        results['comms'] = results.get('sqlite', {}).get('comms', []) if results.get('sqlite') else []

        # search_meta: AI가 소비하는 깔끔한 메타데이터
        sqlite_res = results.get('sqlite') or {}
        results['search_meta'] = {
            'route': route,
            'qmd_available': self.qmd.available,
            'qmd_used': results.get('qmd') is not None,
            'sqlite_count': len(sqlite_res.get('chunks', [])),
            'qmd_count': len(results.get('qmd') or []),
            'rerank_mode': RERANK_MODE,
        }

        return results
