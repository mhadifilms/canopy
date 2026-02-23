import SwiftUI

/// Main screen showing all live terminal sessions grouped by status.
///
/// Groups: Needs Attention -> Running -> Idle
/// Per section 5.5 of the spec.
struct SessionListView: View {
    let appState: AppState

    @State private var isRefreshing = false

    var body: some View {
        NavigationStack {
            List {
                if !appState.sessionStore.needsAttention.isEmpty {
                    Section {
                        ForEach(appState.sessionStore.needsAttention) { session in
                            NavigationLink(value: session.sessionId) {
                                SessionRowView(session: session)
                            }
                            .accessibilityLabel(accessibilityLabel(for: session))
                        }
                    } header: {
                        Label("Needs Attention", systemImage: "bolt.fill")
                            .foregroundStyle(.orange)
                            .font(.subheadline.weight(.semibold))
                    }
                }

                if !appState.sessionStore.running.isEmpty {
                    Section {
                        ForEach(appState.sessionStore.running) { session in
                            NavigationLink(value: session.sessionId) {
                                SessionRowView(session: session)
                            }
                            .accessibilityLabel(accessibilityLabel(for: session))
                        }
                    } header: {
                        Label("Running", systemImage: "circle.fill")
                            .foregroundStyle(.green)
                            .font(.subheadline.weight(.semibold))
                    }
                }

                if !appState.sessionStore.idle.isEmpty {
                    Section {
                        ForEach(appState.sessionStore.idle) { session in
                            NavigationLink(value: session.sessionId) {
                                SessionRowView(session: session)
                            }
                            .accessibilityLabel(accessibilityLabel(for: session))
                        }
                    } header: {
                        Label("Idle", systemImage: "circle")
                            .foregroundStyle(.secondary)
                            .font(.subheadline.weight(.semibold))
                    }
                }

                if appState.sessionStore.liveSessions.isEmpty {
                    EmptySessionsView(
                        hasDevices: !appState.pairedDevices.isEmpty,
                        isConnected: appState.connectionManager.hasActiveConnection
                    )
                }

                Section {
                    NavigationLink {
                        HistoryListView(appState: appState)
                    } label: {
                        Label("History", systemImage: "clock")
                    }
                    .accessibilityLabel("View session history")
                }
            }
            .navigationTitle("Canopy")
            .navigationDestination(for: String.self) { sessionId in
                ConversationView(sessionId: sessionId, appState: appState)
            }
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    NavigationLink {
                        AddDeviceView(appState: appState)
                    } label: {
                        Image(systemName: "plus")
                    }
                    .accessibilityLabel("Add Mac")
                }
                ToolbarItem(placement: .topBarTrailing) {
                    NavigationLink {
                        SettingsView(appState: appState)
                    } label: {
                        Image(systemName: "gearshape")
                    }
                    .accessibilityLabel("Settings")
                }
            }
            .refreshable {
                await appState.connectionManager.refreshAllSessions()
            }
        }
    }

    private func accessibilityLabel(for session: Session) -> String {
        let process = session.currentProcess ?? "shell"
        let status = session.status.rawValue
        let time = RelativeDateFormatter.string(for: session.lastActivityAt ?? session.startedAt)
        let host = session.hostname
        return "\(process) on \(host), status: \(status), \(time)"
    }
}
