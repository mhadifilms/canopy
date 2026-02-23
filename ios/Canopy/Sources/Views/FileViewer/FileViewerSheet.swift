import SwiftUI

/// Half-screen sheet for viewing file contents read-only.
///
/// Line numbers, monospace, horizontal scroll, pinch-to-zoom.
struct FileViewerSheet: View {
    let path: String
    let content: String
    let language: String?

    @State private var fontSize: CGFloat = 12

    private var fileName: String {
        (path as NSString).lastPathComponent
    }

    private var lines: [String] {
        content.components(separatedBy: "\n")
    }

    var body: some View {
        NavigationStack {
            ScrollView([.horizontal, .vertical]) {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(Array(lines.enumerated()), id: \.offset) { index, line in
                        HStack(alignment: .top, spacing: 0) {
                            Text("\(index + 1)")
                                .font(.system(size: fontSize, design: .monospaced))
                                .foregroundStyle(.tertiary)
                                .frame(minWidth: 40, alignment: .trailing)
                                .padding(.trailing, 8)

                            Text(line)
                                .font(.system(size: fontSize, design: .monospaced))
                                .textSelection(.enabled)
                        }
                        .padding(.horizontal, 8)
                        .padding(.vertical, 1)
                    }
                }
                .padding(.vertical, 8)
            }
            .background(Color(.systemBackground))
            .navigationTitle(fileName)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Menu {
                        Button {
                            fontSize = max(8, fontSize - 2)
                        } label: {
                            Label("Smaller text", systemImage: "textformat.size.smaller")
                        }
                        Button {
                            fontSize = min(24, fontSize + 2)
                        } label: {
                            Label("Larger text", systemImage: "textformat.size.larger")
                        }
                    } label: {
                        Image(systemName: "textformat.size")
                    }
                    .accessibilityLabel("Adjust font size")
                }
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("File viewer: \(fileName)")
    }
}
