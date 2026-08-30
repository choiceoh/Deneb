#!/usr/bin/env python3
"""motion-analyze.py — measure a UI transition instead of eyeballing it.

`native-app.sh rec-stop` gives a video and an evenly spaced filmstrip. That was
enough to prove the app animates at all and not much more: an even strip over two
seconds samples every 160ms, Compose transitions run 130-300ms, so a whole
transition hides inside one cell. Reading it that way once produced the confident
and wrong conclusion "there is no animation here".

This finds the transitions itself and reports numbers for each one — when it
started, how long it took, what shape the progress curve has, whether the change
moved across the screen or stayed put, and whether any frames were dropped in the
middle. Those are the questions "is the motion good" decomposes into, and none of
them are answerable by looking at stills.

    scripts/dev/motion-analyze.py <video.mp4> [--strip] [--floor 0.4]

Entry points: main, segments, describe.
Verify: scripts/dev/motion-analyze.py <any recording from native-app.sh rec-stop>
Boundary: this measures pixels over time. It does not know what the UI meant to do
— naming a transition "slide" or "fade" is a description of the pixels, and whether
that was the right choice is a design judgement (docs/adr/0007-design-north-star.md).
Frame pacing is NOT measurable here: the harness renders under Xvfb with a software
renderer, so dropped frames mean the recorder or the compositor, not the phone.
"""

from __future__ import annotations

import argparse
import shutil
import subprocess
import tempfile
from pathlib import Path

import numpy as np
from PIL import Image

# A still screen is not bit-identical: the recorder's encoder and the software
# renderer both dither slightly. This floor (mean abs 0-255 difference per pixel)
# sits above that noise and below any real motion — idle measured 0.001 on the
# 2026-08-30 recordings, so this is ~100x the noise.
#
# ★ Duration is floor-sensitive and the report prints the floor for that reason. A
# fade's last frames carry very little change, so raising the floor silently trims
# the tail: the tab switch measures 150ms at 0.4 and 200ms at 0.05, and it is the
# same animation both times. Cross-check with --floor before quoting a number.
DEFAULT_FLOOR = 0.1


def decode(video: Path, out_dir: Path) -> tuple[list[np.ndarray], float]:
    """Every frame as greyscale arrays, plus the video's real frame rate."""
    rate = subprocess.run(
        ["ffprobe", "-v", "error", "-select_streams", "v:0",
         "-show_entries", "stream=avg_frame_rate", "-of", "csv=p=0", str(video)],
        capture_output=True, text=True, check=True,
    ).stdout.strip()
    num, _, den = rate.partition("/")
    fps = float(num) / float(den or 1)

    subprocess.run(
        ["ffmpeg", "-y", "-loglevel", "error", "-i", str(video), f"{out_dir}/%05d.png"],
        check=True,
    )
    frames = [np.asarray(Image.open(p).convert("L"), dtype=np.float32)
              for p in sorted(out_dir.glob("*.png"))]
    if len(frames) < 3:
        raise SystemExit(f"{video}: only {len(frames)} frames — nothing to measure")
    return frames, fps


def segments(deltas: np.ndarray, fps: float, floor: float) -> list[tuple[int, int]]:
    """Runs of frames that are changing, joined across brief stalls.

    A transition can pause mid-flight (a layout settles, then text fades in) and
    splitting that into two findings would hide the relationship, so gaps shorter
    than 100ms stay inside one segment.
    """
    active = deltas > floor
    gap_frames = max(1, int(0.10 * fps))
    runs: list[list[int]] = []
    for i, is_active in enumerate(active):
        if not is_active:
            continue
        if runs and i - runs[-1][1] <= gap_frames:
            runs[-1][1] = i
        else:
            runs.append([i, i])
    return [(a, b) for a, b in runs if b > a]


