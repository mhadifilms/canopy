import Foundation
import OSLog

private let logger = Logger(subsystem: "dev.canopy.app", category: "EventStore")

/// In-memory store for per-session events.
///
/// Holds events for currently viewed and recently accessed sessions.
/// Events are evicted when memory pressure or session count exceeds limits.
@MainActor
@Observable
final class EventStore {

    /// Events keyed by session ID, in chronological order.
    private(set) var eventsBySession: [String: [SessionEvent]] = [:]

    /// Whether more historical events are available (server-side).
    private var hasMoreHistory: [String: Bool] = [:]

    /// Cursor for fetching the next page of history.
    private var historyCursors: [String: String] = [:]

    /// Maximum number of sessions to keep events for in memory.
    private let maxCachedSessions = 10

    /// Access order for LRU eviction.
    private var accessOrder: [String] = []

    // MARK: - Public API

    /// Get all events for a session. Returns empty array if not loaded.
    func events(for sessionId: String) -> [SessionEvent] {
        touchSession(sessionId)
        return eventsBySession[sessionId] ?? []
    }

    /// Whether more historical events are available for a session.
    func hasMore(for sessionId: String) -> Bool {
        hasMoreHistory[sessionId] ?? true
    }

    /// The cursor for fetching the next page of history.
    func cursor(for sessionId: String) -> String? {
        historyCursors[sessionId]
    }

    /// The timestamp of the most recent event for a session, if any.
    func lastEventTimestamp(for sessionId: String) -> Date? {
        eventsBySession[sessionId]?.last?.timestamp
    }

    // MARK: - Mutation

    /// Append a single real-time event.
    func appendEvent(_ event: SessionEvent, for sessionId: String) {
        touchSession(sessionId)
        if eventsBySession[sessionId] == nil {
            eventsBySession[sessionId] = []
        }
        eventsBySession[sessionId]?.append(event)
    }

    /// Handle a history response from the daemon.
    func handleHistory(events: [SessionEvent], for sessionId: String, hasMore: Bool, cursor: String?) {
        touchSession(sessionId)

        if eventsBySession[sessionId] == nil {
            eventsBySession[sessionId] = events
        } else {
            // Prepend historical events before existing real-time events
            eventsBySession[sessionId]?.insert(contentsOf: events, at: 0)
        }

        hasMoreHistory[sessionId] = hasMore
        if let cursor {
            historyCursors[sessionId] = cursor
        }

        logger.info("Loaded \(events.count) historical events for \(sessionId), hasMore=\(hasMore)")
    }

    /// Clear all events for a session (e.g., after sync_lost).
    func clearEvents(for sessionId: String) {
        eventsBySession.removeValue(forKey: sessionId)
        hasMoreHistory.removeValue(forKey: sessionId)
        historyCursors.removeValue(forKey: sessionId)
    }

    /// Clear all stored events.
    func clearAll() {
        eventsBySession.removeAll()
        hasMoreHistory.removeAll()
        historyCursors.removeAll()
        accessOrder.removeAll()
    }

    // MARK: - LRU management

    private func touchSession(_ sessionId: String) {
        accessOrder.removeAll { $0 == sessionId }
        accessOrder.append(sessionId)
        evictIfNeeded()
    }

    private func evictIfNeeded() {
        while accessOrder.count > maxCachedSessions {
            let oldest = accessOrder.removeFirst()
            eventsBySession.removeValue(forKey: oldest)
            hasMoreHistory.removeValue(forKey: oldest)
            historyCursors.removeValue(forKey: oldest)
            logger.info("Evicted events for session \(oldest)")
        }
    }
}
