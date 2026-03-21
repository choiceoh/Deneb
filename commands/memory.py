#!/usr/bin/env python3
"""
Vega Memory Backend for OpenClaw (v1.43)

4개 CLI 명령: memory-search, memory-update, memory-embed, memory-status.
기존 Vega 명령과 다른 출력 규약: bare JSON stdout, stderr 에러, exit code.
"""

import hashlib
import os
import re
import sys
from pathlib import Path
from datetime import datetime

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))
import config
from config import get_db_connection
from core import register_command


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# 1. .md 파서
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

_HEADING_RE = re.compile(r'^(#{1,3})\s+(.+)$', re.MULTILINE)


def _parse_memory_md(filepath):
    """일반 .md 파일 → 청크 리스트 (heading 기준 분할, 라인 번호 포함).

    Returns: [{'heading': str, 'content': str, 'start_line': int, 'end_line': int}]
    """
    text = Path(filepath).read_text(encoding='utf-8-sig').replace('\r\n', '\n')
    lines = text.split('\n')

    # heading 위치 감지
    headings = []  # (line_idx_0based, heading_text)
    for i, line in enumerate(lines):
        m = _HEADING_RE.match(line)
        if m:
            headings.append((i, m.group(2).strip()))

    # trailing empty lines 제거 (end_line 정확도 위해)
    while lines and not lines[-1].strip():
        lines.pop()
    if not lines:
        return []

    if not headings:
        # heading 없으면 전체를 단일 청크
        content = text.strip()
        if content:
            return [{'heading': '', 'content': content,
                     'start_line': 1, 'end_line': len(lines)}]
        return []

    chunks = []
    for idx, (line_idx, heading) in enumerate(headings):
        start = line_idx
        end = headings[idx + 1][0] - 1 if idx + 1 < len(headings) else len(lines) - 1

        # content = heading 줄 포함 ~ 다음 heading 직전
        body = '\n'.join(lines[start:end + 1]).strip()
        if body:
            chunks.append({
                'heading': heading,
                'content': body,
                'start_line': start + 1,   # 1-indexed
                'end_line': end + 1,
            })

    return chunks


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# 2. workspace / 파일 수집
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

def _get_workspace():
    """memory workspace 루트 결정."""
    if config.MEMORY_WORKSPACE:
        return Path(config.MEMORY_WORKSPACE)
    return Path(config.MD_DIR).parent


def _collect_memory_files(workspace):
    """MEMORY_PATHS 설정에 따라 .md 파일 수집.

    Returns: list[Path]
    """
    files = []
    for rel in config.MEMORY_PATHS:
        target = workspace / rel
        if target.is_file() and target.suffix == '.md':
            files.append(target)
        elif target.is_dir():
            files.extend(sorted(
                f for f in target.rglob('*.md')
                if not f.is_symlink()
            ))
    return files


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# 3. memory-update
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

