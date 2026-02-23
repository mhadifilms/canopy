import Foundation
import Network
import OSLog

private let logger = Logger(subsystem: "dev.canopy.app", category: "TunnelManager")

/// Manages the WireGuard tunnel lifecycle and NAT traversal connection flow.
///
/// Implements the connection flow from PLAN.md section 4.5:
/// 1. STUN to get own public endpoint
/// 2. Lookup Mac's endpoints from coordination server
/// 3. Try direct WireGuard handshake
/// 4. If fails after 5s, request TURN allocation
/// 5. Connect through TURN relay
///
/// Also monitors network path changes (WiFi <-> cellular) and re-establishes
/// the tunnel when the network changes.
@MainActor
@Observable
final class TunnelManager {

    enum TunnelState: Sendable, Hashable {
        case disconnected
        case discoveringEndpoints
        case attemptingDirect
        case requestingTURN
        case connectedDirect
        case connectedRelay
        case failed(String)
    }

    private(set) var state: TunnelState = .disconnected
    private(set) var currentEndpoint: Endpoint?

    private let coordinationClient: CoordinationClient
    private var pathMonitor: NWPathMonitor?
    private var monitorQueue: DispatchQueue?
    private var connectionTask: Task<Void, Never>?
    private var currentPath: NWPath?

    /// Called when the tunnel state changes, allowing the ConnectionManager to react.
    var onStateChange: (@Sendable (TunnelState) -> Void)?

    /// Time to wait for a direct WireGuard handshake before falling back to TURN.
    private static let directHandshakeTimeout: TimeInterval = 5.0

    /// Interval between check-ins with the coordination server.
    private static let checkInInterval: TimeInterval = 30.0

    init(coordinationClient: CoordinationClient) {
        self.coordinationClient = coordinationClient
    }

    // MARK: - Public API

    /// Start monitoring network changes and establish the tunnel.
    func start() {
        startNetworkMonitor()
    }

    /// Stop monitoring and tear down the tunnel.
    func stop() {
        connectionTask?.cancel()
        connectionTask = nil
        pathMonitor?.cancel()
        pathMonitor = nil
        state = .disconnected
        onStateChange?(.disconnected)
    }

    /// Initiate the connection flow to a specific Mac.
    ///
    /// Follows the NAT traversal flow from PLAN.md section 4.5:
    /// STUN -> endpoint lookup -> direct attempt -> TURN fallback.
    func connect(peerWGKey: String) {
        connectionTask?.cancel()
        connectionTask = Task { [weak self] in
            guard let self else { return }
            await self.performConnectionFlow(peerWGKey: peerWGKey)
        }
    }

    /// Force reconnection (e.g., after network change).
    func reconnect(peerWGKey: String) {
        logger.info("Reconnecting tunnel due to network change")
        connect(peerWGKey: peerWGKey)
    }

    // MARK: - Connection Flow

    private func performConnectionFlow(peerWGKey: String) async {
        // Step 1: STUN to discover our public endpoint
        state = .discoveringEndpoints
        onStateChange?(.discoveringEndpoints)

        let ownEndpoint: Endpoint?
        do {
            ownEndpoint = try await coordinationClient.discoverPublicEndpoint()
            logger.info("STUN discovered public endpoint: \(ownEndpoint!.ip):\(ownEndpoint!.port)")
        } catch {
            logger.warning("STUN discovery failed: \(error). Proceeding without public endpoint.")
            ownEndpoint = nil
        }

        guard !Task.isCancelled else { return }

        // Step 2: Look up the Mac's endpoints from the coordination server
        let peerEndpoints: EndpointLookupResponse
        do {
            peerEndpoints = try await coordinationClient.lookupEndpoints(peerWGKey: peerWGKey)
            logger.info("Peer has \(peerEndpoints.endpoints.count) endpoints, online=\(peerEndpoints.online)")
        } catch {
            logger.error("Endpoint lookup failed: \(error)")
            state = .failed("Could not find Mac endpoints")
            onStateChange?(state)
            return
        }

        guard !Task.isCancelled else { return }
        guard !peerEndpoints.endpoints.isEmpty else {
            state = .failed("Mac has no registered endpoints")
            onStateChange?(state)
            return
        }

        // Step 3: Try direct WireGuard handshake
        state = .attemptingDirect
        onStateChange?(.attemptingDirect)

        let directSuccess = await attemptDirectConnection(
            peerEndpoints: peerEndpoints.endpoints,
            peerWGKey: peerWGKey
        )

        guard !Task.isCancelled else { return }

        if directSuccess {
            state = .connectedDirect
            currentEndpoint = peerEndpoints.endpoints.first
            onStateChange?(.connectedDirect)
            logger.info("Direct P2P connection established")
            return
        }

        // Step 4: Request TURN allocation
        logger.info("Direct connection failed after \(Self.directHandshakeTimeout)s, requesting TURN")
        state = .requestingTURN
        onStateChange?(.requestingTURN)

        let turnSuccess = await attemptTURNConnection(peerWGKey: peerWGKey)

        guard !Task.isCancelled else { return }

        if turnSuccess {
            state = .connectedRelay
            onStateChange?(.connectedRelay)
            logger.info("TURN relay connection established")
        } else {
            state = .failed("Could not establish connection (direct or relay)")
            onStateChange?(state)
            logger.error("All connection attempts failed")
        }
    }

