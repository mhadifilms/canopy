import SwiftUI

/// Subtle inline label showing token/cost info for an AI interaction.
struct AIUsageLabel: View {
    let payload: SessionEvent.AIUsage

    var body: some View {
        HStack(spacing: 4) {
            Text(tokenString)
            Text("tokens")
            if payload.costUsd > 0 {
                Text("$\(payload.costUsd, specifier: "%.3f")")
            }
        }
        .font(.caption2)
        .foregroundStyle(.tertiary)
        .padding(.vertical, 2)
        .accessibilityLabel("AI usage: \(payload.tokensIn + payload.tokensOut) tokens, \(String(format: "$%.3f", payload.costUsd))")
    }

    private var tokenString: String {
        let total = payload.tokensIn + payload.tokensOut
        if total >= 1_000_000 {
            return String(format: "%.1fM", Double(total) / 1_000_000)
        } else if total >= 1000 {
            return String(format: "%.1fk", Double(total) / 1000)
        }
        return "\(total)"
    }
}
