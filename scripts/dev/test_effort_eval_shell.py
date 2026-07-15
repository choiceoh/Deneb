"""Data-sufficiency and acceptance-boundary tests for effort-eval.sh's report stage."""

from __future__ import annotations

import subprocess
import tempfile
import unittest
from pathlib import Path

from test_shell_support import REPO_ROOT, extract_heredoc

ROWS_FILE = Path("/tmp/effort-eval-rows.tsv")
PROGRAM = extract_heredoc(
    REPO_ROOT / "scripts/dev/effort-eval.sh",
    'python3 - "$OUT" <<\'PY\'',
    "PY",
)


def korean_text(label: str) -> str:
    return f"{label} 충분한 품질을 가진 한국어 응답이며 구체적인 설명을 함께 제공합니다."


def english_text(label: str) -> str:
    return f"{label} this is a sufficiently long English-only response for the proxy."


class EffortReportTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.output = self.root / "report.md"
        self.saved_rows = ROWS_FILE.read_bytes() if ROWS_FILE.exists() else None
        self.addCleanup(self.restore_rows)
        ROWS_FILE.unlink(missing_ok=True)

    def restore_rows(self) -> None:
        if self.saved_rows is None:
            ROWS_FILE.unlink(missing_ok=True)
        else:
            ROWS_FILE.write_bytes(self.saved_rows)

    def write_rows(self, policies: dict[str, dict]) -> None:
        rows = []
        for policy in ("always-high", "always-non", "router"):
            spec = policies[policy]
            for index in range(1, 13):
                token_value = spec["tokens"](index) if callable(spec["tokens"]) else spec["tokens"]
                text_value = spec["text"](index) if callable(spec["text"]) else spec["text"]
                rows.append(f"{policy}\t{index}\t{token_value}\t{100 + index}\t{text_value}")
        ROWS_FILE.write_text("\n".join(rows) + "\n", encoding="utf-8")

    def run_program(self):
        return subprocess.run(
            ["python3", "-", str(self.output)],
            cwd=REPO_ROOT,
            input=PROGRAM,
            capture_output=True,
            text=True,
            timeout=10,
            check=False,
        )

    def full_specs(self, **overrides):
        specs = {
            "always-high": {"tokens": 100, "text": korean_text("high")},
            "always-non": {"tokens": 50, "text": korean_text("non")},
            "router": {"tokens": 60, "text": korean_text("router")},
        }
        for policy, values in overrides.items():
            specs[policy].update(values)
        return specs

    def test_missing_rows_fail_data_gate_before_writing_report(self) -> None:
        ROWS_FILE.write_text(
            "always-high\t1\t100\t20\tpartial response\n",
            encoding="utf-8",
        )
        proc = self.run_program()
        self.assertEqual(proc.returncode, 2)
        self.assertIn("VERDICT: INVALID — always-high arm has 1 rows", proc.stdout)
        self.assertIn("need 12", proc.stdout)
        self.assertFalse(self.output.exists())

    def test_too_many_zero_token_rows_are_invalid_even_with_complete_text(self) -> None:
        specs = self.full_specs()
        specs["always-non"]["tokens"] = lambda index: 0 if index <= 3 else 20
        self.write_rows(specs)
        proc = self.run_program()
        self.assertEqual(proc.returncode, 2)
        self.assertIn("always-non arm has 12 rows / 9 with tokens", proc.stdout)
        self.assertFalse(self.output.exists())

    def test_equal_quality_frontier_passes_and_returns_simple_subset_saving(self) -> None:
        self.write_rows(self.full_specs())
        proc = self.run_program()
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("VERDICT: PASS", proc.stdout)
        self.assertIn("interp ok", proc.stdout)
        self.assertIn("quality_drop 0.0pt ok", proc.stdout)
        self.assertIn("tokens: high=1200 non=600 router=720", proc.stdout)
        report = self.output.read_text(encoding="utf-8")
        self.assertIn("| always-high | 1200 | 100.0 |", report)
        self.assertIn("CAUSAL token saving on routed turns", report)
        self.assertIn("40.0% (목표 40-50%: 달성)", report)
        self.assertIn("Hard half", report)
        for index in range(1, 13):
            self.assertIn(f"| {index} | m{index} |", report)

    def test_quality_drop_over_two_points_fails_even_when_frontier_is_flat(self) -> None:
        specs = self.full_specs()
        specs["router"]["text"] = english_text("router")
        self.write_rows(specs)
        proc = self.run_program()
        self.assertEqual(proc.returncode, 0)
        self.assertIn("VERDICT: FAIL", proc.stdout)
        self.assertIn("quality_drop 40.0pt >2pt", proc.stdout)
        self.assertIn("Interpolation quality", self.output.read_text(encoding="utf-8"))

    def test_when_router_below_nondegenerate_interpolation_line_is_explicit_miss(self) -> None:
        specs = self.full_specs()
        specs["always-non"] = {"tokens": 20, "text": english_text("non")}
        specs["router"] = {"tokens": 60, "text": english_text("router")}
        self.write_rows(specs)
        proc = self.run_program()
        self.assertEqual(proc.returncode, 0)
        self.assertIn("VERDICT: FAIL (interp MISS", proc.stdout)
        report = self.output.read_text(encoding="utf-8")
        self.assertIn("Interpolation quality @ router's spend: 80.0", report)
        self.assertIn("router 60.0", report)

    def test_when_router_spend_is_clamped_to_fixed_policy_segment(self) -> None:
        specs = self.full_specs()
        specs["always-non"] = {"tokens": 20, "text": english_text("non")}
        specs["router"] = {"tokens": 5, "text": english_text("router")}
        self.write_rows(specs)
        proc = self.run_program()
        self.assertEqual(proc.returncode, 0)
        report = self.output.read_text(encoding="utf-8")
        self.assertIn("Interpolation quality @ router's spend: 60.0", report)
        self.assertNotIn("Interpolation quality @ router's spend: 52", report)

    def test_returns_html_escapes_markup_and_escapes_markdown_pipes(self) -> None:
        specs = self.full_specs()
        specs["router"]["text"] = lambda index: (
            korean_text("router") + f" <script>{index}</script> | 구분"
        )
        self.write_rows(specs)
        proc = self.run_program()
        self.assertEqual(proc.returncode, 0)
        report = self.output.read_text(encoding="utf-8")
        self.assertIn("&lt;script&gt;1&lt;/script&gt; \\| 구분", report)
        self.assertNotIn("<script>1</script>", report)


if __name__ == "__main__":
    unittest.main()
