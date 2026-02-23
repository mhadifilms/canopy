import SwiftUI

/// Right-aligned bubble showing user input.
///
/// Monospace for commands/responses, regular font for AI messages.
struct UserInputBubble: View {
    let payload: SessionEvent.UserInput

    var body: some View {
        HStack {
            Spacer(minLength: 60)

            Text(payload.text)
                .font(fontForInputType)
                .foregroundStyle(.white)
                .padding(.horizontal, 14)
                .padding(.vertical, 8)
                .background(
                    RoundedRectangle(cornerRadius: 16)
                        .fill(Color.accentColor)
                )
                .accessibilityLabel(accessibilityText)
        }
    }

    private var fontForInputType: Font {
        switch payload.inputType {
        case .command, .response:
            .system(.body, design: .monospaced)
        case .aiMessage:
            .body
        }
    }

    private var accessibilityText: String {
        switch payload.inputType {
        case .command:
            "Command: \(payload.text)"
        case .response:
            "Response: \(payload.text)"
        case .aiMessage:
            "Message: \(payload.text)"
        }
    }
}
