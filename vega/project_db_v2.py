#!/usr/bin/env python3
"""
Vega 프로젝트 .md → SQLite 변환 및 다중 조건 검색 도구 (v2)

실제 파일 포맷 대응:
  - 마크다운 테이블(| 항목 | 내용 |) 메타데이터 추출
  - 날짜별 커뮤니케이션 로그 파싱
  - 태양광 EPC 도메인 키워드 태깅

사용법:
  python project_db_v2.py import ./md_files
  python project_db_v2.py search "해저케이블"
  python project_db_v2.py search --client "기아" --status "진행"
  python project_db_v2.py search "O&M" --person "고건"
  python project_db_v2.py search --tag "기술:ESS"
  python project_db_v2.py list
  python project_db_v2.py show 1
  python project_db_v2.py tags
  python project_db_v2.py timeline 1
"""

import sqlite3
import os
import re
import sys
import argparse
from pathlib import Path
from datetime import datetime



_self_dir = str(Path(__file__).parent)
if _self_dir not in sys.path:
    sys.path.insert(0, _self_dir)
import config
from config import set_schema_version


# ──────────────────────────────────────────────
# 1. 스키마
# ──────────────────────────────────────────────

SCHEMA = """
CREATE TABLE IF NOT EXISTS projects (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT,
    client TEXT,
    status TEXT,
    capacity TEXT,
    biz_type TEXT,
    person_internal TEXT,
    person_external TEXT,
    partner TEXT,
    source_file TEXT UNIQUE,
    imported_at TEXT,
    source_type TEXT DEFAULT 'project'
);

CREATE TABLE IF NOT EXISTS chunks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER REFERENCES projects(id),
    section_heading TEXT,
    content TEXT,
    chunk_type TEXT,
    entry_date TEXT,
    start_line INTEGER,
    end_line INTEGER
);

CREATE TABLE IF NOT EXISTS tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE
);

CREATE TABLE IF NOT EXISTS chunk_tags (
    chunk_id INTEGER REFERENCES chunks(id),
    tag_id INTEGER REFERENCES tags(id),
    PRIMARY KEY (chunk_id, tag_id)
);

CREATE TABLE IF NOT EXISTS comm_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER REFERENCES projects(id),
    log_date TEXT,
    sender TEXT,
    subject TEXT,
    summary TEXT
);

CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
    project_name,
    client,
    section_heading,
    content,
    content='chunks',
    content_rowid='id',
    tokenize='unicode61'
);

CREATE TRIGGER IF NOT EXISTS chunks_ai AFTER INSERT ON chunks BEGIN
    INSERT INTO chunks_fts(rowid, project_name, client, section_heading, content)
    SELECT NEW.id, p.name, p.client, NEW.section_heading, NEW.content
    FROM projects p WHERE p.id = NEW.project_id;
END;

CREATE VIRTUAL TABLE IF NOT EXISTS comm_fts USING fts5(
    project_name,
    sender,
    subject,
    summary,
    content='comm_log',
    content_rowid='id',
    tokenize='unicode61'
);

CREATE TRIGGER IF NOT EXISTS comm_ai AFTER INSERT ON comm_log BEGIN
    INSERT INTO comm_fts(rowid, project_name, sender, subject, summary)
    SELECT NEW.id, p.name, NEW.sender, NEW.subject, NEW.summary
    FROM projects p WHERE p.id = NEW.project_id;
END;

CREATE TABLE IF NOT EXISTS file_hashes (
    source_file TEXT PRIMARY KEY,
    content_hash TEXT,
    updated_at TEXT
);

CREATE TABLE IF NOT EXISTS audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER REFERENCES projects(id),
    action TEXT,
    actor TEXT DEFAULT 'user',
    field TEXT,
    old_value TEXT,
    new_value TEXT,
    timestamp TEXT DEFAULT (datetime('now'))
);

-- FTS5 trigram 테이블 (부분 문자열 매칭, SQLite 3.34+)
-- unicode61은 토큰 단위, trigram은 부분 문자열 매칭 지원
-- "해저케이블" → "해저 케이블"도 매칭
CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts_trigram USING fts5(
    project_name,
    content,
    content='chunks',
    content_rowid='id',
    tokenize='trigram'
);

CREATE TRIGGER IF NOT EXISTS chunks_tri_ai AFTER INSERT ON chunks BEGIN
    INSERT INTO chunks_fts_trigram(rowid, project_name, content)
    SELECT NEW.id, p.name, NEW.content
    FROM projects p WHERE p.id = NEW.project_id;
END;

-- 벡터 임베딩 저장 (v1.4: 로컬 모델 내장)
CREATE TABLE IF NOT EXISTS chunk_embeddings (
    chunk_id INTEGER PRIMARY KEY REFERENCES chunks(id),
    embedding BLOB NOT NULL,
    model_name TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- chunk 삭제 시 임베딩도 삭제
CREATE TRIGGER IF NOT EXISTS chunks_emb_ad AFTER DELETE ON chunks BEGIN
    DELETE FROM chunk_embeddings WHERE chunk_id = OLD.id;
END;

-- DELETE 트리거: 증분 업데이트 시 FTS 인덱스 정합성 유지
CREATE TRIGGER IF NOT EXISTS chunks_ad AFTER DELETE ON chunks BEGIN
    INSERT INTO chunks_fts(chunks_fts, rowid, project_name, client, section_heading, content)
    SELECT 'delete', OLD.id, p.name, p.client, OLD.section_heading, OLD.content
    FROM projects p WHERE p.id = OLD.project_id;
END;

CREATE TRIGGER IF NOT EXISTS chunks_tri_ad AFTER DELETE ON chunks BEGIN
    INSERT INTO chunks_fts_trigram(chunks_fts_trigram, rowid, project_name, content)
    SELECT 'delete', OLD.id, p.name, OLD.content
    FROM projects p WHERE p.id = OLD.project_id;
END;

CREATE TRIGGER IF NOT EXISTS comm_ad AFTER DELETE ON comm_log BEGIN
    INSERT INTO comm_fts(comm_fts, rowid, project_name, sender, subject, summary)
    SELECT 'delete', OLD.id, p.name, OLD.sender, OLD.subject, OLD.summary
    FROM projects p WHERE p.id = OLD.project_id;
END;
"""


