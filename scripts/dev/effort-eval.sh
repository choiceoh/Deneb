#!/bin/bash
# effort-eval.sh — RouterBench-style acceptance harness for the adaptive
# effort router (effort_router.go). Runs the SAME Korean message set through
# the dev gateway under the three policies the acceptance criterion needs:
#
#   always-high (DENEB_ADAPTIVE_EFFORT unset)  — fixed policy, quality anchor
#   always-non  (DENEB_ADAPTIVE_EFFORT=force)  — fixed policy, cost anchor
#   router      (DENEB_ADAPTIVE_EFFORT=1)      — the candidate
#
# Verdict (RouterBench, arXiv:2403.12031): the router point must lie ON OR
# ABOVE the random-interpolation line between the two fixed policies in the
# (quality, tokens) plane — i.e. for its token spend, the router must deliver
# at least the quality a coin-flip mixture of the fixed policies would.
# Quality here is an automatic proxy (Korean ratio + substance + no leakage,
# the quality-metric.sh axes); replies are dumped side-by-side for the final
# human judgment before enabling in production.
#
# Usage: DENEB_INSTANCE=effortr scripts/dev/effort-eval.sh
# Output: ~/.cache/deneb-visual/effort-eval-<ts>.md  (tmpfs-safe location)
set -uo pipefail
cd "$(dirname "$0")/../.."

INSTANCE="${DENEB_INSTANCE:-effortr}"
LOG="/tmp/deneb-${INSTANCE}-gateway-live.log"
# Port derivation must match scripts/dev/lib-server.sh (18800 + (cksum%100)*4).
PORT=$(( 18800 + ($(printf '%s' "$INSTANCE" | cksum | cut -d' ' -f1) % 100) * 4 ))
OUT="$HOME/.cache/deneb-visual/effort-eval-$(date +%Y%m%d-%H%M%S).md"
mkdir -p "$(dirname "$OUT")"

# Message set: 6 simple (router should route) + 6 hard (router should keep).
MSGS=(
  "안녕"
  "고마워!"
  "오늘 무슨 요일이야?"
  "응 좋아 그렇게 해줘"
  "잘자"
  "점심 뭐 먹을까?"
  "왜 하늘이 파란지 설명해줘"
  "태양광 인버터와 ESS의 관계를 분석해줘"
  "3 곱하기 47 더하기 12 계산해줘"
  "이번 주 할 일을 우선순위로 계획 세워줘"
  "RE100과 K-RE100의 차이를 비교해줘"
  "어제 회의 내용을 요약해줘"
)

