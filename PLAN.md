# Canopy — Build Specification

**Version:** 1.0 Final
**Date:** February 22, 2026
**Status:** Pre-development

---

## 1. Product Overview

### 1.1 One-Liner

Your Mac's terminal, on your phone.

### 1.2 Problem

Developers live in the terminal. Long-running builds, AI coding sessions, deploy scripts, test suites, SSH sessions into servers — things that take minutes or hours and demand attention at unpredictable moments. Today the only way to check on them is to be physically at your Mac or awkwardly VNC in from your phone. There is no native, clean, mobile-first way to monitor and interact with terminal sessions running on your own machine.

Cloud-based companion apps (Claude mobile, Cursor mobile) exist but they operate on cloud workstations. They cannot see or interact with sessions running against your local filesystem.

### 1.3 Solution

Two components:

1. **Mac Daemon (`canopyd`)** — A CLI tool installed via a one-line script. Auto-hooks into every new terminal session via shell config. Captures all terminal I/O from every session. Parses everything into a structured conversation model — every command you type and every response the system gives. Stores full session history. Exposes a direct connection to the iPhone via an encrypted tunnel.

2. **iOS App ("Canopy")** — A native SwiftUI app. Connects to one or more Macs via an encrypted peer-to-peer tunnel — works from anywhere, any network, cellular data, no configuration. Displays every terminal session as a conversation: your commands on one side, the system's responses on the other. AI coding sessions get richer rendering (tool use cards, approval buttons, diffs) but the fundamental model is the same for everything. You can read, type, approve, and browse history.

### 1.4 Connection Architecture — Native Encrypted Tunnel

Canopy creates its own encrypted tunnel between your Mac and iPhone, similar to how Tailscale works under the hood. No third-party VPN, no relay service sitting in the middle reading your data, no network configuration.

**How it works:**

1. **WireGuard under the hood.** The Mac daemon runs a userspace WireGuard endpoint. The iOS app uses Apple's Network Extension framework to establish a WireGuard VPN tunnel. This gives you a direct, encrypted, peer-to-peer connection between your Mac and iPhone.

2. **Coordination server for NAT traversal.** Both devices check in with a lightweight Canopy coordination server (`coord.canopy.dev`). The coordination server does NOT relay any session data — it only helps devices find each other by exchanging public endpoints (IP + port), similar to a STUN server. Once devices know each other's endpoints, they connect directly.

3. **Direct peer-to-peer when possible (~80% of cases).** WireGuard uses UDP, which traverses most NAT configurations. When both devices can reach each other directly, data flows straight between them with no intermediary. The coordination server is not involved after the initial handshake.

4. **TURN-style relay fallback for restrictive NAT (~20% of cases).** When direct P2P fails (symmetric NAT, some corporate firewalls), the coordination server acts as a TURN relay — forwarding encrypted WireGuard packets between devices. Even in this mode, the relay sees only encrypted WireGuard traffic and cannot read any content.

5. **Works from anywhere.** WireGuard's UDP-based protocol traverses virtually every network: home routers, corporate firewalls, cellular data, hotel WiFi, airplane WiFi. The iOS VPN tunnel means the Mac daemon's port is directly reachable from the phone regardless of network topology.

**What the user experiences:** Install on Mac. Open app on iPhone. Scan QR code. Done. The tunnel is always on. They never think about networking.

### 1.5 Key Design Principles

- **Everything is a conversation.** The phone always displays terminal sessions as conversations — your input on the right, the system's response on the left. Whether you typed `npm run build` or asked Claude Code to fix a bug, the interaction model is the same. There is no "raw terminal view" vs "chat view" — there is one view that presents all terminal activity as a clean, readable conversation.
- **Every session, automatically.** Canopy captures all terminal sessions. After the install script runs, every new shell session is captured with zero user action.
- **Zero-config networking.** The encrypted tunnel means the user never thinks about networking. Install, pair, done. Works from anywhere on any network.
- **Multi-Mac.** A single phone connects to multiple Macs. Sessions from all Macs appear in one unified list.
- **Collaborative.** Multiple phones can connect to the same Mac. Everyone sees the same sessions in real-time. Anyone can type.
- **Offline-resilient.** The daemon stores everything to disk. The phone can disconnect for hours, come back, and catch up on everything it missed.

---

## 2. System Architecture

### 2.1 High-Level Diagram

```
┌─────────────────┐                                       ┌─────────────────┐
│   iPhone App     │    ┌───────────────────────┐          │   Mac Daemon     │
│   ("Canopy")     │    │  Canopy Coordination  │          │   ("canopyd")    │
│                  │    │  Server               │          │                  │
│ - Conversation   │    │                       │          │ - Shell hook     │
│   view           │    │  - Endpoint exchange  │          │ - PTY capture    │
│ - Approve/reject │    │  - NAT traversal      │          │ - Parser engine  │
│ - File peek      │    │  - TURN relay         │          │ - Session store  │
│ - History        │    │    (fallback only)     │          │ - WireGuard      │
│ - WireGuard      │    │  - Push forwarding    │          │   endpoint       │
│   Network Ext.   │    └──────────┬────────────┘          │ - WebSocket API  │
│                  │               │                       │                  │
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

### 2.2 Component Summary

| Component | Language | Description |
|-----------|----------|-------------|
| `canopyd` (Mac daemon) | Go | Background process — PTY capture, parsing, API, storage, WireGuard endpoint |
| Shell hook | Shell (zsh/bash/fish) | Injected into shell config — wraps each session in a captured PTY |
| Canopy iOS app | Swift / SwiftUI | Native iPhone app — conversation UI, WireGuard Network Extension |
| Coordination server | Go | Lightweight — endpoint exchange, NAT traversal assistance, TURN relay fallback, push forwarding |

### 2.3 Data Flow — A Complete Example

1. User opens a new Terminal.app window on their Mac.
2. The shell hook fires. It runs `exec canopyd attach --session-id <uuid> -- /bin/zsh -l`, creating a transparent PTY proxy.
3. `canopyd attach` registers the session with the daemon over a Unix domain socket. All I/O is copied to the daemon.
4. The user types `npm run build`. The daemon's parser sees the OSC 133 shell integration marker for command start, captures the command text from the input stream, and emits a `user_input` event.
5. Build output streams. The daemon accumulates it and emits `system_output` events.
6. The build finishes. The shell's `precmd` hook fires an OSC 133 marker with the exit code. The daemon emits a `command_complete` event.
7. All events are pushed over the WebSocket to the iPhone, which is connected via the WireGuard tunnel.
8. The iPhone app renders this as a conversation: the user's command "npm run build" appears as a sent message on the right. The build output appears on the left as the response, with an exit code badge.
9. The user then types `claude` to start Claude Code. The daemon detects the foreground process change and enriches subsequent parsing.
10. The user types "fix the auth bug" — this appears as a message on the right. Claude Code's response streams in on the left, with tool use cards for file reads and edits. An approval request appears as an interactive banner.
11. The phone buzzes with a push notification. The user taps "Approve" from the lock screen. The app connects briefly in the background, sends "y\n" to the PTY, and disconnects.

---

## 3. Mac Daemon (`canopyd`)

### 3.1 Overview

`canopyd` is a single Go binary installed to `/usr/local/bin/canopyd`. It runs as a background daemon (managed by launchd) and performs:

1. Accepts session registrations from shell hook PTY proxies
2. Captures all PTY I/O from every terminal session
3. Parses all output into a universal conversation model
4. Stores session data to disk (raw logs + structured events)
5. Runs a userspace WireGuard endpoint for the encrypted tunnel
6. Exposes a WebSocket API over the tunnel
7. Sends push notification triggers to the coordination server

### 3.2 Installation

```bash
curl -fsSL https://canopy.dev/install.sh | bash
```

The install script performs:

#### 3.2.1 Binary Download

Detects architecture (`uname -m`) and downloads the appropriate binary:
- `canopyd-darwin-arm64` for Apple Silicon
- `canopyd-darwin-amd64` for Intel

Downloads from `https://releases.canopy.dev/latest/canopyd-darwin-{arch}`.

Verifies the download against a SHA256 checksum fetched from `https://releases.canopy.dev/latest/checksums.txt`.

Installs to `/usr/local/bin/canopyd` with `chmod 755`.

#### 3.2.2 Config Directory

Creates:

```
~/.config/canopy/
├── config.json           # Daemon configuration
├── identity.key          # Ed25519 identity private key (mode 0600)
├── identity.pub          # Ed25519 identity public key
├── wg_private.key        # WireGuard private key (mode 0600)
├── wg_public.key         # WireGuard public key
├── devices.json          # Paired device records (mode 0600)
├── sessions/             # Session storage
└── parsers/              # Optional custom parser configs
```

#### 3.2.3 Key Generation

- **Identity keypair:** Ed25519 for authentication and signing. Public key hash (first 8 bytes of SHA256, hex) serves as the human-readable device ID, e.g., `a3f1c9b2`.
- **WireGuard keypair:** Curve25519 for the encrypted tunnel.

