#!/usr/bin/env python3
"""
Vega 프로젝트 검색엔진 — 핵심 인프라

명령 레지스트리, 실행기, NL 라우팅, 유틸리티, AI 헬퍼.
개별 명령 핸들러는 commands/ 디렉토리에 위치.
"""

import sys, json, re, os
from pathlib import Path
from datetime import datetime, timedelta

SELF_DIR = Path(__file__).parent
sys.path.insert(0, str(SELF_DIR))
import config as _cfg
from config import get_db_connection, db_session, VegaError, write_audit_log


def _publish(names_dict):
    """Register public aliases for private functions in module globals."""
    g = globals()
    for public_name, private_func in names_dict.items():
        g[public_name] = private_func


# 공개 API — commands/ 모듈이 import할 수 있는 이름
__all__ = [
    # 핵심 인프라
    'execute', 'route_input', 'register_command', 'main',
    'EXPLICIT_COMMANDS', 'NL_ROUTES',
    # config 재수출 (DB_PATH, MD_DIR은 config에서 직접 import 권장)
    'SELF_DIR', 'get_db_connection', 'db_session',
    'VegaError', 'write_audit_log',
    # 유틸리티 (공개)
    'find_project_id', 'find_project_id_in_text', 'fuzzy_find_project',
    'get_flag', 'escape_like',
    'extract_days', 'extract_limit', 'extract_bullets',
    'build_search_suggestions', 'build_single_brief',
    'ensure_db',
    'SEARCH_HINT_STOPWORDS',
    # AI 헬퍼 (공개)
    'apply_depth', 'build_ai_hint', 'build_bundle',
    'try_auto_correct', 'route_confidence', 'smart_route',
    'apply_format', 'generate_summary',
    # 세션
    'load_session', 'save_session', 'update_session', 'resolve_session_context',
]


def _ensure_db():
    """DB 없으면 .md에서 자동 재빌드. 컨테이너 재시작 후 자동 복구용."""
    db_path = _cfg.DB_PATH
    md_dir = _cfg.MD_DIR
    if os.path.exists(db_path):
        return True
    if not os.path.isdir(md_dir):
        return False
    try:
        import project_db_v2, io
        _orig_stdout = sys.stdout
        sys.stdout = io.StringIO()  # stdout 억제 (JSON 출력 오염 방지)
        try:
            project_db_v2.import_files(md_dir, db_path=db_path)
        finally:
            sys.stdout = _orig_stdout
        return os.path.exists(db_path)
    except Exception as e:
        import logging as _logging
        _logging.getLogger(__name__).warning("DB 자동 재빌드 실패: %s", e)
        return False

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# 0. 공통 유틸리티
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

def _get_flag(args, flag):
    """인수 리스트에서 --flag VALUE 패턴의 값을 추출"""
    for i, arg in enumerate(args):
        if arg == flag and i + 1 < len(args):
            return args[i + 1]
    return None


def _escape_like(s):
    """SQL LIKE 패턴에서 특수문자(%와 _)를 이스케이프. ESCAPE '\\' 와 함께 사용."""
    if not s:
        return s
    return s.replace('\\', '\\\\').replace('%', '\\%').replace('_', '\\_')


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# 1. 자연어 → 명령 라우팅
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

# 명시적 서브명령 목록
EXPLICIT_COMMANDS = {
    'search', 'cross', 'dashboard', 'contacts', 'pipeline',
    'weekly', 'changelog', 'timeline', 'show', 'list',
    'template', 'sync-back', 'health', 'mail-append',
    'update', 'urgent', 'person', 'add-action', 'brief', 'recent',
    'upgrade',
}

# 자연어 → 명령 매핑 패턴
NL_ROUTES = [
    # 긴급/관심 필요 (우선순위 높게: "긴급 변경" 등에서 긴급이 먼저 매칭되도록)
    (r'(급한|긴급|위험|관심.*필요|뭐.*해야|우선순위|blocked|막힌)', 'urgent'),
    # 크로스 분석
    (r'(연결고리|관련.*프로젝트|같은.*거래처|같은.*자재|인력.*충돌|시너지)', 'cross'),
    # 빠른 브리프 / 최근 활동
    (r'(브리프|한눈에.*요약|빠른.*요약|간단.*브리핑|프로젝트.*한눈)', 'brief'),
    (r'(최근.*활동|최근.*업데이트|최근.*변화|최신.*활동|latest)', 'recent'),
    # 비교
    (r'(비교|차이|다른점|공통점|같은점)', 'compare'),
    # 통계
    (r'(통계|분석|수치|평균|활동량|빈도)', 'stats'),
    # 대시보드/현황
    (r'(현황|대시보드|전체.*상태|프로젝트.*몇|요약)', 'dashboard'),
    # 인물 포트폴리오 (연락처보다 앞에 — "고건이 뭐해" 같은 쿼리)
    (r'(뭐\s*하고\s*있|맡은\s*거|담당하는|포트폴리오|업무.*현황)', 'person'),
    # 연락처
    (r'(연락처|전화번호|이메일|담당자.*연락)', 'contacts'),
    # 문제/이슈
    (r'(문제|이슈|결함|불량|장애|고장)', 'search'),
    # 인력/리소스
    (r'(인력|팀\s*현황|인원|리소스)', 'cross'),
    # 파이프라인/금액/비용
    (r'(금액|매출|파이프라인|수주.*금|얼마|비용|원가|예산|단가|견적)', 'pipeline'),
    # 주간보고/변경
    (r'(주간|이번.*주|뭐.*바뀌|변경|리포트|보고)', 'weekly_or_changelog'),
    # 타임라인 / 일정 / 할일
    (r'(타임라인|이력|경과|순서|일정|스케줄|공정)', 'timeline'),
    (r'(마감|기한|납기|데드라인|언제까지)', 'urgent'),
    (r'(이번\s*달|다음\s*달|이달|금월|할\s*일|할일|액션\s*아이템|해야.*할)', 'urgent'),
    # 프로젝트 목록
    (r'(프로젝트.*목록|프로젝트.*리스트|전체.*프로젝트|몇.*개)', 'list'),
]


