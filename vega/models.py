"""
Vega 로컬 AI 모델 관리 (v1.41)

세 개의 GGUF 모델을 llama-cpp-python으로 직접 로드하여
쿼리 확장, 리랭킹, 임베딩을 수행합니다.

- Qwen3.5-9B Q4_K_M — 쿼리 확장 (텍스트 생성)
- Qwen3-Reranker-4B — 리랭킹 (cross-encoder)
- Qwen3-Embedding-8B Q4_K_M — 벡터 임베딩

사용법:
    from models import LocalAdapter
    adapter = LocalAdapter()
    results = adapter.search("비금도 케이블", mode='query')
"""

import os
import time
import logging
import threading
from datetime import datetime

_log = logging.getLogger(__name__)

try:
    import numpy as np
    _HAS_NUMPY = True
except ImportError:
    np = None
    _HAS_NUMPY = False

try:
    from llama_cpp import Llama
    _HAS_LLAMA = True
except ImportError:
    Llama = None
    _HAS_LLAMA = False


# ──────────────────────────────────────────────
# 1. ModelManager — 싱글톤, lazy loading, TTL
# ──────────────────────────────────────────────

class ModelManager:
    """GGUF 모델 lazy loading + TTL 자동 해제."""

    _instance = None
    _lock = threading.Lock()

    def __new__(cls):
        if cls._instance is None:
            with cls._lock:
                if cls._instance is None:
                    cls._instance = super().__new__(cls)
                    cls._instance._initialized = False
        return cls._instance

    def __init__(self):
        with self._lock:
            if self._initialized:
                return
            self._initialized = True
            self._models = {}        # role -> Llama instance
            self._last_used = {}     # role -> timestamp
            self._model_lock = threading.Lock()
            import config
            self._config = config

    @classmethod
    def reset(cls):
        """싱글톤 초기화 (테스트용). 로드된 모델도 전부 해제."""
        with cls._lock:
            if cls._instance is not None:
                cls._instance._models.clear()
                cls._instance._last_used.clear()
                cls._instance._initialized = False
            cls._instance = None

    def get_model(self, role):
        """모델 반환. 없으면 lazy load. 파일 없으면 None.

        Args:
            role: 'expander' | 'embedder' | 'reranker'
        """
        with self._model_lock:
            # 이미 로드된 모델은 즉시 반환 (외부 주입 mock 포함)
            if role in self._models:
                self._last_used[role] = time.time()
                return self._models[role]

        if not _HAS_LLAMA:
            _log.warning("llama-cpp-python 미설치 — pip install llama-cpp-python")
            return None

        with self._model_lock:
            # double-check: 다른 스레드가 이미 로드했을 수 있음
            if role in self._models:
                self._last_used[role] = time.time()
                return self._models[role]

            path = self._get_path(role)
            if not path or not os.path.isfile(path):
                _log.warning("모델 파일 없음: %s (%s)", role, path)
                return None

            try:
                _log.info("모델 로딩: %s ← %s", role, path)
                if role == 'embedder':
                    model = Llama(
                        model_path=path,
                        n_ctx=512,
                        n_batch=512,
                        embedding=True,
                        verbose=False,
                    )
                elif role == 'reranker':
                    model = Llama(
                        model_path=path,
                        n_ctx=512,
                        n_batch=512,
                        verbose=False,
                    )
                else:  # expander
                    model = Llama(
                        model_path=path,
                        n_ctx=2048,
                        n_batch=512,
                        verbose=False,
                    )
                self._models[role] = model
                self._last_used[role] = time.time()
                _log.info("모델 로드 완료: %s", role)
                return model
            except Exception as e:
                _log.error("모델 로드 실패: %s — %s", role, e)
                return None

    def _get_path(self, role):
        """role에 해당하는 모델 파일 경로."""
        if role == 'expander':
            return self._config.MODEL_EXPANDER
        elif role == 'embedder':
            return self._config.MODEL_EMBEDDER
        elif role == 'reranker':
            return self._config.MODEL_RERANKER
        return None

    def unload(self, role=None):
        """모델 해제. role=None이면 전부 해제."""
        with self._model_lock:
            if role:
                self._models.pop(role, None)
                self._last_used.pop(role, None)
                _log.info("모델 해제: %s", role)
            else:
                self._models.clear()
                self._last_used.clear()
                _log.info("전체 모델 해제")

    def unload_expired(self):
        """TTL 경과 모델 해제."""
        ttl = self._config.MODEL_UNLOAD_TTL
        now = time.time()
        with self._model_lock:
            expired = [r for r, ts in self._last_used.items() if now - ts > ttl]
            for role in expired:
                self._models.pop(role, None)
                self._last_used.pop(role, None)
                _log.info("TTL 만료 해제: %s", role)

    def status(self):
        """각 모델 상태 반환."""
        result = {}
        for role in ('expander', 'embedder', 'reranker'):
            path = self._get_path(role)
            loaded = role in self._models
            last = self._last_used.get(role)
            # TOCTOU 방지: getsize가 isfile 사이에 삭제되면 OSError
            file_size_mb = 0
            file_exists = False
            if path:
                try:
                    size = os.path.getsize(path)
                    file_exists = True
                    file_size_mb = round(size / 1024 / 1024, 1)
                except OSError:
                    pass
            result[role] = {
                'path': path,
                'file_exists': file_exists,
                'file_size_mb': file_size_mb,
                'loaded': loaded,
                'last_used': datetime.fromtimestamp(last).isoformat() if last else None,
            }
        result['llama_cpp_available'] = _HAS_LLAMA
        result['numpy_available'] = _HAS_NUMPY
        return result


