#!/usr/bin/env bash
set -euo pipefail

# Uninstall Canopy — removes the binary and optionally the data directory.
#
#   bash uninstall.sh
#   INSTALL_DIR=~/.local/bin bash uninstall.sh

INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
BINARY="canopy"
DATA_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/canopy"

die() {
    echo "error: $*" >&2
    exit 1
}

# Stop the daemon gracefully before removing the binary.
bin="${INSTALL_DIR}/${BINARY}"
if [ -x "$bin" ]; then
    "$bin" daemon stop 2>/dev/null && echo "Stopped canopy daemon." || true
fi

# Remove the binary.
if [ -f "$bin" ]; then
    rm -f "$bin"
    echo "Removed ${bin}"
else
    echo "Binary not found at ${bin}, skipping."
fi

# Offer to remove the data directory (sessions DB, socket, PID file).
if [ -d "$DATA_DIR" ]; then
    printf "Remove data directory %s? [y/N] " "$DATA_DIR"
    read -r answer </dev/tty
    case "$answer" in
        [yY]*)
            rm -rf "$DATA_DIR"
            echo "Removed ${DATA_DIR}"
            ;;
        *)
            echo "Kept ${DATA_DIR}"
            ;;
    esac
fi

# Remove the PATH export line from known shell profiles.
path_line="export PATH=\"${INSTALL_DIR}:\$PATH\""
for profile in "$HOME/.bashrc" "$HOME/.bash_profile" "${ZDOTDIR:-$HOME}/.zshrc"; do
    [ -f "$profile" ] || continue
    if grep -Fq "$path_line" "$profile"; then
        tmp="$(mktemp)"
        grep -Fv "$path_line" "$profile" | grep -v "^# Canopy$" > "$tmp"
        mv "$tmp" "$profile"
        echo "Removed PATH entry from ${profile}"
    fi
done

echo "Canopy uninstalled."
