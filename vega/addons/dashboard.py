"""애드온: 대시보드 — 프로젝트 현황 (상태별/담당별/최근활동/과부하)"""

from collections import defaultdict
from datetime import datetime, timedelta
from pathlib import Path

from ._base import BaseAddon
from .cross import CrossAnalysis


class Dashboard(BaseAddon):
    name = 'dashboard'
    description = '프로젝트 현황 (상태별/담당별/최근활동/과부하)'
    commands = {'--html FILE': 'HTML 저장'}
    SG = dict([('🔴 긴급',['긴급','화재']),('🟢 시공중',['시공','공사']),('🟡 검토/설계',['검토','설계','제안','문의']),
                       ('🟠 계약/금융',['계약','금융','결재','대기','팩토링']),('🔵 준공/운영',['준공','완료','운영']),('⚪ 기타',[])])

    def _cls(self, s):
        if not s: return '⚪ 기타'
        sl = s.lower()
        for g,kws in self.SG.items():
            if any(k in sl for k in kws): return g
        return '⚪ 기타'

    def _compute(self, ctx):
        P = ctx.projects
        bs = defaultdict(list)
        for p in P.values():
            bs[self._cls(p['status'])].append(p)
        bp = defaultdict(list)
        for p in P.values():
            for person in p['persons']:
                bp[person].append(p)
        wa = (datetime.now() - timedelta(days=7)).strftime('%Y-%m-%d')
        conn = ctx.get_conn()
        try:
            recent = conn.execute(
                "SELECT p.name,cl.log_date,cl.sender,cl.subject FROM comm_log cl JOIN projects p ON cl.project_id=p.id WHERE cl.log_date>=? ORDER BY cl.log_date DESC LIMIT 15",
                (wa,)).fetchall()
        finally:
            conn.close()
        ol = CrossAnalysis()._personnel(P)
        active = sum(1 for p in P.values() if not any(k in (p['status'] or '').lower() for k in ['완료', '준공']))
        return {'P': P, 'bs': bs, 'bp': bp, 'recent': recent, 'ol': ol, 'active': active}

    def api(self, cmd, args, ctx):
        d = self._compute(ctx)
        P, bs, bp, recent, ol, active = d['P'], d['bs'], d['bp'], d['recent'], d['ol'], d['active']
        return {
            'total_projects': len(P),
            'active_projects': active,
            'by_status': {g: [{'id': p['id'], 'name': p['name'], 'status': p['status']} for p in ps] for g, ps in bs.items()},
            'by_person': {p: len(ps) for p, ps in sorted(bp.items(), key=lambda x: -len(x[1]))},
            'recent_activity': [dict(r) for r in recent],
            'overloaded_persons': {p: len(ps) for p, ps in ol.items()},
        }

    def run(self, cmd, args, ctx):
        html_out = None
        for i,a in enumerate(args):
            if a=='--html' and i+1<len(args): html_out = args[i+1]

        d = self._compute(ctx)
        P, bs, bp, recent, ol = d['P'], d['bs'], d['bp'], d['recent'], d['ol']

        if ctx.json_out:
            ctx.output({'by_status':{g:[{'id':p['id'],'name':p['name']} for p in ps] for g,ps in bs.items()},'by_person':{p:len(ps) for p,ps in bp.items()},'overloaded':len(ol)})
        else:
            now = datetime.now().strftime('%Y-%m-%d %H:%M')
            print(f"\n{'━'*70}\n 📊 대시보드 ({now}) — {len(P)}개\n{'━'*70}")
            print(f"\n ■ 상태별")
            for g in self.SG:
                ps = bs.get(g,[])
                if ps:
                    print(f"   {g} ({len(ps)}개)")
                    for p in ps: print(f"     [{p['id']:>2}] {p['name'][:35]:<36} {(p['status'] or '-')[:25]}")
            print(f"\n ■ 담당별")
            for person,ps in sorted(bp.items(),key=lambda x:-len(x[1])):
                ac = len([p for p in ps if not any(k in (p['status'] or '').lower() for k in ['완료','준공'])])
                print(f"   {person:<12} 총 {len(ps)}개 (활성 {ac}개){'  ⚠️' if ac>=3 else ''}")
            if recent:
                print(f"\n ■ 최근 7일 ({len(recent)}건)")
                for r in recent: print(f"   [{r['log_date']}] {r['name'][:18]} — {r['sender']}: {r['subject'][:40]}")
            if ol:
                print(f"\n ■ ⚠️ 과부하")
                for p,ps in ol.items(): print(f"   {p}: {len(ps)}개 동시")
        if html_out:
            self._html(P,bs,bp,recent,ol,html_out)
            print(f"\n ✅ HTML: {html_out}")

    def _html(self,P,bs,bp,recent,ol,path):
        sc = ""
        for g,ps in bs.items():
            if not ps: continue
            rows = ''.join(f'<tr><td>{p["id"]}</td><td>{p["name"]}</td><td>{(p["status"] or "-")[:30]}</td></tr>' for p in ps)
            sc += f'<div class="c"><h3>{g} ({len(ps)})</h3><table><tr><th>ID</th><th>프로젝트</th><th>상태</th></tr>{rows}</table></div>'
        bars = ""
        for person,ps in sorted(bp.items(),key=lambda x:-len(x[1])):
            ac = len([p for p in ps if not any(k in (p['status'] or '').lower() for k in ['완료','준공'])])
            bars += f'<div style="display:flex;align-items:center;margin:4px 0"><span style="width:80px;font-size:13px">{person}</span><div style="height:24px;border-radius:4px;color:#fff;font-size:12px;display:flex;align-items:center;padding:0 8px;min-width:30px;width:{min(ac*60,300)}px;background:{"#e74c3c" if ac>=3 else "#3498db"}">{ac}</div></div>'
        rr = ''.join(f'<tr><td>{r["log_date"]}</td><td>{r["name"][:20]}</td><td>{r["sender"]}</td><td>{r["subject"][:50]}</td></tr>' for r in recent)
        Path(path).write_text(f"""<!DOCTYPE html><html><head><meta charset="utf-8"><title>Vega 대시보드</title>
        <style>*{{margin:0;padding:0;box-sizing:border-box}}body{{font-family:-apple-system,sans-serif;background:#f5f6fa;color:#2d3436;padding:20px}}
        h1{{text-align:center;margin:20px 0}}.su{{display:flex;gap:12px;justify-content:center;margin:20px 0;flex-wrap:wrap}}
        .s{{background:#fff;border-radius:10px;padding:16px 24px;text-align:center;box-shadow:0 2px 8px rgba(0,0,0,.08)}}
        .s .n{{font-size:28px;font-weight:700;color:#0984e3}}.s .l{{font-size:12px;color:#636e72;margin-top:4px}}
        .g{{display:grid;grid-template-columns:repeat(auto-fit,minmax(400px,1fr));gap:16px;margin:20px 0}}
        .c{{background:#fff;border-radius:10px;padding:20px;box-shadow:0 2px 8px rgba(0,0,0,.08)}}.c h3{{margin-bottom:12px;font-size:16px}}
        table{{width:100%;border-collapse:collapse;font-size:13px}}th,td{{padding:6px 8px;text-align:left;border-bottom:1px solid #eee}}th{{background:#f8f9fa}}</style></head><body>
        <h1>📊 Vega 대시보드</h1>
        <div class="su"><div class="s"><div class="n">{len(P)}</div><div class="l">전체</div></div>
        <div class="s"><div class="n">{sum(1 for p in P.values() if not any(k in (p['status'] or '').lower() for k in ['완료','준공']))}</div><div class="l">활성</div></div>
        <div class="s"><div class="n">{len(ol)}</div><div class="l">과부하</div></div></div>
        <div class="g">{sc}<div class="c"><h3>👥 담당별</h3>{bars}</div>
        <div class="c"><h3>📬 최근 7일</h3><table><tr><th>날짜</th><th>프로젝트</th><th>발신</th><th>제목</th></tr>{rr}</table></div></div>
        <p style="text-align:center;color:#b2bec3;font-size:12px;margin-top:20px">생성: {datetime.now().strftime('%Y-%m-%d %H:%M')}</p></body></html>""", encoding='utf-8')
