import SwiftUI

/// Left-aligned AI response text with markdown rendering.
///
/// Parses markdown into blocks: paragraphs with inline formatting,
/// code blocks with monospace + syntax-aware background,
/// headings, and lists. Replaces system_output when available.
struct AIResponseBlock: View {
    let payload: SessionEvent.AIResponse

    private var blocks: [MarkdownBlock] {
        MarkdownRenderer.parseBlocks(payload.content)
    }

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 6) {
                ForEach(Array(blocks.enumerated()), id: \.offset) { _, block in
                    blockView(for: block)
                }

                if payload.streaming {
                    HStack(spacing: 4) {
                        ProgressView()
                            .controlSize(.mini)
                        Text("Thinking...")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                }
            }
            .padding(.vertical, 4)

            Spacer(minLength: 60)
        }
        .accessibilityLabel("AI response: \(payload.content.prefix(200))")
    }

    @ViewBuilder
    private func blockView(for block: MarkdownBlock) -> some View {
        switch block {
        case .paragraph(let text):
            Text(MarkdownRenderer.render(text))
                .font(.body)
                .textSelection(.enabled)

        case .heading(let level, let text):
            Text(text)
                .font(headingFont(level: level))
                .fontWeight(.semibold)
                .padding(.top, level == 1 ? 4 : 2)

        case .codeBlock(_, let code):
            ScrollView(.horizontal, showsIndicators: false) {
                Text(code)
                    .font(.system(.caption, design: .monospaced))
                    .textSelection(.enabled)
                    .padding(8)
            }
            .background(
                RoundedRectangle(cornerRadius: 8)
                    .fill(Color(.tertiarySystemBackground))
            )

        case .listItem(let text, let ordered):
            HStack(alignment: .top, spacing: 6) {
                Text(ordered ? "1." : "\u{2022}")
                    .font(.body)
                    .foregroundStyle(.secondary)
                    .frame(width: 16, alignment: .trailing)
                Text(MarkdownRenderer.render(text))
                    .font(.body)
                    .textSelection(.enabled)
            }
        }
    }

    private func headingFont(level: Int) -> Font {
        switch level {
        case 1: .title3
        case 2: .headline
        default: .subheadline
        }
    }
}
