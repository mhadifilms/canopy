import Foundation
import OSLog

private let logger = Logger(subsystem: "dev.canopy.app", category: "MessageRouter")

/// Decodes incoming daemon messages and routes them to the appropriate stores.
@MainActor
final class MessageRouter: Sendable {

    private let sessionStore: SessionStore
    private let eventStore: EventStore

    /// Callback for file_contents responses. Keyed by file path.
    var onFileContents: ((DaemonMessage.FileContentsPayload) -> Void)?

    /// Callback for search_results responses.
    var onSearchResults: ((DaemonMessage.SearchResultsPayload) -> Void)?

    init(sessionStore: SessionStore, eventStore: EventStore) {
        self.sessionStore = sessionStore
        self.eventStore = eventStore
    }

    /// Route a decoded daemon message from a specific Mac.
    func route(_ message: DaemonMessage, from deviceId: String) {
        switch message {

        case .sessionList(let payload):
            sessionStore.handleSessionList(payload.sessions, from: deviceId)

        case .event(let payload):
            eventStore.appendEvent(payload.event, for: payload.sessionId)

        case .sessionStatus(let payload):
            sessionStore.handleStatusChange(
                sessionId: payload.sessionId,
                status: payload.status,
                detail: payload.detail
            )

        case .sessionStarted(let payload):
            sessionStore.handleSessionStarted(payload.session, from: deviceId)

        case .sessionEnded(let payload):
            sessionStore.handleSessionEnded(
                sessionId: payload.sessionId,
                endedAt: payload.endedAt,
                exitCode: payload.lastExitCode
            )

        case .history(let payload):
            eventStore.handleHistory(
                events: payload.events,
                for: payload.sessionId,
                hasMore: payload.hasMore,
                cursor: payload.nextCursor
            )

        case .fileContents(let payload):
            onFileContents?(payload)

        case .searchResults(let payload):
            onSearchResults?(payload)

        case .info(let payload):
            logger.info("Daemon info from \(deviceId): \(payload.hostname) v\(payload.version), \(payload.activeSessions) active sessions")

        case .error(let payload):
            logger.error("Daemon error from \(deviceId): [\(payload.code)] \(payload.message)")

        case .pong:
            break

        case .syncLost(let payload):
            logger.warning("Sync lost for session \(payload.sessionId), must re-fetch from \(payload.resumeFrom)")
            eventStore.clearEvents(for: payload.sessionId)
        }
    }
}
