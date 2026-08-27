#!/usr/bin/env bash
set -e

BINARY="canopy"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

need_cmd() {
    if ! command -v "$1" &>/dev/null; then
        echo "error: '$1' is required but not installed." >&2
        exit 1
    fi
}

need_cmd go

mkdir -p "$INSTALL_DIR"

echo "Building $BINARY..."
go build -o "$INSTALL_DIR/$BINARY" ./cmd/canopy
echo "Removing stale copies..."
for stale in /usr/local/bin/$BINARY /usr/bin/$BINARY; do
    [ -f "$stale" ] && sudo rm -f "$stale" && echo "  removed $stale" || true
done

# Add to shell profile if not already in PATH.
if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
    PROFILE=""
    if [ -f "$HOME/.zshrc" ]; then
        PROFILE="$HOME/.zshrc"
    elif [ -f "$HOME/.bashrc" ]; then
        PROFILE="$HOME/.bashrc"
    fi

    if [ -n "$PROFILE" ]; then
        echo "" >> "$PROFILE"
        echo "export PATH=\"\$HOME/.local/bin:\$PATH\"" >> "$PROFILE"
        echo "Added $INSTALL_DIR to PATH in $PROFILE"
        echo "Run: source $PROFILE"
    else
        echo "warning: could not find shell profile. Add this manually:"
        echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
    fi
fi

echo "Installed: $INSTALL_DIR/$BINARY"
echo "Run: $BINARY --help"
