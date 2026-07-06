#!/usr/bin/env bash
# topology-parity.sh — check the runbooks' verifiable infrastructure claims
# against reality, so out-of-band host changes can't silently rot the docs.
#
# WHY: the 2026-07-06 srv4 unification left three silent divergences behind
# (split gx10 runners, missing APK signing material → debug-signed OTA builds,
# a zombie wormhole on srv1). Each took hours of ssh archaeology to DISCOVER
# and minutes to fix. This script makes the discovery free: every claim below
# cites the doc that makes it, so a FAIL pinpoints both the drifted host state
# and the doc line to reconcile.
#
# Runs ON srv4 (the gateway/runner host — srv1 checks hop over ssh, srv2 over
# HTTP). Weekly via .github/workflows/topology-parity.yml, which opens/updates
# a `doc-drift` issue on failure. Run manually anytime:
#   ssh srv1 "ssh srv4 'cd ~/deneb && scripts/audit/topology-parity.sh'"
#
# Conventions: FAIL = a doc claim is now false (fix host or doc). WARN = soft
# signal (rule-file bloat, optional probe unreachable). Only FAILs fail the run.
set -uo pipefail

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

# Probe helpers — never abort the sweep on a single failed command.
# Remote probes use an explicit token protocol (the remote echoes FOUND/NONE):
# an EMPTY result means the probe itself failed (ssh/key/network), which must
# read as WARN+skip, never as PASS or FAIL — the first live run of this script
# mis-reported a wormhole zombie because a missing srv4→srv1 key made the two
# srv1 checks silently resolve in opposite directions.
http_ok() { curl -s -o /dev/null -m 8 -w '%{http_code}' "$1" 2>/dev/null; }
on_srv1() { ssh -o BatchMode=yes -o ConnectTimeout=8 srv1 "$1" 2>/dev/null; }

SRV1_REACHABLE=0
if [ "$(on_srv1 'echo OK')" = "OK" ]; then
    SRV1_REACHABLE=1
fi

if [ "$(hostname -s)" != "srv4" ]; then
    echo "이 스크립트는 srv4(게이트웨이 호스트)에서 실행해야 합니다 — 로컬 서비스 주장을 localhost 로 검사합니다." >&2
    echo "예: ssh srv1 \"ssh srv4 'cd ~/deneb && scripts/audit/topology-parity.sh'\"" >&2
    exit 2
fi

echo "== 게이트웨이·배포 (docs/agent-rules/release-and-deploy.md) =="

if [ "$(http_ok http://127.0.0.1:18789/health)" = "200" ]; then
    pass "게이트웨이 /health 200 (srv4 로컬)"
else
    fail "게이트웨이 /health 응답 없음 (127.0.0.1:18789)" "release-and-deploy.md '수동 배포' 검증 절차"
fi

if systemctl --user is-active --quiet deneb-auto-deploy.timer; then
    pass "auto-deploy 타이머 active (srv4-로컬 자동 배포)"
else
    fail "deneb-auto-deploy.timer 비활성 — 머지가 프로덕션에 배포되지 않는다" "release-and-deploy.md '자동 배포 (srv4-로컬)'"
fi

if systemctl --user is-active --quiet deneb-lmtp.socket; then
    pass "LMTP 소켓 활성화 유닛 active (핫스왑 중 메일 보호)"
else
    fail "deneb-lmtp.socket 비활성 — 재시작 창에 메일 유실 가능 (2026-06-16 사고 조건)" "sidecar-models.md 메일 체인 / setup-lmtp-socket.sh"
fi

if [ -f "$HOME/.deneb/apk-signing.env" ] && [ -f "$HOME/.deneb/keys/deneb-release.p12" ]; then
    pass "APK release 서명 재료 존재 (env + keystore)"
else
    fail "APK 서명 재료 누락 — fossRelease 발행이 hard fail 한다 (2026-07-06 사고 재현 조건)" "release-and-deploy.md 'APK release 서명' + 러너 이사 체크리스트"
fi

echo "== APK 러너 (release-and-deploy.md 'srv4 self-hosted 러너') =="

if systemctl list-units 'actions.runner.*' --no-legend 2>/dev/null | grep -q 'choiceoh-Deneb.srv4-apk.*running'; then
    pass "Deneb 러너 srv4-apk running (srv4)"
