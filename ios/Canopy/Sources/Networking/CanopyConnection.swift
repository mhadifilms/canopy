import Foundation
import OSLog

private let logger = Logger(subsystem: "dev.canopy.app", category: "CanopyConnection")

/// Connection state for a single Mac WebSocket link.
enum ConnectionState: Sendable, Hashable {
    case disconnected
    case connecting
    case connected
    case reconnecting(attempt: Int)
}

/// A WebSocket client that connects to a single Mac daemon over the WireGuard tunnel.
///
/// Handles connect, disconnect, send, receive, ping/pong keep-alive, and auto-reconnect.
@MainActor
final class CanopyConnection: Sendable {

    let device: MacDevice

    private(set) var state: ConnectionState = .disconnected {
        didSet { onStateChange?(state) }
    }

    /// Called on every incoming daemon message.
    var onMessage: (@Sendable (DaemonMessage) -> Void)?

    /// Called when connection state changes.
    var onStateChange: (@Sendable (ConnectionState) -> Void)?

    private var webSocketTask: URLSessionWebSocketTask?
    private var pingTask: Task<Void, Never>?
    private var receiveTask: Task<Void, Never>?
    private var reconnectTask: Task<Void, Never>?
    private var isIntentionalDisconnect = false

    private let session: URLSession
    private let encoder: JSONEncoder
    private let decoder: JSONDecoder

    private static let maxReconnectAttempts = 10
    private static let baseReconnectDelay: TimeInterval = 1.0
    private static let maxReconnectDelay: TimeInterval = 30.0
    private static let pingInterval: TimeInterval = 15.0
    private static let port = 19876

    init(device: MacDevice) {
        self.device = device

        let config = URLSessionConfiguration.default
        config.waitsForConnectivity = true
        config.timeoutIntervalForRequest = 30
        self.session = URLSession(configuration: config)

        self.encoder = JSONEncoder()
        self.encoder.dateEncodingStrategy = .iso8601

        self.decoder = JSONDecoder()
        self.decoder.dateDecodingStrategy = .iso8601
    }

    // MARK: - Public API

    func connect() {
        guard state == .disconnected else { return }
        isIntentionalDisconnect = false
        establishConnection()
    }

    func disconnect() {
        isIntentionalDisconnect = true
        tearDown(reason: .goingAway)
        state = .disconnected
    }

    func send(_ message: ClientMessage) async throws {
        guard let task = webSocketTask else {
            throw ConnectionError.notConnected
        }

        let data = try encoder.encode(message)
        try await task.send(.string(String(data: data, encoding: .utf8)!))
    }

    // MARK: - Connection lifecycle

    private func establishConnection() {
        state = .connecting

        let urlString = "ws://\(device.tunnelIP):\(Self.port)/ws"
        guard let url = URL(string: urlString) else {
            logger.error("Invalid WebSocket URL: \(urlString)")
            state = .disconnected
            return
        }

        let task = session.webSocketTask(with: url)
        task.maximumMessageSize = 4 * 1024 * 1024 // 4MB
        self.webSocketTask = task
        task.resume()

        state = .connected
        logger.info("Connected to \(self.device.hostname) at \(urlString)")

        startReceiveLoop()
        startPingLoop()
    }

    private func tearDown(reason: URLSessionWebSocketTask.CloseCode) {
        pingTask?.cancel()
        pingTask = nil
        receiveTask?.cancel()
        receiveTask = nil
        reconnectTask?.cancel()
        reconnectTask = nil

        webSocketTask?.cancel(with: reason, reason: nil)
        webSocketTask = nil
    }

    // MARK: - Receive loop

    private func startReceiveLoop() {
        receiveTask?.cancel()
        receiveTask = Task { [weak self] in
            guard let self else { return }
            while !Task.isCancelled {
                guard let task = self.webSocketTask else { break }
                do {
                    let wsMessage = try await task.receive()
                    self.handleRawMessage(wsMessage)
                } catch {
                    if !Task.isCancelled {
                        logger.warning("Receive error from \(self.device.hostname): \(error)")
                        self.handleDisconnect()
                    }
                    break
                }
            }
        }
    }

    private func handleRawMessage(_ message: URLSessionWebSocketTask.Message) {
        let data: Data
        switch message {
        case .string(let text):
            guard let d = text.data(using: .utf8) else { return }
            data = d
        case .data(let d):
            data = d
        @unknown default:
            return
        }

        do {
            let daemonMessage = try decoder.decode(DaemonMessage.self, from: data)
            onMessage?(daemonMessage)
        } catch {
            logger.error("Failed to decode daemon message: \(error)")
        }
    }

    // MARK: - Ping keep-alive

    private func startPingLoop() {
        pingTask?.cancel()
        pingTask = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(Self.pingInterval))
                guard !Task.isCancelled, let self else { break }
                do {
                    try await self.send(.ping)
                } catch {
                    logger.warning("Ping failed to \(self.device.hostname): \(error)")
                }
            }
        }
    }

    // MARK: - Reconnection

    private func handleDisconnect() {
        guard !isIntentionalDisconnect else { return }
        tearDown(reason: .abnormalClosure)
        scheduleReconnect()
    }

    private func scheduleReconnect() {
        reconnectTask?.cancel()
        reconnectTask = Task { [weak self] in
            guard let self else { return }
            for attempt in 1...Self.maxReconnectAttempts {
                guard !Task.isCancelled else { return }

                self.state = .reconnecting(attempt: attempt)
                let delay = min(
                    Self.baseReconnectDelay * pow(2.0, Double(attempt - 1)),
                    Self.maxReconnectDelay
                )
                logger.info("Reconnecting to \(self.device.hostname) in \(delay)s (attempt \(attempt))")

                try? await Task.sleep(for: .seconds(delay))
                guard !Task.isCancelled else { return }

                self.establishConnection()

                // If we connected successfully, stop retrying
                if self.state == .connected { return }
            }

            logger.error("Gave up reconnecting to \(self.device.hostname)")
            self.state = .disconnected
        }
    }
}

// MARK: - Errors

enum ConnectionError: Error, LocalizedError {
    case notConnected
    case encodingFailed

    var errorDescription: String? {
        switch self {
        case .notConnected: "Not connected to daemon"
        case .encodingFailed: "Failed to encode message"
        }
    }
}