def route_input(args):
    """
    입력을 분석하여 실행할 명령과 파라미터를 결정.

    Returns:
        (command, params)
        command: 'search' | 'cross' | 'dashboard' | 'contacts' | 'pipeline' |
                 'weekly' | 'changelog' | 'timeline' | 'show' | 'list' | 'template'
        params: dict of parameters
    """
    if not args:
        return 'dashboard', {}

    first = args[0]

    # 명시적 서브명령
    if first in EXPLICIT_COMMANDS:
        return first, {'sub_args': args[1:]}

    # 숫자만 → show (프로젝트 상세)
    if first.isdigit():
        return 'show', {'id': int(first)}

    # 자연어 입력
    query = ' '.join(args)

    # NL 라우팅
    for pattern, cmd in NL_ROUTES:
        if re.search(pattern, query):
            if cmd == 'weekly_or_changelog':
                if any(kw in query for kw in ['바뀌', '변경']):
                    return 'changelog', {'query': query}
                return 'weekly', {'query': query}
            if cmd == 'timeline':
                # 프로젝트명 추출 시도
                return 'timeline', {'query': query}
            if cmd == 'brief':
                return 'brief', {'query': query}
            if cmd == 'recent':
                return 'recent', {'query': query}
            if cmd == 'compare':
                return 'compare', {'query': query}
            if cmd == 'stats':
                return 'stats', {}
            if cmd == 'contacts':
                # 이름 추출
                return 'contacts', {'query': query}
            if cmd == 'cross':
                return 'cross', {'query': query}
            return cmd, {'query': query}

    # 기본: 검색
    return 'search', {'query': query}


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# 2. 명령 레지스트리 & 실행기
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

# 명령 레지스트리: 새 명령 추가 시 이 dict에만 등록하면 됨
# key: 명령 이름
# value: (handler_function, needs_db)  — needs_db=False면 DB 없어도 실행 가능
_COMMAND_REGISTRY = {}

# 오류 자동 교정 재귀 깊이 추적 (search→brief→search 무한 루프 방지)
_auto_correct_depth = 0

def register_command(name, needs_db=True, read_only=True, category='query', summary_fn=None):
    """명령 핸들러 데코레이터.

    Args:
        name: 명령 이름
        needs_db: DB 필요 여부
        read_only: True면 읽기 전용 (데이터 변경 없음)
        category: 'query'|'write'|'system'|'ai'
        summary_fn: 요약 생성 함수 (data → str). 지정하면 _generate_summary의 if/elif 불필요
    """
    def decorator(fn):
        _COMMAND_REGISTRY[name] = {
            'handler': fn,
            'needs_db': needs_db,
            'read_only': read_only,
            'category': category,
            'summary_fn': summary_fn,
        }
        EXPLICIT_COMMANDS.add(name)
        return fn
    return decorator


def addon_command(name, addon_class, sub_cmd='', **reg_kwargs):
    """애드온 브릿지 명령을 한 줄로 등록.

    Usage:
        addon_command('dashboard', Dashboard)
        addon_command('changelog', Changelog)
    """
    def handler(params=None):
        from addons import Ctx
        ctx = Ctx(_cfg.DB_PATH)
        addon = addon_class()
        params = params or {}
        sub_args = params.get('sub_args', [])
        cmd = sub_args[0] if sub_args else sub_cmd
        return addon.safe_api(cmd, sub_args[1:] if sub_args else [], ctx)
    reg_kwargs.setdefault('needs_db', True)
    reg_kwargs.setdefault('read_only', True)
    reg_kwargs.setdefault('category', 'query')
    _COMMAND_REGISTRY[name] = {
        'handler': handler,
        'needs_db': reg_kwargs['needs_db'],
        'read_only': reg_kwargs['read_only'],
        'category': reg_kwargs['category'],
        'summary_fn': reg_kwargs.get('summary_fn'),
    }
    EXPLICIT_COMMANDS.add(name)
    return handler


def require_project(params, usage_hint=None, fuzzy=False):
    """params에서 프로젝트 ID를 추출. 못 찾으면 error dict 반환, 찾으면 int 반환.
    fuzzy=True면 LIKE 실패 시 difflib 퍼지 매칭 시도.
    """
    pid = params.get('id')
    sub_args = params.get('sub_args', [])
    query = params.get('query', '')
    if not pid and sub_args:
        pid = _find_project_id(sub_args[0])
    if not pid and query:
        pid = _find_project_id(query)
    # fuzzy 폴백
    if not pid and fuzzy:
        ref = (sub_args[0] if sub_args else '') or query
        if ref:
            pid, _conf = _fuzzy_find_project(ref)
    if not pid:
        hint = usage_hint or f'프로젝트 ID 또는 이름을 지정해주세요.'
        return {'error': hint}
    return pid


