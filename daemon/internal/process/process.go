// Package process detects the foreground process for a given terminal session,
// used to activate AI-enhanced parsing when tools like Claude Code are running.
package process

import "github.com/canopy-dev/canopyd/internal/session"

// KnownTools maps process names to tool types.
var KnownTools = map[string]session.ToolType{
	"claude": session.ToolClaudeCode,
	"aider":  session.ToolAider,
	"goose":  session.ToolGoose,
	"codex":  session.ToolCodex,
}

// Info holds information about the current foreground process.
type Info struct {
	PID  int
	Name string
	Path string
	CWD  string
}

// ToolTypeForProcess returns the ToolType for a process name, or ToolGeneric.
func ToolTypeForProcess(name string) session.ToolType {
	if t, ok := KnownTools[name]; ok {
		return t
	}
	return session.ToolGeneric
}
