# Engine statistics merge evidence — 2026-09-17

Base: `59d41ec41` (`origin/main`, PR #5106). Branch: `codex/engine-statistics-merge-0917`.
Validation ran in the isolated srv4 checkout `/home/choiceoh/deneb-engine-statistics-merge-0917`.

- `GOMAXPROCS=4 ANDROID_HOME=/home/choiceoh/android-sdk CI_CHECK_BASE=59d41ec41 make ci`: all 18 gates pass, 126.7 seconds (`ci.log`).
- `pnpm install --frozen-lockfile`, then `pnpm verify` with pnpm 11.7.0: typecheck, lint, format, 125 test files / 2088 tests, production build pass (`andromeda-verify.log`). The install-generated service worker was restored before this final verification; dependency files are unchanged.
- `python3 scripts/dev/engine-statistics-smoke.py --output review/merge-smoke.json`: isolated loopback scrape, interval history and authenticated RPC pass (`synthetic-rpc.json`). The temporary gateway is stopped by the harness.
- Kotlin and TypeScript wire artifacts were regenerated from the integrated Go contract. All 35 changed source/generated/golden files matched byte-for-byte between the server and local commit tree after all checks; the SHA-256 manifest is retained locally at `/tmp/deneb-stats-merge-remote-sha256.json`.

The initial failures were corrected without suppressing assertions: hook tests isolate the separately-tested indexer; fallback role keys match the Go registry; the scene fixture supplies its known end time rather than depending on an absent ffprobe. Preview/inspection tasks pin the Korean locale. RSI's font-dependent disclosure glyph is now a vector. Only engine, new engine_statistics, and the intentional RSI vector change have updated goldens; remaining screens match.

This is functional and CPU-side validation, not an engine performance claim. Synthetic rates are scripted values. New diagnostics require deployment of ST PR #1116 plus the gateway/app; no production deployment or fleet/GPU run was performed for this merge.
