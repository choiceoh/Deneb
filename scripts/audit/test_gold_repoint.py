"""Fixture tests for gold_repoint."""

from __future__ import annotations

import json
import os
import subprocess
import tempfile
import unittest

import gold_repoint as gr


def write_page(wiki: str, rel: str, title: str) -> None:
    path = os.path.join(wiki, rel)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8") as fh:
        fh.write(f"---\ntitle: {title}\ncategory: 프로젝트\n---\n\n본문\n")


class GoldRepointTest(unittest.TestCase):
    def test_resolves_by_basename_title_and_project_folder(self) -> None:
        with tempfile.TemporaryDirectory() as wiki:
            write_page(wiki, "프로젝트/거래/현대에너지솔루션.md", "현대에너지솔루션")
            write_page(wiki, "프로젝트/pl1-ygb-bes-001/대표.md", "영광 한빛 BESS (80MW/480MWh)")
            write_page(wiki, "기타/차량-핫스팟.md", "차량 핫스팟")
            pages = gr.live_pages(wiki)

            # moved file, same filename
            self.assertEqual(
                gr.resolve(wiki, "업무/현대에너지솔루션", pages),
                ("프로젝트/거래/현대에너지솔루션", "unique-basename"),
            )
            # renamed file, title still matches
            self.assertEqual(
                gr.resolve(wiki, "시스템/차량-핫스팟", pages),
                ("기타/차량-핫스팟", "unique-basename"),
            )
            # folder migrated to a deal code: the rep title keeps the alias
            self.assertEqual(
                gr.resolve(wiki, "프로젝트/영광-한빛-bess", pages),
                ("프로젝트/pl1-ygb-bes-001", "project-folder-title"),
            )

    def test_refuses_ambiguous_and_missing(self) -> None:
        with tempfile.TemporaryDirectory() as wiki:
            write_page(wiki, "프로젝트/a/대표.md", "같은 이름")
            write_page(wiki, "프로젝트/b/대표.md", "같은 이름")
            pages = gr.live_pages(wiki)

            new, reason = gr.resolve(wiki, "프로젝트/사라진-것", pages)
            self.assertEqual(new, "")
            self.assertIn("no candidate", reason)

            # 대표.md exists many times over → basename is not a signal
            new, reason = gr.resolve(wiki, "프로젝트/옛폴더/대표.md", pages)
            self.assertEqual(new, "")
            self.assertIn("ambiguous", reason)

    def test_dry_run_leaves_the_file_untouched(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            wiki = os.path.join(root, "wiki")
            write_page(wiki, "프로젝트/거래/현대에너지솔루션.md", "현대에너지솔루션")
            gold = os.path.join(root, "wiki-qa-gold.jsonl")
            row = {"id": "c1", "question": "q", "gold_paths": ["업무/현대에너지솔루션"]}
            with open(gold, "w", encoding="utf-8") as fh:
                fh.write("# header comment\n" + json.dumps(row, ensure_ascii=False) + "\n")
            before = open(gold, encoding="utf-8").read()
            pages = gr.live_pages(wiki)

            repointed, unresolved = gr.process(gold, wiki, pages, apply=False)
            self.assertEqual((repointed, unresolved), (1, 0))
            self.assertEqual(open(gold, encoding="utf-8").read(), before)

            repointed, _ = gr.process(gold, wiki, pages, apply=True)
            self.assertEqual(repointed, 1)
            written = [
                json.loads(line)
                for line in open(gold, encoding="utf-8").read().split("\n")
                if line.strip() and not line.startswith("#")
            ]
            self.assertEqual(written[0]["gold_paths"], ["프로젝트/거래/현대에너지솔루션"])
            # The comment header survives, and a backup exists.
            self.assertTrue(open(gold, encoding="utf-8").read().startswith("# header comment"))
            self.assertTrue(any(n.startswith("wiki-qa-gold.jsonl.bak-") for n in os.listdir(root)))

    def test_git_rename_is_preferred_evidence(self) -> None:
        if not _have_git():
            self.skipTest("git not available")
        with tempfile.TemporaryDirectory() as wiki:
            _git(wiki, "init", "-q")
            _git(wiki, "config", "user.email", "t@t")
            _git(wiki, "config", "user.name", "t")
            write_page(wiki, "프로젝트/옛-이름/대표.md", "프로젝트 하나")
            _git(wiki, "add", "-A")
            _git(wiki, "commit", "-qm", "seed")
            os.makedirs(os.path.join(wiki, "프로젝트/pl1-abc-dev-001"), exist_ok=True)
            os.replace(
                os.path.join(wiki, "프로젝트/옛-이름/대표.md"),
                os.path.join(wiki, "프로젝트/pl1-abc-dev-001/대표.md"),
            )
            os.rmdir(os.path.join(wiki, "프로젝트/옛-이름"))
            _git(wiki, "add", "-A")
            _git(wiki, "commit", "-qm", "rename")

            pages = gr.live_pages(wiki)
            new, reason = gr.resolve(wiki, "프로젝트/옛-이름/대표.md", pages)
            self.assertEqual((new, reason), ("프로젝트/pl1-abc-dev-001/대표", "git-rename"))


def _have_git() -> bool:
    try:
        subprocess.run(["git", "--version"], capture_output=True, check=True)
        return True
    except (OSError, subprocess.SubprocessError):
        return False


def _git(wiki: str, *args: str) -> None:
    subprocess.run(["git", "-C", wiki, *args], capture_output=True, check=True)


if __name__ == "__main__":
    unittest.main()
