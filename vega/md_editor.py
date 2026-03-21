#!/usr/bin/env python3
"""
md_editor.py — .md 프로젝트 파일 구조화 편집

mail_to_md.py가 날짜 섹션 삽입을 담당한다면,
이 모듈은 메타 테이블, 구조화 섹션(현재 상황, 다음 예상 액션, 이력)의 편집을 담당합니다.

모든 편집은 .md 파일 원본을 직접 수정하며,
DB는 별도로 동기화해야 합니다 (또는 sync-db 데몬이 감지).
"""

import os, re, shutil
from datetime import datetime
import config
from config import get_db_connection


def _backup_file(md_path):
    """편집 전 .md 파일 백업 (.bak 확장자)"""
    import logging as _logging
    bak_path = md_path + '.bak'
    try:
        shutil.copy2(md_path, bak_path)
    except Exception as e:
        _logging.getLogger(__name__).debug("백업 실패 (편집은 진행): %s — %s", md_path, e)


# ──────────────────────────────────────────────
# 1. 프로젝트 .md 파일 찾기
# ──────────────────────────────────────────────

def find_md_path(project_id_or_name, db_path=None, md_dir=None):
    """
    프로젝트 ID(int) 또는 이름(str)으로 .md 파일 경로 반환.

    Returns: (project_id, project_name, md_file_path)
        - 프로젝트 없음: (None, None, None)
        - .md 파일 없음: (id, name, None)
        - 정상: (id, name, "/path/to/file.md")
    """
    db_path = db_path or config.DB_PATH
    md_dir = md_dir or config.MD_DIR
    try:
        conn = get_db_connection(db_path, row_factory=True)
    except Exception:
        return None, None, None

    try:
        if isinstance(project_id_or_name, int) or (isinstance(project_id_or_name, str) and project_id_or_name.isdigit()):
            row = conn.execute("SELECT id, name, source_file FROM projects WHERE id = ?",
                              (int(project_id_or_name),)).fetchone()
        else:
            row = conn.execute("SELECT id, name, source_file FROM projects WHERE name LIKE ?",
                              (f"%{project_id_or_name}%",)).fetchone()
    except Exception:
        row = None
    finally:
        conn.close()

    if not row:
        return None, None, None

    source = row['source_file']
    if source and os.path.exists(source):
        return row['id'], row['name'], source

    # source_file이 없거나 경로가 다르면 md_dir에서 찾기 (캐시 사용)
    cache = _get_md_path_cache(md_dir)
    if cache and row['name']:
        # 정확히 매칭되는 stem 먼저, 부분 매칭 폴백
        for stem, fpath in cache.items():
            if row['name'] in stem or stem in row['name']:
                return row['id'], row['name'], fpath

    return row['id'], row['name'], None


# md_dir rglob 캐시 — mtime 기반 자동 갱신
_md_path_cache = {'paths': {}, 'mtime': 0, 'md_dir': None}

def _get_md_path_cache(md_dir):
    """md_dir의 .md 파일 목록을 {stem: full_path} dict로 캐싱"""
    from pathlib import Path
    md_path = Path(md_dir)
    if not md_path.is_dir():
        return {}
    try:
        mtime = os.path.getmtime(md_dir)
    except OSError:
        mtime = 0
    if (_md_path_cache['paths']
            and mtime <= _md_path_cache['mtime']
            and _md_path_cache.get('md_dir') == md_dir):
        return _md_path_cache['paths']
    paths = {f.stem: str(f) for f in md_path.rglob('*.md')}
    _md_path_cache['paths'] = paths
    _md_path_cache['mtime'] = mtime
    _md_path_cache['md_dir'] = md_dir
    return paths


# ──────────────────────────────────────────────
# 2. 메타 테이블 편집 (상태 등)
# ──────────────────────────────────────────────

def update_meta_field(md_path, field_name, new_value):
    """
    .md 메타 테이블의 필드를 업데이트.
    예: update_meta_field(path, '상태', '완료 🟢')

    Returns: (success, old_value, message)
    """
    _backup_file(md_path)

    with open(md_path, 'r', encoding='utf-8') as f:
        content = f.read().replace('\r\n', '\n')

    # 메타 테이블 행 찾기: | **필드명** | 값 |
    pattern = re.compile(
        rf'(\|\s*\*?\*?{re.escape(field_name)}\*?\*?\s*\|)\s*(.+?)\s*(\|)',
        re.MULTILINE
    )
    match = pattern.search(content)
    if not match:
        return False, None, f"필드 '{field_name}'을(를) 찾을 수 없습니다"

    old_value = match.group(2).strip()
    new_line = f"{match.group(1)} {new_value} {match.group(3)}"
    new_content = content[:match.start()] + new_line + content[match.end():]

    with open(md_path, 'w', encoding='utf-8') as f:
        f.write(new_content)

    return True, old_value, f"'{field_name}' 변경: {old_value} → {new_value}"