    /// Attempt a direct WireGuard handshake to the peer's endpoints.
    ///
    /// Tries each endpoint (public first, then local) with a timeout.
    /// In the real implementation, this updates the WireGuard peer's endpoint
    /// via the Network Extension and waits for a successful handshake.
    private func attemptDirectConnection(
        peerEndpoints: [Endpoint],
        peerWGKey: String
    ) async -> Bool {
        // Sort endpoints: try public endpoints first, then local
        let sorted = peerEndpoints.sorted { a, b in
            (a.type == .public ? 0 : 1) < (b.type == .public ? 0 : 1)
        }

        for endpoint in sorted {
            guard !Task.isCancelled else { return false }
            logger.info("Trying direct connection to \(endpoint.ip):\(endpoint.port)")

            // In the real implementation, this would:
            // 1. Update the NETunnelProviderManager peer endpoint
            // 2. Send a message to the PacketTunnelProvider to update the WireGuard peer
            // 3. Wait for a successful handshake (via WireGuard stats)
            let success = await waitForHandshake(
                endpoint: endpoint,
                timeout: Self.directHandshakeTimeout
            )

            if success {
                currentEndpoint = endpoint
                return true
            }
        }

        return false
    }

    /// Wait for a WireGuard handshake to succeed with a timeout.
    ///
    /// In the real implementation, this polls the WireGuard tunnel provider
    /// for the latest handshake timestamp via IPC.
    private func waitForHandshake(endpoint: Endpoint, timeout: TimeInterval) async -> Bool {
        let deadline = ContinuousClock.now + .seconds(timeout)

        while ContinuousClock.now < deadline {
            guard !Task.isCancelled else { return false }

            // Poll interval: check every 500ms
            try? await Task.sleep(for: .milliseconds(500))

            // Placeholder: in real implementation, check WireGuard handshake status
            // via NETunnelProviderSession.sendProviderMessage
            let handshakeComplete = await checkHandshakeStatus(endpoint: endpoint)
            if handshakeComplete {
                return true
            }
        }

        return false
    }

    /// Check if a WireGuard handshake has completed with the given endpoint.
    ///
    /// In real implementation, sends a message to the PacketTunnelProvider
    /// to query the WireGuard interface statistics.
    private func checkHandshakeStatus(endpoint: Endpoint) async -> Bool {
        // Placeholder: will be implemented with Network Extension IPC
        // Returns false until the real tunnel provider is wired up
        return false
    }

    /// Attempt a TURN-relayed connection through the coordination server.
    private func attemptTURNConnection(peerWGKey: String) async -> Bool {
        // In the real implementation:
        // 1. Request a TURN allocation from the coordination server
        // 2. Receive a relay address
        // 3. Update WireGuard peer endpoint to the TURN relay address
        // 4. Wait for handshake through the relay

        // Placeholder: TURN protocol integration will be added
        // when the coordination server's TURN support is ready
        logger.info("TURN relay connection attempt (pending coord server TURN support)")
        return false
    }

    // MARK: - Network Monitoring

    /// Start monitoring network path changes to detect WiFi <-> cellular transitions.
    private func startNetworkMonitor() {
        let queue = DispatchQueue(label: "dev.canopy.tunnel.monitor")
        monitorQueue = queue

        let monitor = NWPathMonitor()
        pathMonitor = monitor

        monitor.pathUpdateHandler = { [weak self] newPath in
            Task { @MainActor [weak self] in
                self?.handlePathUpdate(newPath)
            }
        }

        monitor.start(queue: queue)
        logger.info("Network path monitor started")
    }

    /// Handle a network path change. Re-establishes the tunnel if needed.
    private func handlePathUpdate(_ newPath: NWPath) {
        let previousPath = currentPath
        currentPath = newPath

        guard let previousPath else {
            // First path update, nothing to compare
            logger.info("Initial network path: \(newPath.status == .satisfied ? "satisfied" : "unsatisfied")")
            return
        }

        // Detect meaningful network change
        let networkChanged = hasNetworkChanged(from: previousPath, to: newPath)

        if networkChanged && newPath.status == .satisfied {
            logger.info("Network changed (WiFi<->Cellular or IP change), tunnel re-establishment needed")
            // Notify consumers so they can trigger reconnection
            if case .connectedDirect = state {
                state = .discoveringEndpoints
                onStateChange?(.discoveringEndpoints)
            } else if case .connectedRelay = state {
                state = .discoveringEndpoints
                onStateChange?(.discoveringEndpoints)
            }
        } else if newPath.status == .unsatisfied {
            logger.warning("Network path unsatisfied - no connectivity")
        }
    }

    /// Determine if a meaningful network change occurred.
    private func hasNetworkChanged(from oldPath: NWPath, to newPath: NWPath) -> Bool {
        // Check if the interface type changed (WiFi <-> Cellular)
        let oldInterfaces = Set(oldPath.availableInterfaces.map(\.type))
        let newInterfaces = Set(newPath.availableInterfaces.map(\.type))

        if oldInterfaces != newInterfaces {
            return true
        }

        // Check if connectivity status changed
        if oldPath.status != newPath.status {
            return true
        }

        return false
    }
}
