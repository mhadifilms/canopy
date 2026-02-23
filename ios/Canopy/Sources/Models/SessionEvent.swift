import Foundation

/// All event types emitted by the canopyd parser (section 3.5.2).
///
/// Each variant maps to a JSON object with `"type": "<variant>"`.
/// Uses an unkeyed coding strategy with a `type` discriminator.
enum SessionEvent: Sendable, Hashable {

    // MARK: - User side

    case userInput(UserInput)
    case remoteInput(RemoteInput)

    // MARK: - System side

    case systemOutput(SystemOutput)
    case completed(Completed)
    case inputRequest(InputRequest)
    case idle(Idle)

    // MARK: - AI enhancements

    case aiResponse(AIResponse)
    case aiAction(AIAction)
    case aiApproval(AIApproval)
    case aiUsage(AIUsage)

    // MARK: - Meta

    case processChange(ProcessChange)
    case statusChange(StatusChange)

    // MARK: - Payload types

    struct UserInput: Codable, Sendable, Hashable {
        let ts: Date
        let text: String
        let cwd: String?
        let inputType: InputType

        enum InputType: String, Codable, Sendable, Hashable {
            case command
            case response
            case aiMessage = "ai_message"
        }

        enum CodingKeys: String, CodingKey {
            case ts, text, cwd
            case inputType = "input_type"
        }
    }

    struct RemoteInput: Codable, Sendable, Hashable {
        let ts: Date
        let fromDevice: String
        let text: String

        enum CodingKeys: String, CodingKey {
            case ts
            case fromDevice = "from_device"
            case text
        }
    }

    struct SystemOutput: Codable, Sendable, Hashable {
        let ts: Date
        let content: String
        let streaming: Bool
    }

    struct Completed: Codable, Sendable, Hashable {
        let ts: Date
        let exitCode: Int
        let durationMs: Int

        enum CodingKeys: String, CodingKey {
            case ts
            case exitCode = "exit_code"
            case durationMs = "duration_ms"
        }
    }

    struct InputRequest: Codable, Sendable, Hashable {
        let ts: Date
        let promptText: String
        let quickActions: [String]?
        let process: String?

        enum CodingKeys: String, CodingKey {
            case ts
            case promptText = "prompt_text"
            case quickActions = "quick_actions"
            case process
        }
    }

    struct Idle: Codable, Sendable, Hashable {
        let ts: Date
        let cwd: String?
        let promptText: String?

        enum CodingKeys: String, CodingKey {
            case ts, cwd
            case promptText = "prompt_text"
        }
    }

    struct AIResponse: Codable, Sendable, Hashable {
        let ts: Date
        let content: String
        let tool: ToolType
        let streaming: Bool
    }

    struct AIAction: Codable, Sendable, Hashable {
        let ts: Date
        let tool: ToolType
        let action: ActionKind
        let description: String
        let detail: String?
        let status: ActionStatus

        enum ActionKind: String, Codable, Sendable, Hashable {
            case readFile = "read_file"
            case editFile = "edit_file"
            case runCommand = "run_command"
            case search
            case writeFile = "write_file"
        }

        enum ActionStatus: String, Codable, Sendable, Hashable {
            case running
            case done
            case error
        }
    }

    struct AIApproval: Codable, Sendable, Hashable {
        let ts: Date
        let tool: ToolType
        let description: String
        let action: AIAction.ActionKind
        let diff: String?
    }

    struct AIUsage: Codable, Sendable, Hashable {
        let ts: Date
        let tool: ToolType
        let tokensIn: Int
        let tokensOut: Int
        let costUsd: Double

        enum CodingKeys: String, CodingKey {
            case ts, tool
            case tokensIn = "tokens_in"
            case tokensOut = "tokens_out"
            case costUsd = "cost_usd"
        }
    }

    struct ProcessChange: Codable, Sendable, Hashable {
        let ts: Date
        let processName: String
        let toolType: ToolType?
        let pid: Int

        enum CodingKeys: String, CodingKey {
            case ts
            case processName = "process_name"
            case toolType = "tool_type"
            case pid
        }
    }