def change_box(a: np.ndarray, b: np.ndarray, floor: float) -> tuple[int, int, int, int] | None:
    """Bounding box of where two frames differ — the footprint of the movement."""
    mask = np.abs(a - b) > max(floor * 4, 6.0)
    rows, cols = np.any(mask, axis=1), np.any(mask, axis=0)
    if not rows.any() or not cols.any():
        return None
    y = np.where(rows)[0]
    x = np.where(cols)[0]
    return int(x[0]), int(y[0]), int(x[-1]), int(y[-1])


def translation_fit(a: np.ndarray, b: np.ndarray) -> tuple[int, float]:
    """Best vertical shift explaining b from a, and how much it beats no shift.

    This exists because the change-box centroid cannot tell a slide from a stagger.
    When content appears top-to-bottom in sequence the changing region marches down
    the screen exactly as it would if the screen had scrolled, and the first version
    of this script duly reported the feed's row expansion as a 541px vertical slide.
    It was not: nothing translated, different things appeared at different times.

    A real translation reconstructs the later frame from the earlier one at some
    offset. A stagger does not, because the new content was not on screen before.
    Returns (best_dy, gain) where gain > 1 means the shift explains the frame better
    than leaving it in place; a true slide lands well above 1.5.
    """
    small_a = a[::4, ::4]
    small_b = b[::4, ::4]
    h = small_a.shape[0]
    base = float(np.abs(small_a - small_b).mean())
    best_dy, best_err = 0, base
    for dy in range(-h // 3, h // 3 + 1):
        if dy == 0:
            continue
        if dy > 0:
            err = float(np.abs(small_a[:-dy] - small_b[dy:]).mean())
        else:
            err = float(np.abs(small_a[-dy:] - small_b[:dy]).mean())
        if err < best_err:
            best_dy, best_err = dy, err
    gain = base / max(best_err, 1e-9)
    return best_dy * 4, gain


def describe(frames: list[np.ndarray], start: int, end: int, fps: float,
             deltas: np.ndarray, floor: float) -> dict:
    """Turn one segment into the numbers a design review actually argues about."""
    span = deltas[start:end + 1]
    total = float(span.sum())
    dur_ms = (end - start + 1) / fps * 1000.0

    # Progress curve: what fraction of the total change had landed by each frame.
    # Its shape is the easing — a straight ramp is linear, a curve that front-loads
    # is ease-out, and a single step means the UI did not animate at all.
    cumulative = np.cumsum(span) / max(total, 1e-9)
    peak_share = float(span.max() / max(total, 1e-9))
    # Where the halfway point sits tells ease-in from ease-out without eyeballing.
    half_at = float(np.searchsorted(cumulative, 0.5) + 1) / len(span)

    boxes = [change_box(frames[i], frames[i + 1], floor) for i in range(start, end)]
    boxes = [b for b in boxes if b]
    drift_x = drift_y = 0
    if len(boxes) >= 2:
        cx = [(b[0] + b[2]) / 2 for b in boxes]
        cy = [(b[1] + b[3]) / 2 for b in boxes]
        drift_x = int(max(cx) - min(cx))
        drift_y = int(max(cy) - min(cy))

    # Frames inside an active stretch that carried no change: the animation stalled.
    stalled = int(np.sum(span <= floor))

    # Only call it a slide if the pixels actually translated. The drift box alone
    # says "the changing region moved", which a top-to-bottom stagger also does.
    shift, gain = translation_fit(frames[start], frames[end])
    if peak_share > 0.80:
        kind = "snap (한 프레임이 변화의 대부분)"
    elif (drift_x > 40 or drift_y > 40) and gain > 1.5 and abs(shift) > 16:
        kind = f"slide (세로 {shift:+d}px 평행이동, 설명력 {gain:.1f}x)"
    elif drift_y > 40 and gain <= 1.5:
        kind = (f"stagger (변화가 세로 {drift_y}px에 걸쳐 순차 등장 — "
                f"평행이동 아님, 설명력 {gain:.1f}x)")
    else:
        kind = "fade/expand (제자리에서 밝기·크기 변화)"

    if half_at < 0.4:
        easing = "front-loaded (빠르게 시작해 감속 — ease-out 계열)"
    elif half_at > 0.6:
        easing = "back-loaded (느리게 시작해 가속 — ease-in 계열)"
    else:
        easing = "선형에 가까움"

    return {
        "start_s": start / fps, "dur_ms": dur_ms, "kind": kind,
        "easing": easing, "half_at": half_at, "peak_share": peak_share,
        "stalled": stalled, "frames": end - start + 1,
        "drift": (drift_x, drift_y),
    }


def write_strip(video: Path, start_s: float, dur_ms: float, out: Path, count: int = 10) -> None:
    """A strip of the active window only — the frames that actually differ."""
    with tempfile.TemporaryDirectory() as td:
        paths = []
        for i in range(count):
            t = start_s + (dur_ms / 1000.0) * i / max(count - 1, 1)
            p = Path(td) / f"{i:02d}.png"
            subprocess.run(["ffmpeg", "-y", "-loglevel", "error", "-ss", f"{t:.3f}",
                            "-i", str(video), "-frames:v", "1", str(p)], check=True)
            paths.append((t, p))
        ims = [(t, Image.open(p).convert("RGB")) for t, p in paths]
        w, h = ims[0][1].size
        scale = min(1.0, 190 / w)
        tw, th = int(w * scale), int(h * scale)
        strip = Image.new("RGB", (tw * len(ims) + 4 * (len(ims) - 1), th + 16), (24, 24, 24))
        from PIL import ImageDraw
        draw = ImageDraw.Draw(strip)
        for i, (t, im) in enumerate(ims):
            x = i * (tw + 4)
            strip.paste(im.resize((tw, th), Image.LANCZOS), (x, 16))
            draw.text((x + 2, 3), f"{t:.3f}s", fill=(190, 190, 190))
        strip.save(out)


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("video", type=Path)
    ap.add_argument("--floor", type=float, default=DEFAULT_FLOOR)
    ap.add_argument("--strip", action="store_true", help="write a PNG strip per segment")
    args = ap.parse_args()

    if not args.video.exists():
        raise SystemExit(f"no such video: {args.video}")

    tmp = Path(tempfile.mkdtemp(prefix="motion-"))
    try:
        frames, fps = decode(args.video, tmp)
        deltas = np.array([float(np.abs(frames[i] - frames[i - 1]).mean())
                           for i in range(1, len(frames))])
        idle = float(np.median(deltas))
        found = segments(deltas, fps, args.floor)

        print(f"{args.video.name}  {len(frames)} frames @ {fps:.1f}fps "
              f"({len(frames) / fps:.2f}s)  idle noise {idle:.3f}  floor {args.floor}")
        if not found:
            print(f"  변화 없음 (floor {args.floor}) — 화면이 정지해 있었다")
            return 0

        for n, (a, b) in enumerate(found, 1):
            d = describe(frames, a, b, fps, deltas, args.floor)
            print(f"\n  [{n}] {d['start_s']:.3f}s 부터 {d['dur_ms']:.0f}ms "
                  f"({d['frames']} frames)")
            print(f"      종류   {d['kind']}")
            print(f"      이징   {d['easing']}  (50% 지점 {d['half_at']:.0%})")
            print(f"      최대 단일 프레임 비중 {d['peak_share']:.0%}"
                  f"  ·  변화 상자 이동 {d['drift'][0]}x{d['drift'][1]}px")
            if d["stalled"]:
                print(f"      ⚠ 진행 중 정지 프레임 {d['stalled']}개 "
                      f"— 녹화 드롭이거나 애니메이션이 끊긴 것")
            if args.strip:
                out = args.video.with_name(f"{args.video.stem}-seg{n}.png")
                write_strip(args.video, d["start_s"], d["dur_ms"], out)
                print(f"      strip  {out}")
        return 0
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


if __name__ == "__main__":
    raise SystemExit(main())
