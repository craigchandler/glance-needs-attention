#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
    echo "Usage: sudo $0 /path/to/needs-attention-linux-amd64" >&2
    echo "Run from a release support bundle or a source checkout." >&2
    exit 1
fi
if [ "$(id -u)" -ne 0 ]; then
    echo "Run this installer as root (sudo)." >&2
    exit 1
fi

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PROJECT_DIR=$(dirname -- "$SCRIPT_DIR")
BINARY=$1
SERVICE_USER=needsattention
SERVICE_GROUP=needsattention
STATE_DIR=/var/lib/needs-attention
STAGED_BINARY=
cleanup() {
    if [ -n "$STAGED_BINARY" ]; then rm -f -- "$STAGED_BINARY"; fi
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

# Check inputs before making changes. Never execute a supplied binary as root.
for INPUT_FILE in "$BINARY" "$PROJECT_DIR/config.example.json" \
    "$PROJECT_DIR/.env.example" "$PROJECT_DIR/systemd/needs-attention.service"; do
    if [ ! -f "$INPUT_FILE" ] || [ ! -r "$INPUT_FILE" ]; then
        echo "Missing or unreadable input: $INPUT_FILE" >&2
        exit 1
    fi
done
if ! getent group "$SERVICE_GROUP" >/dev/null; then
    groupadd --system "$SERVICE_GROUP"
fi
if ! id "$SERVICE_USER" >/dev/null 2>&1; then
    useradd --system --gid "$SERVICE_GROUP" --home-dir "$STATE_DIR" \
        --shell /usr/bin/nologin "$SERVICE_USER"
fi
if [ "$(id -u "$SERVICE_USER")" -eq 0 ] || [ "$(getent group "$SERVICE_GROUP" | cut -d: -f3)" -eq 0 ]; then
    echo "Refusing to install with a root service identity." >&2
    exit 1
fi

install -d -m 0755 /usr/local/bin /etc/systemd/system
install -d -m 0700 -o "$SERVICE_USER" -g "$SERVICE_GROUP" "$STATE_DIR"
# Rename on the same filesystem allows upgrades while the old binary runs.
STAGED_BINARY=$(mktemp /usr/local/bin/.needs-attention.XXXXXX)
install -m 0755 -o root -g root -- "$BINARY" "$STAGED_BINARY"
mv -f -- "$STAGED_BINARY" /usr/local/bin/needs-attention
STAGED_BINARY=

# Preserve existing files, including symlinks, on every upgrade.
if [ ! -e /etc/needs-attention.json ] && [ ! -L /etc/needs-attention.json ]; then
    install -m 0640 -o root -g "$SERVICE_GROUP" \
        "$PROJECT_DIR/config.example.json" /etc/needs-attention.json
fi
if [ ! -e /etc/needs-attention.env ] && [ ! -L /etc/needs-attention.env ]; then
    install -m 0640 -o root -g "$SERVICE_GROUP" \
        "$PROJECT_DIR/.env.example" /etc/needs-attention.env
fi
install -m 0644 -o root -g root "$PROJECT_DIR/systemd/needs-attention.service" \
    /etc/systemd/system/needs-attention.service
systemctl daemon-reload
printf '%s\n' \
    'Installed. Existing configuration and environment files were preserved.' \
    'Ensure needsattention can read the configured JSON file.' \
    'Edit /etc/needs-attention.json, then: sudo systemctl enable --now needs-attention' \
    'For an upgrade: sudo systemctl restart needs-attention'
