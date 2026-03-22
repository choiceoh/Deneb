#!/usr/bin/env python3
"""
mail_to_md.py — 메일 → 프로젝트 .md 자동 삽입

메일 데이터를 받아:
  1. 프로젝트 자동 매칭 (발신자/제목/키워드 기반)
  2. 해당 .md 파일의 날짜 섹션에 표준 포맷으로 삽입
  3. 매칭 불확실 시 후보 목록 반환 (AI가 선택)

사용법:
  # JSON stdin으로 메일 데이터 전달
  echo '{"subject":"비금도 케이블 납기 건","sender":"Christina Gu","date":"2026-03-19","body":"..."}' | python3 mail_to_md.py

  # CLI 인수로 전달
  python3 mail_to_md.py --subject "비금도 케이블" --sender "Christina" --date "2026-03-19" --body "..."

  # 프로젝트 지정 (매칭 생략)
  python3 mail_to_md.py --project "비금도" --subject "..." --sender "..." --date "..." --body "..."

  # 후보만 조회 (삽입 안 함)
  python3 mail_to_md.py --dry-run --subject "비금도 케이블" --sender "Christina"

  # vega.py에서 호출
  python3 vega.py mail-append '{"subject":"...", ...}'

출력 (JSON):
  {"status": "ok", "matched_project": "비금도", "file": "비금도.md", "action": "inserted"}
  {"status": "candidates", "candidates": [{"project": "비금도", "score": 85}, ...]}
  {"status": "error", "error": "매칭 실패"}
"""

import json, os, re, sys
from pathlib import Path
from datetime import datetime
import config
from config import get_db_connection

# ──────────────────────────────────────────────
# 1. 프로젝트 매칭 엔진
# ──────────────────────────────────────────────

def _load_project_index(db_path=None):
    """DB에서 프로젝트 인덱스 로드: {name, file_path, client, persons, keywords}"""
    db_path = db_path or config.DB_PATH
    if not os.path.exists(db_path):
        return []
    conn = get_db_connection(db_path, row_factory=True)
    projects = []
    try:
        rows = conn.execute("""
            SELECT id, name, source_file, client, person_internal, person_external,
                   status, capacity, biz_type, partner
            FROM projects
        """).fetchall()
        for r in rows:
            # comm_log에서 발신자 목록 추출
            senders = conn.execute(
                "SELECT DISTINCT sender FROM comm_log WHERE project_id = ?",
                (r['id'],)
            ).fetchall()
            sender_names = [s['sender'] for s in senders if s['sender']]

            projects.append({
                'id': r['id'],
                'name': r['name'] or '',
                'file_path': r['source_file'] or '',
                'client': r['client'] or '',
                'person_internal': r['person_internal'] or '',
                'person_external': r['person_external'] or '',
                'partner': r['partner'] or '',
                'senders': sender_names,
                # 매칭용 키워드: 프로젝트명 토큰 + 고객사
                'keywords': _extract_keywords(r),
            })
    except Exception as e:
        import logging as _logging
        _logging.getLogger(__name__).warning("프로젝트 인덱스 로드 실패: %s", e)
    finally:
        conn.close()
    return projects


def _extract_keywords(row):
    """프로젝트 행에서 매칭용 키워드 추출"""
    tokens = set()
    for field in ('name', 'client', 'partner'):
        val = row[field] or ''
        # 한글/영문 토큰 분리
        tokens.update(re.findall(r'[가-힣]{2,}|[a-zA-Z]{2,}', val))
    return tokens


