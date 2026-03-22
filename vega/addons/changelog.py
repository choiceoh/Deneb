"""애드온: 변경 리포트 — 변경 감지 (신규/상태변경/새 커뮤니케이션/수정 섹션)"""

import json, hashlib
from datetime import datetime
from pathlib import Path

from ._base import BaseAddon


class Changelog(BaseAddon):
    name = 'changelog'
    description = '변경 감지 (신규/상태변경/새 커뮤니케이션/수정 섹션)'
    SNAP = ".snapshot.json"

    def _snap_path(self, ctx):
        """스냅샷 파일 경로 (Windows/Unix 양쪽 호환)"""
        db = Path(ctx.db_path)
        return db.parent / self.SNAP if db.parent != Path('.') else Path(self.SNAP)

    def _compute_changes(self, ctx):
        sp = self._snap_path(ctx)
        cur = self._snap(ctx)
        if sp.exists():
            try:
                old = json.loads(sp.read_text(encoding='utf-8'))
                ch = self._diff(old, cur)
            except (json.JSONDecodeError, KeyError):
                ch = self._initial_changes(cur)
        else:
            ch = self._initial_changes(cur)
        sp.write_text(json.dumps(cur, ensure_ascii=False), encoding='utf-8')
        ch['total_changes'] = sum(len(v) for v in ch.values() if isinstance(v, list))
        return ch

    def _initial_changes(self, cur):
        return {'new_projects': [{'id': pid, 'name': p['name']} for pid, p in cur.items()],
                'removed': [], 'status': [], 'comms': [], 'modified': [], 'new_chunks': []}

    def api(self, cmd, args, ctx):
        return self._compute_changes(ctx)

    def run(self, cmd, args, ctx):
        ch = self._compute_changes(ctx)
        ctx.output(ch, self._fmt)

    def _snap(self, ctx):
        conn = ctx.get_conn(); s = {}
        try:
            for p in conn.execute("SELECT * FROM projects"):
                pid = p['id']
                cks = conn.execute("SELECT id,section_heading,content FROM chunks WHERE project_id=?",(pid,)).fetchall()
                cms = conn.execute("SELECT id,log_date,sender,subject FROM comm_log WHERE project_id=?",(pid,)).fetchall()
                s[str(pid)] = dict(name=p['name'],status=p['status'],
                    h={str(c['id']):hashlib.md5(c['content'].encode()).hexdigest() for c in cks},
                    hd={str(c['id']):c['section_heading'] for c in cks},
                    cm={str(c['id']):f"{c['log_date']}|{c['sender']}|{c['subject']}" for c in cms})
        finally:
            conn.close()
        return s

    def _diff(self, o, n):
        ch = dict(new_projects=[],removed=[],status=[],comms=[],modified=[],new_chunks=[])
        for pid in set(n)-set(o): ch['new_projects'].append(dict(id=pid,name=n[pid]['name']))
        for pid in set(o)-set(n): ch['removed'].append(dict(id=pid,name=o[pid]['name']))
        for pid in set(o)&set(n):
            a,b = o[pid],n[pid]
            if a.get('status')!=b.get('status'): ch['status'].append(dict(id=pid,name=b['name'],old=a.get('status'),new=b.get('status')))
            a_cm = a.get('cm') or a.get('comm_ids') or {}
            b_cm = b.get('cm') or b.get('comm_ids') or {}
            for cid in set(b_cm)-set(a_cm):
                ps = b_cm[cid].split('|',2)
                ch['comms'].append(dict(project=b['name'],date=ps[0],sender=ps[1] if len(ps)>1 else '?',subject=ps[2] if len(ps)>2 else '?'))
            a_h = a.get('h') or a.get('chunk_hashes') or {}
            b_h = b.get('h') or b.get('chunk_hashes') or {}
            b_hd = b.get('hd') or b.get('chunk_headings') or {}
            for cid in set(a_h)&set(b_h):
                if a_h[cid]!=b_h[cid]: ch['modified'].append(dict(project=b['name'],section=b_hd.get(cid,'?')))
            for cid in set(b_h)-set(a_h): ch['new_chunks'].append(dict(project=b['name'],section=b_hd.get(cid,'?')))
        return ch

    def _fmt(self, ch):
        total = sum(len(v) for v in ch.values() if isinstance(v, list))
        print(f"\n{'━'*70}\n 📋 변경 리포트 ({datetime.now().strftime('%Y-%m-%d %H:%M')}) — {total}건\n{'━'*70}")
        if not total: print("\n  변경 없음"); return
        for key,icon in [('new_projects','🆕 신규'),('removed','🗑️ 삭제'),('status','🔄 상태'),('comms','📬 커뮤니케이션'),('modified','✏️ 수정'),('new_chunks','📄 새 섹션')]:
            items = ch[key]
            if not items: continue
            print(f"\n  {icon} ({len(items)}건)")
            for it in items[:15]:
                if 'old' in it: print(f"     [{it['id']}] {it['name']}: {it['old'] or '(없음)'} → {it['new'] or '(없음)'}")
                elif 'date' in it: print(f"     [{it['date']}] {it['project'][:18]} — {it['sender']}: {it['subject'][:45]}")
                elif 'section' in it: print(f"     {it['project'][:20]} — {it['section'][:40]}")
                else: print(f"     [{it.get('id','?')}] {it.get('name','?')}")
