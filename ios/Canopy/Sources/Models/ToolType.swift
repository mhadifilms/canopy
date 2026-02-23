import Foundation

/// The type of AI coding tool detected as the foreground process.
enum ToolType: String, Codable, Sendable, Hashable {
    case claudeCode = "claude_code"
    case aider
    case goose
    case codex
    case generic
}
