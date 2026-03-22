from datetime import datetime
import config
from core import register_command, addon_command
from addons import Changelog


@register_command('weekly',
    summary_fn=lambda d: f"기간 {d.get('period',{}).get('from','')}~{d.get('period',{}).get('to','')}: 활성 {d.get('active_projects',0)}개, 활동 {d.get('total_activity',0)}건")
def _exec_weekly(params):
    from addons import WeeklyReport, Ctx
    ctx = Ctx(config.DB_PATH)
    w = WeeklyReport()

    sub_args = params.get('sub_args', [])
    # MCP params에서 --since 추출
    since = params.get('since', '')
    if since and '--since' not in sub_args:
        sub_args = ['--since', since] + list(sub_args)
    result_raw = w.safe_api('', sub_args, ctx)
    if 'error' in result_raw:
        return result_raw

    since = result_raw['since']
    report = result_raw['report']
    return {
        'period': {'from': since, 'to': datetime.now().strftime('%Y-%m-%d')},
        'active_projects': len(report),
        'total_activity': sum(r['activity_count'] for r in report.values()),
        'projects': report,
    }


addon_command('changelog', Changelog,
              summary_fn=lambda d: f"변경 {d.get('total_changes',0)}건")