def execute(command, params):
    """명령 실행 → 표준화된 JSON 응답 반환"""

    response = {
        'command': command,
        'timestamp': datetime.now().isoformat(),
        'status': 'ok',
        'data': None,
        'summary': '',
    }

    entry = _COMMAND_REGISTRY.get(command)
    if not entry:
        response['status'] = 'error'
        response['data'] = {'error': f'알 수 없는 명령: {command}',
                           'available': sorted(_COMMAND_REGISTRY.keys())}
        return response

    if isinstance(entry, dict):
        handler = entry['handler']
        needs_db = entry['needs_db']
    else:
        # backward compat with old (handler, needs_db) tuples
        handler, needs_db = entry

    if needs_db and not _ensure_db():
        response['status'] = 'error'
        response['data'] = {'error': 'DB 파일 없고 자동 재빌드 실패. .md 디렉토리를 확인하세요.',
                            'db_path': _cfg.DB_PATH, 'md_dir': _cfg.MD_DIR}
        response['summary'] = 'DB 자동 재빌드 실패'
        return response

    try:
        response['data'] = handler(params)

        # addon safe_api 에러 감지: 'error' 키가 있고 정상 데이터 키가 없을 때만 에러 처리
        if isinstance(response['data'], dict) and 'error' in response['data']:
            data_keys = set(response['data'].keys()) - {'error', 'error_type', 'debug', 'addon', 'usage'}
            if not data_keys:
                response['status'] = 'error'
                response['summary'] = f"오류: {response['data']['error']}"

        # 자동 요약 생성
        if response['status'] == 'ok' and response['data']:
            response['summary'] = _generate_summary(command, response['data'])

            # 세션 업데이트 (E-5)
            if command != 'ask':  # ask는 자체적으로 세션 관리
                _update_session(command, response['data'])

        # --format 후처리
        fmt = params.get('format', 'summary') if isinstance(params, dict) else 'summary'
        if fmt != 'summary' and response['status'] == 'ok' and response['data']:
            response['data'] = _apply_format(command, response['data'], fmt)

    except VegaError as e:
        response['status'] = 'error'
        response['data'] = {'error': e.message, 'error_type': e.error_type}
        if e.usage:
            response['data']['usage'] = e.usage
        response['summary'] = f'오류: {e.message}'

    except Exception as e:
        import traceback
        response['status'] = 'error'
        response['data'] = {'error': str(e), 'type': type(e).__name__,
                           'debug': traceback.format_exc()}
        response['summary'] = f'오류: {e}'

    # 오류 자동 교정 (E-7) — ask 이외 명령에서도 작동
    # _auto_correct_depth로 재귀 교정(search→brief→search) 무한 루프 방지
    if response['status'] == 'error' and command != 'ask' and _auto_correct_depth < 1:
        corrected = _try_auto_correct(command, params, response)
        if corrected and corrected.get('status') == 'ok':
            corrected['_meta'] = corrected.get('_meta', {})
            corrected['_meta']['auto_corrected'] = {
                'original_command': command,
                'corrected_to': corrected['command'],
            }
            return corrected

    return response


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# 3. 공통 유틸리티
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

def _find_project_id(ref):
    """프로젝트 ID 또는 이름으로 프로젝트 ID 반환. 못 찾으면 None."""
    if isinstance(ref, int):
        return ref
    if isinstance(ref, str) and ref.isdigit():
        return int(ref)
    if not ref:
        return None
    conn = get_db_connection()
    try:
        row = conn.execute("SELECT id FROM projects WHERE name LIKE ? ESCAPE '\\'", (f"%{_escape_like(ref)}%",)).fetchone()
        return row[0] if row else None
    finally:
        conn.close()


_SEARCH_HINT_STOPWORDS = {
    '프로젝트', '검색', '찾아', '찾아줘', '보여줘', '알려줘', '최근', '활동',
    '업데이트', '변화', '요약', '브리프', '한눈에', '빠른', '간단', '정리',
    '무엇', '뭐', '어떻게', '현황', '상태', '문의', '관련', '내용'
}


def _extract_days(params, default=7, max_days=90):
    sub_args = params.get('sub_args', []) or []
    query = params.get('query', '') or ''
    for i, arg in enumerate(sub_args):
        if arg == '--days' and i + 1 < len(sub_args):
            try:
                return max(1, min(max_days, int(sub_args[i + 1])))
            except ValueError:
                break
    text = ' '.join(sub_args) + ' ' + query
    for pattern, mult in ((r'(\d+)\s*일', 1), (r'(\d+)\s*주', 7), (r'(\d+)\s*개월', 30)):
        m = re.search(pattern, text)
        if m:
            return max(1, min(max_days, int(m.group(1)) * mult))
    if '이번 주' in text or '이번주' in text:
        return 7
    if '이번 달' in text or '이번달' in text:
        return min(max_days, 30)
    return default


def _extract_limit(params, default=20, max_limit=100):
    sub_args = params.get('sub_args', []) or []
    for i, arg in enumerate(sub_args):
        if arg == '--limit' and i + 1 < len(sub_args):
            try:
                return max(1, min(max_limit, int(sub_args[i + 1])))
            except ValueError:
                break
    return default


def _find_project_id_in_text(text):
    """자연어 안에서 프로젝트명을 찾아 가장 그럴듯한 프로젝트 ID 반환."""
    if not text:
        return None
    direct = _find_project_id(text)
    if direct:
        return direct

    query = str(text)
    query_norm = re.sub(r'\s+', '', query.lower())
    conn = get_db_connection(row_factory=True)
    try:
        rows = conn.execute("SELECT id, name FROM projects").fetchall()
    finally:
        conn.close()

    best_id = None
    best_score = 0
    for row in rows:
        name = (row['name'] or '').strip()
        if not name:
            continue
        name_norm = re.sub(r'\s+', '', name.lower())
        score = 0
        if name in query:
            score += 20
        if name_norm and name_norm in query_norm:
            score += 18
        for token in re.findall(r'[가-힣A-Za-z0-9]+', name):
            if len(token) >= 2 and token in query:
                score += min(8, len(token))
        if score > best_score:
            best_id = row['id']
            best_score = score
    if best_score >= 6:
        return best_id
    # fuzzy 폴백
    pid, _conf = _fuzzy_find_project(text)
    return pid


