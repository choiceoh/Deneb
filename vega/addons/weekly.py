"""애드온: 주간보고 자동 생성 — 프로젝트별 최근 활동을 정형 보고서로 출력"""

import re
from datetime import datetime, timedelta
from pathlib import Path

from ._base import BaseAddon


class WeeklyReport(BaseAddon):
    name = 'weekly'
    description = '프로젝트별 최근 활동을 정형 보고서로 출력'
    commands = {'': '이번 주 보고 (기본)', '--since YYYY-MM-DD': '특정일 이후', '--md report.md': '마크다운 파일 출력'}

    def api(self, cmd, args, ctx):
        since = None
        for i, a in enumerate(args):
            if a == '--since' and i+1 < len(args): since = args[i+1]
        if cmd and re.match(r'^\d{4}-\d{2}-\d{2}$', cmd):
            since = cmd
        if not since:
            today = datetime.now()
            monday = today - timedelta(days=today.weekday())
            since = monday.strftime('%Y-%m-%d')
        report = self._generate(ctx, since)
        return {'since': since, 'report': report}

    def run(self, cmd, args, ctx):
        # 기간 결정
        since = None
        md_out = None
        for i, a in enumerate(args):
            if a == '--since' and i+1 < len(args): since = args[i+1]
            if a == '--md' and i+1 < len(args): md_out = args[i+1]
        if cmd and re.match(r'^\d{4}-\d{2}-\d{2}$', cmd):
            since = cmd
        if not since:
            # 이번 주 월요일
            today = datetime.now()
            monday = today - timedelta(days=today.weekday())
            since = monday.strftime('%Y-%m-%d')

        report = self._generate(ctx, since)
        if md_out:
            Path(md_out).write_text(self._to_markdown(report, since), encoding='utf-8')
            print(f"✅ 주간보고 저장: {md_out}")
        else:
            ctx.output(report, lambda d: self._fmt(d, since))

    def _generate(self, ctx, since):
        conn = ctx.get_conn()
        report = dict()

        try:
            # 프로젝트별 활동
            for pid, proj in ctx.projects.items():
                comms = conn.execute("""
                    SELECT log_date, sender, subject, summary
                    FROM comm_log WHERE project_id=? AND log_date>=?
                    ORDER BY log_date DESC
                """, (pid, since)).fetchall()

                chunks = conn.execute("""
                    SELECT section_heading, entry_date
                    FROM chunks WHERE project_id=? AND entry_date>=?
                    ORDER BY entry_date DESC
                """, (pid, since)).fetchall()

                if not comms and not chunks:
                    continue

                report[pid] = {
                    'name': proj['name'],
                    'status': proj['status'],
                    'person': proj['person_internal'],
                    'comms': [dict(c) for c in comms],
                    'chunks': [dict(c) for c in chunks],
                    'activity_count': len(comms) + len(chunks),
                }
        finally:
            conn.close()
        return dict(sorted(report.items(), key=lambda x: -x[1]['activity_count']))

    def _fmt(self, report, since):
        now = datetime.now().strftime('%Y-%m-%d')
        total_activity = sum(r['activity_count'] for r in report.values())
        print(f"\n{'━'*70}")
        print(f" 📋 주간보고 ({since} ~ {now})")
        print(f" 활성 프로젝트 {len(report)}개 | 총 활동 {total_activity}건")
        print(f"{'━'*70}")

        for pid, r in report.items():
            print(f"\n ┌─ [{pid}] {r['name']}")
            print(f" │  상태: {r['status'] or '-'} | 담당: {(r['person'] or '-')[:20]}")
            print(f" │  활동: {r['activity_count']}건")
            for c in r['comms'][:5]:
                print(f" │  📬 [{c['log_date']}] {c['sender']}: {c['subject'][:50]}")
            if len(r['comms']) > 5:
                print(f" │  ... +{len(r['comms'])-5}건")
            print(f" └{'─'*68}")

    def _to_markdown(self, report, since):
        now = datetime.now().strftime('%Y-%m-%d')
        lines = [f"# 주간보고 ({since} ~ {now})\n"]
        lines.append(f"활성 프로젝트 {len(report)}개 | 총 활동 {sum(r['activity_count'] for r in report.values())}건\n")

        for pid, r in report.items():
            lines.append(f"\n## [{pid}] {r['name']}")
            lines.append(f"- **상태**: {r['status'] or '-'}")
            lines.append(f"- **담당**: {r['person'] or '-'}")
            lines.append(f"- **활동**: {r['activity_count']}건\n")
            if r['comms']:
                lines.append("### 커뮤니케이션")
                for c in r['comms'][:10]:
                    lines.append(f"- [{c['log_date']}] {c['sender']}: {c['subject']}")
            lines.append("")

        return '\n'.join(lines)
