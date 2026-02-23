import SwiftUI

/// The universal conversation view for a terminal session.
///
/// Renders all session events as a chat-like conversation:
/// user input on the right, system responses on the left.
/// AI-enhanced events replace raw output when available.
/// Per section 5.4 of the spec.
struct ConversationView: View {
    let sessionId: String
    let appState: AppState

    @State private var inputText = ""

    private var session: Session? {
        appState.sessionStore.sessions[sessionId]
    }

    private var events: [SessionEvent] {
        appState.eventStore.events(for: sessionId)
    }

    private var pendingApproval: SessionEvent.AIApproval? {
        // Find the most recent unanswered AI approval
        for event in events.reversed() {
            if case .aiApproval(let approval) = event {
                return approval
            }
            // If we see a user_input after the approval, it was already answered
            if case .userInput = event {
                break
            }
        }
        return nil
    }

    var body: some View {
        VStack(spacing: 0) {
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 8) {
                        ForEach(Array(events.enumerated()), id: \.offset) { index, event in
                            eventView(for: event, at: index)
                                .id(index)
                        }
                    }
                    .padding(.horizontal, 12)
                    .padding(.vertical, 8)
                }
                .onChange(of: events.count) { _, newCount in
                    withAnimation {
                        proxy.scrollTo(newCount - 1, anchor: .bottom)
                    }
                }
            }

            Divider()

            if let approval = pendingApproval {
                AIApprovalBanner(
                    approval: approval,
                    onApprove: { Task { await appState.approveAction(for: sessionId) } },
                    onReject: { Task { await appState.rejectAction(for: sessionId) } }
                )
            }

            InputBar(
                text: $inputText,
                placeholder: inputPlaceholder,
                onSend: {
                    let text = inputText
                    inputText = ""
                    Task { await appState.sendInput(text, to: sessionId) }
                },
                onInterrupt: {
                    Task { await appState.sendInterrupt(to: sessionId) }
                }
            )
        }
        .navigationTitle(session?.title ?? session?.currentProcess ?? "Session")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .principal) {
                VStack(spacing: 0) {
                    Text(session?.title ?? session?.currentProcess ?? "Session")
                        .font(.headline)
                        .lineLimit(1)
                    if let hostname = session?.hostname {
                        Text(hostname)
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                }
            }
        }
        .task {
            await appState.subscribeToSession(sessionId)
        }
        .onDisappear {
            Task { await appState.unsubscribeFromSession(sessionId) }
        }
    }

    private var inputPlaceholder: String {
        guard let session else { return "Type a command..." }
        if session.toolType != nil && session.toolType != .generic && session.status == .active {
            return "Type a message..."
        }
        return "Type a command..."
    }

    // MARK: - Event rendering

    @ViewBuilder
    private func eventView(for event: SessionEvent, at index: Int) -> some View {
        switch event {
        case .userInput(let payload):
            UserInputBubble(payload: payload)

        case .systemOutput(let payload):
            // Skip system_output if an ai_response covers the same span
            if !isReplacedByAIResponse(at: index) {
                SystemOutputBlock(
                    payload: payload,
                    completion: completionForOutput(after: index)
                )
            }

        case .completed(let payload):
            // Rendered as a badge on the preceding SystemOutputBlock, not standalone.
            // Only render standalone if no preceding system_output.
            if !hasPrecedingSystemOutput(before: index) {
                CompletedBadge(payload: payload)
            }

        case .inputRequest(let payload):
            InputRequestCard(payload: payload) { response in
                Task { await appState.sendInput(response, to: sessionId) }
            }

        case .idle:
            // Not rendered as a distinct message per spec
            EmptyView()

        case .aiResponse(let payload):
            AIResponseBlock(payload: payload)

        case .aiAction(let payload):
            AIActionCard(payload: payload)

        case .aiApproval:
            // Rendered as the sticky banner above input bar, not inline
            EmptyView()

        case .aiUsage(let payload):
            AIUsageLabel(payload: payload)

        case .processChange(let payload):
            ProcessChangeDivider(payload: payload)

        case .statusChange:
            EmptyView()

        case .remoteInput(let payload):
            RemoteInputIndicator(payload: payload)
        }
    }

    // MARK: - Helpers

    /// Check if a system_output at this index is replaced by a nearby ai_response.
    private func isReplacedByAIResponse(at index: Int) -> Bool {
        let searchEnd = min(index + 3, events.count)
        for i in (index + 1)..<searchEnd {
            if case .aiResponse = events[i] { return true }
            if case .userInput = events[i] { break }
        }
        return false
    }

    /// Find the completion event immediately following a system_output.
    private func completionForOutput(after index: Int) -> SessionEvent.Completed? {
        let next = index + 1
        guard next < events.count else { return nil }
        if case .completed(let c) = events[next] { return c }
        return nil
    }

    /// Check if there's a system_output immediately before a completed event.
    private func hasPrecedingSystemOutput(before index: Int) -> Bool {
        guard index > 0 else { return false }
        if case .systemOutput = events[index - 1] { return true }
        return false
    }
}