# ──────────────────────────────────────────────
# 2. LocalEmbedder
# ──────────────────────────────────────────────

def _call_embed(model, text):
    """llama-cpp-python 버전별 임베딩 API 호환 레이어.

    - embed(text) → list[float] 또는 list[list[float]]  (0.3.x+)
    - create_embedding(text) → {'data': [{'embedding': [...]}]}  (0.2.x)
    어느 API든 1D float 리스트를 반환.
    """
    # 1) embed() 시도 (최신 API)
    if hasattr(model, 'embed'):
        result = model.embed(text)
        if isinstance(result, list) and len(result) > 0:
            if isinstance(result[0], list):
                if len(result[0]) > 0:
                    return result[0]    # [[0.1, 0.2, ...]] → [0.1, 0.2, ...]
                # [[]] — 빈 내부 리스트
                raise RuntimeError("embed()가 빈 벡터를 반환")
            if isinstance(result[0], (int, float)):
                return result       # [0.1, 0.2, ...] → 그대로
        # 빈 리스트 또는 이상한 타입
        raise RuntimeError(f"embed() 비정상 반환: 길이={len(result) if isinstance(result, list) else type(result)}")

    # 2) create_embedding() 폴백 (구 API)
    if hasattr(model, 'create_embedding'):
        resp = model.create_embedding(text)
        data = resp.get('data', [])
        if data and 'embedding' in data[0]:
            emb = data[0]['embedding']
            if emb and len(emb) > 0:
                return emb
            raise RuntimeError("create_embedding()이 빈 벡터를 반환")

    raise RuntimeError("llama-cpp-python에 embed()/create_embedding() API 없음")


class LocalEmbedder:
    """텍스트 → 벡터 임베딩 (Qwen3-Embedding-8B)."""

    def __init__(self, manager=None):
        self.mgr = manager or ModelManager()

    def embed(self, texts):
        """텍스트 리스트 → numpy 배열 (N x dim).

        실패 시 None 반환.
        """
        if not _HAS_NUMPY:
            _log.warning("numpy 미설치")
            return None
        if not texts:
            return None

        model = self.mgr.get_model('embedder')
        if model is None:
            return None

        try:
            embeddings = []
            for text in texts:
                vec = _call_embed(model, text)
                embeddings.append(vec)
            arr = np.array(embeddings, dtype=np.float32)
            if arr.ndim == 1:
                arr = arr.reshape(1, -1)
            # 0차원 또는 빈 배열 방어
            if arr.size == 0:
                return None
            # L2 정규화 (코사인 유사도 → dot product)
            norms = np.linalg.norm(arr, axis=1, keepdims=True)
            norms = np.where(norms == 0, 1, norms)
            arr = arr / norms
            return arr
        except Exception as e:
            _log.error("임베딩 실패: %s", e)
            return None

    def embed_single(self, text):
        """단일 텍스트 → 1D numpy 배열."""
        if not text:
            return None
        result = self.embed([text])
        if result is not None and result.size > 0 and len(result) > 0:
            vec = result[0]
            if vec.size > 0:
                return vec
        return None


# ──────────────────────────────────────────────
# 3. LocalReranker
# ──────────────────────────────────────────────