def _fuzzy_find_project(ref, threshold=0.55):
    """프로젝트명 퍼지 매칭. LIKE 실패 시 difflib로 폴백.
    Returns: (project_id, confidence) or (None, 0)
    """
    exact = _find_project_id(ref)
    if exact:
        return exact, 1.0

    import difflib
    ref_norm = re.sub(r'\s+', '', (ref or '').lower())
    if not ref_norm:
        return None, 0

    # ref 토큰 분리 (문장에서 각 단어도 매칭 시도)
    ref_tokens = [t for t in re.findall(r'[가-힣A-Za-z0-9]+', ref or '') if len(t) >= 2]

    conn = get_db_connection(row_factory=True)
    try:
        rows = conn.execute("SELECT id, name FROM projects").fetchall()
    finally:
        conn.close()

    best_id, best_score = None, 0
    for row in rows:
        name_raw = (row['name'] or '').strip()
        name_norm = re.sub(r'\s+', '', name_raw.lower())
        if not name_norm:
            continue
        name_tokens = [t.lower() for t in re.split(r'\s+', name_raw) if len(t) >= 2]

        # 전체 ref vs 전체 이름
        ratio = difflib.SequenceMatcher(None, ref_norm, name_norm).ratio()
        # ref vs 이름 토큰 (e.g. "비금또" vs "비금도")
        for nt in name_tokens:
            ratio = max(ratio, difflib.SequenceMatcher(None, ref_norm, nt).ratio())
        # ref 토큰 vs 이름 토큰 (e.g. 문장 중 "비금또" vs "비금도")
        for rt in ref_tokens:
            rt_lower = rt.lower()
            for nt in name_tokens:
                ratio = max(ratio, difflib.SequenceMatcher(None, rt_lower, nt).ratio())
        # 부분 포함 보너스
        if ref_norm in name_norm or name_norm in ref_norm:
            ratio = max(ratio, 0.85)
        if ratio > best_score:
            best_id, best_score = row['id'], ratio

    if best_score >= threshold:
        return best_id, round(best_score, 2)
    return None, 0


def _extract_bullets(text, limit=5):
    if not text:
        return []
    items = []
    for raw in str(text).splitlines():
        line = raw.strip()
        if not line:
            continue
        line = re.sub(r'^[\-•*]+\s*', '', line)
        line = re.sub(r'^\d+[.)]\s*', '', line)
        line = re.sub(r'\s+', ' ', line)
        if len(line) < 3:
            continue
        if line not in items:
            items.append(line[:220])
        if len(items) >= limit:
            break
    if not items:
        compact = re.sub(r'\s+', ' ', str(text)).strip()
        if compact:
            items.append(compact[:220])
    return items[:limit]


def _build_search_suggestions(query, limit=8):
    """검색 결과가 없을 때 프로젝트/고객사/담당자 후보를 제안."""
    import difflib

    tokens = [
        t for t in re.findall(r'[가-힣A-Za-z0-9&+/.-]+', query or '')
        if len(t) >= 2 and t not in _SEARCH_HINT_STOPWORDS
    ]
    normalized_query = re.sub(r'\s+', '', (query or '').lower())

    conn = get_db_connection(row_factory=True)
    try:
        rows = conn.execute(
            "SELECT id, name, client, person_internal, person_external FROM projects"
        ).fetchall()
    finally:
        conn.close()

    scored = {'projects': [], 'clients': [], 'persons': []}
    seen = {'projects': set(), 'clients': set(), 'persons': set()}

    def _score(text_value):
        text_value = (text_value or '').strip()
        if not text_value:
            return 0.0
        norm = re.sub(r'\s+', '', text_value.lower())
        score = 0.0
        if normalized_query and normalized_query in norm:
            score += 9.0
        for tok in tokens:
            tok_norm = re.sub(r'\s+', '', tok.lower())
            if tok_norm and tok_norm in norm:
                score += 3.0 + min(2.0, len(tok) / 3.0)
        if query:
            ratio = difflib.SequenceMatcher(None, normalized_query or query.lower(), norm).ratio()
            if ratio >= 0.5:
                score += ratio * 4.0
        return score

    for row in rows:
        project_score = max(_score(row['name']), _score(f"{row['name']} {row['client'] or ''}"))
        if project_score > 0 and row['id'] not in seen['projects']:
            seen['projects'].add(row['id'])
            scored['projects'].append({
                'id': row['id'], 'name': row['name'], 'client': row['client'], 'score': round(project_score, 2)
            })

        client = (row['client'] or '').strip()
        client_score = _score(client)
        if client and client_score > 0 and client not in seen['clients']:
            seen['clients'].add(client)
            scored['clients'].append({'name': client, 'score': round(client_score, 2)})

        for person_field in (row['person_internal'], row['person_external']):
            for person in re.split(r'[\/,·]+', person_field or ''):
                person = person.strip()
                if not person:
                    continue
                person_score = _score(person)
                if person_score > 0 and person not in seen['persons']:
                    seen['persons'].add(person)
                    scored['persons'].append({'name': person, 'score': round(person_score, 2)})

    scored['projects'].sort(key=lambda x: (-x['score'], x['name'] or ''))
    scored['clients'].sort(key=lambda x: (-x['score'], x['name']))
    scored['persons'].sort(key=lambda x: (-x['score'], x['name']))
    return {k: v[:limit] for k, v in scored.items() if v}


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# 세션 컨텍스트 (E-5)
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

_SESSION_FILE = SELF_DIR / '.session.json'
_MAX_SESSION_ITEMS = 10

def _load_session():
    try:
        if _SESSION_FILE.exists():
            return json.loads(_SESSION_FILE.read_text(encoding='utf-8'))
    except Exception:
        pass
    return {'recent': [], 'last_command': None, 'last_at': None}

