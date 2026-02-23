import SwiftUI

/// Connection and security settings.
struct ConnectionSettingsView: View {
    @State private var requireFaceID = false
    @State private var lockTimeout = 5 // minutes

    private let timeoutOptions = [1, 5, 15, 30, 60]

    var body: some View {
        List {
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