    struct StatusChange: Codable, Sendable, Hashable {
        let ts: Date
        let from: SessionStatus
        let to: SessionStatus
    }
}

// MARK: - Codable

extension SessionEvent: Codable {

    private enum TypeKey: String, Codable {
        case userInput = "user_input"
        case remoteInput = "remote_input"
        case systemOutput = "system_output"
        case completed
        case inputRequest = "input_request"
        case idle
        case aiResponse = "ai_response"
        case aiAction = "ai_action"
        case aiApproval = "ai_approval"
        case aiUsage = "ai_usage"
        case processChange = "process_change"
        case statusChange = "status_change"
    }

    private enum CodingKeys: String, CodingKey {
        case type
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        let type = try container.decode(TypeKey.self, forKey: .type)

        switch type {
        case .userInput:
            self = .userInput(try UserInput(from: decoder))
        case .remoteInput:
            self = .remoteInput(try RemoteInput(from: decoder))
        case .systemOutput:
            self = .systemOutput(try SystemOutput(from: decoder))
        case .completed:
            self = .completed(try Completed(from: decoder))
        case .inputRequest:
            self = .inputRequest(try InputRequest(from: decoder))
        case .idle:
            self = .idle(try Idle(from: decoder))
        case .aiResponse:
            self = .aiResponse(try AIResponse(from: decoder))
        case .aiAction:
            self = .aiAction(try AIAction(from: decoder))
        case .aiApproval:
            self = .aiApproval(try AIApproval(from: decoder))
        case .aiUsage:
            self = .aiUsage(try AIUsage(from: decoder))
        case .processChange:
            self = .processChange(try ProcessChange(from: decoder))
        case .statusChange:
            self = .statusChange(try StatusChange(from: decoder))
        }
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)

        switch self {
        case .userInput(let v):
            try container.encode(TypeKey.userInput, forKey: .type)
            try v.encode(to: encoder)
        case .remoteInput(let v):
            try container.encode(TypeKey.remoteInput, forKey: .type)
            try v.encode(to: encoder)
        case .systemOutput(let v):
            try container.encode(TypeKey.systemOutput, forKey: .type)
            try v.encode(to: encoder)
        case .completed(let v):
            try container.encode(TypeKey.completed, forKey: .type)
            try v.encode(to: encoder)
        case .inputRequest(let v):
            try container.encode(TypeKey.inputRequest, forKey: .type)
            try v.encode(to: encoder)
        case .idle(let v):
            try container.encode(TypeKey.idle, forKey: .type)
            try v.encode(to: encoder)
        case .aiResponse(let v):
            try container.encode(TypeKey.aiResponse, forKey: .type)
            try v.encode(to: encoder)
        case .aiAction(let v):
            try container.encode(TypeKey.aiAction, forKey: .type)
            try v.encode(to: encoder)
        case .aiApproval(let v):
            try container.encode(TypeKey.aiApproval, forKey: .type)
            try v.encode(to: encoder)
        case .aiUsage(let v):
            try container.encode(TypeKey.aiUsage, forKey: .type)
            try v.encode(to: encoder)
        case .processChange(let v):
            try container.encode(TypeKey.processChange, forKey: .type)
            try v.encode(to: encoder)
        case .statusChange(let v):
            try container.encode(TypeKey.statusChange, forKey: .type)
            try v.encode(to: encoder)
        }
    }
}

// MARK: - Convenience

extension SessionEvent {

    /// The timestamp of this event, regardless of type.
    var timestamp: Date {
        switch self {
        case .userInput(let v): v.ts
        case .remoteInput(let v): v.ts
        case .systemOutput(let v): v.ts
        case .completed(let v): v.ts
        case .inputRequest(let v): v.ts
        case .idle(let v): v.ts
        case .aiResponse(let v): v.ts
        case .aiAction(let v): v.ts
        case .aiApproval(let v): v.ts
        case .aiUsage(let v): v.ts
        case .processChange(let v): v.ts
        case .statusChange(let v): v.ts
        }
    }
}
