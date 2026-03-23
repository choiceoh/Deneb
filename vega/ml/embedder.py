"""LocalEmbedder -- text to vector embeddings via Qwen3-Embedding-8B."""

import logging

from .manager import ModelManager, _HAS_NUMPY

_log = logging.getLogger(__name__)

try:
    import numpy as np
except ImportError:
    np = None


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
