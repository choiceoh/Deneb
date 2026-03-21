#!/bin/bash
# ──────────────────────────────────────────────────────────────
# blue-green.sh — Deneb 게이트웨이 Blue-Green 배포
#
# 새 버전을 비활성 슬롯에 배포하고, 헬스체크를 통과하면
# 트래픽을 전환한 후 이전 버전을 종료/삭제합니다.
#
# 사용법:
#   ./deploy/blue-green.sh deploy <image>    # 새 이미지 배포
#   ./deploy/blue-green.sh status            # 현재 상태
#   ./deploy/blue-green.sh rollback          # 이전 버전으로 롤백
#   ./deploy/blue-green.sh cleanup           # 비활성 슬롯 정리
#
# 환경변수:
#   DENEB_IMAGE              배포할 Docker 이미지 (deploy 시 인수로도 가능)
#   DENEB_GATEWAY_PORT       외부 노출 포트 (기본: 18789)
#   BLUE_PORT                Blue 슬롯 직접 접근 포트 (기본: 18791)
#   GREEN_PORT               Green 슬롯 직접 접근 포트 (기본: 18792)
#   BG_HEALTH_RETRIES        헬스체크 재시도 횟수 (기본: 30)
#   BG_HEALTH_INTERVAL       헬스체크 간격 초 (기본: 5)
#   BG_DRAIN_WAIT            트래픽 전환 후 구버전 대기 시간 초 (기본: 30)
# ──────────────────────────────────────────────────────────────

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
COMPOSE_FILE="$SCRIPT_DIR/docker-compose.blue-green.yml"
NGINX_TEMPLATE="$SCRIPT_DIR/nginx-bg.conf.template"
NGINX_CONF="$SCRIPT_DIR/nginx-bg.conf"
STATE_FILE="$SCRIPT_DIR/.bg-state"

# 설정
BG_HEALTH_RETRIES="${BG_HEALTH_RETRIES:-30}"
BG_HEALTH_INTERVAL="${BG_HEALTH_INTERVAL:-5}"
BG_DRAIN_WAIT="${BG_DRAIN_WAIT:-30}"
BLUE_PORT="${BLUE_PORT:-18791}"
GREEN_PORT="${GREEN_PORT:-18792}"

# ── 유틸리티 ──

log()  { echo "[$(date '+%H:%M:%S')] $*"; }
ok()   { echo "[$(date '+%H:%M:%S')] ✅ $*"; }
warn() { echo "[$(date '+%H:%M:%S')] ⚠️  $*" >&2; }
fail() { echo "[$(date '+%H:%M:%S')] ❌ $*" >&2; exit 1; }

get_active_slot() {
    if [[ -f "$STATE_FILE" ]]; then
        cat "$STATE_FILE"
    else
        echo "blue"
    fi
}

get_inactive_slot() {
    local active
    active="$(get_active_slot)"
    if [[ "$active" == "blue" ]]; then
        echo "green"
    else
        echo "blue"
    fi
}

get_slot_port() {
    case "$1" in
        blue)  echo "$BLUE_PORT" ;;
        green) echo "$GREEN_PORT" ;;
        *)     fail "알 수 없는 슬롯: $1" ;;
    esac
}

# nginx 설정 생성 (활성 슬롯으로 라우팅)
generate_nginx_conf() {
    local slot="$1"
    sed "s/__ACTIVE_SLOT__/$slot/g" "$NGINX_TEMPLATE" > "$NGINX_CONF"
}

# nginx 리로드 (무중단)
reload_proxy() {
    docker compose -f "$COMPOSE_FILE" --project-directory "$PROJECT_DIR" \
        exec -T proxy nginx -s reload 2>/dev/null || \
    docker compose -f "$COMPOSE_FILE" --project-directory "$PROJECT_DIR" \
        restart proxy
}

# 컨테이너 헬스체크
wait_healthy() {
    local slot="$1"
    local port
    port="$(get_slot_port "$slot")"
    local url="http://127.0.0.1:${port}/healthz"

    log "헬스체크 시작: $slot (${url}, 최대 ${BG_HEALTH_RETRIES}회 × ${BG_HEALTH_INTERVAL}초)"

    for i in $(seq 1 "$BG_HEALTH_RETRIES"); do
        if curl -sf --max-time 5 "$url" > /dev/null 2>&1; then
            ok "헬스체크 통과: $slot (${i}/${BG_HEALTH_RETRIES})"
            return 0
        fi
        log "  대기 중... ($i/${BG_HEALTH_RETRIES})"
        sleep "$BG_HEALTH_INTERVAL"
    done

    warn "헬스체크 실패: $slot"
    return 1
}

# Docker Compose 명령 래퍼
dc() {
    docker compose -f "$COMPOSE_FILE" --project-directory "$PROJECT_DIR" "$@"
}

# ── 명령: status ──

cmd_status() {
    local active inactive
    active="$(get_active_slot)"
    inactive="$(get_inactive_slot)"

    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "  Deneb Blue-Green 배포 상태"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    echo "  활성 슬롯:   $active  ← 트래픽 수신 중"
    echo "  대기 슬롯:   $inactive"
    echo ""

    for slot in blue green; do
        local container="deneb-gateway-${slot}"
        local status
        status="$(docker inspect --format='{{.State.Status}} ({{.State.Health.Status}})' "$container" 2>/dev/null || echo "없음")"
        local image
        image="$(docker inspect --format='{{.Config.Image}}' "$container" 2>/dev/null || echo "-")"
        local marker=""
        [[ "$slot" == "$active" ]] && marker=" ◀ ACTIVE"
        echo "  ${slot}: ${status} [${image}]${marker}"
    done

    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
}

