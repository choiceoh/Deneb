"""Behavior tests for doc_ref_lint — the validate-or-freeze doc reference linter.

Pin the extraction grammar and the miss-tiering: source-file misses are BROKEN
(they fail the strict gate), data/concept misses only warn, and escapes fence
lines out entirely. Uses temp fixture repos, no CodeGraph dependency.
"""

import subprocess
import tempfile
import unittest
from pathlib import Path

from doc_ref_lint import Report, collect_docs, extract_refs, lint, looks_like_path


def make_repo(files: dict[str, str]) -> Path:
    root = Path(tempfile.mkdtemp(prefix="docreflint-"))
    for rel, content in files.items():
        p = root / rel
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(content, encoding="utf-8")
    subprocess.run(["git", "init", "-q"], cwd=root, check=True)
    subprocess.run(["git", "add", "-A"], cwd=root, check=True)
    return root


class ExtractionTest(unittest.TestCase):
    def test_path_shapes(self):
        self.assertTrue(looks_like_path("scripts/dev/live-test.sh"))
        self.assertTrue(looks_like_path("asr.go:131"))
        self.assertTrue(looks_like_path("pipeline.go:AnalyzeEmailPipeline"))
        # non-paths: URLs, home paths, globs, npm scopes, RPC enumerations
        self.assertFalse(looks_like_path("https://x.dev/a/b.go"))
        self.assertFalse(looks_like_path("~/.deneb/deneb.json"))
        self.assertFalse(looks_like_path("skills/*/SKILL.md"))
        self.assertFalse(looks_like_path("@tauri-apps/plugin-http"))
        self.assertFalse(looks_like_path("chat.send/history/abort"))
        self.assertFalse(looks_like_path("client-android/.../Wire.kt"))

    def test_escapes_fence_lines_out(self):
        text = (
            "`gone.go`\n"
            "<!-- docref:off -->\n`alsogone.go`\n<!-- docref:on -->\n"
            "`ghost.go` <!-- docref:ignore -->\n"
        )
        refs = [r for _, r, _ in extract_refs(text)]
        self.assertEqual(refs, ["gone.go"])


class TieringTest(unittest.TestCase):
    def run_lint(self, files: dict[str, str]) -> Report:
        repo = make_repo(files)
        docs = collect_docs(repo, ["CLAUDE.md"])
        return lint(repo, docs, symbols=None)

    def test_source_miss_is_broken_data_miss_warns(self):
        report = self.run_lint(
            {
                "CLAUDE.md": "see `moved_away.go` and runtime file `deneb.json` and `some/api/route`",
                "real.go": "package x\n",
            }
        )
        broken = {f.ref for f in report.broken()}
        warns = {f.ref for f in report.warns()}
        self.assertEqual(broken, {"moved_away.go"})
        self.assertIn("deneb.json", warns)
        self.assertIn("some/api/route", warns)

    def test_line_anchor_past_eof_is_broken(self):
        report = self.run_lint(
            {"CLAUDE.md": "at `pkg/a.go:2` and stale `pkg/a.go:99`", "pkg/a.go": "l1\nl2\nl3\n"}
        )
        self.assertEqual([f.ref for f in report.broken()], ["pkg/a.go:99"])

    def test_resolution_doc_relative_and_suffix(self):
        report = self.run_lint(
            {
                "mod/CLAUDE.md": "sibling `impl.go`, deep shorthand `sub/deep.go`",
                "mod/impl.go": "package m\n",
                "mod/nested/sub/deep.go": "package s\n",
            }
        )
        self.assertEqual(report.broken(), [])

    def test_ambiguous_basename_surfaces_and_line_checks_all_candidates(self):
        report = self.run_lint(
            {
                "CLAUDE.md": "see `x.go` and `x.go:2` and dead `x.go:99`",
                "a/x.go": "l1\n",
                "b/x.go": "l1\nl2\nl3\n",
            }
        )
        # Every ambiguous rescue is surfaced — it's unverifiable, not verified.
        amb = [f for f in report.warns() if f.tier == "warn-ambiguous"]
        self.assertEqual(len(amb), 3)
        # Line anchors accept ANY candidate (b/x.go has 3 lines) but still
        # break when no candidate is long enough.
        self.assertEqual([f.ref for f in report.broken()], ["x.go:99"])

    def test_symbol_validation_uses_index(self):
        repo = make_repo({"CLAUDE.md": "call `pkg.KnownFunc` then `pkg.GhostFunc`"})
        report = lint(repo, collect_docs(repo, ["CLAUDE.md"]), symbols={"KnownFunc"})
        self.assertEqual([f.ref for f in report.warns()], ["pkg.GhostFunc"])

    def test_symbol_anchor_falls_back_to_file_text(self):
        # Shell functions aren't in CodeGraph — the file's own text vouches.
        report = self.run_lint_with_symbols(
            {
                "CLAUDE.md": "see `lib.sh:my_shell_fn` and `lib.sh:gone_fn`",
                "lib.sh": "my_shell_fn() { :; }\n",
            },
            symbols=set(),
        )
        self.assertEqual([f.ref for f in report.warns()], ["lib.sh:gone_fn"])

    def test_strikethrough_marks_retired_refs(self):
        report = self.run_lint({"CLAUDE.md": "~~`retired.go`~~ but `alive.go`", "alive.go": "x\n"})
        self.assertEqual(report.broken(), [])

    def run_lint_with_symbols(self, files, symbols):
        repo = make_repo(files)
        return lint(repo, collect_docs(repo, ["CLAUDE.md"]), symbols=symbols)


