"""Is the dreamer's self-declared page confidence worth anything?

The dream-quality score (`wiki/dreamer_quality.go`) has a Confidence axis: the
mean of the confidence label the SYNTHESIS MODEL put on its own pages. That
number is ledgered and read by the slow loop that rewrites the synthesis rules,
which makes it a self-report inside a scored, optimized loop.

"The Personalization Mirage" (arXiv:2608.04570) is the reason to distrust that
shape: across 12 models it found a Self-Monitoring Inversion — a model's own
assessment of how much it over-inferred was NEGATIVELY rank-correlated with an
independent judge's measurement (rho = -0.60). If that holds here, the axis
rewards confident-sounding synthesis rather than good synthesis.

This audit answers it with data we already have, no model in the loop: join the
index's per-page confidence against the recall-hit ledger (`.recall-hits.jsonl`,
written when a page is actually served to a turn) and report the recalled
fraction per confidence band. Recall is the honest external check — it is what
the page was written FOR, and confidence is not an input to ranking, so the
comparison is not circular.

Reading it:

- monotone (high > medium > low) — the self-report carries information and the
  Confidence axis is earned.
- flat — the label is noise; the axis is measuring nothing.
- inverted (high < low) — the paper's finding reproduces here and the axis is
  actively rewarding the wrong thing.

Measured 2026-08-24 on 1,195 indexed pages / 1,303 recall hits:
high 112/186 = 0.602, medium 198/610 = 0.325, low 5/225 = 0.022 — strongly
monotone. The inversion does NOT reproduce at page level in this corpus, so the
axis stays as it is; this audit is what makes that claim checkable rather than
assumed.

Read-only and advisory: it never gates and never writes.

Entrypoints: `calibrate`, `render`, `main`.
Test: `scripts/audit/test_wiki_confidence_calibration.py`.
Verify: `make python-test`, `python3 scripts/audit/wiki-confidence-calibration.py --json`.
"""

from __future__ import annotations

import argparse
import json
import os
from collections import Counter
from dataclasses import dataclass, field
from pathlib import Path

# Bands are reported in this order — the monotonicity check reads it directly.
BAND_ORDER = ("high", "medium", "low")
UNSET_BAND = "unset"

# The index is a TSV whose header line starts here; the file repeats the header
# per section, so the parser keys columns by name instead of position.
INDEX_HEADER_PREFIX = "id\tpath\t"

# Below this spread the bands are indistinguishable — "flat" rather than a
# monotone trend that happens to be tiny.
FLAT_SPREAD = 0.05


def wiki_dir() -> Path:
    override = os.environ.get("DENEB_WIKI_DIR", "").strip()
    if override:
        return Path(override)
    return Path.home() / ".deneb" / "wiki"


@dataclass
class BandStat:
    pages: int = 0
    recalled: int = 0
    hits: int = 0

    @property
    def rate(self) -> float | None:
        if self.pages <= 0:
            return None
        return self.recalled / self.pages


@dataclass
class Calibration:
    bands: dict[str, BandStat] = field(default_factory=dict)
    pages: int = 0
    hit_paths: int = 0

    def rate(self, band: str) -> float | None:
        stat = self.bands.get(band)
        return stat.rate if stat else None

    @property
    def verdict(self) -> str:
        """monotone | flat | inverted | unmeasured.

        Only bands with pages participate: a corpus missing a band cannot be
        called inverted on the strength of the ones that are present.
        """
        present = [(band, self.rate(band)) for band in BAND_ORDER if self.rate(band) is not None]
        if len(present) < 2:
            return "unmeasured"
        values = [rate for _, rate in present]
        if max(values) - min(values) < FLAT_SPREAD:
            return "flat"
        if all(a > b for a, b in zip(values, values[1:])):
            return "monotone"
        if all(a < b for a, b in zip(values, values[1:])):
            return "inverted"
        return "unmeasured"


def parse_index(path: Path) -> list[dict[str, str]]:
    """Rows of the wiki index TSV, keyed by its repeated header line."""
    rows: list[dict[str, str]] = []
    header: list[str] | None = None
    try:
        text = path.read_text(encoding="utf-8")
    except OSError:
        return rows
    for line in text.splitlines():
        if line.startswith(INDEX_HEADER_PREFIX):
            header = line.split("\t")
            continue
        if not header or "\t" not in line:
            continue
        parts = line.split("\t")
        if len(parts) == len(header):
            rows.append(dict(zip(header, parts)))
    return rows


def load_recall_hits(path: Path) -> Counter:
    """path → number of times it was served, from the recall-hit ledger."""
    hits: Counter = Counter()
    try:
        text = path.read_text(encoding="utf-8")
    except OSError:
        return hits
    for line in text.splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            row = json.loads(line)
        except json.JSONDecodeError:
            continue
        page = str(row.get("path") or "").strip()
        if page:
            hits[page] += 1
    return hits


def calibrate(wiki: Path | None = None) -> Calibration:
    root = wiki or wiki_dir()
    rows = parse_index(root / "index.md")
    hits = load_recall_hits(root / ".recall-hits.jsonl")
    out = Calibration(pages=len(rows), hit_paths=len(hits))
    for row in rows:
        band = (row.get("confidence") or "").strip().lower() or UNSET_BAND
        stat = out.bands.setdefault(band, BandStat())
        stat.pages += 1
        page_hits = hits.get(row.get("path", ""), 0)
        if page_hits:
            stat.recalled += 1
            stat.hits += page_hits
    return out


def render(cal: Calibration) -> str:
    lines = [
        f"DENEB_WIKI_CONFIDENCE_CALIBRATION pages={cal.pages} "
        f"recalledPaths={cal.hit_paths} verdict={cal.verdict} [advisory]"
    ]
    for band in (*BAND_ORDER, UNSET_BAND):
        stat = cal.bands.get(band)
        if not stat or stat.rate is None:
            continue
        lines.append(
            f"  {band:<7} recalled {stat.recalled}/{stat.pages} = {stat.rate:.3f} (hits={stat.hits})"
        )
    if cal.verdict == "inverted":
        lines.append(
            "  INVERTED: self-declared confidence anti-predicts recall — the "
            "dream-quality Confidence axis is rewarding the wrong thing "
            "(arXiv:2608.04570 self-monitoring inversion)"
        )
    elif cal.verdict == "flat":
        lines.append(
            "  FLAT: self-declared confidence carries no recall signal — the "
            "Confidence axis is measuring nothing"
        )
    return "\n".join(lines)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="wiki confidence → recall calibration (advisory)")
    parser.add_argument("--wiki", type=Path, default=None, help="wiki dir (default ~/.deneb/wiki)")
    parser.add_argument("--json", action="store_true", help="machine-readable output")
    args = parser.parse_args(argv)

    cal = calibrate(args.wiki)
    if args.json:
        print(
            json.dumps(
                {
                    "pages": cal.pages,
                    "recalledPaths": cal.hit_paths,
                    "verdict": cal.verdict,
                    "bands": {
                        band: {
                            "pages": stat.pages,
                            "recalled": stat.recalled,
                            "hits": stat.hits,
                            "rate": stat.rate,
                        }
                        for band, stat in sorted(cal.bands.items())
                    },
                },
                ensure_ascii=False,
                indent=2,
            )
        )
    else:
        print(render(cal))
    return 0