def init_db(db_path=None):
    db_path = db_path or config.DB_PATH
    conn = sqlite3.connect(db_path, timeout=10)
    conn.execute("PRAGMA journal_mode=WAL")
    conn.execute("PRAGMA busy_timeout=5000")
    conn.executescript(SCHEMA)

    # ── Migration logic ──
    user_ver = conn.execute("PRAGMA user_version").fetchone()[0]
    if user_ver < 4:
        try:
            conn.execute("ALTER TABLE projects ADD COLUMN amount REAL")
        except Exception:
            pass  # column already exists
        conn.execute("""
            CREATE TABLE IF NOT EXISTS audit_log (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                project_id INTEGER REFERENCES projects(id),
                action TEXT,
                actor TEXT DEFAULT 'user',
                field TEXT,
                old_value TEXT,
                new_value TEXT,
                timestamp TEXT DEFAULT (datetime('now'))
            )
        """)

    if user_ver < 5:
        # v1.4: 벡터 임베딩 테이블 + 삭제 트리거
        conn.execute("""
            CREATE TABLE IF NOT EXISTS chunk_embeddings (
                chunk_id INTEGER PRIMARY KEY REFERENCES chunks(id),
                embedding BLOB NOT NULL,
                model_name TEXT NOT NULL,
                updated_at TEXT NOT NULL
            )
        """)
        conn.execute("""
            CREATE TRIGGER IF NOT EXISTS chunks_emb_ad AFTER DELETE ON chunks BEGIN
                DELETE FROM chunk_embeddings WHERE chunk_id = OLD.id;
            END
        """)

    if user_ver < 6:
        # v1.43: memory backend — 라인 번호 + source_type
        for stmt in [
            "ALTER TABLE chunks ADD COLUMN start_line INTEGER",
            "ALTER TABLE chunks ADD COLUMN end_line INTEGER",
            "ALTER TABLE projects ADD COLUMN source_type TEXT DEFAULT 'project'",
        ]:
            try:
                conn.execute(stmt)
            except Exception:
                pass  # column already exists

    set_schema_version(conn)
    conn.commit()
    return conn


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


# ──────────────────────────────────────────────
# 4. 섹션 유형 분류
# ──────────────────────────────────────────────

def classify_section(heading, content):
    h = (heading or "").lower()
    c = (content or "").lower()

    if any(k in h for k in ['현재 상황', '현재상황', '개요', '프로젝트 개요']):
        return 'status'
    if any(k in h for k in ['다음 예상', '액션']):
        return 'next_action'
    if any(k in h for k in ['이력']):
        return 'history'
    if any(k in h for k in ['로그 20']):
        return 'comm_log'
    if any(k in h for k in ['기술', '사양', '사업 기본']):
        return 'technical'
    if any(k in h for k in ['이슈', '리스크', '문제', '화재']):
        return 'issue'
    if any(k in h for k in ['일정', '마일스톤', '공정']):
        return 'schedule'
    if any(k in h for k in ['경제성', '투자비', '공사비', '운영비', '재무']):
        return 'financial'
    if any(k in h for k in ['인허가', '규제']):
        return 'permit'
    if any(k in h for k in ['결론', '요약', '종합']):
        return 'summary'
    if any(k in h for k in ['관련 메일', '메일']):
        return 'comm_log'
    if any(k in h for k in ['첨부', '자료']):
        return 'attachment'

    # 본문 기반
    if any(k in c for k in ['견적', '계약서', '공사도급']):
        return 'contract'
    if any(k in c for k in ['화재', '소손', '클레임']):
        return 'issue'

    return 'other'


