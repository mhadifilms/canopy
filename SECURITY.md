# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in Canopy, please report it responsibly.

**Email:** security@canopy.dev

Please include:

- Description of the vulnerability
- Steps to reproduce
- Affected component (daemon, iOS app, coordination server)
- Impact assessment if possible

You should receive an acknowledgment within 48 hours. We will work with you to understand the issue and coordinate a fix before any public disclosure.

## Security Architecture

Canopy is designed with the assumption that all networks are hostile:

- **End-to-end encryption.** All communication between the Mac daemon and iOS app uses WireGuard (Curve25519, ChaCha20-Poly1305). The coordination server never sees plaintext session data.

- **No cloud storage.** Session data is stored only on your Mac's local disk. Nothing is uploaded to any server.

- **Minimal coordination server.** The coordination server only facilitates device discovery (endpoint exchange) and relays encrypted packets when direct P2P fails. It cannot decrypt any traffic.

- **Device authentication.** Devices authenticate using Ed25519 identity keypairs generated locally during setup. Pairing requires physical QR code scanning.

## Scope

The following are in scope for security reports:

- Authentication or authorization bypasses
- Encryption weaknesses or key management issues
- Remote code execution
- Information disclosure (session data leaks)
- Privilege escalation in the daemon
- Weaknesses in the pairing protocol
- Coordination server vulnerabilities

Out of scope:

- Denial of service attacks
- Issues requiring physical access to an already-unlocked device
- Social engineering
