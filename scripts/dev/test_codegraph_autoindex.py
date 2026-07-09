import importlib.util
import os
import pathlib
import tempfile
import threading
import time
import unittest


MODULE_PATH = pathlib.Path(__file__).with_name("codegraph-autoindex.py")
SPEC = importlib.util.spec_from_file_location("codegraph_autoindex", MODULE_PATH)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(MODULE)


class AcquireLockTests(unittest.TestCase):
    def test_acquire_lock_allows_only_one_winner(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            lock = os.path.join(tmpdir, "codegraph.lock")
            results = []
            result_lock = threading.Lock()
            barrier = threading.Barrier(8)

            def worker():
                barrier.wait()
                won = MODULE.acquire_lock(lock, time.time(), ttl_seconds=900)
                with result_lock:
                    results.append(won)

            threads = [threading.Thread(target=worker) for _ in range(8)]
            for thread in threads:
                thread.start()
            for thread in threads:
                thread.join()

            self.assertEqual(results.count(True), 1)
            self.assertEqual(results.count(False), 7)

    def test_acquire_lock_replaces_stale_lock(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            lock = os.path.join(tmpdir, "codegraph.lock")
            pathlib.Path(lock).touch()
            stale_time = time.time() - 901
            os.utime(lock, (stale_time, stale_time))

            self.assertTrue(MODULE.acquire_lock(lock, time.time(), ttl_seconds=900))


if __name__ == "__main__":
    unittest.main()
