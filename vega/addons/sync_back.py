"""애드온: 역방향 동기화 — DB -> .md 역반영 (양방향 싱크)"""

import re, shutil
from pathlib import Path

from ._base import BaseAddon


class SyncBack(BaseAddon):
    name = 'sync-back'
    description = 'DB → .md 역반영 (양방향 싱크)'
    commands = {'--dry-run': '미리보기'}

    def run(self, cmd, args, ctx):
        dry = '--dry-run' in args
        conn = ctx.get_conn()
        try:
            projects = conn.execute("SELECT * FROM projects").fetchall()
        finally:
            conn.close()
        md = Path(ctx.md_dir)
        if not md.exists(): print(f"❌ {ctx.md_dir} 없음"); return
        fm = {'상태':'status','사내 담당':'person_internal','사내담당':'person_internal','발주처':'client','규모':'capacity','품목':'biz_type','파트너':'partner'}
        changes = []
        for p in projects:
            if not p['source_file']: continue
            # source_file이 전체 경로이면 직접 사용, 아니면 md_dir 결합
            sf = Path(p['source_file'])
            fp = sf if sf.is_absolute() and sf.exists() else md / sf.name
            if not fp.exists(): continue
            text = fp.read_text(encoding='utf-8-sig')
            orig_text = text
            for fn,dc in fm.items():
                dv = p[dc]
                if not dv: continue
                pat = re.compile(r'(\|\s*\*?\*?'+re.escape(fn)+r'\*?\*?\s*\|)\s*(.+?)\s*(\|)',re.MULTILINE)
                m = pat.search(text)
                if not m: continue
                ov = m.group(2).strip()
                em = re.search(r'[🟢🟡🟠🔴⚪]', ov)
                nv = dv + (' '+em.group() if em else '')
                if ov != nv:
                    text = text[:m.start(2)]+' '+nv+' '+text[m.end(2):]
                    changes.append(dict(file=p['source_file'],field=fn,old=ov,new=nv))
            if text != orig_text and not dry:
                shutil.copy2(fp, fp.with_suffix('.md.bak'))
                fp.write_text(text, encoding='utf-8')
        ctx.output(dict(dry_run=dry,changes=changes), lambda d: self._fmt(d,dry))

    def _fmt(self, d, dry):
        ch = d['changes']
        print(f"\n{'━'*70}\n 🔄 역방향 동기화 {'(DRY RUN)' if dry else ''} — {len(ch)}건\n{'━'*70}")
        if not ch: print("\n  변경 없음"); return
        for c in ch: print(f"\n  📝 {c['file']}\n     {c['field']}: {c['old']}\n          → {c['new']}")
        if dry: print(f"\n  ℹ️  --dry-run 제거 시 실제 수정")