def _save_session(session):
    try:
        import tempfile
        data = json.dumps(session, ensure_ascii=False)
        # Atomic write: write to temp file then rename to avoid corruption on concurrent access
        fd, tmp_path = tempfile.mkstemp(dir=str(SELF_DIR), suffix='.tmp', prefix='.session_')
        try:
            os.write(fd, data.encode('utf-8'))
            os.close(fd)
            fd = None
            # On Windows, target must not exist for os.rename; use os.replace which handles this
            os.replace(tmp_path, str(_SESSION_FILE))
        except Exception:
            if fd is not None:
                os.close(fd)
            # Clean up temp file on failure
            try:
                os.unlink(tmp_path)
            except OSError:
                pass
    except Exception:
        pass

def _update_session(command, data):
    """조회된 프로젝트를 세션에 기록"""
    session = _load_session()
    project_refs = []
    if isinstance(data, dict):
        if 'project_id' in data and data['project_id']:
            project_refs.append({'id': data['project_id'], 'name': data.get('project_name', '')})
        elif 'projects' in data and isinstance(data['projects'], list):
            for p in (data['projects'] or [])[:3]:
                if isinstance(p, dict) and p.get('id'):
                    project_refs.append({'id': p['id'], 'name': p.get('name', '')})
        elif 'id' in data:
            project_refs.append({'id': data['id'], 'name': data.get('name', '')})
    if project_refs:
        existing_ids = {r['id'] for r in project_refs}
        session['recent'] = project_refs + [
            item for item in session.get('recent', [])
            if item.get('id') not in existing_ids
        ]
        session['recent'] = session['recent'][:_MAX_SESSION_ITEMS]
        session['last_command'] = command
        session['last_at'] = datetime.now().isoformat()
        _save_session(session)

def _resolve_session_context(query, context=None):
    """'그 프로젝트', '거기' 등의 대명사를 세션에서 해석"""
    pronouns = ['그 프로젝트', '거기', '그거', '아까 그', '방금 그', '위 프로젝트']
    has_pronoun = any(p in query for p in pronouns)

    recent_ids = []
    if context and context.get('recent_project_ids'):
        recent_ids = context['recent_project_ids']
    elif has_pronoun:
        session = _load_session()
        if session.get('recent'):
            recent_ids = [r['id'] for r in session['recent'][:3]]

    return recent_ids


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# AI 헬퍼 (E-2 ~ E-7)
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

def _apply_depth(data, command, depth):
    """응답 깊이에 따라 필드를 선택적으로 잘라냄 (E-2)"""
    if not depth or depth == 'normal':
        return data
    if not isinstance(data, dict):
        return data

    if depth == 'brief':
        if command == 'show':
            return {
                'id': data.get('id'), 'name': data.get('name'),
                'status': data.get('status'), 'client': data.get('client'),
                'person_internal': data.get('person_internal'),
                'capacity': data.get('capacity'),
                'section_count': len(data.get('sections', [])),
                'comm_count': len(data.get('recent_comms', [])),
                'recommended_commands': data.get('recommended_commands'),
            }
        if command == 'search':
            return {
                'query': data.get('query'),
                'projects': [
                    {'id': p.get('id'), 'name': p.get('name'), 'status': p.get('status'), 'score': p.get('score')}
                    for p in data.get('projects', [])
                ],
                'result_count': data.get('result_count'),
                'matched_keywords': data.get('matched_keywords'),
                'follow_up_hint': data.get('follow_up_hint'),
                'suggestions': data.get('suggestions'),
            }
        if command == 'urgent':
            return {
                'total': data.get('total'), 'critical': data.get('critical'),
                'overdue': data.get('overdue'), 'stale': data.get('stale'),
                'items': [
                    {'project_name': i.get('project_name'), 'priority': i.get('priority'), 'reason': i.get('reason')}
                    for i in data.get('items', [])[:5]
                ],
            }
        if command == 'brief':
            kept = {k: data[k] for k in ('project_id', 'project_name', 'status', 'client',
                    'person_internal', 'latest_activity', 'next_actions', 'risks') if k in data}
            kept['comm_count'] = len(data.get('recent_comms', []))
            return kept
        if command == 'dashboard':
            return {
                'total_projects': data.get('total_projects'),
                'active_projects': data.get('active_projects'),
                'overloaded_persons': data.get('overloaded_persons'),
                'by_status': {k: len(v) for k, v in (data.get('by_status') or {}).items()},
            }
        if command == 'person':
            return {
                'person': data.get('person'),
                'project_count': data.get('project_count'),
                'projects': [{'id': p.get('id'), 'name': p.get('name'), 'status': p.get('status')} for p in data.get('projects', [])],
                'comm_count': data.get('comm_count'),
            }
        if command == 'list':
            return {
                'total': data.get('total'),
                'projects': [{'id': p.get('id'), 'name': p.get('name'), 'status': p.get('status')} for p in data.get('projects', [])],
                'filters': data.get('filters'),
            }
        if command == 'compare':
            return {
                'project_count': data.get('project_count'),
                'projects': [
                    {'id': p.get('id'), 'name': p.get('name'), 'status': p.get('status'), 'client': p.get('client')}
                    for p in data.get('projects', [])
                ],
                'shared': data.get('shared'),
                'summary': data.get('summary'),
            }
        if command == 'stats':
            return {
                'projects': data.get('projects'),
                'communication': {
                    'total': (data.get('communication') or {}).get('total'),
                    'avg_per_project': (data.get('communication') or {}).get('avg_per_project'),
                },
                'summary': data.get('summary'),
            }

    # depth == 'full': content 제한 해제 (현재는 normal과 동일)
    return data


