import SwiftUI

/// Half-screen sheet for viewing file contents read-only.
///
/// Line numbers, monospace, horizontal scroll, pinch-to-zoom,
/// basic syntax highlighting, copy to clipboard.
struct FileViewerSheet: View {
    let path: String
    let content: String?
    let language: String?
    let errorMessage: String?

    @State private var scale: CGFloat = 1.0
    @State private var lastScale: CGFloat = 1.0
    @Environment(\.dismiss) private var dismiss

    init(path: String, content: String?, language: String?, errorMessage: String? = nil) {
        self.path = path
        self.content = content
        self.language = language
        self.errorMessage = errorMessage
    }

    private var fileName: String {
        (path as NSString).lastPathComponent
    }

    private var baseFontSize: CGFloat { 12 }

    private var effectiveFontSize: CGFloat {
        baseFontSize * scale
    }

    var body: some View {
        NavigationStack {
            Group {
                if let errorMessage {
                    errorView(errorMessage)
                } else if let content {
                    fileContentView(content)
                } else {
                    loadingView
                }
            }
            .navigationTitle(fileName)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    Button("Done") { dismiss() }
                        .frame(minWidth: 44, minHeight: 44)
                        .accessibilityLabel("Dismiss file viewer")
                }
                ToolbarItem(placement: .topBarTrailing) {
                    if content != nil {
                        Button {
                            UIPasteboard.general.string = content
                        } label: {
                            Image(systemName: "doc.on.doc")
                        }
                        .frame(minWidth: 44, minHeight: 44)
                        .accessibilityLabel("Copy file contents to clipboard")
                    }
                }
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("File viewer: \(fileName)")
    }

    // MARK: - Content View

    private func fileContentView(_ text: String) -> some View {
        let lines = text.components(separatedBy: "\n")
        let gutterWidth = max(40.0, CGFloat(String(lines.count).count) * 10 + 16)

        return ScrollView([.horizontal, .vertical]) {
            VStack(alignment: .leading, spacing: 0) {
                ForEach(Array(lines.enumerated()), id: \.offset) { index, line in
                    HStack(alignment: .top, spacing: 0) {
                        Text("\(index + 1)")
                            .font(.system(size: effectiveFontSize, design: .monospaced))
                            .foregroundStyle(.tertiary)
                            .frame(width: gutterWidth, alignment: .trailing)
                            .padding(.trailing, 8)

                        highlightedLine(line)
                            .textSelection(.enabled)
                    }
                    .padding(.horizontal, 8)
                    .padding(.vertical, 1)
                }
            }
            .padding(.vertical, 8)
        }
        .background(Color(.systemBackground))
        .gesture(
            MagnificationGesture()
                .onChanged { value in
                    let newScale = lastScale * value
                    scale = min(max(newScale, 0.5), 3.0)
                }
                .onEnded { _ in
                    lastScale = scale
                }
        )
        .accessibilityLabel("File content, \(lines.count) lines")
    }

    // MARK: - Loading View

