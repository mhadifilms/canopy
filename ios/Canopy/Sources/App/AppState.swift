import Foundation
import OSLog

private let logger = Logger(subsystem: "dev.canopy.app", category: "AppState")

/// Root application state.
///
/// Owns all stores and the connection manager. Injected into the SwiftUI
/// environment as an `@Observable` object.
@MainActor
@Observable
final class AppState {

    let sessionStore: SessionStore
    let eventStore: EventStore
    let router: MessageRouter
    let connectionManager: ConnectionManager

    /// The currently selected/viewed session ID.
    var selectedSessionId: String?

    /// Whether the onboarding flow should be shown.
    var showOnboarding: Bool = false

    /// Paired Mac devices (loaded from Keychain/persistent storage).
    private(set) var pairedDevices: [MacDevice] = []

    init() {
        let sessionStore = SessionStore()
        let eventStore = EventStore()
        let router = MessageRouter(sessionStore: sessionStore, eventStore: eventStore)
        let connectionManager = ConnectionManager(router: router)

        self.sessionStore = sessionStore
        self.eventStore = eventStore
        self.router = router
        self.connectionManager = connectionManager
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
        } catch {
            logger.error("Failed to send approval for session \(sessionId): \(error)")
        }
    }

    /// Reject an AI action by sending "n\n" as raw input.
    func rejectAction(for sessionId: String) async {
        guard let deviceId = sessionStore.deviceId(for: sessionId) else { return }
        let noBytes = Data("n\n".utf8).base64EncodedString()
        do {
            try await connectionManager.sendRawInput(noBytes, sessionId: sessionId, on: deviceId)
        } catch {
            logger.error("Failed to send rejection for session \(sessionId): \(error)")
        }
    }

    // MARK: - Persistence (placeholder — real implementation uses Keychain)

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
