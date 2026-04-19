import Foundation
import OSLog

private let logger = Logger(subsystem: "dev.canopy.app", category: "AppState")

/// Root application state.
///
/// Owns all stores, the connection manager, the push notification service, and
/// the tunnel manager. Injected into the SwiftUI environment as an
/// `@Observable` object.
@MainActor
@Observable
final class AppState {

    let sessionStore: SessionStore
    let eventStore: EventStore
    let router: MessageRouter
    let connectionManager: ConnectionManager

    #if os(iOS)
    /// Push notification service — owns APNs registration, categories, and
    /// the approve/reject action pipeline from the lock screen.
    let pushService: PushNotificationService
    #endif

    /// The currently selected/viewed session ID.
    var selectedSessionId: String?

    /// Whether the onboarding flow should be shown.
    var showOnboarding: Bool = false

    /// A session ID from a deep link or notification tap, consumed by the view layer.
    var pendingDeepLinkSessionId: String?

    /// Paired Mac devices (loaded from Keychain/persistent storage).
    private(set) var pairedDevices: [MacDevice] = []

    /// File contents response from the daemon, keyed by path.
    private(set) var fileContentsCache: [String: DaemonMessage.FileContentsPayload] = [:]

    /// Error message for a failed file read, keyed by path.
    private(set) var fileErrors: [String: String] = [:]

    /// Latest search results from all connected Macs.
    private(set) var searchResults: [DaemonMessage.SearchResultsPayload.SearchResult] = []

    /// Whether a search is currently in progress.
    private(set) var isSearching: Bool = false

    init() {
        let sessionStore = SessionStore()
        let eventStore = EventStore()
        let router = MessageRouter(sessionStore: sessionStore, eventStore: eventStore)
        let connectionManager = ConnectionManager(router: router)

        self.sessionStore = sessionStore
        self.eventStore = eventStore
        self.router = router
        self.connectionManager = connectionManager
        #if os(iOS)
        self.pushService = PushNotificationService()
        #endif

        // Wire up router callbacks. The `self?` guard is intentional: during
        // app teardown the router can outlive AppState (e.g. a final message
        // arrives while the scene is tearing down); in that case we drop
        // the payload silently, which is correct.
        router.onFileContents = { [weak self] payload in
            self?.handleFileContents(payload)
        }
        router.onSearchResults = { [weak self] payload in
            self?.handleSearchResults(payload)
        }

        #if os(iOS)
        // Route lock-screen approve/reject actions through AppState so the
        // normal approveAction/rejectAction code paths are reused.
        pushService.onApprovalAction = { [weak self] sessionId, approved, _ in
            guard let self else { return }
            if approved {
                await self.approveAction(for: sessionId)
            } else {
                await self.rejectAction(for: sessionId)
            }
        }
        pushService.onOpenSession = { [weak self] sessionId, _ in
            Task { @MainActor in
                self?.pendingDeepLinkSessionId = sessionId
                self?.selectedSessionId = sessionId
            }
        }
        #endif
    }

    // MARK: - Device management

    /// Add a newly paired device and connect to it.
    func addPairedDevice(_ device: MacDevice) {
        guard !pairedDevices.contains(where: { $0.deviceId == device.deviceId }) else { return }
        pairedDevices.append(device)
        connectionManager.addDevice(device)
        savePairedDevices()
        logger.info("Paired with \(device.hostname) (\(device.deviceId))")
    }

    /// Remove a paired device.
    func removePairedDevice(_ deviceId: String) {
        connectionManager.removeDevice(deviceId)
        sessionStore.removeSessionsForDevice(deviceId)
        pairedDevices.removeAll { $0.deviceId == deviceId }
        savePairedDevices()
        logger.info("Unpaired device \(deviceId)")
    }

    /// Load saved devices and establish connections on app launch.
    func loadAndConnect() {
        loadPairedDevices()

        if pairedDevices.isEmpty {
            showOnboarding = true
        } else {
            for device in pairedDevices {
                connectionManager.addDevice(device)
            }
        }
    }

    // MARK: - Session interaction

    /// Subscribe to events for the selected session.
    func subscribeToSession(_ sessionId: String) async {
        guard let deviceId = sessionStore.deviceId(for: sessionId) else { return }
        do {
            try await connectionManager.subscribe(sessionId: sessionId, on: deviceId)

            // Request recent history if we have no events cached
            if eventStore.events(for: sessionId).isEmpty {
                let historyMsg = ClientMessage.getHistory(
                    .init(sessionId: sessionId, since: nil, limit: 200)
                )
                try await connectionManager.send(historyMsg, to: deviceId)
            }
        } catch {
            logger.error("Failed to subscribe to session \(sessionId): \(error)")
        }
    }

    /// Load history for an ended session (no subscription needed).
    func loadHistory(for sessionId: String) async {
        guard let deviceId = sessionStore.deviceId(for: sessionId) else { return }
        do {
            let historyMsg = ClientMessage.getHistory(
                .init(sessionId: sessionId, since: nil, limit: 500)
            )
            try await connectionManager.send(historyMsg, to: deviceId)
        } catch {
            logger.error("Failed to load history for session \(sessionId): \(error)")
        }
    }

