import re
import config
from core import register_command, get_db_connection, _find_project_id, VegaError


@register_command('compare')
def _exec_compare(params):
    """프로젝트 간 비교"""
    sub_args = params.get('sub_args', []) or []
    project_refs = params.get('projects', []) or sub_args

    if not project_refs or len(project_refs) < 2:
        # 자연어에서 프로젝트 2개 추출 시도
        query = params.get('query', '')
        if query:
            conn = get_db_connection(row_factory=True)
            try:
                rows = conn.execute("SELECT id, name FROM projects").fetchall()
            finally:
                conn.close()
            found = []
            for row in rows:
                name = (row['name'] or '').strip()
                if name and name in query:
                    found.append(str(row['id']))
            if len(found) >= 2:
                project_refs = found[:5]

    if not project_refs or len(project_refs) < 2:
        raise VegaError(
            '2개 이상의 프로젝트를 지정해주세요.',
            usage=['compare 5 7', 'compare "비금도" "화성산단"']
        )

    from addons import Ctx
    ctx = Ctx(config.DB_PATH)
    P = ctx.projects

    pids = []
    seen_pids = set()
    for ref in project_refs:
        pid = _find_project_id(ref)
        if pid and pid in P and pid not in seen_pids:
            pids.append(pid)
            seen_pids.add(pid)

    if len(pids) < 2:
        raise VegaError(f'비교할 프로젝트를 2개 이상 찾을 수 없습니다. (찾은 수: {len(pids)})')

    projects_info = []
    for pid in pids:
        p = P[pid]
        projects_info.append({
            'id': pid, 'name': p['name'], 'status': p['status'],
            'client': p['client'], 'capacity': p.get('capacity'),
            'person_internal': p.get('person_internal'),
            'vendors': sorted(p.get('vendors', set())),
            'materials': sorted(p.get('materials', set())),
            'persons': sorted(p.get('persons', set())),
            'tags': sorted(p.get('tags', set())),
            'date_range': p.get('date_range', (None, None)),
        })

    # 교집합/차집합 계산
    all_vendors = [set(pi['vendors']) for pi in projects_info]
    all_materials = [set(pi['materials']) for pi in projects_info]
    all_persons = [set(pi['persons']) for pi in projects_info]
    all_tags = [set(pi['tags']) for pi in projects_info]

    shared = {
        'vendors': sorted(set.intersection(*all_vendors)) if all_vendors else [],
        'materials': sorted(set.intersection(*all_materials)) if all_materials else [],
        'personnel': sorted(set.intersection(*all_persons)) if all_persons else [],
        'tags': sorted(set.intersection(*all_tags)) if all_tags else [],
    }

    unique_per_project = []
    for i, pi in enumerate(projects_info):
        others_v = set().union(*(all_vendors[:i] + all_vendors[i+1:])) if len(all_vendors) > 1 else set()
        others_m = set().union(*(all_materials[:i] + all_materials[i+1:])) if len(all_materials) > 1 else set()
        others_p = set().union(*(all_persons[:i] + all_persons[i+1:])) if len(all_persons) > 1 else set()
        unique_per_project.append({
            'id': pi['id'], 'name': pi['name'],
            'unique_vendors': sorted(set(pi['vendors']) - others_v),
            'unique_materials': sorted(set(pi['materials']) - others_m),
            'unique_persons': sorted(set(pi['persons']) - others_p),
        })

    names = [pi['name'] for pi in projects_info]
    return {
        'project_count': len(pids),
        'projects': projects_info,
        'shared': shared,
        'unique_per_project': unique_per_project,
        'summary': f"{', '.join(names)} 비교: 공유 거래처 {len(shared['vendors'])}개, 공유 인력 {len(shared['personnel'])}명",
    }


@register_command('stats')
def _exec_stats(params=None):
    """프로젝트 통계 분석"""
    conn = get_db_connection(row_factory=True)
    try:
        # 전체 프로젝트 수
        row = conn.execute("SELECT COUNT(*) as c FROM projects").fetchone()
        total = row['c'] if row else 0
        row = conn.execute(
            "SELECT COUNT(*) as c FROM projects WHERE status NOT LIKE '%완료%' AND status NOT LIKE '%취소%'"
        ).fetchone()
        active = row['c'] if row else 0

        # 커뮤니케이션 통계
        row = conn.execute("SELECT COUNT(*) as c FROM comm_log").fetchone()
        comm_total = row['c'] if row else 0
        comm_per_project = conn.execute("""
            SELECT p.id, p.name, COUNT(cl.id) as comm_count
            FROM projects p LEFT JOIN comm_log cl ON p.id = cl.project_id
            GROUP BY p.id ORDER BY comm_count DESC
        """).fetchall()
        comm_counts = [r['comm_count'] for r in comm_per_project]
        avg_comms = round(sum(comm_counts) / len(comm_counts), 1) if comm_counts else 0

        # 월별 트렌드 (최근 6개월)
        monthly = conn.execute("""
            SELECT substr(log_date, 1, 7) as month, COUNT(*) as cnt
            FROM comm_log
            WHERE log_date >= date('now', '-6 months')
            GROUP BY month ORDER BY month
        """).fetchall()

        # 가장 활발한 프로젝트 (30일)
        top_active = conn.execute("""
            SELECT p.id, p.name, COUNT(cl.id) as recent_comms
            FROM projects p JOIN comm_log cl ON p.id = cl.project_id
            WHERE cl.log_date >= date('now', '-30 days')
            GROUP BY p.id ORDER BY recent_comms DESC LIMIT 10
        """).fetchall()

        # 담당자별 부하
        person_load = conn.execute("""
            SELECT person_internal, COUNT(*) as cnt
            FROM projects
            WHERE status NOT LIKE '%완료%' AND status NOT LIKE '%취소%'
              AND person_internal IS NOT NULL AND person_internal != ''
            GROUP BY person_internal ORDER BY cnt DESC
        """).fetchall()

        # 미활동 프로젝트 (30일+)
        stale_count = conn.execute("""
            SELECT COUNT(*) as c FROM (
                SELECT p.id, MAX(cl.log_date) as last_comm
                FROM projects p LEFT JOIN comm_log cl ON p.id = cl.project_id
                WHERE p.status NOT LIKE '%완료%' AND p.status NOT LIKE '%취소%'
                GROUP BY p.id
                HAVING last_comm < date('now', '-30 days') OR last_comm IS NULL
            )
        """).fetchone()
    finally:
        conn.close()

    return {
        'projects': {'total': total, 'active': active, 'stale': stale_count['c'] if stale_count else 0},
        'communication': {
            'total': comm_total,
            'avg_per_project': avg_comms,
            'monthly_trend': [{'month': r['month'], 'count': r['cnt']} for r in monthly],
        },
        'top_active_30d': [{'id': r['id'], 'name': r['name'], 'comms': r['recent_comms']} for r in top_active],
        'person_workload': [{'person': r['person_internal'], 'projects': r['cnt']} for r in person_load],
        'summary': f"총 {total}개 프로젝트 (활성 {active}), 커뮤니케이션 {comm_total}건 (평균 {avg_comms}건/프로젝트)",
    }