# ──────────────────────────────────────────────
# 5. 태그 추출 (태양광 EPC 도메인)
# ──────────────────────────────────────────────

def extract_tags(meta, sections):
    tags = set()

    # 메타 기반
    if meta.get('client'):
        tags.add(f"고객:{meta['client']}")
    if meta.get('person_internal'):
        for p in re.split(r'[,/·]', meta['person_internal']):
            p = p.strip()
            if p:
                tags.add(f"담당:{p}")
    if meta.get('status'):
        tags.add(f"상태:{meta['status']}")

    all_text = ' '.join(body for _, body, _ in sections).lower()
    all_text += ' ' + (meta.get('name', '') or '').lower()
    all_text += ' ' + (meta.get('biz_type', '') or '').lower()

    # 테이블 메타 _ 접두어 필드 → 태그 (v1.34)
    for key, val in meta.items():
        if key.startswith('_') and val:
            tag_name = key.lstrip('_')
            tags.add(f"기술:{tag_name}")

    # 태양광 EPC 도메인 키워드
    tech_kw = {
        'EPC': ['epc', '시공'],
        'O&M': ['o&m', '운영관리', '유지관리', '유지보수'],
        'PPA': ['ppa', '직접전력거래', '전력거래'],
        '설비리스': ['설비리스', '리스사업', '임대차'],
        'ESS': ['ess', 'bess', '에너지저장'],
        '해저케이블': ['해저케이블', 'submarine cable', '154kv'],
        '모듈': ['모듈', 'module', '진코', 'jinko', 'ja solar', '트리나', '한화'],
        '인버터': ['인버터', 'inverter', '화웨이', 'huawei', 'pcs'],
        '구조검토': ['구조검토', '구조계산', '구조물'],
        'TPO방수': ['tpo', '방수', '현대l&c'],
        '환경공단': ['환경공단', '탄소중립', '감축설비', '지원사업'],
        'PF금융': ['pf', '팩토링', '대출', '금융조건', '펀드'],
        'CU헷징': ['헷징', 'hedging', 'lme'],
        'MC4화재': ['mc4', '커넥터 화재', '소손'],
        '해상풍력': ['해상풍력', '풍력', '풍황'],
        '수상태양광': ['수상태양광', '수상'],
        '접속단': ['접속단', '계통연계', 'kepco', '한전'],
        'REC': ['rec', 'rec 가중치', 'rps'],
        'SMP': ['smp'],
    }
    for tag, keywords in tech_kw.items():
        if any(kw in all_text for kw in keywords):
            tags.add(f"기술:{tag}")

    # 프로젝트 유형
    type_kw = {
        '지붕태양광': ['지붕', '루프탑', 'rooftop'],
        '주차장태양광': ['주차장', '캐노피'],
        '수상태양광': ['수상', '석문호'],
        '지상태양광': ['토지', '지상'],
        '해상풍력': ['해상풍력'],
        'BESS': ['bess', 'ess 발전'],
    }
    for tag, keywords in type_kw.items():
        if any(kw in all_text for kw in keywords):
            tags.add(f"유형:{tag}")

    # 현대차그룹 관련
    hyundai = ['현대', '기아', '모비스', '글로비스', '위아']
    if any(kw in all_text for kw in hyundai):
        tags.add("그룹:현대차")

    return tags


# ──────────────────────────────────────────────
# 6. 임포트
# ──────────────────────────────────────────────

