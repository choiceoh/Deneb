"""Unit and concurrency-boundary tests for the BGE-M3 embedding service."""

from __future__ import annotations

import asyncio
import io
import logging
import queue
import sys
import types
import unittest
from unittest import mock


class FakeHTTPException(Exception):
    def __init__(self, status_code, detail) -> None:
        super().__init__(detail)
        self.status_code = status_code
        self.detail = detail


class FakeFastAPI:
    def __init__(self, **kwargs) -> None:
        self.kwargs = kwargs
        self.routes = []

    def get(self, path, **kwargs):
        return self._route("GET", path, kwargs)

    def post(self, path, **kwargs):
        return self._route("POST", path, kwargs)

    def _route(self, method, path, kwargs):
        def decorate(function):
            self.routes.append((method, path, kwargs, function))
            return function

        return decorate


class FakeBaseModel:
    def __init__(self, **values) -> None:
        for key, value in values.items():
            setattr(self, key, value)


fake_uvicorn = types.ModuleType("uvicorn")
fake_uvicorn.run = mock.Mock()
fake_fastapi = types.ModuleType("fastapi")
fake_fastapi.FastAPI = FakeFastAPI
fake_fastapi.HTTPException = FakeHTTPException
fake_pydantic = types.ModuleType("pydantic")
fake_pydantic.BaseModel = FakeBaseModel
fake_pydantic.Field = lambda default, **_kwargs: default
fake_numpy = types.ModuleType("numpy")

with mock.patch.dict(sys.modules, {
    "uvicorn": fake_uvicorn,
    "fastapi": fake_fastapi,
    "pydantic": fake_pydantic,
    "numpy": fake_numpy,
}):
    from test_support import load_script

    bge = load_script("scripts/deploy/bge-m3-server.py")


