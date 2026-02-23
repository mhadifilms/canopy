import SwiftUI

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

        // Placeholder: real pairing involves exchanging WireGuard keys
        // via the coordination server using the 6-digit code.
        // For now, just show the flow.
        try? await Task.sleep(for: .seconds(2))

        isPairing = false
        errorMessage = "Pairing not yet implemented in this build"
    }
}
