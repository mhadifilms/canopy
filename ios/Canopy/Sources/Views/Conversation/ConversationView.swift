import SwiftUI

/// The universal conversation view for a terminal session.
///
/// Renders all session events as a chat-like conversation:
/// user input on the right, system responses on the left.
/// AI-enhanced events replace raw output when available.
/// Per section 5.4 of the spec.
///
/// When the session is ended, enters read-only mode:
/// loads history via get_history, disables input, shows end time.
struct ConversationView: View {
    let sessionId: String
    let appState: AppState

    @State private var inputText = ""
    @State private var fileViewerPath: String?

    private var session: Session? {
        appState.sessionStore.sessions[sessionId]
    }

    private var events: [SessionEvent] {
        appState.eventStore.events(for: sessionId)
    }

    private var isEnded: Bool {
        session?.status == .ended
    }

    private var pendingApproval: SessionEvent.AIApproval? {
        guard !isEnded else { return nil }
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

    /// File contents for the currently requested path.
    private var fileContents: DaemonMessage.FileContentsPayload? {
        guard let path = fileViewerPath else { return nil }
        return appState.fileContentsCache[path]
    }

    /// Error for the currently requested file path.
    private var fileError: String? {
        guard let path = fileViewerPath else { return nil }
        return appState.fileErrors[path]
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

                        if isEnded {
                            sessionEndedFooter
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

            if isEnded {
                sessionEndedBar
            } else {
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
                        HStack(spacing: 4) {
                            Image(systemName: "desktopcomputer")
                                .font(.caption2)
                            Text(hostname)
                        }
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                    }
                }
            }
        }
        .task {
            if isEnded {
                // For ended sessions, just load history without subscribing
                await appState.loadHistory(for: sessionId)
            } else {
                await appState.subscribeToSession(sessionId)
            }
        }
        .onDisappear {
            if !isEnded {
                Task { await appState.unsubscribeFromSession(sessionId) }
            }
        }
        .sheet(item: $fileViewerPath) { path in
            FileViewerSheet(
                path: path,
                content: fileContents?.content,
                language: fileContents?.language,
                errorMessage: fileError
            )
        }
        .onChange(of: fileViewerPath) { _, newPath in
            if let path = newPath {
                Task { await appState.readFile(path: path, forSession: sessionId) }
            }
        }
    }

    // MARK: - Ended session UI

    private var sessionEndedBar: some View {
        HStack {
            Spacer()
            Text("Session ended")
                .font(.subheadline)
                .foregroundStyle(.secondary)
            Spacer()
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 14)
        .background(Color(.systemBackground))
        .accessibilityLabel("Session has ended, input is disabled")
    }

    private var sessionEndedFooter: some View {
        VStack(spacing: 4) {
            Divider()
                .padding(.vertical, 8)
            HStack(spacing: 6) {
                Image(systemName: "checkmark.circle")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                if let endTime = session?.lastActivityAt {
                    Text("Session ended \(endTime.formatted(date: .abbreviated, time: .shortened))")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                } else {
                    Text("Session ended")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            .padding(.bottom, 8)
        }
        .accessibilityLabel("Session ended")
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
            if isEnded {
                // In read-only mode, show the request without the action handler
                InputRequestCard(payload: payload) { _ in }
            } else {
                InputRequestCard(payload: payload) { response in
                    Task { await appState.sendInput(response, to: sessionId) }
                }
            }

        case .idle:
            // Not rendered as a distinct message per spec
            EmptyView()

        case .aiResponse(let payload):
            AIResponseBlock(payload: payload)

        case .aiAction(let payload):
            AIActionCard(payload: payload) { path in
                fileViewerPath = path
            }

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
    ///
    /// When AI events are available for a time range, the UI renders those instead
    /// of raw system_output. We look forward from the current system_output to see
    /// if an ai_response follows before the next user_input.
    private func isReplacedByAIResponse(at index: Int) -> Bool {
        // Look ahead for ai_response events that would cover this output
        let searchEnd = min(index + 5, events.count)
        for i in (index + 1)..<searchEnd {
            switch events[i] {
            case .aiResponse:
                return true
            case .userInput:
                // New user input means we've passed the boundary
                return false
            default:
                continue
            }
        }

        // Also look backward: if a recent ai_response preceded this system_output
        // (within the same AI conversation turn), skip it
        let searchStart = max(0, index - 3)
        for i in stride(from: index - 1, through: searchStart, by: -1) {
            switch events[i] {
            case .aiResponse:
                return true
            case .userInput:
                return false
            default:
                continue
            }
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

// MARK: - String Identifiable for sheet presentation

extension String: @retroactive Identifiable {
    public var id: String { self }
}
