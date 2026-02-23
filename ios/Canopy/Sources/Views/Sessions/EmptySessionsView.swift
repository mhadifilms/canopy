import SwiftUI

/// Shown when there are no live sessions to display.
struct EmptySessionsView: View {
    let hasDevices: Bool
    let isConnected: Bool

    var body: some View {
        Section {
            VStack(spacing: 16) {
                Image(systemName: iconName)
                    .font(.system(size: 40))
                    .foregroundStyle(.secondary)

                Text(title)
                    .font(.headline)

                Text(subtitle)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, 32)
        }
    }

    private var iconName: String {
        if !hasDevices {
            return "desktopcomputer"
        } else if !isConnected {
            return "wifi.slash"
        } else {
            return "terminal"
        }
    }

    private var title: String {
        if !hasDevices {
            return "No Macs paired"
        } else if !isConnected {
            return "Not connected"
        } else {
            return "No active sessions"
        }
    }

    private var subtitle: String {
        if !hasDevices {
            return "Tap + to pair your first Mac"
        } else if !isConnected {
            return "Reconnecting to your Macs..."
        } else {
            return "Open a terminal on your Mac to see sessions here"
        }
    }
}
