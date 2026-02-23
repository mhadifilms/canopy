import SwiftUI
import CryptoKit

/// QR scanner + manual code entry for pairing a new Mac.
struct AddDeviceView: View {
    let appState: AppState

    @State private var manualCode = ""
    @State private var isPairing = false
    @State private var errorMessage: String?
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(spacing: 24) {
            // QR scanner placeholder — requires AVFoundation camera access
            // In a real implementation this would be a live camera view
            RoundedRectangle(cornerRadius: 16)
                .fill(Color(.secondarySystemBackground))
                .aspectRatio(1, contentMode: .fit)
                .overlay {
                    VStack(spacing: 12) {
                        Image(systemName: "qrcode.viewfinder")
                            .font(.system(size: 48))
                            .foregroundStyle(.secondary)
                        Text("Point camera at QR code")
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                }
                .padding(.horizontal, 32)
                .accessibilityLabel("QR code scanner. Point your camera at the QR code displayed on your Mac")

            Text("or enter the 6-digit code:")
                .font(.subheadline)
                .foregroundStyle(.secondary)

            HStack(spacing: 12) {
                TextField("000000", text: $manualCode)
                    .font(.system(.title, design: .monospaced))
                    .multilineTextAlignment(.center)
                    .keyboardType(.numberPad)
                    .frame(maxWidth: 200)
                    .textFieldStyle(.roundedBorder)
                    .accessibilityLabel("Six digit pairing code")

                Button {
                    Task { await pairWithCode() }
                } label: {
                    if isPairing {
                        ProgressView()
                    } else {
                        Text("Pair")
                            .font(.headline)
                    }
                }
                .buttonStyle(.borderedProminent)
                .disabled(manualCode.count != 6 || isPairing)
                .accessibilityLabel("Pair with code")
            }

            if let errorMessage {
                Text(errorMessage)
                    .font(.caption)
                    .foregroundStyle(.red)
                    .accessibilityLabel("Error: \(errorMessage)")
            }
        }
        .navigationTitle("Add Mac")
        .navigationBarTitleDisplayMode(.inline)
    }

    private func pairWithCode() async {
        isPairing = true
        errorMessage = nil

        do {
            let code = manualCode.trimmingCharacters(in: .whitespaces)
            guard code.count == 6, code.allSatisfy(\.isNumber) else {
                throw PairingError.invalidCode
            }

            // Create a temporary CoordinationClient to poll for pairing status.
            // The coord URL defaults to the production server; the identity key
            // is ephemeral for this polling-only flow.
            let identityKey = Curve25519.Signing.PrivateKey()
            let wgPublicKey = identityKey.publicKey.rawRepresentation.base64EncodedString()
            let coordURL = URL(string: "https://coord.canopy.dev")!
            let client = CoordinationClient(
                coordURL: coordURL,
                identityKey: identityKey,
                wgPublicKey: wgPublicKey
            )

            // Poll the coordination server for the Mac's confirmation.
            let maxAttempts = 30 // 30 * 2s = 60s timeout
            for attempt in 1...maxAttempts {
                let confirmation = try await client.checkPairingStatus(code: code)

                if confirmation.status == "confirmed",
                   let hostname = confirmation.hostname,
                   let deviceId = confirmation.deviceId,
                   let wgPub = confirmation.wgPub,
                   let identity = confirmation.identity {
                    let device = MacDevice(
                        hostname: hostname,
                        deviceId: deviceId,
                        wgPublicKey: wgPub,
                        identityPublicKey: identity,
                        tunnelIP: tunnelIPFromWGKey(wgPub),
                        isOnline: true,
                        lastSeen: Date()
                    )
                    appState.addPairedDevice(device)
                    isPairing = false
                    dismiss()
                    return
                }

                if attempt < maxAttempts {
                    try await Task.sleep(for: .seconds(2))
                }
            }

            throw PairingError.timeout
        } catch {
            isPairing = false
            errorMessage = error.localizedDescription
        }
    }

    /// Derive a tunnel IP from a WireGuard public key (deterministic).
    private func tunnelIPFromWGKey(_ wgKey: String) -> String {
        guard let keyData = Data(base64Encoded: wgKey) else {
            return "100.100.0.1"
        }
        let hash = SHA256.hash(data: keyData)
        let bytes = Array(hash)
        return "100.100.\(bytes[0]).\(max(1, bytes[1]))"
    }
}