def update_db_field(project_id, field, value, db_path=None):
    """DB의 프로젝트 필드도 동시에 업데이트"""
    field_map = {
        '상태': 'status', '발주처': 'client', '고객사': 'client',
        '사내 담당': 'person_internal', '사내담당': 'person_internal',
        '거래처 담당': 'person_external', '거래처담당': 'person_external',
        '규모': 'capacity', '용량': 'capacity', '품목': 'biz_type',
        '파트너': 'partner',
    }
    db_field = field_map.get(field)
    if not db_field:
        return False
    db_path = db_path or config.DB_PATH
    conn = None
    try:
        # 상태에서 이모지 제거 (DB용)
        db_value = value
        if db_field == 'status':
            db_value = re.sub(r'[🟢🟡🟠🔴⚪]', '', value).strip()
        conn = get_db_connection(db_path)
        conn.execute(f"UPDATE projects SET {db_field} = ? WHERE id = ?", (db_value, project_id))
        conn.commit()
        return True
    except Exception:
        return False
    finally:
        if conn:
            conn.close()


# ──────────────────────────────────────────────
# 3. 구조화 섹션 편집 (다음 예상 액션, 이력 등)
# ──────────────────────────────────────────────

def append_to_section(md_path, section_name, text):
    """
    특정 섹션(예: '다음 예상 액션', '이력')에 항목 추가.

    Returns: (success, message)
    """
    _backup_file(md_path)

    with open(md_path, 'r', encoding='utf-8') as f:
        content = f.read().replace('\r\n', '\n')

    # 섹션 헤딩 찾기
    # "## 다음 예상 액션" 또는 "### 다음 예상 액션" 등
    pattern = re.compile(
        rf'^(#{2,3})\s+({re.escape(section_name)}[^\n]*)\n',
        re.MULTILINE
    )
    match = pattern.search(content)

    if not match:
        # 유사 섹션명으로 재시도
        similar = {
            '다음 예상 액션': ['다음 액션', '다음 예상', '액션 아이템', 'Next Action'],
            '이력': ['히스토리', 'History', '경과'],
            '현재 상황': ['현재상황', 'Current Status', '현황'],
            '이슈': ['리스크', '문제점', 'Issues'],
        }
        alternatives = similar.get(section_name, [])
        for alt in alternatives:
            alt_pattern = re.compile(rf'^(#{2,3})\s+({re.escape(alt)}[^\n]*)\n', re.MULTILINE)
            match = alt_pattern.search(content)
            if match:
                break

    if not match:
        # 섹션이 없으면 생성 (## 이력 앞에 삽입)
        history_match = re.search(r'^##\s+이력', content, re.MULTILINE)
        if history_match:
            insert_pos = history_match.start()
            new_section = f"## {section_name}\n{text}\n\n"
            new_content = content[:insert_pos] + new_section + content[insert_pos:]
        else:
            # 첫 날짜 섹션 앞에 삽입
            date_match = re.search(r'^##\s+20\d{2}[-/]', content, re.MULTILINE)
            if date_match:
                insert_pos = date_match.start()
                new_section = f"## {section_name}\n{text}\n\n"
                new_content = content[:insert_pos] + new_section + content[insert_pos:]
            else:
                new_content = content.rstrip() + f"\n\n## {section_name}\n{text}\n"

        with open(md_path, 'w', encoding='utf-8') as f:
            f.write(new_content)
        return True, f"섹션 '{section_name}' 생성 및 항목 추가"

    # 섹션 끝 찾기 (같은 레벨 또는 상위 ## 헤딩, 또는 --- 또는 파일 끝)
    section_level = len(match.group(1))  # 2 for ##, 3 for ###
    section_start = match.end()
    # 같은 레벨 이하(## 이하)의 헤딩만 경계로 인식 (### 하위 섹션은 포함)
    boundary_pattern = r'\n#{2,' + str(section_level) + r'}\s+|\n---'
    next_section = re.search(boundary_pattern, content[section_start:])
    if next_section:
        section_end = section_start + next_section.start()
    else:
        section_end = len(content)

    # 기존 섹션 내용 끝에 추가
    existing = content[section_start:section_end].rstrip()
    new_content = content[:section_start] + existing + '\n' + text + '\n' + content[section_end:]

    with open(md_path, 'w', encoding='utf-8') as f:
        f.write(new_content)

    return True, f"'{section_name}'에 항목 추가"


def add_history_entry(md_path, text, date_str=None):
    """이력 섹션에 날짜 포함 항목 추가"""
    if not date_str:
        date_str = datetime.now().strftime('%Y-%m-%d')
    entry = f"- {date_str}: {text}"
    return append_to_section(md_path, '이력', entry)


def add_action_item(md_path, text):
    """다음 예상 액션 섹션에 항목 추가"""
    entry = f"- {text}"
    return append_to_section(md_path, '다음 예상 액션', entry)