class LocalReranker:
    """cross-encoder 리랭킹 (Qwen3-Reranker-4B).

    Qwen3-Reranker 방식:
    1. 프롬프트: query + document → "yes"/"no" 판별
    2. "yes" 토큰의 logprob → sigmoid → 관련성 점수 (0~1)
    """

    # Qwen3-Reranker 공식 프롬프트 템플릿
    _PROMPT_TEMPLATE = (
        '<|im_start|>system\nJudge whether the document is relevant to the search query. '
        'Answer only "yes" or "no".<|im_end|>\n'
        '<|im_start|>user\n<query>{query}</query>\n<document>{document}</document><|im_end|>\n'
        '<|im_start|>assistant\n'
    )

    def __init__(self, manager=None):
        self.mgr = manager or ModelManager()

    def rerank(self, query, docs):
        """query에 대한 각 doc의 관련성 점수 리스트 반환 (0~1).

        Returns:
            list[float] — 정상 (빈 docs면 빈 리스트)
            None — 모델 로드 실패
        """
        if not docs:
            return []
        if not query:
            return [0.0] * len(docs)

        model = self.mgr.get_model('reranker')
        if model is None:
            return None

        import math
        scores = []
        for doc in docs:
            try:
                prompt = self._PROMPT_TEMPLATE.format(
                    query=str(query)[:1000],
                    document=str(doc)[:1500]  # 문서 길이 제한
                )
                output = model(
                    prompt,
                    max_tokens=1,
                    logprobs=1,  # top_logprobs 1개 요청 (True가 아닌 int)
                    temperature=0.0,
                )
                logprob = self._extract_yes_logprob(output)
                score = 1.0 / (1.0 + math.exp(-logprob))  # sigmoid
                scores.append(score)
            except Exception as e:
                _log.warning("리랭킹 실패 (단건): %s", e)
                scores.append(0.0)

        return scores

    @staticmethod
    def _extract_yes_logprob(output):
        """llama-cpp-python 출력에서 "yes" 토큰의 logprob 추출.

        llama-cpp-python의 completion 응답 형식:
        {
          'choices': [{
            'text': 'yes',
            'logprobs': {
              'tokens': ['yes'],
              'token_logprobs': [-0.5],
              'top_logprobs': [{'yes': -0.5, 'no': -1.2}],
              'text_offset': [0]
            }
          }]
        }
        """
        try:
            choices = output.get('choices', [])
            if not choices:
                return 0.0

            logprobs_data = choices[0].get('logprobs')
            if logprobs_data:
                # top_logprobs에서 "yes" 토큰 logprob 탐색
                top_list = logprobs_data.get('top_logprobs') or []
                if top_list:
                    top = top_list[0]  # 첫 번째 토큰의 top logprobs
                    if isinstance(top, dict):
                        for token, lp in top.items():
                            if token.strip().lower() == 'yes' and isinstance(lp, (int, float)):
                                return float(lp)

                # top_logprobs에 "yes" 없으면 token_logprobs 사용
                tokens = logprobs_data.get('tokens', [])
                token_logprobs = logprobs_data.get('token_logprobs', [])
                if tokens and token_logprobs and token_logprobs[0] is not None:
                    lp = token_logprobs[0]
                    if not isinstance(lp, (int, float)):
                        return 0.0
                    if tokens[0].strip().lower() == 'yes':
                        return float(lp)
                    # 생성된 토큰이 "no"면 logprob을 반전
                    if tokens[0].strip().lower() == 'no':
                        return -abs(float(lp)) - 1.0

            # logprobs 구조가 없으면 생성된 텍스트로 판별
            text = choices[0].get('text', '').strip().lower()
            if text.startswith('yes'):
                return 2.0
            return -2.0
        except Exception:
            return 0.0


# ──────────────────────────────────────────────
# 4. LocalExpander
# ──────────────────────────────────────────────

