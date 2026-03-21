import re
from core import register_command, get_db_connection, _find_project_id, _find_project_id_in_text, _fuzzy_find_project, _extract_bullets, _extract_days, _extract_limit


def _build_single_brief(pid):
    """단일 프로젝트 브리프 생성 (brief 멀티 모드에서도 재사용)."""
    conn = get_db_connection(row_factory=True)
    try:
        proj = conn.execute("SELECT * FROM projects WHERE id=?", (pid,)).fetchone()
        if not proj:
            return {'error': f'프로젝트 ID {pid} 없음'}

        chunks = conn.execute("""
            SELECT section_heading, content, chunk_type, entry_date
            FROM chunks
            WHERE project_id=? AND chunk_type != 'comm_log'
            ORDER BY COALESCE(entry_date, '0000-00-00') DESC, id DESC
        """, (pid,)).fetchall()
        comms = conn.execute("""
            SELECT log_date, sender, subject, summary
            FROM comm_log WHERE project_id=?
            ORDER BY log_date DESC, id DESC LIMIT 5
        """, (pid,)).fetchall()
    finally:
        conn.close()

    bucket = {}
    for ch in chunks:
        bucket.setdefault(ch['chunk_type'] or 'other', []).append(ch)

    next_actions = []
    for ch in bucket.get('next_action', [])[:3]:
        next_actions.extend(_extract_bullets(ch['content'], limit=3))
    next_actions = next_actions[:5]

    risks = []
    for ch in bucket.get('issue', [])[:3]:
        risks.extend(_extract_bullets(ch['content'], limit=3))
    if not risks:
        for ch in bucket.get('status', [])[:2]:
            for line in _extract_bullets(ch['content'], limit=4):
                if re.search(r'(이슈|리스크|지연|미정|보류|주의|대응)', line):
                    risks.append(line)
    risks = risks[:4]

    key_points = []
    for ctype in ('summary', 'status', 'history', 'technical'):
        for ch in bucket.get(ctype, [])[:2]:
            key_points.extend(_extract_bullets(ch['content'], limit=2))
        if len(key_points) >= 6:
            break
    deduped = []
    for point in key_points:
        if point not in deduped:
            deduped.append(point)
    key_points = deduped[:6]

    recent_comms = [{
        'date': c['log_date'],
        'sender': c['sender'],
        'subject': c['subject'],
        'summary': (c['summary'] or '')[:160],
    } for c in comms[:3]]

    dates = [d for d in [*(c['log_date'] for c in comms), *(ch['entry_date'] for ch in chunks if ch['entry_date'])] if d]
    latest_activity = max(dates) if dates else None

    return {
        'project_id': pid,
        'project_name': proj['name'],
        'client': proj['client'],
        'status': proj['status'],
        'capacity': proj['capacity'],
        'biz_type': proj['biz_type'],
        'person_internal': proj['person_internal'],
        'person_external': proj['person_external'],
        'partner': proj['partner'],
        'latest_activity': latest_activity,
        'next_actions': next_actions,
        'risks': risks,
        'key_points': key_points,
        'recent_comms': recent_comms,
        'recommended_commands': [
            f'show {pid}',
            f'timeline {pid}',
            f'search {proj["name"]}',
        ],
    }


def _brief_summary(d):
    if 'briefs' in d:
        return d.get('summary', f"{d.get('count', 0)}개 프로젝트 브리프")
    return f"[{d.get('project_id','')}] {d.get('project_name','?')} — 액션 {len(d.get('next_actions', []))}개, 리스크 {len(d.get('risks', []))}개"


@register_command('brief', summary_fn=_brief_summary)
def _exec_brief(params):
    """프로젝트 핵심만 빠르게 보는 원페이지 브리프. 멀티 프로젝트 지원."""
    sub_args = params.get('sub_args', []) or []
    refs = [a for a in sub_args if not a.startswith('--')]

    # 멀티 프로젝트 감지
    multi_refs = params.get('projects', [])
    if not multi_refs and len(refs) >= 2:
        multi_refs = refs
    if multi_refs:
        briefs = []
        for ref in multi_refs:
            pid = _find_project_id(ref) or _find_project_id_in_text(ref)
            if pid:
                briefs.append(_build_single_brief(pid))
            else:
                briefs.append({'error': f'프로젝트 미발견: {ref}'})
        return {'briefs': briefs, 'count': len(briefs),
                'summary': f"{len(briefs)}개 프로젝트 브리프"}

    # 단일 프로젝트
    ref = ' '.join(refs).strip()
    if not ref:
        ref = params.get('project', '') or params.get('query', '')

    pid = _find_project_id(ref) or _find_project_id_in_text(ref)
    if not pid:
        # fuzzy 폴백
        pid, _conf = _fuzzy_find_project(ref)
    if not pid:
        return {
            'error': '프로젝트를 특정할 수 없습니다. ID 또는 프로젝트명을 포함해주세요.',
            'usage': 'brief 5  또는  brief "비금도"',
        }

    return _build_single_brief(pid)


