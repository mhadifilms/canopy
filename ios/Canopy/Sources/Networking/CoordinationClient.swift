import Foundation
import CryptoKit
import OSLog

private let logger = Logger(subsystem: "dev.canopy.app", category: "CoordinationClient")

/// Client for the Canopy coordination server (coord.canopy.dev).
///
/// Handles check-in, endpoint lookup, pairing registration, STUN discovery,
/// and push token registration. All requests are Ed25519-signed per PLAN.md section 4.3.
actor CoordinationClient {

    private let coordURL: URL
    private let identityKey: Curve25519.Signing.PrivateKey
    private let wgPublicKey: String
    private let session: URLSession

    private let encoder: JSONEncoder = {
        let e = JSONEncoder()
        e.dateEncodingStrategy = .iso8601
        e.outputFormatting = .sortedKeys
        return e
    }()

    private let decoder: JSONDecoder = {
        let d = JSONDecoder()
        d.dateDecodingStrategy = .iso8601
        return d
    }()

    init(
        coordURL: URL,
        identityKey: Curve25519.Signing.PrivateKey,
        wgPublicKey: String,
        session: URLSession = .shared
    ) {
        self.coordURL = coordURL
        self.identityKey = identityKey
        self.wgPublicKey = wgPublicKey
        self.session = session
    }

    /// The device's Ed25519 public key in raw base64.
    var devicePublicKey: String {
        identityKey.publicKey.rawRepresentation.base64EncodedString()
    }

    // MARK: - Check-In

    /// Check in with the coordination server, reporting our endpoints and paired devices.
    func checkIn(
        endpoints: [Endpoint],
        pairedDeviceWGKeys: [String],
        apnsToken: String?
    ) async throws {
        let body = CheckInRequest(
            deviceKey: devicePublicKey,
            wgPublicKey: wgPublicKey,
            endpoints: endpoints,
            pairedDevices: pairedDeviceWGKeys,
            apnsTokens: apnsToken.map { [$0] } ?? [],
            timestamp: ISO8601DateFormatter().string(from: Date()),
            sig: "" // placeholder, filled after signing
        )

        let data = try encoder.encode(body)
        let signature = try sign(data)

        // Re-encode with real signature
        var signed = try decoder.decode(CheckInRequest.self, from: data)
        signed.sig = signature
        let signedData = try encoder.encode(signed)

        let url = coordURL.appendingPathComponent("v1/checkin")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = signedData

        let (_, response) = try await session.data(for: request)
        try validateResponse(response)
        logger.info("Check-in successful")
    }

    // MARK: - Endpoint Lookup

    /// Look up the current endpoints for a paired Mac by its WireGuard public key.
    func lookupEndpoints(peerWGKey: String) async throws -> EndpointLookupResponse {
        var components = URLComponents(url: coordURL.appendingPathComponent("v1/endpoints"), resolvingAgainstBaseURL: false)!
        components.queryItems = [URLQueryItem(name: "peer_wg_key", value: peerWGKey)]

        var request = URLRequest(url: components.url!)
        request.httpMethod = "GET"
        request.setValue("Bearer \(try signedBearerToken())", forHTTPHeaderField: "Authorization")

        let (data, response) = try await session.data(for: request)
        try validateResponse(response)

        return try decoder.decode(EndpointLookupResponse.self, from: data)
    }

    // MARK: - Pairing Registration

    /// Register a pairing with the coordination server so both devices can discover each other.
    func registerPairing(peerWGKey: String) async throws {
        let body = RegisterPairingRequest(
            deviceKey: devicePublicKey,
            peerWgKey: peerWGKey,
            sig: ""
        )

        let data = try encoder.encode(body)
        let signature = try sign(data)

        var signed = try decoder.decode(RegisterPairingRequest.self, from: data)
        signed.sig = signature
        let signedData = try encoder.encode(signed)

        let url = coordURL.appendingPathComponent("v1/register_pairing")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = signedData

        let (_, response) = try await session.data(for: request)
        try validateResponse(response)
        logger.info("Pairing registered with coord server")
    }

    // MARK: - Pairing Status

    /// Poll the coordination server for the status of a pairing session by 6-digit code.
    /// Returns the pairing confirmation with Mac device info when confirmed.
    func checkPairingStatus(code: String) async throws -> PairingConfirmation {
        let url = coordURL.appendingPathComponent("v1/pairing/\(code)/status")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"

        let (data, response) = try await session.data(for: request)
        try validateResponse(response)

        return try decoder.decode(PairingConfirmation.self, from: data)
    }

    // MARK: - TURN

    /// Request a TURN relay allocation for a peer.
    /// Returns the relay endpoint that both sides can use to relay WireGuard traffic.
    func requestTURNAllocation(peerWGKey: String) async throws -> TURNAllocationResponse {
        let body = TURNAllocationRequest(
            deviceKey: devicePublicKey,
            peerWgKey: peerWGKey,
            sig: ""
        )

        let data = try encoder.encode(body)
        let signature = try sign(data)

        var signed = try decoder.decode(TURNAllocationRequest.self, from: data)
        signed.sig = signature
        let signedData = try encoder.encode(signed)

        let url = coordURL.appendingPathComponent("v1/turn/allocate")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = signedData

        let (responseData, response) = try await session.data(for: request)
        try validateResponse(response)

        return try decoder.decode(TURNAllocationResponse.self, from: responseData)
    }

    // MARK: - STUN

    /// Discover our own public endpoint via the coordination server's STUN endpoint.
    /// Uses a simple HTTP-based STUN reflection (the coord server returns the caller's IP:port).
    func discoverPublicEndpoint() async throws -> Endpoint {
        let url = coordURL.appendingPathComponent("v1/stun")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        request.setValue("Bearer \(try signedBearerToken())", forHTTPHeaderField: "Authorization")

        let (data, response) = try await session.data(for: request)
        try validateResponse(response)

        return try decoder.decode(Endpoint.self, from: data)
    }

    // MARK: - Signing

    /// Sign arbitrary data with our Ed25519 identity key.
    /// Produces signatures compatible with Go's crypto/ed25519 (RFC 8032 pure Ed25519).
    func sign(_ data: Data) throws -> String {
        let signature = try identityKey.signature(for: data)
        return Data(signature).base64EncodedString()
    }

    /// Create a signed bearer token: base64(publicKey).timestamp.base64(signature(timestamp)).
    /// Uses `.` as separator to avoid conflicts with `:` in ISO8601 timestamps.
    private func signedBearerToken() throws -> String {
        let timestamp = ISO8601DateFormatter().string(from: Date())
        let timestampData = Data(timestamp.utf8)
        let sig = try identityKey.signature(for: timestampData)
        let pubB64 = devicePublicKey
        let sigB64 = Data(sig).base64EncodedString()
        return "\(pubB64).\(timestamp).\(sigB64)"
    }

    // MARK: - Helpers

    private func validateResponse(_ response: URLResponse) throws {
        guard let http = response as? HTTPURLResponse else {
            throw CoordinationError.invalidResponse
        }
        guard (200...299).contains(http.statusCode) else {
            throw CoordinationError.httpError(statusCode: http.statusCode)
        }
    }
}

