from core import (register_command, execute, route_input, get_db_connection, VegaError,
                  _resolve_session_context, _apply_depth, _build_ai_hint, _build_bundle,
                  _update_session, _try_auto_correct, _route_confidence, _smart_route)


@register_command('ask')
def _exec_ask(params):
    """AI가 사용자의 자연어를 그대로 전달하는 통합 엔드포인트 (E-1)."""
    from core import _auto_correct_depth

    query = (params.get('query', '') or '').strip()
    if not query:
        raise VegaError('query 파라미터가 필요합니다.', usage=['ask "비금도 어떻게 되고 있어?"'])

    depth = params.get('depth', 'normal')
    context = params.get('context')

    # 세션 컨텍스트에서 대명사 해석 (E-5)
    recent_ids = _resolve_session_context(query, context)
    if recent_ids and any(p in query for p in ['그 프로젝트', '거기', '그거', '아까 그', '방금 그']):
        conn = get_db_connection(row_factory=True)
        try:
            row = conn.execute("SELECT name FROM projects WHERE id=?", (recent_ids[0],)).fetchone()
            if row:
                name = row['name'] or ''
                for pronoun in ['그 프로젝트', '거기', '그거', '아까 그', '방금 그']:
                    query = query.replace(pronoun, name)
        finally:
            conn.close()

    # NL 라우팅 (스마트: 신뢰도 낮은 search → 프로젝트 brief 자동 전환)
    command, routed_params = _smart_route(query.split())

    # 순환 방지: route_input이 'ask'를 반환하면 'search'로 폴백
    if command == 'ask':
        command = 'search'
        routed_params = {'query': query}

    # 내부 명령 실행 (execute 통해서 — 에러 핸들링/summary 포함)
    inner = execute(command, routed_params)

    # depth 적용 (E-2)
    data = inner.get('data', {})
    if depth != 'normal' and inner.get('status') == 'ok' and data:
        data = _apply_depth(data, command, depth)

    # 결과 조립
    out = data if isinstance(data, dict) else {'_raw': data}

    out['_meta'] = {
        'routed_to': command,
        'routed_params': {k: v for k, v in routed_params.items() if k != 'context'},
        'confidence': _route_confidence(query, command),
        'inner_status': inner.get('status'),
        'inner_summary': inner.get('summary'),
    }

    # AI 행동 가이드 (E-3)
    if inner.get('status') == 'ok' and data:
        ai_hints = _build_ai_hint(command, data, query)
        if ai_hints:
            out['_ai_hint'] = ai_hints

        # 선제 번들링 (E-4) — depth=brief이면 생략
        if depth != 'brief':
            pid = data.get('project_id') or data.get('id') if isinstance(data, dict) else None
            bundle = _build_bundle(command, data, pid)
            if bundle:
                out['_bundle'] = bundle

        # 세션 업데이트 (E-5)
        _update_session(command, data)

    # 오류 자동 교정 (E-7)
    if inner.get('status') == 'error':
        corrected = _try_auto_correct(command, routed_params, inner)
        if corrected and corrected.get('status') == 'ok':
            out = corrected.get('data', {}) if isinstance(corrected.get('data'), dict) else {'_raw': corrected.get('data')}
            out['_meta'] = {
                'routed_to': command,
                'auto_corrected_to': corrected.get('command'),
                'confidence': 0.5,
                'inner_status': 'ok',
                'inner_summary': corrected.get('summary'),
            }
            _update_session(corrected.get('command', command), out)
        else:
            out['_meta']['inner_status'] = 'error'
    elif inner.get('status') == 'ok' and command == 'search' and _auto_correct_depth < 1:
        # 검색 결과 0건이면 status는 'ok'이지만 suggestions 기반 자동 교정 시도
        corrected = _try_auto_correct(command, routed_params, inner)
        if corrected and corrected.get('status') == 'ok':
            out = corrected.get('data', {}) if isinstance(corrected.get('data'), dict) else {'_raw': corrected.get('data')}
            out['_meta'] = {
                'routed_to': command,
                'auto_corrected_to': corrected.get('command'),
                'confidence': 0.5,
                'inner_status': 'ok',
                'inner_summary': corrected.get('summary'),
            }
            _update_session(corrected.get('command', command), out)

    out['summary'] = inner.get('summary', '')
    return out
