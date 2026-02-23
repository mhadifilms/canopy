import SwiftUI

/// Per-trigger notification toggles.
struct NotificationSettingsView: View {
    @State private var approvalNotifications = true
    @State private var errorNotifications = true
    @State private var completionNotifications = false
    @State private var longRunningNotifications = false

    var body: some View {
        List {
            Section {
                Toggle(isOn: $approvalNotifications) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("AI Approval Requests")
                        Text("When an AI tool needs approval")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
                .accessibilityLabel("AI approval notifications, \(approvalNotifications ? "enabled" : "disabled")")

                Toggle(isOn: $errorNotifications) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Session Errors")
                        Text("When a session enters an error state")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
                .accessibilityLabel("Error notifications, \(errorNotifications ? "enabled" : "disabled")")
            } header: {
                Text("Enabled by default")
            }

            Section {
                Toggle(isOn: $completionNotifications) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Command Failures")
                        Text("When a command exits with non-zero code")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
                .accessibilityLabel("Failure notifications, \(completionNotifications ? "enabled" : "disabled")")

                Toggle(isOn: $longRunningNotifications) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Long Command Completed")
                        Text("When a command running 60s+ finishes")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
                .accessibilityLabel("Long command notifications, \(longRunningNotifications ? "enabled" : "disabled")")
            } header: {
                Text("Optional")
            }
        }
        .navigationTitle("Notifications")
    }
}
