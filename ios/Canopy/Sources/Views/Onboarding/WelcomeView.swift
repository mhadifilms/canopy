import SwiftUI

/// Initial welcome screen shown when no Macs are paired.
struct WelcomeView: View {
    let appState: AppState

    var body: some View {
        NavigationStack {
            VStack(spacing: 32) {
                Spacer()

                Image(systemName: "desktopcomputer.and.arrow.down")
                    .font(.system(size: 60))
                    .foregroundStyle(.accentColor)

                VStack(spacing: 8) {
                    Text("Welcome to Canopy")
                        .font(.largeTitle.weight(.bold))

                    Text("Your Mac's terminal, on your phone.")
                        .font(.title3)
                        .foregroundStyle(.secondary)
                }

                VStack(alignment: .leading, spacing: 16) {
                    featureRow(
                        icon: "terminal",
                        title: "Every session, automatically",
                        subtitle: "See all terminal sessions as conversations"
                    )
                    featureRow(
                        icon: "bell.badge",
                        title: "Approve from anywhere",
                        subtitle: "Push notifications for AI tool approvals"
                    )
                    featureRow(
                        icon: "lock.shield",
                        title: "Direct encrypted tunnel",
                        subtitle: "WireGuard peer-to-peer, no cloud relay"
                    )
                }
                .padding(.horizontal, 24)

                Spacer()

                NavigationLink {
                    SetupGuideView(appState: appState)
                } label: {
                    Text("Get Started")
                        .font(.headline)
                        .frame(maxWidth: .infinity)
                        .frame(height: 50)
                }
                .buttonStyle(.borderedProminent)
                .padding(.horizontal, 24)
                .accessibilityLabel("Start setup")
            }
            .padding(.bottom, 32)
        }
    }

    private func featureRow(icon: String, title: String, subtitle: String) -> some View {
        HStack(spacing: 16) {
            Image(systemName: icon)
                .font(.title2)
                .foregroundStyle(.accentColor)
                .frame(width: 36)

            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(.subheadline.weight(.medium))
                Text(subtitle)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
    }
}
