import SwiftUI

/// Per-category notification toggles.
///
/// Persists preferences to UserDefaults so that the push notification
/// service can check which categories the user has enabled.
struct NotificationSettingsView: View {
    @AppStorage("notify.approvals") private var approvalNotifications = true
    @AppStorage("notify.errors") private var errorNotifications = true
    @AppStorage("notify.keywords") private var keywordAlerts = false
    @AppStorage("notify.completions") private var completionNotifications = false
    @AppStorage("notify.longRunning") private var longRunningNotifications = false
    @AppStorage("notify.keywords.list") private var keywordList = ""

    var body: some View {
        List {
            Section {
                Toggle(isOn: $approvalNotifications) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("AI Approval Requests")
                        Text("When an AI tool needs your approval")
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
                Text("Alerts")
            } footer: {
                Text("These are enabled by default and recommended for active monitoring.")
            }

            Section {
                Toggle(isOn: $keywordAlerts) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Keyword Alerts")
                        Text("Notify when output contains specific words")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
                .accessibilityLabel("Keyword alerts, \(keywordAlerts ? "enabled" : "disabled")")

                if keywordAlerts {
                    TextField("Keywords (comma-separated)", text: $keywordList)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .accessibilityLabel("Keyword list")
                        .accessibilityHint("Enter keywords separated by commas")
                }
            } header: {
                Text("Keyword Monitoring")
            } footer: {
                if keywordAlerts {
                    Text("Example: error, FAIL, panic, segfault")
                }
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