def match_project(subject, sender, body=None, db_path=None, project_name=None):
    """
    메일을 프로젝트에 매칭.

    Returns:
        (matched_project, score, candidates)
        - matched_project: 확실한 매칭 시 프로젝트 dict, 없으면 None
        - score: 매칭 점수 (0~100)
        - candidates: [(project, score), ...] 후보 리스트 (점수 내림차순)
    """
    projects = _load_project_index(db_path)
    if not projects:
        return None, 0, []

    # 프로젝트 이름 직접 지정
    if project_name:
        for p in projects:
            if project_name.lower() in p['name'].lower() or p['name'].lower() in project_name.lower():
                return p, 100, [(p, 100)]
        # 부분 매칭
        for p in projects:
            if any(project_name.lower() in kw.lower() for kw in p['keywords']):
                return p, 90, [(p, 90)]
        # 지정한 이름으로 찾을 수 없으면 매칭 실패 반환 (자동 매칭으로 넘어가지 않음)
        return None, 0, []

    text = f"{subject} {sender} {body or ''}"
    text_lower = text.lower()

    scored = []
    for p in projects:
        score = 0

        # 1. 프로젝트명 매칭 (가장 강력)
        name = p['name']
        if name and len(name) >= 2:
            # 정확한 프로젝트명 포함
            if name in subject:
                score += 50
            elif name.lower() in text_lower:
                score += 30
            # 프로젝트명 토큰 매칭
            name_tokens = re.findall(r'[가-힣]{2,}|[a-zA-Z]{2,}', name)
            for tok in name_tokens:
                if tok.lower() in text_lower:
                    score += 15

        # 2. 발신자 매칭
        sender_lower = sender.lower() if sender else ''
        if sender_lower:
            # comm_log 발신자와 매칭
            for s in p['senders']:
                s_lower = s.lower()
                if sender_lower in s_lower or s_lower in sender_lower:
                    score += 25
                    break
            # person_external 매칭
            for field in ('person_external', 'person_internal'):
                persons = p[field].lower()
                if sender_lower in persons:
                    score += 20
                    break

        # 3. 고객사/파트너 매칭
        for field in ('client', 'partner'):
            val = p[field]
            if val and len(val) >= 2 and val.lower() in text_lower:
                score += 20

        # 4. 키워드 매칭 (본문)
        if body:
            keyword_hits = sum(1 for kw in p['keywords']
                             if len(kw) >= 2 and kw.lower() in text_lower)
            score += min(keyword_hits * 5, 20)

        if score > 0:
            scored.append((p, score))

    # 점수 내림차순 정렬 (동점 시 프로젝트 ID 오름차순으로 결정적 순서 보장)
    scored.sort(key=lambda x: (-x[1], x[0].get('id', 0)))

    if not scored:
        return None, 0, []

    best_project, best_score = scored[0]

    # 확실한 매칭 기준: 40점 이상 & 2위와 15점 이상 차이
    if best_score >= 40:
        if len(scored) < 2 or (best_score - scored[1][1]) >= 15:
            return best_project, best_score, scored[:5]

    return None, best_score, scored[:5]


# ──────────────────────────────────────────────
# 2. .md 파일 삽입
# ──────────────────────────────────────────────

DATE_HEADING_RE = re.compile(r'^(#{2,3})\s+(20\d{2}[-/]\d{2}[-/]\d{2})', re.MULTILINE)


_MAX_SUMMARY_LEN = 2000  # 요약 최대 길이 (문자 수)

def _format_entry(subject, sender, summary=None):
    """표준 커뮤니케이션 로그 엔트리 포맷"""
    entry = f"- **{subject}** ({sender})"
    if summary:
        # 요약을 들여쓰기 라인으로 (과도한 길이 방지)
        summary = summary.strip()
        if len(summary) > _MAX_SUMMARY_LEN:
            summary = summary[:_MAX_SUMMARY_LEN] + '...(생략)'
        if summary:
            lines = summary.split('\n')
            for line in lines:
                line = line.strip()
                if line:
                    entry += f"\n  - {line}"
    return entry


def _find_md_file(project, md_dir=None):
    """프로젝트에 해당하는 .md 파일 경로 찾기"""
    md_dir = md_dir or config.MD_DIR
    # file_path가 있으면 사용
    if project.get('file_path'):
        fp = project['file_path']
        if os.path.isabs(fp) and os.path.exists(fp):
            return fp
        # 상대경로로 시도
        candidate = os.path.join(md_dir, os.path.basename(fp))
        if os.path.exists(candidate):
            return candidate

    # 프로젝트명으로 찾기
    name = project.get('name', '')
    if not name:
        return None

    md_dir_path = Path(md_dir)
    if not md_dir_path.exists():
        return None

    # 정확한 이름 매칭
    exact = md_dir_path / f"{name}.md"
    if exact.exists():
        return str(exact)

    # 부분 매칭
    for f in md_dir_path.rglob('*.md'):
        if f.is_symlink():
            continue
        if name in f.stem or f.stem in name:
            return str(f)

    return None