#### 3.2.4 Shell Hook Injection

Detects which shells are in use and injects hooks. Idempotent — running again does not create duplicates.

**For zsh (`~/.zshrc`):**

```bash
# --- Canopy Hook (do not edit) ---
if [ -z "$CANOPY_SESSION_ID" ] && command -v canopyd &>/dev/null && canopyd daemon ping &>/dev/null; then
  export CANOPY_SESSION_ID=$(uuidgen)
  exec canopyd attach --session-id "$CANOPY_SESSION_ID" -- "$SHELL" -l
fi
# --- End Canopy Hook ---
```

**For bash (`~/.bashrc`):** Same logic, same structure.

**For fish (`~/.config/fish/config.fish`):**

```fish
# --- Canopy Hook (do not edit) ---
if not set -q CANOPY_SESSION_ID; and command -v canopyd &>/dev/null; and canopyd daemon ping &>/dev/null
  set -gx CANOPY_SESSION_ID (uuidgen)
  exec canopyd attach --session-id $CANOPY_SESSION_ID -- $SHELL -l
end
# --- End Canopy Hook ---
```

**Key details:**
- `CANOPY_SESSION_ID` prevents recursive hooking — the spawned shell already has it set, so the hook is a no-op.
- `canopyd daemon ping` check makes the hook a no-op when the daemon isn't running. Terminal works normally with zero overhead.
- `exec` replaces the process — no extra process in the chain.

#### 3.2.5 Shell Integration Markers

The install script also injects shell integration hooks that emit OSC 133 markers. These provide reliable command boundary detection:

**For zsh** (appended to the same block or to a separate sourced file):

```bash
__canopy_precmd() {
  local exit_code=$?
  printf '\e]133;D;%s\a' "$exit_code"   # Report previous command's exit code
  printf '\e]133;A\a'                     # Mark prompt start
}
__canopy_preexec() {
  printf '\e]133;C\a'                     # Mark command execution start
}
[[ -z "$precmd_functions" ]] || precmd_functions=($precmd_functions __canopy_precmd)
[[ -z "$preexec_functions" ]] || preexec_functions=($preexec_functions __canopy_preexec)
# Fallback for shells without function arrays:
autoload -Uz add-zsh-hook 2>/dev/null && {
  add-zsh-hook precmd __canopy_precmd
  add-zsh-hook preexec __canopy_preexec
}
```

OSC 133 is the same standard used by VS Code's terminal integration and iTerm2. It gives us:
- `133;A` — prompt was just displayed (shell is idle, waiting for input)
- `133;C` — user pressed Enter, command is about to execute
- `133;D;{exit_code}` — previous command finished with this exit code

These markers flow through the PTY proxy and are intercepted by the parser (stripped from display but used as structured metadata).

#### 3.2.6 Launchd Plist

Installs `~/Library/LaunchAgents/dev.canopy.daemon.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>dev.canopy.daemon</string>
  <key>ProgramArguments</key>
  <array>
    <string>/usr/local/bin/canopyd</string>
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
```

Loads immediately: `launchctl load ~/Library/LaunchAgents/dev.canopy.daemon.plist`

`KeepAlive` with `SuccessfulExit: false` restarts the daemon on crash but not on clean exit (e.g., `canopyd daemon stop`).

#### 3.2.7 Post-Install Output

```
  ✓ canopyd installed to /usr/local/bin/canopyd
  ✓ Shell hooks added to ~/.zshrc
  ✓ Daemon started
  ✓ Device ID: a3f1c9b2 (hadis-macbook.local)

  To pair your iPhone:
    canopyd pair

  Open a new terminal tab to start capturing sessions.
```

### 3.3 Uninstallation

```bash
canopyd uninstall
```

1. Stops daemon (`launchctl unload`)
2. Removes launchd plist
3. Removes shell hook lines from all rc files (detects by marker comments)
4. Removes `/usr/local/bin/canopyd`
5. Prompts: "Remove session history and config? (~/.config/canopy/) [y/N]"

### 3.4 Shell Hook — PTY Interposition

#### 3.4.1 Architecture

```
Without Canopy:
  Terminal.app  ←→  PTY  ←→  zsh  ←→  child processes (claude, npm, etc.)

With Canopy:
  Terminal.app  ←→  PTY_outer  ←→  canopyd attach (proxy)  ←→  PTY_inner  ←→  zsh  ←→  child processes
                                          │
                                          │ (copy of all I/O via Unix socket)
                                          ▼
                                    canopyd daemon
```

The user sees no difference. Terminal behaves identically — colors, interactive programs, tab completion, everything. The proxy is byte-level transparent.

#### 3.4.2 The `canopyd attach` Command

Implemented in Go using `github.com/creack/pty`.

**Startup:**

1. Parse arguments: `--session-id <uuid> -- <command> [args...]`
2. Connect to daemon via Unix domain socket at `$TMPDIR/canopyd.sock`
3. Send `session_register`: session ID, shell PID, TTY name, CWD, terminal dimensions, selected env vars, hostname, timestamp
4. Create PTY pair via `pty.Start()`
5. Match inner PTY terminal size to outer
6. Enter main I/O loop

**Four concurrent goroutines:**

```
Goroutine 1 — User Input:
  Read os.Stdin → write to PTY_inner → send copy to daemon (tagged "input")

Goroutine 2 — Terminal Output:
  Read PTY_inner → write to os.Stdout → send copy to daemon (tagged "output")

Goroutine 3 — Remote Input:
  Read from daemon (input from phone) → write to PTY_inner

Goroutine 4 — Signals:
  SIGWINCH → propagate resize to PTY_inner + notify daemon
  SIGTERM/SIGINT → forward to child process group
  SIGCHLD → read exit code, notify daemon, clean up, exit
```

**Unix socket frame format:**

```
[4 bytes: frame length (big-endian uint32)]
[1 byte: frame type]
[N bytes: payload]

Types:
  0x01 = session_register (JSON)
  0x02 = output_data (raw bytes)
  0x03 = input_data (raw bytes)
  0x04 = resize (JSON: {rows, cols})
  0x05 = session_end (JSON: {exit_code, ended_at})
  0x06 = remote_input (raw bytes from phone)
  0x07 = heartbeat (empty, every 5s)
```

**Performance:** < 1ms latency per I/O op. Socket copy is non-blocking with 1MB buffer — drops frames on overflow (never happens in practice; terminal I/O peaks at ~5MB/s during `cat` of large files, sustained < 100KB/s). ~5MB memory per `canopyd attach` process.

**Graceful degradation:** If daemon socket is unavailable, `canopyd attach` retries every 10s in background. All I/O continues flowing normally. User is never blocked. If `canopyd attach` crashes, child shell gets SIGHUP and the terminal window closes (same as if shell crashed — extremely rare).

#### 3.4.3 Session Metadata Enrichment

The daemon continuously enriches sessions:

**Foreground process detection:** Every 500ms, queries the process tree from the shell PID using `sysctl(KERN_PROC)` + `proc_pidpath()`. Maps process names to known tools:

| Process | Tool type | Enhanced parsing |
|---|---|---|
| `claude` | Claude Code | Yes — AI conversation model |
| `aider` | Aider | Yes — AI conversation model |
| `goose` | Goose | Yes — AI conversation model |
| `codex` | Codex CLI | Yes — AI conversation model |
| Everything else | Generic | Standard command/output model |

When the foreground process changes, the daemon emits a `process_change` event and activates/deactivates enhanced parsing.

**Working directory:** Read via `proc_pidinfo()` for the foreground process. Updated on process change.

### 3.5 Session Storage

```
~/.config/canopy/sessions/
  {session-id}/
    meta.json          # Session metadata (created on start, updated live)
    raw.log            # Complete raw PTY output (binary, includes ANSI codes)
    input.log          # Complete raw user input
    events.jsonl       # Structured events (append-only, one JSON per line)
```

#### 3.5.1 meta.json

```json
{
  "session_id": "f7a12c3e-4b56-4d89-a012-3c4d5e6f7890",
  "started_at": "2026-02-22T10:30:00-08:00",
  "ended_at": null,
  "shell": "/bin/zsh",
  "initial_cwd": "/Users/hadi/projects/sync",
  "current_cwd": "/Users/hadi/projects/sync/backend",
  "terminal_size": {"rows": 48, "cols": 120},
  "hostname": "hadis-macbook",
  "device_id": "a3f1c9b2",
  "current_process": "claude",
  "tool_type": "claude_code",
  "status": "active",
  "last_activity_at": "2026-02-22T11:45:30-08:00",
  "total_commands": 14,
  "title": "claude: fix auth bug in server.ts",
  "raw_log_bytes": 245832,
  "events_count": 87
}
```

**Title generation:** Auto-generated from first AI message (if AI tool detected) or first 2-3 commands. Examples: "claude: fix auth bug in server.ts", "npm install && npm build", "ssh production-server".

#### 3.5.2 events.jsonl — Universal Conversation Model

