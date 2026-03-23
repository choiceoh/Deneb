"""Vector search, LocalAdapter, and batch embedding utilities."""

import os
import logging
from datetime import datetime
import config as _cfg
from config import get_db_connection as _get_db_connection

from .manager import ModelManager, _HAS_NUMPY, _HAS_LLAMA
from .embedder import LocalEmbedder, _call_embed
from .reranker import LocalReranker
from .expander import LocalExpander

_log = logging.getLogger(__name__)

try:
    import numpy as np
except ImportError:
    np = None


# ──────────────────────────────────────────────
# 5. vector_search — 코사인 유사도 검색
# ──────────────────────────────────────────────

def _blob_to_vector(blob):
    """BLOB → numpy float32 배열. numpy 네이티브 디코딩 (빠름).

    빈/손상 BLOB이면 빈 배열 반환.
    """
    if not blob or len(blob) < 4:
        return np.array([], dtype=np.float32)
    # float32 정렬: 4바이트 배수가 아닌 데이터는 잘라냄
    usable = len(blob) - (len(blob) % 4)
    return np.frombuffer(blob[:usable], dtype=np.float32).copy()


def _vector_to_blob(vec):
    """numpy float32 배열 → BLOB. numpy 네이티브 인코딩 (빠름)."""
    return np.asarray(vec, dtype=np.float32).tobytes()


def vector_search(query_vec, db_path=None, limit=20, source_type=None):
    """chunk_embeddings에서 코사인 유사도 상위 K 반환.

    Args:
        query_vec: 1D numpy 배열
        db_path: DB 경로 (None이면 _cfg.DB_PATH)
        limit: 반환 수
        source_type: 'memory'|'project'|None (None=전체)

    Returns:
        [(chunk_id, score, project_id, project_name, content,
          start_line, end_line, section_heading, source_file), ...]
    """
    if not _HAS_NUMPY:
        return []

    db = db_path or _cfg.DB_PATH

    conn = None
    try:
        conn = _get_db_connection(db)
        sql = """
            SELECT ce.chunk_id, ce.embedding, c.project_id, p.name, c.content,
                   c.start_line, c.end_line, c.section_heading, p.source_file
            FROM chunk_embeddings ce
            JOIN chunks c ON c.id = ce.chunk_id
            JOIN projects p ON p.id = c.project_id
        """
        params = []
        if source_type:
            sql += " WHERE p.source_type = ?"
            params.append(source_type)
        rows = conn.execute(sql, params).fetchall()
    except Exception as e:
        _log.warning("chunk_embeddings 조회 실패: %s", e)
        return []
    finally:
        if conn:
            conn.close()

    if not rows:
        return []

    # query_vec 검증
    try:
        query_vec = np.asarray(query_vec, dtype=np.float32).ravel()
    except (ValueError, TypeError):
        _log.warning("query_vec 변환 실패")
        return []
    if query_vec.size == 0:
        return []
    # NaN/Inf 체크
    if not np.all(np.isfinite(query_vec)):
        _log.warning("query_vec에 NaN/Inf 포함")
        return []

    # 벡터 파싱
    chunk_ids = []
    project_ids = []
    project_names = []
    contents = []
    start_lines = []
    end_lines = []
    section_headings = []
    source_files = []
    vectors = []
    expected_dim = None

    for row in rows:
        try:
            vec = _blob_to_vector(row[1])
            if vec.size == 0:
                continue
            if expected_dim is None:
                expected_dim = len(vec)
            elif len(vec) != expected_dim:
                _log.debug("차원 불일치 스킵: chunk %s (got %d, expected %d)",
                           row[0], len(vec), expected_dim)
                continue
            vectors.append(vec)
            chunk_ids.append(row[0])
            project_ids.append(row[2])
            project_names.append(row[3])
            contents.append(row[4])
            start_lines.append(row[5])
            end_lines.append(row[6])
            section_headings.append(row[7])
            source_files.append(row[8])
        except Exception as e:
            _log.debug("벡터 파싱 실패: chunk %s — %s", row[0], e)
            continue

    if not vectors or expected_dim is None:
        return []

    # query_vec 차원 검증
    if query_vec.shape[0] != expected_dim:
        _log.warning("query_vec 차원(%d) != DB 임베딩 차원(%d)", query_vec.shape[0], expected_dim)
        return []

    mat = np.array(vectors, dtype=np.float32)
    # 정규화
    norms = np.linalg.norm(mat, axis=1, keepdims=True)
    norms = np.where(norms == 0, 1, norms)
    mat = mat / norms

    # query_vec 정규화
    qnorm = np.linalg.norm(query_vec)
    if qnorm > 0:
        query_vec = query_vec / qnorm

    # 코사인 유사도
    scores = mat @ query_vec

    # top-K
    top_idx = np.argsort(scores)[::-1][:limit]
    results = []
    for idx in top_idx:
        score = float(scores[idx])
        if score < 0.01:
            break
        results.append((
            chunk_ids[idx],
            score,
            project_ids[idx],
            project_names[idx],
            (contents[idx] or '')[:500],
            start_lines[idx],
            end_lines[idx],
            section_headings[idx],
            source_files[idx],
        ))

    return results


