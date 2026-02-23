import Foundation
import Network
import OSLog

private let logger = Logger(subsystem: "dev.canopy.app", category: "WireGuardBridge")

// MARK: - Protocol

/// Protocol abstracting WireGuard tunnel operations.
/// Allows simulator testing without real Network Extension.
protocol WireGuardTunnelProvider: Sendable {
    func configurePeer(publicKey: String, endpoint: String, allowedIP: String) async throws
    func removePeer(publicKey: String) async throws
    func checkHandshake(publicKey: String) async -> Bool
    func getStats(publicKey: String) async -> TunnelStats?
}

struct TunnelStats: Sendable {
    let rxBytes: UInt64
    let txBytes: UInt64
    let lastHandshake: Date?
}

// MARK: - Simulator Provider

/// Actor-based WireGuard provider for simulator builds.
/// Stores configured peers and probes endpoints via lightweight TCP connections.
actor SimulatorWireGuardProvider: WireGuardTunnelProvider {

    private struct PeerInfo: Sendable {
        let publicKey: String
        let endpoint: String
        let allowedIP: String
        var rxBytes: UInt64 = 0
        var txBytes: UInt64 = 0
        var lastHandshake: Date?
    }

    private var peers: [String: PeerInfo] = [:]

    func configurePeer(publicKey: String, endpoint: String, allowedIP: String) async throws {
        peers[publicKey] = PeerInfo(
            publicKey: publicKey,
            endpoint: endpoint,
            allowedIP: allowedIP
        )
        logger.info("Simulator: configured peer \(publicKey.prefix(8)) -> \(endpoint)")
    }

    func removePeer(publicKey: String) async throws {
        peers.removeValue(forKey: publicKey)
        logger.info("Simulator: removed peer \(publicKey.prefix(8))")
    }

    func checkHandshake(publicKey: String) async -> Bool {
        guard let peer = peers[publicKey] else {
            logger.warning("Simulator: checkHandshake for unknown peer \(publicKey.prefix(8))")
            return false
        }

        let reachable = await probeEndpoint(peer.endpoint)

        if reachable {
            peers[publicKey]?.lastHandshake = Date()
            logger.info("Simulator: handshake succeeded for \(publicKey.prefix(8))")
        } else {
            // In simulator, treat as successful after a short delay to allow testing
            #if targetEnvironment(simulator)
            try? await Task.sleep(for: .milliseconds(200))
            peers[publicKey]?.lastHandshake = Date()
            logger.info("Simulator: synthetic handshake for \(publicKey.prefix(8))")
            return true
            #else
            logger.info("Simulator: handshake failed for \(publicKey.prefix(8))")
            #endif
        }

        return reachable
    }

    func getStats(publicKey: String) async -> TunnelStats? {
        guard var peer = peers[publicKey] else { return nil }

        // Simulate incremental traffic
        peer.rxBytes += UInt64.random(in: 64...2048)
        peer.txBytes += UInt64.random(in: 32...1024)
        peers[publicKey] = peer

        return TunnelStats(
            rxBytes: peer.rxBytes,
            txBytes: peer.txBytes,
            lastHandshake: peer.lastHandshake
        )
    }

    // MARK: - TCP Probe

    /// Attempts a lightweight TCP connection to the endpoint with a 2s timeout.
    private func probeEndpoint(_ endpoint: String) async -> Bool {
        let parts = endpoint.split(separator: ":")
        guard parts.count == 2,
              let port = UInt16(parts[1]) else {
            logger.warning("Simulator: invalid endpoint format: \(endpoint)")
            return false
        }
        let host = String(parts[0])

        return await withCheckedContinuation { continuation in
            let connection = NWConnection(
                host: NWEndpoint.Host(host),
                port: NWEndpoint.Port(rawValue: port)!,
                using: .tcp
            )

            let queue = DispatchQueue(label: "dev.canopy.probe.\(host).\(port)")
            var resumed = false

            connection.stateUpdateHandler = { state in
                guard !resumed else { return }
                switch state {
                case .ready:
                    resumed = true
                    connection.cancel()
                    continuation.resume(returning: true)
                case .failed, .cancelled:
                    resumed = true
                    continuation.resume(returning: false)
                default:
                    break
                }
            }

            connection.start(queue: queue)

            // Timeout after 2 seconds
            queue.asyncAfter(deadline: .now() + 2.0) {
                guard !resumed else { return }
                resumed = true
                connection.cancel()
                continuation.resume(returning: false)
            }
        }
    }
}

// MARK: - Network Extension Provider (Stub)

/// Stub provider for real device builds using NETunnelProviderSession.
/// Will be implemented when Network Extension support is added.
struct NetworkExtensionWireGuardProvider: WireGuardTunnelProvider {

    func configurePeer(publicKey: String, endpoint: String, allowedIP: String) async throws {
        throw WireGuardBridgeError.notAvailableInSimulator
    }

    func removePeer(publicKey: String) async throws {
        throw WireGuardBridgeError.notAvailableInSimulator
    }

    func checkHandshake(publicKey: String) async -> Bool {
        logger.warning("NetworkExtension provider not yet implemented")
        return false
    }

    func getStats(publicKey: String) async -> TunnelStats? {
        logger.warning("NetworkExtension provider not yet implemented")
        return nil
    }
}

// MARK: - Errors

enum WireGuardBridgeError: Error, LocalizedError {
    case notAvailableInSimulator
    case invalidEndpoint(String)

    var errorDescription: String? {
        switch self {
        case .notAvailableInSimulator:
            "Network Extension WireGuard is not available in the simulator"
        case .invalidEndpoint(let ep):
            "Invalid endpoint: \(ep)"
        }
    }
}