def import_files(directory, db_path=None):
    db_path = db_path or config.DB_PATH
    conn = init_db(db_path)
    try:
        cur = conn.cursor()

        md_files = sorted(
            f for f in Path(directory).rglob("*.md")
            if not f.is_symlink()
        )
        if not md_files:
            print("파일을 찾을 수 없습니다.")
            return

        imported = 0
        errors = []
        for fpath in md_files:
            fname = fpath.name
            fpath_str = str(fpath.resolve())
            try:
                text = fpath.read_text(encoding='utf-8-sig')
                text = text.replace('\r\n', '\n')

                # source_file은 파일명 또는 전체 경로로 확인 (이전 버전 호환)
                if cur.execute("SELECT id FROM projects WHERE source_file=? OR source_file=?", (fname, fpath_str)).fetchone():
                    print(f"  건너뜀: {fname}")
                    continue

                meta = extract_table_meta(text)
                sections, comm_entries = split_sections(text)
                tags = extract_tags(meta, sections)

                # 프로젝트 삽입 (source_file에 전체 경로 저장)
                cur.execute("""
                    INSERT INTO projects (name, client, status, capacity, biz_type,
                                          person_internal, person_external, partner,
                                          source_file, imported_at)
                    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                """, (
                    meta.get('name', fname.replace('.md', '')),
                    meta.get('client'),
                    meta.get('status'),
                    meta.get('capacity'),
                    meta.get('biz_type'),
                    meta.get('person_internal'),
                    meta.get('person_external'),
                    meta.get('partner'),
                    fpath_str,
                    datetime.now().isoformat()
                ))
                pid = cur.lastrowid

                # 섹션 삽입
                for heading, body, entry_date in sections:
                    ctype = classify_section(heading, body)
                    cur.execute("""
                        INSERT INTO chunks (project_id, section_heading, content, chunk_type, entry_date)
                        VALUES (?, ?, ?, ?, ?)
                    """, (pid, heading, body, ctype, entry_date))
                    cid = cur.lastrowid

                    for tag in tags:
                        cur.execute("INSERT OR IGNORE INTO tags (name) VALUES (?)", (tag,))
                        tag_row = cur.execute("SELECT id FROM tags WHERE name=?", (tag,)).fetchone()
                        if tag_row:
                            cur.execute("INSERT OR IGNORE INTO chunk_tags (chunk_id, tag_id) VALUES (?, ?)", (cid, tag_row[0]))

                # 커뮤니케이션 로그 삽입
                for entry in comm_entries:
                    cur.execute("""
                        INSERT INTO comm_log (project_id, log_date, sender, subject, summary)
                        VALUES (?, ?, ?, ?, ?)
                    """, (pid, entry['date'], entry['sender'], entry['subject'], entry['summary']))

                imported += 1
                print(f"  ✓ {fname} → {len(sections)}섹션, {len(comm_entries)}로그, {len(tags)}태그")
            except Exception as e:
                errors.append(f"{fname}: {e}")
                print(f"  ✗ {fname} — 오류: {e}")
                continue

        if errors:
            print(f"\n⚠ {len(errors)}개 파일 오류 (나머지 {imported}개 정상 임포트)")

        conn.commit()

        stats = cur.execute("""
            SELECT
                (SELECT COUNT(*) FROM projects),
                (SELECT COUNT(*) FROM chunks),
                (SELECT COUNT(*) FROM comm_log),
                (SELECT COUNT(DISTINCT name) FROM tags)
        """).fetchone()
        print(f"\n완료: {imported}개 임포트")
        print(f"DB: 프로젝트 {stats[0]}개 | 섹션 {stats[1]}개 | 커뮤니케이션 {stats[2]}건 | 태그 {stats[3]}종")
    finally:
        conn.close()


def import_incremental(directory, db_path=None):
    """증분 업데이트: 변경된 .md 파일만 재파싱 (프로젝트 ID 유지)"""
    db_path = db_path or config.DB_PATH
    conn = init_db(db_path)
    try:
        _import_incremental_impl(conn, directory)
    finally:
        conn.close()


def upsert_md_file(cur, fpath, existing_hashes=None):
    """단일 .md 파일을 파싱하여 DB에 upsert. 공유 로직.

    Returns:
        'updated' | 'skipped' | None (에러 시)
    Raises:
        Exception on parse/DB errors (호출자가 처리)
    """
    import hashlib
    fname = fpath.name
    fpath_str = str(fpath.resolve())

    text = fpath.read_text(encoding='utf-8-sig').replace('\r\n', '\n')
    content_hash = hashlib.md5(text.encode('utf-8')).hexdigest()

    # 해시 동일하면 스킵
    if existing_hashes:
        if existing_hashes.get(fpath_str) == content_hash or existing_hashes.get(fname) == content_hash:
            return 'skipped'

    meta = extract_table_meta(text)
    sections, comm_entries = split_sections(text)
    tags = extract_tags(meta, sections)

    # 기존 프로젝트 확인
    old_proj = cur.execute(
        "SELECT id FROM projects WHERE source_file=? OR source_file=?",
        (fname, fpath_str)
    ).fetchone()

    if old_proj:
        pid = old_proj[0]
        cur.execute("""
            UPDATE projects SET name=?, client=?, status=?, capacity=?, biz_type=?,
                person_internal=?, person_external=?, partner=?,
                source_file=?, imported_at=?
            WHERE id=?
        """, (
            meta.get('name', fname.replace('.md', '')),
            meta.get('client'), meta.get('status'), meta.get('capacity'),
            meta.get('biz_type'), meta.get('person_internal'),
            meta.get('person_external'), meta.get('partner'),
            fpath_str, datetime.now().isoformat(), pid
        ))
        cur.execute("DELETE FROM comm_log WHERE project_id=?", (pid,))
        cur.execute("DELETE FROM chunk_tags WHERE chunk_id IN (SELECT id FROM chunks WHERE project_id=?)", (pid,))
        cur.execute("DELETE FROM chunks WHERE project_id=?", (pid,))
    else:
        cur.execute("""
            INSERT INTO projects (name, client, status, capacity, biz_type,
                                  person_internal, person_external, partner,
                                  source_file, imported_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        """, (
            meta.get('name', fname.replace('.md', '')),
            meta.get('client'), meta.get('status'), meta.get('capacity'),
            meta.get('biz_type'), meta.get('person_internal'),
            meta.get('person_external'), meta.get('partner'),
            fpath_str, datetime.now().isoformat()
        ))
        pid = cur.lastrowid

    # 섹션/태그 삽입
    for heading, body, entry_date in sections:
        ctype = classify_section(heading, body)
        cur.execute(
            "INSERT INTO chunks (project_id, section_heading, content, chunk_type, entry_date) VALUES (?, ?, ?, ?, ?)",
            (pid, heading, body, ctype, entry_date))
        cid = cur.lastrowid
        for tag in tags:
            cur.execute("INSERT OR IGNORE INTO tags (name) VALUES (?)", (tag,))
            tag_row = cur.execute("SELECT id FROM tags WHERE name=?", (tag,)).fetchone()
            if tag_row:
                cur.execute("INSERT OR IGNORE INTO chunk_tags (chunk_id, tag_id) VALUES (?, ?)", (cid, tag_row[0]))

    # 커뮤니케이션 로그
    for entry in comm_entries:
        cur.execute(
            "INSERT INTO comm_log (project_id, log_date, sender, subject, summary) VALUES (?, ?, ?, ?, ?)",
            (pid, entry['date'], entry['sender'], entry['subject'], entry['summary']))

    # 해시 갱신
    cur.execute("DELETE FROM file_hashes WHERE source_file=?", (fname,))
    cur.execute(
        "INSERT OR REPLACE INTO file_hashes (source_file, content_hash, updated_at) VALUES (?, ?, ?)",
        (fpath_str, content_hash, datetime.now().isoformat()))

    return 'updated'


