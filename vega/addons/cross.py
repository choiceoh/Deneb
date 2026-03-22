"""애드온: 크로스 분석 — 프로젝트 간 연결고리 (거래처/자재/인력/일정/시너지)"""

from collections import defaultdict

from ._base import BaseAddon, _extract
import config


class CrossAnalysis(BaseAddon):
    name = 'cross'
    description = '프로젝트 간 연결고리 (거래처·자재·인력·일정·시너지)'
    commands = {'all':'전체','vendors':'거래처','materials':'자재','personnel':'인력','schedule':'일정','synergy':'시너지','project ID':'프로젝트별'}

    def api(self, cmd, args, ctx):
        """교차 분석 API.
        Returns: {type: "all"|"single"|"project", vendors, materials, personnel, schedule, synergy, project}
        - type="all": 5개 분석 전부 포함
        - type="single": cmd에 해당하는 분석만 포함 (나머지는 None)
        - type="project": 특정 프로젝트의 교차 정보
        """
        P = ctx.projects
        fns = {'vendors': self._vendors, 'materials': self._materials,
               'personnel': self._personnel, 'schedule': self._schedule,
               'synergy': lambda P: self._synergy(P)[:20]}
        # 항상 동일한 키 구조 반환
        result = {k: None for k in fns}
        result['project'] = None
        if cmd == 'project':
            pid = int(args[0]) if args else 0
            result['type'] = 'project'
            result['project'] = self._project(P, pid)
        elif cmd in fns:
            result['type'] = 'single'
            result[cmd] = fns[cmd](P)
        else:
            result['type'] = 'all'
            for k, fn in fns.items():
                result[k] = fn(P)
        return result

    def run(self, cmd, args, ctx):
        P = ctx.projects
        fns = {'vendors':(self._vendors,'v'), 'materials':(self._materials,'m'), 'personnel':(self._personnel,'p'),
               'schedule':(self._schedule,'s'), 'synergy':(lambda P: self._synergy(P)[:20],'y'),
               'project':(lambda P: self._project(P, int(args[0]) if args else 0),'j')}
        fmts = {'v':self._fv,'m':self._fm,'p':self._fp,'s':self._fs,'y':self._fy,'j':self._fj}
        if cmd in fns:
            fn, fk = fns[cmd]
            ctx.output(fn(P), fmts[fk])
        else:
            r = {k: fn(P) for k,(fn,_) in fns.items() if k != 'project'}
            if ctx.json_out: ctx.output(r)
            else:
                for k,(fn,fk) in fns.items():
                    if k != 'project': ctx.output(fn(P), fmts[fk])
                v,m,p,s = r['vendors'],r['materials'],r['personnel'],r['schedule']
                print(f"\n{'━'*70}\n 📊 요약: 거래처 {len(v)} | 자재 {len(m)} | 과부하 {len(p)}명 | 충돌 {len(s)}건 | 시너지 {len(r['synergy'])}쌍\n{'━'*70}")

    def _vendors(self, P):
        d = defaultdict(list)
        for pid,p in P.items():
            for v in p['vendors']: d[v].append((pid,p['name']))
        return {v:ps for v,ps in sorted(d.items(),key=lambda x:-len(x[1])) if len(ps)>=2}

    def _materials(self, P):
        d = defaultdict(list)
        for pid,p in P.items():
            for m in p['materials']: d[m].append((pid,p['name']))
        return {m:ps for m,ps in sorted(d.items(),key=lambda x:-len(x[1])) if len(ps)>=2}

    def _personnel(self, P):
        d = defaultdict(list)
        for pid,p in P.items():
            if any(k in (p['status'] or '').lower() for k in ['완료','준공']): continue
            for person in p['persons']: d[person].append((pid,p['name'],p['status']))
        return {p:ps for p,ps in sorted(d.items(),key=lambda x:-len(x[1])) if len(ps)>=2}

    def _schedule(self, P):
        a = defaultdict(list)
        for pid,p in P.items():
            dr = p['date_range']
            if not dr[0] or not dr[1] or any(k in (p['status'] or '').lower() for k in ['완료','준공']): continue
            for person in p['persons']: a[person].append(dict(pid=pid,name=p['name'],s=dr[0],e=dr[1]))
        c = []
        for person,ps in a.items():
            for i in range(len(ps)):
                for j in range(i+1,len(ps)):
                    x,y = ps[i],ps[j]
                    if x['s'] and x['e'] and y['s'] and y['e'] and x['s']<=y['e'] and y['s']<=x['e']:
                        c.append(dict(person=person,a=f"[{x['pid']}] {x['name']}",b=f"[{y['pid']}] {y['name']}",overlap=f"{max(x['s'],y['s'])} ~ {min(x['e'],y['e'])}"))
        return c

    def _synergy(self, P):
        t = {pid:{x for x in p['tags'] if x.startswith('기술:')} for pid,p in P.items()}
        s = []
        pids = list(t.keys())
        for i in range(len(pids)):
            for j in range(i+1,len(pids)):
                sh = t[pids[i]]&t[pids[j]]
                if len(sh)>=2: s.append(dict(a=f"[{pids[i]}] {P[pids[i]]['name']}",b=f"[{pids[j]}] {P[pids[j]]['name']}",shared=sorted(sh),count=len(sh)))
        return sorted(s,key=lambda x:-x['count'])

    def _project(self, P, pid):
        if pid not in P: return None
        t = P[pid]; c = dict(project=f"[{pid}] {t['name']}",vendors=[],materials=[],persons=[],synergy=[])
        for oid,o in P.items():
            if oid==pid: continue
            for cat,src in [('vendors','vendors'),('materials','materials'),('persons','persons')]:
                sh = t[src]&o[src]
                if sh: c[cat].append(dict(project=f"[{oid}] {o['name']}",shared=sorted(sh)))
            st = {x for x in t['tags'] if x.startswith('기술:')} & {x for x in o['tags'] if x.startswith('기술:')}
            if len(st)>=2: c['synergy'].append(dict(project=f"[{oid}] {o['name']}",shared=sorted(st)))
        return c

    def _fv(self,d):
        print(f"\n{'='*70}\n 🏢 거래처 공유 ({len(d)}개)\n{'='*70}")
        for v,ps in d.items(): print(f"\n  📌 {v} ({len(ps)}개)\n"+'\n'.join(f"     [{p}] {n}" for p,n in ps))
    def _fm(self,d):
        print(f"\n{'='*70}\n 🔩 자재 공유 ({len(d)}개)\n{'='*70}")
        for m,ps in d.items(): print(f"\n  📌 {m} ({len(ps)}개)\n"+'\n'.join(f"     [{p}] {n}" for p,n in ps))
    def _fp(self,d):
        print(f"\n{'='*70}\n 👥 인력 부하 ({len(d)}명)\n{'='*70}")
        for p,ps in d.items(): print(f"\n  ⚠️  {p} ({len(ps)}개)\n"+'\n'.join(f"     [{pid}] {n} ({(s or '-')[:25]})" for pid,n,s in ps))
    def _fs(self,d):
        print(f"\n{'='*70}\n 📅 일정 충돌 ({len(d)}건)\n{'='*70}")
        for c in d: print(f"\n  ⏰ {c['person']}\n     {c['a']}\n     {c['b']}\n     겹침: {c['overlap']}")
    def _fy(self,d):
        print(f"\n{'='*70}\n 🔗 기술 시너지 ({len(d)}쌍)\n{'='*70}")
        for s in d: print(f"\n  🤝 {s['a']}\n     ↔ {s['b']}\n     공유: {', '.join(t.replace('기술:','') for t in s['shared'])} ({s['count']}개)")
    def _fj(self,c):
        if not c: print("프로젝트 없음"); return
        print(f"\n{'='*70}\n 🔍 {c['project']} 연결고리\n{'='*70}")
        for cat,icon in [('vendors','🏢'),('materials','🔩'),('persons','👥'),('synergy','🔗')]:
            if c[cat]:
                print(f"\n  {icon} {cat} ({len(c[cat])}건)")
                for i in c[cat]: print(f"     {i['project']} — {', '.join(i['shared'])}")
