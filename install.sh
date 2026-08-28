#!/usr/bin/env bash
set -euo pipefail

# Install the latest published Canopy binary.
#
#   curl -fsSL https://raw.githubusercontent.com/sharathk-dev/canopy/master/install.sh | bash
#   curl -fsSL https://raw.githubusercontent.com/sharathk-dev/canopy/master/install.sh | VERSION=v0.1.0-beta.1 bash

REPO="${REPO:-sharathk-dev/canopy}"
VERSION="${VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
BINARY="canopy"

die() {
    echo "error: $*" >&2
    exit 1
}

command -v curl >/dev/null 2>&1 || die "curl is required"

case "$(uname -s)" in
    Darwin) os="darwin" ;;
    Linux)  os="linux" ;;
    *)      die "unsupported operating system: $(uname -s) (use macOS or Linux)" ;;
esac

case "$(uname -m)" in
    x86_64|amd64) arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
    *) die "unsupported CPU architecture: $(uname -m) (use amd64 or arm64)" ;;
esac

asset="${BINARY}-${os}-${arch}"
if [ "$VERSION" = "latest" ]; then
    url="https://github.com/${REPO}/releases/latest/download/${asset}"
else
    url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"
fi

tmp="$(mktemp)"
checksums="$(mktemp)"
trap 'rm -f "$tmp" "$checksums"' EXIT

echo "Downloading ${asset} (${VERSION})..."
curl --fail --location --silent --show-error --retry 3 "$url" --output "$tmp" \
    || die "could not download ${url}; check that this version has a published ${asset} release"

if [ "$VERSION" = "latest" ]; then
    checksum_url="https://github.com/${REPO}/releases/latest/download/checksums.txt"
else
    checksum_url="https://github.com/${REPO}/releases/download/${VERSION}/checksums.txt"
fi
echo "Verifying ${asset}..."
curl --fail --location --silent --show-error --retry 3 "$checksum_url" --output "$checksums" \
    || die "could not download release checksums"
expected="$(awk -v asset="$asset" '$2 == asset { print $1; exit }' "$checksums")"
[ -n "$expected" ] || die "no checksum found for ${asset}"
if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$tmp" | awk '{print $1}')"
else
    actual="$(shasum -a 256 "$tmp" | awk '{print $1}')"
fi
[ "$actual" = "$expected" ] || die "checksum verification failed for ${asset}"

mkdir -p "$INSTALL_DIR"
install -m 0755 "$tmp" "${INSTALL_DIR}/${BINARY}"
trap - EXIT

installed_version="$("${INSTALL_DIR}/${BINARY}" --version 2>/dev/null || echo "unknown")"
echo "Installed ${INSTALL_DIR}/${BINARY}  (${installed_version})"

case ":${PATH}:" in
    *":${INSTALL_DIR}:"*) ;;
    *)
        profile=""
        shell_name="${SHELL##*/}"
        case "$shell_name" in
            zsh) profile="${ZDOTDIR:-$HOME}/.zshrc" ;;
            bash)
                if [ "$(uname -s)" = "Darwin" ]; then
                    profile="$HOME/.bash_profile"
                else
                    profile="$HOME/.bashrc"
                fi
                ;;
        esac

        path_line="export PATH=\"${INSTALL_DIR}:\$PATH\""
        if [ -n "$profile" ]; then
            touch "$profile"
            if ! grep -Fqx "$path_line" "$profile"; then
                printf '\n# Canopy\n%s\n' "$path_line" >> "$profile"
            fi
            echo "Added ${INSTALL_DIR} to ${profile}"
            echo "Run: source ${profile} or open a new terminal"
        else
            echo "Add this to your shell profile:"
            echo "  ${path_line}"
        fi
        ;;
esac

echo "Run: ${BINARY} --help"
