# Canopy

Your Mac's terminal, on your phone.

Canopy is a macOS daemon + iOS app that lets you monitor and interact with every terminal session running on your Mac — from anywhere, on any network. Long-running builds, AI coding sessions, deploy scripts, SSH sessions — all streamed to your iPhone as clean, readable conversations over an encrypted peer-to-peer tunnel.

## How It Works

```
┌─────────────────┐                                       ┌─────────────────┐
│   iPhone App     │    ┌───────────────────────┐          │   Mac Daemon     │
│   ("Canopy")     │    │  Coordination Server  │          │   ("canopyd")    │
│                  │    │                       │          │                  │
│ - Conversation   │    │  - Endpoint exchange  │          │ - Shell hook     │
│   view           │    │  - NAT traversal      │          │ - PTY capture    │
│ - Approve/reject │    │  - TURN relay         │          │ - Parser engine  │
│ - File viewer    │    │    (fallback only)     │          │ - Session store  │
│ - History        │    │  - Push forwarding    │          │ - WireGuard      │
│ - WireGuard VPN  │    └──────────┬────────────┘          │   endpoint       │
│                  │               │                       │ - WebSocket API  │
│                  │◄──────────────┼──────────────────────►│                  │
│                  │  Direct P2P WireGuard tunnel (UDP)    │                  │
│                  │  (or relayed via coord when needed)   │                  │
└─────────────────┘                                       └────────┬─────────┘
                                                                   │
                                                     ┌─────────────┼─────────────┐
                                                     │             │             │
                                                 ┌───▼───┐    ┌───▼───┐    ┌───▼───┐
                                                 │ zsh   │    │ zsh   │    │ zsh   │
                                                 │ sess1 │    │ sess2 │    │ sess3 │
                                                 │(claude│    │(npm   │    │(ssh   │
                                                 │ code) │    │ build)│    │ prod) │
                                                 └───────┘    └───────┘    └───────┘
```

**Install on Mac. Open app on iPhone. Scan QR code. Done.**

## Features

- **Everything is a conversation.** Terminal sessions are rendered as conversations — your commands on the right, output on the left. AI coding sessions (Claude Code, Aider, Goose, Codex) get rich rendering with tool use cards, diffs, and approval buttons.
- **Every session, automatically.** After install, every new shell session is captured with zero user action.
- **Zero-config networking.** WireGuard-based encrypted tunnel. Works from anywhere — home, office, cellular, airplane WiFi. No port forwarding, no VPN setup.
- **Multi-Mac.** Connect a single phone to multiple Macs. All sessions appear in one unified list.
- **Collaborative.** Multiple phones can connect to the same Mac. Everyone sees the same sessions in real-time.
- **Offline-resilient.** The daemon stores everything to disk. Disconnect for hours, come back, and catch up on everything you missed.
- **File viewer.** Peek at files on your Mac directly from your phone.
- **Push notifications.** Get notified when builds finish, tests fail, or AI agents need approval.

## Requirements

- **Mac:** macOS (Apple Silicon or Intel)
- **iPhone:** iOS 17+
- **Build from source:** Go 1.21+, Xcode 15+

## Install

### Mac Daemon (build from source)

```bash
curl -fsSL https://raw.githubusercontent.com/mhadifilms/canopy/main/daemon/install.sh | bash
```

This clones the repo, builds `canopyd`, installs it to `/usr/local/bin`, injects shell hooks into your `.zshrc`/`.bashrc`, and starts the daemon via launchd.

Or build manually:

```bash
git clone https://github.com/mhadifilms/canopy.git
cd canopy/daemon
make build
sudo cp bin/canopyd /usr/local/bin/
canopyd setup
```

### iOS App

Build from source with Xcode:

```bash
cd ios/Canopy
open Package.swift
```

Or generate the Xcode project:

```bash
brew install xcodegen  # if needed
cd ios/Canopy
xcodegen generate
open Canopy.xcodeproj
```

### Pairing

Once both are running:

```bash
canopyd pair
```

Scan the QR code from the iOS app's onboarding screen. That's it.

## Usage

### CLI Commands

```
canopyd daemon start|stop|ping    Start/stop the daemon or check status
canopyd pair                      Generate a QR code to pair your iPhone
canopyd devices                   List paired devices
canopyd sessions                  List active and recent sessions
canopyd storage                   Show storage usage
canopyd config                    View or edit configuration
canopyd update                    Update canopyd to the latest version
canopyd uninstall                 Remove canopyd and all hooks
canopyd debug                     Diagnostic info for troubleshooting
canopyd version                   Print version
```

