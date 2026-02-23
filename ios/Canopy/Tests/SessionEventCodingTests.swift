import Foundation
import Testing

@testable import CanopyKit

@Suite("SessionEvent Coding")
struct SessionEventCodingTests {

    private let decoder: JSONDecoder = {
        let d = JSONDecoder()
        d.dateDecodingStrategy = .iso8601
        return d
    }()

    private let encoder: JSONEncoder = {
        let e = JSONEncoder()
        e.dateEncodingStrategy = .iso8601
        return e
    }()

    @Test("Decode user_input event")
    func decodeUserInput() throws {
        let json = """
        {
            "type": "user_input",
            "ts": "2026-02-22T10:30:05.456Z",
            "text": "npm run build",
            "cwd": "/Users/hadi/projects/sync",
            "input_type": "command"
        }
        """
        let event = try decoder.decode(SessionEvent.self, from: Data(json.utf8))

        guard case .userInput(let payload) = event else {
            Issue.record("Expected userInput, got \(event)")
            return
        }
        #expect(payload.text == "npm run build")
        #expect(payload.inputType == .command)
        #expect(payload.cwd == "/Users/hadi/projects/sync")
    }

    @Test("Decode system_output event")
    func decodeSystemOutput() throws {
        let json = """
        {
            "type": "system_output",
            "ts": "2026-02-22T10:30:06.789Z",
            "content": "Building project...\\nCompiling 42 files...",
            "streaming": true
        }
        """
        let event = try decoder.decode(SessionEvent.self, from: Data(json.utf8))

        guard case .systemOutput(let payload) = event else {
            Issue.record("Expected systemOutput, got \(event)")
            return
        }
        #expect(payload.streaming == true)
        #expect(payload.content.contains("Compiling"))
    }

    @Test("Decode completed event")
    func decodeCompleted() throws {
        let json = """
        {
            "type": "completed",
            "ts": "2026-02-22T10:30:45.012Z",
            "exit_code": 0,
            "duration_ms": 39556
        }
        """
        let event = try decoder.decode(SessionEvent.self, from: Data(json.utf8))

        guard case .completed(let payload) = event else {
            Issue.record("Expected completed, got \(event)")
            return
        }
        #expect(payload.exitCode == 0)
        #expect(payload.durationMs == 39556)
    }

    @Test("Decode ai_approval event")
    func decodeAIApproval() throws {
        let json = """
        {
            "type": "ai_approval",
            "ts": "2026-02-22T10:35:30.000Z",
            "tool": "claude_code",
            "description": "Edit server.ts lines 40-45",
            "action": "edit_file",
            "diff": "- if (token.valid) {\\n+ if (token.valid && !token.expired) {"
        }
        """
        let event = try decoder.decode(SessionEvent.self, from: Data(json.utf8))

        guard case .aiApproval(let payload) = event else {
            Issue.record("Expected aiApproval, got \(event)")
            return
        }
        #expect(payload.tool == .claudeCode)
        #expect(payload.action == .editFile)
        #expect(payload.diff != nil)
    }

    @Test("Decode process_change event")
    func decodeProcessChange() throws {
        let json = """
        {
            "type": "process_change",
            "ts": "2026-02-22T10:35:00.000Z",
            "process_name": "claude",
            "tool_type": "claude_code",
            "pid": 12345
        }
        """
        let event = try decoder.decode(SessionEvent.self, from: Data(json.utf8))

        guard case .processChange(let payload) = event else {
            Issue.record("Expected processChange, got \(event)")
            return
        }
        #expect(payload.processName == "claude")
        #expect(payload.toolType == .claudeCode)
        #expect(payload.pid == 12345)
    }