def _recent_summary(d):
    pf = d.get('project_filter') or {}
    scope = pf.get('name') or '전체'
    return f"최근 {d.get('period',{}).get('days',0)}일 {scope} 활동 {d.get('total_events',0)}건"


@register_command('recent', summary_fn=_recent_summary)
def _exec_recent(params):
    """최근 활동/업데이트 피드. --days, --limit 지원."""
    days = _extract_days(params, default=7, max_days=90)
    limit = _extract_limit(params, default=20, max_limit=100)
    sub_args = params.get('sub_args', []) or []
    query = params.get('query', '') or ''
    ref_parts = []
    skip_next = False
    for i, arg in enumerate(sub_args):
        if skip_next:
            skip_next = False
            continue
        if arg in ('--days', '--limit'):
            skip_next = True
            continue
        if not arg.startswith('--'):
            ref_parts.append(arg)
    ref = ' '.join(ref_parts).strip() or query

    pid = _find_project_id(ref) or _find_project_id_in_text(ref)

    conn = get_db_connection(row_factory=True)
    try:
        sql_filter = ''
        bind = [f'-{days} days']
        if pid:
            sql_filter = ' AND p.id = ?'
            bind.append(pid)

        comms = conn.execute(f"""
            SELECT p.id as project_id, p.name as project_name, cl.log_date as activity_date,
                   'communication' as activity_type, cl.sender as actor, cl.subject as title,
                   COALESCE(cl.summary, '') as detail
            FROM comm_log cl
            JOIN projects p ON p.id = cl.project_id
            WHERE cl.log_date >= date('now', ?){sql_filter}
            ORDER BY cl.log_date DESC, cl.id DESC
            LIMIT ?
        """, (*bind, limit)).fetchall()

        chunk_bind = [f'-{days} days']
        if pid:
            chunk_bind.append(pid)
        chunks = conn.execute(f"""
            SELECT p.id as project_id, p.name as project_name, c.entry_date as activity_date,
                   c.chunk_type as activity_type, '' as actor,
                   COALESCE(c.section_heading, c.chunk_type) as title,
                   substr(replace(COALESCE(c.content, ''), char(10), ' '), 1, 180) as detail
            FROM chunks c
            JOIN projects p ON p.id = c.project_id
            WHERE c.entry_date IS NOT NULL
              AND c.entry_date >= date('now', ?)
              AND c.chunk_type IN ('next_action', 'history', 'issue', 'status')
              {sql_filter}
            ORDER BY c.entry_date DESC, c.id DESC
            LIMIT ?
        """, (*chunk_bind, limit)).fetchall()

        project_name = None
        if pid:
            row = conn.execute('SELECT name FROM projects WHERE id=?', (pid,)).fetchone()
            project_name = row['name'] if row else None
    finally:
        conn.close()

    events = [dict(r) for r in list(comms) + list(chunks)]
    events.sort(key=lambda x: (x.get('activity_date') or '', x.get('project_name') or ''), reverse=True)
    events = events[:limit]

    by_project = {}
    for ev in events:
        rec = by_project.setdefault(ev['project_id'], {
            'project_id': ev['project_id'],
            'project_name': ev['project_name'],
            'events': 0,
            'last_activity': ev.get('activity_date'),
            'communication_count': 0,
            'action_count': 0,
        })
        rec['events'] += 1
        if ev.get('activity_type') == 'communication':
            rec['communication_count'] += 1
        else:
            rec['action_count'] += 1
        if ev.get('activity_date') and (not rec['last_activity'] or ev['activity_date'] > rec['last_activity']):
            rec['last_activity'] = ev['activity_date']

    projects = sorted(by_project.values(), key=lambda x: (-x['events'], x['project_name'] or ''))

    return {
        'period': {'days': days},
        'project_filter': {'id': pid, 'name': project_name} if pid else None,
        'total_events': len(events),
        'projects': projects,
        'events': events,
    }
