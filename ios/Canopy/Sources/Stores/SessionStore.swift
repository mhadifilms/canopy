import Foundation
import OSLog

private let logger = Logger(subsystem: "dev.canopy.app", category: "SessionStore")

/// Maintains the merged session list from all connected Macs.
///
/// Sessions are grouped by status for the session list view:
/// 1. Needs attention (approval_needed, error)
/// 2. Running (active)
/// 3. Idle
/// 4. Ended (only if requested)
@MainActor
@Observable
final class SessionStore {

    /// All sessions keyed by session ID.
    private(set) var sessions: [String: Session] = [:]

    /// Timestamp of last full refresh per device.
    private var lastRefresh: [String: Date] = [:]

    // MARK: - Grouped accessors

    /// Sessions needing user attention (approval or error), sorted by most recent activity.
    var needsAttention: [Session] {
        sessions.values
            .filter { $0.status == .approvalNeeded || $0.status == .error }
            .sorted { ($0.lastActivityAt ?? $0.startedAt) > ($1.lastActivityAt ?? $1.startedAt) }
    }

    /// Active/running sessions, sorted by most recent activity.
    var running: [Session] {
        sessions.values
            .filter { $0.status == .active }
            .sorted { ($0.lastActivityAt ?? $0.startedAt) > ($1.lastActivityAt ?? $1.startedAt) }
    }

    /// Idle sessions, sorted by most recent activity.
    var idle: [Session] {
        sessions.values
            .filter { $0.status == .idle }
            .sorted { ($0.lastActivityAt ?? $0.startedAt) > ($1.lastActivityAt ?? $1.startedAt) }
    }

    /// Ended sessions, sorted by most recent activity.
    var ended: [Session] {
        sessions.values
            .filter { $0.status == .ended }
            .sorted { ($0.lastActivityAt ?? $0.startedAt) > ($1.lastActivityAt ?? $1.startedAt) }
    }

    /// All live (non-ended) sessions in display order.
    var liveSessions: [Session] {
        needsAttention + running + idle
    }

    /// Total count of sessions needing attention (for badge).
    var attentionCount: Int {
        needsAttention.count
    }

    /// Number of live (non-ended) sessions for a specific device.
    func sessionCount(for deviceId: String) -> Int {
        sessions.values.filter { $0.macDeviceId == deviceId && $0.status != .ended }.count
    }

    // MARK: - Message handlers

    /// Replace the session list for a specific device.
    func handleSessionList(_ incoming: [Session], from deviceId: String) {
        // Remove old sessions from this device
        let existingIds = sessions.values
            .filter { $0.macDeviceId == deviceId }
            .map(\.sessionId)
        for id in existingIds {
            sessions.removeValue(forKey: id)
        }

        // Insert new sessions, tagged with device ID
        for var session in incoming {
            session.macDeviceId = deviceId
            sessions[session.sessionId] = session
        }

        lastRefresh[deviceId] = Date()
        logger.info("Updated session list from \(deviceId): \(incoming.count) sessions")
    }

    /// Handle a new session appearing.
    func handleSessionStarted(_ session: Session, from deviceId: String) {
        var tagged = session
        tagged.macDeviceId = deviceId
        sessions[session.sessionId] = tagged
        logger.info("Session started: \(session.sessionId) on \(deviceId)")
    }

    /// Handle a session ending.
    func handleSessionEnded(sessionId: String, endedAt: Date, exitCode: Int?) {
        guard var session = sessions[sessionId] else { return }
        session.status = .ended
        session.lastActivityAt = endedAt
        sessions[sessionId] = session
        logger.info("Session ended: \(sessionId)")
    }

    /// Handle a status change for a session.
    func handleStatusChange(sessionId: String, status: SessionStatus, detail: String?) {
        guard var session = sessions[sessionId] else { return }
        session.status = status
        session.lastActivityAt = Date()
        if let detail {
            session.preview = detail
        }
        sessions[sessionId] = session
    }

    /// Remove all sessions for a disconnected device.
    func removeSessionsForDevice(_ deviceId: String) {
        let toRemove = sessions.values
            .filter { $0.macDeviceId == deviceId }
            .map(\.sessionId)
        for id in toRemove {
            sessions.removeValue(forKey: id)
        }
        lastRefresh.removeValue(forKey: deviceId)
    }

    /// Look up which device owns a session.
    func deviceId(for sessionId: String) -> String? {
        sessions[sessionId]?.macDeviceId
    }
}
