# package hanja

Hanja→Hangul (한자→한글 독음) transliteration for user-facing model output.
Chinese-lineage models (GLM, MiMo, DeepSeek) sometimes write Sino-Korean
vocabulary in Hanja (報告書) instead of Hangul (보고서). This package converts the
reading deterministically — it is a per-character **reading lookup**, not
translation — so it needs no model and no sentence context, and is safe to apply
mid-stream (`Streamer`). It is **not** a Chinese→Korean translator: it reads
Hanja as Korean, it does not render actual Chinese sentences into Korean.

## API

- `Transliterate(s string) string` — whole-string convert (final/sync text).
- `NewStreamer()` + `Write(delta) / Flush()` — stream-safe convert (live deltas);
  shares logic with `Transliterate` so streamed and final text match.
- `ContainsHan(s string) bool` — cheap guard to skip the all-Korean common case.

Code fences (```` ``` ````), inline code (`` `…` ``), and Han with no known
reading pass through untouched. 두음법칙 (旅行→여행, 女子→여자) is applied at the
first Hanja of a consecutive run — correct for common compounds, but a
word-initial heuristic that can miss morpheme-internal cases (新女性→신녀성).

## Regenerating `readings.tsv`

The reading table is the **only** data input and is committed (no codegen step;
it is `go:embed`-ed and parsed at init). Two passes over the Unicode Character
Database (Unihan), keyed off `$HOME/Unihan_Readings.txt` and
`$HOME/Unihan_Variants.txt` (from `Unihan.zip`):

1. **Traditional/shared** — the `kHangul` field, preferring the standard South
   Korean reading (source flag `E`), else the first reading.
2. **Simplified** — for a Simplified char with no `kHangul` of its own, resolve
   `kTraditionalVariant` to its Traditional form and reuse that reading (时→時→시,
   发→發→발). This is why reading Chinese Sino-vocabulary as Korean works
   (时间→시간, 发生→발생).

```bash
curl -sSL -o ~/Unihan.zip https://www.unicode.org/Public/UCD/latest/ucd/Unihan.zip
unzip -o ~/Unihan.zip Unihan_Readings.txt Unihan_Variants.txt -d ~
# Then run the two-pass Python generator (see the script in the PR that added
# Simplified coverage) and write readings.tsv with the provenance header. Update
# the "Unicode 17.0.0" line to match the fetched version.
```

Pure-Chinese function words whose Korean reading is gibberish (所以→소이) are not
covered by readings; a small curated map handles them — see `phrases.go`.

Unicode data is under the Unicode License
(<https://www.unicode.org/terms_of_use.html>); attribution is in the
`readings.tsv` header.