def _build_ai_hint(command, data, query=''):
    """AI가 결과를 어떻게 전달하면 좋을지 상황별 가이드 (E-3)"""
    hints = []
    if not isinstance(data, dict):
        return hints

    if command == 'search':
        n = data.get('result_count', {}).get('projects', 0)
        if n == 0:
            hints.append({'situation': 'no_results',
                'guide': '검색 결과가 없습니다. suggestions 필드의 후보를 안내하거나 키워드 변경을 제안하세요.'})
        elif n == 1:
            p = data.get('projects', [{}])[0]
            hints.append({'situation': 'single_match',
                'guide': f"정확히 1개 프로젝트가 매칭됐습니다. 추가 정보가 필요하면 brief {p.get('id', '')}를 호출하세요."})
        elif n > 5:
            hints.append({'situation': 'too_many_results',
                'guide': '결과가 많습니다. 상위 3개만 언급하고, 조건을 좁혀달라고 요청하세요.'})
        # 의미 검색 기여도 안내
        sm = data.get('search_meta', {})
        if sm.get('semantic_used') and sm.get('semantic_count', 0) > 0:
            hints.append({'situation': 'semantic_enriched',
                'guide': f"의미 검색이 {sm['semantic_count']}건 추가 결과를 포함합니다. sections[*].source='semantic' 항목 참고."})

    if command == 'urgent':
        if data.get('critical', 0) > 0:
            hints.append({'situation': 'has_critical', 'tone': 'alert',
                'guide': '긴급(🔴) 프로젝트가 있습니다. 이것을 먼저 언급하세요.'})
        elif data.get('total', 0) == 0:
            hints.append({'situation': 'all_clear', 'tone': 'reassuring',
                'guide': '긴급 항목이 없습니다. "현재 긴급한 것은 없습니다"라고 짧게 답하세요.'})

    if command == 'brief':
        if data.get('risks'):
            hints.append({'situation': 'has_risks',
                'guide': '리스크 항목이 있습니다. 상태/액션 보고 후 리스크를 별도로 강조하세요.'})
        if not data.get('next_actions'):
            hints.append({'situation': 'no_actions',
                'guide': '다음 액션이 비어 있습니다. 사용자에게 액션 항목을 추가할지 물어보세요.',
                'suggested_followup': f"add-action {data.get('project_id', '')}"})

    if command == 'person':
        if data.get('project_count', 0) >= 5:
            hints.append({'situation': 'overloaded',
                'guide': f"이 인물이 {data['project_count']}개 프로젝트를 담당합니다. 과부하 상태임을 언급하세요."})

    if command == 'search' and data.get('_auto_brief'):
        ab = data['_auto_brief']
        hints.append({'situation': 'fuzzy_matched',
            'guide': f"정확한 검색 결과는 없지만, '{ab.get('name','')}' 프로젝트가 유사하게 매칭됐습니다. "
                     f"이 프로젝트 정보를 _auto_brief에서 직접 답변하세요."})

    if command == 'show':
        pid_val = data.get('id') or data.get('project_id')
        if pid_val:
            hints.append({'situation': 'show_detail',
                'guide': f"프로젝트 상세입니다. 요약이 필요하면 brief {pid_val}, 이력은 timeline {pid_val}."})

    if command == 'list':
        count = len(data.get('projects', []))
        hints.append({'situation': 'project_list',
            'guide': f"{count}개 프로젝트 목록입니다. 특정 프로젝트 상세가 필요하면 brief <ID>를 호출하세요."})

    if command == 'dashboard':
        hints.append({'situation': 'dashboard_overview',
            'guide': '전체 현황 대시보드입니다. 긴급 사항은 urgent, 금액은 pipeline으로 확인하세요.'})

    if command == 'timeline':
        hints.append({'situation': 'timeline_view',
            'guide': '프로젝트 이력/일정입니다. 시간순으로 핵심 이벤트를 요약하세요.'})

    if command == 'pipeline':
        hints.append({'situation': 'pipeline_view',
            'guide': '금액/수주 현황입니다. 총액과 상위 프로젝트를 먼저 언급하세요.'})

    return hints


def _build_bundle(command, data, pid=None):
    """AI가 다음에 필요로 할 가능성 높은 데이터를 선제적으로 번들링 (E-4)"""
    bundle = {}
    if not isinstance(data, dict):
        return bundle

    three_days_ago = (datetime.now() - timedelta(days=3)).strftime('%Y-%m-%d')
    monday = (datetime.now() - timedelta(days=datetime.now().weekday())).strftime('%Y-%m-%d')

    if command == 'brief' and pid:
        # 이 프로젝트의 urgent 상태
        try:
            conn = get_db_connection(row_factory=True)
            try:
                # overdue 체크
                actions = conn.execute(
                    "SELECT content FROM chunks WHERE project_id=? AND chunk_type='next_action'", (pid,)
                ).fetchall()
                today = datetime.now().strftime('%Y-%m-%d')
                for a in actions:
                    dates = re.findall(r'20\d{2}[-/]\d{2}[-/]\d{2}', a['content'] or '')
                    for d in dates:
                        if d.replace('/', '-') <= today:
                            bundle['urgency'] = {'priority': 'overdue', 'reason': f'기한 도래: {d}'}
                            break
                # stale 체크
                last_comm = conn.execute(
                    "SELECT MAX(log_date) as d FROM comm_log WHERE project_id=?", (pid,)
                ).fetchone()
                if last_comm and last_comm['d']:
                    stale_cutoff = (datetime.now() - timedelta(days=30)).strftime('%Y-%m-%d')
                    if last_comm['d'] < stale_cutoff:
                        bundle['urgency'] = {'priority': 'stale', 'reason': f'마지막 활동: {last_comm["d"]}'}
                # 최근 3일 comms
                recent_3d = [c for c in data.get('recent_comms', []) if (c.get('date') or '') >= three_days_ago]
                if recent_3d:
                    bundle['recent_3d_comms'] = recent_3d
                # 관련 프로젝트 (같은 담당자)
                person = (data.get('person_internal') or '').strip()
                if person and person.split():
                    related = conn.execute(
                        "SELECT id, name, status FROM projects WHERE person_internal LIKE ? AND id != ? LIMIT 3",
                        (f"%{person.split()[0]}%", pid)
                    ).fetchall()
                    if related:
                        bundle['related_projects'] = [dict(r) for r in related]
            finally:
                conn.close()
        except Exception:
            pass

    if command == 'person':
        # 이번 주 활동
        comms = data.get('recent_communications', [])
        this_week = [c for c in comms if (c.get('date') or '') >= monday]
        bundle['this_week_activity'] = {'comm_count': len(this_week), 'comms': this_week[:5]}

    if command == 'search':
        # 단일 매칭 시 brief 데이터 선제 로드
        projects = data.get('projects') or []
        if len(projects) == 1:
            try:
                first_pid = projects[0].get('id') if isinstance(projects[0], dict) else None
                if first_pid is not None:
                    from commands.brief import _build_single_brief
                    bundle['auto_brief'] = _build_single_brief(first_pid)
            except Exception:
                pass

    if command == 'urgent':
        # top critical 항목의 brief 번들
        items = data.get('items', [])
        if items:
            top = items[0]
            top_pid = top.get('id') or top.get('project_id')
            if top_pid:
                try:
                    from commands.brief import _build_single_brief
                    bundle['top_brief'] = _build_single_brief(top_pid)
                except Exception:
                    pass

    if command == 'show':
        # 같은 발주처의 관련 프로젝트
        client = data.get('client')
        show_pid = data.get('id') or data.get('project_id')
        if client and show_pid:
            try:
                conn = get_db_connection(row_factory=True)
                try:
                    related = conn.execute(
                        "SELECT id, name, status FROM projects WHERE client LIKE ? AND id != ? LIMIT 3",
                        (f"%{_escape_like(client.split()[0] if client.split() else '')}%", show_pid)
                    ).fetchall()
                    if related:
                        bundle['same_client_projects'] = [dict(r) for r in related]
                finally:
                    conn.close()
            except Exception:
                pass

    return bundle


