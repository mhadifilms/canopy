import SwiftUI

/// Left-aligned block showing command/system output.
///
/// Monospace, subtle dark background. Collapsed if > 20 lines with tap-to-expand.
/// Shows a streaming indicator when output is still arriving.
/// Completed badge shown inline when a completion event follows.
struct SystemOutputBlock: View {
    let payload: SessionEvent.SystemOutput
    let completion: SessionEvent.Completed?

    @State private var isExpanded = false

    private static let collapsedLineLimit = 20

    private var lines: [String] {
        payload.content.components(separatedBy: "\n")
    }

    private var isCollapsible: Bool {
        lines.count > Self.collapsedLineLimit
    }

    private var displayText: String {
        if isCollapsible && !isExpanded {
            return lines.prefix(Self.collapsedLineLimit).joined(separator: "\n")
        }
        return payload.content
    }

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 4) {
                Text(displayText)
                    .font(.system(.caption, design: .monospaced))
                    .foregroundStyle(.primary)
                    .textSelection(.enabled)

                if payload.streaming {
                    HStack(spacing: 4) {
                        ProgressView()
                            .controlSize(.mini)
                        Text("Streaming...")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                }

                if isCollapsible && !isExpanded {
                    Button {
                        withAnimation { isExpanded = true }
                    } label: {
                        Text("\(lines.count - Self.collapsedLineLimit) more lines...")
                            .font(.caption2)
                            .foregroundStyle(Color.accentColor)
                    }
                    .accessibilityLabel("Expand \(lines.count) total lines of output")
                }

                if let completion {
                    CompletedBadge(payload: completion)
                }
            }
            .padding(10)
            .background(
                RoundedRectangle(cornerRadius: 12)
                    .fill(Color(.secondarySystemBackground))
            )

            Spacer(minLength: 40)
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel(accessibilityText)
    }

    private var accessibilityText: String {
        var label = "Output: \(payload.content.prefix(200))"
        if let completion {
            let status = completion.exitCode == 0 ? "succeeded" : "failed with exit code \(completion.exitCode)"
            let duration = RelativeDateFormatter.duration(ms: completion.durationMs)
            label += ". Command \(status) in \(duration)"
        }
        return label
    }
}
