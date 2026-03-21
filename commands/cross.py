import re
import config
from core import register_command


def _cross_summary(data):
    parts = []
    labels = {'vendors': '거래처', 'personnel': '과부하', 'schedule': '충돌', 'synergy': '시너지'}
    for k, label in labels.items():
        v = data.get(k)
        if v and isinstance(v, (list, dict)):
            parts.append(f"{label} {len(v)}")
    if data.get('project'):
        parts.append("프로젝트별 연결")
    return f"크로스 분석: {', '.join(parts)}" if parts else "크로스 분석 완료"


@register_command('cross', summary_fn=_cross_summary)
def _exec_cross(params):
    from addons import Ctx, CrossAnalysis
    ctx = Ctx(config.DB_PATH)
    ca = CrossAnalysis()

    sub_args = params.get('sub_args', [])
    query = params.get('query', '')
    cmd = sub_args[0] if sub_args else 'all'

    # 자연어에서 프로젝트 매칭 (라우팅 로직은 core의 역할)
    if query:
        P = ctx.projects
        for pid, p in P.items():
            if p['name'] and any(kw in query for kw in p['name'].split() if len(kw) >= 2):
                return ca.safe_api('project', [str(pid)], ctx)
        # 순수 숫자만 (앞뒤에 한글/영문 없을 때만 프로젝트 ID로 인식)
        pid_match = re.search(r'(?<![가-힣a-zA-Z])(\d+)(?![가-힣a-zA-Z개건명])', query)
        if pid_match and int(pid_match.group(1)) in P:
            return ca.safe_api('project', [pid_match.group(1)], ctx)
        cmd = 'all'

    if cmd == 'project' and len(sub_args) > 1:
        return ca.safe_api('project', sub_args[1:], ctx)

    return ca.safe_api(cmd, sub_args[1:] if sub_args else [], ctx)