def insert_to_md(md_path, date_str, entry_text):
    """
    .md 파일의 날짜 섹션에 엔트리 삽입.
    날짜 섹션이 없으면 적절한 위치에 생성.
    시간순(최신이 위) 유지.

    Returns: (success, message)
    """
    if not os.path.exists(md_path):
        return False, f"파일 없음: {md_path}"

    with open(md_path, 'r', encoding='utf-8-sig') as f:
        content = f.read().replace('\r\n', '\n')

    # 날짜 정규화
    date_str = date_str.replace('/', '-')

    # 기존 날짜 헤딩 찾기
    date_headings = list(DATE_HEADING_RE.finditer(content))
    target_heading = None
    for m in date_headings:
        heading_date = m.group(2).replace('/', '-')
        if heading_date == date_str:
            target_heading = m
            break

    if target_heading:
        # 기존 날짜 섹션에 추가
        # 헤딩 바로 다음 줄에 삽입
        insert_pos = target_heading.end()
        # 줄바꿈 이후에 삽입
        if insert_pos < len(content) and content[insert_pos] == '\n':
            insert_pos += 1

        new_content = content[:insert_pos] + entry_text + '\n' + content[insert_pos:]
    else:
        # 새 날짜 섹션 생성 — 시간순으로 적절한 위치에 삽입
        new_section = f"## {date_str}\n{entry_text}\n"

        if date_headings:
            # 기존 날짜들과 비교하여 삽입 위치 결정 (최신이 위)
            insert_before = None
            for m in date_headings:
                heading_date = m.group(2).replace('/', '-')
                if date_str > heading_date:
                    insert_before = m
                    break

            if insert_before:
                # 이 헤딩 바로 앞에 삽입
                pos = insert_before.start()
                new_content = content[:pos] + new_section + '\n' + content[pos:]
            else:
                # 모든 기존 날짜보다 오래됨 → 마지막 날짜 섹션 뒤에 삽입
                last = date_headings[-1]
                # 마지막 날짜 섹션의 끝 찾기
                end_pos = _find_section_end(content, last.end())
                new_content = content[:end_pos] + '\n' + new_section + content[end_pos:]
        else:
            # 날짜 섹션이 하나도 없음 → 파일 끝에 추가
            # <!-- reads: --> 태그 앞에 삽입
            reads_match = re.search(r'\n<!-- reads:', content)
            if reads_match:
                pos = reads_match.start()
                new_content = content[:pos] + '\n' + new_section + content[pos:]
            else:
                new_content = content.rstrip() + '\n\n' + new_section

    with open(md_path, 'w', encoding='utf-8') as f:
        f.write(new_content)

    return True, "삽입 완료"


def _find_section_end(content, start_pos):
    """다음 ## 헤딩이나 파일 끝까지의 위치 반환"""
    next_heading = re.search(r'\n#{2,3}\s+', content[start_pos:])
    if next_heading:
        return start_pos + next_heading.start()
    # <!-- reads: --> 태그 찾기
    reads_match = re.search(r'\n<!-- reads:', content[start_pos:])
    if reads_match:
        return start_pos + reads_match.start()
    return len(content)


# ──────────────────────────────────────────────
# 3. 통합 API
# ──────────────────────────────────────────────

