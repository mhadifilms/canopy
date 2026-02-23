import SwiftUI

/// Sticky banner above the input bar for AI approval requests.
///
/// Shows the action description, optional diff preview,
/// and full-width Approve/Reject buttons (56pt height per spec).
/// Haptic feedback on appear (notification) and on action (impact).
struct AIApprovalBanner: View {
    let approval: SessionEvent.AIApproval
    let onApprove: () -> Void
    let onReject: () -> Void

    @State private var showDiff = false
    @State private var appeared = false

    var body: some View {
        VStack(spacing: 12) {
            // Action description header
            HStack(spacing: 8) {
                Image(systemName: "exclamationmark.triangle.fill")
                    .foregroundStyle(.orange)
                    .font(.body)
                VStack(alignment: .leading, spacing: 2) {
                    Text(approval.description)
                        .font(.subheadline.weight(.medium))
                        .lineLimit(2)
                    Text(actionTypeLabel)
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
                Spacer()
            }

            // Diff toggle and preview
            if let diff = approval.diff {
                Button {
                    withAnimation(.easeInOut(duration: 0.2)) { showDiff.toggle() }
                } label: {
                    HStack(spacing: 4) {
                        Text(showDiff ? "Hide diff" : "Show diff")
                            .font(.caption)
                        Image(systemName: showDiff ? "chevron.up" : "chevron.down")
                            .font(.caption2)
                    }
                    .foregroundStyle(Color.accentColor)
                }
                .frame(minHeight: 44)
                .accessibilityLabel(showDiff ? "Hide code diff" : "Show code diff")

                if showDiff {
                    DiffView(diff: diff)
                        .frame(maxHeight: 200)
                }
            }

            // Full-width action buttons
            HStack(spacing: 12) {
                Button(role: .destructive) {
                    triggerImpactHaptic()
                    onReject()
                } label: {
                    Label("Reject", systemImage: "xmark")
                        .font(.body.weight(.semibold))
                        .frame(maxWidth: .infinity)
                        .frame(height: 56)
                }
                .buttonStyle(.bordered)
                .tint(.red)
                .accessibilityLabel("Reject this action")

                Button {
                    triggerImpactHaptic()
                    onApprove()
                } label: {
                    Label("Approve", systemImage: "checkmark")
                        .font(.body.weight(.semibold))
                        .frame(maxWidth: .infinity)
                        .frame(height: 56)
                }
                .buttonStyle(.borderedProminent)
                .tint(.green)
                .accessibilityLabel("Approve this action")
            }
        }
        .padding(16)
        .background(
            RoundedRectangle(cornerRadius: 16)
                .fill(Color(.systemBackground))
                .shadow(color: .black.opacity(0.12), radius: 12, y: -4)
        )
        .onAppear {
            if !appeared {
                appeared = true
                triggerNotificationHaptic()
            }
        }
    }

    private var actionTypeLabel: String {
        switch approval.action {
        case .editFile: "File edit"
        case .writeFile: "New file"
        case .runCommand: "Command execution"
        case .readFile: "File read"
        case .search: "Search"
        }
    }

    private func triggerImpactHaptic() {
        #if os(iOS)
        HapticService.medium()
        #endif
    }

    private func triggerNotificationHaptic() {
        #if os(iOS)
        HapticService.warning()
        #endif
    }
}