def delete_project_by_source(cur, source_key):
    """source_file 키로 프로젝트 및 관련 데이터 삭제."""
    old_proj = cur.execute(
        "SELECT id FROM projects WHERE source_file=? OR source_file=?",
        (source_key, os.path.basename(source_key))
    ).fetchone()
    if old_proj:
        pid = old_proj[0]
        cur.execute("DELETE FROM comm_log WHERE project_id=?", (pid,))
        cur.execute("DELETE FROM chunk_tags WHERE chunk_id IN (SELECT id FROM chunks WHERE project_id=?)", (pid,))
        cur.execute("DELETE FROM chunks WHERE project_id=?", (pid,))
        cur.execute("DELETE FROM projects WHERE id=?", (pid,))
    cur.execute("DELETE FROM file_hashes WHERE source_file=?", (source_key,))


def _import_incremental_impl(conn, directory):
    """import_incremental 구현부 (conn은 호출자가 관리)."""
    cur = conn.cursor()

    md_files = sorted(
        f for f in Path(directory).rglob("*.md")
        if not f.is_symlink()
    )
    if not md_files:
        print("파일을 찾을 수 없습니다.")
        return

    # 기존 해시 로드 (키가 파일명일 수도 전체경로일 수도 있음 — 양쪽 다 확인)
    try:
        existing_hashes = {row[0]: row[1] for row in cur.execute("SELECT source_file, content_hash FROM file_hashes")}
    except Exception:
        existing_hashes = {}

    updated = 0
    skipped = 0
    current_files = set()

    for fpath in md_files:
        current_files.add(str(fpath.resolve()))
        current_files.add(fpath.name)
        try:
            result = upsert_md_file(cur, fpath, existing_hashes)
            if result == 'skipped':
                skipped += 1
            elif result == 'updated':
                updated += 1
                print(f"  ✓ {fpath.name}")
        except Exception as e:
            print(f"  ✗ {fpath.name} — 오류: {e}")
            continue

    # 삭제된 파일 처리
    for key in set(existing_hashes.keys()) - current_files:
        delete_project_by_source(cur, key)
        print(f"  ✗ {os.path.basename(key)} (삭제됨)")

    # FTS 인덱스 정합성 보장: 대량 변경 시에만 rebuild (트리거가 기본 정합성 유지)
    if updated > 10:
        rebuild_fts(conn)

    conn.commit()
    print(f"\n증분 업데이트 완료: {updated}개 변경, {skipped}개 변경 없음")


def rebuild_fts(conn):
    """FTS 인덱스 전체 rebuild. health/system에서 명시적 호출 가능."""
    import logging as _logging
    try:
        cur = conn.cursor()
        cur.execute("INSERT INTO chunks_fts(chunks_fts) VALUES('rebuild')")
        cur.execute("INSERT INTO chunks_fts_trigram(chunks_fts_trigram) VALUES('rebuild')")
        cur.execute("INSERT INTO comm_fts(comm_fts) VALUES('rebuild')")
    except Exception as e:
        _logging.getLogger(__name__).warning("FTS rebuild 실패 (데이터는 보존): %s", e)


# ──────────────────────────────────────────────
# 7. 검색
# ──────────────────────────────────────────────