def process_mail(mail_data, db_path=None, md_dir=None, dry_run=False):
    """
    메일 처리 메인 함수.

    Args:
        mail_data: dict with keys:
            - subject (str): 메일 제목 (필수)
            - sender (str): 발신자 이름 또는 이메일 (필수)
            - date (str): YYYY-MM-DD (필수)
            - body (str): 메일 본문 요약 (선택)
            - summary (str): 삽입할 요약 텍스트 (선택, body와 별도)
            - project (str): 프로젝트명 직접 지정 (선택)
        dry_run: True면 매칭만 하고 삽입 안 함

    Returns:
        dict: {status, matched_project, file, action, candidates, ...}
    """
    subject = (mail_data.get('subject') or '').strip()
    sender = (mail_data.get('sender') or '').strip()
    date_str = (mail_data.get('date') or '').strip()
    body = (mail_data.get('body') or '').strip()
    summary = (mail_data.get('summary') or body or '').strip()  # summary가 없으면 body 사용
    project_name = (mail_data.get('project') or '').strip()

    # 필수 필드 검증
    if not subject:
        return {'status': 'error', 'error': '제목(subject)이 필요합니다'}
    if not sender:
        return {'status': 'error', 'error': '발신자(sender)가 필요합니다'}
    if not date_str:
        # 오늘 날짜 사용
        date_str = datetime.now().strftime('%Y-%m-%d')
    elif not re.match(r'^20\d{2}[-/]\d{2}[-/]\d{2}$', date_str):
        return {'status': 'error', 'error': f'날짜 형식이 올바르지 않습니다: {date_str} (YYYY-MM-DD)'}
    else:
        # 실제 날짜 유효성 검증 (2099-99-99 같은 값 방지)
        try:
            datetime.strptime(date_str.replace('/', '-'), '%Y-%m-%d')
        except ValueError:
            return {'status': 'error', 'error': f'유효하지 않은 날짜입니다: {date_str}'}

    date_str = date_str.replace('/', '-')

    # 프로젝트 매칭
    matched, score, candidates = match_project(
        subject, sender, body, db_path, project_name or None
    )

    # 후보 리스트 (공통)
    candidate_list = [
        {'project': c['name'], 'score': s, 'client': c.get('client', '')}
        for c, s in candidates[:5]
    ]

    if not matched and not candidates:
        return {
            'status': 'ok',
            'action': 'no_match',
            'matched_project': None,
            'candidates': [],
            'mail': {'subject': subject, 'sender': sender, 'date': date_str},
            'summary': '매칭되는 프로젝트가 없습니다. --project 옵션으로 프로젝트를 지정하세요.',
        }

    if not matched:
        return {
            'status': 'ok',
            'action': 'needs_selection',
            'matched_project': None,
            'candidates': candidate_list,
            'mail': {'subject': subject, 'sender': sender, 'date': date_str},
            'summary': f'{len(candidate_list)}개 후보 프로젝트 발견. --project 옵션으로 선택하세요.',
        }

    if dry_run:
        return {
            'status': 'ok',
            'action': 'dry_run',
            'matched_project': matched['name'],
            'score': score,
            'candidates': candidate_list,
            'summary': f"매칭 결과: '{matched['name']}' (점수 {score}). 실행 시 삽입됩니다.",
        }

    # .md 파일 찾기
    md_path = _find_md_file(matched, md_dir)
    if not md_path:
        return {
            'status': 'error',
            'action': 'file_not_found',
            'error': f"프로젝트 '{matched['name']}'의 .md 파일을 찾을 수 없습니다",
            'matched_project': matched['name'],
            'summary': f"'{matched['name']}'의 .md 파일 없음. MD_DIR을 확인하세요.",
        }

    # 엔트리 포맷팅
    entry_text = _format_entry(subject, sender, summary if summary != body else None)

    # 중복 체크
    with open(md_path, 'r', encoding='utf-8-sig') as f:
        existing = f.read().replace('\r\n', '\n')
    # 동일 제목+발신자가 같은 날짜에 이미 있으면 스킵
    if subject in existing and f"({sender})" in existing:
        # 더 정확한 체크: 같은 날짜 섹션 내에 있는지
        pattern = re.compile(
            rf'##\s+{re.escape(date_str)}.*?(?=\n##|\Z)',
            re.DOTALL
        )
        date_section = pattern.search(existing)
        if date_section and subject in date_section.group():
            return {
                'status': 'ok',
                'action': 'skipped',
                'matched_project': matched['name'],
                'file': os.path.basename(md_path),
                'summary': f"'{subject}'은(는) 이미 {matched['name']}에 존재합니다. 중복 삽입 방지.",
            }

    # 삽입
    success, msg = insert_to_md(md_path, date_str, entry_text)
    if not success:
        return {
            'status': 'error',
            'error': msg,
            'matched_project': matched['name'],
        }

    # DB 자동 동기화: .md 변경 → DB 즉시 반영
    db_synced = _auto_sync_db(md_path, matched['id'], date_str, subject, sender, summary, db_path)

    return {
        'status': 'ok',
        'matched_project': matched['name'],
        'file': os.path.basename(md_path),
        'date': date_str,
        'action': 'inserted',
        'score': score,
        'db_synced': db_synced,
        'summary': f"'{subject}' → {matched['name']} ({os.path.basename(md_path)}) {date_str}에 삽입됨",
    }


