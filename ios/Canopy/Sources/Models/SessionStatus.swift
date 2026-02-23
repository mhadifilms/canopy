import Foundation

/// The current status of a terminal session.
enum SessionStatus: String, Codable, Sendable, Hashable {
    case active
    case idle
    case approvalNeeded = "approval_needed"
    case ended
    case error
}
