#!/usr/bin/env bash
# topology-parity.sh — check the runbooks' verifiable infrastructure claims
# against reality, so out-of-band host changes can't silently rot the docs.
#
# WHY: the 2026-07-06 srv4 unification left three silent divergences behind
# (split srv4 runners, missing APK signing material → debug-signed OTA builds,
# a zombie wormhole on srv1). Each took hours of ssh archaeology to DISCOVER
# and minutes to fix. This sweep makes the discovery free: every claim cites
# the doc that makes it, so a FAIL pinpoints both the drifted host state and
# the doc line to reconcile ("호스트를 고치거나 문서를 고치거나").
#
# Runs ON srv4 (the gateway/runner host — srv1 checks hop over ssh, srv2 and
# the public tunnel over HTTP). Daily via .github/workflows/topology-parity.yml
# (FAIL → doc-drift issue; back-to-green → auto-close). Run manually anytime:
#   ssh srv1 "ssh srv4 'cd ~/deneb && scripts/audit/topology-parity.sh'"
#
# Semantics — keep these uniform when adding checks:
#   FAIL = a doc claim is now false. Only FAILs fail the run / open the issue.
#   WARN = degraded-but-designed (sidecar fallback paths), flaky-external
#          (public tunnel), probe-not-possible (ssh unreachable), or hygiene
#          (rule-file bloat). Remote probes use an explicit token protocol
#          (remote echoes FOUND/NONE): an EMPTY result is a failed PROBE and
#          must become WARN+skip, never PASS or FAIL — the first live run
#          mis-reported a wormhole zombie because a missing srv4→srv1 key made
#          two empty probes resolve in opposite directions.
#
# Future evolution: absorb this as a gateway autonomous PeriodicTask so a FAIL
# lands as a workfeed card on the operator's phone (needs Go work — the miniapp
# surface has no "send notification" RPC today, only push.register/unregister).
set -uo pipefail

# systemctl --user needs XDG_RUNTIME_DIR to locate the user bus. This sweep runs
# on the self-hosted runner, which starts as a service with no login session, so
# XDG_RUNTIME_DIR is unset and `systemctl --user` dies with "Failed to connect to
# bus: No medium found" — every user-unit check (deneb-auto-deploy.timer,
# deneb-lmtp.socket) then false-FAILs even while the units are active. The runner
# runs as the unit owner, so point it at that user's runtime dir when absent.
: "${XDG_RUNTIME_DIR:=/run/user/$(id -u)}"
export XDG_RUNTIME_DIR

FAILS=0
WARNS=0

pass() { printf 'PASS  %s\n' "$1"; }
fail() {
    printf 'FAIL  %s\n      ↳ 문서: %s\n' "$1" "$2"
    FAILS=$((FAILS + 1))
}
warn() {
    printf 'WARN  %s\n' "$1"
    WARNS=$((WARNS + 1))
}

http_code() { curl -s -o /dev/null -m 8 -w '%{http_code}' "$1" 2>/dev/null; }
on_srv1() { ssh -o BatchMode=yes -o ConnectTimeout=8 srv1 "$1" 2>/dev/null; }

# check_http URL DESC DOC — claim: URL answers 200.
check_http() {
    if [ "$(http_code "$1")" = "200" ]; then pass "$2"; else fail "$2 — 응답 없음 ($1)" "$3"; fi
}

# check_unit UNIT DESC DOC — claim: the systemd user unit is active.
check_unit() {
    if systemctl --user is-active --quiet "$1"; then pass "$2"; else fail "$2 — $1 비활성" "$3"; fi
}

# check_srv1 REMOTE_PROBE BAD_TOKEN DESC_OK DESC_BAD DOC
# REMOTE_PROBE must echo exactly FOUND or NONE; BAD_TOKEN says which one fails.
check_srv1() {
    if [ "$SRV1_REACHABLE" != "1" ]; then
        warn "srv1 ssh 프로브 불가 — 스킵: $3"
        return
    fi
    local got
    got="$(on_srv1 "$1")"
    if [ "$got" = "$2" ]; then
        fail "$4" "$5"
    elif [ "$got" = "FOUND" ] || [ "$got" = "NONE" ]; then
        pass "$3"
    else
        warn "프로브 결과 불명(수동 확인): $3"
    fi
}

