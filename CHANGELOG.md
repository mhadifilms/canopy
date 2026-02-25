# Changelog

## 0.1.0 — 2026-02-22

Initial release. Built in five phases:

### Phase 1: Foundation
- Go daemon (`canopyd`) with PTY capture and session management
- Terminal output parser engine with generic and AI-specific parsers
- iOS app scaffold with SwiftUI conversation view
- WebSocket API for real-time session streaming

### Phase 2: Networking
- Coordination server for device discovery and NAT traversal
- AI parsers for Claude Code, Aider, Goose, and Codex
- Push notification pipeline (daemon -> coord -> APNs)
- STUN/TURN relay for restrictive NAT environments

### Phase 3: Multi-Device & Polish
- Multi-Mac support — one phone, multiple Macs
- Session history with search and filtering
- Collaborative viewing — multiple phones per Mac
- File viewer for remote file browsing
- Storage management with compression and retention policies

### Phase 4: Security Audit
- Signal forwarding hardening (SIGTERM, SIGINT, SIGWINCH)
- WireGuard bridge handshake fixes
- Pairing protocol cryptographic audit
- APNs push notification reliability improvements

### Phase 5: Release Preparation
- Build-from-source install script
- Shell hook injection (zsh, bash, fish)
- Launchd integration for daemon auto-start
- Uninstall command for clean removal
