import Foundation

/// Parses basic markdown text into an AttributedString.
///
/// Supports: bold, italic, inline code, code blocks, headers, links, lists.
/// Designed for rendering AI response text in the conversation view.
enum MarkdownRenderer {

    /// Parse a markdown string into an AttributedString suitable for SwiftUI Text.
    ///
    /// Uses Foundation's built-in Markdown parsing for AttributedString,
    /// with fallback to plain text if parsing fails.
    static func render(_ markdown: String) -> AttributedString {
        // Foundation's AttributedString(markdown:) supports:
        // bold, italic, strikethrough, inline code, links, etc.
        do {
            var options = AttributedString.MarkdownParsingOptions()
            options.interpretedSyntax = .inlineOnlyPreservingWhitespace
            return try AttributedString(markdown: markdown, options: options)
        } catch {
            return AttributedString(markdown)
        }
    }

    /// Parse markdown with full block-level support (headers, code blocks, lists).
    ///
    /// Returns an array of blocks that the view layer can render with appropriate
    /// styling (e.g., code blocks get monospace + syntax highlighting).
    static func parseBlocks(_ markdown: String) -> [MarkdownBlock] {
        var blocks: [MarkdownBlock] = []
        var currentCodeBlock: (language: String?, lines: [String])? = nil
        let lines = markdown.components(separatedBy: "\n")

        var i = 0
        while i < lines.count {
            let line = lines[i]

            // Code block fence
            if line.hasPrefix("```") {
                if let code = currentCodeBlock {
                    // End of code block
                    blocks.append(.codeBlock(
                        language: code.language,
                        code: code.lines.joined(separator: "\n")
                    ))
                    currentCodeBlock = nil
                } else {
                    // Start of code block
                    let lang = String(line.dropFirst(3)).trimmingCharacters(in: .whitespaces)
                    currentCodeBlock = (language: lang.isEmpty ? nil : lang, lines: [])
                }
                i += 1
                continue
            }

            // Inside a code block
            if currentCodeBlock != nil {
                currentCodeBlock?.lines.append(line)
                i += 1
                continue
            }

            // Headers
            if line.hasPrefix("### ") {
                let text = String(line.dropFirst(4))
                blocks.append(.heading(level: 3, text: text))
                i += 1
                continue
            }
            if line.hasPrefix("## ") {
                let text = String(line.dropFirst(3))
                blocks.append(.heading(level: 2, text: text))
                i += 1
                continue
            }
            if line.hasPrefix("# ") {
                let text = String(line.dropFirst(2))
                blocks.append(.heading(level: 1, text: text))
                i += 1
                continue
            }

            // List items
            if line.hasPrefix("- ") || line.hasPrefix("* ") {
                let text = String(line.dropFirst(2))
                blocks.append(.listItem(text: text, ordered: false))
                i += 1
                continue
            }
            if let match = line.range(of: #"^\d+\.\s"#, options: .regularExpression) {
                let text = String(line[match.upperBound...])
                blocks.append(.listItem(text: text, ordered: true))
                i += 1
                continue
            }

            // Empty line
            if line.trimmingCharacters(in: .whitespaces).isEmpty {
                i += 1
                continue
            }

            // Regular paragraph - accumulate consecutive non-empty lines
            var paragraphLines = [line]
            i += 1
            while i < lines.count {
                let next = lines[i]
                if next.trimmingCharacters(in: .whitespaces).isEmpty
                    || next.hasPrefix("```")
                    || next.hasPrefix("# ")
                    || next.hasPrefix("## ")
                    || next.hasPrefix("### ")
                    || next.hasPrefix("- ")
                    || next.hasPrefix("* ") {
                    break
                }
                paragraphLines.append(next)
                i += 1
            }
            blocks.append(.paragraph(text: paragraphLines.joined(separator: "\n")))
        }

        // Handle unclosed code block
        if let code = currentCodeBlock {
            blocks.append(.codeBlock(
                language: code.language,
                code: code.lines.joined(separator: "\n")
            ))
        }

        return blocks
    }

    /// Detect the programming language from a file extension.
    static func languageFromExtension(_ ext: String) -> String? {
        let map: [String: String] = [
            "swift": "swift", "ts": "typescript", "tsx": "typescript",
            "js": "javascript", "jsx": "javascript", "py": "python",
            "rb": "ruby", "go": "go", "rs": "rust", "java": "java",
            "kt": "kotlin", "c": "c", "cpp": "cpp", "h": "c",
            "m": "objc", "cs": "csharp", "sh": "shell", "bash": "shell",
            "zsh": "shell", "json": "json", "yaml": "yaml", "yml": "yaml",
            "toml": "toml", "xml": "xml", "html": "html", "css": "css",
            "sql": "sql", "md": "markdown", "dockerfile": "dockerfile",
        ]
        return map[ext.lowercased()]
    }
}

/// A parsed markdown block for rich rendering.
enum MarkdownBlock: Sendable, Hashable {
    case paragraph(text: String)
    case heading(level: Int, text: String)
    case codeBlock(language: String?, code: String)
    case listItem(text: String, ordered: Bool)
}
