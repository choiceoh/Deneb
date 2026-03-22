from core import register_command, get_db_connection, require_project, _get_flag, _escape_like


@register_command('timeline',
    summary_fn=lambda d: f"{d.get('project_name','?')} 타임라인: {d.get('total_entries',0)}건")
def _exec_timeline(params):
    pid = require_project(params,
        usage_hint='프로젝트를 특정할 수 없습니다. ID 또는 프로젝트명을 포함해주세요.')
    if isinstance(pid, dict):
        pid['usage'] = 'timeline 5  또는  timeline "비금도"'
        return pid

    conn = get_db_connection(row_factory=True)
    try:
        proj = conn.execute("SELECT name FROM projects WHERE id=?", (pid,)).fetchone()
        comms = conn.execute("""
            SELECT log_date, sender, subject, summary
            FROM comm_log WHERE project_id=? ORDER BY log_date DESC
        """, (pid,)).fetchall()
    finally:
        conn.close()

    return {
        'project_id': pid,
        'project_name': proj['name'] if proj else '?',
        'total_entries': len(comms),
        'timeline': [{'date': c['log_date'], 'sender': c['sender'],
                      'subject': c['subject'], 'summary': (c['summary'] or '')[:200]}
                     for c in comms],
    }


@register_command('show',
    summary_fn=lambda d: f"[{d.get('id','')}] {d.get('name','')} — {d.get('status','')}")
def _exec_show(params):
    pid = require_project(params,
        usage_hint='프로젝트 ID 또는 이름을 지정해주세요.')
    if isinstance(pid, dict):
        pid['usage'] = 'show 5  또는  show "비금도"'
        return pid

    conn = get_db_connection(row_factory=True)
    try:
        proj = conn.execute("SELECT * FROM projects WHERE id=?", (pid,)).fetchone()
        if not proj:
            return {'error': f'프로젝트 ID {pid} 없음'}

        chunks = conn.execute("SELECT section_heading, content, chunk_type, entry_date FROM chunks WHERE project_id=? AND chunk_type != 'comm_log' ORDER BY id", (pid,)).fetchall()
        comms = conn.execute("SELECT log_date, sender, subject, summary FROM comm_log WHERE project_id=? ORDER BY log_date DESC LIMIT 10", (pid,)).fetchall()
        tags = conn.execute("""
            SELECT DISTINCT t.name FROM tags t
            JOIN chunk_tags ct ON ct.tag_id=t.id JOIN chunks c ON c.id=ct.chunk_id
            WHERE c.project_id=?
        """, (pid,)).fetchall()
    finally:
        conn.close()

    return {
        'id': pid,
        'name': proj['name'], 'client': proj['client'], 'status': proj['status'],
        'capacity': proj['capacity'], 'biz_type': proj['biz_type'],
        'person_internal': proj['person_internal'], 'person_external': proj['person_external'],
        'partner': proj['partner'],
        'tags': [t['name'] for t in tags],
        'sections': [{'heading': c['section_heading'], 'content': (c['content'] or '')[:800],
                      'type': c['chunk_type']} for c in chunks],
        'recent_comms': [{'date': c['log_date'], 'sender': c['sender'],
                         'subject': c['subject'], 'summary': (c['summary'] or '')[:200]}
                        for c in comms],
        'recommended_commands': [f'brief {pid}', f'timeline {pid}', f'recent {pid}'],
    }


def _list_summary(data):
    filters = data.get('filters')
    if filters:
        filter_desc = ', '.join(f'{k}={v}' for k, v in filters.items())
        return f"{data.get('total',0)}개 프로젝트 (필터: {filter_desc})"
    return f"전체 {data.get('total',0)}개 프로젝트"


@register_command('list', summary_fn=_list_summary)
def _exec_list(params=None):
    params = params or {}
    sub_args = params.get('sub_args', []) or []

    status_filter = _get_flag(sub_args, '--status') or params.get('status')
    person_filter = _get_flag(sub_args, '--person') or params.get('person')
    client_filter = _get_flag(sub_args, '--client') or params.get('client')

    conditions = []
    bind = []
    if status_filter:
        conditions.append("p.status LIKE ? ESCAPE '\\'")
        bind.append(f"%{_escape_like(status_filter)}%")
    if person_filter:
        conditions.append("p.person_internal LIKE ? ESCAPE '\\'")
        bind.append(f"%{_escape_like(person_filter)}%")
    if client_filter:
        conditions.append("(p.client LIKE ? ESCAPE '\\' OR p.name LIKE ? ESCAPE '\\')")
        bind.extend([f"%{_escape_like(client_filter)}%", f"%{_escape_like(client_filter)}%"])

    where = " WHERE " + " AND ".join(conditions) if conditions else ""

    conn = get_db_connection()
    try:
        rows = conn.execute(f"""
            SELECT p.id, p.name, p.client, p.status, p.person_internal, p.capacity,
                   (SELECT COUNT(*) FROM chunks WHERE project_id=p.id) as chunks,
                   (SELECT COUNT(*) FROM comm_log WHERE project_id=p.id) as comms
            FROM projects p{where} ORDER BY p.id
        """, bind).fetchall()
    finally:
        conn.close()

    filters_applied = {}
    if status_filter: filters_applied['status'] = status_filter
    if person_filter: filters_applied['person'] = person_filter
    if client_filter: filters_applied['client'] = client_filter

    result = {
        'total': len(rows),
        'projects': [
            {'id': r[0], 'name': r[1], 'client': r[2], 'status': r[3],
             'person': r[4], 'capacity': r[5], 'sections': r[6], 'comms': r[7]}
            for r in rows
        ],
    }
    if filters_applied:
        result['filters'] = filters_applied
    return result
