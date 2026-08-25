"""Role resolution contracts for model_role.py (scripts must not pin model names)."""

from __future__ import annotations

import json
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from model_role import role_model  # noqa: E402  (path set above)


def write_config(payload: object) -> str:
    handle = tempfile.NamedTemporaryFile("w", suffix=".json", delete=False, encoding="utf-8")
    json.dump(payload, handle)
    handle.close()
    return handle.name


class RoleModelTests(unittest.TestCase):
    def setUp(self) -> None:
        self.paths: list[str] = []

    def tearDown(self) -> None:
        for path in self.paths:
            os.unlink(path)

    def config(self, payload: object) -> str:
        path = write_config(payload)
        self.paths.append(path)
        return path

    def test_returns_the_bare_route_name(self) -> None:
        path = self.config({"agents": {"tinyModel": "wormhole/dsv4-nothink"}})

        self.assertEqual(role_model("tiny", "fb", path), "dsv4-nothink")

    def test_finds_the_role_at_any_nesting_depth(self) -> None:
        path = self.config({"agents": {"defaults": {"deep": {"tinyModel": "wormhole/tiny-1"}}}})

        self.assertEqual(role_model("tiny", "fb", path), "tiny-1")

    def test_unconfigured_role_yields_the_fallback(self) -> None:
        path = self.config({"agents": {"codingModel": "wormhole/glm-5.3"}})

        self.assertEqual(role_model("tiny", "fb", path), "fb")

    def test_unknown_role_yields_the_fallback(self) -> None:
        path = self.config({"agents": {"tinyModel": "wormhole/dsv4-nothink"}})

        self.assertEqual(role_model("nonesuch", "fb", path), "fb")

    def test_missing_or_broken_config_yields_the_fallback(self) -> None:
        """A lookup, never a gate: a caller with no Deneb state still runs."""
        broken = self.config("not an object")

        self.assertEqual(role_model("tiny", "fb", "/nonexistent/deneb.json"), "fb")
        self.assertEqual(role_model("tiny", "fb", broken), "fb")

    def test_empty_role_value_yields_the_fallback(self) -> None:
        path = self.config({"agents": {"tinyModel": "   "}})

        self.assertEqual(role_model("tiny", "fb", path), "fb")


if __name__ == "__main__":
    unittest.main()
