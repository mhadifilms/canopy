import SwiftUI

/// Thin divider showing when the foreground process changed.
///
/// Example: "-- claude started --" or "-- returned to shell --"
struct ProcessChangeDivider: View {
    let payload: SessionEvent.ProcessChange

    var body: some View {
        HStack(spacing: 8) {
            line
            Text(label)
                .font(.caption2)
                .foregroundStyle(.secondary)
            line
        }
        .padding(.vertical, 4)
        .accessibilityLabel(label)
    }

    private var label: String {
        if payload.toolType == .generic || payload.toolType == nil {
            return "returned to shell"
        }
        return "\(payload.processName) started"
    }

    private var line: some View {
        Rectangle()
            .fill(Color(.separator))
            .frame(height: 0.5)
    }
}
