"""Backward-compatibility wrapper — actual code lives in ml/ package."""
from ml import *
from ml.manager import ModelManager
from ml.embedder import LocalEmbedder, _call_embed
from ml.reranker import LocalReranker
from ml.expander import LocalExpander
from ml.search import (vector_search, LocalAdapter, embed_all_chunks,
                        _blob_to_vector, _vector_to_blob)