// MARK: - Request/Response Types

struct Endpoint: Codable, Sendable, Hashable {
    let ip: String
    let port: Int
    var type: EndpointType?
    var lastSeen: String?

    enum EndpointType: String, Codable, Sendable, Hashable {
        case `public`
        case local
    }

    enum CodingKeys: String, CodingKey {
        case ip, port, type
        case lastSeen = "last_seen"
    }
}

struct EndpointLookupResponse: Codable, Sendable {
    let endpoints: [Endpoint]
    let online: Bool
}

private struct CheckInRequest: Codable {
    let deviceKey: String
    let wgPublicKey: String
    let endpoints: [Endpoint]
    let pairedDevices: [String]
    let apnsTokens: [String]
    let timestamp: String
    var sig: String

    enum CodingKeys: String, CodingKey {
        case deviceKey = "device_key"
        case wgPublicKey = "wg_public_key"
        case endpoints
        case pairedDevices = "paired_devices"
        case apnsTokens = "apns_tokens"
        case timestamp, sig
    }
}

struct TURNAllocationResponse: Codable, Sendable {
    let relayEndpoint: Endpoint
    let token: String
    let expiresAt: String

    enum CodingKeys: String, CodingKey {
        case relayEndpoint = "relay_endpoint"
        case token
        case expiresAt = "expires_at"
    }
}

private struct TURNAllocationRequest: Codable {
    let deviceKey: String
    let peerWgKey: String
    var sig: String

    enum CodingKeys: String, CodingKey {
        case deviceKey = "device_key"
        case peerWgKey = "peer_wg_key"
        case sig
    }
}

/// Response from the coordination server's pairing status endpoint.
struct PairingConfirmation: Codable, Sendable {
    let status: String
    let hostname: String?
    let deviceId: String?
    let wgPub: String?
    let identity: String?

    enum CodingKeys: String, CodingKey {
        case status, hostname
        case deviceId = "device_id"
        case wgPub = "wg_pub"
        case identity
    }
}

private struct RegisterPairingRequest: Codable {
    let deviceKey: String
    let peerWgKey: String
    var sig: String

    enum CodingKeys: String, CodingKey {
        case deviceKey = "device_key"
        case peerWgKey = "peer_wg_key"
        case sig
    }
}

// MARK: - Errors

enum CoordinationError: Error, LocalizedError {
    case invalidResponse
    case httpError(statusCode: Int)
    case stunFailed

    var errorDescription: String? {
        switch self {
        case .invalidResponse:
            "Invalid response from coordination server"
        case .httpError(let code):
            "Coordination server returned HTTP \(code)"
        case .stunFailed:
            "STUN discovery failed"
        }
    }
}
