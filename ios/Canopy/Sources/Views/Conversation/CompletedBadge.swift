import SwiftUI

/// Badge showing command completion status and duration.
///
/// Green check for exit 0, red X for non-zero.
struct CompletedBadge: View {
    let payload: SessionEvent.Completed

    private var isSuccess: Bool { payload.exitCode == 0 }

    var body: some View {
        HStack(spacing: 4) {
            Image(systemName: isSuccess ? "checkmark" : "xmark")
                .font(.caption2.weight(.bold))
                .foregroundStyle(isSuccess ? .green : .red)

            if !isSuccess {
                Text("exit \(payload.exitCode)")
                    .font(.caption2)
                    .foregroundStyle(.red)
            }

            Text(RelativeDateFormatter.duration(ms: payload.durationMs))
                .font(.caption2)
                .foregroundStyle(.secondary)
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel(
            isSuccess
                ? "Succeeded in \(RelativeDateFormatter.duration(ms: payload.durationMs))"
                : "Failed with exit code \(payload.exitCode) in \(RelativeDateFormatter.duration(ms: payload.durationMs))"
        )
    }
}
