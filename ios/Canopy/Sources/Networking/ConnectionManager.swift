import Foundation
import OSLog

private let logger = Logger(subsystem: "dev.canopy.app", category: "ConnectionManager")

/// Manages WebSocket connections to multiple Macs.
///
/// Owns one `CanopyConnection` per paired device. Routes incoming messages
/// to the `MessageRouter` for dispatch to stores.
@MainActor
@Observable
final class ConnectionManager {

    private(set) var connections: [String: CanopyConnection] = [:]
    private(set) var connectionStates: [String: ConnectionState] = [:]

    private let router: MessageRouter

    init(router: MessageRouter) {
        self.router = router
    }

    /// Whether any connection is currently active.
    var hasActiveConnection: Bool {
        connectionStates.values.contains(.connected)
    }

    /// All device IDs that are currently connected.
    var connectedDeviceIds: [String] {
        connectionStates.compactMap { id, state in
            if case .connected = state { return id } else { return nil }
        }
    }

    // MARK: - Lifecycle

    /// Add a Mac device and connect to it.
    func addDevice(_ device: MacDevice) {
        guard connections[device.deviceId] == nil else {
            logger.info("Device \(device.deviceId) already has a connection")
            return
        }

        let connection = CanopyConnection(device: device)

        connection.onMessage = { [weak self] message in
            Task { @MainActor in
                self?.router.route(message, from: device.deviceId)
            }
        }

        connection.onStateChange = { [weak self] state in
            Task { @MainActor in
                self?.connectionStates[device.deviceId] = state
                logger.info("Device \(device.hostname) state: \(String(describing: state))")
            }
        }

        connections[device.deviceId] = connection
        connectionStates[device.deviceId] = .disconnected
        connection.connect()
    }

    /// Disconnect and remove a Mac device.
    func removeDevice(_ deviceId: String) {
        connections[deviceId]?.disconnect()
        connections.removeValue(forKey: deviceId)
        connectionStates.removeValue(forKey: deviceId)
    }

    /// Reconnect a specific device.
    func reconnect(_ deviceId: String) {
        guard let connection = connections[deviceId] else { return }
        connection.disconnect()
        connection.connect()
    }

    /// Connect all known devices.
    func connectAll() {
        for connection in connections.values {
            if connection.state == .disconnected {
                connection.connect()
            }
        }
    }

    /// Disconnect all devices.
    func disconnectAll() {
        for connection in connections.values {
            connection.disconnect()
        }
    }

    // MARK: - Sending messages

    /// Send a message to a specific Mac.
    func send(_ message: ClientMessage, to deviceId: String) async throws {
        guard let connection = connections[deviceId] else {
            throw ConnectionError.notConnected
        }
        try await connection.send(message)
    }

    /// Send a message to the Mac that owns a given session.
    func send(_ message: ClientMessage, forSession sessionId: String, on deviceId: String) async throws {
        try await send(message, to: deviceId)
    }

    /// Request session lists from all connected Macs.
    func refreshAllSessions() async {
        let listMessage = ClientMessage.listSessions(
            .init(filter: .init(status: nil, includeEnded: false, since: nil))
        )
        for (deviceId, connection) in connections where connection.state == .connected {
            do {
                try await connection.send(listMessage)
            } catch {
                logger.error("Failed to request sessions from \(deviceId): \(error)")
            }
        }
    }

    /// Subscribe to real-time events for a session.
    func subscribe(sessionId: String, on deviceId: String) async throws {
        try await send(.subscribe(.init(sessionId: sessionId)), to: deviceId)
    }

    /// Unsubscribe from a session's events.
    func unsubscribe(sessionId: String, on deviceId: String) async throws {
        try await send(.unsubscribe(.init(sessionId: sessionId)), to: deviceId)
    }

    /// Send text input to a session.
    func sendInput(_ text: String, sessionId: String, on deviceId: String) async throws {
        try await send(.input(.init(sessionId: sessionId, text: text)), to: deviceId)
    }

    /// Send raw bytes (base64-encoded) to a session.
    func sendRawInput(_ bytesB64: String, sessionId: String, on deviceId: String) async throws {
        try await send(.inputRaw(.init(sessionId: sessionId, bytesB64: bytesB64)), to: deviceId)
    }

    /// Send a signal to a session.
    func sendSignal(_ signal: String, sessionId: String, on deviceId: String) async throws {
        try await send(.signal(.init(sessionId: sessionId, signal: signal)), to: deviceId)
    }
}
