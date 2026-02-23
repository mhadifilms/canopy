import SwiftUI

/// Connection and security settings.
///
/// Provides manual endpoint configuration (host + port), auto-reconnect toggle,
/// and existing security settings (Face ID, tunnel status).
struct ConnectionSettingsView: View {
    @AppStorage("connection.host") private var host = ""
    @AppStorage("connection.port") private var portString = "8443"
    @AppStorage("connection.autoReconnect") private var autoReconnect = true
    @AppStorage("connection.requireFaceID") private var requireFaceID = false
    @AppStorage("connection.lockTimeout") private var lockTimeout = 5

    private let timeoutOptions = [1, 5, 15, 30, 60]

    var body: some View {
        List {
            Section {
                HStack {
                    Text("Host")
                        .frame(width: 48, alignment: .leading)
                    TextField("Auto-discover", text: $host)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .keyboardType(.URL)
                        .accessibilityLabel("Server host address")
                }
                .frame(minHeight: 44)

                HStack {
                    Text("Port")
                        .frame(width: 48, alignment: .leading)
                    TextField("8443", text: $portString)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .keyboardType(.numberPad)
                        .accessibilityLabel("Server port number")
                }
                .frame(minHeight: 44)
            } header: {
                Text("Endpoint")
            } footer: {
                Text("Leave host empty to use automatic discovery via the coordination server.")
            }

            Section {
                Toggle(isOn: $autoReconnect) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Auto-Reconnect")
                        Text("Reconnect automatically after network changes")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
                .accessibilityLabel("Auto-reconnect, \(autoReconnect ? "enabled" : "disabled")")
            } header: {
                Text("Reliability")
            }

            Section {
                Toggle(isOn: $requireFaceID) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Require Face ID")
                        Text("Require authentication to open the app")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
                .accessibilityLabel("Require Face ID, \(requireFaceID ? "enabled" : "disabled")")

                if requireFaceID {
                    Picker("Lock after", selection: $lockTimeout) {
                        ForEach(timeoutOptions, id: \.self) { minutes in
                            Text(minutes == 1 ? "1 minute" : "\(minutes) minutes")
                                .tag(minutes)
                        }
                    }
                    .accessibilityLabel("Auto-lock timeout")
                }
            } header: {
                Text("Security")
            }

            Section {
                HStack {
                    Text("Tunnel Status")
                    Spacer()
                    Text("Active")
                        .foregroundStyle(.green)
                }

                HStack {
                    Text("Protocol")
                    Spacer()
                    Text("WireGuard")
                        .foregroundStyle(.secondary)
                }
            } header: {
                Text("Tunnel")
            }
        }
        .navigationTitle("Connection")
    }
}
