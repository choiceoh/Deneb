#!/usr/bin/env python3
"""
Vega upgrade 명령 — 버전 업그레이드 후 단일 명령으로 전체 정비.

  python vega.py upgrade [--force]

수행 작업:
  1. DB 스키마 마이그레이션 (SCHEMA_VERSION 체크)
  2. .md 파일 증분 재파싱 (변경분만, --force면 전체)
  3. memory 파일 증분 업데이트
  4. 미임베딩 청크 벡터 임베딩
  5. FTS 인덱스 정합성 확인
"""

import os
import sys
from pathlib import Path
from datetime import datetime

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))
import config
from config import get_db_connection
from core import register_command


def _upgrade_summary(d):
    parts = []
    if d.get('schema_migrated'):
        parts.append(f"스키마 v{d['schema_version']}")
    sync = d.get('sync', {})
    if sync.get('updated', 0):
        parts.append(f"프로젝트 {sync['updated']}개 갱신")
    mem = d.get('memory', {})
    if mem.get('updated', 0):
        parts.append(f"메모리 {mem['updated']}개 갱신")
    emb = d.get('embed', {})
    if emb.get('embedded', 0):
        parts.append(f"임베딩 {emb['embedded']}개 생성")
    if not parts:
        parts.append("변경 없음 — 최신 상태")
    return ' | '.join(parts)


@register_command('upgrade', needs_db=False, category='system',
                   summary_fn=_upgrade_summary)
def _exec_upgrade(params):
    force = '--force' in (params.get('sub_args') or [])
    result = {'started_at': datetime.now().isoformat()}
    steps_done = []

    import project_db_v2

    # ── 1. 스키마 마이그레이션 ──
    old_ver = 0
    if os.path.exists(config.DB_PATH):
        conn = get_db_connection(config.DB_PATH)
        try:
            old_ver = conn.execute("PRAGMA user_version").fetchone()[0]
        finally:
            conn.close()

    # init_db가 CREATE TABLE IF NOT EXISTS + 마이그레이션 처리
    conn = project_db_v2.init_db(config.DB_PATH)
    new_ver = conn.execute("PRAGMA user_version").fetchone()[0]
    conn.close()

    result['schema_version'] = new_ver
    result['schema_migrated'] = old_ver < new_ver
    if result['schema_migrated']:
        steps_done.append(f"스키마 v{old_ver} → v{new_ver}")

    # ── 2. .md 증분 재파싱 ──
    md_dir = config.MD_DIR
    sync_result = _sync_projects(md_dir, force)
    result['sync'] = sync_result
    if sync_result.get('updated', 0):
        steps_done.append(f"프로젝트 {sync_result['updated']}개 갱신")

    # ── 3. memory 파일 업데이트 ──
    mem_result = {'updated': 0, 'skipped': 0, 'total': 0, 'error': None}
    try:
        from commands.memory import _exec_memory_update
        mem_params = {'sub_args': ['--force'] if force else []}
        mr = _exec_memory_update(mem_params)
        if isinstance(mr, dict):
            mem_result.update(mr)
        if mem_result.get('updated'):
            steps_done.append(f"메모리 {mem_result['updated']}개 갱신")
    except Exception as e:
        mem_result['error'] = str(e)
    result['memory'] = mem_result

    # ── 4. 벡터 임베딩 ──
    embed_result = {'embedded': 0, 'skipped': 0, 'errors': 0, 'available': False}
    try:
        from models import embed_all_chunks
        er = embed_all_chunks(db_path=config.DB_PATH)
        if isinstance(er, dict):
            embed_result.update(er)
        embed_result['available'] = True
        if embed_result.get('embedded'):
            steps_done.append(f"임베딩 {embed_result['embedded']}개 생성")
    except Exception as e:
        embed_result['error'] = str(e)
    result['embed'] = embed_result

    # ── 5. FTS 정합성 (변경 있을 때) ──
    total_changes = sync_result.get('updated', 0) + mem_result.get('updated', 0)
    if total_changes > 0 or force:
        try:
            conn = get_db_connection(config.DB_PATH)
            try:
                project_db_v2.rebuild_fts(conn)
                conn.commit()
            finally:
                conn.close()
            result['fts_rebuilt'] = True
        except Exception:
            result['fts_rebuilt'] = False

    # ── 6. 모델 감지 상태 ──
    result['models'] = {
        'dir': config.MODELS_DIR,
        'embedder': config.MODEL_EMBEDDER if os.path.isfile(config.MODEL_EMBEDDER) else None,
        'reranker': config.MODEL_RERANKER if os.path.isfile(config.MODEL_RERANKER) else None,
        'expander': config.MODEL_EXPANDER if os.path.isfile(config.MODEL_EXPANDER) else None,
    }

    # ── 요약 ──
    result['steps'] = steps_done if steps_done else ['변경 없음 — 최신 상태']
    result['finished_at'] = datetime.now().isoformat()
    return result


def _sync_projects(md_dir, force):
    """프로젝트 .md 파일 증분 동기화 (단일 커넥션, WAL 안전)."""
    import project_db_v2

    sync = {'updated': 0, 'skipped': 0, 'error': None}
    if not os.path.isdir(md_dir):
        sync['error'] = f'MD_DIR 없음: {md_dir}'
        return sync

    md_files = sorted(
        f for f in Path(md_dir).rglob("*.md")
        if not f.is_symlink()
    )
    if not md_files:
        return sync

    conn = get_db_connection(config.DB_PATH)
    try:
        cur = conn.cursor()

        # 스키마 보장 (init_db는 _exec_upgrade에서 이미 호출됨 — 여기선 생략)

        # 기존 해시 로드
        try:
            existing_hashes = {
                row[0]: row[1]
                for row in cur.execute("SELECT source_file, content_hash FROM file_hashes")
            }
        except Exception:
            existing_hashes = {}

        if force:
            # force: 프로젝트 해시만 삭제 (memory: 제외)
            cur.execute("DELETE FROM file_hashes WHERE source_file NOT LIKE 'memory:%'")
            existing_hashes = {k: v for k, v in existing_hashes.items() if k.startswith('memory:')}

        current_files = set()

        for fpath in md_files:
            current_files.add(str(fpath.resolve()))
            current_files.add(fpath.name)
            try:
                result = project_db_v2.upsert_md_file(cur, fpath, existing_hashes)
                if result == 'skipped':
                    sync['skipped'] += 1
                elif result == 'updated':
                    sync['updated'] += 1
            except Exception as e:
                sync.setdefault('errors', [])
                sync['errors'].append(f"{fpath.name}: {e}")

        # 삭제된 파일 처리
        for key in set(existing_hashes.keys()) - current_files:
            if key.startswith('memory:'):
                continue
            project_db_v2.delete_project_by_source(cur, key)

        conn.commit()
    finally:
        conn.close()

    return sync