    private var loadingView: some View {
        VStack(spacing: 12) {
            ProgressView()
                .controlSize(.large)
            Text("Loading file...")
                .font(.subheadline)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .accessibilityLabel("Loading file contents")
    }

    // MARK: - Error View

    private func errorView(_ message: String) -> some View {
        VStack(spacing: 12) {
            Image(systemName: "exclamationmark.triangle")
                .font(.largeTitle)
                .foregroundStyle(.secondary)
            Text("Failed to load file")
                .font(.headline)
            Text(message)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .padding(.horizontal, 24)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .accessibilityLabel("Error loading file: \(message)")
    }

    // MARK: - Syntax Highlighting

    private func highlightedLine(_ line: String) -> Text {
        guard let language, !line.isEmpty else {
            return Text(line)
                .font(.system(size: effectiveFontSize, design: .monospaced))
        }

        let keywords = Self.keywords(for: language)
        guard !keywords.isEmpty else {
            return Text(line)
                .font(.system(size: effectiveFontSize, design: .monospaced))
        }

        return colorizedText(line, keywords: keywords)
    }

    private func colorizedText(_ line: String, keywords: Set<String>) -> Text {
        let font = Font.system(size: effectiveFontSize, design: .monospaced)

        // Handle full-line comments
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        if trimmed.hasPrefix("//") || trimmed.hasPrefix("#") || trimmed.hasPrefix("--") {
            return Text(line)
                .font(font)
                .foregroundColor(.gray)
        }

        // Handle strings (simplistic: highlight quoted segments)
        if trimmed.hasPrefix("\"") || trimmed.hasPrefix("'") || trimmed.hasPrefix("`") {
            return Text(line)
                .font(font)
                .foregroundColor(.red)
        }

        // Word-level keyword highlighting
        let words = tokenize(line)
        var result = Text("")
        for token in words {
            if keywords.contains(token.text) {
                result = result + Text(token.text)
                    .font(font)
                    .foregroundColor(.purple)
            } else if token.text.first?.isNumber == true {
                result = result + Text(token.text)
                    .font(font)
                    .foregroundColor(.cyan)
            } else {
                result = result + Text(token.text)
                    .font(font)
            }
        }
        return result
    }

    private struct Token {
        let text: String
    }

    /// Split a line into word and non-word tokens, preserving all characters.
    private func tokenize(_ line: String) -> [Token] {
        var tokens: [Token] = []
        var current = ""
        var inWord = false

        for char in line {
            let isWordChar = char.isLetter || char.isNumber || char == "_"
            if isWordChar {
                if !inWord && !current.isEmpty {
                    tokens.append(Token(text: current))
                    current = ""
                }
                inWord = true
                current.append(char)
            } else {
                if inWord && !current.isEmpty {
                    tokens.append(Token(text: current))
                    current = ""
                }
                inWord = false
                current.append(char)
            }
        }
        if !current.isEmpty {
            tokens.append(Token(text: current))
        }
        return tokens
    }

    // MARK: - Language Keywords

    private static func keywords(for language: String) -> Set<String> {
        switch language.lowercased() {
        case "swift":
            return ["import", "func", "var", "let", "struct", "class", "enum", "protocol",
                    "if", "else", "guard", "switch", "case", "return", "for", "while",
                    "do", "try", "catch", "throw", "throws", "async", "await", "actor",
                    "private", "public", "internal", "fileprivate", "open", "static",
                    "override", "init", "deinit", "self", "Self", "nil", "true", "false",
                    "some", "any", "where", "in", "as", "is", "extension", "typealias",
                    "associatedtype", "defer", "break", "continue", "fallthrough"]
        case "go", "golang":
            return ["package", "import", "func", "var", "const", "type", "struct",
                    "interface", "map", "chan", "range", "if", "else", "switch", "case",
                    "default", "for", "return", "go", "select", "defer", "break",
                    "continue", "fallthrough", "nil", "true", "false", "err"]
        case "javascript", "js", "typescript", "ts", "jsx", "tsx":
            return ["import", "export", "from", "function", "const", "let", "var",
                    "class", "extends", "implements", "interface", "type", "enum",
                    "if", "else", "switch", "case", "default", "for", "while", "do",
                    "return", "async", "await", "try", "catch", "throw", "new",
                    "this", "super", "null", "undefined", "true", "false", "of", "in"]
        case "python", "py":
            return ["import", "from", "def", "class", "if", "elif", "else", "for",
                    "while", "return", "yield", "async", "await", "try", "except",
                    "finally", "raise", "with", "as", "pass", "break", "continue",
                    "lambda", "None", "True", "False", "and", "or", "not", "in", "is",
                    "self", "global", "nonlocal"]
        case "rust", "rs":
            return ["fn", "let", "mut", "const", "struct", "enum", "impl", "trait",
                    "pub", "mod", "use", "crate", "self", "super", "if", "else",
                    "match", "for", "while", "loop", "return", "async", "await",
                    "move", "where", "type", "true", "false", "None", "Some", "Ok",
                    "Err", "unsafe", "extern"]
        case "c", "cpp", "c++", "h", "hpp":
            return ["include", "define", "ifdef", "ifndef", "endif", "int", "char",
                    "float", "double", "void", "bool", "struct", "class", "enum",
                    "union", "typedef", "if", "else", "switch", "case", "default",
                    "for", "while", "do", "return", "break", "continue", "const",
                    "static", "extern", "volatile", "nullptr", "NULL", "true", "false",
                    "public", "private", "protected", "virtual", "override", "template",
                    "namespace", "using", "auto"]
        case "ruby", "rb":
            return ["def", "end", "class", "module", "if", "elsif", "else", "unless",
                    "while", "until", "for", "do", "return", "yield", "begin", "rescue",
                    "ensure", "raise", "require", "include", "attr_accessor",
                    "attr_reader", "attr_writer", "nil", "true", "false", "self",
                    "puts", "print"]
        case "shell", "sh", "bash", "zsh":
            return ["if", "then", "else", "elif", "fi", "for", "while", "do", "done",
                    "case", "esac", "function", "return", "exit", "echo", "export",
                    "local", "readonly", "set", "unset", "shift", "true", "false",
                    "in", "source"]
        default:
            return []
        }
    }
}
