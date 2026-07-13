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
    "- 완료 후 상태 갱신은 불필요 — 배차 마커가 원장이다."
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
            self.assertIn("배차 마커가 원장이다", prompt)
            # Marker written with prompt provenance (RSI P1.5 attribution).
            rec = json.loads(marker.read_text(encoding="utf-8"))
            want = hashlib.sha256(CONTRACT.encode("utf-8")).hexdigest()[:12]
            self.assertEqual(rec["promptVersion"], want)
            self.assertEqual(rec["promptSource"], "artifact")
            self.assertEqual(rec["id"], "sc-test-1234")

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
