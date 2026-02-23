import SwiftUI

/// Collapsible card showing an AI tool action (read, edit, run, search, write).
///
/// Icon per action type. Tap to expand: file contents, diff, or command output.
/// File paths are tappable for the file viewer.
struct AIActionCard: View {
    let payload: SessionEvent.AIAction

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
                .accessibilityLabel("\(actionLabel): \(payload.description), \(payload.status.rawValue)")

                if isExpanded, let detail = payload.detail {
                    Divider()
                    Text(detail)
                        .font(.system(.caption, design: .monospaced))
                        .foregroundStyle(.primary)
                        .textSelection(.enabled)
                        .padding(10)
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
}
