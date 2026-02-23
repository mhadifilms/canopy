import Foundation

/// A paired Mac running the canopyd daemon.
struct MacDevice: Codable, Identifiable, Sendable, Hashable {
    var id: String { deviceId }

    let hostname: String
    let deviceId: String
    let wgPublicKey: String
    let identityPublicKey: String
    let tunnelIP: String
    var isOnline: Bool
    var lastSeen: Date?

    enum CodingKeys: String, CodingKey {
        case hostname
        case deviceId = "device_id"
        case wgPublicKey = "wg_public_key"
        case identityPublicKey = "identity_public_key"
        case tunnelIP = "tunnel_ip"
        case isOnline = "is_online"
        case lastSeen = "last_seen"
    }
}