Every terminal interaction is modeled as a conversation. The fundamental unit is always the same: **the user says something, the system responds.** The event types below reflect this.

```jsonc
// ============================================================
// THE USER'S SIDE OF THE CONVERSATION
// ============================================================

// The user typed something. ALWAYS emitted for every input.
// This is the left-side "sent message" in the conversation.
{
  "type": "user_input",
  "ts": "2026-02-22T10:30:05.456Z",
  "text": "npm run build",
  "cwd": "/Users/hadi/projects/sync",
  "input_type": "command"  // "command" at shell prompt, "response" to interactive prompt, "ai_message" to AI tool
}

// ============================================================
// THE SYSTEM'S SIDE OF THE CONVERSATION
// ============================================================

// Output from a running command or tool. Chunked.
// This is the right-side "received message" in the conversation.
{
  "type": "system_output",
  "ts": "2026-02-22T10:30:06.789Z",
  "content": "Building project...\nCompiling 42 files...",
  "streaming": true  // true = more output coming, false = this chunk is final
}

// A command or operation completed.
// Rendered as a status badge on the preceding conversation block.
{
  "type": "completed",
  "ts": "2026-02-22T10:30:45.012Z",
  "exit_code": 0,
  "duration_ms": 39556
}

// The system is asking the user for input — an interactive prompt.
// Rendered as a prompt card with quick-action buttons if applicable.
{
  "type": "input_request",
  "ts": "2026-02-22T10:32:00.000Z",
  "prompt_text": "Are you sure you want to continue? [y/N]",
  "quick_actions": ["y", "N"],  // parsed from the prompt pattern, null if not detected
  "process": "npm"
}

// The shell is idle, showing its prompt. Waiting for user's next command.
// Rendered as a subtle "ready" indicator, not as a message.
{
  "type": "idle",
  "ts": "2026-02-22T10:31:00.000Z",
  "cwd": "/Users/hadi/projects/sync",
  "prompt_text": "❯"
}

// ============================================================
// AI TOOL ENHANCEMENTS
// When an AI coding tool is the foreground process, the parser
// emits these richer events IN ADDITION to the base events above.
// The phone uses these instead of raw system_output when available.
// ============================================================

// AI tool is generating a response (replaces system_output for display)
{
  "type": "ai_response",
  "ts": "2026-02-22T10:35:12.000Z",
  "content": "I'll look at the auth module. Let me read the file first.",
  "tool": "claude_code",
  "streaming": false
}

// AI tool is performing an action (file read, edit, command run, search)
{
  "type": "ai_action",
  "ts": "2026-02-22T10:35:14.000Z",
  "tool": "claude_code",
  "action": "read_file",         // read_file, edit_file, run_command, search, write_file
  "description": "Read server.ts",
  "detail": null,                 // populated on completion: file contents preview, command output, etc.
  "status": "running"             // running, done, error
}

// AI tool is requesting explicit approval (replaces generic input_request)
{
  "type": "ai_approval",
  "ts": "2026-02-22T10:35:30.000Z",
  "tool": "claude_code",
  "description": "Edit server.ts lines 40-45",
  "action": "edit_file",
  "diff": "- if (token.valid) {\n+ if (token.valid && !token.expired) {"
}

// Token/cost tracking
{
  "type": "ai_usage",
  "ts": "2026-02-22T10:36:01.000Z",
  "tool": "claude_code",
  "tokens_in": 1500,
  "tokens_out": 800,
  "cost_usd": 0.012
}

// ============================================================
// META EVENTS
// ============================================================

// Foreground process changed
{
  "type": "process_change",
  "ts": "2026-02-22T10:35:00.000Z",
  "process_name": "claude",
  "tool_type": "claude_code",
  "pid": 12345
}

// Session status changed
{
  "type": "status_change",
  "ts": "2026-02-22T10:35:02.000Z",
  "from": "idle",
  "to": "active"
}

// Input from a remote client (phone, collaborator)
{
  "type": "remote_input",
  "ts": "2026-02-22T10:36:00.000Z",
  "from_device": "hadi-iphone",
  "text": "y"
}
```

**Key design principle: AI events supplement, they don't replace.** When an AI tool is active, both `user_input`/`system_output` events AND `ai_response`/`ai_action`/`ai_approval` events are emitted from the same output stream. The phone app uses the AI events for richer rendering when available. If AI parsing fails for any line of output, the base `system_output` event is always there. Nothing is lost.

#### 3.5.3 Output Chunking

Raw terminal output arrives as a byte stream. Chunking strategy:

- Buffer for up to 200ms or 4KB of stripped text, whichever comes first
- Flush → strip ANSI → emit `system_output` event
- If stripped content is empty (only cursor movement), don't emit
- Set `streaming: true` while the command is still running, `streaming: false` on the final chunk before a `completed` or `idle` event

#### 3.5.4 Storage Management

| Setting | Default | Config key |
|---|---|---|
| Retention period | 30 days | `retention_days` |
| Max disk usage | 10 GB | `max_storage_gb` |
| Compress after | 24 hours | `compress_after_hours` |

**Compression:** Sessions older than 24h have `raw.log` and `input.log` gzip-compressed. `events.jsonl` stays uncompressed (small, needs to be searchable).

**Pruning:** When over `max_storage_gb`:
1. Delete `raw.log`/`input.log` for oldest sessions (keep `meta.json` + `events.jsonl`)
2. If still over, delete entire oldest session directories
3. Never delete sessions < 24h old

### 3.6 Parser Engine

#### 3.6.1 Pipeline

```
Raw PTY bytes
    ▼
┌─────────────────────┐
│  ANSI Stripping      │  State machine: removes SGR, cursor, screen control.
│                      │  Preserves OSC 133 markers as metadata.
│                      │  Retains newlines and printable text.
└─────────┬───────────┘
          ▼
┌─────────────────────┐
│  Line Accumulator    │  Buffers into complete lines.
│                      │  Handles \r (progress bar overwriting).
│                      │  Flushes on 500ms timeout (for prompts without \n).
│                      │  Tracks alternate screen mode (vim/less — pauses parsing).
└─────────┬───────────┘
          ▼
┌─────────────────────┐
│  Conversation        │  ALWAYS active.
│  Parser              │  Uses OSC 133 markers for reliable command boundaries.
│                      │  Correlates input stream with output stream.
│                      │  Emits: user_input, system_output, completed, input_request, idle.
└─────────┬───────────┘
          ▼
┌─────────────────────┐
│  AI Enhancement      │  Active only when known AI tool is foreground.
│  Layer               │  Parses the same output the conversation parser sees.
│                      │  Emits: ai_response, ai_action, ai_approval, ai_usage.
│                      │  These supplement (not replace) the base events.
└─────────┬───────────┘
          ▼
┌─────────────────────┐
│  Event Emitter       │  Writes to events.jsonl.
│                      │  Pushes to WebSocket subscribers.
│                      │  Triggers push notifications.
└─────────────────────┘
```

#### 3.6.2 ANSI Stripping

State machine processing byte-by-byte:

