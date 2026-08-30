#!/usr/bin/env python3
"""render-golden.py — pixel regression gate for the native client's preview renders.

`:composeApp:renderPreviews` draws ~117 PNGs of real screens. Until now they were
drawn and looked at: nothing compared one run to the next, so a change that moved
type by two pixels was invisible unless somebody happened to be staring at the
right image. Every native defect found on 2026-08-30 had exactly that shape —
Korean line spacing 29px -> 36px, an elapsed suffix sitting 2px above its status,
a suffix clipped out of existence when the status wrapped. None of them were
visible in a diff of the Kotlin.

This compares a fresh render against committed goldens and fails on any
difference, writing a side-by-side strip for each one so the change can be judged
rather than guessed at.

    scripts/dev/render-golden.py check     # render + compare (the gate)
    scripts/dev/render-golden.py update    # accept the current render as golden

Determinism is a precondition, not an assumption: two consecutive renders of the
same tree are byte-identical for 115 of 117 fixtures. The preview clock is frozen
(PREVIEW_NOW_MS in RenderPreviewFixtures.kt) so relative timestamps stop drifting;
the two that remain read a real clock from inside production code and are skipped
by name below.

Entry points: main, render, write_diff.
Verify: scripts/dev/render-golden.py check  (also run by `make render-golden-check`).
Boundary: this script owns comparison only. Fixtures and their registration live in
client-android/app/composeApp/src/desktopMain/kotlin/ai/deneb/RenderPreview*.kt —
add a screen there, then update goldens here.
"""

from __future__ import annotations

import hashlib
import os
import shutil
import subprocess
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
APP = REPO / "client-android" / "app"
GOLDEN = APP / "composeApp" / "screenshots" / "golden"
RENDER = Path("/tmp/deneb-render")
DIFF_OUT = Path("/tmp/deneb-render-diff")

# Fixtures whose pixels legitimately change between runs. WorkFeedPanel renders a
# relative timestamp ("3분 전") from a real clock inside the component itself, so
# freezing the fixture clock does not reach it — and reaching into production code
# to make a preview deterministic would be the tail wagging the dog.
SKIP = {"workfeed_dark.png", "workfeed_light.png"}


def render() -> None:
    """Draw the previews into an empty directory, failing loudly if the SDK is missing.

    The directory is cleared first so it holds this run's output and nothing else.
    Without that, `update` copies whatever else is lying in /tmp/deneb-render into the
    goldens (it swept up a scratch crop once), and a deleted fixture's stale PNG keeps
    matching forever instead of showing up as MISSING.
    """
    shutil.rmtree(RENDER, ignore_errors=True)
    env = dict(os.environ)
    env.setdefault("ANDROID_HOME", str(Path.home() / "android-sdk"))
    proc = subprocess.run(
        ["./gradlew", ":composeApp:renderPreviews", "--console=plain", "-q"],
        cwd=APP,
        env=env,
        capture_output=True,
        text=True,
    )
    if proc.returncode != 0:
        sys.exit(f"renderPreviews failed:\n{proc.stdout}\n{proc.stderr}")


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def write_diff(name: str, golden: Path, fresh: Path) -> str:
    """Write a golden|fresh|amplified-difference strip and describe the change."""
    try:
        from PIL import Image, ImageChops
    except ImportError:
        return "(install Pillow for a visual diff)"

    a, b = Image.open(golden).convert("RGB"), Image.open(fresh).convert("RGB")
    if a.size != b.size:
        return f"size {a.size} -> {b.size}"

    delta = ImageChops.difference(a, b)
    box = delta.getbbox()
    changed = sum(1 for px in delta.convert("L").getdata() if px > 8)

    DIFF_OUT.mkdir(parents=True, exist_ok=True)
    strip = Image.new("RGB", (a.width * 3 + 24, a.height), (16, 16, 16))
    strip.paste(a, (0, 0))
    strip.paste(b, (a.width + 12, 0))
    strip.paste(delta.point(lambda v: min(255, v * 6)), (a.width * 2 + 24, 0))
    strip.save(DIFF_OUT / name)
    return f"{changed}px changed, bbox={box} -> {DIFF_OUT / name}"


def main() -> int:
    mode = sys.argv[1] if len(sys.argv) > 1 else "check"
    if mode not in {"check", "update"}:
        sys.exit(__doc__)

    render()
    fresh = {p.name: p for p in sorted(RENDER.glob("*.png")) if p.name not in SKIP}

    if mode == "update":
        GOLDEN.mkdir(parents=True, exist_ok=True)
        for stale in GOLDEN.glob("*.png"):
            if stale.name not in fresh:
                stale.unlink()
        for name, path in fresh.items():
            shutil.copy2(path, GOLDEN / name)
        print(f"golden updated: {len(fresh)} PNGs -> {GOLDEN.relative_to(REPO)}")
        return 0

    golden = {p.name: p for p in sorted(GOLDEN.glob("*.png"))}
    if not golden:
        sys.exit(f"no goldens in {GOLDEN} — run: scripts/dev/render-golden.py update")

    changed = [n for n in sorted(golden.keys() & fresh.keys()) if digest(golden[n]) != digest(fresh[n])]
    missing = sorted(golden.keys() - fresh.keys())  # a fixture was removed
    added = sorted(fresh.keys() - golden.keys())  # a fixture was added

    for name in changed:
        print(f"  CHANGED  {name}  {write_diff(name, golden[name], fresh[name])}")
    for name in missing:
        print(f"  MISSING  {name}  (golden exists, nothing rendered)")
    for name in added:
        print(f"  NEW      {name}  (rendered, no golden yet)")

    if changed or missing or added:
        print(
            f"\n{len(changed)} changed · {len(missing)} missing · {len(added)} new"
            f"\nLook at the strips in {DIFF_OUT} (golden | fresh | amplified diff)."
            "\nIf the change is intended: scripts/dev/render-golden.py update"
        )
        return 1

    print(f"render golden: {len(golden)} PNGs match")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
