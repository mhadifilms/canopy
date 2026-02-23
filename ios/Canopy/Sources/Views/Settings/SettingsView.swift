import SwiftUI

/// App settings screen.
struct SettingsView: View {
    let appState: AppState

    var body: some View {
        List {
            Section("Paired Macs") {
                NavigationLink {
                    DeviceListView(appState: appState)
                } label: {
                    HStack {
                        Label("Macs", systemImage: "desktopcomputer")
                        Spacer()
                        Text("\(appState.pairedDevices.count)")
                            .foregroundStyle(.secondary)
                    }
                }
                .accessibilityLabel("\(appState.pairedDevices.count) paired Macs")
            }

            Section("Notifications") {
                NavigationLink {
                    NotificationSettingsView()
                } label: {
                    Label("Notification Settings", systemImage: "bell")
                }
            }

            Section("Security") {
                NavigationLink {
                    ConnectionSettingsView()
                } label: {
                    Label("Connection", systemImage: "lock.shield")
                }
            }

            Section("About") {
                HStack {
                    Text("Version")
                    Spacer()
                    Text("1.0.0")
                        .foregroundStyle(.secondary)
                }
            }
        }
        .navigationTitle("Settings")
    }
}
