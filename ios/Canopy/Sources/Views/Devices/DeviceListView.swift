import SwiftUI

/// List of paired Mac devices with connection status.
struct DeviceListView: View {
    let appState: AppState

    var body: some View {
        List {
            ForEach(appState.pairedDevices) { device in
                NavigationLink {
                    DeviceDetailView(device: device, appState: appState)
                } label: {
                    HStack(spacing: 12) {
                        Image(systemName: "desktopcomputer")
                            .font(.title2)
                            .foregroundStyle(.secondary)
                            .frame(width: 36)

                        VStack(alignment: .leading, spacing: 2) {
                            Text(device.hostname)
                                .font(.body.weight(.medium))

                            HStack(spacing: 4) {
                                connectionStatusDot(for: device)
                                Text(connectionStatusText(for: device))
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }
                        }

                        Spacer()

                        Text(device.deviceId.prefix(8))
                            .font(.system(.caption, design: .monospaced))
                            .foregroundStyle(.tertiary)
                    }
                }
                .accessibilityLabel("\(device.hostname), \(connectionStatusText(for: device))")
            }
        }
        .navigationTitle("Paired Macs")
    }

    @ViewBuilder
    private func connectionStatusDot(for device: MacDevice) -> some View {
        let state = appState.connectionManager.connectionStates[device.deviceId]
        Circle()
            .fill(state == .connected ? .green : .orange)
            .frame(width: 6, height: 6)
    }

    private func connectionStatusText(for device: MacDevice) -> String {
        guard let state = appState.connectionManager.connectionStates[device.deviceId] else {
            return "Not connected"
        }
        switch state {
        case .connected: return "Connected"
        case .connecting: return "Connecting..."
        case .reconnecting(let attempt): return "Reconnecting (\(attempt))..."
        case .disconnected: return "Disconnected"
        }
    }
}
