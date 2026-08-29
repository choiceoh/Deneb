#!/usr/bin/env bash
# spark-clock-cap.sh — install GPU 1600 MHz + CPU 2.808 GHz caps on a Spark.
#
# Why: nvidia-smi -lgc and cpufreq scaling_max_freq both reset on reboot.
# Host-only units since 2026-07-14 were the live policy with no repo copy, so
# a reimage silently returned to 2418 MHz / 3.9 GHz. This script is the
# source of truth: copy the systemd units, enable them, apply now.
#
# Usage:
#   scripts/deploy/spark-clock-cap.sh            # this host
#   scripts/deploy/spark-clock-cap.sh --fleet    # srv4 + srv1 + srv2 + srv3 (fabric hop)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
UNIT_DIR="$(cd "$SCRIPT_DIR/../systemd" && pwd)"
GPU_UNIT="$UNIT_DIR/gpu-clock-cap.service"
CPU_UNIT="$UNIT_DIR/cpu-clock-cap.service"
SRV3_FABRIC="${SPARK_SRV3:-choiceoh@10.10.10.3}"

install_from_dir() {
    local dir="$1"
    sudo install -m 0644 "$dir/gpu-clock-cap.service" /etc/systemd/system/gpu-clock-cap.service
    sudo install -m 0644 "$dir/cpu-clock-cap.service" /etc/systemd/system/cpu-clock-cap.service
    sudo systemctl daemon-reload
    sudo systemctl enable --now gpu-clock-cap.service cpu-clock-cap.service
    echo "spark-clock-cap: $(hostname) gpu=$(nvidia-smi --query-gpu=clocks.current.graphics --format=csv,noheader) cpu5=$(cat /sys/devices/system/cpu/cpu5/cpufreq/scaling_max_freq)"
}

install_local() {
    [[ -f "$GPU_UNIT" && -f "$CPU_UNIT" ]] || {
        echo "spark-clock-cap: missing unit files in $UNIT_DIR" >&2
        return 1
    }
    install_from_dir "$UNIT_DIR"
}

# Stream the two unit files over ssh, install, enable.
# Remote script is single-quoted so $(hostname) expands on the target.
REMOTE_INSTALL='tmp="$(mktemp -d)"; trap "rm -rf $tmp" EXIT; tar -C "$tmp" -xzf -; sudo install -m 0644 "$tmp/gpu-clock-cap.service" /etc/systemd/system/gpu-clock-cap.service; sudo install -m 0644 "$tmp/cpu-clock-cap.service" /etc/systemd/system/cpu-clock-cap.service; sudo systemctl daemon-reload; sudo systemctl enable --now gpu-clock-cap.service cpu-clock-cap.service; echo "spark-clock-cap: $(hostname) gpu=$(nvidia-smi --query-gpu=clocks.current.graphics --format=csv,noheader) cpu5=$(cat /sys/devices/system/cpu/cpu5/cpufreq/scaling_max_freq)"'

install_remote() {
    local hop="$1" dest="$2"
    if [[ -n "$hop" ]]; then
        tar -C "$UNIT_DIR" -czf - gpu-clock-cap.service cpu-clock-cap.service |
            ssh -o BatchMode=yes -o ConnectTimeout=10 "$hop" \
                "ssh -o BatchMode=yes -o ConnectTimeout=10 $dest '${REMOTE_INSTALL}'"
    else
        tar -C "$UNIT_DIR" -czf - gpu-clock-cap.service cpu-clock-cap.service |
            ssh -o BatchMode=yes -o ConnectTimeout=10 "$dest" "${REMOTE_INSTALL}"
    fi
}

if [[ "${1:-}" == "--fleet" ]]; then
    install_local
    install_remote "" srv1
    install_remote "" srv2
    install_remote srv2 "$SRV3_FABRIC"
    exit 0
fi
install_local