# FTS5 예약어 (쿼리에 들어가면 크래시)
_FTS_RESERVED = {'AND', 'OR', 'NOT', 'NEAR'}

def _sanitize_fts(term):
    """FTS5 안전 변환: 특수문자/예약어를 따옴표로 감싸기"""
    if not term or not term.strip():
        return None
    t = term.strip()
    if t.upper() in _FTS_RESERVED:
        return f'"{t}"'
    # FTS5 column filter (e.g., "project_name:비금도") → 따옴표 감싸기
    if ':' in t:
        return f'"{t}"'
    if re.search(r'[&|!@#$%^*()\-+=\[\]{}<>?/\\~`]', t):
        return f'"{t}"'
    if not re.search(r'[가-힣a-zA-Z0-9]', t):
        return None
    return t


def search(query=None, client=None, status=None, person=None, tag=None,
           chunk_type=None, date_from=None, date_to=None, db_path=None):
    db_path = db_path or config.DB_PATH
    conn = sqlite3.connect(db_path)
    conn.row_factory = sqlite3.Row

    # 섹션 검색
    conditions = []
    params = []
    sql = """
        SELECT DISTINCT
            c.id as chunk_id, p.id as project_id,
            p.name as project_name, p.client, p.status,
            p.person_internal, p.capacity,
            c.section_heading, c.content, c.chunk_type, c.entry_date
        FROM chunks c
        JOIN projects p ON c.project_id = p.id
    """
    if query:
        safe_q = _sanitize_fts(query)
        if safe_q:
            sql += " JOIN chunks_fts fts ON fts.rowid = c.id"
            conditions.append("chunks_fts MATCH ?")
            params.append(safe_q)
        else:
            conditions.append("c.content LIKE ?")
            params.append(f"%{query}%")
    if client:
        conditions.append("(p.client LIKE ? OR p.name LIKE ?)")
        params.extend([f"%{client}%", f"%{client}%"])
    if status:
        conditions.append("p.status LIKE ?")
        params.append(f"%{status}%")
    if person:
        conditions.append("(p.person_internal LIKE ? OR c.content LIKE ?)")
        params.extend([f"%{person}%", f"%{person}%"])
    if chunk_type:
        conditions.append("c.chunk_type = ?")
        params.append(chunk_type)
    if tag:
        sql += " JOIN chunk_tags ct ON ct.chunk_id = c.id JOIN tags t ON t.id = ct.tag_id"
        conditions.append("t.name LIKE ?")
        params.append(f"%{tag}%")
    if date_from:
        conditions.append("c.entry_date >= ?")
        params.append(date_from)
    if date_to:
        conditions.append("c.entry_date <= ?")
        params.append(date_to)

    if conditions:
        sql += " WHERE " + " AND ".join(conditions)
    sql += " ORDER BY p.id, c.id"

    try:
        chunk_results = conn.execute(sql, params).fetchall()
    except Exception:
        # FTS 실패 → LIKE 폴백
        if query:
            fallback_sql = """
                SELECT DISTINCT
                    c.id as chunk_id, p.id as project_id,
                    p.name as project_name, p.client, p.status,
                    p.person_internal, p.capacity,
                    c.section_heading, c.content, c.chunk_type, c.entry_date
                FROM chunks c
                JOIN projects p ON c.project_id = p.id
                WHERE c.content LIKE ?
                ORDER BY p.id, c.id
            """
            chunk_results = conn.execute(fallback_sql, [f"%{query}%"]).fetchall()
        else:
            chunk_results = []

    # 커뮤니케이션 로그도 검색
    comm_results = []
    if query:
        safe_q = _sanitize_fts(query)
        if safe_q:
            try:
                comm_sql = """
                    SELECT cl.id, p.id as project_id, p.name as project_name,
                           cl.log_date, cl.sender, cl.subject, cl.summary
                    FROM comm_log cl
                    JOIN projects p ON cl.project_id = p.id
                    JOIN comm_fts cf ON cf.rowid = cl.id
                    WHERE comm_fts MATCH ?
                """
                comm_params = [safe_q]
                if client:
                    comm_sql += " AND (p.client LIKE ? OR p.name LIKE ?)"
                    comm_params.extend([f"%{client}%", f"%{client}%"])
                if person:
                    comm_sql += " AND cl.sender LIKE ?"
                    comm_params.append(f"%{person}%")
                comm_sql += " ORDER BY cl.log_date DESC LIMIT 20"
                comm_results = conn.execute(comm_sql, comm_params).fetchall()
            except Exception:
                pass  # FTS 실패 시 무시

    conn.close()
    return chunk_results, comm_results


