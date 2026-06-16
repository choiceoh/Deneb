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
it is `go:embed`-ed and parsed at init). It is derived from the Unicode Character
Database (Unihan) `kHangul` field, taking the first (dominant) reading per char.

```bash
# 1. Fetch Unihan (≈8.5 MB) and extract the readings file.
curl -sSL -o /tmp/Unihan.zip https://www.unicode.org/Public/UCD/latest/ucd/Unihan.zip
unzip -o /tmp/Unihan.zip Unihan_Readings.txt -d /tmp

# 2. Project kHangul → "<hex codepoint>\t<first Hangul syllable>" (strip :source).
awk -F'\t' '/\tkHangul\t/ {cp=$1; sub(/^U\+/,"",cp); split($3,a," "); r=a[1]; sub(/:.*/,"",r); print cp"\t"r}' \
  /tmp/Unihan_Readings.txt > /tmp/body.tsv

# 3. Re-attach the provenance header (keep the existing one in readings.tsv),
#    then append /tmp/body.tsv. Update the "Unicode Version" line to match.
```

Unicode data is under the Unicode License
(<https://www.unicode.org/terms_of_use.html>); attribution is in the
`readings.tsv` header.
