import SwiftUI

/// Subtle inline label showing input from another connected client.
///
/// Example: "Alice sent: y"
struct RemoteInputIndicator: View {
    let payload: SessionEvent.RemoteInput

    var body: some View {
        HStack(spacing: 4) {
            Image(systemName: "person.wave.2")
                .font(.caption2)
                .foregroundStyle(.secondary)
            Text("\(payload.fromDevice) sent: \(payload.text)")
                .font(.caption2)
                .foregroundStyle(.secondary)
                .italic()
        }
        .padding(.vertical, 2)
        .accessibilityLabel("\(payload.fromDevice) sent \(payload.text)")
    }
}
