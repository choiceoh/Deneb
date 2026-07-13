"""Deterministic tests for the L4 dispatch-prompt composer.

The load-bearing behaviors: a usable artifact yields the composed prompt plus
a marker carrying prompt provenance; an unusable artifact DEFERS (no marker,
no output, DEFER_EXIT) instead of falling back to a second copy of the
contract; and the artifact name cannot drift from the Go registry constant.
"""

from __future__ import annotations

import hashlib
import io
import json
import re
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest import mock

import dispatch_prompt

CONTRACT = (
    "## 계약 (오퍼레이터 승인 2026-07-12)\n"
    "- 이 워크트리에서만 편집. 게이트 전부 준수하며 검증까지 완료하라.\n"
    "- 게이트 그린이면 커밋 → push → PR → 체크 그린 대기 → 직접 랜딩하라.\n"
    "- 구현이 부적절하다고 판단되면 아무것도 랜딩하지 말고 근거를 남겨라.\n"
    "- 배차·PR·머지 결과는 호출 스크립트가 tracker RPC 원장에 기록한다. "
    "skill_lifecycle 상태를 직접 조작하거나 배차 마커를 완료 증거로 간주하지 마라."
)

CANDIDATE = {
    "id": "sc-test-1234",
    "title": "toolctx 캐시 무효화 수리",
    "skillName": "",
    "candidate": "관찰 내용",
    "proposedChange": "제안 변경 내용",
    "evidence": "health-finding:volatile-contract",
    "risk": "낮음",
}


def run_main(meta_dir: Path, marker: Path, candidate: dict) -> tuple[int, str, str]:
    argv = [
        "dispatch_prompt.py",
        "--meta-dir",
        str(meta_dir),
        "--marker",
        str(marker),
        "--attempt-id",
        "attempt-1",
        "--branch",
        "dispatch/sc-test-1234",
    ]
    out, err = io.StringIO(), io.StringIO()
    stdin = io.StringIO(json.dumps(candidate, ensure_ascii=False))
    with (
        mock.patch("sys.argv", argv),
        mock.patch("sys.stdin", stdin),
        redirect_stdout(out),
        redirect_stderr(err),
    ):
        rc = dispatch_prompt.main()
    return rc, out.getvalue(), err.getvalue()