class ModelPoolTests(unittest.TestCase):
    def setUp(self) -> None:
        bge._pool = queue.Queue()
        bge._pool_size = 0
        bge._executor = None
        bge._llama_log_cb = None

    def test_missing_llama_log_symbols_is_a_quiet_noop(self) -> None:
        module = types.ModuleType("llama_cpp")
        with mock.patch.dict(sys.modules, {"llama_cpp": module}):
            bge._silence_llama_logs()
        self.assertIsNone(bge._llama_log_cb)

    def test_llama_callback_drops_low_levels_and_forwards_error_bytes(self) -> None:
        calls = []
        module = types.ModuleType("llama_cpp")
        module.llama_log_callback = lambda function: function
        module.llama_log_set = lambda callback, user_data: calls.append((callback, user_data))

        class Buffer:
            def __init__(self) -> None:
                self.buffer = io.BytesIO()

        stderr = Buffer()
        with mock.patch.dict(sys.modules, {"llama_cpp": module}):
            with mock.patch.object(bge.sys, "stderr", stderr):
                bge._silence_llama_logs()
                bge._llama_log_cb(3, b"warning\n", None)
                bge._llama_log_cb(4, b"error\n", None)
                bge._llama_log_cb(5, "fatal\n", None)

        self.assertEqual(len(calls), 1)
        self.assertIs(calls[0][0], bge._llama_log_cb)
        self.assertEqual(stderr.buffer.getvalue(), b"error\nfatal\n")

    def test_missing_model_exits_before_constructing_any_context(self) -> None:
        module = types.ModuleType("llama_cpp")
        module.Llama = mock.Mock()
        with mock.patch.dict(sys.modules, {"llama_cpp": module}):
            with mock.patch.object(bge.os.path, "exists", return_value=False):
                with self.assertRaises(SystemExit) as raised:
                    bge.load_model(n_gpu_layers=7, pool_size=2)
        self.assertEqual(raised.exception.code, 1)
        module.Llama.assert_not_called()
        self.assertEqual(bge._pool.qsize(), 0)
        self.assertEqual(bge._pool_size, 0)

    def test_load_model_builds_independent_contexts_and_matching_executor(self) -> None:
        contexts = [object(), object(), object()]
        llama = mock.Mock(side_effect=contexts)
        module = types.ModuleType("llama_cpp")
        module.Llama = llama

        class Executor:
            def __init__(self, **kwargs) -> None:
                self.kwargs = kwargs

        with mock.patch.dict(sys.modules, {"llama_cpp": module}):
            with mock.patch.object(bge, "_silence_llama_logs") as silence:
                with mock.patch.object(bge.os.path, "exists", return_value=True):
                    with mock.patch.object(bge.os.path, "getsize", return_value=50 * 1024 * 1024):
                        with mock.patch.object(bge, "ThreadPoolExecutor", Executor):
                            with mock.patch.object(
                                bge.time,
                                "monotonic",
                                side_effect=[10.0, 11.25],
                            ):
                                bge.load_model(n_gpu_layers=23, pool_size=3)

        silence.assert_called_once_with()
        self.assertEqual(llama.call_count, 3)
        for call in llama.call_args_list:
            self.assertEqual(call.kwargs, {
                "model_path": bge._model_path,
                "n_gpu_layers": 23,
                "n_ctx": 8192,
                "embedding": True,
                "verbose": False,
                "pooling_type": 1,
            })
        self.assertEqual([bge._pool.get() for _ in range(3)], contexts)
        self.assertEqual(bge._pool_size, 3)
        self.assertEqual(bge._executor.kwargs, {
            "max_workers": 3,
            "thread_name_prefix": "embed",
        })

    def test_embed_one_flattens_single_batch_and_returns_context_to_pool(self) -> None:
        nested = mock.Mock()
        nested.embed.return_value = [[0.1, 0.2, 0.3]]
        bge._pool.put(nested)
        self.assertEqual(bge._embed_one("hello"), [0.1, 0.2, 0.3])
        nested.embed.assert_called_once_with("hello")
        self.assertIs(bge._pool.get(), nested)

        flat = mock.Mock()
        flat.embed.return_value = [0.4, 0.5]
        bge._pool.put(flat)
        self.assertEqual(bge._embed_one("flat"), [0.4, 0.5])
        self.assertIs(bge._pool.get(), flat)

    def test_embed_one_returns_context_even_when_model_raises(self) -> None:
        model = mock.Mock()
        model.embed.side_effect = RuntimeError("CUDA context lost")
        bge._pool.put(model)
        with self.assertRaisesRegex(RuntimeError, "CUDA context lost"):
            bge._embed_one("boom")
        self.assertEqual(bge._pool.qsize(), 1)
        self.assertIs(bge._pool.get(), model)