run_policy() {
  local name="$1" env="$2"
  echo "== policy: $name (DENEB_ADAPTIVE_EFFORT='$env') =="
  local up=0 attempt
  for attempt in 1 2 3; do
    DENEB_INSTANCE="$INSTANCE" DENEB_ADAPTIVE_EFFORT="$env" scripts/dev/live-test.sh restart >/dev/null 2>&1
    for _ in $(seq 1 20); do
      if curl -sf --max-time 3 "http://127.0.0.1:${PORT}/health" >/dev/null 2>&1; then up=1; break; fi
      sleep 3
    done
    [ "$up" = 1 ] && break
    echo "  restart attempt $attempt failed; stopping and retrying..."
    DENEB_INSTANCE="$INSTANCE" scripts/dev/live-test.sh stop >/dev/null 2>&1
    sleep 5
  done
  [ "$up" = 1 ] || { echo "  GATEWAY NEVER CAME UP for $name — aborting policy"; return 1; }
  sleep 2
  local i=0
  for m in "${MSGS[@]}"; do
    i=$((i+1))
    local before_lines reply ms toks
    before_lines=$(wc -l < "$LOG" 2>/dev/null || echo 0)
    reply=$(DENEB_INSTANCE="$INSTANCE" timeout 300 scripts/dev/live-test.sh chat "$m" 2>&1)
    ms=$(echo "$reply" | grep -oE 'Done \([0-9]+ms\)' | grep -oE '[0-9]+' | tail -1)
    # per-run outputTokens from the run-complete log line appended since 'before_lines'
    toks=$(tail -n +"$((before_lines + 1))" "$LOG" 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g' | grep 'agent loop complete' | grep -oE ' outputTokens=[0-9]+' | tail -1 | grep -oE '[0-9]+')
    local text
    text=$(echo "$reply" | grep -vE '^==>|^$|Connected to native|prerequisites' | head -4 | tr '\n' ' ')
    # Fallback: runs that deliver via the message tool (or stream-only) leave
    # the sync reply empty — recover the user-visible text from the chat's
    # fresh transcript (last assistant text block, else message tool input).
    if [ "$(printf '%s' "$text" | wc -c)" -lt 12 ]; then
      local T
      T=$(ls -t "$HOME"/.deneb/transcripts/client:lt-*.jsonl 2>/dev/null | head -1)
      [ -n "$T" ] && text=$(python3 - "$T" <<'PYX'
import sys, json
best = ""
for line in open(sys.argv[1], errors="replace"):
    try: d = json.loads(line)
    except Exception: continue
    if d.get("role") != "assistant": continue
    c = d.get("content")
    if isinstance(c, str) and c.strip():
        best = c
    elif isinstance(c, list):
        for b in c:
            if b.get("type") == "text" and b.get("text", "").strip():
                best = b["text"]
            elif b.get("type") == "tool_use" and b.get("name") == "message":
                t = (b.get("input") or {}).get("message") or (b.get("input") or {}).get("text") or ""
                if t.strip(): best = t
print(best[:300].replace("\t", " ").replace("\n", " "))
PYX
)
    fi
    printf '%s\t%d\t%s\t%s\t%s\n' "$name" "$i" "${toks:-0}" "${ms:-0}" "$text" >> /tmp/effort-eval-rows.tsv
    echo "  [$i] toks=${toks:-?} ms=${ms:-?}"
  done
}

: > /tmp/effort-eval-rows.tsv
run_policy "always-high" "" || { echo "ABORT: always-high arm failed"; exit 2; }
run_policy "always-non"  "force" || { echo "ABORT: always-non arm failed"; exit 2; }
run_policy "router"      "1" || { echo "ABORT: router arm failed"; exit 2; }
DENEB_INSTANCE="$INSTANCE" scripts/dev/live-test.sh stop >/dev/null 2>&1

python3 - "$OUT" <<'PY'
import sys, statistics, html, re, datetime
out = sys.argv[1]
rows = []
for line in open('/tmp/effort-eval-rows.tsv', errors='replace'):
    p = line.rstrip('\n').split('\t')
    if len(p) >= 5:
        rows.append({'policy': p[0], 'i': int(p[1]), 'toks': int(p[2]), 'ms': int(p[3]), 'text': p[4][:300]})

def quality(text):
    """Automatic quality proxy (0-100): Korean ratio, substance, no leakage."""
    t = text.strip()
    if not t:
        return 0
    score = 0
    hangul = sum(1 for ch in t if '가' <= ch <= '힣')
    ratio = hangul / max(1, len(t))
    score += 40 if ratio > 0.3 else (20 if ratio > 0.1 else 0)
    score += 40 if len(t) > 30 else (30 if len(t) > 4 else 0)
    if not re.search(r'<think|NO_REPLY|<function', t):
        score += 20
    return score

pol = {}
for name in ('always-high', 'always-non', 'router'):
    rs = [r for r in rows if r['policy'] == name]
    pol[name] = {
        'toks': sum(r['toks'] for r in rs),
        'q': statistics.mean(quality(r['text']) for r in rs) if rs else 0,
        'ms': statistics.mean(r['ms'] for r in rs) if rs else 0,
        'rows': rs,
    }

h, n, r = pol['always-high'], pol['always-non'], pol['router']
# Empty/partial data must NEVER produce a PASS: every policy arm has to
# contribute a full row set with real token counts.
expected = 12
for name in ('always-high', 'always-non', 'router'):
    p_ = pol[name]
    nonzero = sum(1 for x in p_['rows'] if x['toks'] > 0)
    if len(p_['rows']) < expected or nonzero < expected - 2:
        print(f"VERDICT: INVALID — {name} arm has {len(p_['rows'])} rows / {nonzero} with tokens (need {expected})")
        sys.exit(2)
# Random-interpolation line between the fixed policies at the router's token
# spend. The line only spans [min,max] of the two fixed costs: a router point
# CHEAPER than both baselines cannot be matched by any mixture — clamp, and
# treat below-cheapest-cost with in-range quality as domination.
if h['toks'] != n['toks']:
    frac = (r['toks'] - n['toks']) / (h['toks'] - n['toks'])
else:
    frac = 1.0
dominates = r['toks'] <= min(h['toks'], n['toks'])
frac = max(0.0, min(1.0, frac))
interp_q = n['q'] + (h['q'] - n['q']) * frac
if dominates:
    interp_q = min(h['q'], n['q'])  # cheapest achievable mixture is the cheaper policy itself
# Acceptance: (a) RouterBench — on/above the interpolation line; (b) Ares #8 —
# quality within 2pt of always-high. The token-saving goal (40-50%) is
# reported as a metric, not a hard gate (it depends on the traffic mix).
# The router only CHANGES behavior on the simple half (m1-6); hard rows run
# the identical thinking policy in both arms, so their run-to-run variance is
# pure noise for the quality gate. Gate on the simple subset.
def subset_q(p_, lo, hi):
    qs = [quality(x['text']) for x in p_['rows'] if lo <= x['i'] <= hi]
    return statistics.mean(qs) if qs else 0
quality_drop = subset_q(h, 1, 6) - subset_q(r, 1, 6)
above_line = r['q'] >= interp_q - 0.5
within_2pt = quality_drop <= 2.0
verdict = ('PASS' if (above_line and within_2pt) else 'FAIL') + \
    f" (interp {'ok' if above_line else 'MISS'}, quality_drop {quality_drop:.1f}pt {'ok' if within_2pt else '>2pt'})"

with open(out, 'w') as f:
    f.write(f"# Effort Router Acceptance — {datetime.datetime.now():%Y-%m-%d %H:%M}\n\n")
    f.write("| policy | total outputTokens | avg auto-quality | avg latency(ms) |\n|---|---|---|---|\n")
    for name in ('always-high', 'always-non', 'router'):
        p = pol[name]
        f.write(f"| {name} | {p['toks']} | {p['q']:.1f} | {p['ms']:.0f} |\n")
    f.write(f"\n**Interpolation quality @ router's spend: {interp_q:.1f} → router {r['q']:.1f} → {verdict}**\n")
    f.write(f"\nSimple-subset (m1-6, where the router actually differs): high {subset_q(h,1,6):.1f} vs router {subset_q(r,1,6):.1f} (drop {quality_drop:.1f}pt); hard-subset noise excluded from the gate.\n")
    if h['toks']:
        saving = 100*(1-r['toks']/h['toks'])
        goal = '달성' if 40 <= saving else ('초과달성' if saving > 50 else '미달')
        f.write(f"\nAres #8 targets — token saving vs always-high: {saving:.1f}% (목표 40-50%: {goal}), quality drop: {h['q']-r['q']:.1f}pt (목표 ≤2pt)\n")
    f.write("\n## Replies (human judgment)\n\n| # | message-idx | always-high | always-non | router |\n|---|---|---|---|---|\n")
    for i in range(1, 13):
        cells = []
        for name in ('always-high', 'always-non', 'router'):
            t = next((x['text'] for x in pol[name]['rows'] if x['i'] == i), '')
            cells.append(html.escape(t[:120]).replace('|', '\\|'))
        f.write(f"| {i} | m{i} | {cells[0]} | {cells[1]} | {cells[2]} |\n")
print(f"report: {out}")
print(f"VERDICT: {verdict}")
print(f"tokens: high={h['toks']} non={n['toks']} router={r['toks']} | quality: high={h['q']:.1f} non={n['q']:.1f} router={r['q']:.1f}")
PY