@register_command('memory-update', needs_db=True, read_only=False, category='system')
def _exec_memory_update(params):
    force = '--force' in (params.get('sub_args') or [])
    workspace = _get_workspace()
    md_files = _collect_memory_files(workspace)

    conn = get_db_connection(config.DB_PATH)
    try:
        cur = conn.cursor()

        # 기존 해시 로드
        try:
            existing_hashes = {
                row[0]: row[1]
                for row in cur.execute("SELECT source_file, content_hash FROM file_hashes")
            }
        except Exception:
            existing_hashes = {}

        updated = 0
        skipped = 0
        current_keys = set()

        for fpath in md_files:
            fpath_str = str(fpath.resolve())
            rel_path = str(fpath.relative_to(workspace)).replace('\\', '/')
            # source_file 키는 workspace 상대경로 (memory: 접두사)
            source_key = f"memory:{rel_path}"
            current_keys.add(source_key)

            try:
                raw = fpath.read_text(encoding='utf-8-sig').replace('\r\n', '\n')
                content_hash = hashlib.md5(raw.encode('utf-8')).hexdigest()

                if not force and existing_hashes.get(source_key) == content_hash:
                    skipped += 1
                    continue

                chunks = _parse_memory_md(fpath)

                # upsert project
                old = cur.execute(
                    "SELECT id FROM projects WHERE source_file=?", (source_key,)
                ).fetchone()

                if old:
                    pid = old[0]
                    cur.execute(
                        "UPDATE projects SET name=?, source_file=?, imported_at=?, source_type='memory' WHERE id=?",
                        (rel_path, source_key, datetime.now().isoformat(), pid)
                    )
                    # 기존 chunks 삭제 (cascade: chunk_embeddings 트리거, FTS 트리거)
                    cur.execute(
                        "DELETE FROM chunk_tags WHERE chunk_id IN (SELECT id FROM chunks WHERE project_id=?)",
                        (pid,)
                    )
                    cur.execute("DELETE FROM chunks WHERE project_id=?", (pid,))
                else:
                    cur.execute(
                        "INSERT INTO projects (name, source_file, imported_at, source_type) VALUES (?, ?, ?, 'memory')",
                        (rel_path, source_key, datetime.now().isoformat())
                    )
                    pid = cur.lastrowid

                # 청크 삽입
                for chunk in chunks:
                    cur.execute(
                        "INSERT INTO chunks (project_id, section_heading, content, chunk_type, start_line, end_line) VALUES (?, ?, ?, 'memory', ?, ?)",
                        (pid, chunk['heading'], chunk['content'],
                         chunk['start_line'], chunk['end_line'])
                    )

                # 해시 갱신
                cur.execute("DELETE FROM file_hashes WHERE source_file=?", (source_key,))
                cur.execute(
                    "INSERT INTO file_hashes (source_file, content_hash, updated_at) VALUES (?, ?, ?)",
                    (source_key, content_hash, datetime.now().isoformat())
                )
                updated += 1

            except Exception as e:
                print(f"memory-update: {rel_path}: {e}", file=sys.stderr)
                continue

        # 삭제된 파일 정리
        memory_keys = {
            k for k in existing_hashes if k.startswith('memory:')
        }
        for gone_key in memory_keys - current_keys:
            old = cur.execute(
                "SELECT id FROM projects WHERE source_file=?", (gone_key,)
            ).fetchone()
            if old:
                pid = old[0]
                cur.execute(
                    "DELETE FROM chunk_tags WHERE chunk_id IN (SELECT id FROM chunks WHERE project_id=?)",
                    (pid,)
                )
                cur.execute("DELETE FROM chunks WHERE project_id=?", (pid,))
                cur.execute("DELETE FROM projects WHERE id=?", (pid,))
            cur.execute("DELETE FROM file_hashes WHERE source_file=?", (gone_key,))

        conn.commit()
        return {'updated': updated, 'skipped': skipped, 'total': len(md_files)}
    finally:
        conn.close()


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# 4. memory-embed
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

@register_command('memory-embed', needs_db=True, read_only=False, category='system')
def _exec_memory_embed(params):
    force = '--force' in (params.get('sub_args') or [])

    if force:
        conn = get_db_connection(config.DB_PATH)
        try:
            conn.execute("""
                DELETE FROM chunk_embeddings WHERE chunk_id IN (
                    SELECT c.id FROM chunks c
                    JOIN projects p ON p.id = c.project_id
                    WHERE p.source_type = 'memory'
                )
            """)
            conn.commit()
        finally:
            conn.close()

    from models import embed_all_chunks
    result = embed_all_chunks(db_path=config.DB_PATH)
    return result


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# 5. memory-search
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

def _fts_escape(query):
    """FTS5 쿼리 이스케이프: 예약어·특수문자 안전 처리."""
    reserved = {'AND', 'OR', 'NOT', 'NEAR'}
    tokens = query.split()
    safe = []
    for t in tokens:
        upper = t.upper()
        if upper in reserved or any(c in t for c in '"*(){}[]'):
            safe.append(f'"{t}"')
        else:
            safe.append(t)
    return ' '.join(safe)


