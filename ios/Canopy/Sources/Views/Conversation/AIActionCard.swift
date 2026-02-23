import SwiftUI

/// Collapsible card showing an AI tool action (read, edit, run, search, write).
///
/// Icon per action type. Tap to expand and show full detail:
/// - read_file: file path and line count preview
/// - edit_file: full diff rendered via DiffView
/// - run_command: command and output
/// - search: query and results
/// - write_file: file path and content preview
///
/// File paths are tappable to trigger the file viewer.
struct AIActionCard: View {
    let payload: SessionEvent.AIAction
    var onFilePathTapped: ((String) -> Void)?

    @State private var isExpanded = false

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 0) {
                Button {
                    withAnimation(.easeInOut(duration: 0.2)) {
                        isExpanded.toggle()
                    }
                } label: {
                    HStack(spacing: 8) {
                        Image(systemName: iconName)
                            .font(.caption)
                            .foregroundStyle(iconColor)
                            .frame(width: 20)

                        Text(payload.description)
                            .font(.subheadline)
                            .foregroundStyle(.primary)
                            .lineLimit(isExpanded ? nil : 1)

                        Spacer()

                        statusIndicator

                        Image(systemName: isExpanded ? "chevron.up" : "chevron.down")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                    .padding(.horizontal, 10)
                    .padding(.vertical, 8)
                }
                .frame(minHeight: 44)
                .accessibilityLabel("\(actionLabel): \(payload.description), \(payload.status.rawValue)")

                if isExpanded, let detail = payload.detail {
                    Divider()
                    expandedDetail(detail)
                }
            }
            .background(
                RoundedRectangle(cornerRadius: 10)
                    .fill(Color(.secondarySystemBackground))
            )
            .overlay(
                RoundedRectangle(cornerRadius: 10)
                    .stroke(Color(.separator).opacity(0.5), lineWidth: 0.5)
            )

            Spacer(minLength: 40)
        }
    }

    // MARK: - Expanded Detail

    @ViewBuilder
    private func expandedDetail(_ detail: String) -> some View {
        switch payload.action {
        case .editFile, .writeFile:
            // Show diff for edit/write actions
            DiffView(diff: detail)
                .frame(maxHeight: 300)
                .padding(8)

        case .readFile:
            // Show file content preview with tappable path
            VStack(alignment: .leading, spacing: 4) {
                if let path = extractFilePath(from: payload.description) {
                    Button {
                        onFilePathTapped?(path)
                    } label: {
                        HStack(spacing: 4) {
                            Image(systemName: "doc.text")
                                .font(.caption2)
                            Text(path)
                                .font(.system(.caption2, design: .monospaced))
                                .lineLimit(1)
                                .truncationMode(.middle)
                        }
                        .foregroundStyle(.accentColor)
                    }
                    .frame(minHeight: 44)
                    .accessibilityLabel("Open file: \(path)")
                }

                Text(detail)
                    .font(.system(.caption2, design: .monospaced))
                    .foregroundStyle(.primary)
                    .textSelection(.enabled)
                    .lineLimit(30)
            }
            .padding(10)

        case .runCommand:
            // Show command output
            VStack(alignment: .leading, spacing: 4) {
                Text(detail)
                    .font(.system(.caption2, design: .monospaced))
                    .foregroundStyle(.primary)
                    .textSelection(.enabled)
            }
            .padding(10)

        case .search:
            // Show search results
            Text(detail)
                .font(.system(.caption2, design: .monospaced))
                .foregroundStyle(.primary)
                .textSelection(.enabled)
                .padding(10)
        }
    }

    // MARK: - Icon/Color

    private var iconName: String {
        switch payload.action {
        case .readFile: "doc.text"
        case .editFile: "pencil"
        case .runCommand: "bolt"
        case .search: "magnifyingglass"
        case .writeFile: "doc.badge.plus"
        }
    }

    private var iconColor: Color {
        switch payload.action {
        case .readFile: .blue
        case .editFile: .orange
        case .runCommand: .purple
        case .search: .cyan
        case .writeFile: .green
        }
    }

    private var actionLabel: String {
        switch payload.action {
        case .readFile: "Read file"
        case .editFile: "Edit file"
        case .runCommand: "Run command"
        case .search: "Search"
        case .writeFile: "Write file"
        }
    }

    @ViewBuilder
    private var statusIndicator: some View {
        switch payload.status {
        case .running:
            ProgressView()
                .controlSize(.mini)
        case .done:
            Image(systemName: "checkmark.circle.fill")
                .font(.caption)
                .foregroundStyle(.green)
        case .error:
            Image(systemName: "exclamationmark.circle.fill")
                .font(.caption)
                .foregroundStyle(.red)
        }
    }

    // MARK: - Helpers

    /// Extract a file path from the action description.
    /// Descriptions follow patterns like "Read server.ts" or "Edit src/auth.ts lines 40-45".
    private func extractFilePath(from description: String) -> String? {
        let words = description.split(separator: " ")
        guard words.count >= 2 else { return nil }
        // The file path is typically the second word
        let candidate = String(words[1])
        // Basic heuristic: contains a dot (file extension) or a slash (path separator)
        if candidate.contains(".") || candidate.contains("/") {
            return candidate
        }
        return nil
    }
}
