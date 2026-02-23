import SwiftUI

/// Detail view for a single paired Mac.
struct DeviceDetailView: View {
    let device: MacDevice
    let appState: AppState

    @State private var showRemoveConfirmation = false
    @Environment(\.dismiss) private var dismiss

    private var connectionState: ConnectionState {
        appState.connectionManager.connectionStates[device.deviceId] ?? .disconnected
    }

    var body: some View {
        List {
            Section("Connection") {
                HStack {
                    Text("Status")
                    Spacer()
                    HStack(spacing: 6) {
                        Circle()
                            .fill(statusColor)
                            .frame(width: 8, height: 8)
                        Text(statusText)
                            .foregroundStyle(.secondary)
                    }
                }

                HStack {
                    Text("Tunnel IP")
                    Spacer()
                    Text(device.tunnelIP)
                        .font(.system(.body, design: .monospaced))
                        .foregroundStyle(.secondary)
                }

                if connectionState != .connected {
                    Button("Reconnect") {
                        appState.connectionManager.reconnect(device.deviceId)
                    }
                    .accessibilityLabel("Reconnect to \(device.hostname)")
                }
            }

            Section("Device Info") {
                HStack {
                    Text("Hostname")
                    Spacer()
                    Text(device.hostname)
                        .foregroundStyle(.secondary)
                }
                HStack {
                    Text("Device ID")
                    Spacer()
                    Text(device.deviceId)
                        .font(.system(.body, design: .monospaced))
                        .foregroundStyle(.secondary)
                }
                if let lastSeen = device.lastSeen {
                    HStack {
                        Text("Last seen")
                        Spacer()
                        Text(RelativeDateFormatter.string(for: lastSeen))
                            .foregroundStyle(.secondary)
                    }
                }
            }

            Section {
                Button(role: .destructive) {
                    showRemoveConfirmation = true
                } label: {
                    Label("Remove Mac", systemImage: "trash")
                }
                .accessibilityLabel("Remove \(device.hostname) from paired devices")
            }
        }
        .navigationTitle(device.hostname)
        .navigationBarTitleDisplayMode(.inline)
        .confirmationDialog(
            "Remove \(device.hostname)?",
            isPresented: $showRemoveConfirmation,
            titleVisibility: .visible
        ) {
            Button("Remove", role: .destructive) {
                appState.removePairedDevice(device.deviceId)
                dismiss()
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("This will disconnect from the Mac and remove it from your paired devices.")
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
