"""애드온: 금액 파이프라인 — 프로젝트 금액 집계, 단계별 매출 파이프라인"""

import re
from collections import defaultdict

from ._base import BaseAddon


class Pipeline(BaseAddon):
    name = 'pipeline'
    description = '프로젝트 금액 집계, 단계별 매출 파이프라인'
    commands = {'': '전체 파이프라인 (기본)', 'by-person': '담당별 집계', 'by-stage': '단계별 집계'}

    # 금액 추출 패턴 (순서 중요: 구체적인 것 먼저)
    AMOUNT_PATTERNS = [
        # 1,568,000,000원
        re.compile(r'([\d,]+)\s*원'),
        # 약 65억, 45억, 1,701억원, 16.68억
        re.compile(r'약?\s*([\d,.]+)\s*억(?:\s*원)?'),
        # USD 5M
        re.compile(r'USD\s*([\d.]+)\s*M', re.IGNORECASE),
        # 2,300만원
        re.compile(r'([\d,]+)\s*만\s*원'),
    ]

    STAGE_MAP = {
        '수주': ['계약 마무리', '계약', '날인', '체결'],
        '시공': ['시공', '공사', '설치'],
        '검토/제안': ['검토', '설계', '제안', '문의', '가견적', '가배치'],
        '금융/PF': ['금융', 'pf', '팩토링', '대출', '결재'],
        '준공/운영': ['준공', '완료', '운영', 'o&m'],
    }

    # 금액과 함께 나타나면 프로젝트 금액일 가능성 높은 컨텍스트
    AMOUNT_CONTEXT_POSITIVE = [
        '계약금액', '계약금', '금액', '총사업비', '공사비', '견적', '수주',
        '도급', '규모', '사업비', 'EPC', '낙찰', '수주액', '도급금액',
        '총공사비', '공사대금', '도급계약', '발전사업', '투자', '사업규모',
    ]
    # 이 컨텍스트의 금액은 무시
    AMOUNT_CONTEXT_NEGATIVE = [
        '보상비', '어업보상', '보험료', '수수료', '운영비', 'O&M', 'o&m',
        '임대료', '예비비', '이자', 'DSRA', '선급금', '보증보험',
        'LTSA', '감축', '감축량', '단가', 'kW당', 'MW당',
    ]

    def api(self, cmd, args, ctx):
        return {'items': self._build(ctx)}

    def run(self, cmd, args, ctx):
        pipeline = self._build(ctx)
        if cmd == 'by-person':
            ctx.output(pipeline, lambda d: self._fmt_by_person(d))
        elif cmd == 'by-stage':
            ctx.output(pipeline, lambda d: self._fmt_by_stage(d))
        else:
            ctx.output(pipeline, lambda d: self._fmt_all(d))

    def _extract_amount(self, text):
        """컨텍스트 인식 금액 추출 — 프로젝트 계약/사업 금액만 추출"""
        if not text:
            return None

        candidates = []

        # 줄 단위로 금액 탐색 (컨텍스트 판단을 위해)
        for line in text.split('\n'):
            line_lower = line.lower()

            # 부정 컨텍스트면 건너뛰기
            if any(neg in line_lower for neg in self.AMOUNT_CONTEXT_NEGATIVE):
                continue

            # 긍정 컨텍스트 가중치
            is_positive = any(pos in line for pos in self.AMOUNT_CONTEXT_POSITIVE)

            # 매칭된 span 추적 — 같은 위치 이중 매칭 방지
            matched_spans = []
            for pattern in self.AMOUNT_PATTERNS:
                for m in pattern.finditer(line):
                    # 이미 매칭된 범위와 겹치면 건너뛰기
                    if any(m.start() < end and m.end() > start for start, end in matched_spans):
                        continue
                    matched_spans.append((m.start(), m.end()))

                    val_str = m.group(1).replace(',', '')
                    try:
                        val = float(val_str)
                    except ValueError:
                        continue
                    if val == 0:
                        continue

                    full = m.group(0)
                    amount_억 = None
                    if '억' in full:
                        amount_억 = val
                    elif '만' in full:
                        amount_억 = val / 10000  # 만원 → 억원
                    elif 'USD' in full.upper() and 'M' in full.upper():
                        amount_억 = val * 13
                    elif val > 100_000_000:
                        amount_억 = val / 100_000_000

                    if amount_억 and amount_억 > 0.5:  # 0.5억 미만 무시
                        candidates.append((amount_억, is_positive))

        # 긍정 컨텍스트가 있으면 그 중 최대값, 없으면 전체 최대값
        positive = [a for a, p in candidates if p]
        if positive:
            return max(positive)
        # 긍정 없으면 — 첫 번째로 나온 합리적 금액 (최대값이 아닌)
        all_amounts = [a for a, _ in candidates if a < 10000]  # 1조 이상은 제외
        return all_amounts[0] if all_amounts else None

    def _classify_stage(self, status):
        if not status:
            return '기타'
        s = status.lower()
        for stage, keywords in self.STAGE_MAP.items():
            if any(kw in s for kw in keywords):
                return stage
        return '기타'

    def _extract_amount_from_table(self, text):
        """마크다운 테이블에서 금액 추출"""
        for line in text.split('\n'):
            if '|' not in line:
                continue
            cells = [c.strip().strip('*') for c in line.split('|')]
            for i, cell in enumerate(cells):
                cell_lower = cell.lower()
                if any(kw in cell_lower for kw in ['금액', '사업비', '공사비', '도급', '규모', '계약']):
                    # 같은 행의 다음 셀 또는 현재 셀에서 금액 찾기
                    for j in range(max(0, i), min(len(cells), i+3)):
                        amount = self._extract_amount(cells[j])
                        if amount:
                            return amount
        return None

    def _build(self, ctx):
        items = []
        for pid, proj in ctx.projects.items():
            # 금액: 메타 → 테이블 → 본문 전체 (금액 섹션 우선) 순서로 탐색
            amount = self._extract_amount(proj.get('capacity', ''))
            if not amount:
                amount = self._extract_amount_from_table(proj['all_text'])
            if not amount:
                # 금액 관련 섹션 우선 탐색
                for section_kw in ['금액', '계약', '사업비', '비용', '금융']:
                    for line in proj['all_text'].split('\n'):
                        if section_kw in line:
                            # 이 줄 전후 5줄 범위에서 금액 탐색
                            idx = proj['all_text'].index(line)
                            ctx_text = proj['all_text'][max(0, idx-200):idx+500]
                            amount = self._extract_amount(ctx_text)
                            if amount:
                                break
                    if amount:
                        break
            if not amount:
                amount = self._extract_amount(proj['all_text'])

            stage = self._classify_stage(proj['status'])
            items.append({
                'id': pid,
                'name': proj['name'],
                'status': proj['status'],
                'stage': stage,
                'amount': amount,
                'amount_str': f"{amount:.1f}억" if amount else '-',
                'person': proj['person_internal'],
                'persons': sorted(proj['persons']),
            })
        return sorted(items, key=lambda x: -(x['amount'] or 0))

    def _fmt_all(self, items):
        total = sum(i['amount'] or 0 for i in items)
        with_amount = [i for i in items if i['amount']]
        print(f"\n{'━'*70}")
        print(f" 💰 프로젝트 파이프라인 — 총 {total:,.1f}억원 ({len(with_amount)}/{len(items)}개 금액 파악)")
        print(f"{'━'*70}")
        print(f"\n {'ID':>3} | {'프로젝트':<32} | {'금액':>10} | {'단계':<12} | 담당")
        print(f" {'─'*3}-+-{'─'*32}-+-{'─'*10}-+-{'─'*12}-+-{'─'*15}")
        for i in items:
            print(f" {i['id']:>3} | {i['name'][:32]:<32} | {i['amount_str']:>10} | {i['stage']:<12} | {(i['person'] or '-')[:15]}")

        # 단계별 소계
        print(f"\n ■ 단계별 소계")
        by_stage = defaultdict(lambda: {'count': 0, 'amount': 0})
        for i in items:
            by_stage[i['stage']]['count'] += 1
            by_stage[i['stage']]['amount'] += i['amount'] or 0
        for stage in ['수주', '시공', '검토/제안', '금융/PF', '준공/운영', '기타']:
            s = by_stage.get(stage, {'count': 0, 'amount': 0})
            if s['count']:
                bar = '█' * int(s['amount'] / max(1, total) * 30) if total else ''
                print(f"   {stage:<12} {s['count']:>2}건  {s['amount']:>8.1f}억  {bar}")

    def _fmt_by_person(self, items):
        by_person = defaultdict(lambda: {'projects': [], 'total': 0})
        for i in items:
            for p in (i['persons'] or ['(미지정)']):
                by_person[p]['projects'].append(i)
                by_person[p]['total'] += i['amount'] or 0
        print(f"\n{'━'*70}\n 👤 담당별 파이프라인\n{'━'*70}")
        for person, data in sorted(by_person.items(), key=lambda x: -x[1]['total']):
            print(f"\n  {person} — {data['total']:.1f}억원 ({len(data['projects'])}건)")
            for i in data['projects']:
                print(f"    [{i['id']:>2}] {i['name'][:30]} {i['amount_str']:>8} ({i['stage']})")

    def _fmt_by_stage(self, items):
        by_stage = defaultdict(list)
        for i in items:
            by_stage[i['stage']].append(i)
        total = sum(i['amount'] or 0 for i in items)
        print(f"\n{'━'*70}\n 📊 단계별 파이프라인 — 총 {total:,.1f}억원\n{'━'*70}")
        for stage in ['수주', '시공', '검토/제안', '금융/PF', '준공/운영', '기타']:
            ps = by_stage.get(stage, [])
            if not ps: continue
            stotal = sum(i['amount'] or 0 for i in ps)
            print(f"\n  ■ {stage} ({len(ps)}건, {stotal:.1f}억원)")
            for i in ps:
                print(f"    [{i['id']:>2}] {i['name'][:30]} {i['amount_str']:>8}")
