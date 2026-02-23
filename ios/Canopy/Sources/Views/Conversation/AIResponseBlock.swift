import SwiftUI

/// Left-aligned AI response text with markdown rendering.
///
/// Replaces system_output display when available.
struct AIResponseBlock: View {
    let payload: SessionEvent.AIResponse

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 4) {
                Text(LocalizedStringKey(payload.content))
                    .font(.body)
                    .textSelection(.enabled)

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
}