if [ "$(hostname -s)" != "srv4" ]; then
    echo "이 스크립트는 srv4(게이트웨이 호스트)에서 실행해야 합니다 — 로컬 서비스 주장을 localhost 로 검사합니다." >&2
    echo "예: ssh srv1 \"ssh srv4 'cd ~/deneb && scripts/audit/topology-parity.sh'\"" >&2
    exit 2
fi

SRV1_REACHABLE=0
[ "$(on_srv1 'echo OK')" = "OK" ] && SRV1_REACHABLE=1

echo "== 게이트웨이·배포 (docs/agent-rules/release-and-deploy.md) =="

check_http http://127.0.0.1:18789/health \
    "게이트웨이 /health 200 (srv4 로컬)" \
    "release-and-deploy.md '수동 배포' 검증 절차"
check_unit deneb-auto-deploy.timer \
    "auto-deploy 타이머 active (srv4-로컬 자동 배포)" \
    "release-and-deploy.md '자동 배포 (srv4-로컬)'"
check_unit deneb-lmtp.socket \
    "LMTP 소켓 활성화 유닛 active (핫스왑 중 메일 보호)" \
    "sidecar-models.md 메일 체인 / setup-lmtp-socket.sh"

# auto-deploy freshness: active timer ≠ successful deploys. Compare the prod
# checkout's HEAD to origin/main, tolerating the designed quiet period + one
# build cycle (~15 min) for a just-landed merge.
PROD=/home/choiceoh/deneb
if git -C "$PROD" fetch --quiet origin main 2>/dev/null; then
    head_sha="$(git -C "$PROD" rev-parse HEAD 2>/dev/null)"
    main_sha="$(git -C "$PROD" rev-parse origin/main 2>/dev/null)"
    if [ "$head_sha" = "$main_sha" ]; then
        pass "auto-deploy 신선도: 프로덕션 HEAD == origin/main"
    else
        main_age=$(($(date +%s) - $(git -C "$PROD" log -1 --format=%ct origin/main 2>/dev/null || echo 0)))
        if [ "$main_age" -lt 900 ]; then
            pass "auto-deploy 신선도: origin/main 이 ${main_age}초 전 갱신 — quiet period 내 정착 대기 중"
        else
            fail "프로덕션이 origin/main 보다 뒤처짐 (HEAD ${head_sha:0:9} ≠ main ${main_sha:0:9}, ${main_age}초 경과) — 타이머는 돌지만 배포가 실패 중" \
                "release-and-deploy.md '자동 배포' — /tmp/deneb-auto-deploy.log 확인"
        fi
    fi
else
    warn "프로덕션 체크아웃 fetch 실패 — auto-deploy 신선도 체크 스킵"
fi

echo "== APK 발행·OTA (release-and-deploy.md) =="

if [ -f "$HOME/.deneb/apk-signing.env" ] && [ -f "$HOME/.deneb/keys/deneb-release.p12" ]; then
    pass "APK release 서명 재료 존재 (env + keystore)"
else
    fail "APK 서명 재료 누락 — fossRelease 발행이 hard fail 한다 (2026-07-06 사고 재현 조건)" \
        "release-and-deploy.md 'APK release 서명' + 러너 이사 체크리스트"
fi

# End-to-end signature guard: the NEWEST published APK must carry the release
# cert. Catches every debug-signed-publish path (missing env was one; a gradle
# signingConfig regression would be another) at the artifact, not the process.
APK_DIR="$HOME/.cache/deneb-apk"
REF_CERT="$HOME/.deneb/keys/deneb-release-cert.sha256"
APKSIGNER="$(find "$HOME/android-sdk/build-tools" -name apksigner 2>/dev/null | sort -V | tail -1)"
latest_apk="$(ls -t "$APK_DIR"/*.apk 2>/dev/null | head -1)"
if [ -n "$latest_apk" ] && [ -n "$APKSIGNER" ] && [ -f "$REF_CERT" ]; then
    got_cert="$("$APKSIGNER" verify --print-certs "$latest_apk" 2>/dev/null | sed -n 's/.*SHA-256 digest: //p' | head -1)"
    if [ -n "$got_cert" ] && grep -q "$got_cert" "$REF_CERT"; then
        pass "최신 APK($(basename "$latest_apk")) 서명 == release 인증서"
    else
        fail "최신 APK($(basename "$latest_apk")) 서명이 release 인증서와 다름 — OTA '앱이 설치되지 않음' 재발 (got ${got_cert:0:12}…)" \
            "release-and-deploy.md 'APK release 서명' (2026-07-06 사고)"
    fi
