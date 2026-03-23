"""ml package -- local AI model management for Vega."""

from .manager import ModelManager, _HAS_NUMPY, _HAS_LLAMA
from .embedder import LocalEmbedder, _call_embed
from .reranker import LocalReranker
from .expander import LocalExpander
from .search import (
    _blob_to_vector,
    _vector_to_blob,
    vector_search,
    LocalAdapter,
    embed_all_chunks,
)

__all__ = [
    "ModelManager",
    "_HAS_NUMPY",
    "_HAS_LLAMA",
    "LocalEmbedder",
    "_call_embed",
    "LocalReranker",
    "LocalExpander",
    "_blob_to_vector",
    "_vector_to_blob",
    "vector_search",
    "LocalAdapter",
    "embed_all_chunks",
]
