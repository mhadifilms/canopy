import SwiftUI

/// Full-text search across session history.
///
/// Query is sent to all connected Mac daemons for server-side search.
/// Results arrive via the MessageRouter and are collected in AppState.
struct HistorySearchView: View {
    let appState: AppState

    @State private var query = ""

    private var results: [DaemonMessage.SearchResultsPayload.SearchResult] {
        appState.searchResults
    }

    private var isSearching: Bool {
        appState.isSearching
    }

    var body: some View {
        List {
            if isSearching {
                Section {
                    HStack {
                        Spacer()
                        ProgressView("Searching...")
                        Spacer()
                    }
                }
            } else if results.isEmpty && !query.isEmpty {
                Section {
                    VStack(spacing: 8) {
                        Image(systemName: "magnifyingglass")
                            .font(.title)
                            .foregroundStyle(.secondary)
                        Text("No results found")
                            .font(.headline)
                    }
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 24)
                }
            } else {
                ForEach(results, id: \.sessionId) { result in
                    NavigationLink(value: result.sessionId) {
                        VStack(alignment: .leading, spacing: 4) {
                            HStack {
                                Text(result.title ?? "Session")
                                    .font(.body.weight(.medium))

                                Spacer()

                                Text(result.startedAt.formatted(date: .abbreviated, time: .shortened))
                                    .font(.caption2)
                                    .foregroundStyle(.tertiary)
                            }

                            ForEach(result.matches.prefix(3), id: \.ts) { match in
                                HStack(spacing: 4) {
                                    Text(match.eventType)
                                        .font(.system(.caption2, design: .monospaced))
                                        .foregroundStyle(.tertiary)
                                        .frame(width: 60, alignment: .leading)

                                    Text(match.snippet)
                                        .font(.caption)
                                        .foregroundStyle(.secondary)
                                        .lineLimit(1)
                                }
                            }
                        }
                        .padding(.vertical, 2)
                    }
                    .frame(minHeight: 44)
                    .accessibilityLabel("\(result.title ?? "Session") with \(result.matches.count) matches")
                }
            }
        }
        .navigationTitle("Search")
        .searchable(text: $query, prompt: "Search sessions...")
        .onSubmit(of: .search) {
            Task { await performSearch() }
        }
        .accessibilityLabel("Search session history")
    }

    private func performSearch() async {
        guard !query.isEmpty else { return }
        await appState.searchSessions(query: query)
    }
}