def _try_auto_correct(command, params, error_response):
    """흔한 오류 패턴에 대한 자동 교정 (E-7)"""
    global _auto_correct_depth
    _auto_correct_depth += 1
    try:
        return _try_auto_correct_inner(command, params, error_response)
    finally:
        _auto_correct_depth -= 1

def _try_auto_correct_inner(command, params, error_response):
    """자동 교정 내부 로직"""
    error_msg = (error_response.get('data') or {}).get('error', '')

    # 패턴 1: show/brief/timeline에 프로젝트명을 넣었는데 못 찾음 → search로 전환
    if '찾을 수 없습니다' in error_msg and command in ('show', 'brief', 'timeline'):
        query = params.get('query', '') or ' '.join(params.get('sub_args', []))
        if query:
            return execute('search', {'query': query})

    # 패턴 2: person에 프로젝트명을 넣음 → brief로 전환
    if command == 'person' and error_msg:
        name = params.get('name', '') or params.get('query', '')
        pid = _find_project_id(name)
        if pid:
            return execute('brief', {'sub_args': [str(pid)]})

    # 패턴 3: search 결과 0건 + suggestions 있음 → 첫 후보 brief
    if command == 'search':
        data = error_response.get('data', {})
        rc = data.get('result_count', {})
        if rc.get('projects', 0) == 0 and rc.get('communications', 0) == 0:
            # 패턴 4: _auto_brief가 이미 있으면 brief 응답으로 전환
            if data.get('_auto_brief') and data['_auto_brief'].get('project_id'):
                corrected = execute('brief', {'sub_args': [str(data['_auto_brief']['project_id'])]})
                if corrected.get('status') == 'ok':
                    return corrected
            suggestions = data.get('suggestions', {})
            if suggestions.get('projects'):
                first = suggestions['projects'][0]
                corrected = execute('brief', {'sub_args': [str(first.get('id', ''))]})
                if corrected.get('status') == 'ok':
                    return corrected

    # 패턴 5: timeline에 텍스트(프로젝트명) 넣었는데 ID가 아님 → fuzzy로 찾아서 재시도
    if command == 'timeline' and '찾을 수 없습니다' in error_msg:
        query = params.get('query', '') or ' '.join(params.get('sub_args', []))
        if query:
            pid, conf = _fuzzy_find_project(query)
            if pid:
                return execute('timeline', {'sub_args': [str(pid)]})

    return None


def _route_confidence(query, command):
    """NL 라우팅의 신뢰도 추정"""
    if not query:
        return 1.0
    # 명시적 명령어가 쿼리에 포함된 경우 높은 신뢰도
    if command in query.lower():
        return 0.95
    # NL 패턴으로 라우팅된 경우 중간 신뢰도
    for pattern, cmd in NL_ROUTES:
        if re.search(pattern, query):
            # weekly_or_changelog는 route_input에서 weekly 또는 changelog로 분기
            if cmd == command or (cmd == 'weekly_or_changelog' and command in ('weekly', 'changelog')):
                return 0.8
    # 기본 search 폴백은 낮은 신뢰도
    if command == 'search':
        return 0.6
    return 0.7


def _smart_route(args):
    """route_input + 신뢰도 기반 폴백.
    낮은 신뢰도 search 라우팅에서 프로젝트명이 감지되면 brief로 전환.
    """
    cmd, params = route_input(args)
    query = params.get('query', '') or ' '.join(args)
    conf = _route_confidence(query, cmd)

    # 신뢰도 낮은 search 라우팅 + 프로젝트명 감지 → brief 전환
    if conf <= 0.65 and cmd == 'search':
        pid, fuzzy_conf = _fuzzy_find_project(query)
        if pid and fuzzy_conf >= 0.7:
            return 'brief', {'sub_args': [str(pid)], '_from_smart_route': True}

    return cmd, params


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# Format + Summary
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

