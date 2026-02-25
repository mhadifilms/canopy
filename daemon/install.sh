#!/bin/bash
# Canopy Install Script
# Usage: curl -fsSL https://raw.githubusercontent.com/mhadifilms/canopy/main/daemon/install.sh | bash
#
# Clones the repo, builds canopyd from source, sets up shell hooks, generates keys,
# and starts the daemon.
set -euo pipefail

INSTALL_DIR="/usr/local/bin"
BINARY_NAME="canopyd"
REPO_URL="https://github.com/mhadifilms/canopy.git"
BUILD_DIR="${HOME}/.canopy-build"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

info() { echo -e "${GREEN}  $1${NC}"; }
warn() { echo -e "${YELLOW}  $1${NC}"; }
error() { echo -e "${RED}Error: $1${NC}" >&2; exit 1; }

# --- 1. Check platform ---
OS="$(uname -s)"
if [ "$OS" != "Darwin" ]; then
    error "canopyd only supports macOS. Detected: $OS"
fi

echo ""
echo "  Installing canopyd..."
echo ""

# --- 2. Check dependencies ---
if ! command -v go &>/dev/null; then
    error "Go is required but not installed. Install it from https://go.dev/dl/"
fi

if ! command -v git &>/dev/null; then
    error "Git is required but not installed. Install Xcode CLI tools: xcode-select --install"
fi

GO_VERSION=$(go version | grep -oE 'go[0-9]+\.[0-9]+' | sed 's/go//')
GO_MAJOR=$(echo "$GO_VERSION" | cut -d. -f1)
GO_MINOR=$(echo "$GO_VERSION" | cut -d. -f2)
if [ "$GO_MAJOR" -lt 1 ] || { [ "$GO_MAJOR" -eq 1 ] && [ "$GO_MINOR" -lt 21 ]; }; then
    error "Go 1.21+ required. Found: go${GO_VERSION}. Update from https://go.dev/dl/"
fi

info "Go $(go version | grep -oE 'go[0-9]+\.[0-9]+\.[0-9]+') detected"

# --- 3. Clone or update repo ---
if [ -d "${BUILD_DIR}" ]; then
    echo "  Updating existing checkout..."
    cd "${BUILD_DIR}"
    git pull --ff-only origin main 2>/dev/null || {
        warn "Pull failed, doing fresh clone"
        rm -rf "${BUILD_DIR}"
        git clone --depth 1 "$REPO_URL" "${BUILD_DIR}"
        cd "${BUILD_DIR}"
    }
else
    echo "  Cloning canopy..."
    git clone --depth 1 "$REPO_URL" "${BUILD_DIR}"
    cd "${BUILD_DIR}"
fi

info "Source ready at ${BUILD_DIR}"

# --- 4. Build binary ---
echo "  Building canopyd..."
cd "${BUILD_DIR}/daemon"
go build -o "${BINARY_NAME}" ./cmd/canopyd || error "Build failed"

info "Build succeeded"

# --- 5. Install binary ---
echo "  Installing to ${INSTALL_DIR}/${BINARY_NAME}..."
chmod 755 "${BINARY_NAME}"
if [ -w "$INSTALL_DIR" ]; then
    mv "${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
else
    sudo mv "${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
fi
info "canopyd installed to ${INSTALL_DIR}/${BINARY_NAME}"

# --- 6. Create config directory ---
CONFIG_DIR="${HOME}/.config/canopy"
mkdir -p "${CONFIG_DIR}/sessions" "${CONFIG_DIR}/parsers"

