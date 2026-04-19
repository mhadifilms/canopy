import Foundation
import CryptoKit

/// Shared configuration constants for the iOS client.
///
/// Centralising these values avoids force-unwraps at each call site and gives
/// us a single place to swap URLs between production, staging, and local dev.
enum CanopyConfig {

    /// Production coordination server.
    static let defaultCoordURLString = "https://coord.canopy.dev"

    /// Optional override stored in UserDefaults under this key (used by
    /// ConnectionSettingsView so developers can point at a local coord).
    static let coordURLOverrideKey = "dev.canopy.coord_url"

    /// Resolves the coordination server URL, honouring any override stored in
    /// UserDefaults. Falls back to `defaultCoordURLString`. This never returns
    /// `nil` — the fallback URL is statically known-valid.
    static func coordURL() -> URL {
        if let override = UserDefaults.standard.string(forKey: coordURLOverrideKey),
           !override.isEmpty,
           let url = URL(string: override) {
            return url
        }
        // `defaultCoordURLString` is a compile-time constant verified by tests;
        // force-unwrap here is acceptable because it would crash on first
        // launch in any environment that typoed the constant.
        return URL(string: defaultCoordURLString)!
    }

    /// WebSocket port the Mac daemon listens on for the API.
    /// Must match `listen_port` in the daemon config (default 19876).
    static let defaultDaemonPort = 19876

    /// Derive a deterministic 100.100.x.y tunnel IP from a base64-encoded
    /// WireGuard public key. Used to route packets through the VPN tunnel to
    /// a specific paired Mac. Matches the daemon's allocation scheme.
    static func tunnelIP(fromWGPublicKeyBase64 wgKey: String) -> String {
        guard let keyData = Data(base64Encoded: wgKey) else {
            return "100.100.0.1"
        }
        let bytes = Array(SHA256.hash(data: keyData))
        return "100.100.\(bytes[0]).\(max(1, bytes[1]))"
    }
}
