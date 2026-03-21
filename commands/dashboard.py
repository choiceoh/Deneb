from addons import Dashboard
from core import addon_command

addon_command('dashboard', Dashboard,
              summary_fn=lambda d: f"전체 {d.get('total_projects',0)}개 중 활성 {d.get('active_projects',0)}개, 과부하 담당자 {len(d.get('overloaded_persons') or {})}명")
