import json
import sys
from pathlib import Path
from core import register_command, get_db_connection, _get_flag, _find_project_id, write_audit_log, VegaError

SELF_DIR = Path(__file__).resolve().parent.parent


@register_command('mail-append', read_only=False, category='write')
def _exec_mail_append(params):
    """메일 → 프로젝트 .md 자동 삽입"""
    from mail_to_md import process_mail, process_mail_batch

    sub_args = params.get('sub_args', [])
    dry_run = '--dry-run' in sub_args

    mail_data = None

    # 방법 1: 개별 인수 모드 (--subject, --sender 등)
    subject = _get_flag(sub_args, '--subject') or _get_flag(sub_args, '-s')
    sender = _get_flag(sub_args, '--sender')
    if subject and sender:
        mail_data = {
            'subject': subject,
            'sender': sender,
            'date': _get_flag(sub_args, '--date') or _get_flag(sub_args, '-d') or '',
            'body': _get_flag(sub_args, '--body') or _get_flag(sub_args, '-b') or '',
            'summary': _get_flag(sub_args, '--summary') or '',
            'project': _get_flag(sub_args, '--project') or _get_flag(sub_args, '-p') or '',
        }

    # 방법 2: JSON 문자열
    if not mail_data:
        for arg in sub_args:
            if arg.startswith('{'):
                try:
                    mail_data = json.loads(arg)
                    break
                except json.JSONDecodeError as e:
                    return {
                        'error': f'JSON 파싱 실패: {e}',
                        'input': arg[:200],
                        'usage': '올바른 형식: \'{"subject":"제목", "sender":"발신자", "date":"2026-03-19"}\'',
                    }

    # 방법 3: params에서 직접 전달 (프로그래밍 호출 / MCP)
    if not mail_data and 'mail_data' in params:
        mail_data = params['mail_data']

    # 방법 4: params에 직접 subject/sender가 있는 경우 (MCP tool call)
    if not mail_data and params.get('subject'):
        mail_data = {
            'subject': params.get('subject', ''),
            'sender': params.get('sender', ''),
            'date': params.get('date', ''),
            'body': params.get('body', ''),
            'project': params.get('project', ''),
        }

    if not mail_data:
        return {
            'error': '메일 데이터가 필요합니다',
            'usage': [
                '방법 1 (권장): vega.py mail-append --subject "제목" --sender "발신자" --date "2026-03-19" --project "프로젝트"',
                '방법 2: vega.py mail-append \'{"subject":"...", "sender":"..."}\'',
            ],
        }

    # --project 옵션 (JSON 모드에서도 오버라이드 가능)
    project_override = _get_flag(sub_args, '--project') or _get_flag(sub_args, '-p')
    if project_override and isinstance(mail_data, dict):
        mail_data['project'] = project_override

    if isinstance(mail_data, list):
        return process_mail_batch(mail_data, dry_run=dry_run)
    return process_mail(mail_data, dry_run=dry_run)


@register_command('update', read_only=False, category='write')
def _exec_update(params):
    """프로젝트 상태/필드 업데이트. .md와 DB 동시에 반영."""
    from md_editor import find_md_path, update_meta_field, update_db_field, add_history_entry

    sub_args = params.get('sub_args', [])

    # 프로젝트 참조: sub_args 또는 MCP params에서
    project_ref = sub_args[0] if sub_args else params.get('project', '')
    if not project_ref:
        return {
            'error': '사용법: update <프로젝트ID|이름> --status "진행 중 🟡"',
            'usage': [
                'update 5 --status "완료 🟢"',
                'update "비금도" --status "긴급 대응 중 🔴"',
                'update 5 --field "사내 담당" "고건 팀장"',
            ],
        }

    pid, pname, md_path = find_md_path(project_ref)

    if not pid:
        return {'error': f"프로젝트를 찾을 수 없습니다: {project_ref}"}
    if not md_path:
        return {'error': f"프로젝트 '{pname}'의 .md 파일 없음"}

    # 인수 파싱
    field_name, new_value = None, None

    if _get_flag(sub_args, '--status'):
        field_name = '상태'
        new_value = _get_flag(sub_args, '--status')
    elif _get_flag(sub_args, '--field'):
        field_name = _get_flag(sub_args, '--field')
        # --field 다음다음 인수가 값
        for j, a in enumerate(sub_args):
            if a == '--field' and j + 2 < len(sub_args):
                new_value = sub_args[j + 2]
                break

    # params에서 직접 전달 (MCP)
    if not field_name:
        if params.get('status'):
            field_name = '상태'
            new_value = params['status']
        elif params.get('field') and params.get('value'):
            field_name = params['field']
            new_value = params['value']

    if not field_name or not new_value:
        return {'error': '--status 또는 --field 옵션이 필요합니다'}

    success, old_value, msg = update_meta_field(md_path, field_name, new_value)
    if not success:
        return {'error': msg}

    # DB도 동기화
    db_updated = update_db_field(pid, field_name, new_value)

    # audit_log 기록
    try:
        conn = get_db_connection()
        try:
            write_audit_log(conn, pid, 'update', field=field_name, old_value=old_value, new_value=new_value, actor='user')
            conn.commit()
        finally:
            conn.close()
    except Exception:
        pass

    # 이력에 변경 기록
    add_history_entry(md_path, f"{field_name} 변경: {old_value} → {new_value}")

    return {
        'project_id': pid,
        'project_name': pname,
        'field': field_name,
        'old_value': old_value,
        'new_value': new_value,
        'db_synced': db_updated,
        'summary': f"[{pname}] {msg}",
    }


