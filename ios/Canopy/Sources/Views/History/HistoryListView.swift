import SwiftUI

/// Past sessions grouped by date.
///
/// Loaded on-demand from the Mac daemon.
struct HistoryListView: View {
    let appState: AppState

    @State private var searchText = ""

    private var endedSessions: [Session] {
        appState.sessionStore.ended
    }

    var body: some View {
        List {
            if endedSessions.isEmpty {
                Section {
                    VStack(spacing: 12) {
                        Image(systemName: "clock")
                            .font(.system(size: 32))
                            .foregroundStyle(.secondary)
                        Text("No history yet")
                            .font(.headline)
                        Text("Ended sessions will appear here")
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 32)
                }
            } else {
                ForEach(groupedByDate, id: \.0) { label, sessions in
                    Section(label) {
                        ForEach(sessions) { session in
                            NavigationLink(value: session.sessionId) {
                                historyRow(session)
                            }
                        }
                    }
                }
            }
        }
        .navigationTitle("History")
        .searchable(text: $searchText, prompt: "Search sessions...")
        .accessibilityLabel("Session history")
    }

    private func historyRow(_ session: Session) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(session.currentProcess ?? "zsh")
                    .font(.body.weight(.medium))
                Spacer()
                Text(session.hostname)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            if let title = session.title {
                Text(title)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }

            HStack {
                Text("\(session.totalCommands) commands")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
                Spacer()
                Text(RelativeDateFormatter.string(for: session.startedAt))
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }
        }
        .padding(.vertical, 2)
        .accessibilityLabel("\(session.currentProcess ?? "shell") on \(session.hostname), \(session.totalCommands) commands")
    }

    private var groupedByDate: [(String, [Session])] {
        let calendar = Calendar.current
        let grouped = Dictionary(grouping: endedSessions) { session -> String in
            let date = session.startedAt
            if calendar.isDateInToday(date) { return "Today" }
            if calendar.isDateInYesterday(date) { return "Yesterday" }
            let formatter = DateFormatter()
            formatter.dateStyle = .medium
            return formatter.string(from: date)
        }
        return grouped.sorted { $0.key < $1.key }
    }
}
