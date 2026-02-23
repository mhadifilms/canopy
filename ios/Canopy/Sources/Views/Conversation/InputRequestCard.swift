import SwiftUI

/// Card for interactive prompts with optional quick-action buttons.
///
/// Shows the prompt text and quick-action buttons parsed from patterns
/// like [y/N], (yes/no). Always includes a text input fallback.
struct InputRequestCard: View {
    let payload: SessionEvent.InputRequest
    let onSend: (String) -> Void

    @State private var customInput = ""

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 8) {
                Text(payload.promptText)
                    .font(.system(.body, design: .monospaced))
                    .foregroundStyle(.primary)

                if let actions = payload.quickActions, !actions.isEmpty {
                    HStack(spacing: 8) {
                        ForEach(actions, id: \.self) { action in
                            Button {
                                onSend(action)
                            } label: {
                                Text(action)
                                    .font(.body.weight(.medium))
                                    .padding(.horizontal, 16)
                                    .padding(.vertical, 8)
                                    .background(
                                        RoundedRectangle(cornerRadius: 8)
                                            .fill(Color.accentColor.opacity(0.15))
                                    )
                            }
                            .accessibilityLabel("Respond with \(action)")
                        }
                    }
                }

                HStack {
                    TextField("Type response...", text: $customInput)
                        .font(.system(.body, design: .monospaced))
                        .textFieldStyle(.roundedBorder)
                        .onSubmit {
                            guard !customInput.isEmpty else { return }
                            onSend(customInput)
                            customInput = ""
                        }
                        .accessibilityLabel("Custom response input")

                    Button {
                        guard !customInput.isEmpty else { return }
                        onSend(customInput)
                        customInput = ""
                    } label: {
                        Image(systemName: "arrow.up.circle.fill")
                            .font(.title2)
                    }
                    .disabled(customInput.isEmpty)
                    .accessibilityLabel("Send custom response")
                }
            }
            .padding(12)
            .background(
                RoundedRectangle(cornerRadius: 12)
                    .fill(Color(.tertiarySystemBackground))
                    .overlay(
                        RoundedRectangle(cornerRadius: 12)
                            .stroke(Color.accentColor.opacity(0.3), lineWidth: 1)
                    )
            )

            Spacer(minLength: 24)
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Interactive prompt: \(payload.promptText)")
    }
}