def _apply_format(command, data, fmt):
    """용도별 출력 변환. summary(기본), detail, markdown, ids."""
    if fmt == 'ids':
        projects = data.get('projects', [])
        if projects and isinstance(projects[0], dict):
            return {'ids': [p.get('id') or p.get('project_id') for p in projects]}
        return data

    if fmt == 'markdown':
        projects = data.get('projects', [])
        if not projects:
            return data
        header = "| ID | 프로젝트 | 상태 | 담당 |"
        sep = "|---|---|---|---|"
        rows = [
            f"| {p.get('id','')} | {p.get('name','')} | {p.get('status','')} | {p.get('person','') or p.get('person_internal','')} |"
            for p in projects
        ]
        data['markdown'] = '\n'.join([header, sep] + rows)
        return data

    if fmt == 'detail':
        projects = data.get('projects', [])
        data['lines'] = [
            f"[{p.get('id','')}] {p.get('name','')} — {p.get('status','')}"
            for p in projects
        ]
        return data

    return data


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# 4. 자동 요약 생성 (AI가 바로 사용할 수 있는 한 줄 요약)
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

def _generate_summary(command, data):
    """AI가 바로 응답에 활용할 수 있는 한 줄 요약"""
    if not data:
        return '결과 없음'

    # 레지스트리에 summary_fn이 있으면 우선 사용
    entry = _COMMAND_REGISTRY.get(command)
    if entry and isinstance(entry, dict) and entry.get('summary_fn'):
        try:
            return entry['summary_fn'](data)
        except Exception:
            pass  # 폴백: 아래 기존 로직

    # ask는 _meta.inner_summary를 사용하는 특수 케이스
    if command == 'ask':
        meta = data.get('_meta', {})
        return meta.get('inner_summary', data.get('summary', ''))

    # 레지스트리 기반 폴백: data에 summary가 있으면 사용
    if isinstance(data, dict) and 'summary' in data:
        return data['summary']

    return ''


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# Public aliases — backward compatibility
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
_publish({
    'find_project_id': _find_project_id,
    'find_project_id_in_text': _find_project_id_in_text,
    'fuzzy_find_project': _fuzzy_find_project,
    'get_flag': _get_flag,
    'escape_like': _escape_like,
    'extract_days': _extract_days,
    'extract_limit': _extract_limit,
    'extract_bullets': _extract_bullets,
    'build_search_suggestions': _build_search_suggestions,
    'ensure_db': _ensure_db,
    'load_session': _load_session,
    'save_session': _save_session,
    'update_session': _update_session,
    'resolve_session_context': _resolve_session_context,
    'apply_depth': _apply_depth,
    'build_ai_hint': _build_ai_hint,
    'build_bundle': _build_bundle,
    'try_auto_correct': _try_auto_correct,
    'route_confidence': _route_confidence,
    'smart_route': _smart_route,
    'apply_format': _apply_format,
    'generate_summary': _generate_summary,
    'addon_command': addon_command,
    'require_project': require_project,
    'SEARCH_HINT_STOPWORDS': _SEARCH_HINT_STOPWORDS,
})


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# 명령 자동 로드 (commands/ 디렉토리)
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

def _load_commands():
    """commands/ 디렉토리의 모든 .py 파일을 자동 import하여 @register_command 실행."""
    commands_dir = SELF_DIR / 'commands'
    if not commands_dir.is_dir():
        return
    for f in sorted(commands_dir.glob('*.py')):
        if f.name.startswith('_'):
            continue
        module_name = f'commands.{f.stem}'
        try:
            __import__(module_name)
        except Exception as e:
            import traceback
            print(f"[warn] 명령 모듈 로드 실패: {module_name}: {e}", file=sys.stderr)

_load_commands()


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# 5. CLI
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

def main():
    # Memory backend: bare JSON stdout, stderr errors, exit codes
    if len(sys.argv) > 1 and sys.argv[1].startswith('memory-'):
        _run_memory_command()
        return

    human_mode = '--human' in sys.argv
    args = [a for a in sys.argv[1:] if a not in ('--human', '--json')]

    command, params = route_input(args)
    response = execute(command, params)

    if human_mode:
        # 사람용 출력 (디버깅)
        print(f"\n[{command}] {response['summary']}")
        print(json.dumps(response['data'], ensure_ascii=False, indent=2, default=_json_ser)[:3000])
    else:
        # AI용 JSON 출력 (기본)
        print(json.dumps(response, ensure_ascii=False, default=_json_ser))


def _run_memory_command():
    """OpenClaw memory backend — bare JSON 출력, exit code 에러."""
    args = sys.argv[1:]
    cmd = args[0]

    # 파라미터 파싱
    params = {'sub_args': args[1:]}
    if cmd == 'memory-search':
        query_args = [a for a in args[1:] if not a.startswith('--')]
        params['query'] = ' '.join(query_args)
        params['limit'] = _get_flag(args, '--limit') or '6'
        params['collection'] = _get_flag(args, '--collection')
        params['mode'] = _get_flag(args, '--mode')  # v1.48: search/vsearch/query
    # memory-update, memory-embed: --force는 sub_args에서 처리
    # memory-status, memory-version: 파라미터 없음

    try:
        _ensure_db()
        entry = _COMMAND_REGISTRY.get(cmd)
        if not entry:
            print(f"Unknown command: {cmd}", file=sys.stderr)
            sys.exit(1)
        handler = entry['handler'] if isinstance(entry, dict) else entry[0]
        result = handler(params)
        if result is not None:
            print(json.dumps(result, ensure_ascii=False, default=_json_ser))
        sys.exit(0)
    except Exception as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)


def _json_ser(obj):
    if isinstance(obj, set): return sorted(obj)
    if isinstance(obj, Path): return str(obj)
    return str(obj)


if __name__ == '__main__':
    main()
