import Foundation

/// A terminal session running on a paired Mac.
/// Fields match the daemon's `session_list` response in section 3.8.2.
struct Session: Codable, Identifiable, Sendable, Hashable {
    let sessionId: String
    var status: SessionStatus
    var toolType: ToolType?
    var currentProcess: String?
    var title: String?
    var cwd: String?
    let startedAt: Date
    var lastActivityAt: Date?
    let hostname: String
    var preview: String?
    var totalCommands: Int
    var connectedClients: Int

    /// The device ID of the Mac this session belongs to (set client-side).
    var macDeviceId: String?

    var id: String { sessionId }

    enum CodingKeys: String, CodingKey {
        case sessionId = "session_id"
        case status
        case toolType = "tool_type"
        case currentProcess = "current_process"
        case title
        case cwd
        case startedAt = "started_at"
        case lastActivityAt = "last_activity_at"
        case hostname
        case preview
        case totalCommands = "total_commands"
        case connectedClients = "connected_clients"
        case macDeviceId = "mac_device_id"
    }
}
