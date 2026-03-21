"""
Vega 프로젝트 검색엔진 애드온 시스템

플러그인 아키텍처:
  각 애드온은 BaseAddon을 상속하는 독립 클래스.
  파일 하단의 ADDONS 리스트에 추가만 하면 자동 등록.

사용법:
  python -m addons cross [all|vendors|materials|personnel|schedule|synergy|project ID]
  python -m addons dashboard [--html output.html]
  python -m addons changelog
  python -m addons sync-back [--dry-run]
  python -m addons contacts [all|project ID|search 이름]
  python -m addons weekly [--since YYYY-MM-DD] [--md report.md]
  python -m addons pipeline [by-person|by-stage]
  python -m addons template [quick "이름" "고객" "담당"]
  python -m addons help
"""

import sys

import config

from ._base import Ctx, BaseAddon, _load_projects, _extract, _json_default, _project_cache
from .cross import CrossAnalysis
from .dashboard import Dashboard
from .changelog import Changelog
from .sync_back import SyncBack
from .contacts import Contacts
from .weekly import WeeklyReport
from .pipeline import Pipeline
from .template import Template


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# 등록 레지스트리
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

# ┌─────────────────────────────────────────────┐
# │  새 애드온: 이 리스트에 클래스만 추가하세요    │
# └─────────────────────────────────────────────┘

ADDONS = [
    CrossAnalysis,
    Dashboard,
    Changelog,
    SyncBack,
    Contacts,
    WeeklyReport,
    Pipeline,
    Template,
]

def _registry():
    r = {}
    for cls in ADDONS:
        a = cls(); r[a.name] = a
    return r

def main():
    reg = _registry()
    db, md, jout = config.DB_PATH, config.MD_DIR, '--json' in sys.argv
    if '--db' in sys.argv:
        i = sys.argv.index('--db'); db = sys.argv[i+1] if i+1<len(sys.argv) else config.DB_PATH
    if '--md' in sys.argv:
        i = sys.argv.index('--md'); md = sys.argv[i+1] if i+1<len(sys.argv) else config.MD_DIR
    ctx = Ctx(db, md, jout)
    skip = {db, md, '--db', '--md', '--json'}
    pos = [a for a in sys.argv[1:] if a not in skip]
    name = pos[0] if pos else ''
    sub = pos[1] if len(pos)>1 else ''
    rest = pos[2:] + [a for a in sys.argv[1:] if a.startswith('--') and a not in ('--db','--md','--json')]

    if name in reg:
        reg[name].run(sub, rest, ctx)
    elif name == 'help' or not name:
        print(f"\nVega 애드온 (플러그인 {len(reg)}개)\n")
        for n, a in reg.items():
            print(f"  {n:<14} {a.description}")
            for c, d in a.commands.items(): print(f"    {c:<18} {d}")
            print()
        print("공통: --db PATH  --md PATH  --json\n새 애드온: BaseAddon 상속 → ADDONS 리스트에 등록")
    else:
        print(f"알 수 없음: {name}\n사용 가능: {', '.join(reg.keys())}\n도움말: python -m addons help")

if __name__ == '__main__':
    main()
