"""
공유 인프라: Ctx, BaseAddon, _load_projects, _extract, _json_default, _project_cache
"""

import re, json, os
from collections import defaultdict
from abc import ABC, abstractmethod

import config


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# 공유 인프라
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

class Ctx:
    """실행 컨텍스트. 모든 애드온이 공유 자원에 접근하는 유일한 경로."""
    def __init__(self, db_path=None, md_dir=None, json_out=False):
        self.db_path = db_path if db_path is not None else config.DB_PATH
        self.md_dir = md_dir if md_dir is not None else config.MD_DIR
        self.json_out = json_out
        self._projects = None

    def get_conn(self):
        if not os.path.exists(self.db_path):
            # DB 없으면 자동 재빌드 시도
            try:
                import project_db_v2
                if os.path.isdir(self.md_dir):
                    project_db_v2.import_files(self.md_dir, db_path=self.db_path)
            except Exception:
                pass
        return config.get_db_connection(self.db_path, row_factory=True)

    @property
    def projects(self):
        if self._projects is None:
            self._projects = _load_projects(self.db_path)
        return self._projects

    def output(self, data, formatter=None):
        if self.json_out:
            print(json.dumps(data, ensure_ascii=False, indent=2, default=_json_default))
        elif formatter:
            formatter(data)

def _json_default(obj):
    import sqlite3
    if isinstance(obj, set): return sorted(obj)
    if isinstance(obj, sqlite3.Row): return dict(obj)
    return str(obj)



# KNOWN_VENDORS, KNOWN_MATERIALS는 config에서 참조

def _extract(text, patterns):
    tl = text.lower()
    return {n for n, kws in patterns for kw in kws if re.search(kw, tl)}

_project_cache = {'data': None, 'mtime': 0, 'db_path': None}

def _load_projects(db_path):
    try:
        mtime = os.path.getmtime(db_path)
    except OSError:
        mtime = 0
    if (_project_cache['data'] is not None
            and mtime <= _project_cache['mtime']
            and _project_cache.get('db_path') == db_path):
        return _project_cache['data']

    if not os.path.exists(db_path):
        return {}
    try:
        conn = config.get_db_connection(db_path, row_factory=True)
    except Exception:
        return {}
    try:
        # 테이블 존재 확인 (빈 DB 또는 스키마 미생성 시)
        if not conn.execute("SELECT name FROM sqlite_master WHERE type='table' AND name='projects'").fetchone():
            conn.close()
            return {}

        # 벌크 쿼리: 3N+1 → 4 쿼리로 축소

        # 1) 프로젝트 메타
        projects_rows = conn.execute("SELECT * FROM projects ORDER BY id").fetchall()

        # 2) chunks — 프로젝트별 content/entry_date
        chunks_by_pid = defaultdict(list)
        for c in conn.execute("SELECT project_id, content, entry_date FROM chunks"):
            chunks_by_pid[c['project_id']].append(c)

        # 3) comm_log — 프로젝트별
        comms_by_pid = defaultdict(list)
        for m in conn.execute("SELECT project_id, log_date, sender, subject, summary FROM comm_log"):
            comms_by_pid[m['project_id']].append(m)

        # 4) tags — 프로젝트별
        tags_by_pid = defaultdict(set)
        for t in conn.execute(
            "SELECT DISTINCT c.project_id, t.name FROM tags t "
            "JOIN chunk_tags ct ON ct.tag_id=t.id "
            "JOIN chunks c ON c.id=ct.chunk_id"
        ):
            tags_by_pid[t['project_id']].add(t['name'])

        P = {}
        for p in projects_rows:
            pid = p['id']
            chunks = chunks_by_pid.get(pid, [])
            comms = comms_by_pid.get(pid, [])
            all_text = '\n'.join(c['content'] or '' for c in chunks) + '\n' + '\n'.join(f"{m['sender'] or ''} {m['subject'] or ''} {m['summary'] or ''}" for m in comms)
            persons = set()
            if p['person_internal']:
                for x in re.split(r'[,/·]', p['person_internal']):
                    n = re.sub(r'\s*(과장|주임|대리|차장|부장|이사|전무|팀장|실장)\s*', '', re.sub(r'\(.*?\)', '', x)).strip()
                    if n and len(n) >= 2: persons.add(n)
            dates = sorted([c['entry_date'] for c in chunks if c['entry_date']] + [m['log_date'] for m in comms if m['log_date']])
            P[pid] = dict(id=pid, name=p['name'], client=p['client'], status=p['status'],
                          person_internal=p['person_internal'], capacity=p['capacity'],
                          source_file=p['source_file'], all_text=all_text, persons=persons,
                          tags=tags_by_pid.get(pid, set()), vendors=_extract(all_text, config.KNOWN_VENDORS),
                          materials=_extract(all_text, config.KNOWN_MATERIALS),
                          date_range=(dates[0], dates[-1]) if dates else (None, None), dates=dates)
    finally:
        conn.close()
    _project_cache['data'] = P
    _project_cache['mtime'] = mtime
    _project_cache['db_path'] = db_path
    return P


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# 베이스 클래스
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

class BaseAddon(ABC):
    """
    새 애드온 작성 가이드:

    class MyAddon(BaseAddon):
        name = 'my-addon'
        description = '한 줄 설명'
        commands = {'sub1': '설명1', 'sub2': '설명2'}

        def run(self, cmd, args, ctx):
            # cmd: 서브명령 (빈 문자열이면 기본 동작)
            # args: 나머지 위치 인수
            # ctx: Ctx 객체 (ctx.projects, ctx.get_conn(), ctx.output())
            data = self._my_logic(ctx.projects)
            ctx.output(data, self._my_formatter)

    규칙:
      - 다른 애드온 인스턴스에 직접 접근 금지
      - 공유 데이터는 ctx.projects 또는 ctx.get_conn()으로만
      - 출력은 ctx.output(data, formatter)로 통일
      - 상태를 self에 저장하지 않기 (stateless 권장)
    """
    @property
    @abstractmethod
    def name(self) -> str: ...
    @property
    @abstractmethod
    def description(self) -> str: ...
    @property
    def commands(self) -> dict: return {}
    @abstractmethod
    def run(self, cmd: str, args: list, ctx: 'Ctx'): ...

    def api(self, cmd: str, args: list, ctx: 'Ctx') -> dict:
        """vega.py가 호출하는 공개 인터페이스. 데이터를 dict로 반환."""
        raise NotImplementedError(f"{self.name}.api() not implemented")

    def safe_api(self, cmd: str, args: list, ctx: 'Ctx') -> dict:
        """api()를 에러 핸들링으로 감싼 래퍼."""
        try:
            return self.api(cmd, args, ctx)
        except Exception as e:
            import traceback
            return {'error': str(e), 'error_type': type(e).__name__,
                    'debug': traceback.format_exc(), 'addon': self.name}
