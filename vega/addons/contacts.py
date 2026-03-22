"""애드온: 연락처 자동 추출 — .md에 흩어진 이름/이메일/전화번호를 거래처별로 통합"""

import re
from collections import defaultdict

from ._base import BaseAddon


class Contacts(BaseAddon):
    name = 'contacts'
    description = '.md에 흩어진 이름/이메일/전화번호를 거래처별로 통합'
    commands = {'all': '전체 연락처 (기본)', 'project': '프로젝트별 ID', 'search': '이름/회사 검색'}

    # 전화번호 패턴 (한국 휴대폰 + 유선, 더 긴 번호 내부 매칭 방지)
    PHONE_RE = re.compile(r'(?<!\d)((?:01[0-9]|0[2-6][0-9]?)[-.\s]?\d{3,4}[-.\s]?\d{4})(?!\d)')
    # 이메일 패턴
    EMAIL_RE = re.compile(r'([a-zA-Z0-9_.+-]+@[a-zA-Z0-9-]+\.[a-zA-Z0-9-.]+)')
    # 이름+소속 패턴들
    NAME_PATTERNS = [
        # 테이블: | **거래처 담당** | 김서현 책임매니저 (현대위아) |
        re.compile(r'(?:거래처\s*담당|담당|주요\s*인물)[*\s|]*[:：]?\s*(.+?)(?:\||$)', re.MULTILINE),
        # 본문: 김서현 책임매니저(현대위아)
        re.compile(r'([가-힣]{2,4})\s*(책임매니저|매니저|팀장|과장|차장|부장|대리|주임|이사|전무|대표|상무)\s*\(([^)]+)\)'),
        # 화살표: 김대희 → 대한전선 박성호 과장
        re.compile(r'([가-힣]{2,4})\s*(?:→|->)\s*(?:([가-힣]+)\s+)?([가-힣]{2,4})\s*(과장|차장|부장|대리|주임|이사|팀장|대표)'),
        # 발신자(소속): Christina Gu(ZTT), Alan Zhang (ZTT)
        re.compile(r'([A-Za-z]+\s+[A-Za-z]+)\s*\(([^)]+)\)'),
        # 발신자/회사: 이경렬/금호타이어
        re.compile(r'([가-힣]{2,4})/([가-힣A-Za-z]+)'),
    ]

    def api(self, cmd, args, ctx):
        """연락처 API.
        Returns: {count, contacts}
        - cmd='': 전체 연락처
        - cmd='project', args=[id]: 프로젝트별
        - cmd='search', args=[query]: 이름/회사 검색
        """
        all_contacts = self._extract_all(ctx)
        if cmd == 'project' and args:
            pid = int(args[0])
            filtered = {k: v for k, v in all_contacts.items() if pid in v.get('_projects', set())}
            return {'count': len(filtered), 'contacts': self._serialize(filtered)}
        if cmd == 'search' and args:
            q = args[0].lower()
            filtered = {k: v for k, v in all_contacts.items()
                       if q in k.lower() or q in v.get('company', '').lower()
                       or any(q in p.lower() for p in v.get('projects', []))}
            return {'count': len(filtered), 'contacts': self._serialize(filtered)}
        return {'count': len(all_contacts), 'contacts': self._serialize(all_contacts)}

    def run(self, cmd, args, ctx):
        contacts = self._extract_all(ctx)
        if cmd == 'project' and args:
            pid = int(args[0])
            filtered = {k: v for k, v in contacts.items() if pid in v.get('_projects', set())}
            ctx.output(self._serialize(filtered), lambda d: self._fmt_contacts(d, f"프로젝트 [{pid}]"))
        elif cmd == 'search' and args:
            q = args[0].lower()
            filtered = {k: v for k, v in contacts.items()
                       if q in k.lower() or q in v.get('company', '').lower()
                       or any(q in p.lower() for p in v.get('projects', []))}
            ctx.output(self._serialize(filtered), lambda d: self._fmt_contacts(d, f"검색: {args[0]}"))
        else:
            ctx.output(self._serialize(contacts), lambda d: self._fmt_contacts(d, "전체"))

    def _extract_all(self, ctx):
        """모든 프로젝트에서 연락처 추출"""
        contacts = {}  # key: 이름, value: {company, phones, emails, projects, role}

        # DB 연결을 루프 밖에서 한 번만 열기
        conn = ctx.get_conn()
        try:
            self._extract_all_impl(ctx, contacts, conn)
        finally:
            conn.close()
        return self._filter_contacts(contacts)

    def _extract_all_impl(self, ctx, contacts, conn):
        for pid, proj in ctx.projects.items():
            text = proj['all_text']
            project_name = proj['name']

            # 전화번호 추출
            phones = set(self.PHONE_RE.findall(text))
            # 이메일 추출
            emails = set(self.EMAIL_RE.findall(text))

            # 이름+소속 추출
            for pattern in self.NAME_PATTERNS:
                for m in pattern.finditer(text):
                    groups = m.groups()
                    self._merge_contact(contacts, groups, phones, emails, pid, project_name)

            # 메타데이터의 거래처 담당
            if proj.get('person_external'):
                for part in re.split(r'[,;]', proj['person_external']):
                    part = part.strip()
                    name_m = re.match(r'([가-힣]{2,4}|[A-Za-z]+\s+[A-Za-z]+)\s*(.*)', part)
                    if name_m:
                        name = name_m.group(1).strip()
                        rest = name_m.group(2).strip()
                        role = ''; company = ''
                        role_m = re.search(r'(책임매니저|매니저|팀장|과장|차장|부장|대리|주임|이사|전무|대표)', rest)
                        if role_m: role = role_m.group(1)
                        comp_m = re.search(r'\(([^)]+)\)', rest)
                        if comp_m: company = comp_m.group(1)
                        if name not in contacts:
                            contacts[name] = {'company': company, 'role': role, 'phones': set(), 'emails': set(), 'projects': set(), '_projects': set()}
                        if company: contacts[name]['company'] = company
                        if role: contacts[name]['role'] = role
                        contacts[name]['projects'].add(project_name)
                        contacts[name]['_projects'].add(pid)

            # 커뮤니케이션 로그의 발신자
            comms = conn.execute("SELECT DISTINCT sender FROM comm_log WHERE project_id=?", (pid,)).fetchall()
            for cm in comms:
                sender = cm['sender']
                if not sender or len(sender) < 2:
                    continue
                # 소속 분리: 김서현(현대위아)
                sm = re.match(r'([가-힣]{2,4}|[A-Za-z]+(?:\s+[A-Za-z]+)?)\s*(?:\(([^)]+)\))?', sender)
                if sm:
                    name = sm.group(1).strip()
                    company = (sm.group(2) or '').strip()
                    if name not in contacts:
                        contacts[name] = {'company': company, 'role': '', 'phones': set(), 'emails': set(), 'projects': set(), '_projects': set()}
                    if company and not contacts[name]['company']:
                        contacts[name]['company'] = company
                    contacts[name]['projects'].add(project_name)
                    contacts[name]['_projects'].add(pid)

            # 화살표 패턴 강화: "이름 → 회사 이름 직함" 또는 "회사 이름"
            for am in re.finditer(r'→\s*(?:([가-힣A-Za-z]+(?:\s*[가-힣A-Za-z]+)*?)\s+)?([가-힣]{2,4})\s*(과장|차장|부장|대리|주임|이사|팀장|대표|책임|매니저|상무|전무)?', text):
                company = (am.group(1) or '').strip()
                name = am.group(2).strip()
                role = (am.group(3) or '').strip()
                if name and len(name) >= 2:
                    if name not in contacts:
                        contacts[name] = {'company': company, 'role': role, 'phones': set(), 'emails': set(), 'projects': set(), '_projects': set()}
                    if company and not contacts[name]['company']:
                        contacts[name]['company'] = company
                    if role and not contacts[name]['role']:
                        contacts[name]['role'] = role
                    contacts[name]['projects'].add(project_name)
                    contacts[name]['_projects'].add(pid)

            # 프로젝트 client 필드로 소속 교차참조
            client = proj.get('client', '')
            if client:
                # 사내 담당자가 아닌 사람 중 소속 없는 사람 → client를 소속으로 추정
                internal_names = proj.get('persons', set())
                for name, info in contacts.items():
                    if name in internal_names:
                        continue
                    if not info['company'] and pid in info.get('_projects', set()):
                        # 이 프로젝트에만 등장하는 외부인 → client 소속 추정
                        other_projects = info['_projects'] - {pid}
                        if not other_projects:
                            info['company'] = client

            # 전화번호를 근처 이름에 연결
            for phone in phones:
                # 전화번호 앞뒤 50자에서 이름 찾기
                for pm in re.finditer(re.escape(phone), text):
                    ctx_text = text[max(0,pm.start()-80):pm.end()+30]
                    for name in contacts:
                        if name in ctx_text:
                            contacts[name]['phones'].add(re.sub(r'[-.\s]', '-', phone))
                            break

            # 이메일을 근처 이름에 연결
            for email in emails:
                for em in re.finditer(re.escape(email), text):
                    ctx_text = text[max(0,em.start()-80):em.end()+30]
                    for name in contacts:
                        if name in ctx_text:
                            contacts[name]['emails'].add(email)
                            break

    def _filter_contacts(self, contacts):
        """노이즈 필터"""
        noise_exact = {
            'Vega', '남도에코에너지', '선택님', '클로', 'INBOX', 'CATEGORY', 'RE', 'FW', 'Re',
            '공사도급', '구조물', '기지출', '가배치도', '계약금', '사업비', '견적서',
            '변경계약', '추가사업', '설계변경', '내역서', '사용전검사',
        }
        def _is_valid_contact(name):
            if not name or len(name) < 2:
                return False
            if name in noise_exact:
                return False
            # 한글 2~4자 이름 또는 영문 이름(공백 포함)만 허용
            is_korean_name = re.match(r'^[가-힣]{2,4}$', name)
            is_english_name = re.match(r'^[A-Z][a-z]+(?:\s+[A-Z][a-z]+)+$', name)
            # 한글이지만 이름이 아닌 것 (일반 명사) 필터
            if is_korean_name:
                # 일반 명사 패턴 (동사/형용사 어근, ~사, ~서 등)
                if re.match(r'^(검토|송부|요청|확인|관련|진행|설치|공사|견적|계약|승인|신청|변경|수정|추가|내역|도면|자료|보고|정리|협의|산정|발주|납부|발급|준비|수신|처리|착수|안내|목록|내용|사항|일정|현황|결과|방문|반영|기준|비용|금액|단가|물량|설비|시설|장비|모듈|케이블|배치|구간|공장|현장|사업|프로젝트)', name):
                    return False
                return True
            if is_english_name:
                return True
            # 짧은 영문(이메일 핸들 등)은 제외
            if re.match(r'^[a-z]+\d*$', name) and len(name) < 8:
                return False
            return False

        return {k: v for k, v in contacts.items() if _is_valid_contact(k)}

    def _merge_contact(self, contacts, groups, phones, emails, pid, pname):
        """패턴 매칭 결과를 contacts에 병합"""
        # groups 구조가 패턴마다 다르므로 유연하게 처리
        name = None; company = ''; role = ''
        for g in groups:
            if not g: continue
            g = g.strip()
            if re.match(r'^(책임매니저|매니저|팀장|과장|차장|부장|대리|주임|이사|전무|대표|상무)$', g):
                role = g
            elif re.match(r'^[가-힣]{2,4}$', g) or re.match(r'^[A-Za-z]+\s+[A-Za-z]+$', g):
                if not name:
                    name = g
            elif len(g) > 4:
                company = g
        if not name:
            return
        if name not in contacts:
            contacts[name] = {'company': company, 'role': role, 'phones': set(), 'emails': set(), 'projects': set(), '_projects': set()}
        if company: contacts[name]['company'] = company
        if role: contacts[name]['role'] = role
        contacts[name]['projects'].add(pname)
        contacts[name]['_projects'].add(pid)

    def _serialize(self, contacts):
        return {k: {**v, 'phones': sorted(v.get('phones', set())), 'emails': sorted(v.get('emails', set())),
                     'projects': sorted(v.get('projects', set()))}
                for k, v in sorted(contacts.items()) if k}

    def _fmt_contacts(self, data, label):
        print(f"\n{'━'*70}\n 📇 연락처 ({label}) — {len(data)}명\n{'━'*70}")
        by_company = defaultdict(list)
        for name, info in data.items():
            by_company[info.get('company') or '(소속 미상)'].append((name, info))
        for company in sorted(by_company.keys()):
            people = by_company[company]
            print(f"\n  🏢 {company} ({len(people)}명)")
            for name, info in sorted(people):
                parts = [name]
                if info.get('role'): parts.append(info['role'])
                phone_str = ', '.join(info.get('phones', [])) if info.get('phones') else ''
                email_str = ', '.join(info.get('emails', [])) if info.get('emails') else ''
                proj_str = ', '.join(info.get('projects', []))
                line2 = []
                if phone_str: line2.append(f"📱 {phone_str}")
                if email_str: line2.append(f"✉️  {email_str}")
                print(f"     {' / '.join(parts)}")
                if line2: print(f"       {' | '.join(line2)}")
                if proj_str: print(f"       📂 {proj_str[:60]}")