class EndpointTests(unittest.TestCase):
    def setUp(self) -> None:
        bge._pool = queue.Queue()
        bge._pool_size = 0
        bge._executor = None

    def test_app_registers_health_and_embed_contracts(self) -> None:
        routes = [(method, path) for method, path, _kwargs, _fn in bge.app.routes]
        self.assertEqual(routes, [("GET", "/health"), ("POST", "/embed")])
        embed_route = bge.app.routes[1]
        self.assertIs(embed_route[2]["response_model"], bge.EmbedResponse)
        self.assertEqual(bge.app.kwargs["title"], "BGE-M3 Embedding Server")

    def test_health_fails_before_load_then_reports_exact_pool_metadata(self) -> None:
        with self.assertRaises(FakeHTTPException) as raised:
            asyncio.run(bge.health())
        self.assertEqual((raised.exception.status_code, raised.exception.detail), (503, "model not loaded"))

        bge._pool_size = 6
        self.assertEqual(asyncio.run(bge.health()), {
            "status": "ok",
            "model": "bge-m3-Q5_K_M",
            "dimensions": 1024,
            "pool": 6,
        })

    def test_embed_rejects_unloaded_model_without_touching_request(self) -> None:
        request = bge.EmbedRequest(texts=["one"])
        with self.assertRaises(FakeHTTPException) as raised:
            bge.embed(request)
        self.assertEqual(raised.exception.status_code, 503)
        self.assertEqual(raised.exception.detail, "model not loaded")

    def test_embed_uses_ordered_executor_map_and_reports_shape(self) -> None:
        calls = []

        class Executor:
            def map(self, function, values):
                calls.append((function, list(values)))
                return iter([[1.0, 2.0], [3.0, 4.0], [5.0, 6.0]])

        bge._pool_size = 3
        bge._executor = Executor()
        request = bge.EmbedRequest(texts=["first", "second", "third"])
        with mock.patch.object(bge.time, "monotonic", side_effect=[3.0, 3.125]):
            response = bge.embed(request)
        self.assertEqual(calls, [(bge._embed_one, ["first", "second", "third"])])
        self.assertEqual(response.embeddings, [[1.0, 2.0], [3.0, 4.0], [5.0, 6.0]])
        self.assertEqual(response.dimensions, 2)
        self.assertEqual(response.count, 3)

    def test_slow_embed_logs_once_without_changing_response(self) -> None:
        class Executor:
            def map(self, _function, _values):
                return iter([[0.25]])

        bge._pool_size = 1
        bge._executor = Executor()
        with mock.patch.object(bge.time, "monotonic", side_effect=[1.0, 2.1]):
            with mock.patch.object(bge.logger, "info") as info:
                response = bge.embed(bge.EmbedRequest(texts=["slow"]))
        info.assert_called_once_with("embed %d texts in %.0fms", 1, 1100.0)
        self.assertEqual((response.count, response.dimensions), (1, 1))

    def test_executor_failure_is_normalized_to_http_500(self) -> None:
        class Executor:
            def map(self, _function, _values):
                raise RuntimeError("worker failed")

        bge._pool_size = 1
        bge._executor = Executor()
        with mock.patch.object(bge.logger, "exception") as log:
            with self.assertRaises(FakeHTTPException) as raised:
                bge.embed(bge.EmbedRequest(texts=["bad"]))
        log.assert_called_once_with("embedding failed")
        self.assertEqual(raised.exception.status_code, 500)
        self.assertEqual(raised.exception.detail, "worker failed")

    def test_lifespan_yields_and_logs_shutdown(self) -> None:
        async def exercise():
            context = bge.lifespan(bge.app)
            await context.__aenter__()
            await context.__aexit__(None, None, None)

        with mock.patch.object(bge.logger, "info") as info:
            asyncio.run(exercise())
        info.assert_called_once_with("shutting down")


class EntryPointTests(unittest.TestCase):
    def test_health_access_filter_drops_only_health_messages(self) -> None:
        filt = bge._HealthAccessFilter()
        health = logging.LogRecord("uvicorn", logging.INFO, "", 0, "GET /health 200", (), None)
        embed = logging.LogRecord("uvicorn", logging.INFO, "", 0, "POST /embed 200", (), None)
        embedded_word = logging.LogRecord("uvicorn", logging.INFO, "", 0, "healthy pool", (), None)
        self.assertFalse(filt.filter(health))
        self.assertTrue(filt.filter(embed))
        self.assertTrue(filt.filter(embedded_word))

    def test_main_clamps_pool_size_installs_signal_filter_and_runs_uvicorn(self) -> None:
        access_logger = mock.Mock()
        fake_uvicorn.run.reset_mock()
        argv = [
            bge.__file__,
            "--host", "0.0.0.0",
            "--port", "8123",
            "--gpu-layers", "17",
            "--pool-size", "0",
        ]
        with mock.patch.object(sys, "argv", argv):
            with mock.patch.object(bge, "load_model") as load:
                with mock.patch.object(bge.signal, "signal") as install_signal:
                    with mock.patch.object(bge.logging, "getLogger", return_value=access_logger):
                        bge.main()

        load.assert_called_once_with(17, 1)
        install_signal.assert_called_once()
        self.assertEqual(install_signal.call_args.args[0], bge.signal.SIGTERM)
        handler = install_signal.call_args.args[1]
        with self.assertRaises(SystemExit) as raised:
            handler(None, None)
        self.assertEqual(raised.exception.code, 0)
        self.assertIsInstance(access_logger.addFilter.call_args.args[0], bge._HealthAccessFilter)
        fake_uvicorn.run.assert_called_once_with(
            bge.app,
            host="0.0.0.0",
            port=8123,
            log_level="info",
        )


if __name__ == "__main__":
    unittest.main()