else
    fail "srv4-apk 러너 서비스가 running 이 아님" "release-and-deploy.md '현역 러너: srv4-apk'"
fi

if [ "$SRV1_REACHABLE" != "1" ]; then
    warn "srv1 ssh 프로브 불가 (srv4→srv1 BatchMode 키 확인) — srv1 관련 체크 스킵"
else
    case "$(on_srv1 '[ -d ~/actions-runner-deneb ] && echo FOUND || echo NONE')" in
    NONE) pass "srv1 구 러너 잔재 없음" ;;
    FOUND) fail "srv1 에 구 Deneb 러너 디렉토리 부활 — gx10 라벨 이중화(잡 분열) 위험" "release-and-deploy.md (구 gx10-apk 는 해제·삭제됨)" ;;
    *) warn "srv1 러너 잔재 프로브 결과 불명 — 수동 확인 필요" ;;
    esac
fi

echo "== wormhole (docs/agent-rules/sidecar-models.md) =="

if [ "$(http_ok http://127.0.0.1:18800/health)" = "200" ]; then
    pass "wormhole /health 200 (srv4 로컬 — 게이트웨이가 127.0.0.1:18800 소비)"
else
    fail "wormhole 이 srv4 로컬에서 응답하지 않음 — 전 모델 트래픽 단일 관문 다운" "sidecar-models.md '상주 호스트 = srv4'"
fi

if [ "$SRV1_REACHABLE" != "1" ]; then
    warn "srv1 ssh 프로브 불가 — wormhole 좀비 체크 스킵"
else
    case "$(on_srv1 'ss -ltn 2>/dev/null | grep -q ":18800 " && echo LISTEN || echo NONE')" in
    NONE) pass "srv1 에 wormhole 좀비 없음" ;;
    LISTEN) fail "srv1 :18800 리스너 존재 — wormhole 이중 기동(설정 드리프트 위험)" "sidecar-models.md (srv1 구 인스턴스는 disable 됨)" ;;
    *) warn "srv1 wormhole 좀비 프로브 결과 불명 — 수동 확인 필요" ;;
    esac
fi

echo "== GPU 보조 사이드카 (sidecar-models.md 호스트 배치표) =="

if [ "$(http_ok http://100.105.145.6:18011/health)" = "200" ]; then
    pass "PaddleOCR-VL @srv1:18011 (게이트웨이 DENEB_OCR_VL_URL 대상)"
else
    warn "PaddleOCR-VL @srv1:18011 무응답 — OCR 은 tesseract 폴백으로 저하 동작 (sidecar-models.md)"
fi

if [ "$(http_ok http://100.105.145.6:18013/health)" = "200" ]; then
    pass "VibeVoice-ASR @srv1:18013 (게이트웨이 DENEB_ASR_URL 대상)"
else
    warn "VibeVoice-ASR @srv1:18013 무응답 — 오디오 캡처 전사가 명확한 에러로 실패 (sidecar-models.md)"
fi

if [ "$(http_ok http://100.125.220.117:8000/health)" = "200" ]; then
    pass "dsv4 엔진 @srv2:8000 (fallback·lightweight/tiny 역할 대상)"
else
    warn "dsv4 엔진 @srv2:8000 무응답 — 로컬 역할·폴백 체인 영향 (sidecar-models.md·model-roles.md)"
fi

echo "== 룰 문서 비대화 워치독 (docs/agent-rules/README.md 큐레이션 규약) =="

while IFS= read -r line; do
    size="${line%% *}"
    f="${line#* }"
    if [ "$size" -gt 20480 ]; then
        warn "$(basename "$f") ${size}B > 20KB — 사고 서사를 '교훈 한 줄 + 보관'으로 접는 다이어트 검토"
    fi
done < <(find "$HOME/deneb/docs/agent-rules" -name '*.md' -exec wc -c {} \; 2>/dev/null | awk '{print $1" "$2}')

echo
echo "== 결과: FAIL=$FAILS WARN=$WARNS =="
if [ "$FAILS" -gt 0 ]; then
    echo "FAIL 항목은 '호스트를 고치거나 인용된 문서를 고치거나' 둘 중 하나다 — 방치가 유일한 오답." >&2
    exit 1
fi
exit 0
