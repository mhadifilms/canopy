import SwiftUI

/// Step-by-step guide for installing canopyd on a Mac and pairing.
struct SetupGuideView: View {
    let appState: AppState

    @State private var currentStep = 0

    var body: some View {
        VStack(spacing: 24) {
            TabView(selection: $currentStep) {
                installStep.tag(0)
                pairStep.tag(1)
            }
            .tabViewStyle(.page(indexDisplayMode: .always))
        }
        .navigationTitle("Setup")
        .navigationBarTitleDisplayMode(.inline)
    }

    private var installStep: some View {
        VStack(spacing: 20) {
            Spacer()

            Image(systemName: "terminal")
                .font(.system(size: 48))
                .foregroundStyle(Color.accentColor)

            Text("Install on your Mac")
                .font(.title2.weight(.bold))

            Text("Open Terminal on your Mac and run:")
                .foregroundStyle(.secondary)

            Text("curl -fsSL https://raw.githubusercontent.com/mhadifilms/canopy/main/daemon/install.sh | bash")
                .font(.system(.caption, design: .monospaced))
                .padding(12)
                .background(
                    RoundedRectangle(cornerRadius: 8)
                        .fill(Color(.secondarySystemBackground))
                )
                .textSelection(.enabled)
                .accessibilityLabel("Install command: curl dash f s s L raw github URL pipe bash")

            Spacer()

            Button {
                withAnimation { currentStep = 1 }
            } label: {
                Text("Next: Pair your Mac")
                    .frame(maxWidth: .infinity)
                    .frame(height: 44)
            }
            .buttonStyle(.borderedProminent)
            .padding(.horizontal, 24)
            .accessibilityLabel("Continue to pairing step")
        }
        .padding()
    }

    private var pairStep: some View {
        VStack(spacing: 20) {
            Spacer()

            Image(systemName: "qrcode.viewfinder")
                .font(.system(size: 48))
                .foregroundStyle(Color.accentColor)

            Text("Pair your Mac")
                .font(.title2.weight(.bold))

            Text("On your Mac, run:")
                .foregroundStyle(.secondary)

            Text("canopyd pair")
                .font(.system(.body, design: .monospaced))
                .padding(12)
                .background(
                    RoundedRectangle(cornerRadius: 8)
                        .fill(Color(.secondarySystemBackground))
                )
                .textSelection(.enabled)
                .accessibilityLabel("Pair command: canopyd pair")

            Text("Then scan the QR code or enter the 6-digit code.")
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)

            Spacer()

            NavigationLink {
                AddDeviceView(appState: appState)
            } label: {
                Text("Scan QR Code")
                    .frame(maxWidth: .infinity)
                    .frame(height: 44)
            }
            .buttonStyle(.borderedProminent)
            .padding(.horizontal, 24)
            .accessibilityLabel("Open QR code scanner")
        }
        .padding()
    }
}