# ──────────────────────────────────────────────
# 6. LocalAdapter — 로컬 모델 기반 의미 검색 어댑터
# ──────────────────────────────────────────────

class LocalAdapter:
    """로컬 모델 기반 의미 검색 어댑터.

    search() 반환 형식:
    [{'source': ..., 'content': ..., 'score': ..., 'metadata': {...}}, ...]
    """

    def __init__(self):
        self.mgr = ModelManager()
        self.embedder = LocalEmbedder(self.mgr)
        self.reranker = LocalReranker(self.mgr)
        self.expander = LocalExpander(self.mgr)
        self.available = self._check_availability()

    def _check_availability(self):
        """최소한 embedder 모델 파일이 있으면 사용 가능."""
        if not _HAS_LLAMA or not _HAS_NUMPY:
            return False
        return os.path.isfile(_cfg.MODEL_EMBEDDER)

    def search(self, query, project_filter=None, mode='query', intent=None):
        """의미 검색 실행.

        Args:
            query: 검색 질의
            project_filter: 프로젝트명 리스트
            mode: 'query' (확장+벡터+리랭크) | 'search' (BM25 보조) | 'vsearch' (벡터만)
            intent: 검색 의도 힌트 (선택)

        Returns:
            list — 결과 리스트 (빈 리스트 = 결과 없음)
            None — 어댑터 사용 불가 (모델 미설치 등)
        """
        if not self.available:
            return None

        if mode == 'search':
            # BM25 보조 모드: 확장 키워드 생성 후 벡터 검색도 실행
            expanded = self.expander.expand(query)
            search_query = query
            if expanded:
                search_query = query + ' ' + ' '.join(expanded[:3])
            query_vec = self.embedder.embed_single(search_query)
            if query_vec is None:
                return []  # 임베딩 실패 → 빈 결과 (None이 아닌 [])
            results = vector_search(query_vec, db_path=_cfg.DB_PATH, limit=10)
            return self._results_to_items(results)

        # vsearch 또는 query: 벡터 검색
        query_text = query
        if mode == 'query':
            expanded = self.expander.expand(query)
            if expanded:
                query_text = query + ' ' + ' '.join(expanded[:3])

        query_vec = self.embedder.embed_single(query_text)
        if query_vec is None:
            return []  # 임베딩 실패 → 빈 결과

        results = vector_search(query_vec, db_path=_cfg.DB_PATH, limit=20)
        items = self._results_to_items(results)

        # query 모드: 리랭킹 추가
        if mode == 'query' and items:
            docs = [item['content'] for item in items]
            scores = self.reranker.rerank(query, docs)
            if scores:  # None이면 리랭킹 스킵, []이면 docs 없었음
                for item, rerank_score in zip(items, scores):
                    vec_score = item.get('score', 0.0)
                    if not isinstance(vec_score, (int, float)):
                        vec_score = 0.0
                    item['score'] = 0.4 * vec_score + 0.6 * rerank_score
                    item['metadata']['vec_score'] = vec_score
                    item['metadata']['rerank_score'] = rerank_score
                items.sort(key=lambda x: x.get('score', 0), reverse=True)

        # 프로젝트 필터 적용
        if project_filter and items:
            filter_lower = [pf.lower() for pf in project_filter if pf]
            if filter_lower:
                filtered = [
                    item for item in items
                    if any(pf in (item.get('metadata', {}).get('project_name', '') or '').lower()
                           for pf in filter_lower)
                ]
                if filtered:
                    items = filtered

        return items

    @staticmethod
    def _results_to_items(results):
        """vector_search 결과 → 의미 검색 아이템 리스트.

        router.py _semantic_items_to_unified()가 기대하는 metadata 필드:
        uri, filepath, context, docid, best_chunk_pos, filter_bypassed, project_name, title
        """
        items = []
        for row in results:
            chunk_id, score, project_id, project_name, content = row[:5]
            source_file = row[8] if len(row) > 8 else ''
            items.append({
                'source': 'local-vec',
                'content': content,
                'score': float(score),
                'metadata': {
                    'chunk_id': chunk_id,
                    'project_id': project_id,
                    'project_name': project_name or '',
                    'filepath': source_file or '',
                    'uri': f'chunk:{chunk_id}',
                    'docid': f'chunk:{chunk_id}',
                    'title': project_name or '',
                    'context': '',
                    'best_chunk_pos': None,
                    'filter_bypassed': False,
                }
            })
        return items

    def search_fast(self, query, project_filter=None):
        return self.search(query, project_filter, mode='search')

    def search_semantic(self, query, project_filter=None):
        return self.search(query, project_filter, mode='vsearch')