def print_results(chunk_results, comm_results):
    if not chunk_results and not comm_results:
        print("검색 결과 없음.")
        return

    # 섹션 결과
    if chunk_results:
        from collections import OrderedDict
        grouped = OrderedDict()
        for r in chunk_results:
            pid = r['project_id']
            if pid not in grouped:
                grouped[pid] = {
                    'name': r['project_name'],
                    'client': r['client'],
                    'status': r['status'],
                    'person': r['person_internal'],
                    'capacity': r['capacity'],
                    'chunks': []
                }
            grouped[pid]['chunks'].append({
                'heading': r['section_heading'],
                'content': r['content'][:300] + ('...' if len(r['content']) > 300 else ''),
                'type': r['chunk_type'],
                'date': r['entry_date']
            })

        print(f"\n{'='*70}")
        print(f" 섹션 매칭: {len(grouped)}개 프로젝트, {len(chunk_results)}개 섹션")
        print(f"{'='*70}")

        type_icons = {
            'status': '📋', 'next_action': '🎯', 'history': '📜',
            'comm_log': '📬', 'technical': '🔧', 'issue': '⚠️',
            'schedule': '📅', 'financial': '💰', 'permit': '📑',
            'summary': '📊', 'contract': '📝', 'attachment': '📎',
            'other': '📄'
        }

        for pid, proj in grouped.items():
            print(f"\n┌─ [{pid}] {proj['name']}")
            info = []
            if proj['client']: info.append(f"고객:{proj['client']}")
            if proj['person']: info.append(f"담당:{proj['person']}")
            if proj['status']: info.append(f"상태:{proj['status']}")
            if proj['capacity']: info.append(f"규모:{proj['capacity']}")
            print(f"│  {' | '.join(info)}")
            for ch in proj['chunks'][:5]:  # 최대 5개
                icon = type_icons.get(ch['type'], '📄')
                date_str = f" ({ch['date']})" if ch['date'] else ""
                print(f"│  {icon} [{ch['heading']}]{date_str}")
                for line in ch['content'].split('\n')[:4]:
                    print(f"│    {line}")
            if len(proj['chunks']) > 5:
                print(f"│  ... +{len(proj['chunks'])-5}개 더")
            print(f"└{'─'*69}")

    # 커뮤니케이션 로그 결과
    if comm_results:
        print(f"\n{'='*70}")
        print(f" 커뮤니케이션 매칭: {len(comm_results)}건")
        print(f"{'='*70}")
        for r in comm_results[:10]:
            print(f"  📬 [{r['log_date']}] {r['project_name']}")
            print(f"     {r['sender']}: {r['subject'][:80]}")
            if r['summary']:
                print(f"     → {r['summary'][:120]}")


# ──────────────────────────────────────────────
# 8. 유틸리티
# ──────────────────────────────────────────────

def list_projects(db_path=None):
    db_path = db_path or config.DB_PATH
    conn = sqlite3.connect(db_path)
    rows = conn.execute("""
        SELECT p.id, p.name, p.client, p.status, p.person_internal, p.capacity,
               (SELECT COUNT(*) FROM chunks WHERE project_id=p.id) as chunks,
               (SELECT COUNT(*) FROM comm_log WHERE project_id=p.id) as comms
        FROM projects p ORDER BY p.id
    """).fetchall()
    conn.close()

    print(f"\n{'ID':>3} | {'프로젝트':<30} | {'고객/발주처':<15} | {'상태':<20} | {'담당':<15} | 섹션 | 로그")
    print("-" * 120)
    for r in rows:
        print(f"{r[0]:>3} | {(r[1] or '-')[:30]:<30} | {(r[2] or '-')[:15]:<15} | {(r[3] or '-')[:20]:<20} | {(r[4] or '-')[:15]:<15} | {r[6]:>4} | {r[7]:>4}")


def show_project(project_id, db_path=None):
    db_path = db_path or config.DB_PATH
    conn = sqlite3.connect(db_path)
    conn.row_factory = sqlite3.Row

    proj = conn.execute("SELECT * FROM projects WHERE id=?", (project_id,)).fetchone()
    if not proj:
        print(f"프로젝트 ID {project_id} 없음.")
        return

    chunks = conn.execute("SELECT * FROM chunks WHERE project_id=? ORDER BY id", (project_id,)).fetchall()
    tags = conn.execute("""
        SELECT DISTINCT t.name FROM tags t
        JOIN chunk_tags ct ON ct.tag_id = t.id
        JOIN chunks c ON c.id = ct.chunk_id
        WHERE c.project_id = ?
    """, (project_id,)).fetchall()
    comms = conn.execute("""
        SELECT * FROM comm_log WHERE project_id=? ORDER BY log_date DESC LIMIT 10
    """, (project_id,)).fetchall()
    conn.close()

    print(f"\n{'='*70}")
    print(f" {proj['name']}")
    print(f"{'='*70}")
    fields = [
        ('고객/발주처', proj['client']),
        ('상태', proj['status']),
        ('규모', proj['capacity']),
        ('사업유형', proj['biz_type']),
        ('사내담당', proj['person_internal']),
        ('거래처담당', proj['person_external']),
        ('파트너', proj['partner']),
    ]
    for label, val in fields:
        if val:
            print(f" {label}: {val}")
    print(f" 태그: {', '.join(t['name'] for t in tags)}")
    print(f"{'─'*70}")

    # 구조화 섹션 (비로그)
    for c in chunks:
        if c['chunk_type'] != 'comm_log':
            print(f"\n  [{c['chunk_type']}] {c['section_heading']}")
            print(f"  {'─'*40}")
            for line in c['content'].split('\n')[:10]:
                print(f"  {line}")
            if len(c['content'].split('\n')) > 10:
                print(f"  ... (생략)")

    # 최근 커뮤니케이션
    if comms:
        print(f"\n{'─'*70}")
        print(f"  최근 커뮤니케이션 (최대 10건)")
        for cm in comms:
            print(f"  📬 [{cm['log_date']}] {cm['sender']}: {cm['subject'][:70]}")