if __name__ == "__main__":
    unittest.main()


class FixAnchorsTest(unittest.TestCase):
    def test_fix_prefers_symbol_hint_then_drops_line(self):
        from doc_ref_lint import fix_broken_lines, lint

        repo = make_repo(
            {
                "CLAUDE.md": "see `DoThing` at `pkg/a.go:99` and bare `pkg/b.go:99`",
                "pkg/a.go": "package a\nfunc DoThing() {}\n",
                "pkg/b.go": "package b\n",
            }
        )
        docs = collect_docs(repo, ["CLAUDE.md"])
        report = lint(repo, docs, symbols=None)
        self.assertEqual(len(report.broken()), 2)
        fixed = fix_broken_lines(repo, report, {"pkg/a.go": {"DoThing": (2, 2)}})
        self.assertEqual(fixed, 2)
        text = (repo / "CLAUDE.md").read_text()
        self.assertIn("`pkg/a.go:2`", text)   # 심볼 힌트 → 심볼 시작 라인으로 재작성
        self.assertIn("`pkg/b.go`", text)      # 힌트 없음 → 라인 드롭 (거짓 앵커 제거)
        self.assertEqual(lint(repo, docs, symbols=None).broken(), [])


class DriftTest(unittest.TestCase):
    def _lint(self, files, sym_locs):
        from doc_ref_lint import lint

        repo = make_repo(files)
        return repo, lint(repo, collect_docs(repo, ["CLAUDE.md"]), symbols=None, sym_locs=sym_locs)

    def test_in_bounds_anchor_can_still_drift_outside_hinted_symbol(self):
        repo, report = self._lint(
            {"CLAUDE.md": "`DoThing` lives at `pkg/a.go:9`", "pkg/a.go": "\n" * 20},
            {"pkg/a.go": {"DoThing": (2, 5)}},
        )
        drifts = [f for f in report.findings if f.tier == "warn-drift"]
        self.assertEqual([f.ref for f in drifts], ["pkg/a.go:9"])

    def test_any_hinted_symbol_vouches(self):
        # 줄에 심볼이 여럿이면 하나라도 범위에 맞으면 무고 (Persist/ErrSkillDeduped 실사례).
        _, report = self._lint(
            {"CLAUDE.md": "`Alpha` and `Beta` at `pkg/a.go:9`", "pkg/a.go": "\n" * 20},
            {"pkg/a.go": {"Alpha": (2, 5), "Beta": (8, 12)}},
        )
        self.assertEqual([f for f in report.findings if f.tier == "warn-drift"], [])

    def test_fix_snaps_drifted_anchor_to_symbol_start(self):
        from doc_ref_lint import fix_broken_lines, lint

        repo, report = self._lint(
            {"CLAUDE.md": "`DoThing` lives at `pkg/a.go:9`", "pkg/a.go": "\n" * 20},
            {"pkg/a.go": {"DoThing": (2, 5)}},
        )
        fixed = fix_broken_lines(repo, report, {"pkg/a.go": {"DoThing": (2, 5)}})
        self.assertEqual(fixed, 1)
        self.assertIn("`pkg/a.go:2`", (repo / "CLAUDE.md").read_text())
        again = lint(repo, collect_docs(repo, ["CLAUDE.md"]), symbols=None, sym_locs={"pkg/a.go": {"DoThing": (2, 5)}})
        self.assertEqual([f for f in again.findings if f.tier == "warn-drift"], [])
