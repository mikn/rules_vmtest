#!/bin/bash
# Teardown bridge and TAP devices created by setup-bridge.sh.
# Run with sudo.
set -euo pipefail

BRIDGE="${1:-mltt-br0}"

case "$(uname -s)" in
Linux)
    # Remove TAP devices attached to the bridge
    if [ -d "/sys/class/net/${BRIDGE}/brif" ]; then
        for iface in /sys/class/net/"${BRIDGE}"/brif/mltt-tap*; do
            TAP="$(basename "$iface")"
            ip link set "$TAP" down 2>/dev/null || true
            ip tuntap del dev "$TAP" mode tap 2>/dev/null || true
        done
    fi

    # Remove the bridge
    ip link set "$BRIDGE" down 2>/dev/null || true
    ip link del "$BRIDGE" 2>/dev/null || true
    echo "Bridge $BRIDGE removed"
    ;;
Darwin)
    echo "macOS: no bridge teardown needed (vmnet is managed by the framework)"
    ;;
*)
    echo "Unsupported platform: $(uname -s)" >&2
    exit 1
    ;;
esac