### Configuration

Config lives at `~/.config/canopy/config.json`:

```json
{
  "listen_port": 19876,
  "wg_listen_port": 51820,
  "coord_url": "https://coord.canopy.dev",
  "capture_all_sessions": true,
  "capture_exclude_processes": ["ssh-agent", "gpg-agent"],
  "parsers_enabled": ["generic", "claude_code", "aider", "goose", "codex"],
  "retention_days": 30,
  "max_storage_gb": 10,
  "prevent_sleep_while_active": true,
  "max_paired_devices": 10
}
```

To disable capture for a specific session, set the env var before opening the shell:

```bash
CANOPY_DISABLE=1 zsh
```

### Uninstall

```bash
canopyd uninstall
```

This removes the binary, shell hooks, launchd plist, and config directory.

## Project Structure

```
canopy/
├── daemon/           Go — macOS background daemon
│   ├── cmd/canopyd/  CLI entry point (cobra)
│   ├── internal/     Core packages
│   │   ├── api/      WebSocket API server
│   │   ├── attach/   PTY proxy for session capture
│   │   ├── parser/   Terminal output → structured events
│   │   ├── session/  Session lifecycle management
│   │   ├── storage/  Disk persistence + compression
│   │   ├── wg/       Userspace WireGuard endpoint
│   │   ├── coord/    Coordination server client
│   │   └── push/     Push notification triggers
│   └── Makefile
├── coord/            Go — lightweight coordination server
│   ├── cmd/coord/    Server entry point
│   ├── internal/     STUN/TURN, auth, rate limiting, APNs
│   └── Dockerfile
├── ios/Canopy/       Swift/SwiftUI — iPhone app
│   ├── Sources/
│   │   ├── Views/    Conversation, Sessions, Devices, History,
│   │   │             FileViewer, Settings, Onboarding
│   │   ├── Models/   Data structures
│   │   ├── Networking/  WebSocket + WireGuard clients
│   │   ├── Services/    Parsing, notifications, tunneling
│   │   └── Stores/      State management
│   ├── Package.swift
│   └── project.yml   XcodeGen spec
└── PLAN.md           Full build specification
```

## Connection Architecture

Canopy creates its own encrypted tunnel between your Mac and iPhone using WireGuard:

1. **WireGuard under the hood.** The Mac daemon runs a userspace WireGuard endpoint. The iOS app uses Apple's Network Extension framework to establish a WireGuard VPN tunnel.

2. **Coordination server for discovery.** Both devices check in with a lightweight coordination server that only exchanges public endpoints (IP + port). No session data passes through it.

3. **Direct peer-to-peer (~80% of networks).** WireGuard uses UDP, which traverses most NAT configurations. Data flows directly between devices with no intermediary.

4. **TURN relay fallback (~20%).** When direct P2P fails (symmetric NAT, some corporate firewalls), the coordination server relays encrypted WireGuard packets. Even then, it cannot read any content.

## Parser Engine

The daemon parses terminal output into structured conversation events. Built-in parsers:

| Parser | What it handles |
|--------|----------------|
| `generic` | Any shell session — commands, output, exit codes |
| `claude_code` | Claude Code sessions — tool use, file edits, approvals |
| `aider` | Aider sessions — chat, edits, commands |
| `goose` | Goose sessions |
| `codex` | OpenAI Codex CLI sessions |

Parsers use OSC 133 shell integration markers for precise command boundary detection and process table inspection for foreground process identification.

## Self-Hosting the Coordination Server

The coordination server is included and can be self-hosted:

```bash
cd coord
docker build -t canopy-coord .
docker run -p 8080:8080 -p 3478:3478/udp -p 3479:3479/udp canopy-coord
```

Then point your daemon at it:

```bash
canopyd config set coord_url http://your-server:8080
```

## Development

### Daemon

```bash
cd daemon
make build          # Build for current arch
make test           # Run tests with race detector
make lint           # Run go vet
make build-all      # Build for both arm64 and amd64
make clean          # Clean build artifacts
```

### Coordination Server

```bash
cd coord
go build -o coord ./cmd/coord
go test ./...
```

### iOS App

Open `ios/Canopy/Package.swift` in Xcode. Requires iOS 17+ deployment target and Xcode 15+.