class DispatchPromptTest(unittest.TestCase):
    def test_artifact_composes_prompt_and_marker(self):
        with TemporaryDirectory() as td:
            meta = Path(td) / "meta"
            meta.mkdir()
            (meta / dispatch_prompt.ARTIFACT_NAME).write_text(
                CONTRACT, encoding="utf-8"
            )
            marker = Path(td) / "sc-test-1234.json"
            rc, prompt, _ = run_main(meta, marker, CANDIDATE)
            self.assertEqual(rc, 0)
            # Candidate data block + contract policy, id in the header.
            self.assertIn("id=sc-test-1234", prompt)
            self.assertIn("- 제목: toolctx 캐시 무효화 수리", prompt)
            self.assertIn("## 계약", prompt)
            self.assertIn("tracker RPC 원장에 기록한다", prompt)
            # Marker written with prompt provenance (RSI P1.5 attribution).
            rec = json.loads(marker.read_text(encoding="utf-8"))
            want = hashlib.sha256(CONTRACT.encode("utf-8")).hexdigest()[:12]
            self.assertEqual(rec["promptVersion"], want)
            self.assertEqual(rec["promptSource"], "artifact")
            self.assertEqual(rec["id"], "sc-test-1234")
            self.assertEqual(rec["attemptId"], "attempt-1")
            self.assertEqual(rec["branch"], "dispatch/sc-test-1234")

    def test_absent_artifact_defers_without_marker(self):
        with TemporaryDirectory() as td:
            meta = Path(td) / "meta"
            meta.mkdir()
            marker = Path(td) / "m.json"
            rc, prompt, err = run_main(meta, marker, CANDIDATE)
            self.assertEqual(rc, dispatch_prompt.DEFER_EXIT)
            self.assertEqual(prompt, "")
            self.assertFalse(marker.exists(), "defer must not burn the marker")
            self.assertIn("deferring dispatch", err)

    def test_retry_snapshots_prior_dispatch_time_and_charges_a_new_attempt(self):
        with TemporaryDirectory() as td:
            meta = Path(td) / "meta"
            meta.mkdir()
            (meta / dispatch_prompt.ARTIFACT_NAME).write_text(CONTRACT, encoding="utf-8")
            marker = Path(td) / "sc-test-1234.json"
            marker.write_text(json.dumps({
                **CANDIDATE,
                "attemptId": "attempt-0",
                "outcome": "failed",
                "dispatchedAt": 1000,
            }) + "\n", encoding="utf-8")
            with mock.patch("time.time", return_value=2):
                rc, _, _ = run_main(meta, marker, CANDIDATE)
            self.assertEqual(rc, 0)
            rec = json.loads(marker.read_text(encoding="utf-8"))
            self.assertEqual(rec["dispatchedAt"], 2000)
            self.assertEqual(rec["attempts"][-1]["dispatchedAt"], 1000)

    def test_short_artifact_defers(self):
        with TemporaryDirectory() as td:
            meta = Path(td) / "meta"
            meta.mkdir()
            (meta / dispatch_prompt.ARTIFACT_NAME).write_text(
                "짧은 스텁", encoding="utf-8"
            )
            marker = Path(td) / "m.json"
            rc, _, _ = run_main(meta, marker, CANDIDATE)
            self.assertEqual(rc, dispatch_prompt.DEFER_EXIT)
            self.assertFalse(marker.exists())

    def test_forbidden_surface_mention_defers(self):
        # C2 defense-in-depth: prose naming acceptance machinery must never
        # reach a headless session with landing authority — defer, no marker.
        with TemporaryDirectory() as td:
            meta = Path(td) / "meta"
            meta.mkdir()
            (meta / dispatch_prompt.ARTIFACT_NAME).write_text(
                CONTRACT, encoding="utf-8"
            )
            marker = Path(td) / "m.json"
            bad = dict(CANDIDATE)
            bad["proposedChange"] = "validation_engine.go의 flip gate 완화"
            rc, prompt, err = run_main(meta, marker, bad)
            self.assertEqual(rc, dispatch_prompt.DEFER_EXIT)
            self.assertEqual(prompt, "")
            self.assertFalse(marker.exists(), "defer must not burn the marker")
            self.assertIn("forbidden acceptance surfaces", err)
            # Structured targets are scanned too.
            bad2 = dict(CANDIDATE)
            bad2["targetFiles"] = ["scripts/dev/coding-dispatch.sh"]
            rc2, _, err2 = run_main(meta, marker, bad2)
            self.assertEqual(rc2, dispatch_prompt.DEFER_EXIT)
            self.assertIn("coding-dispatch.sh", err2)

    def test_forbidden_basenames_cover_go_whitelist(self):
        # Every basename in the Go forbidden surface whitelist must be in the
        # scripts-side guard so the two cannot drift. Order/comment-tolerant:
        # a forbidden entry is any struct literal whose body contains
        # `Tier: SurfaceTierForbidden`, regardless of field order or interposed
        # comments/Note (3rd-review C2-C3 — the old regex assumed Tier directly
        # before Patterns and could be evaded). pr.sh/ci.yml are now guarded
        # too (boundary matching makes them prose-safe), so no exemption.
        go_src = (
            Path(__file__).resolve().parents[2]
            / "gateway-go/internal/domain/skills/genesis/surfaces/surfaces.go"
        ).read_text(encoding="utf-8")
        # Split into brace-balanced struct literals inside DeclaredEditableSurfaces
        # and keep those declaring the forbidden tier.
        entries = re.findall(r"\{([^{}]*Patterns:\s*\[\]string\{[^}]*\}[^{}]*)\}", go_src, re.S)
        forbidden_blocks = [e for e in entries if "SurfaceTierForbidden" in e]
        self.assertTrue(forbidden_blocks, "no forbidden pattern blocks found in surfaces.go")
        for block in forbidden_blocks:
            patterns_body = re.search(r"Patterns:\s*\[\]string\{(.*?)\}", block, re.S)
            self.assertIsNotNone(patterns_body)
            for pattern in re.findall(r'"([^"]+)"', patterns_body.group(1)):
                if pattern.startswith("*."):
                    continue  # extension globs are a tier, not a basename to scan
                basename = pattern.rsplit("/", 1)[-1]
                self.assertIn(
                    basename,
                    dispatch_prompt.FORBIDDEN_SURFACE_BASENAMES,
                    f"surfaces.go forbidden pattern {pattern} missing from dispatch guard",
                )

    def test_artifact_name_matches_go_registry(self):
        # The gateway materializes the artifact this script consumes; the two
        # sides name it independently, so pin the parity against the Go source.
        go_src = (
            Path(__file__).resolve().parents[2]
            / "gateway-go/internal/domain/skills/genesis/generation/meta_artifacts.go"
        ).read_text(encoding="utf-8")
        m = re.search(r'MetaDispatchContractPrompt\s*=\s*"([^"]+)"', go_src)
        self.assertIsNotNone(m, "Go registry constant not found")
        self.assertEqual(m.group(1), dispatch_prompt.ARTIFACT_NAME)


if __name__ == "__main__":
    unittest.main()
