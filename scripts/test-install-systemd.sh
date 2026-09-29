#!/usr/bin/env bash
# Exercise fresh installs and upgrades using temporary paths and mocked accounts/systemd.
set -euo pipefail
PROJECT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
TEST_DIR=$(mktemp -d)
trap 'rm -rf "$TEST_DIR"' EXIT
trap 'exit 1' HUP INT TERM
export TEST_ROOT="$TEST_DIR/root" MOCK_STATE="$TEST_DIR/mock-state"
mkdir -p "$TEST_DIR/bundle/scripts" "$TEST_DIR/bundle/systemd" "$TEST_DIR/mocks" "$MOCK_STATE"
cp "$PROJECT_DIR/config.example.json" "$PROJECT_DIR/.env.example" "$TEST_DIR/bundle/"
cp "$PROJECT_DIR/systemd/needs-attention.service" "$TEST_DIR/bundle/systemd/"
# Only the copied installer is redirected; the real installer has fixed host paths.
sed -e "s|/usr/local/bin|$TEST_ROOT/usr/local/bin|g" \
    -e "s|/etc/|$TEST_ROOT/etc/|g" \
    -e "s|/var/lib/needs-attention|$TEST_ROOT/var/lib/needs-attention|g" \
    "$PROJECT_DIR/scripts/install-systemd.sh" > "$TEST_DIR/bundle/scripts/install-systemd.sh"
cat > "$TEST_DIR/mocks/id" <<'MOCK'
#!/bin/sh
if [ "$#" -eq 1 ] && [ "$1" = -u ]; then echo 0; exit; fi
[ -f "$MOCK_STATE/user" ] || exit 1
[ "$1" != -u ] || echo 999
MOCK
cat > "$TEST_DIR/mocks/getent" <<'MOCK'
#!/bin/sh
[ -f "$MOCK_STATE/group" ] || exit 2
echo needsattention:x:999:
MOCK
cat > "$TEST_DIR/mocks/groupadd" <<'MOCK'
#!/bin/sh
echo "$*" >> "$MOCK_STATE/group-calls"
touch "$MOCK_STATE/group"
MOCK
cat > "$TEST_DIR/mocks/useradd" <<'MOCK'
#!/bin/sh
echo "$*" >> "$MOCK_STATE/user-calls"
touch "$MOCK_STATE/user"
MOCK
cat > "$TEST_DIR/mocks/systemctl" <<'MOCK'
#!/bin/sh
[ "$*" = daemon-reload ] || exit 1
echo reload >> "$MOCK_STATE/reloads"
MOCK
cat > "$TEST_DIR/mocks/install" <<'MOCK'
#!/usr/bin/env bash
set -eu
printf '%s\n' "$*" >> "$MOCK_STATE/install-calls"
args=()
while [ "$#" -gt 0 ]; do
    case "$1" in
        -o|-g) shift 2 ;; # Test user cannot change ownership; inspect calls below.
        *) args+=("$1"); shift ;;
    esac
done
exec /usr/bin/install "${args[@]}"
MOCK
chmod +x "$TEST_DIR/mocks/"*
export PATH="$TEST_DIR/mocks:$PATH"
INSTALLER="$TEST_DIR/bundle/scripts/install-systemd.sh"
printf 'first binary\n' > "$TEST_DIR/binary"
sh "$INSTALLER" "$TEST_DIR/binary" >/dev/null
cmp "$TEST_DIR/binary" "$TEST_ROOT/usr/local/bin/needs-attention"
cmp "$PROJECT_DIR/config.example.json" "$TEST_ROOT/etc/needs-attention.json"
cmp "$PROJECT_DIR/systemd/needs-attention.service" "$TEST_ROOT/etc/systemd/system/needs-attention.service"
test "$(stat -c %a "$TEST_ROOT/etc/needs-attention.json")" = 640
test "$(stat -c %a "$TEST_ROOT/var/lib/needs-attention")" = 700
grep -q -- '-m 0640 -o root -g needsattention' "$MOCK_STATE/install-calls"
grep -q -- '--system --gid needsattention' "$MOCK_STATE/user-calls"
printf 'local configuration\n' > "$TEST_ROOT/etc/needs-attention.json"
printf 'local environment\n' > "$TEST_ROOT/etc/needs-attention.env"
printf 'second binary\n' > "$TEST_DIR/binary"
sh "$INSTALLER" "$TEST_DIR/binary" >/dev/null
cmp "$TEST_DIR/binary" "$TEST_ROOT/usr/local/bin/needs-attention"
test "$(cat "$TEST_ROOT/etc/needs-attention.json")" = 'local configuration'
test "$(cat "$TEST_ROOT/etc/needs-attention.env")" = 'local environment'
test "$(wc -l < "$MOCK_STATE/user-calls")" -eq 1
test "$(wc -l < "$MOCK_STATE/group-calls")" -eq 1
# A dangling config symlink must be preserved, not followed or replaced.
rm "$TEST_ROOT/etc/needs-attention.json"
ln -s "$TEST_DIR/missing-config" "$TEST_ROOT/etc/needs-attention.json"
sh "$INSTALLER" "$TEST_DIR/binary" >/dev/null
test -L "$TEST_ROOT/etc/needs-attention.json"
test ! -e "$TEST_DIR/missing-config"
test "$(wc -l < "$MOCK_STATE/reloads")" -eq 3
if sh "$INSTALLER" "$TEST_DIR/missing-binary" >/dev/null 2>&1; then
    echo 'Installer accepted a missing binary' >&2; exit 1
fi
test "$(wc -l < "$MOCK_STATE/reloads")" -eq 3
test -z "$(find "$TEST_ROOT/usr/local/bin" -name '.needs-attention.*' -print)"
echo 'Installer fresh install, upgrade, preservation, cleanup and missing-input checks passed (mock host).'
