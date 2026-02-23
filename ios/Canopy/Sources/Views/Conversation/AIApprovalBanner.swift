import SwiftUI

/// Sticky banner above the input bar for AI approval requests.
///
/// Shows the action description, optional diff preview,
/// and large Approve/Reject buttons (min 44x44pt).
/// Haptic feedback on appear and on action.
struct AIApprovalBanner: View {
    let approval: SessionEvent.AIApproval
    let onApprove: () -> Void
    let onReject: () -> Void

    @State private var showDiff = false

    var body: some View {
        VStack(spacing: 10) {
            HStack {
                Image(systemName: "exclamationmark.triangle.fill")
                    .foregroundStyle(.orange)
                Text(approval.description)
                    .font(.subheadline.weight(.medium))
                    .lineLimit(2)
                Spacer()
            }

            if let diff = approval.diff {
                Button {
                    withAnimation { showDiff.toggle() }
                } label: {
                    HStack {
                        Text(showDiff ? "Hide diff" : "Show diff")
                            .font(.caption)
                        Image(systemName: showDiff ? "chevron.up" : "chevron.down")
                            .font(.caption2)
                    }
                    .foregroundStyle(.accentColor)
                }
                .accessibilityLabel(showDiff ? "Hide code diff" : "Show code diff")

                if showDiff {
                    DiffView(diff: diff)
                        .frame(maxHeight: 200)
                }
            }

            HStack(spacing: 12) {
                Button(role: .destructive) {
                    let generator = UIImpactFeedbackGenerator(style: .medium)
                    generator.impactOccurred()
                    onReject()
                } label: {
                    Label("Reject", systemImage: "xmark")
                        .font(.body.weight(.medium))
                        .frame(maxWidth: .infinity)
                        .frame(minHeight: 44)
                }
                .buttonStyle(.bordered)
                .tint(.red)
                .accessibilityLabel("Reject this action")

                Button {
                    let generator = UIImpactFeedbackGenerator(style: .medium)
                    generator.impactOccurred()
                    onApprove()
                } label: {
                    Label("Approve", systemImage: "checkmark")
                        .font(.body.weight(.medium))
                        .frame(maxWidth: .infinity)
                        .frame(minHeight: 44)
                }
                .buttonStyle(.borderedProminent)
                .tint(.green)
                .accessibilityLabel("Approve this action")
            }
        }
        .padding(12)
        .background(
            RoundedRectangle(cornerRadius: 12)
                .fill(Color(.systemBackground))
                .shadow(color: .black.opacity(0.1), radius: 8, y: -2)
        )
        .sensoryFeedback(.warning, trigger: approval.description)
    }
}
