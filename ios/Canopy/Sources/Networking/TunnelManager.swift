import Foundation
import Network
import OSLog

private let logger = Logger(subsystem: "dev.canopy.app", category: "TunnelManager")

/// Manages the WireGuard tunnel lifecycle and NAT traversal connection flow
/// for multiple paired Macs simultaneously.
///
/// Each paired Mac is a separate WireGuard peer. The tunnel configuration
/// contains all peers so that the single VPN tunnel routes to all Macs.
///
/// Implements the connection flow from PLAN.md section 4.5:
/// 1. STUN to get own public endpoint
/// 2. Lookup Mac's endpoints from coordination server
/// 3. Try direct WireGuard handshake
/// 4. If fails after 5s, request TURN allocation
/// 5. Connect through TURN relay
///
/// Also monitors network path changes (WiFi <-> cellular) and re-establishes
/// connections when the network changes.
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

    /// Per-peer connection state, keyed by WireGuard public key.
    private(set) var peerStates: [String: TunnelState] = [:]

    /// Per-peer resolved endpoint, keyed by WireGuard public key.
    private(set) var peerEndpoints: [String: Endpoint] = [:]

    /// Overall tunnel state (the Network Extension VPN itself).
    private(set) var tunnelActive: Bool = false

    private let coordinationClient: CoordinationClient
    private var pathMonitor: NWPathMonitor?
    private var monitorQueue: DispatchQueue?
    private var connectionTasks: [String: Task<Void, Never>] = [:]
    private var currentPath: NWPath?

    /// Called when a peer's tunnel state changes.
    var onPeerStateChange: (@Sendable (_ peerWGKey: String, _ state: TunnelState) -> Void)?

    /// Time to wait for a direct WireGuard handshake before falling back to TURN.
    private static let directHandshakeTimeout: TimeInterval = 5.0

    /// Interval between check-ins with the coordination server.
    private static let checkInInterval: TimeInterval = 30.0

    init(coordinationClient: CoordinationClient) {
        self.coordinationClient = coordinationClient
    }

    // MARK: - Public API

    /// Start monitoring network changes.
    func start() {
        startNetworkMonitor()
        tunnelActive = true
    }

    /// Stop monitoring and tear down all peer connections.
    func stop() {
        for (_, task) in connectionTasks {
            task.cancel()
        }
        connectionTasks.removeAll()
        peerStates.removeAll()
        peerEndpoints.removeAll()
        pathMonitor?.cancel()
        pathMonitor = nil
        tunnelActive = false
    }

    /// Add a peer and initiate the connection flow.
    ///
    /// Each paired Mac is a WireGuard peer with its own allowed IP (100.100.x.x/32).
    /// Adding a peer updates the VPN configuration dynamically.
    func addPeer(wgPublicKey: String) {
        guard connectionTasks[wgPublicKey] == nil else {
            logger.info("Peer \(wgPublicKey.prefix(8)) already has an active connection task")
            return
        }
        peerStates[wgPublicKey] = .disconnected
        connect(peerWGKey: wgPublicKey)
    }

    /// Remove a peer and tear down its connection.
    func removePeer(wgPublicKey: String) {
        connectionTasks[wgPublicKey]?.cancel()
        connectionTasks.removeValue(forKey: wgPublicKey)
        peerStates.removeValue(forKey: wgPublicKey)
        peerEndpoints.removeValue(forKey: wgPublicKey)
        logger.info("Removed peer \(wgPublicKey.prefix(8))")
        // In real implementation: update the NETunnelProviderManager config
        // to remove this peer from the WireGuard interface.
    }

    /// Initiate the connection flow to a specific Mac peer.
    ///
    /// Follows the NAT traversal flow from PLAN.md section 4.5:
    /// STUN -> endpoint lookup -> direct attempt -> TURN fallback.
    func connect(peerWGKey: String) {
        connectionTasks[peerWGKey]?.cancel()
        connectionTasks[peerWGKey] = Task { [weak self] in
            guard let self else { return }
            await self.performConnectionFlow(peerWGKey: peerWGKey)
        }
    }

    /// Disconnect a specific peer without removing it.
    func disconnect(peerWGKey: String) {
        connectionTasks[peerWGKey]?.cancel()
        connectionTasks.removeValue(forKey: peerWGKey)
        peerStates[peerWGKey] = .disconnected
        onPeerStateChange?(peerWGKey, .disconnected)
    }

    /// Force reconnection for a specific peer (e.g., user-initiated).
    func reconnect(peerWGKey: String) {
        logger.info("Reconnecting peer \(peerWGKey.prefix(8))")
        connect(peerWGKey: peerWGKey)
    }

    /// Reconnect all peers (e.g., after network change).
    func reconnectAll() {
        for wgKey in peerStates.keys {
            reconnect(peerWGKey: wgKey)
        }
    }

    /// The state for a specific peer, or `.disconnected` if unknown.
    func state(for peerWGKey: String) -> TunnelState {
        peerStates[peerWGKey] ?? .disconnected
    }

    /// Whether a specific peer is connected (direct or relay).
    func isConnected(_ peerWGKey: String) -> Bool {
        switch peerStates[peerWGKey] {
        case .connectedDirect, .connectedRelay:
            return true
        default:
            return false
        }
    }

    /// Whether any peer is connected.
    var hasAnyConnectedPeer: Bool {
        peerStates.values.contains { state in
            if case .connectedDirect = state { return true }
            if case .connectedRelay = state { return true }
            return false
        }
    }

    // MARK: - Connection Flow

    private func performConnectionFlow(peerWGKey: String) async {
        // Step 1: STUN to discover our public endpoint
        updatePeerState(peerWGKey, .discoveringEndpoints)

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
        let peerEndpointLookup: EndpointLookupResponse
        do {
            peerEndpointLookup = try await coordinationClient.lookupEndpoints(peerWGKey: peerWGKey)
            logger.info("Peer \(peerWGKey.prefix(8)) has \(peerEndpointLookup.endpoints.count) endpoints, online=\(peerEndpointLookup.online)")
        } catch {
            logger.error("Endpoint lookup failed for \(peerWGKey.prefix(8)): \(error)")
            updatePeerState(peerWGKey, .failed("Could not find Mac endpoints"))
            return
        }

        guard !Task.isCancelled else { return }
        guard !peerEndpointLookup.endpoints.isEmpty else {
            updatePeerState(peerWGKey, .failed("Mac has no registered endpoints"))
            return
        }

        // Step 3: Try direct WireGuard handshake
        updatePeerState(peerWGKey, .attemptingDirect)

        let directSuccess = await attemptDirectConnection(
            peerEndpoints: peerEndpointLookup.endpoints,
            peerWGKey: peerWGKey
        )

        guard !Task.isCancelled else { return }

        if directSuccess {
            peerEndpoints[peerWGKey] = peerEndpointLookup.endpoints.first
            updatePeerState(peerWGKey, .connectedDirect)
            logger.info("Direct P2P connection established to \(peerWGKey.prefix(8))")
            return
        }

        // Step 4: Request TURN allocation
        logger.info("Direct connection failed for \(peerWGKey.prefix(8)) after \(Self.directHandshakeTimeout)s, requesting TURN")
        updatePeerState(peerWGKey, .requestingTURN)

        let turnSuccess = await attemptTURNConnection(peerWGKey: peerWGKey)

        guard !Task.isCancelled else { return }

        if turnSuccess {
            updatePeerState(peerWGKey, .connectedRelay)
            logger.info("TURN relay connection established to \(peerWGKey.prefix(8))")
        } else {
            updatePeerState(peerWGKey, .failed("Could not establish connection (direct or relay)"))
            logger.error("All connection attempts failed for \(peerWGKey.prefix(8))")
        }
    }

    private func updatePeerState(_ peerWGKey: String, _ state: TunnelState) {
        peerStates[peerWGKey] = state
        onPeerStateChange?(peerWGKey, state)
    }

    /// Attempt a direct WireGuard handshake to the peer's endpoints.
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
            logger.info("Trying direct connection to \(endpoint.ip):\(endpoint.port) for peer \(peerWGKey.prefix(8))")

            // In the real implementation, this would:
            // 1. Update the NETunnelProviderManager peer endpoint
            // 2. Send a message to the PacketTunnelProvider to update the WireGuard peer
            // 3. Wait for a successful handshake (via WireGuard stats)
            let success = await waitForHandshake(
                endpoint: endpoint,
                timeout: Self.directHandshakeTimeout
            )

            if success {
                self.peerEndpoints[peerWGKey] = endpoint
                return true
            }
        }

        return false
    }

    /// Wait for a WireGuard handshake to succeed with a timeout.
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
    private func checkHandshakeStatus(endpoint: Endpoint) async -> Bool {
        // Placeholder: will be implemented with Network Extension IPC
        return false
    }

    /// Attempt a TURN-relayed connection through the coordination server.
    private func attemptTURNConnection(peerWGKey: String) async -> Bool {
        // Placeholder: TURN protocol integration will be added
        // when the coordination server's TURN support is ready
        logger.info("TURN relay attempt for peer \(peerWGKey.prefix(8)) (pending coord server TURN support)")
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

    /// Handle a network path change. Re-establishes peer connections if needed.
    private func handlePathUpdate(_ newPath: NWPath) {
        let previousPath = currentPath
        currentPath = newPath

        guard let previousPath else {
            logger.info("Initial network path: \(newPath.status == .satisfied ? "satisfied" : "unsatisfied")")
            return
        }

        let networkChanged = hasNetworkChanged(from: previousPath, to: newPath)

        if networkChanged && newPath.status == .satisfied {
            logger.info("Network changed, reconnecting all peers")
            reconnectAll()
        } else if newPath.status == .unsatisfied {
            logger.warning("Network path unsatisfied - no connectivity")
        }
    }

    /// Determine if a meaningful network change occurred.
    private func hasNetworkChanged(from oldPath: NWPath, to newPath: NWPath) -> Bool {
        let oldInterfaces = Set(oldPath.availableInterfaces.map(\.type))
        let newInterfaces = Set(newPath.availableInterfaces.map(\.type))

        if oldInterfaces != newInterfaces {
            return true
        }

        if oldPath.status != newPath.status {
            return true
        }

        return false
    }
}