class LocalExpander:
    """쿼리 확장 (Qwen3.5-9B) — 동의어/관련 키워드 생성."""

    _PROMPT_TEMPLATE = (
        '<|im_start|>system\n'
        '당신은 검색 쿼리 확장 전문가입니다. '
        '주어진 검색어에 대해 관련 동의어, 유의어, 관련 키워드를 쉼표로 구분하여 나열하세요. '
        '한국어와 영어를 모두 포함하세요. 설명 없이 키워드만 출력하세요.'
        '<|im_end|>\n'
        '<|im_start|>user\n검색어: {query}<|im_end|>\n'
        '<|im_start|>assistant\n'
    )

    def __init__(self, manager=None):
        self.mgr = manager or ModelManager()

    def expand(self, query):
        """검색어 확장 키워드 리스트 반환.

        실패 시 빈 리스트 반환.
        """
        if not query or not str(query).strip():
            return []

        model = self.mgr.get_model('expander')
        if model is None:
            return []

        try:
            # 쿼리 길이 제한 (프롬프트 오버플로우 방지)
            safe_query = str(query).strip()[:500]
            prompt = self._PROMPT_TEMPLATE.format(query=safe_query)
            output = model(
                prompt,
                max_tokens=64,
                temperature=0.3,
                stop=['<|im_end|>', '\n\n'],
            )
            text = output['choices'][0]['text'].strip()
            return self._parse_keywords(text, safe_query)
        except Exception as e:
            _log.warning("쿼리 확장 실패: %s", e)
            return []

    @staticmethod
    def _parse_keywords(text, original_query):
        """LLM 출력에서 키워드 파싱. 중복 제거, 원본 쿼리 제외."""
        keywords = []
        seen = set()
        original_lower = original_query.lower().strip()
        for part in text.replace('\n', ',').replace(';', ',').replace('/', ',').split(','):
            kw = part.strip().strip('-').strip('•').strip('*').strip('"').strip("'").strip()
            kw_lower = kw.lower()
            if (kw
                    and len(kw) >= 2
                    and kw_lower != original_lower
                    and kw_lower not in seen
                    and not kw_lower.startswith('검색어')
                    and not kw_lower.startswith('keyword')):
                keywords.append(kw)
                seen.add(kw_lower)
        return keywords[:10]  # 최대 10개


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
        db_path: DB 경로 (None이면 config.DB_PATH)
        limit: 반환 수
        source_type: 'memory'|'project'|None (None=전체)

    Returns:
        [(chunk_id, score, project_id, project_name, content,
          start_line, end_line, section_heading, source_file), ...]
    """
    if not _HAS_NUMPY:
        return []

    import config
    from config import get_db_connection
    db = db_path or config.DB_PATH

    conn = None
    try:
        conn = get_db_connection(db)
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
# 6. LocalAdapter — QMDAdapter drop-in replacement
# ──────────────────────────────────────────────

class LocalAdapter:
    """로컬 모델 기반 검색 어댑터. QMDAdapter와 동일 인터페이스.

    search() 반환 형식은 QMDAdapter._parse_output()과 동일:
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
        import config
        if not _HAS_LLAMA or not _HAS_NUMPY:
            return False
        return os.path.isfile(config.MODEL_EMBEDDER)

    def search(self, query, project_filter=None, mode='query', intent=None):
        """QMDAdapter.search()와 동일 인터페이스.

        Args:
            query: 검색 질의
            project_filter: 프로젝트명 리스트
            mode: 'query' (확장+벡터+리랭크) | 'search' (BM25 보조) | 'vsearch' (벡터만)
            intent: 무시 (QMD 호환용)

        Returns:
            list — 결과 리스트 (빈 리스트 = 결과 없음)
            None — 어댑터 사용 불가 (모델 미설치 등)
        """
        if not self.available:
            return None

        import config

        if mode == 'search':
            # BM25 보조 모드: 확장 키워드 생성 후 벡터 검색도 실행
            expanded = self.expander.expand(query)
            search_query = query
            if expanded:
                search_query = query + ' ' + ' '.join(expanded[:3])
            query_vec = self.embedder.embed_single(search_query)
            if query_vec is None:
                return []  # 임베딩 실패 → 빈 결과 (None이 아닌 [])
            results = vector_search(query_vec, db_path=config.DB_PATH, limit=10)
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

        results = vector_search(query_vec, db_path=config.DB_PATH, limit=20)
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
        """vector_search 결과 → QMDAdapter 호환 아이템 리스트.

        router.py _qmd_items_to_unified()가 기대하는 metadata 필드:
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

    import config
    from config import get_db_connection
    db = db_path or config.DB_PATH

    embedder = LocalEmbedder()

    conn = get_db_connection(db)
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
                        (chunk_id, blob, os.path.basename(config.MODEL_EMBEDDER), datetime.now().isoformat())
                    )
                    embedded += 1
                except Exception as e:
                    _log.warning("임베딩 저장 실패 (chunk %d): %s", chunk_id, e)
                    errors += 1

            conn.commit()

        return {'embedded': embedded, 'skipped': skipped, 'errors': errors, 'total': total}
    finally:
        conn.close()