    @Test("Roundtrip encode/decode all event types")
    func roundtripAllTypes() throws {
        let ts = ISO8601DateFormatter().date(from: "2026-02-22T10:00:00Z")!

        let events: [SessionEvent] = [
            .userInput(.init(ts: ts, text: "ls", cwd: "/tmp", inputType: .command)),
            .remoteInput(.init(ts: ts, fromDevice: "iphone", text: "y")),
            .systemOutput(.init(ts: ts, content: "output", streaming: false)),
            .completed(.init(ts: ts, exitCode: 0, durationMs: 1000)),
            .inputRequest(.init(ts: ts, promptText: "[y/N]", quickActions: ["y", "N"], process: "npm")),
            .idle(.init(ts: ts, cwd: "/home", promptText: "$")),
            .aiResponse(.init(ts: ts, content: "I will help.", tool: .claudeCode, streaming: false)),
            .aiAction(.init(ts: ts, tool: .claudeCode, action: .readFile, description: "Read foo.ts", detail: nil, status: .done)),
            .aiApproval(.init(ts: ts, tool: .claudeCode, description: "Edit foo.ts", action: .editFile, diff: "+ line")),
            .aiUsage(.init(ts: ts, tool: .claudeCode, tokensIn: 100, tokensOut: 50, costUsd: 0.01)),
            .processChange(.init(ts: ts, processName: "claude", toolType: .claudeCode, pid: 1)),
            .statusChange(.init(ts: ts, from: .idle, to: .active)),
        ]

        for event in events {
            let data = try encoder.encode(event)
            let decoded = try decoder.decode(SessionEvent.self, from: data)
            #expect(event == decoded)
        }
    }
}

@Suite("DaemonMessage Coding")
struct DaemonMessageCodingTests {

    private let decoder: JSONDecoder = {
        let d = JSONDecoder()
        d.dateDecodingStrategy = .iso8601
        return d
    }()

    @Test("Decode session_list message")
    func decodeSessionList() throws {
        let json = """
        {
            "type": "session_list",
            "sessions": [{
                "session_id": "f7a12c3e-4b56-4d89-a012-3c4d5e6f7890",
                "status": "approval_needed",
                "tool_type": "claude_code",
                "current_process": "claude",
                "title": "claude: fix auth bug",
                "cwd": "/Users/hadi/projects/sync",
                "started_at": "2026-02-22T10:30:00Z",
                "last_activity_at": "2026-02-22T11:45:30Z",
                "hostname": "hadis-macbook",
                "preview": "Edit server.ts lines 40-45",
                "total_commands": 14,
                "connected_clients": 2
            }],
            "total": 1
        }
        """
        let message = try decoder.decode(DaemonMessage.self, from: Data(json.utf8))

        guard case .sessionList(let payload) = message else {
            Issue.record("Expected sessionList, got \(message)")
            return
        }
        #expect(payload.sessions.count == 1)
        #expect(payload.sessions[0].status == .approvalNeeded)
        #expect(payload.sessions[0].toolType == .claudeCode)
        #expect(payload.sessions[0].hostname == "hadis-macbook")
    }

    @Test("Decode error message")
    func decodeError() throws {
        let json = """
        {"type": "error", "code": "session_not_found", "message": "No such session"}
        """
        let message = try decoder.decode(DaemonMessage.self, from: Data(json.utf8))

        guard case .error(let payload) = message else {
            Issue.record("Expected error, got \(message)")
            return
        }
        #expect(payload.code == "session_not_found")
    }

    @Test("Decode pong message")
    func decodePong() throws {
        let json = """
        {"type": "pong"}
        """
        let message = try decoder.decode(DaemonMessage.self, from: Data(json.utf8))
        #expect(message == .pong)
    }
}

@Suite("RelativeDateFormatter")
struct RelativeDateFormatterTests {

    @Test("Just now")
    func justNow() {
        let now = Date()
        #expect(RelativeDateFormatter.string(for: now, relativeTo: now) == "just now")
    }

    @Test("Minutes ago")
    func minutesAgo() {
        let now = Date()
        let twoMinAgo = now.addingTimeInterval(-120)
        #expect(RelativeDateFormatter.string(for: twoMinAgo, relativeTo: now) == "2m ago")
    }

    @Test("Hours ago")
    func hoursAgo() {
        let now = Date()
        let threeHoursAgo = now.addingTimeInterval(-10800)
        #expect(RelativeDateFormatter.string(for: threeHoursAgo, relativeTo: now) == "3h ago")
    }

    @Test("Duration formatting")
    func durationFormat() {
        #expect(RelativeDateFormatter.duration(ms: 500) == "< 1s")
        #expect(RelativeDateFormatter.duration(ms: 3000) == "3s")
        #expect(RelativeDateFormatter.duration(ms: 39556) == "39s")
        #expect(RelativeDateFormatter.duration(ms: 135000) == "2m 15s")
        #expect(RelativeDateFormatter.duration(ms: 3900000) == "1h 5m")
    }
}