@register_command('memory-search', needs_db=True, read_only=True, category='query')
def _exec_memory_search(params):
    query = params.get('query', '').strip()
    if not query:
        return []
    limit = min(int(params.get('limit', 6)), 20)
    collection = params.get('collection')

    workspace = _get_workspace()
    conn = get_db_connection(config.DB_PATH)

    try:
        results_map = {}  # chunk_id → result dict

        # ── FTS5 검색 ──
        fts_query = _fts_escape(query)
        try:
            fts_rows = conn.execute("""
                SELECT c.id, c.content, c.start_line, c.end_line, c.section_heading,
                       p.source_file, rank
                FROM chunks_fts f
                JOIN chunks c ON c.rowid = f.rowid
                JOIN projects p ON p.id = c.project_id
                WHERE chunks_fts MATCH ? AND p.source_type = 'memory'
                ORDER BY rank
                LIMIT ?
            """, (fts_query, limit * 3)).fetchall()
        except Exception:
            fts_rows = []

        for row in fts_rows:
            cid, content, sl, el, heading, sf, rank = row
            results_map[cid] = {
                'chunk_id': cid, 'content': content or '',
                'start_line': sl, 'end_line': el,
                'heading': heading, 'source_file': sf,
                'score': -rank if rank else 0.0,  # FTS rank는 음수 — 변환
            }

        # ── 벡터 검색 ──
        try:
            from models import LocalEmbedder, vector_search
            embedder = LocalEmbedder()
            query_vec = embedder.embed_single(query)
            if query_vec is not None:
                vec_results = vector_search(
                    query_vec, db_path=config.DB_PATH,
                    limit=limit * 2, source_type='memory'
                )
                for row in vec_results:
                    cid = row[0]
                    if cid not in results_map:
                        score = row[1]
                        content = row[4] if len(row) > 4 else ''
                        sl = row[5] if len(row) > 5 else None
                        el = row[6] if len(row) > 6 else None
                        heading = row[7] if len(row) > 7 else ''
                        sf = row[8] if len(row) > 8 else ''
                        results_map[cid] = {
                            'chunk_id': cid, 'content': content,
                            'start_line': sl, 'end_line': el,
                            'heading': heading, 'source_file': sf,
                            'score': score,
                        }
        except Exception:
            pass  # 벡터 검색 불가 시 FTS만으로 진행

        if not results_map:
            return []

        # ── 리랭킹 (가능하면) ──
        items = list(results_map.values())
        try:
            from models import LocalReranker
            reranker = LocalReranker()
            docs = [it['content'][:500] for it in items]
            scores = reranker.rerank(query, docs)
            if scores is not None:
                for it, sc in zip(items, scores):
                    it['score'] = sc
        except Exception as e:
            import logging as _logging
            _logging.getLogger(__name__).debug("memory rerank 실패 (점수 없이 진행): %s", e)

        # 점수 내림차순 정렬
        items.sort(key=lambda x: x['score'], reverse=True)

        # ── collection 필터 ──
        if collection:
            items = [it for it in items if collection in (it['source_file'] or '')]

        # ── 상위 limit개 → MemorySearchResult 변환 ──
        output = []
        for it in items[:limit]:
            sf = it['source_file'] or ''
            # source_key에서 'memory:' 접두사 제거 → workspace 상대경로 (Unix 구분자)
            rel_path = sf.replace('memory:', '', 1) if sf.startswith('memory:') else sf
            rel_path = rel_path.replace('\\', '/')
            output.append({
                'path': rel_path,
                'startLine': it['start_line'],
                'endLine': it['end_line'],
                'score': round(it['score'], 4) if isinstance(it['score'], float) else it['score'],
                'snippet': (it['content'] or '')[:500],
                'source': 'memory',
            })

        return output
    finally:
        conn.close()


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# 6. memory-status
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

@register_command('memory-status', needs_db=True, read_only=True, category='system')
def _exec_memory_status(params):
    conn = get_db_connection(config.DB_PATH)
    try:
        row = conn.execute("""
            SELECT
                COUNT(DISTINCT p.id) as files,
                COUNT(c.id) as chunks,
                COUNT(ce.chunk_id) as embedded
            FROM projects p
            LEFT JOIN chunks c ON c.project_id = p.id
            LEFT JOIN chunk_embeddings ce ON ce.chunk_id = c.id
            WHERE p.source_type = 'memory'
        """).fetchone()

        return {
            'files': row[0],
            'chunks': row[1],
            'embedded': row[2],
            'model': os.path.basename(config.MODEL_EMBEDDER),
            'dbPath': config.DB_PATH,
        }
    finally:
        conn.close()