else
    warn "APK 서명 실검증 스킵 (apk/apksigner/레퍼런스 해시 중 부재)"
fi

# OTA split-brain guard: version.json must advertise the highest versionCode
# actually present in the serve dir (builds 563~574 went undelivered when the
# manifest and the dir diverged across hosts).
if [ -f "$APK_DIR/version.json" ]; then
    adv_code="$(python3 -c "import json;print(json.load(open('$APK_DIR/version.json')).get('code',0))" 2>/dev/null)"
    max_code="$(ls "$APK_DIR" 2>/dev/null | sed -n 's/^deneb-\([0-9]*\)-.*\.apk$/\1/p' | sort -n | tail -1)"
    if [ -n "$adv_code" ] && [ "$adv_code" = "${max_code:-}" ]; then
        pass "OTA 매니페스트 일관성: version.json code=$adv_code == serve dir 최고 빌드"
    else
        fail "version.json code=${adv_code:-?} ≠ serve dir 최고 빌드 ${max_code:-?} — OTA split-brain (563~574 사고 클래스)" \
            "release-and-deploy.md 'Automated OTA publish'"
    fi
else
    fail "OTA version.json 부재 ($APK_DIR)" "release-and-deploy.md 'Native Client APK Publishing'"
fi

echo "== APK 러너 (release-and-deploy.md 'srv4 self-hosted 러너') =="

if systemctl list-units 'actions.runner.*' --no-legend 2>/dev/null | grep -q 'choiceoh-Deneb.srv4-apk.*running'; then
    pass "Deneb 러너 srv4-apk running (srv4)"
else
    fail "srv4-apk 러너 서비스가 running 이 아님" "release-and-deploy.md '현역 러너: srv4-apk'"
fi

check_srv1 '[ -d ~/actions-runner-deneb ] && echo FOUND || echo NONE' FOUND \
    "srv1 구 러너 잔재 없음" \
    "srv1 에 구 Deneb 러너 디렉토리 부활 — srv4 라벨 이중화(잡 분열) 위험" \
    "release-and-deploy.md (구 gx10-apk 는 해제·삭제됨 — legacy gx10-apk)"

echo "== wormhole (docs/agent-rules/sidecar-models.md) =="

check_http http://127.0.0.1:18800/health \
    "wormhole /health 200 (srv4 로컬 — 게이트웨이가 127.0.0.1:18800 소비)" \
    "sidecar-models.md '상주 호스트 = srv4'"

check_srv1 'ss -ltn 2>/dev/null | grep -q ":18800 " && echo FOUND || echo NONE' FOUND \
    "srv1 에 wormhole 좀비 없음" \
    "srv1 :18800 리스너 존재 — wormhole 이중 기동(설정 드리프트 위험)" \
    "sidecar-models.md (srv1 구 인스턴스는 disable 됨)"

echo "== GPU 보조 사이드카 (sidecar-models.md 호스트 배치표) =="

# Sidecar outages degrade by design (tesseract fallback / clean error), so
# these are WARN — real, but not doc drift.
[ "$(http_code http://100.105.145.6:18011/health)" = "200" ] \
    && pass "PaddleOCR-VL @srv1:18011" \
    || warn "PaddleOCR-VL @srv1:18011 무응답 — OCR 은 tesseract 폴백으로 저하 동작 (sidecar-models.md)"
[ "$(http_code http://100.105.145.6:18013/health)" = "200" ] \
    && pass "VibeVoice-ASR @srv1:18013" \
    || warn "VibeVoice-ASR @srv1:18013 무응답 — 오디오 전사가 명확한 에러로 실패 (sidecar-models.md)"
