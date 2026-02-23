import Foundation
import CryptoKit
import OSLog

private let logger = Logger(subsystem: "dev.canopy.app", category: "PairingManager")

// MARK: - QR Payload (cross-platform)

/// The JSON payload encoded in the pairing QR code.
struct PairingQRPayload: Codable, Sendable, Hashable {
    let code: String
    let wgPub: String
    let identity: String
    let coord: String
    let endpoints: [String]

    enum CodingKeys: String, CodingKey {
        case code
        case wgPub = "wg_pub"
        case identity
        case coord
        case endpoints
    }
}

/// Parse a QR code string into a pairing payload.
///
/// Expected JSON format from `canopyd pair`:
/// ```json
/// {
///   "code": "482917",
///   "wg_pub": "<mac_wg_public_key>",
///   "identity": "<mac_ed25519_public_key>",
///   "coord": "coord.canopy.dev",
///   "endpoints": ["192.168.1.100:51820"]
/// }
/// ```
func parsePairingQRPayload(_ string: String) -> PairingQRPayload? {
    guard let data = string.data(using: .utf8) else { return nil }
    return try? JSONDecoder().decode(PairingQRPayload.self, from: data)
}

// MARK: - PairingManager (iOS only — requires AVFoundation camera)

#if os(iOS)
import AVFoundation

/// Manages QR code scanning for device pairing and the pairing handshake.
///
/// Flow per PLAN.md section 6.2:
/// 1. Mac runs `canopyd pair`, generates 6-digit code, displays QR
/// 2. iPhone scans QR containing: code, wg_pub, identity, coord, endpoints
/// 3. iPhone sends pairing_request (via coord server or direct)
/// 4. Mac verifies code, adds iPhone as WireGuard peer
/// 5. Mac sends pairing_confirmed with hostname, device_id
/// 6. Both store peer's WG public key, tunnel established
@MainActor
@Observable
final class PairingManager: NSObject {

    enum PairingState: Sendable, Hashable {
        case idle
        case scanning
        case scanned(PairingQRPayload)
        case sendingRequest
        case awaitingConfirmation
        case paired(MacDevice)
        case failed(String)
    }

    private(set) var state: PairingState = .idle

    /// Camera permission status.
    private(set) var cameraPermissionGranted: Bool = false

    private var captureSession: AVCaptureSession?
    private var coordinationClient: CoordinationClient?

    /// The phone's WireGuard public key to send during pairing.
    var phoneWGPublicKey: String = ""

    /// The phone's Ed25519 public key to send during pairing.
    var phoneIdentityPublicKey: String = ""

    /// Callback when pairing succeeds, providing the new MacDevice.
    var onPaired: ((MacDevice) -> Void)?

    // MARK: - Camera Permission

    /// Request camera permission for QR code scanning.
    func requestCameraPermission() async {
        let status = AVCaptureDevice.authorizationStatus(for: .video)

        switch status {
        case .authorized:
            cameraPermissionGranted = true
        case .notDetermined:
            let granted = await AVCaptureDevice.requestAccess(for: .video)
            cameraPermissionGranted = granted
        case .denied, .restricted:
            cameraPermissionGranted = false
        @unknown default:
            cameraPermissionGranted = false
        }
    }

    // MARK: - QR Scanning

    /// Start the camera capture session for QR code scanning.
    ///
    /// Returns the AVCaptureSession so the view layer can display the preview.
    func startScanning() -> AVCaptureSession? {
        guard cameraPermissionGranted else {
            state = .failed("Camera permission not granted")
            return nil
        }

        let session = AVCaptureSession()
        session.beginConfiguration()

        guard let device = AVCaptureDevice.default(for: .video),
              let input = try? AVCaptureDeviceInput(device: device) else {
            state = .failed("Camera not available")
            return nil
        }

        guard session.canAddInput(input) else {
            state = .failed("Cannot add camera input")
            return nil
        }
        session.addInput(input)

        let output = AVCaptureMetadataOutput()
        guard session.canAddOutput(output) else {
            state = .failed("Cannot add metadata output")
            return nil
        }
        session.addOutput(output)

        output.setMetadataObjectsDelegate(self, queue: .main)
        output.metadataObjectTypes = [.qr]

        session.commitConfiguration()
        self.captureSession = session

        state = .scanning

        // Start the capture session on a background queue
        Task.detached {
            session.startRunning()
        }

        return session
    }

    /// Stop the camera capture session.
    func stopScanning() {
        Task.detached { [captureSession] in
            captureSession?.stopRunning()
        }
        captureSession = nil
        if case .scanning = state {
            state = .idle
        }
    }

    // MARK: - QR Parsing

    /// Parse a QR code string into a pairing payload (convenience wrapper).
    nonisolated static func parseQRPayload(_ string: String) -> PairingQRPayload? {
        parsePairingQRPayload(string)
    }

    // MARK: - Pairing Request

    /// Send the pairing request to the Mac (via coordination server or direct).
    func sendPairingRequest(
        to payload: PairingQRPayload,
        coordinationClient: CoordinationClient
    ) async {
        self.coordinationClient = coordinationClient
        state = .sendingRequest

        do {
            // Register the pairing with the coordination server
            try await coordinationClient.registerPairing(peerWGKey: payload.wgPub)
            state = .awaitingConfirmation
            logger.info("Pairing request sent for code \(payload.code)")

            // In the real implementation, the Mac daemon confirms the pairing
            // via the coordination server or direct connection.
            // For now, we create the device directly from the QR payload.
            let device = MacDevice(
                hostname: "Mac",
                deviceId: String(payload.identity.prefix(16)),
                wgPublicKey: payload.wgPub,
                identityPublicKey: payload.identity,
                tunnelIP: tunnelIPFromWGKey(payload.wgPub),
                isOnline: true,
                lastSeen: Date()
            )

            state = .paired(device)
            onPaired?(device)
            logger.info("Pairing completed with \(device.hostname)")

        } catch {
            logger.error("Pairing request failed: \(error)")
            state = .failed("Pairing failed: \(error.localizedDescription)")
        }
    }

    /// Reset the pairing state.
    func reset() {
        stopScanning()
        state = .idle
    }

    // MARK: - Helpers

    /// Derive a tunnel IP from a WireGuard public key (deterministic).
    /// Uses first 2 bytes of SHA256(wg_pub) for the last two octets of 100.100.x.x.
    private func tunnelIPFromWGKey(_ wgKey: String) -> String {
        guard let keyData = Data(base64Encoded: wgKey) else {
            return "100.100.0.1"
        }
        let hash = SHA256.hash(data: keyData)
        let bytes = Array(hash)
        return "100.100.\(bytes[0]).\(max(1, bytes[1]))"
    }
}

// MARK: - AVCaptureMetadataOutputObjectsDelegate

extension PairingManager: AVCaptureMetadataOutputObjectsDelegate {

    nonisolated func metadataOutput(
        _ output: AVCaptureMetadataOutput,
        didOutput metadataObjects: [AVMetadataObject],
        from connection: AVCaptureConnection
    ) {
        guard let readable = metadataObjects.first as? AVMetadataMachineReadableCodeObject,
              readable.type == .qr,
              let stringValue = readable.stringValue else {
            return
        }

        guard let payload = Self.parseQRPayload(stringValue) else {
            Task { @MainActor in
                logger.warning("Scanned QR code is not a valid Canopy pairing code")
            }
            return
        }

        Task { @MainActor [weak self] in
            guard let self, case .scanning = self.state else { return }
            self.stopScanning()
            self.state = .scanned(payload)
            logger.info("Valid pairing QR scanned: code=\(payload.code)")
        }
    }
}
#endif
