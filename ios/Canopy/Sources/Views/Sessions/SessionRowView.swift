import SwiftUI

/// A single row in the session list.
///
/// Shows: status dot, process name, Mac hostname, relative time, preview text.
struct SessionRowView: View {
    let session: Session

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            statusDot
                .padding(.top, 4)

            VStack(alignment: .leading, spacing: 2) {
                HStack {
                    Text(session.currentProcess ?? "zsh")
                        .font(.body.weight(.medium))
                        .lineLimit(1)

                    Spacer()

                    Text(RelativeDateFormatter.string(
                        for: session.lastActivityAt ?? session.startedAt
                    ))
                    .font(.caption)
                    .foregroundStyle(.secondary)
                }

                Text(session.hostname)
                    .font(.caption)
                    .foregroundStyle(.secondary)

                if let preview = session.preview {
                    Text(preview)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
            }
        }
        .padding(.vertical, 2)
    }

    @ViewBuilder
    private var statusDot: some View {
        switch session.status {
        case .approvalNeeded, .error:
            Circle()
                .fill(.orange)
                .frame(width: 10, height: 10)
                .accessibilityLabel("needs attention")
        case .active:
            Circle()
                .fill(.green)
                .frame(width: 10, height: 10)
                .accessibilityLabel("running")
        case .idle:
            Circle()
                .stroke(.secondary, lineWidth: 1.5)
                .frame(width: 10, height: 10)
                .accessibilityLabel("idle")
        case .ended:
            Circle()
                .fill(.gray)
                .frame(width: 10, height: 10)
                .accessibilityLabel("ended")
        }
    }
}