# ── 명령: deploy ──

cmd_deploy() {
    local new_image="${1:-${DENEB_IMAGE:-}}"
    [[ -z "$new_image" ]] && fail "사용법: $0 deploy <image>\n  예: $0 deploy deneb:v3.142"

    local active inactive
    active="$(get_active_slot)"
    inactive="$(get_inactive_slot)"

    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "  Blue-Green 배포 시작"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "  새 이미지:    $new_image"
    echo "  대상 슬롯:    $inactive (현재 비활성)"
    echo "  활성 슬롯:    $active (트래픽 유지)"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""

    # 1단계: 초기 nginx 설정 확인
    if [[ ! -f "$NGINX_CONF" ]]; then
        log "nginx 설정 초기화 (활성: $active)"
        generate_nginx_conf "$active"
    fi

    # 2단계: 비활성 슬롯에 새 이미지 배포
    log "1/5 비활성 슬롯($inactive)에 새 이미지 배포..."
    export DENEB_IMAGE="$new_image"
    dc up -d "gateway-${inactive}" proxy

    # 3단계: 새 버전 헬스체크
    log "2/5 새 버전 헬스체크..."
    if ! wait_healthy "$inactive"; then
        warn "새 버전 헬스체크 실패 → 비활성 슬롯 정리"
        dc stop "gateway-${inactive}"
        dc rm -f "gateway-${inactive}"
        fail "배포 중단: 새 버전이 정상 상태가 아닙니다"
    fi

    # 4단계: 기존 버전도 아직 살아있는지 확인
    log "3/5 기존 버전($active) 상태 확인..."
    if wait_healthy "$active"; then
        ok "기존 버전 정상 → 트래픽 전환 진행"
    else
        warn "기존 버전 비정상 → 즉시 전환"
    fi

    # 5단계: 트래픽 전환 (nginx 설정 변경 + 리로드)
    log "4/5 트래픽 전환: $active → $inactive"
    generate_nginx_conf "$inactive"
    reload_proxy
    echo "$inactive" > "$STATE_FILE"
    ok "트래픽 전환 완료 → 활성 슬롯: $inactive"

    # 6단계: 구버전 드레인 대기 후 종료
    log "5/5 구버전($active) 연결 드레인 대기 (${BG_DRAIN_WAIT}초)..."
    sleep "$BG_DRAIN_WAIT"

    log "구버전 컨테이너 종료 및 삭제..."
    dc stop "gateway-${active}"
    dc rm -f "gateway-${active}"

    echo ""
    ok "Blue-Green 배포 완료!"
    echo ""
    cmd_status
}

# ── 명령: rollback ──

cmd_rollback() {
    local active inactive
    active="$(get_active_slot)"
    inactive="$(get_inactive_slot)"

    log "롤백 시작: $active → $inactive"

    # 비활성 슬롯(이전 버전)이 실행 중인지 확인
    local container="deneb-gateway-${inactive}"
    if ! docker inspect "$container" > /dev/null 2>&1; then
        fail "롤백 불가: 이전 버전 컨테이너($container)가 존재하지 않습니다"
    fi

    # 이전 버전 시작 (정지 상태일 수 있음)
    dc up -d "gateway-${inactive}"

    # 이전 버전 헬스체크
    if ! wait_healthy "$inactive"; then
        fail "롤백 실패: 이전 버전($inactive)이 정상 상태가 아닙니다"
    fi

    # 트래픽 전환
    generate_nginx_conf "$inactive"
    reload_proxy
    echo "$inactive" > "$STATE_FILE"
    ok "롤백 완료 → 활성 슬롯: $inactive"

    # 현재(실패한) 버전 정리
    log "실패 버전($active) 정리..."
    dc stop "gateway-${active}"
    dc rm -f "gateway-${active}"

    cmd_status
}

# ── 명령: cleanup ──

cmd_cleanup() {
    local inactive
    inactive="$(get_inactive_slot)"
    log "비활성 슬롯($inactive) 정리..."
    dc stop "gateway-${inactive}" 2>/dev/null || true
    dc rm -f "gateway-${inactive}" 2>/dev/null || true
    ok "정리 완료"
}

# ── 메인 ──

case "${1:-help}" in
    deploy)   shift; cmd_deploy "$@" ;;
    status)   cmd_status ;;
    rollback) cmd_rollback ;;
    cleanup)  cmd_cleanup ;;
    help|*)
        echo "사용법: $0 <명령> [인수]"
        echo ""
        echo "명령:"
        echo "  deploy <image>   새 이미지를 비활성 슬롯에 배포하고 전환"
        echo "  status           현재 Blue-Green 상태 확인"
        echo "  rollback         이전 버전으로 롤백"
        echo "  cleanup          비활성 슬롯 정리"
        echo ""
        echo "예시:"
        echo "  $0 deploy deneb:v3.142"
        echo "  $0 deploy ghcr.io/deneb/deneb:latest"
        echo "  $0 status"
        echo "  $0 rollback"
        ;;
esac
