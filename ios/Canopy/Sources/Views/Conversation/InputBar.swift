import SwiftUI

/// Text input bar at the bottom of the conversation view.
///
/// Includes: text field, send button, Ctrl+C button.
/// Adapts placeholder based on session context.
struct InputBar: View {
    @Binding var text: String
    let placeholder: String
    let onSend: () -> Void
    let onInterrupt: () -> Void

    var body: some View {
        HStack(spacing: 8) {
            TextField(placeholder, text: $text)
                .textFieldStyle(.plain)
                .font(.body)
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
                .background(
                    RoundedRectangle(cornerRadius: 20)
                        .fill(Color(.secondarySystemBackground))
                )
                .onSubmit {
                    guard !text.isEmpty else { return }
                    onSend()
                }
                .accessibilityLabel("Message input")

            Button {
                onInterrupt()
            } label: {
                Text("^C")
                    .font(.system(.caption, design: .monospaced).weight(.bold))
                    .foregroundStyle(.red)
                    .padding(8)
                    .background(Circle().fill(Color(.tertiarySystemBackground)))
            }
            .frame(minWidth: 44, minHeight: 44)
            .accessibilityLabel("Send interrupt signal, Control C")

            Button {
                guard !text.isEmpty else { return }
                onSend()
            } label: {
                Image(systemName: "arrow.up.circle.fill")
                    .font(.title2)
                    .foregroundStyle(text.isEmpty ? Color.secondary : Color.accentColor)
            }
            .disabled(text.isEmpty)
            .frame(minWidth: 44, minHeight: 44)
            .accessibilityLabel("Send message")
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(Color(.systemBackground))
    }
}