- `Normal`: emit printable chars. ESC (0x1B) → `Escape` state.
- `Escape`: `[` → `CSI`, `]` → `OSC`, other → discard and return to Normal.
- `CSI`: accumulate params until final byte (letter) → discard whole sequence. **Exception:** detect `\e[?1049h` (enter alternate screen) and `\e[?1049l` (leave) — set/clear a flag.
- `OSC`: accumulate until ST (ESC+`\` or BEL 0x07). **Exception:** OSC 133 markers → extract and pass as metadata to the parser (don't discard these).

Special chars: `\r` without `\n` → replace current line buffer from position 0 (progress bars). `\b` → remove previous char. `\t` → replace with spaces.

#### 3.6.3 Conversation Parser

This is the core parser. Always active, every session.

**State machine:**

```
IDLE          Shell prompt visible, waiting for user input
RUNNING       A command is executing
INPUT_WAIT    Running process asking for interactive input
```

**Transitions using OSC 133:**

- `OSC 133;A` received → emit `idle` event → state = `IDLE`
- `OSC 133;C` received → capture command text from input stream → emit `user_input` → state = `RUNNING`
- `OSC 133;D;{code}` received → emit `completed` with exit code → (followed by `133;A` which triggers `idle`)

**Input classification:**

The `user_input.input_type` field is set based on the parser's current context:
- `"command"` — user typed at a shell prompt (IDLE → RUNNING)
- `"response"` — user typed in response to an `input_request` (INPUT_WAIT → RUNNING)
- `"ai_message"` — user typed while an AI tool is the foreground process and the tool was waiting for user input

**Interactive input detection (→ INPUT_WAIT):**

While in RUNNING state, detect prompts via heuristics:
- Output ends with known patterns: `[y/N]`, `[Y/n]`, `password:`, `(yes/no)`, `Press any key`, `Enter`, `:`
- Output stopped for > 2 seconds, process still alive, last line doesn't end with `\n`
- Emit `input_request` with prompt text and parsed quick actions (extract bracketed options like `[y/N]` → `quick_actions: ["y", "N"]`)

**Quick action parsing:**

The parser extracts actionable options from prompt text using patterns:
- `[y/N]` → `["y", "N"]` (default highlighted by capitalization)
- `(yes/no)` → `["yes", "no"]`
- `[1/2/3]` → `["1", "2", "3"]`
- `Continue? (Y/n)` → `["Y", "n"]`
- No recognizable pattern → `quick_actions: null` (phone shows just a text input)

#### 3.6.4 AI Enhancement Layer

Activates when the conversation parser's `process_change` event indicates a known AI tool.

**Claude Code parser:**

| Pattern | Event |
|---|---|
| User types at Claude Code's prompt | `user_input` with `input_type: "ai_message"` (from base parser) |
| Claude Code text response | `ai_response` (accumulated, replaces `system_output` for display) |
| `⏺ Read {filepath}` | `ai_action` (action: "read_file") |
| `⏺ Edit {filepath}` | `ai_action` (action: "edit_file") |
| `⏺ Run {command}` | `ai_action` (action: "run_command") |
| `⏺ Search` | `ai_action` (action: "search") |
| `⏺ Write {filepath}` | `ai_action` (action: "write_file") |
| Diff lines (`+`/`-` prefixed) | Captured into preceding `ai_action.detail` or `ai_approval.diff` |
| `Do you want to proceed?` / `(y/n)` while tool was doing an edit | `ai_approval` (replaces generic `input_request`) |
| Token/cost display (`\d+[kM]? tokens`, `\$\d+\.\d+`) | `ai_usage` |
| Return to Claude Code's input prompt | Base parser emits `idle`, AI layer emits `ai_response` final |

**Aider parser:** Similar structure. User prompt: `> `. Edit blocks: search/replace format. Git operations. Cost display.

**Goose, Codex CLI:** TBD — implement after Claude Code parser is stable.

**Failure mode:** If the AI parser encounters unrecognized output, it does nothing. The base `system_output` events are always emitted regardless. Nothing is ever lost. The phone app displays base events for anything the AI parser didn't enhance.

#### 3.6.5 Parser Testing

**Record real sessions:** `canopyd debug record <file>` captures a session's raw PTY output. Build test suite from real sessions — several for each tool type.

**Replay-based tests:** Replay recordings through the parser, assert on emitted events.

**Fuzz testing:** Random byte sequences must never panic the parser.

**Remote parser updates:** The daemon can fetch updated parsing patterns from `https://canopy.dev/parsers/latest.json` without a full daemon update. These supplement compiled-in parsers. Addresses AI tool output format changes.

### 3.7 Networking — WireGuard Endpoint

The daemon runs a userspace WireGuard endpoint using `wireguard-go` (the same library Tailscale uses).

#### 3.7.1 WireGuard Configuration

On startup, the daemon:

1. Reads its WireGuard private key from `~/.config/canopy/wg_private.key`
2. Starts a userspace WireGuard interface listening on a UDP port (default: 51820, falls back to random port if taken)
3. Assigns itself a private IP from the `100.100.0.0/16` range (deterministic from device ID hash)
4. Configures allowed peers from `devices.json` (each paired phone has its WireGuard public key stored)
5. Registers its current public endpoint (IP + port) with the coordination server

The WebSocket API server listens on the WireGuard interface's private IP, port 19876. It's only reachable through the tunnel — not exposed on any public interface.

#### 3.7.2 Coordination Server Check-In

Every 30 seconds and on network change, the daemon sends a heartbeat to the coordination server:

```json
POST https://coord.canopy.dev/v1/checkin
{
  "device_key": "<ed25519_public_key>",
  "wg_public_key": "<wireguard_public_key>",
  "endpoints": [
    {"ip": "203.0.113.42", "port": 51820},       // public-facing endpoint (from STUN)
    {"ip": "192.168.1.100", "port": 51820}        // local network endpoint
  ],
  "timestamp": "2026-02-22T10:00:00Z",
  "sig": "<ed25519_signature_of_body>"
}
```

The daemon discovers its own public endpoint via a STUN request to the coordination server (the coordination server embeds STUN functionality).

### 3.8 WebSocket API

The daemon runs a WebSocket server at `ws://100.100.x.x:19876/ws`, reachable only through the WireGuard tunnel.

Since all traffic goes through WireGuard (which provides encryption and authentication), the WebSocket layer does not need its own encryption. The WireGuard key exchange during pairing ensures that only paired devices can establish a tunnel.

#### 3.8.1 Client → Daemon Messages

```jsonc
// List sessions
{
  "type": "list_sessions",
  "filter": {
    "status": ["active", "idle", "approval_needed"],
    "include_ended": false,
    "since": "2026-02-22T00:00:00Z"
  },
  "limit": 50,
  "offset": 0
}

// Subscribe to real-time events
{"type": "subscribe", "session_id": "f7a12c3e-..."}

// Unsubscribe
{"type": "unsubscribe", "session_id": "f7a12c3e-..."}

// Get historical events
{
  "type": "get_history",
  "session_id": "f7a12c3e-...",
  "since": "2026-02-22T10:00:00Z",
  "limit": 200
}

// Send text input (written to PTY as keystrokes + newline)
{"type": "input", "session_id": "f7a12c3e-...", "text": "npm run build"}

// Send raw bytes (for y/n, control characters)
{"type": "input_raw", "session_id": "f7a12c3e-...", "bytes_b64": "eQo="}

// Send signal
{"type": "signal", "session_id": "f7a12c3e-...", "signal": "SIGINT"}

// Read file from Mac
{
  "type": "read_file",
  "path": "/Users/hadi/projects/sync/server.ts",
  "max_bytes": 1048576
}

// Search session history
{
  "type": "search_sessions",
  "query": "auth bug",
  "date_range": {"from": "2026-02-01T00:00:00Z", "to": "2026-02-22T23:59:59Z"},
  "limit": 20
}

// Ping
{"type": "ping"}

// Get daemon info
{"type": "get_info"}
```

#### 3.8.2 Daemon → Client Messages

```jsonc
// Session list
{
  "type": "session_list",
  "sessions": [{
    "session_id": "f7a12c3e-...",
    "status": "approval_needed",
    "tool_type": "claude_code",
    "current_process": "claude",
    "title": "claude: fix auth bug",
    "cwd": "/Users/hadi/projects/sync",
    "started_at": "2026-02-22T10:30:00Z",
    "last_activity_at": "2026-02-22T11:45:30Z",
    "hostname": "hadis-macbook",
    "preview": "Edit server.ts lines 40-45 — waiting for approval",
    "total_commands": 14,
    "connected_clients": 2
  }],
  "total": 8
}

// Real-time event push (any event from §3.5.2)
{"type": "event", "session_id": "f7a12c3e-...", "event": { /* ... */ }}

// Session status changed (pushed to all connected clients regardless of subscription)
{
  "type": "session_status",
  "session_id": "f7a12c3e-...",
  "status": "approval_needed",
  "previous_status": "active",
  "detail": "Edit server.ts lines 40-45"
}

// New session / session ended
{"type": "session_started", "session": { /* same as session_list item */ }}
{"type": "session_ended", "session_id": "f7a12c3e-...", "ended_at": "...", "last_exit_code": 0}

// History response
{"type": "history", "session_id": "...", "events": [/* ... */], "has_more": true, "next_cursor": "..."}

// File contents
{"type": "file_contents", "path": "...", "content": "...", "language": "typescript", "size_bytes": 4521}

// Search results
{
  "type": "search_results",
  "query": "auth bug",
  "results": [{
    "session_id": "...", "title": "...", "started_at": "...",
    "matches": [{"event_type": "user_input", "ts": "...", "snippet": "...fix the auth bug..."}]
  }]
}

// Info
{"type": "info", "hostname": "hadis-macbook", "device_id": "a3f1c9b2", "version": "1.0.0", "active_sessions": 3}

// Error
{"type": "error", "code": "session_not_found", "message": "..."}

// Pong
{"type": "pong"}
```

#### 3.8.3 Collaboration

Multiple clients connecting to the same daemon:

- All clients receive `session_status` events for all sessions, regardless of subscription.
- Only subscribed clients receive `event` pushes for a specific session.
- When any client sends `input`, the daemon writes to PTY and emits `remote_input` to all OTHER subscribed clients.
- `connected_clients` count is tracked and included in session status.
- No locking — multiple clients can type simultaneously. The daemon serializes input in arrival order. Same as multiple users SSH'd into the same tmux session.

#### 3.8.4 Catch-Up on Reconnect

When a client reconnects:
1. Send `list_sessions` to refresh
2. For last-viewed session, send `get_history` with `since` = timestamp of last known event
3. Re-subscribe

If the client fell too far behind (> 1000 buffered events), the daemon sends `{"type": "sync_lost", "session_id": "...", "resume_from": "..."}` and the client does a full catch-up.

#### 3.8.5 Rate Limits

| Resource | Limit |
|---|---|
| Connected clients per daemon | 10 |
| Messages/second per client | 100 |
| File reads/minute | 10 |
| Max file read size | 1 MB |
| History requests/minute | 30 |
| Max events per history request | 1000 |

### 3.9 Push Notifications

#### 3.9.1 Triggers

| Trigger | Default | Description |
|---|---|---|
| `ai_approval` event emitted | ON | AI tool waiting for approval |
| `completed` with non-zero exit_code | OFF | Command failed |
| `completed` after > 60s duration | OFF | Long command finished |
| `status_change` to `error` | ON | Session error |
| Output matches custom keyword | OFF | User-defined keyword alert |

#### 3.9.2 Flow

1. Daemon detects triggerable event.
2. Sends push trigger to coordination server (which forwards to APNs):

```json
POST https://coord.canopy.dev/v1/push
{
  "device_key": "<ed25519_public_key>",
  "sig": "<signature>",
  "targets": [{
    "apns_token": "<hex>",
    "notification": {
      "title": "Claude Code needs approval",
      "subtitle": "hadis-macbook",
      "body": "Edit server.ts lines 40-45",
      "category": "APPROVAL_REQUEST",
      "thread_id": "<session_id>",
      "data": {"session_id": "...", "mac_device_id": "a3f1c9b2", "event_type": "ai_approval"}
    }
  }]
}
```

3. Coordination server validates signature, sends APNs notification.

**Privacy:** Push payload contains only vague notification text ("Edit server.ts") and routing metadata. No conversation content, file contents, or raw terminal output.

#### 3.9.3 iOS Notification Categories

```swift
// Registered in app at launch
"APPROVAL_REQUEST" → actions: ["Approve" (auth required), "Reject" (auth required, destructive)]
"SESSION_ALERT"    → actions: ["Open" (foreground)]
```

Tapping "Approve" / "Reject" on lock screen: iOS grants ~30s background execution. App establishes WireGuard tunnel, connects WebSocket, sends `input_raw` with "y\n" or "n\n", disconnects. User never opens the app.

### 3.10 CLI Commands

```
canopyd daemon start|stop|status|ping|restart

canopyd pair [--timeout 300]
canopyd devices
canopyd devices remove|rename <id>

canopyd sessions [--all] [--json]
canopyd sessions info|events|kill <id>

canopyd storage status|prune|export <id>

canopyd config get|set|list|reset

canopyd shell-hook <zsh|bash|fish>

canopyd attach --session-id <id> -- <command> [args...]

canopyd update [--check]
canopyd version
canopyd uninstall

canopyd debug record|replay <file>
```

### 3.11 Configuration

`~/.config/canopy/config.json`:

```jsonc
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
```

---

## 4. Coordination Server

### 4.1 Purpose

A lightweight service that helps devices find each other and establish direct connections. It is NOT a message relay for session data in normal operation.

**Three responsibilities:**

1. **Endpoint exchange:** Devices check in with their current public IP + port. When a phone wants to connect to a Mac, it asks the coordination server for the Mac's latest endpoint.
2. **STUN:** Helps devices discover their own public endpoint (NAT-mapped IP + port).
3. **TURN fallback:** When direct P2P fails (symmetric NAT, ~20% of cases), relays encrypted WireGuard UDP packets. The coordination server sees only encrypted WireGuard traffic — it cannot decrypt anything.
4. **Push forwarding:** Receives push triggers from Mac daemons and forwards to APNs.

### 4.2 Architecture

A Go service deployed on Fly.io. Stateless (device registrations stored in Redis for multi-instance; in-memory for single-instance MVP).

### 4.3 API

#### Device Check-In

```
POST /v1/checkin
{
  "device_key": "<ed25519_pub>",
  "wg_public_key": "<wg_pub>",
  "endpoints": [
    {"ip": "203.0.113.42", "port": 51820, "type": "public"},
    {"ip": "192.168.1.100", "port": 51820, "type": "local"}
  ],
  "paired_devices": ["<peer_wg_pub_1>", "<peer_wg_pub_2>"],
  "apns_tokens": ["<hex>"],   // for phones only
  "timestamp": "...",
  "sig": "<ed25519_sig>"
}
```

#### Endpoint Lookup

```
GET /v1/endpoints?peer_wg_key=<base64>
Authorization: Bearer <signed_token>

→ {
    "endpoints": [
      {"ip": "203.0.113.42", "port": 51820, "type": "public", "last_seen": "..."},
      {"ip": "192.168.1.100", "port": 51820, "type": "local", "last_seen": "..."}
    ],
    "online": true
  }
```

#### STUN

Standard STUN protocol over UDP on port 3478. Devices send STUN binding request, server responds with the device's public IP + port as seen by the server.

#### TURN Relay

Standard TURN protocol over UDP. Allocated when devices detect they cannot establish direct P2P (after trying direct connection for 5 seconds). TURN relays only encrypted WireGuard UDP packets — the coordination server cannot decrypt them.

**TURN allocation limits:** 10 concurrent allocations per device. 1 hour max per allocation (re-allocate if needed). Bandwidth limit: 10 Mbps per allocation (more than enough for terminal data).

#### Push Forwarding

```
POST /v1/push
{
  "device_key": "<ed25519_pub>",
  "sig": "<signature>",
  "targets": [{ "apns_token": "...", "notification": { ... } }]
}
```

Validates signature, sends APNs notifications via HTTP/2 provider API with token-based auth.

### 4.4 Pairing Registration

When pairing completes, both devices tell the coordination server about the pairing:

```
POST /v1/register_pairing
{
  "device_key": "<my_ed25519_pub>",
  "peer_wg_key": "<peer_wg_pub>",
  "sig": "<signature>"
}
```

The coordination server only allows endpoint lookups and TURN relay between devices that have registered pairings. Unpaired devices cannot discover each other.

### 4.5 Connection Flow (Detailed)

When the iPhone app wants to connect to a Mac:

```
Phone                    Coord Server                    Mac
  │                           │                           │
  │  1. STUN request          │                           │
  │  ─────────────────────►   │                           │
  │  ◄─────────────────────   │                           │
  │     Your public IP:port   │                           │
  │                           │                           │
  │  2. Endpoint lookup       │                           │
  │     for Mac's WG key      │                           │
  │  ─────────────────────►   │                           │
  │  ◄─────────────────────   │                           │
  │     Mac endpoints:        │                           │
  │     public + local        │                           │
  │                           │                           │
  │  3. Try direct WireGuard handshake                    │
  │  ─────────────────────────────────────────────────►   │
  │  ◄─────────────────────────────────────────────────   │
  │     (if direct works, done — P2P established)         │
  │                           │                           │
  │  4. If direct fails after 5s, request TURN            │
  │  ─────────────────────►   │                           │
  │  ◄─────────────────────   │                           │
  │     TURN allocation       │                           │
  │                           │                           │
  │  5. WireGuard through TURN relay                      │
  │  ─────────────────────►   │  ─────────────────────►   │
  │  ◄─────────────────────   │  ◄─────────────────────   │
  │     (encrypted, coord     │                           │
  │      can't read it)       │                           │
```

Once the WireGuard tunnel is up (direct or relayed), the phone has a private IP route to the Mac's WireGuard address. The WebSocket connection to `ws://100.100.x.x:19876/ws` works as if both devices were on the same LAN.

### 4.6 Cost

- Fly.io `shared-cpu-1x`, 256MB RAM: ~$2/month
- TURN bandwidth: terminal data is tiny. Estimate 100KB/hour per active session. Even at 1000 users with 5 sessions relayed: ~360GB/month = ~$7/month.
- Redis (Upstash free tier for MVP): $0
- **Total MVP estimate: ~$10-15/month**

### 4.7 Security

| Protection | Implementation |
|---|---|
| Device authentication | Ed25519 signature on all requests |
| Pairing enforcement | Endpoint lookup and TURN only between registered pairs |
| No session data visibility | TURN relays encrypted WireGuard packets — opaque |
| Rate limiting | 100 check-ins/minute per device, 10 TURN allocations per device |
| DDoS protection | Fly.io built-in + connection rate limits |

---

## 5. iOS App ("Canopy")

### 5.1 Tech Stack

| Technology | Purpose |
|---|---|
| Swift 6 (strict concurrency) | Language |
| SwiftUI | UI framework |
| iOS 17.0+ | Minimum deployment target |
| Network Extension (Packet Tunnel) | WireGuard VPN tunnel |
| WireGuard-kit (or wireguard-apple) | WireGuard implementation for iOS |
| URLSessionWebSocketTask | WebSocket over the tunnel |
| CryptoKit | Ed25519 signing, key generation |
| SwiftData | Local cache, device storage |
| AVFoundation | QR scanner for pairing |
| UserNotifications | Push handling, actionable notifications |
| NWPathMonitor | Network change detection |
| BackgroundTasks | Background refresh |

### 5.2 Network Extension — WireGuard Tunnel

The app includes a **Packet Tunnel Provider** extension (a separate target in Xcode) that manages the WireGuard tunnel.

**How it works:**

1. The Packet Tunnel Provider is a system-level extension that runs independently of the main app.
2. When activated, it creates a virtual network interface and establishes WireGuard connections to all paired Macs.
3. It sets up routing so that the private IP range `100.100.0.0/16` is routed through the tunnel.
4. The main app communicates with the extension via the `NETunnelProviderManager` API.
5. iOS manages the extension lifecycle — it can keep the tunnel alive even when the app is backgrounded (unlike a plain WebSocket, which iOS kills after ~30s).

**VPN configuration:**

The app programmatically creates a VPN configuration:

```swift
let manager = NETunnelProviderManager()
manager.localizedDescription = "Canopy"
manager.protocolConfiguration = {
    let proto = NETunnelProviderProtocol()
    proto.providerBundleIdentifier = "dev.canopy.app.tunnel"
    proto.serverAddress = "Canopy Tunnel"  // display only
    proto.providerConfiguration = [
        "wg_private_key": "<base64>",
        "peers": [
            [
                "public_key": "<mac_wg_pub>",
                "endpoint": "<ip:port>",
                "allowed_ips": "100.100.x.x/32",
                "persistent_keepalive": 25
            ]
        ]
    ]
    return proto
}()
manager.isEnabled = true
manager.isOnDemandEnabled = true
manager.onDemandRules = [NEOnDemandRuleConnect()]  // always-on
```

**Always-on behavior:**

With `isOnDemandEnabled = true` and a `NEOnDemandRuleConnect` rule, iOS automatically (re)establishes the tunnel whenever network connectivity is available. The user sees "Canopy" as a VPN in Settings → VPN. The tunnel stays up across app foreground/background transitions, WiFi ↔ cellular switches, and device sleep/wake.

This is the critical advantage over a plain WebSocket: **the tunnel persists in the background.** When a push notification triggers background execution, the tunnel is already up — the app can immediately send a WebSocket message through it without waiting for a connection.

**Multi-Mac peer configuration:**

Each paired Mac is a separate WireGuard peer in the tunnel config. Adding a new Mac means adding a new peer entry and updating the VPN configuration:

```swift
// In PacketTunnelProvider
func addPeer(publicKey: String, endpoint: String, allowedIP: String) {
    // Add WireGuard peer to the active tunnel
    // Update routing table
}
```

### 5.3 Project Structure

```
Canopy/
├── App/
│   ├── CanopyApp.swift                    # Entry, deep links
│   ├── AppState.swift                     # @Observable root state
│   └── AppDelegate.swift                  # Push, background tasks
│
├── PacketTunnel/                          # Separate target: Network Extension
│   ├── PacketTunnelProvider.swift          # WireGuard tunnel provider
│   ├── WireGuardAdapter.swift             # wireguard-apple integration
│   └── TunnelConfiguration.swift          # Peer/endpoint management
│
├── Models/
│   ├── MacDevice.swift                    # hostname, device_id, wg keys
│   ├── Session.swift                      # Terminal session model
│   ├── SessionEvent.swift                 # All event types (enum + associated values)
│   ├── SessionStatus.swift                # active, idle, approval_needed, ended, error
│   └── ToolType.swift                     # claude_code, aider, generic, etc.
│
├── Networking/
│   ├── TunnelManager.swift                # NETunnelProviderManager lifecycle
│   ├── CoordinationClient.swift           # Talks to coord.canopy.dev (endpoint lookup, STUN)
│   ├── CanopyConnection.swift             # WebSocket client per Mac
│   ├── ConnectionManager.swift            # Orchestrates all Mac connections
│   ├── PairingManager.swift               # QR parsing, key exchange, device registration
│   └── MessageRouter.swift                # Deserializes events, routes to stores
│
├── Stores/
│   ├── DeviceStore.swift                  # Paired Macs (SwiftData + Keychain)
│   ├── SessionStore.swift                 # Session list (merged from all Macs)
│   └── EventStore.swift                   # Per-session event history + cache
│
├── Views/
│   ├── Onboarding/
│   │   ├── WelcomeView.swift
│   │   └── SetupGuideView.swift
│   │
│   ├── Devices/
│   │   ├── DeviceListView.swift
│   │   ├── AddDeviceView.swift            # QR scanner + manual code
│   │   └── DeviceDetailView.swift
│   │
│   ├── Sessions/
│   │   ├── SessionListView.swift          # Main screen
│   │   ├── SessionRowView.swift
│   │   └── EmptySessionsView.swift
│   │
│   ├── Conversation/
│   │   ├── ConversationView.swift         # Universal conversation view
│   │   ├── UserInputBubble.swift          # "You typed: npm run build"
│   │   ├── SystemOutputBlock.swift        # Command output card
│   │   ├── CompletedBadge.swift           # ✓ exit 0 / ✗ exit 1 + duration
│   │   ├── InputRequestCard.swift         # Interactive prompt + quick actions
│   │   ├── AIResponseBlock.swift          # AI text with markdown
│   │   ├── AIActionCard.swift             # Tool use (read/edit/run) collapsible
│   │   ├── AIApprovalBanner.swift         # Approve / Reject buttons
│   │   ├── ProcessChangeDivider.swift     # "— claude started —"
│   │   ├── RemoteInputIndicator.swift     # "Alice sent: y"
│   │   ├── InputBar.swift                 # Text input + Ctrl+C
│   │   └── DiffView.swift                 # Inline diff
│   │
│   ├── FileViewer/
│   │   ├── FileViewerSheet.swift
│   │   └── SyntaxHighlighter.swift
│   │
│   ├── History/
│   │   ├── HistoryListView.swift
│   │   └── HistorySearchView.swift
│   │
│   └── Settings/
│       ├── SettingsView.swift
│       ├── NotificationSettingsView.swift
│       └── ConnectionSettingsView.swift
│
├── Services/
│   ├── PushNotificationService.swift
│   ├── HapticService.swift
│   └── DeepLinkService.swift
│
└── Utilities/
    ├── MarkdownRenderer.swift
    ├── DateFormatter+Relative.swift
    ├── LanguageDetector.swift
    └── Keychain.swift
```

### 5.4 The Conversation View — Universal Design

Every terminal session is displayed as a conversation. There is one view, one model.

#### 5.4.1 How Everything Maps to Messages

```
┌──────────────────────────────────────────┐
│  ← hadis-macbook           ● connected   │
│    ~/projects/sync                        │
├──────────────────────────────────────────┤
│                                          │
│                    ┌─────────────────┐   │
│                    │ npm install     │   │  ← user_input (input_type: "command")
│                    └─────────────────┘   │
│                                          │
│  ┌──────────────────────────────────┐    │
│  │ added 142 packages in 3.2s      │    │  ← system_output
│  │                           ✓ 3s  │    │  ← completed (exit 0)
│  └──────────────────────────────────┘    │
│                                          │
│                    ┌─────────────────┐   │
│                    │ npm run build   │   │  ← user_input (input_type: "command")
│                    └─────────────────┘   │
│                                          │
│  ┌──────────────────────────────────┐    │
│  │ > sync@1.0.0 build              │    │  ← system_output (streaming)
│  │ > tsc && node build.js          │    │
│  │                                  │    │
│  │ src/auth.ts(42,5): error TS2345 │    │
│  │ Argument of type 'string' is    │    │
│  │ not assignable to...            │    │
│  │                                  │    │
│  │ Found 1 error.                   │    │
│  │                          ✗ 1 17s│    │  ← completed (exit 1)
│  └──────────────────────────────────┘    │
│                                          │
│                    ┌─────────────────┐   │
│                    │ claude          │   │  ← user_input (command to start claude)
│                    └─────────────────┘   │
│                                          │
│   — Claude Code started —                │  ← process_change
│                                          │
│                    ┌─────────────────┐   │
│                    │ fix the auth    │   │  ← user_input (input_type: "ai_message")
│                    │ bug in server.ts│   │
│                    └─────────────────┘   │
│                                          │
│  I'll look at the auth module.           │  ← ai_response
│  Let me read the file first.             │
│                                          │
│  ┌─ 📄 Read server.ts ──────────┐       │  ← ai_action (read_file)
│  │ 245 lines                  ▶  │       │
│  └───────────────────────────────┘       │
│                                          │
│  The issue is on line 42. The token      │  ← ai_response
│  validation doesn't check expiration.    │
│                                          │
│  ┌─ ✏️ Edit server.ts ──────────┐       │  ← ai_action (edit_file) + ai_approval
│  │ Lines 40-45                ▶  │       │
│  │                                │       │
│  │  - if (token.valid) {          │       │
│  │  + if (token.valid &&          │       │
│  │  +     !token.expired()) {     │       │
│  └────────────────────────────────┘       │
│                                          │
│ ╔════════════════════════════════════════╗│
│ ║  Apply edit to server.ts?              ║│  ← ai_approval rendered as banner
│ ║                                        ║│
│ ║    [ ✗ Reject ]    [ ✓ Approve ]      ║│
│ ╚════════════════════════════════════════╝│
│                                          │
│  ┌─────────────────────────────────────┐ │
│  │  Type a message...          ⌃C   ➤  │ │
│  └─────────────────────────────────────┘ │
└──────────────────────────────────────────┘
```

#### 5.4.2 Rendering Rules

| Event | Rendering |
|---|---|
| `user_input` | Right-aligned bubble. Monospace if `input_type` is "command" or "response". Regular font if "ai_message". |
| `system_output` | Left-aligned block. Monospace, subtle dark background. Collapsed if > 20 lines (tap to expand). Shows live streaming indicator when `streaming: true`. |
| `completed` | Badge on the preceding `system_output` block. Green check + "3s" for exit 0. Red X + "exit 1 • 17s" for non-zero. |
| `input_request` | Card at bottom of conversation with prompt text. If `quick_actions` present, render as buttons. Always shows text input. |
| `idle` | Subtle ready indicator (thin line + CWD label). Not rendered as a distinct message. |
| `ai_response` | Left-aligned text with markdown rendering. Code blocks get monospace + syntax highlighting. Replaces `system_output` display when available. |
| `ai_action` | Collapsible card. Icon per action type (📄 read, ✏️ edit, ⚡ run, 🔍 search). Tap to expand: file contents, full diff, or command output. File paths are tappable → opens File Viewer. |
| `ai_approval` | Sticky banner above input bar. Diff preview. Large Approve (green) / Reject (red) buttons (min 44x44pt). Haptic on appear + action. |
| `ai_usage` | Subtle inline text: "1.5k tokens • $0.01". |
| `process_change` | Thin divider: "— claude started —" or "— returned to shell —". |
| `remote_input` | Subtle inline label: "Alice sent: y". |

**Key principles:**

- **User input always on the right.** Whether it's `npm install` or "fix the auth bug" — it's something the user said, right-aligned.
- **System response always on the left.** Whether it's build output or Claude Code's prose — it's the system responding, left-aligned.
- **AI events replace, not supplement, in the UI.** When `ai_response` events are available for a span of output, the UI renders those instead of the corresponding `system_output` events. The `system_output` events still exist in the data layer as fallback.
- **No toggle, no mode switch.** The conversation view seamlessly transitions between shell commands and AI conversations as the session progresses. A session might start with `cd project && npm install`, then transition to `claude` for an AI conversation, then back to shell commands. All of it renders as one continuous conversation.

#### 5.4.3 Input Bar

Always visible at the bottom of the conversation.

- Text input field (standard iOS keyboard)
- Send button (➤) — sends `input` message (appends `\n` automatically)
- Ctrl+C button (⌃C) — sends `signal: SIGINT`
- The input bar adapts to context:
  - When `input_request` with `quick_actions` is active: shows quick-action buttons above the text field
  - When `ai_approval` is active: approval banner appears above the input bar
  - When session is `idle`: text field placeholder says "Type a command..."
  - When AI tool is active and waiting: placeholder says "Type a message..."

### 5.5 Session List

```
┌──────────────────────────────────────────┐
│  Canopy                        ⚙️  [+]   │
├──────────────────────────────────────────┤
│                                          │
│  ⚡ NEEDS ATTENTION                       │
│  ┌──────────────────────────────────┐    │
│  │ 🟡 claude                 2m ago │    │
│  │    hadis-macbook                 │    │
│  │    Approve edit: server.ts?      │    │
│  └──────────────────────────────────┘    │
│                                          │
│  ● RUNNING                               │
│  ┌──────────────────────────────────┐    │
│  │ 🟢 npm run build         just now│    │
│  │    hadis-macbook                 │    │
│  │    Compiling 42 files...         │    │
│  └──────────────────────────────────┘    │
│  ┌──────────────────────────────────┐    │
│  │ 🟢 docker compose up     12m ago│    │
│  │    work-mac                      │    │
│  │    3 containers running          │    │
│  └──────────────────────────────────┘    │
│                                          │
│  ○ IDLE                                  │
│  ┌──────────────────────────────────┐    │
│  │ ⚫ zsh                     1h ago│    │
│  │    hadis-macbook                 │    │
│  │    ~/projects/sync $             │    │
│  └──────────────────────────────────┘    │
│                                          │
│            ┌──────────────┐              │
│            │  📜 History   │              │
│            └──────────────┘              │
└──────────────────────────────────────────┘
```

**Grouping:** Needs Attention (🟡 amber tint) → Running (🟢) → Idle (⚫). Within groups, sorted by last activity.

**Session row:** Status dot, foreground process name, Mac hostname, relative time, preview text.

**Interactions:** Tap → conversation view. Pull to refresh. Swipe left → mute / end session. [+] → add Mac. ⚙️ → settings.

### 5.6 History

Accessed via [History] button on session list.

- Past sessions grouped by date (Today, Yesterday, then by date)
- Each row: tool/process, duration, title, Mac hostname, command count
- Tap → conversation view (read-only, no input bar)
- Search: full-text, query sent to Mac daemon
- Loaded on-demand from Mac (not stored locally in full)

### 5.7 File Viewer

Sheet (half-screen slide-up):
- Read-only
- Line numbers + basic syntax highlighting (regex per language)
- Monospace, horizontal scroll
- Pinch-to-zoom
- Max 1MB (daemon enforced)
- Tappable from any file path in conversation

### 5.8 Settings

- **Paired Macs:** list with connection status, add/remove/rename
- **Notifications:** per-trigger toggles (approval, error, completion, custom keyword)
- **Security:** Face ID requirement + lock timeout
- **About:** version, device ID

### 5.9 Local Caching

| Data | Storage | Retention |
|---|---|---|
| Paired devices + keys | SwiftData + Keychain | Permanent |
| Session list snapshot | SwiftData | Refresh on connect |
| Last-viewed session events | In-memory | Until dismissed |
| 10 most recent sessions | SwiftData cache | 7 days |
| File contents | In-memory | Until sheet dismissed |

Mac daemon is always authoritative. Phone cache exists for UI smoothness only.

### 5.10 Accessibility

- Full VoiceOver: all elements labeled. Command blocks: "Command: npm run build, status: running, 45 seconds." Approval: "Approval needed: Edit server.ts. Approve. Reject."
- Dynamic Type: all text scales. Code minimum 11pt.
- Min 44x44pt tap targets. Approve/Reject: full-width, 56pt height.
- Status via labels + color + shape (not color alone).

---

## 6. Security Model

### 6.1 Threat Model

| Threat | Protection |
|---|---|
| Coordination server compromise | WireGuard tunnel is end-to-end encrypted with keys from pairing. Coord server never has WireGuard private keys. Even with full server access, attacker sees only encrypted packets. |
| Network eavesdropping | WireGuard (ChaCha20-Poly1305) encrypts all traffic. |
| Lost iPhone | Face ID gate. WireGuard private key in Keychain (hardware-encrypted, requires device passcode). |
| Unauthorized pairing | Requires physical access to Mac (`canopyd pair`). 6-digit code, 5-min expiry, 3 attempts max. |
| Replay attacks | WireGuard handles this natively (replay protection built into protocol). |
| MITM during pairing | QR code contains Mac's WireGuard public key. Phone verifies during handshake. |

### 6.2 Pairing Protocol

```
Mac                              iPhone
 │                                  │
 │  1. canopyd pair                 │
 │     Generate 6-digit code        │
 │     QR contains: {               │
 │       code: "482917",            │
 │       wg_pub: <mac_wg_public>,   │
 │       identity: <mac_ed25519_pub>│
 │       coord: "coord.canopy.dev"  │
 │       endpoints: [ip:port, ...]  │
 │     }                            │
 │                                  │
 │  2.                    Scan QR   │
 │                                  │
 │  3.  ◄── pairing_request ───────│
 │     (via coord server or direct) │
 │     {code, phone_wg_pub,         │
 │      phone_ed25519_pub}          │
 │                                  │
 │  4. Verify code                  │
 │     Add phone as WireGuard peer  │
 │                                  │
 │  5.  ─── pairing_confirmed ────►│
 │     {mac hostname, device_id}    │
 │                                  │
 │  6. Both store peer's WG pub key │
 │     WireGuard tunnel established │
```

After pairing, WireGuard handles all encryption and authentication. No additional application-layer crypto is needed.

### 6.3 Mac Security

| Asset | Protection |
|---|---|
| WireGuard private key | `~/.config/canopy/wg_private.key` (mode 0600) |
| Identity key | `~/.config/canopy/identity.key` (mode 0600) |
| Paired device data | `~/.config/canopy/devices.json` (mode 0600) |
| Unix socket | `$TMPDIR/canopyd.sock` (per-user TMPDIR on macOS) |
| WebSocket API | Only on WireGuard interface (100.100.x.x) — not exposed publicly |
| File access | Restricted to `file_access_root` (default: home dir). Symlinks not followed outside root. |

### 6.4 iPhone Security

| Asset | Protection |
|---|---|
| WireGuard private key | iOS Keychain (`kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly`) |
| Identity key | iOS Keychain |
| Cached events | SwiftData with NSFileProtectionComplete |
| App access | Optional Face ID / Touch ID gate |

---

## 7. Development Phases

### Phase 1 — Foundation (Weeks 1–4)

**Goal:** Install on Mac, pair iPhone on same network, see all terminal sessions as conversations, send commands from phone.

**Mac daemon:**
- [ ] Go project setup, CI/CD, release builds (arm64 + amd64)
- [ ] `canopyd attach`: PTY proxy, bidirectional I/O, SIGWINCH, Unix socket comm
- [ ] Session registry + metadata
- [ ] Shell hook generation (zsh, bash, fish)
- [ ] Shell integration markers (OSC 133 via precmd/preexec)
- [ ] Raw output + input logging
- [ ] ANSI stripper + line accumulator
- [ ] Conversation parser (OSC 133, command boundaries, exit codes, output chunking, input_request detection, quick_action parsing)
- [ ] events.jsonl storage
- [ ] WebSocket API (list, subscribe, events, history, input, signal, read_file, search, info)
- [ ] Install script (binary, config, hooks, launchd)
- [ ] `canopyd pair` (6-digit code, no QR yet)
- [ ] `canopyd daemon start/stop/status/ping`
- [ ] `canopyd sessions` / `canopyd uninstall`
- [ ] WireGuard userspace endpoint (wireguard-go) — local network only for Phase 1

**iOS app:**
- [ ] Xcode project, SwiftUI, iOS 17
- [ ] Manual code pairing + WireGuard key exchange
- [ ] WireGuard Network Extension (Packet Tunnel Provider)
- [ ] WebSocket connection over tunnel
- [ ] Session list view (grouped by status)
- [ ] Conversation view — universal: UserInputBubble, SystemOutputBlock, CompletedBadge, InputRequestCard with quick actions
- [ ] Input bar (text + send + Ctrl+C)
- [ ] Basic reconnection

**Milestone:** 3 terminal windows open on Mac. All appear as conversations on iPhone. `npm install` shows as: user typed "npm install" → system responded with output → ✓ exit 0. Type `echo hello` from phone, see it execute.

### Phase 2 — Tunnel, Push, AI Enhancement (Weeks 5–8)

**Goal:** Works from anywhere. Push notifications. AI coding sessions get rich cards.

**Coordination server:**
- [ ] Go service: device check-in, endpoint storage, endpoint lookup
- [ ] STUN functionality (public endpoint discovery)
- [ ] TURN relay (encrypted WireGuard packet forwarding)
- [ ] Pairing registration
- [ ] Push forwarding (APNs HTTP/2)
- [ ] Deploy to Fly.io

**Mac daemon additions:**
- [ ] Coordination server check-in (endpoint registration, STUN)
- [ ] NAT traversal: direct P2P with TURN fallback
- [ ] Push triggers (ai_approval, errors → coord server → APNs)
- [ ] Claude Code parser: ai_response, ai_action (read/edit/run/search), ai_approval + diff extraction, ai_usage
- [ ] Aider parser (basic)
- [ ] `canopyd pair` with QR code (terminal-rendered block chars)

**iOS app additions:**
- [ ] Coordination client (endpoint lookup, STUN)
- [ ] Tunnel manager: NAT traversal, direct P2P + TURN fallback
- [ ] QR scanner for pairing
- [ ] Push registration + notification handling
- [ ] Lock screen Approve/Reject actions (background execution)
- [ ] Conversation view AI enhancements: AIResponseBlock (markdown), AIActionCard (collapsible, diffs), AIApprovalBanner (haptic)
- [ ] ProcessChangeDivider ("— claude started —")
- [ ] Network change detection → tunnel re-establishment

**Milestone:** Mac at home, phone on cellular. All sessions visible. Claude Code asks for approval → push notification → tap Approve from lock screen without opening app.

### Phase 3 — Multi-Mac, History, Collaboration (Weeks 9–12)

**Goal:** Full feature set. Production polish.

**Multi-Mac:**
- [ ] iOS: multiple WireGuard peers in tunnel config
- [ ] iOS: ConnectionManager handles N simultaneous Mac connections
- [ ] iOS: unified session list across all Macs
- [ ] iOS: device indicator per session

**History:**
- [ ] Daemon: full-text search across events.jsonl
- [ ] iOS: history list (grouped by date, on-demand loading)
- [ ] iOS: history search
- [ ] iOS: read-only conversation view for ended sessions

**Collaboration:**
- [ ] Daemon: track connected clients, broadcast remote_input
- [ ] iOS: RemoteInputIndicator, connected_clients badge

**File viewer:**
- [ ] iOS: FileViewerSheet with syntax highlighting
- [ ] iOS: tappable file paths in conversation

**Storage:**
- [ ] Daemon: retention pruning, compression, disk cap
- [ ] `canopyd storage` commands

**Polish:**
- [ ] Pull to refresh, swipe actions, empty states
- [ ] Haptic feedback, app badge count
- [ ] Deep links from notifications
- [ ] Settings views
- [ ] `caffeinate` integration
- [ ] Auto-update check

**Milestone:** 2 Macs paired. All sessions in one list. Browse last week's sessions. Search "deploy." Two phones on same Mac — both see session, one approves.

### Phase 4 — Launch (Weeks 13–16)

- [ ] Home screen widget (WidgetKit)
- [ ] iPad layout
- [ ] `canopyd update` self-update
- [ ] Remote parser config updates
- [ ] Goose, Codex CLI parsers
- [ ] Custom keyword notification triggers
- [ ] Security audit
- [ ] Performance testing (50 concurrent sessions)
- [ ] Accessibility audit
- [ ] TestFlight beta
- [ ] App Store submission
- [ ] canopy.dev landing page
- [ ] Documentation

---

## 8. Technical Decisions & Rationale

**Go for the daemon:** Single binary, no dependencies. Excellent concurrency for managing dozens of sessions. `creack/pty` for PTY. `wireguard-go` for the tunnel. Cross-compile for arm64/amd64.

**Swift/SwiftUI for iOS:** Native Network Extension (required for VPN tunnel). Native Keychain. SwiftUI's reactive model fits streaming events. Better battery than RN/Electron.

**PTY interposition over tmux:** Zero friction. User doesn't install or learn anything. Every session captured automatically. Works with any terminal emulator.

**Native WireGuard tunnel over relay service:** The tunnel persists in the background (Network Extension survives app backgrounding). Direct P2P in most cases (no intermediary). The coordination server is much thinner than a full message relay — it just helps devices find each other. WireGuard is battle-tested, audited crypto.

**Universal conversation model over raw terminal:** Every terminal interaction IS a conversation. Mapping commands and output to a chat-like model isn't a lossy abstraction — it's a more readable representation. AI tool sessions naturally fit the same model. One view, one paradigm, always.

**events.jsonl over SQLite:** Append-only, human-readable, corruption-resistant. Terminal data volumes are small (few MB per session). Grep-style search is sufficient at this scale.

**OSC 133 shell integration markers:** Same standard as VS Code and iTerm2. Provides reliable command boundary detection, exit codes, working directory — without brittle regex heuristics.

---

## 9. Open Questions

1. **iOS Network Extension approval:** Apple reviews VPN apps carefully. Ensure compliance with App Store guidelines for VPN/Network Extension usage. File for the entitlement early.

2. **WireGuard key rotation:** Should keys be rotated periodically? WireGuard supports seamless key rotation. Plan for a rotation mechanism even if not in v1.

3. **Process detection on macOS:** Verify `sysctl(KERN_PROC)` + `proc_pidpath()` reliability for foreground process detection. Test edge cases: background jobs, piped commands, subshells.

4. **PTY interposition compatibility:** Exhaustive testing: ssh, tmux (nested), vim/nvim, docker exec -it, screen, mosh, fzf, atuin, zsh autocomplete.

5. **Session naming:** Auto-generation good enough, or manual rename from phone?

6. **Cost model:** Daemon always free. Coordination server costs ~$15/month at small scale. Subscription? One-time app purchase? Free with generous limits?

7. **iPad:** Same app with adaptive layout? Or separate submission?

8. **Android / Linux:** Out of scope for v1. Daemon is cross-platform (Go). Android would need a separate app but same protocol.

---

## 10. Success Metrics

| Metric | Target |
|---|---|
| Install → first session on phone | < 3 minutes |
| Push notification → approval sent | < 3 seconds |
| Event latency (Mac → iPhone, direct P2P) | < 100ms |
| Event latency (Mac → iPhone, TURN relay) | < 300ms |
| Tunnel uptime when both devices online | 99.9% |
| Sessions per Mac | 50+ concurrent |
| Conversation parser accuracy (command detection) | 95%+ |
| Claude Code parser accuracy | 90%+ |
| Cold launch → session list | < 2 seconds |
| Tunnel re-establishment after network change | < 5 seconds |

---

*End of specification.*