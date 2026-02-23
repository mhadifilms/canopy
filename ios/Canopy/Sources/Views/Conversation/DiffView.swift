import SwiftUI

/// Inline diff view with additions in green and removals in red.
struct DiffView: View {
    let diff: String

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            VStack(alignment: .leading, spacing: 0) {
                ForEach(Array(lines.enumerated()), id: \.offset) { _, line in
                    Text(line.text)
                        .font(.system(.caption2, design: .monospaced))
                        .foregroundStyle(line.color)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, 6)
                        .padding(.vertical, 1)
                        .background(line.background)
                }
            }
        }
        .background(
            RoundedRectangle(cornerRadius: 6)
                .fill(Color(.secondarySystemBackground))
        )
        .accessibilityLabel("Code diff: \(diff)")
    }

    private struct DiffLine {
        let text: String
        let color: Color
        let background: Color
    }

    private var lines: [DiffLine] {
        diff.components(separatedBy: "\n").map { line in
            if line.hasPrefix("+") {
                return DiffLine(
                    text: line,
                    color: .green,
                    background: .green.opacity(0.1)
                )
            } else if line.hasPrefix("-") {
                return DiffLine(
                    text: line,
                    color: .red,
                    background: .red.opacity(0.1)
                )
            } else {
                return DiffLine(
                    text: line,
                    color: .primary,
                    background: .clear
                )
            }
        }
    }
}
