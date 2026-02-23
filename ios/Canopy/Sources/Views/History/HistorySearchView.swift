import SwiftUI

/// Full-text search across session history.
///
/// Query is sent to the Mac daemon for server-side search.
struct HistorySearchView: View {
    let appState: AppState

    @State private var query = ""
    @State private var results: [DaemonMessage.SearchResultsPayload.SearchResult] = []
    @State private var isSearching = false

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
                            Text(result.title ?? "Session")
                                .font(.body.weight(.medium))

                            ForEach(result.matches.prefix(3), id: \.ts) { match in
                                Text(match.snippet)
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                                    .lineLimit(1)
                            }
                        }
                    }
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
        isSearching = true
        // In a real implementation, send search_sessions to connected Macs
        // and collect results via the MessageRouter.
        try? await Task.sleep(for: .seconds(1))
        isSearching = false
    }
}