@register_command('add-action', read_only=False, category='write')
def _exec_add_action(params):
    """다음 예상 액션 항목 추가"""
    from md_editor import find_md_path, add_action_item, add_history_entry

    sub_args = params.get('sub_args', [])

    # 프로젝트 참조: sub_args 또는 MCP params에서
    project_ref = sub_args[0] if sub_args else params.get('project', '')
    is_history = '--history' in sub_args or params.get('history', False)

    # 텍스트 추출 (--history 제외)
    text_parts = [a for a in sub_args[1:] if a != '--history']
    text = ' '.join(text_parts)

    # params에서 직접 전달 (MCP)
    if not text:
        text = params.get('text', params.get('action', ''))

    if not project_ref or not text:
        return {
            'error': '사용법: add-action <프로젝트ID|이름> "액션 내용"',
            'usage': [
                'add-action 5 "FAT 출장 일정 확정 필요"',
                'add-action "비금도" "2차 대금 결제 확인"',
                'add-action 5 --history "CU 헷징 완료"',
            ],
        }

    pid, pname, md_path = find_md_path(project_ref)
    if not pid:
        return {'error': f"프로젝트를 찾을 수 없습니다: {project_ref}"}
    if not md_path:
        return {'error': f"프로젝트 '{pname}'의 .md 파일 없음"}

    if is_history:
        success, msg = add_history_entry(md_path, text)
        section = '이력'
    else:
        success, msg = add_action_item(md_path, text)
        section = '다음 예상 액션'

    if not success:
        return {'error': msg}

    # DB 즉시 반영: chunks 테이블에 해당 섹션 갱신 + 태그 재연결
    try:
        import project_db_v2
        with open(md_path, 'r', encoding='utf-8') as f:
            md_text = f.read().replace('\r\n', '\n')
        meta = project_db_v2.extract_table_meta(md_text)
        sections, _ = project_db_v2.split_sections(md_text)
        tags = project_db_v2.extract_tags(meta, sections)
        conn = get_db_connection()
        try:
            for heading, body, entry_date in sections:
                ctype = project_db_v2.classify_section(heading, body)
                if ctype in ('next_action', 'history') and section in heading:
                    old_ids = [r[0] for r in conn.execute(
                        "SELECT id FROM chunks WHERE project_id=? AND chunk_type=?", (pid, ctype)).fetchall()]
                    if old_ids:
                        conn.execute(f"DELETE FROM chunk_tags WHERE chunk_id IN ({','.join('?' * len(old_ids))})", old_ids)
                    conn.execute("DELETE FROM chunks WHERE project_id=? AND chunk_type=?", (pid, ctype))
                    conn.execute("INSERT INTO chunks (project_id, section_heading, content, chunk_type, entry_date) VALUES (?, ?, ?, ?, ?)",
                                (pid, heading, body, ctype, entry_date))
                    _row = conn.execute("SELECT last_insert_rowid()").fetchone()
                    new_cid = _row[0] if _row else None
                    for tag in (tags if new_cid else []):
                        conn.execute("INSERT OR IGNORE INTO tags (name) VALUES (?)", (tag,))
                        tag_row = conn.execute("SELECT id FROM tags WHERE name=?", (tag,)).fetchone()
                        if tag_row:
                            conn.execute("INSERT OR IGNORE INTO chunk_tags (chunk_id, tag_id) VALUES (?, ?)", (new_cid, tag_row[0]))
            conn.commit()
            db_synced = True
        finally:
            conn.close()
    except Exception:
        db_synced = False

    # audit_log 기록
    try:
        conn_audit = get_db_connection()
        try:
            write_audit_log(conn_audit, pid, 'add-action', field=section, new_value=text, actor='user')
            conn_audit.commit()
        finally:
            conn_audit.close()
    except Exception:
        pass

    return {
        'project_id': pid,
        'project_name': pname,
        'section': section,
        'added_text': text,
        'db_synced': db_synced,
        'summary': f"[{pname}] {section}에 추가: {text}",
    }
