package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	hookStartMarker = "# --- Canopy Hook (do not edit) ---"
	hookEndMarker   = "# --- End Canopy Hook ---"
)

// ShellHook contains the hook code and the shell integration markers for a given shell.
type ShellHook struct {
	Hook        string // The exec hook for .zshrc / .bashrc / config.fish
	Integration string // The OSC 133 shell integration markers
}

// ZshHook returns the zsh hook code and shell integration markers.
func ZshHook() ShellHook {
	return ShellHook{
		Hook: `# --- Canopy Hook (do not edit) ---
if [ -z "$CANOPY_SESSION_ID" ] && command -v canopyd &>/dev/null && canopyd daemon ping &>/dev/null; then
  export CANOPY_SESSION_ID=$(uuidgen)
  exec canopyd attach --session-id "$CANOPY_SESSION_ID" -- "$SHELL" -l
fi
# --- End Canopy Hook ---`,
		Integration: `# --- Canopy Shell Integration (do not edit) ---
__canopy_precmd() {
  local exit_code=$?
  printf '\e]133;D;%s\a' "$exit_code"
  printf '\e]133;A\a'
}
__canopy_preexec() {
  printf '\e]133;C\a'
}
autoload -Uz add-zsh-hook 2>/dev/null && {
  add-zsh-hook precmd __canopy_precmd
  add-zsh-hook preexec __canopy_preexec
}
# --- End Canopy Shell Integration ---`,
	}
}

// BashHook returns the bash hook code and shell integration markers.
func BashHook() ShellHook {
	return ShellHook{
		Hook: `# --- Canopy Hook (do not edit) ---
if [ -z "$CANOPY_SESSION_ID" ] && command -v canopyd &>/dev/null && canopyd daemon ping &>/dev/null; then
  export CANOPY_SESSION_ID=$(uuidgen)
  exec canopyd attach --session-id "$CANOPY_SESSION_ID" -- "$SHELL" -l
fi
# --- End Canopy Hook ---`,
		Integration: `# --- Canopy Shell Integration (do not edit) ---
__canopy_precmd() {
  local exit_code=$?
  printf '\e]133;D;%s\a' "$exit_code"
  printf '\e]133;A\a'
}
__canopy_preexec() {
  printf '\e]133;C\a'
}
PROMPT_COMMAND="__canopy_precmd${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
trap '__canopy_preexec' DEBUG
# --- End Canopy Shell Integration ---`,
	}
}

// FishHook returns the fish hook code and shell integration markers.
func FishHook() ShellHook {
	return ShellHook{
		Hook: `# --- Canopy Hook (do not edit) ---
if not set -q CANOPY_SESSION_ID; and command -v canopyd &>/dev/null; and canopyd daemon ping &>/dev/null
  set -gx CANOPY_SESSION_ID (uuidgen)
  exec canopyd attach --session-id $CANOPY_SESSION_ID -- $SHELL -l
end
# --- End Canopy Hook ---`,
		Integration: `# --- Canopy Shell Integration (do not edit) ---
function __canopy_postexec --on-event fish_postexec
  printf '\e]133;D;%s\a' $status
  printf '\e]133;A\a'
end
function __canopy_preexec --on-event fish_preexec
  printf '\e]133;C\a'
end
# --- End Canopy Shell Integration ---`,
	}
}

// ShellRCPath returns the rc file path for a given shell.
func ShellRCPath(shell string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory: %w", err)
	}

	switch shell {
	case "zsh":
		return filepath.Join(home, ".zshrc"), nil
	case "bash":
		return filepath.Join(home, ".bashrc"), nil
	case "fish":
		return filepath.Join(home, ".config", "fish", "config.fish"), nil
	default:
		return "", fmt.Errorf("unsupported shell: %s", shell)
	}
}

// InjectHook injects the Canopy hook and shell integration markers into a shell's rc file.
// Idempotent: detects existing hooks by marker comments and replaces them.
func InjectHook(shell string) error {
	rcPath, err := ShellRCPath(shell)
	if err != nil {
		return err
	}

	var hook ShellHook
	switch shell {
	case "zsh":
		hook = ZshHook()
	case "bash":
		hook = BashHook()
	case "fish":
		hook = FishHook()
	default:
		return fmt.Errorf("unsupported shell: %s", shell)
	}

	// Ensure the directory exists (for fish).
	if err := os.MkdirAll(filepath.Dir(rcPath), 0755); err != nil {
		return fmt.Errorf("create rc directory: %w", err)
	}

	content, err := os.ReadFile(rcPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", rcPath, err)
	}

	text := string(content)

	// Remove existing hook if present, then append fresh.
	text = removeBlock(text, hookStartMarker, hookEndMarker)
	text = removeBlock(text, "# --- Canopy Shell Integration (do not edit) ---", "# --- End Canopy Shell Integration ---")

	// Ensure trailing newline before appending.
	text = strings.TrimRight(text, "\n") + "\n\n"
	text += hook.Hook + "\n\n"
	text += hook.Integration + "\n"

	return os.WriteFile(rcPath, []byte(text), 0644)
}

// RemoveHook removes the Canopy hook and integration markers from a shell's rc file.
func RemoveHook(shell string) error {
	rcPath, err := ShellRCPath(shell)
	if err != nil {
		return err
	}

	content, err := os.ReadFile(rcPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", rcPath, err)
	}

	text := string(content)
	text = removeBlock(text, hookStartMarker, hookEndMarker)
	text = removeBlock(text, "# --- Canopy Shell Integration (do not edit) ---", "# --- End Canopy Shell Integration ---")

	// Clean up extra blank lines left behind.
	for strings.Contains(text, "\n\n\n") {
		text = strings.ReplaceAll(text, "\n\n\n", "\n\n")
	}

	return os.WriteFile(rcPath, []byte(text), 0644)
}

// removeBlock removes a marked block (inclusive of start and end marker lines) from text.
func removeBlock(text, startMarker, endMarker string) string {
	startIdx := strings.Index(text, startMarker)
	if startIdx < 0 {
		return text
	}
	endIdx := strings.Index(text[startIdx:], endMarker)
	if endIdx < 0 {
		return text
	}
	// endIdx is relative to startIdx. Find the end of the endMarker line.
	endAbsolute := startIdx + endIdx + len(endMarker)
	// Skip the trailing newline if present.
	if endAbsolute < len(text) && text[endAbsolute] == '\n' {
		endAbsolute++
	}
	return text[:startIdx] + text[endAbsolute:]
}

// DetectShells returns which shells have rc files present on this system.
func DetectShells() []string {
	var shells []string
	for _, shell := range []string{"zsh", "bash", "fish"} {
		rcPath, err := ShellRCPath(shell)
		if err != nil {
			continue
		}
		// For zsh and bash, the file might not exist yet but the shell is likely present.
		// Check if the shell binary exists.
		if shell == "fish" {
			if _, err := os.Stat(rcPath); err != nil {
				// Fish config doesn't exist and fish might not be installed.
				if _, err := os.Stat("/usr/local/bin/fish"); err != nil {
					if _, err := os.Stat("/opt/homebrew/bin/fish"); err != nil {
						continue
					}
				}
			}
		}
		shells = append(shells, shell)
	}
	return shells
}