def _auto_sync_db(md_path, project_id, date_str, subject, sender, summary, db_path):
    """mail-append 후 DB에 즉시 반영 (전체 재빌드 없이 comm_log에 직접 삽입)"""
    conn = None
    try:
        conn = get_db_connection(db_path)
        # 중복 체크: 같은 프로젝트/날짜/제목/발신자가 이미 있으면 스킵
        existing = conn.execute(
            "SELECT 1 FROM comm_log WHERE project_id = ? AND log_date = ? AND subject = ? AND sender = ? LIMIT 1",
            (project_id, date_str, subject, sender)
        ).fetchone()
        if not existing:
            conn.execute(
                "INSERT INTO comm_log (project_id, log_date, sender, subject, summary) VALUES (?, ?, ?, ?, ?)",
                (project_id, date_str, sender, subject, summary or '')
            )
        conn.commit()  # comm_log 먼저 확정 (chunks 실패와 무관하게 보존)
        # 해당 파일만 증분 재파싱 (chunks 갱신 — 해당 날짜만)
        try:
            import project_db_v2
            with open(md_path, 'r', encoding='utf-8-sig') as f:
                text = f.read().replace('\r\n', '\n')
            sections, _ = project_db_v2.split_sections(text)
            for heading, body, entry_date in sections:
                if entry_date == date_str:
                    ctype = project_db_v2.classify_section(heading, body)
                    conn.execute(
                        "DELETE FROM chunks WHERE project_id=? AND entry_date=? AND section_heading=?",
                        (project_id, entry_date, heading)
                    )
                    conn.execute(
                        "INSERT INTO chunks (project_id, section_heading, content, chunk_type, entry_date) VALUES (?, ?, ?, ?, ?)",
                        (project_id, heading, body, ctype, entry_date)
                    )
            conn.commit()  # chunks 갱신 확정
        except Exception as e:
            import logging
            logging.warning("mail_to_md: chunks 갱신 실패 (project_id=%s, date=%s): %s", project_id, date_str, e)
        return True
    except Exception:
        return False
    finally:
        if conn:
            conn.close()


def process_mail_batch(mails, db_path=None, md_dir=None, dry_run=False):
    """여러 메일을 일괄 처리"""
    results = []
    for mail in mails:
        result = process_mail(mail, db_path, md_dir, dry_run)
        results.append(result)
    return {
        'status': 'ok',
        'total': len(mails),
        'inserted': sum(1 for r in results if r.get('action') == 'inserted'),
        'skipped': sum(1 for r in results if r.get('action') == 'skipped'),
        'failed': sum(1 for r in results if r.get('status') == 'error' or r.get('action') == 'no_match'),
        'needs_review': sum(1 for r in results if r.get('action') == 'needs_selection'),
        'results': results,
    }


# ──────────────────────────────────────────────
# 4. CLI
# ──────────────────────────────────────────────

def main():
    import argparse

    parser = argparse.ArgumentParser(description='메일 → .md 자동 삽입')
    parser.add_argument('--subject', '-s', help='메일 제목')
    parser.add_argument('--sender', help='발신자')
    parser.add_argument('--date', '-d', help='날짜 (YYYY-MM-DD)')
    parser.add_argument('--body', '-b', help='메일 본문 (요약)')
    parser.add_argument('--summary', help='삽입할 요약 텍스트')
    parser.add_argument('--project', '-p', help='프로젝트명 직접 지정')
    parser.add_argument('--dry-run', action='store_true', help='매칭만 확인 (삽입 안 함)')
    parser.add_argument('--batch', action='store_true', help='stdin에서 JSON 배열 읽기')
    parser.add_argument('--db', default=None, help='DB 경로')
    parser.add_argument('--md-dir', default=None, help='.md 디렉토리')

    args = parser.parse_args()

    # stdin JSON 입력 (파이프 또는 --batch)
    if args.batch or (not args.subject and not sys.stdin.isatty()):
        try:
            data = json.load(sys.stdin)
        except json.JSONDecodeError as e:
            print(json.dumps({'status': 'error', 'error': f'JSON 파싱 실패: {e}'}, ensure_ascii=False))
            sys.exit(1)

        if isinstance(data, list):
            result = process_mail_batch(data, args.db, args.md_dir, args.dry_run)
        elif isinstance(data, dict):
            result = process_mail(data, args.db, args.md_dir, args.dry_run)
        else:
            result = {'status': 'error', 'error': '입력은 JSON 객체 또는 배열이어야 합니다'}

        print(json.dumps(result, ensure_ascii=False, indent=2))
        return

    # CLI 인수 모드
    if not args.subject:
        parser.print_help()
        sys.exit(1)

    mail_data = {
        'subject': args.subject,
        'sender': args.sender or '',
        'date': args.date or '',
        'body': args.body or '',
        'summary': args.summary or '',
        'project': args.project or '',
    }

    result = process_mail(mail_data, args.db, args.md_dir, args.dry_run)
    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
