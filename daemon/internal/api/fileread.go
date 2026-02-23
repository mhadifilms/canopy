package api

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LanguageMap maps file extensions to language identifiers.
var LanguageMap = map[string]string{
	".go":    "go",
	".js":    "javascript",
	".ts":    "typescript",
	".tsx":   "typescriptreact",
	".jsx":   "javascriptreact",
	".py":    "python",
	".rb":    "ruby",
	".rs":    "rust",
	".java":  "java",
	".kt":    "kotlin",
	".swift": "swift",
	".c":     "c",
	".cpp":   "cpp",
	".h":     "c",
	".hpp":   "cpp",
	".cs":    "csharp",
	".php":   "php",
	".sh":    "shell",
	".bash":  "shell",
	".zsh":   "shell",
	".fish":  "shell",
	".json":  "json",
	".yaml":  "yaml",
	".yml":   "yaml",
	".toml":  "toml",
	".xml":   "xml",
	".html":  "html",
	".css":   "css",
	".scss":  "scss",
	".sql":   "sql",
	".md":    "markdown",
	".txt":   "plaintext",
	".dockerfile": "dockerfile",
	".tf":    "hcl",
	".lua":   "lua",
	".r":     "r",
	".ex":    "elixir",
	".exs":   "elixir",
	".erl":   "erlang",
	".zig":   "zig",
	".v":     "v",
	".nim":   "nim",
}

// DetectLanguage returns the language identifier from a file extension.
func DetectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if lang, ok := LanguageMap[ext]; ok {
		return lang
	}
	base := strings.ToLower(filepath.Base(path))
	if base == "dockerfile" || base == "makefile" {
		return base
	}
	return "plaintext"
}

// ReadFileRestricted reads a file, respecting the access root and max size.
func ReadFileRestricted(path string, accessRoot string, maxBytes int) ([]byte, error) {
	// Resolve to absolute path.
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	// Evaluate symlinks to prevent escaping the root.
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("eval symlinks: %w", err)
	}

	// Check access root.
	if accessRoot != "" {
		rootReal, err := filepath.EvalSymlinks(accessRoot)
		if err != nil {
			return nil, fmt.Errorf("eval root symlinks: %w", err)
		}
		if !strings.HasPrefix(real, rootReal+string(filepath.Separator)) && real != rootReal {
			return nil, fmt.Errorf("path %q is outside access root %q", path, accessRoot)
		}
	}

	// Check size.
	info, err := os.Stat(real)
	if err != nil {
		return nil, fmt.Errorf("stat: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}
	if maxBytes > 0 && info.Size() > int64(maxBytes) {
		return nil, fmt.Errorf("file too large: %d bytes (max %d)", info.Size(), maxBytes)
	}

	data, err := os.ReadFile(real)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}

	return data, nil
}