# --- 7. Run canopyd setup (keys, config, shell hooks, launchd) ---
echo "  Running setup..."
"${INSTALL_DIR}/${BINARY_NAME}" setup 2>/dev/null || {
    # If setup command doesn't exist yet, do manual setup
    warn "Setup command not available, performing manual setup"

    # Generate keys using openssl if canopyd setup isn't available
    if [ ! -f "${CONFIG_DIR}/identity.key" ]; then
        # Identity keypair (Ed25519)
        openssl genpkey -algorithm ed25519 -out "${CONFIG_DIR}/identity.key" 2>/dev/null
        openssl pkey -in "${CONFIG_DIR}/identity.key" -pubout -out "${CONFIG_DIR}/identity.pub" 2>/dev/null
        chmod 600 "${CONFIG_DIR}/identity.key"
        info "Identity keypair generated"
    fi

    # WireGuard keypair (Curve25519)
    if [ ! -f "${CONFIG_DIR}/wg_private.key" ]; then
        head -c 32 /dev/urandom > "${CONFIG_DIR}/wg_private.key"
        chmod 600 "${CONFIG_DIR}/wg_private.key"
        info "WireGuard keypair generated"
    fi

    # Default config
    if [ ! -f "${CONFIG_DIR}/config.json" ]; then
        cat > "${CONFIG_DIR}/config.json" << 'CONFIGEOF'
{
  "listen_port": 19876,
  "wg_listen_port": 51820,
  "coord_url": "https://coord.canopy.dev",
  "capture_all_sessions": true,
  "capture_exclude_processes": ["ssh-agent", "gpg-agent"],
  "capture_exclude_env": {"CANOPY_DISABLE": "1"},
  "parsers_enabled": ["generic", "claude_code", "aider", "goose", "codex"],
  "shell_integration_markers": true,
  "retention_days": 30,
  "max_storage_gb": 10,
  "compress_after_hours": 24,
  "prevent_sleep_while_active": true,
  "auto_update": true,
  "file_access_root": null,
  "file_access_max_size_mb": 1,
  "max_paired_devices": 10
}
CONFIGEOF
        info "Default config written"
    fi

    # Devices list
    if [ ! -f "${CONFIG_DIR}/devices.json" ]; then
        echo "[]" > "${CONFIG_DIR}/devices.json"
        chmod 600 "${CONFIG_DIR}/devices.json"
    fi

    # Shell hooks
    inject_hook() {
        local rcfile="$1"
        local hook="$2"
        local integration="$3"

        touch "$rcfile"

        if grep -q "Canopy Hook" "$rcfile" 2>/dev/null; then
            return 0
        fi

        printf '\n%s\n\n%s\n' "$hook" "$integration" >> "$rcfile"
    }

    # Zsh hook
    ZSHRC="${HOME}/.zshrc"
    inject_hook "$ZSHRC" \
'# --- Canopy Hook (do not edit) ---
if [ -z "$CANOPY_SESSION_ID" ] && command -v canopyd &>/dev/null && canopyd daemon ping &>/dev/null; then
  export CANOPY_SESSION_ID=$(uuidgen)
  exec canopyd attach --session-id "$CANOPY_SESSION_ID" -- "$SHELL" -l
fi
# --- End Canopy Hook ---' \
'# --- Canopy Shell Integration (do not edit) ---
__canopy_precmd() {
  local exit_code=$?
  printf '"'"'\e]133;D;%s\a'"'"' "$exit_code"
  printf '"'"'\e]133;A\a'"'"'
}
__canopy_preexec() {
  printf '"'"'\e]133;C\a'"'"'
}
autoload -Uz add-zsh-hook 2>/dev/null && {
  add-zsh-hook precmd __canopy_precmd
  add-zsh-hook preexec __canopy_preexec
}
# --- End Canopy Shell Integration ---'
    info "Shell hooks added to ~/.zshrc"

    # Bash hook (if .bashrc exists)
    BASHRC="${HOME}/.bashrc"
    if [ -f "$BASHRC" ]; then
        inject_hook "$BASHRC" \
'# --- Canopy Hook (do not edit) ---
if [ -z "$CANOPY_SESSION_ID" ] && command -v canopyd &>/dev/null && canopyd daemon ping &>/dev/null; then
  export CANOPY_SESSION_ID=$(uuidgen)
  exec canopyd attach --session-id "$CANOPY_SESSION_ID" -- "$SHELL" -l
fi
# --- End Canopy Hook ---' \
'# --- Canopy Shell Integration (do not edit) ---
__canopy_precmd() {
  local exit_code=$?
  printf '"'"'\e]133;D;%s\a'"'"' "$exit_code"
  printf '"'"'\e]133;A\a'"'"'
}
__canopy_preexec() {
  printf '"'"'\e]133;C\a'"'"'
}
PROMPT_COMMAND="__canopy_precmd${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
trap '"'"'__canopy_preexec'"'"' DEBUG
# --- End Canopy Shell Integration ---'
        info "Shell hooks added to ~/.bashrc"
    fi

    # Launchd plist
    PLIST_DIR="${HOME}/Library/LaunchAgents"
    PLIST_PATH="${PLIST_DIR}/dev.canopy.daemon.plist"
    mkdir -p "$PLIST_DIR"
    cat > "$PLIST_PATH" << PLISTEOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>dev.canopy.daemon</string>
  <key>ProgramArguments</key>
  <array>
    <string>${INSTALL_DIR}/${BINARY_NAME}</string>
    <string>daemon</string>
    <string>start</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <dict>
    <key>SuccessfulExit</key>
    <false/>
  </dict>
  <key>StandardOutPath</key>
  <string>/tmp/canopyd.stdout.log</string>
  <key>StandardErrorPath</key>
  <string>/tmp/canopyd.stderr.log</string>
  <key>ProcessType</key>
  <string>Background</string>
  <key>LowPriorityIO</key>
  <true/>
</dict>
</plist>
PLISTEOF
    info "Launchd plist installed"

    # Load daemon
    launchctl load "$PLIST_PATH" 2>/dev/null || true
    info "Daemon started"
}

# --- 8. Post-install output ---
echo ""
info "canopyd installed successfully!"
info "Shell hooks added to ~/.zshrc"
info "Daemon started via launchd"
echo ""
echo "  To pair your iPhone:"
echo "    canopyd pair"
echo ""
echo "  Open a new terminal tab to start capturing sessions."
echo ""