def show_timeline(project_id, db_path=None):
    db_path = db_path or config.DB_PATH
    conn = sqlite3.connect(db_path)
    conn.row_factory = sqlite3.Row
    
    proj = conn.execute("SELECT name FROM projects WHERE id=?", (project_id,)).fetchone()
    if not proj:
        print(f"프로젝트 ID {project_id} 없음.")
        return

    comms = conn.execute("""
        SELECT * FROM comm_log WHERE project_id=? ORDER BY log_date DESC
    """, (project_id,)).fetchall()
    conn.close()

    print(f"\n📅 {proj['name']} 타임라인 ({len(comms)}건)")
    print(f"{'='*70}")
    for cm in comms:
        print(f"  {cm['log_date']} | {cm['sender']}: {cm['subject'][:65]}")
        if cm['summary']:
            print(f"           → {cm['summary'][:100]}")


def list_tags(db_path=None):
    db_path = db_path or config.DB_PATH
    conn = sqlite3.connect(db_path)
    rows = conn.execute("""
        SELECT t.name, COUNT(DISTINCT c.project_id) as cnt
        FROM tags t
        JOIN chunk_tags ct ON ct.tag_id = t.id
        JOIN chunks c ON c.id = ct.chunk_id
        GROUP BY t.name ORDER BY t.name
    """).fetchall()
    conn.close()

    print(f"\n{'태그':<30} | 프로젝트 수")
    print("-" * 45)
    for r in rows:
        print(f"{r[0]:<30} | {r[1]}")


# ──────────────────────────────────────────────
# 9. CLI
# ──────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(description="Vega 프로젝트 DB (v2)")
    sub = parser.add_subparsers(dest='command')

    p_imp = sub.add_parser('import', help='.md → DB 변환')
    p_imp.add_argument('directory')
    p_imp.add_argument('--db', default=None)
    p_imp.add_argument('--incremental', action='store_true', help='증분 업데이트 (변경 파일만)')

    p_s = sub.add_parser('search', help='다중 조건 검색')
    p_s.add_argument('query', nargs='?', help='전문 검색어')
    p_s.add_argument('--client', help='고객사')
    p_s.add_argument('--status', help='상태')
    p_s.add_argument('--person', help='담당자')
    p_s.add_argument('--tag', help='태그')
    p_s.add_argument('--type', dest='chunk_type', help='섹션 유형')
    p_s.add_argument('--from', dest='date_from', help='시작일 (YYYY-MM-DD)')
    p_s.add_argument('--to', dest='date_to', help='종료일')
    p_s.add_argument('--db', default=None)

    p_l = sub.add_parser('list', help='프로젝트 목록')
    p_l.add_argument('--db', default=None)

    p_sh = sub.add_parser('show', help='상세 보기')
    p_sh.add_argument('id', type=int)
    p_sh.add_argument('--db', default=None)

    p_tl = sub.add_parser('timeline', help='타임라인')
    p_tl.add_argument('id', type=int)
    p_tl.add_argument('--db', default=None)

    p_t = sub.add_parser('tags', help='태그 목록')
    p_t.add_argument('--db', default=None)

    args = parser.parse_args()

    if args.command == 'import':
        if getattr(args, 'incremental', False):
            import_incremental(args.directory, args.db)
        else:
            import_files(args.directory, args.db)
    elif args.command == 'search':
        if not any([args.query, args.client, args.status, args.person, args.tag, args.chunk_type]):
            print("조건을 하나 이상 입력하세요.")
            print("예: python project_db_v2.py search '케이블' --client '비금도'")
            return
        cr, cm = search(args.query, args.client, args.status, args.person,
                        args.tag, args.chunk_type, args.date_from, args.date_to, args.db)
        print_results(cr, cm)
    elif args.command == 'list':
        list_projects(args.db)
    elif args.command == 'show':
        show_project(args.id, args.db)
    elif args.command == 'timeline':
        show_timeline(args.id, args.db)
    elif args.command == 'tags':
        list_tags(args.db)
    else:
        parser.print_help()


if __name__ == '__main__':
    main()
