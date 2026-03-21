import config
from collections import OrderedDict
from core import register_command, _get_flag, _build_search_suggestions, _SEARCH_HINT_STOPWORDS


def _search_summary(data):
    rc = data.get('result_count') or {}
    top_names = [p['name'] for p in (data.get('projects') or [])[:3]]
    names_str = ', '.join(top_names)
    if len(data.get('projects') or []) > 3:
        names_str += f' 외 {len(data["projects"]) - 3}개'
    sm = data.get('search_meta', {})
    qmd_note = ''
    if sm.get('qmd_used') and sm.get('qmd_count', 0) > 0:
        qmd_note = f", QMD {sm['qmd_count']}건 포함"
    if names_str:
        return f"검색 결과: {names_str} ({rc.get('projects',0)}개 프로젝트, {rc.get('communications',0)}건 커뮤니케이션{qmd_note})"
    return f"검색 결과: {rc.get('projects',0)}개 프로젝트, {rc.get('communications',0)}건 커뮤니케이션{qmd_note}"


@register_command('search', summary_fn=_search_summary)
def _exec_search(params):
    from router import SearchRouter
    query = params.get('query', '')
    sub_args = params.get('sub_args', [])
    if sub_args:
        query = ' '.join(sub_args)

    if not query or not query.strip():
        return {
            'query': '', 'projects': [], 'communications': [],
            'result_count': {'projects': 0, 'communications': 0},
            'summary': '검색어를 입력하세요. 예: search "비금도 케이블"'
        }

    router = SearchRouter(db_path=config.DB_PATH)
    results = router.search(query)

    # 구조화된 응답 — analysis 대신 search_meta 사용
    out = {
        'query': query,
        'search_meta': results.get('search_meta', {}),
        'projects': [],
        'communications': [],
    }

    # 통합 결과 → 프로젝트별 단일 그룹핑 (SQLite + QMD 합침)
    unified = results.get('unified', [])
    grouped = OrderedDict()
    for r in unified:
        pid = r['project_id']
        if not pid:
            # project_id 없는 QMD 결과 → qmd_extra로 분리
            continue
        if pid not in grouped:
            grouped[pid] = {
                'id': pid, 'name': r['project_name'], 'client': r['client'],
                'status': r['status'], 'person': r['person'],
                'sections': [], 'sources': set(), 'score': r['score'],
            }
        grouped[pid]['sources'].add(r['source'])
        # 점수는 최대값 사용
        if r['score'] > grouped[pid]['score']:
            grouped[pid]['score'] = r['score']
        grouped[pid]['sections'].append({
            'heading': r['heading'],
            'content': r['content'][:500],
            'type': r['chunk_type'],
            'date': r['entry_date'],
            'source': r['source'],
        })

    # project_id 없는 QMD 결과 (추론 실패)
    qmd_extra = [r for r in unified if not r['project_id'] and r['source'] == 'qmd']

    # 랭킹 순서 유지
    ranked_ids = results.get('sqlite', {}).get('project_ids') or list(grouped.keys())
    # grouped에 있지만 ranked_ids에 없는 항목 추가 (고아 QMD 복구)
    for pid in grouped:
        if pid not in ranked_ids:
            ranked_ids.append(pid)

    out['projects'] = []
    for pid in ranked_ids:
        if pid in grouped:
            proj = grouped[pid]
            proj['sources'] = sorted(proj['sources'])  # set → list
            # match_reasons: 왜 이 프로젝트가 매칭됐는지
            reasons = []
            query_lower = query.lower()
            if (proj.get('name') or '').lower() in query_lower or query_lower in (proj.get('name') or '').lower():
                reasons.append('프로젝트명')
            if 'sqlite' in proj['sources']:
                has_comm = any(s.get('type') == 'comm_log' for s in proj.get('sections', []))
                if has_comm:
                    reasons.append('커뮤니케이션')
                if not has_comm or len(proj.get('sections', [])) > 1:
                    reasons.append('본문')
            if 'qmd' in proj['sources']:
                reasons.append('의미검색')
            proj['match_reasons'] = reasons or ['키워드']
            out['projects'].append(proj)

    # project_id 없는 QMD 결과만 별도 (최소한의 분리)
    if qmd_extra:
        out['qmd_extra'] = [
            {'title': r['heading'], 'content': r['content'][:500],
             'score': r['score'], 'source': r['metadata'].get('filepath', '')}
            for r in qmd_extra
        ]

    # 커뮤니케이션 (최신순 정렬 + 상위 10건 제한)
    comms = results.get('comms', [])
    if not comms and results.get('sqlite'):
        comms = results['sqlite'].get('comms', [])
    if comms:
        # 최신순 정렬 (DB에서 이미 정렬되지만, 합산 시 순서 보장)
        comms_sorted = sorted(comms, key=lambda r: r['log_date'] or '', reverse=True)
        _COMM_DISPLAY_LIMIT = 10
        out['communications'] = [
            {'date': r['log_date'], 'project': r['name'],
             'sender': r['sender'], 'subject': r['subject'][:100]}
            for r in comms_sorted[:_COMM_DISPLAY_LIMIT]
        ]
        if len(comms_sorted) > _COMM_DISPLAY_LIMIT:
            out['communications_total'] = len(comms_sorted)
            out['communications_note'] = f"최신 {_COMM_DISPLAY_LIMIT}건 표시 (전체 {len(comms_sorted)}건)"

    out['result_count'] = {
        'projects': len(out['projects']),
        'communications': len(out['communications']),
    }

    # 매칭된 키워드 추출 (전처리된 키워드 우선 사용 — 조사 제거 반영)
    analysis = results.get('analysis', {})
    extracted_all = analysis.get('extracted', {})
    # 모든 추출 토큰 (clients, persons, statuses, keywords) 통합
    keywords = list(extracted_all.get('keywords', []))
    for group in ('clients', 'persons', 'statuses', 'tags'):
        keywords.extend(extracted_all.get(group, []))
    if not keywords:
        keywords = query.split()
    matched = set()
    for p in out['projects']:
        text = (p.get('name', '') + ' ' + ' '.join(
            s.get('content', '') for s in p.get('sections', []))).lower()
        for kw in keywords:
            if kw.lower() in text:
                matched.add(kw)
    for c in out['communications']:
        text = (c.get('subject', '') + ' ' + c.get('sender', '') + ' ' + c.get('project', '')).lower()
        for kw in keywords:
            if kw.lower() in text:
                matched.add(kw)
    out['matched_keywords'] = list(matched)

    total_hits = sum(out['result_count'].values())
    if total_hits == 0:
        # fuzzy 프로젝트명 매칭 시도 → auto_brief 첨부
        from core import _fuzzy_find_project
        fz_pid, fz_conf = _fuzzy_find_project(query)
        if fz_pid and fz_conf >= 0.6:
            try:
                from commands.brief import _build_single_brief
                out['_auto_brief'] = _build_single_brief(fz_pid)
                out['_auto_brief']['_match_confidence'] = fz_conf
            except Exception:
                pass
        out['suggestions'] = _build_search_suggestions(query)
        # 0건일 때 대안 명령 안내 (시간/금액 등 검색으로 풀 수 없는 쿼리)
        import re as _re
        _alt_hints = []
        if _re.search(r'(이번|다음|금|이)\s*(달|월|주)|할\s*일|일정|액션|해야', query):
            _alt_hints.append('urgent (긴급/할일 조회) 또는 timeline <프로젝트> (일정 조회)')
        if _re.search(r'(억|만원|금액|매출|수주|얼마|이상|미만|초과)', query):
            _alt_hints.append('pipeline (금액/수주 현황 조회)')
        if _alt_hints:
            out['alternative_commands'] = _alt_hints
            out['follow_up_hint'] = '검색 결과 없음. 대안: ' + ' / '.join(_alt_hints)
        elif out['suggestions']:
            out['follow_up_hint'] = 'show <ID>, brief <프로젝트명>, person <이름> 형태로 바로 이어서 조회할 수 있습니다.'
    elif out['projects']:
        top = out['projects'][0]
        # 맥락별 힌트 (v1.34)
        persons_in_query = extracted_all.get('persons', [])
        if persons_in_query:
            out['follow_up_hint'] = f"person {persons_in_query[0]} (포트폴리오 조회) / show {top['id']} / brief {top['id']}"
        elif out.get('communications') and not out['projects']:
            out['follow_up_hint'] = '커뮤니케이션만 매칭됨. show <프로젝트ID>로 프로젝트 상세 확인 가능'
        elif len(out['projects']) > 5:
            out['follow_up_hint'] = f"결과 {len(out['projects'])}건. --min-score로 필터 가능. show {top['id']} / brief {top['id']}"
        else:
            out['follow_up_hint'] = f"show {top['id']} / brief {top['id']} / timeline {top['id']}"

    # match_methods 전달
    if results.get('sqlite') and results['sqlite'].get('match_methods'):
        out['search_meta']['match_methods'] = results['sqlite']['match_methods']

    # min_score 필터
    min_score = params.get('min_score')
    if min_score is None:
        sub_args = params.get('sub_args', []) or []
        ms_val = _get_flag(sub_args, '--min-score')
        if ms_val:
            try:
                min_score = float(ms_val)
            except ValueError:
                pass
    if min_score is not None and out['projects']:
        before = len(out['projects'])
        out['projects'] = [p for p in out['projects'] if (p.get('score') or 0) >= min_score]
        out['filtered_out'] = before - len(out['projects'])
        out['result_count']['projects'] = len(out['projects'])

    return out
