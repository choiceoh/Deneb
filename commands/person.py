from core import register_command, get_db_connection, escape_like


@register_command('person', read_only=True, category='query',
    summary_fn=lambda d: f"{d.get('person','?')}: 프로젝트 {d.get('project_count',0)}개, 최근 커뮤니케이션 {d.get('comm_count',0)}건")
def _exec_person(params):
    """인물 중심 프로젝트 조회"""
    sub_args = params.get('sub_args', [])
    query = ' '.join(sub_args) if sub_args else params.get('name', params.get('query', ''))
    if not query:
        return {
            'error': '이름을 지정해주세요',
            'usage': 'person "고건"  또는  person "Christina"',
        }

    # 자연어 노이즈 제거
    noise = {'담당', '프로젝트', '뭐', '하고', '있', '어', '의', '것'}
    q_words = [w for w in query.split() if w not in noise]
    name = ' '.join(q_words) if q_words else query

    conn = get_db_connection(row_factory=True)
    try:
        # 1. 내부 담당 프로젝트
        escaped_name = escape_like(name)
        internal = conn.execute(
            "SELECT id, name, status, client, capacity FROM projects WHERE person_internal LIKE ? ESCAPE '\\'",
            (f"%{escaped_name}%",)
        ).fetchall()

        # 2. 외부 담당 프로젝트
        external = conn.execute(
            "SELECT id, name, status, client, capacity FROM projects WHERE person_external LIKE ? ESCAPE '\\'",
            (f"%{escaped_name}%",)
        ).fetchall()

        # 3. comm_log에서 커뮤니케이션 이력
        comms = conn.execute("""
            SELECT c.log_date, c.subject, c.summary, p.name as project_name, p.id as project_id
            FROM comm_log c JOIN projects p ON c.project_id = p.id
            WHERE c.sender LIKE ? ESCAPE '\\'
            ORDER BY c.log_date DESC
            LIMIT 20
        """, (f"%{escaped_name}%",)).fetchall()
    finally:
        conn.close()

    # 프로젝트 목록 (중복 제거)
    internal_ids = {r['id'] for r in internal}
    seen = set()
    projects = []
    for r in list(internal) + list(external):
        if r['id'] not in seen:
            seen.add(r['id'])
            role = '내부 담당' if r['id'] in internal_ids else '외부 담당'
            projects.append({
                'id': r['id'], 'name': r['name'], 'status': r['status'],
                'client': r['client'], 'capacity': r['capacity'], 'role': role,
            })

    recent_comms = [
        {'date': c['log_date'], 'subject': c['subject'],
         'project': c['project_name'], 'project_id': c['project_id']}
        for c in comms
    ]

    return {
        'person': name,
        'project_count': len(projects),
        'projects': projects,
        'recent_communications': recent_comms,
        'comm_count': len(recent_comms),
    }
