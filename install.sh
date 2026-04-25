#!/usr/bin/env bash
set -e

# NEXUS installer — downloads the nexus binary and clones the repo.
# Usage: curl -sSL https://raw.githubusercontent.com/canoo/agent-nexus/main/install.sh | bash

REPO="canoo/agent-nexus"
INSTALL_DIR="$HOME/.local/bin"
NEXUS_DIR="$HOME/.config/nexus/repo"

# --- helpers ---

info()  { printf "\033[1;34m==>\033[0m %s\n" "$1"; }
ok()    { printf "\033[1;32m  ✓\033[0m %s\n" "$1"; }
warn()  { printf "\033[1;33m  !\033[0m %s\n" "$1"; }
fail()  { printf "\033[1;31m  ✗\033[0m %s\n" "$1"; exit 1; }

detect_platform() {
    OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
    ARCH="$(uname -m)"

    case "$OS" in
        linux)  OS="linux" ;;
        darwin) OS="darwin" ;;
        *)      fail "Unsupported OS: $OS" ;;
    esac

    case "$ARCH" in
        x86_64|amd64)  ARCH="amd64" ;;
        aarch64|arm64) ARCH="arm64" ;;
        *)             fail "Unsupported architecture: $ARCH" ;;
    esac
}

get_latest_version() {
    if command -v curl &>/dev/null; then
        VERSION=$(curl -sSL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | head -1 | cut -d'"' -f4)
    elif command -v wget &>/dev/null; then
        VERSION=$(wget -qO- "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | head -1 | cut -d'"' -f4)
    else
        fail "Neither curl nor wget found."
    fi

    if [ -z "$VERSION" ]; then
        fail "Could not determine latest release. Check https://github.com/$REPO/releases"
    fi
}

download_binary() {
    local url="https://github.com/$REPO/releases/download/$VERSION/nexus-${OS}-${ARCH}"
    local checksum_url="https://github.com/$REPO/releases/download/$VERSION/checksums.txt"
    local dest="$INSTALL_DIR/nexus"
    local checksum_file
    checksum_file="$(mktemp)"

    mkdir -p "$INSTALL_DIR"

    info "Downloading nexus $VERSION ($OS/$ARCH)..."
    if command -v curl &>/dev/null; then
        curl -sSL "$url" -o "$dest"
        curl -sSL "$checksum_url" -o "$checksum_file"
    else
        wget -qO "$dest" "$url"
        wget -qO "$checksum_file" "$checksum_url"
    fi

    info "Verifying checksum..."
    local binary_name="nexus-${OS}-${ARCH}"
    local expected
    expected=$(awk -v name="$binary_name" '$2 == name {print $1}' "$checksum_file")
    if [ -z "$expected" ]; then
        rm -f "$dest" "$checksum_file"
        fail "Checksum entry for $binary_name not found in checksums.txt"
    fi

    # On macOS, BSD sha256sum does not support --check/--status; prefer shasum -a 256.
    # On Linux, prefer sha256sum (GNU coreutils).
    if [ "$OS" = "darwin" ] && command -v shasum &>/dev/null; then
        echo "$expected  $dest" | shasum -a 256 --check --status || {
            rm -f "$dest" "$checksum_file"
            fail "Checksum verification failed — binary may be corrupted or tampered with"
        }
    elif command -v sha256sum &>/dev/null; then
        echo "$expected  $dest" | sha256sum --check --status || {
            rm -f "$dest" "$checksum_file"
            fail "Checksum verification failed — binary may be corrupted or tampered with"
        }
    elif command -v shasum &>/dev/null; then
        echo "$expected  $dest" | shasum -a 256 --check --status || {
            rm -f "$dest" "$checksum_file"
            fail "Checksum verification failed — binary may be corrupted or tampered with"
        }
    else
        rm -f "$dest" "$checksum_file"
        fail "Neither sha256sum nor shasum found — cannot verify binary integrity. Install coreutils and retry."
    fi

    rm -f "$checksum_file"
    chmod +x "$dest"
    ok "Checksum verified. Installed to $dest"
}

clone_repo() {
    if [ -d "$NEXUS_DIR/.git" ]; then
        info "Updating existing repo..."
        git -C "$NEXUS_DIR" pull --ff-only 2>/dev/null || warn "Pull failed — using existing clone"
        ok "Repo up to date at $NEXUS_DIR"
    else
        info "Cloning agent-nexus..."
        mkdir -p "$(dirname "$NEXUS_DIR")"
        git clone "https://github.com/$REPO.git" "$NEXUS_DIR"
        ok "Cloned to $NEXUS_DIR"
    fi
}

check_path() {
    if ! echo "$PATH" | tr ':' '\n' | grep -qx "$INSTALL_DIR"; then
        echo ""
        warn "$INSTALL_DIR is not in your PATH."
        echo "    Add this to your shell profile (~/.bashrc, ~/.zshrc, etc.):"
        echo ""
        echo "      export PATH=\"\$HOME/.local/bin:\$PATH\""
        echo ""
    fi
}

# --- main ---

echo ""
echo "  ⚡ NEXUS Installer"
echo ""

detect_platform
get_latest_version
download_binary
clone_repo
check_path

echo ""
info "Run 'nexus' to complete setup."
echo ""