[ "$(http_code http://100.125.220.117:8000/health)" = "200" ] \
    && pass "dsv4 엔진 @srv2:8000" \
    || warn "dsv4 엔진 @srv2:8000 무응답 — 로컬 역할·폴백 체인 영향 (sidecar-models.md·model-roles.md)"
[ "$(http_code http://100.105.145.6:8000/health)" = "200" ] \
    && pass "qwen3.6 엔진 @srv1:8000" \
    || warn "qwen3.6 엔진 @srv1:8000 무응답 — dsv4 폴백·로컬 역할 영향 (sidecar-models.md·model-roles.md)"

echo "== 메일 엣지 (sidecar-models.md 호스트 배치표 — srv4 메일서버) =="

# The mail edge dies quietly: wblock keeps retrying, the operator just sees
# "메일이 안 들어온다" hours later (2026-07-06 12:10 incident — go-smtp line
# limit rejected every DATA). Probe the edge AND count recent rejects.
if docker ps --format '{{.Names}}' 2>/dev/null | grep -qx deneb-mailserver; then
    pass "deneb-mailserver 컨테이너 running"
else
    fail "deneb-mailserver 컨테이너가 없다 — 전달 메일 인입 전면 중단" "sidecar-models.md 호스트 배치표 (srv4=메일서버)"
fi
if printf 'QUIT\r\n' | timeout 5 nc 127.0.0.1 25 2>/dev/null | grep -q '^220'; then
    pass "SMTP 엣지 :25 응답 (maddy)"
else
    fail "SMTP :25 무응답 — wblock 전달이 큐에서 썩는 중" "sidecar-models.md 메일 체인"
fi
data_err="$(docker logs --since 24h deneb-mailserver 2>&1 | grep -c 'DATA error' || true)"
arch_err="$(docker logs --since 24h deneb-mailarchive 2>&1 | grep -c 'DATA error' || true)"
if [ "$((${data_err:-0} + ${arch_err:-0}))" -eq 0 ]; then
    pass "maddy DATA 거부 없음 (24h — 인입/아카이브 양쪽)"
else
    fail "maddy DATA 거부 인입=${data_err:-0} 아카이브=${arch_err:-0} (24h) — 메일이 엣지에서 죽고 있다 (2026-07-06 사고: smtp_max_line_length, 두 인스턴스 모두)" \
        "$HOME/mailserver/maddy.conf + $HOME/mailserver/archive/maddy.conf (각 smtp/lmtp 블록) + sidecar-models.md"
fi

echo "== 외부 표면 =="

# Public tunnel (remote app access). WARN, not FAIL: an external-network blip
# shouldn't flap the doc-drift issue.
[ "$(http_code https://deneb.topworks.ltd/health)" = "200" ] \
    && pass "공개 터널 deneb.topworks.ltd → 게이트웨이 (cloudflared)" \
    || warn "공개 터널 deneb.topworks.ltd 무응답 — 원격(모바일 데이터) 앱 접속 불가 상태일 수 있음 (cloudflared@srv4 확인)"

echo "== 룰 문서 비대화 워치독 (docs/agent-rules/README.md 큐레이션 규약) =="

while IFS= read -r line; do
    size="${line%% *}"
    f="${line#* }"
    if [ "$size" -gt 20480 ]; then
        warn "$(basename "$f") ${size}B > 20KB — 사고 서사를 '교훈 한 줄 + 보관'으로 접는 다이어트 검토"
    fi
done < <(find "$PROD/docs/agent-rules" -name '*.md' -exec wc -c {} \; 2>/dev/null | awk '{print $1" "$2}')

echo
echo "== 결과: FAIL=$FAILS WARN=$WARNS =="
echo "PARITY_RESULT fail=$FAILS warn=$WARNS"
if [ "$FAILS" -gt 0 ]; then
    echo "FAIL 항목은 '호스트를 고치거나 인용된 문서를 고치거나' 둘 중 하나다 — 방치가 유일한 오답." >&2
    exit 1
fi
exit 0