# ──────────────────────────────────────────────
# 7. 유틸리티: 임베딩 일괄 생성
# ──────────────────────────────────────────────

def embed_all_chunks(db_path=None, batch_size=32):
    """전체 chunks에 대해 임베딩 생성 → chunk_embeddings 저장.

    Returns:
        {'embedded': int, 'skipped': int, 'errors': int, 'total': int}
    """
    if batch_size is None or batch_size < 1:
        batch_size = 32

    db = db_path or _cfg.DB_PATH

    embedder = LocalEmbedder()

    conn = _get_db_connection(db)
    try:
        rows = conn.execute("""
            SELECT c.id, c.content
            FROM chunks c
            LEFT JOIN chunk_embeddings ce ON ce.chunk_id = c.id
            WHERE ce.chunk_id IS NULL AND c.content IS NOT NULL AND LENGTH(c.content) > 10
            ORDER BY c.id
        """).fetchall()

        total = len(rows)
        embedded = 0
        skipped = 0
        errors = 0

        for i in range(0, total, batch_size):
            batch = rows[i:i + batch_size]
            texts = [row[1][:2000] for row in batch]
            ids = [row[0] for row in batch]

            vecs = embedder.embed(texts)
            if vecs is None:
                errors += len(batch)
                continue

            for chunk_id, vec in zip(ids, vecs):
                try:
                    blob = _vector_to_blob(vec)
                    conn.execute(
                        "INSERT OR REPLACE INTO chunk_embeddings (chunk_id, embedding, model_name, updated_at) VALUES (?, ?, ?, ?)",
                        (chunk_id, blob, os.path.basename(_cfg.MODEL_EMBEDDER), datetime.now().isoformat())
                    )
                    embedded += 1
                except Exception as e:
                    _log.warning("임베딩 저장 실패 (chunk %d): %s", chunk_id, e)
                    errors += 1

            conn.commit()

        return {'embedded': embedded, 'skipped': skipped, 'errors': errors, 'total': total}
    finally:
        conn.close()
