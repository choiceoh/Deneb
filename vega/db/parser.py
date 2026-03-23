"""Markdown parsing: table metadata extraction, section splitting, comm log parsing."""

import re


# ──────────────────────────────────────────────
# 2. 마크다운 테이블 메타데이터 파서
# ──────────────────────────────────────────────

def extract_table_meta(text):
    """| 항목 | 내용 | 형태의 마크다운 테이블에서 메타데이터 추출"""
    meta = {}

    # 프로젝트명: 첫 번째 # 제목
    title_match = re.search(r'^#\s+(.+)', text, re.MULTILINE)
    if title_match:
        meta['name'] = title_match.group(1).strip()

    # 마크다운 테이블 행 파싱
    table_rows = re.findall(
        r'^\|\s*\*?\*?(.+?)\*?\*?\s*\|\s*(.+?)\s*\|',
        text, re.MULTILINE
    )

    field_map = {
        '상태': 'status',
        '발주처': 'client',
        '고객사': 'client',
        '사내 담당': 'person_internal',
        '사내담당': 'person_internal',
        '거래처 담당': 'person_external',
        '거래처담당': 'person_external',
        '규모': 'capacity',
        '용량': 'capacity',
        '품목': 'biz_type',
        '사업구조': 'biz_type',
        '파트너': 'partner',
        '주요 인물': 'person_external',
        '현대엔지니어링': 'person_external',
        '해저케이블': '_해저케이블',
        'CU 헷징': '_CU헷징',
        '모듈': '_모듈',
        '금융': '_금융',
        '신규 소유자': 'client',
    }

    for raw_key, raw_val in table_rows:
        key = raw_key.strip().replace('*', '').strip()
        val = raw_val.strip().replace('*', '').strip()

        if key in ('항목', '---', '-', '내용', '구분'):
            continue

        mapped = field_map.get(key)
        if mapped and not mapped.startswith('_'):
            if mapped not in meta or not meta[mapped]:
                meta[mapped] = val
        elif mapped and mapped.startswith('_'):
            meta[mapped] = val

    # 상태에서 이모지 제거
    if 'status' in meta:
        meta['status'] = re.sub(r'[🟢🟡🟠🔴⚪]', '', meta['status']).strip()

    return meta


# ──────────────────────────────────────────────
# 3. 섹션 분할
# ──────────────────────────────────────────────

DATE_HEADING_RE = re.compile(r'^#{1,3}\s+(20\d{2}[-/]\d{2}[-/]\d{2})', re.MULTILINE)
HEADING_RE = re.compile(r'^(#{1,3})\s+(.+)', re.MULTILINE)

def split_sections(text):
    """섹션 분할: 구조화 섹션 + 날짜별 로그"""
    sections = []
    comm_entries = []

    parts = HEADING_RE.split(text)

    # 첫 번째 부분 (헤딩 전)
    if parts[0].strip():
        # 테이블 메타를 제외한 도입부
        intro = parts[0].strip()
        # 테이블 이후 텍스트만
        table_end = 0
        for m in re.finditer(r'^\|.+\|$', intro, re.MULTILINE):
            table_end = m.end()
        remaining = intro[table_end:].strip()
        if remaining and len(remaining) > 20:
            sections.append(('개요', remaining, None))

    i = 1
    while i < len(parts):
        if i + 2 > len(parts):
            break
        level = parts[i]  # '#' or '##' etc.
        heading = parts[i+1].strip()
        body = parts[i+2].strip() if i+2 < len(parts) else ""
        i += 3

        # 날짜 헤딩 감지
        date_match = re.match(r'^(20\d{2}[-/]\d{2}[-/]\d{2})', heading)
        if date_match:
            entry_date = date_match.group(1).replace('/', '-')
            # 이 아래 내용을 커뮤니케이션 로그로 파싱
            _parse_comm_block(entry_date, body, comm_entries)
            # 섹션으로도 저장 (전문검색용)
            if body:
                sections.append((f"로그 {entry_date}", body, entry_date))
        else:
            if body:
                sections.append((heading, body, None))

    return sections, comm_entries


def _parse_comm_block(date_str, body, comm_entries):
    """날짜 블록 안의 커뮤니케이션 항목 파싱 (관대한 모드)

    매칭 패턴:
    1. - **제목** (발신자)         ← 표준 형식
    2. - 제목 (발신자)            ← 볼드 없음
    3. - **제목**                 ← 발신자 누락
    4. - 제목                    ← 둘 다 없음 → 이전 항목 summary에 병합
    """
    lines = body.split('\n')
    current_subject = None
    current_sender = None
    current_summary_lines = []

    # 확장 패턴: 볼드 유/무 + 발신자 유/무
    _COMM_PATTERNS = [
        re.compile(r'^[-*]\s*\*{1,2}(.+?)\*{1,2}\s*\(([^)]+)\)\s*$'),     # **제목** (발신자)
        re.compile(r'^[-*]\s+(.+?)\s*\(([^)]+)\)\s*$'),                     # 제목 (발신자)
        re.compile(r'^[-*]\s*\*{1,2}(.+?)\*{1,2}\s*$'),                     # **제목** (발신자 없음)
    ]

    for line in lines:
        stripped = line.strip()
        if not stripped:
            continue

        matched = False
        for pi, pat in enumerate(_COMM_PATTERNS):
            m = pat.match(stripped)
            if m:
                # 이전 항목 저장
                if current_subject:
                    comm_entries.append({
                        'date': date_str,
                        'sender': current_sender or '',
                        'subject': current_subject.strip(),
                        'summary': '\n'.join(current_summary_lines).strip()
                    })
                current_subject = m.group(1).strip().strip('*').strip()
                current_sender = m.group(2).strip() if m.lastindex >= 2 else ''
                current_summary_lines = []
                matched = True
                break

        if not matched:
            # 매칭 안 됨 → 이전 항목의 summary에 병합, 또는 plain bullet을 새 항목으로
            if stripped.startswith('>'):
                current_summary_lines.append(stripped.lstrip('>').strip())
            elif stripped.startswith('-') or stripped.startswith('*'):
                plain_text = stripped.lstrip('-*').strip()
                if current_subject:
                    # 이전 항목 있으면 summary에 병합
                    current_summary_lines.append(plain_text)
                elif plain_text and len(plain_text) >= 5:
                    # 이전 항목 없는 plain bullet → 독립 comm entry로 (v1.34)
                    current_subject = plain_text
                    current_sender = ''
                    current_summary_lines = []
            elif current_subject:
                current_summary_lines.append(stripped)

    # 마지막 항목
    if current_subject:
        comm_entries.append({
            'date': date_str,
            'sender': current_sender or '',
            'subject': current_subject.strip(),
            'summary': '\n'.join(current_summary_lines).strip()
        })
