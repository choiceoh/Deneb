from core import register_command, get_db_connection
import re
from datetime import datetime


def _urgent_summary(d):
    c = d.get('critical', 0)
    o = d.get('overdue', 0)
    s = d.get('stale', 0)
    top_items = [i['project_name'] for i in (d.get('items') or [])[:3] if i.get('project_name')]
    names_part = f" — {', '.join(top_items)}" if top_items else ""
    return f"관심 필요 {d.get('total',0)}건 (긴급 {c}, 기한초과 {o}, 미활동 {s}){names_part}"


@register_command('urgent', read_only=True, category='query', summary_fn=_urgent_summary)
def _exec_urgent(params):
    """긴급/관심 필요 프로젝트 목록"""
    conn = get_db_connection(row_factory=True)
    try:
        urgent_items = []

        # 1. 🔴 상태 프로젝트
        red = conn.execute(
            "SELECT id, name, status, person_internal FROM projects WHERE status LIKE '%🔴%' OR status LIKE '%긴급%' OR status LIKE '%중단%'"
        ).fetchall()
        for r in red:
            urgent_items.append({
                'project_id': r['id'], 'project_name': r['name'],
                'reason': f"상태: {r['status']}", 'priority': 'critical',
                'person': r['person_internal'] or '',
            })

        # 2. 최근 30일 커뮤니케이션 없는 활성 프로젝트 (stale)
        stale = conn.execute("""
            SELECT p.id, p.name, p.status, p.person_internal,
                   MAX(c.log_date) as last_comm
            FROM projects p
            LEFT JOIN comm_log c ON p.id = c.project_id
            WHERE p.status NOT LIKE '%완료%' AND p.status NOT LIKE '%취소%'
                  AND p.status NOT LIKE '%보류%'
            GROUP BY p.id
            HAVING last_comm < date('now', '-30 days') OR last_comm IS NULL
        """).fetchall()
        for r in stale:
            urgent_items.append({
                'project_id': r['id'], 'project_name': r['name'],
                'reason': f"마지막 활동: {r['last_comm'] or '기록 없음'} (30일+ 미활동)",
                'priority': 'stale',
                'person': r['person_internal'] or '',
            })

        # 3. 다음 예상 액션에 기한이 있는 항목 (chunks에서 추출)
        actions = conn.execute("""
            SELECT p.id, p.name, c.content, p.person_internal
            FROM chunks c JOIN projects p ON c.project_id = p.id
            WHERE c.chunk_type = 'next_action'
        """).fetchall()
        today = datetime.now().strftime('%Y-%m-%d')
        for r in actions:
            # 날짜 패턴 추출
            dates = re.findall(r'20\d{2}[-/]\d{2}[-/]\d{2}', r['content'] or '')
            for d in dates:
                d_norm = d.replace('/', '-')
                if d_norm <= today:
                    urgent_items.append({
                        'project_id': r['id'], 'project_name': r['name'],
                        'reason': f"기한 도래/초과: {d_norm}",
                        'priority': 'overdue',
                        'person': r['person_internal'] or '',
                    })
                    break

        # 4. 담당자 과부하 (5개 이상 프로젝트)
        overloaded = conn.execute("""
            SELECT person_internal, COUNT(*) as cnt
            FROM projects
            WHERE status NOT LIKE '%완료%' AND status NOT LIKE '%취소%'
                  AND person_internal IS NOT NULL AND person_internal != ''
            GROUP BY person_internal
            HAVING cnt >= 5
        """).fetchall()
        for r in overloaded:
            urgent_items.append({
                'project_id': None, 'project_name': None,
                'reason': f"담당자 과부하: {r['person_internal']} ({r['cnt']}개 프로젝트)",
                'priority': 'overloaded',
                'person': r['person_internal'],
            })
    finally:
        conn.close()

    # 우선순위 정렬
    priority_order = {'critical': 0, 'overdue': 1, 'overloaded': 2, 'stale': 3}
    urgent_items.sort(key=lambda x: priority_order.get(x['priority'], 9))

    return {
        'total': len(urgent_items),
        'critical': sum(1 for i in urgent_items if i['priority'] == 'critical'),
        'overdue': sum(1 for i in urgent_items if i['priority'] == 'overdue'),
        'stale': sum(1 for i in urgent_items if i['priority'] == 'stale'),
        'overloaded': sum(1 for i in urgent_items if i['priority'] == 'overloaded'),
        'items': urgent_items,
    }
