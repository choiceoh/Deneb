"""ModelManager -- singleton lazy-loading GGUF model manager with TTL."""

import os
import time
import logging
import threading
from datetime import datetime
import config as _cfg

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
            self._config = _cfg

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
