import os
import sys
import re
from datetime import datetime
from pathlib import Path
import config
from core import register_command, get_db_connection, _ensure_db

SELF_DIR = Path(__file__).resolve().parent.parent


def _health_summary(d):
    issues = d.get('issues', [])
    if not issues:
        return f"정상: DB {d.get('db_projects',0)}개 프로젝트"
    return f"이슈 {len(issues)}건: {'; '.join(issues[:3])}"


@register_command('health', needs_db=False, summary_fn=_health_summary)
def _exec_health(params=None):
    """시스템 상태 자가 진단 + 자동 복구"""
    issues = []

    # DB 상태
    db_exists = os.path.exists(config.DB_PATH)
    db_size = os.path.getsize(config.DB_PATH) if db_exists else 0
    db_projects = 0
    if db_exists:
        try:
            conn = get_db_connection()
            try:
                result = conn.execute("SELECT COUNT(*) FROM projects").fetchone()
                db_projects = result[0] if result else 0
            finally:
                conn.close()
        except Exception as e:
            issues.append(f"DB 읽기 실패: {e}")
    else:
        issues.append("DB 파일 없음")
        # 자동 복구 시도
        if _ensure_db():
            issues[-1] += " → 자동 재빌드 성공"
            db_exists = os.path.exists(config.DB_PATH)
            db_size = os.path.getsize(config.DB_PATH) if db_exists else 0
            try:
                conn = get_db_connection()
                try:
                    result = conn.execute("SELECT COUNT(*) FROM projects").fetchone()
                    db_projects = result[0] if result else 0
                finally:
                    conn.close()
            except Exception:
                pass
        else:
            issues[-1] += " → 자동 재빌드 실패"

    # MD 디렉토리
    md_exists = os.path.isdir(config.MD_DIR)
    md_count = 0
    if md_exists:
        md_count = sum(1 for _ in Path(config.MD_DIR).rglob('*.md'))
    else:
        issues.append(f"MD 디렉토리 없음: {config.MD_DIR}")

    # sync-db 데몬 (Windows에서는 os.kill(pid,0)이 프로세스를 종료시킴 — 분기 처리)
    pid_file = SELF_DIR / '.sync-db.pid'
    sync_daemon = False
    if pid_file.exists():
        try:
            spid = int(pid_file.read_text().strip())
            if sys.platform == 'win32':
                import ctypes
                kernel32 = ctypes.windll.kernel32
                h = kernel32.OpenProcess(0x100000, False, spid)  # SYNCHRONIZE
                if h:
                    kernel32.CloseHandle(h)
                    sync_daemon = True
            else:
                os.kill(spid, 0)
                sync_daemon = True
        except (ValueError, OSError, Exception):
            issues.append("sync-db 데몬 PID 파일은 있으나 프로세스 미작동")

    if not sync_daemon:
        if sys.platform == 'win32':
            pass  # Windows에서는 sync-db 데몬 없어도 정상
        else:
            issues.append("sync-db 데몬 미작동 (bootstrap.sh로 재시작 가능)")

    # .snapshot.json
    snap = SELF_DIR / '.snapshot.json'
    if not snap.exists():
        issues.append(".snapshot.json 없음 (changelog 첫 실행 시 전부 신규로 표시)")

    # 데이터 분류 통계 (health 모니터링)
    classification_stats = {}
    if db_exists and db_projects > 0:
        try:
            conn = get_db_connection()
            try:
                type_rows = conn.execute(
                    "SELECT chunk_type, COUNT(*) as cnt FROM chunks GROUP BY chunk_type ORDER BY cnt DESC"
                ).fetchall()
                total_chunks = sum(r[1] for r in type_rows)
                other_count = sum(r[1] for r in type_rows if r[0] == 'other')
                classification_stats = {
                    'total_chunks': total_chunks,
                    'by_type': {r[0]: r[1] for r in type_rows},
                    'other_ratio': round(other_count / total_chunks * 100, 1) if total_chunks > 0 else 0,
                    'comm_entries': (conn.execute("SELECT COUNT(*) FROM comm_log").fetchone() or [0])[0],
                }
            finally:
                conn.close()
            if classification_stats['other_ratio'] > 30:
                issues.append(f"분류 미달: 섹션의 {classification_stats['other_ratio']}%가 'other' (30% 이상)")
        except Exception:
            pass

    return {
        'db_exists': db_exists, 'db_size': db_size, 'db_projects': db_projects,
        'sync_daemon': sync_daemon,
        'md_dir': config.MD_DIR, 'md_file_count': md_count,
        'snapshot_exists': snap.exists(),
        'classification': classification_stats,
        'issues': issues,
    }


@register_command('template')
def _exec_template(params):
    sub_args = params.get('sub_args', [])
    if not sub_args or sub_args[0] != 'quick' or len(sub_args) < 2:
        return {'error': '사용법: template quick "프로젝트명" "고객사" "담당자"'}

    name = sub_args[1] if len(sub_args) > 1 else '신규'
    client = sub_args[2] if len(sub_args) > 2 else ''
    person = sub_args[3] if len(sub_args) > 3 else ''

    from addons import Template, Ctx
    ctx = Ctx(config.DB_PATH, config.MD_DIR)
    t = Template()
    data = {
        'name': name, 'client': client, 'person': person,
        'status': '초기 검토 단계 🟡', 'capacity': '', 'biz_type': '',
        'external': '', 'situation': '초기 검토 진행 중',
        'date': datetime.now().strftime('%Y-%m-%d'),
    }
    content = t.TEMPLATE.format(**data)
    safe = re.sub(r'[^\w가-힣\s-]', '', name).strip().replace(' ', '_')
    fpath = Path(config.MD_DIR) / f"{safe}.md"

    if fpath.exists():
        return {'error': f'파일 이미 존재: {fpath}', 'path': str(fpath)}

    fpath.parent.mkdir(parents=True, exist_ok=True)
    fpath.write_text(content, encoding='utf-8')

    # 자동 DB 임포트
    importer = SELF_DIR / 'project_db_v2.py'
    if importer.exists():
        import subprocess
        subprocess.run([sys.executable, str(importer), 'import', config.MD_DIR, '--db', config.DB_PATH],
                      capture_output=True, timeout=10)

    return {'created': str(fpath), 'name': name, 'client': client, 'person': person}


@register_command('sync-back')
def _exec_syncback(params):
    from addons import SyncBack, Ctx
    ctx = Ctx(config.DB_PATH, config.MD_DIR, json_out=True)
    sub_args = params.get('sub_args', [])
    dry = '--dry-run' in sub_args
    # SyncBack의 로직을 직접 호출
    sb = SyncBack()
    # 간단하게 dry-run으로만
    return {'message': 'sync-back은 addons.py sync-back [--dry-run]으로 직접 실행하세요.'}
