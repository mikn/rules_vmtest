#!/bin/bash
# Setup a bridge with TAP devices for vmtest multi-VM networking.
# Run once with sudo. Tests then run unprivileged.
#
# Linux: creates bridge + TAP pool
# macOS: signs QEMU with vmnet entitlement
set -euo pipefail

BRIDGE="${1:-mltt-br0}"
SUBNET="${2:-10.0.0.1/24}"
NUM_TAPS="${3:-8}"

case "$(uname -s)" in
Linux)
    ip link add "$BRIDGE" type bridge
    ip addr add "$SUBNET" dev "$BRIDGE"
    ip link set "$BRIDGE" up

    for i in $(seq 0 $((NUM_TAPS - 1))); do
        TAP="mltt-tap${i}"
        ip tuntap add dev "$TAP" mode tap user "$(id -u)"
        ip link set "$TAP" master "$BRIDGE"
        ip link set "$TAP" up
    done
    echo "Bridge $BRIDGE ready with $NUM_TAPS TAP devices"
    ;;
Darwin)
    QEMU_PATH="${1:-$(which qemu-system-x86_64)}"
    SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
    codesign -s - --entitlements "${SCRIPT_DIR}/vmnet.entitlements" --force "$QEMU_PATH"
    echo "QEMU signed with vmnet entitlement: $QEMU_PATH"
    ;;
*)
    echo "Unsupported platform: $(uname -s)" >&2
    exit 1
    ;;
esac