    /// Unsubscribe from a session's events.
    func unsubscribeFromSession(_ sessionId: String) async {
        guard let deviceId = sessionStore.deviceId(for: sessionId) else { return }
        do {
            try await connectionManager.unsubscribe(sessionId: sessionId, on: deviceId)
        } catch {
            logger.error("Failed to unsubscribe from session \(sessionId): \(error)")
        }
    }

    /// Send text input to the currently selected session.
    func sendInput(_ text: String, to sessionId: String) async {
        guard let deviceId = sessionStore.deviceId(for: sessionId) else { return }
        do {
            try await connectionManager.sendInput(text, sessionId: sessionId, on: deviceId)
        } catch {
            logger.error("Failed to send input to session \(sessionId): \(error)")
        }
    }

    /// Send SIGINT to a session.
    func sendInterrupt(to sessionId: String) async {
        guard let deviceId = sessionStore.deviceId(for: sessionId) else { return }
        do {
            try await connectionManager.sendSignal("SIGINT", sessionId: sessionId, on: deviceId)
        } catch {
            logger.error("Failed to send SIGINT to session \(sessionId): \(error)")
        }
    }

    /// Approve an AI action by sending "y\n" as raw input.
    func approveAction(for sessionId: String) async {
        guard let deviceId = sessionStore.deviceId(for: sessionId) else { return }
        let yesBytes = Data("y\n".utf8).base64EncodedString()
        do {
            try await connectionManager.sendRawInput(yesBytes, sessionId: sessionId, on: deviceId)
            #if os(iOS)
            HapticService.success()
            #endif
        } catch {
            #if os(iOS)
            HapticService.error()
            #endif
            logger.error("Failed to send approval for session \(sessionId): \(error)")
        }
    }

    /// Reject an AI action by sending "n\n" as raw input.
    func rejectAction(for sessionId: String) async {
        guard let deviceId = sessionStore.deviceId(for: sessionId) else { return }
        let noBytes = Data("n\n".utf8).base64EncodedString()
        do {
            try await connectionManager.sendRawInput(noBytes, sessionId: sessionId, on: deviceId)
            #if os(iOS)
            HapticService.success()
            #endif
        } catch {
            #if os(iOS)
            HapticService.error()
            #endif
            logger.error("Failed to send rejection for session \(sessionId): \(error)")
        }
    }

    // MARK: - File reading

    /// Request file contents from the Mac that owns the given session.
    func readFile(path: String, forSession sessionId: String) async {
        guard let deviceId = sessionStore.deviceId(for: sessionId) else { return }

        // Clear previous state for this path
        fileContentsCache.removeValue(forKey: path)
        fileErrors.removeValue(forKey: path)

        do {
            let msg = ClientMessage.readFile(.init(path: path, maxBytes: 1_000_000))
            try await connectionManager.send(msg, to: deviceId)
        } catch {
            fileErrors[path] = error.localizedDescription
            logger.error("Failed to request file \(path): \(error)")
        }
    }

    /// Handle incoming file contents from the daemon.
    private func handleFileContents(_ payload: DaemonMessage.FileContentsPayload) {
        fileContentsCache[payload.path] = payload
    }

    // MARK: - Search

    /// Search sessions across all connected Macs.
    func searchSessions(query: String) async {
        guard !query.isEmpty else { return }
        isSearching = true
        searchResults = []

        let msg = ClientMessage.searchSessions(.init(query: query, limit: 50))
        for deviceId in connectionManager.connectedDeviceIds {
            do {
                try await connectionManager.send(msg, to: deviceId)
            } catch {
                logger.error("Failed to send search to \(deviceId): \(error)")
            }
        }

        // Timeout: if no results arrive within 5 seconds, stop searching
        Task {
            try? await Task.sleep(for: .seconds(5))
            if isSearching {
                isSearching = false
            }
        }
    }

    /// Handle incoming search results from the daemon.
    private func handleSearchResults(_ payload: DaemonMessage.SearchResultsPayload) {
        // Merge results, avoiding duplicates by session ID
        let existingIds = Set(searchResults.map(\.sessionId))
        let newResults = payload.results.filter { !existingIds.contains($0.sessionId) }
        searchResults.append(contentsOf: newResults)
        isSearching = false
    }

    // MARK: - Persistence

    private func savePairedDevices() {
        do {
            let data = try JSONEncoder().encode(pairedDevices)
            try KeychainHelper.save(data, forKey: "dev.canopy.paired_devices")
        } catch {
            logger.error("Failed to save paired devices: \(error)")
        }
    }

    private func loadPairedDevices() {
        do {
            if let data = try KeychainHelper.load(forKey: "dev.canopy.paired_devices") {
                pairedDevices = try JSONDecoder().decode([MacDevice].self, from: data)
            }
        } catch {
            logger.error("Failed to load paired devices: \(error)")
        }
    }
}
