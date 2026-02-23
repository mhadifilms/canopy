import SwiftUI

/// List of paired Mac devices with connection status, session count,
/// and swipe actions to disconnect/reconnect.
struct DeviceListView: View {
    let appState: AppState

    var body: some View {
        List {
            ForEach(appState.pairedDevices) { device in
                NavigationLink {
                    DeviceDetailView(device: device, appState: appState)
                } label: {
                    DeviceRow(device: device, appState: appState)
                }
                .swipeActions(edge: .trailing, allowsFullSwipe: false) {
                    if isConnected(device) {
                        Button {
                            appState.connectionManager.reconnect(device.deviceId)
                        } label: {
                            Label("Reconnect", systemImage: "arrow.clockwise")
                        }
                        .tint(.blue)
                    } else {
                        Button {
                            appState.connectionManager.reconnect(device.deviceId)
                        } label: {
                            Label("Connect", systemImage: "bolt")
                        }
                        .tint(.green)
                    }
                }
                .accessibilityLabel(accessibilityLabel(for: device))
            }
        }
        .navigationTitle("Paired Macs")
    }

    private func isConnected(_ device: MacDevice) -> Bool {
        appState.connectionManager.connectionStates[device.deviceId] == .connected
    }

    private func accessibilityLabel(for device: MacDevice) -> String {
        let status = connectionStatusText(for: device)
        let sessions = appState.sessionStore.sessionCount(for: device.deviceId)
        return "\(device.hostname), \(status), \(sessions) sessions"
    }

    private func connectionStatusText(for device: MacDevice) -> String {
        guard let state = appState.connectionManager.connectionStates[device.deviceId] else {
            return "Not connected"
        }
        switch state {
        case .connected: return "Connected"
        case .connecting: return "Connecting"
        case .reconnecting(let attempt): return "Reconnecting (\(attempt))"
        case .disconnected: return "Disconnected"
        }
    }
}

// MARK: - Device Row

private struct DeviceRow: View {
    let device: MacDevice
    let appState: AppState

    private var connectionState: ConnectionState {
        appState.connectionManager.connectionStates[device.deviceId] ?? .disconnected
    }

    private var sessionCount: Int {
        appState.sessionStore.sessionCount(for: device.deviceId)
    }

    private var lastHeard: Date? {
        appState.connectionManager.lastHeardFrom[device.deviceId] ?? device.lastSeen
    }

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: "desktopcomputer")
                .font(.title2)
                .foregroundStyle(statusColor)
                .frame(width: 36)

            VStack(alignment: .leading, spacing: 2) {
                Text(device.hostname)
                    .font(.body.weight(.medium))

                HStack(spacing: 4) {
                    Circle()
                        .fill(statusColor)
                        .frame(width: 6, height: 6)
                    Text(statusText)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                HStack(spacing: 12) {
                    if sessionCount > 0 {
                        Label("\(sessionCount)", systemImage: "terminal")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }

                    if let lastHeard {
                        Text(RelativeDateFormatter.string(for: lastHeard))
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                    }
                }
            }

            Spacer()

            Text(String(device.deviceId.prefix(8)))
                .font(.system(.caption, design: .monospaced))
                .foregroundStyle(.tertiary)
        }
    }

    private var statusColor: Color {
        switch connectionState {
        case .connected: .green
        case .connecting, .reconnecting: .orange
        case .disconnected: .red
        }
    }

    private var statusText: String {
        switch connectionState {
        case .connected: "Connected"
        case .connecting: "Connecting..."
        case .reconnecting(let n): "Reconnecting (\(n))..."
        case .disconnected: "Disconnected"
        }
    }
}
