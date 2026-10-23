// Package agent describes CLI agents, builds the command that starts them and
// receives status/usage messages from Claude Code hooks and statusLine.
package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Preset struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Command string `json:"command"`
	// Claude marks Claude Code: it gets our hooks/statusLine via --settings
	// and can resume with --continue.
	Claude bool `json:"claude"`
	// FileRef is how a file is referenced in a prompt.
	FileRef string `json:"fileRef"`
	// ACP starts the agent in chat mode (Agent Client Protocol over stdio).
	ACP string `json:"acp"`
}

var Presets = []Preset{
	{ID: "claude", Name: "Claude Code", Command: "claude", Claude: true, FileRef: "@{path}", ACP: "npx -y @agentclientprotocol/claude-agent-acp"},
	{ID: "codex", Name: "Codex CLI", Command: "codex", FileRef: "@{path}", ACP: "npx -y @zed-industries/codex-acp"},
	{ID: "gemini", Name: "Gemini CLI", Command: "gemini", FileRef: "@{path}", ACP: "gemini --experimental-acp"},
	{ID: "opencode", Name: "OpenCode", Command: "opencode", FileRef: "@{path}", ACP: "opencode acp"},
}

func Find(id string) Preset {
	for _, p := range Presets {
		if p.ID == id {
			return p
		}
	}
	return Presets[0]
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// CommandLine is the shell command that starts the agent.
func (p Preset) CommandLine(settingsPath string, resume bool) string {
	c := p.Command
	if p.Claude {
		if settingsPath != "" {
			c += " --settings " + shellQuote(settingsPath)
		}
		if resume {
			c += " --continue"
		}
	}
	return c
}

// LoginShell returns the user's shell (zsh on macOS by default).
func LoginShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/zsh"
}

// Argv wraps a command line in an interactive login shell, so PATH, proxies
// and other variables from the user's dotfiles apply even when the app was
// launched from the Dock. The setup script, if any, runs first.
func Argv(cmdline, setup string) []string {
	full := "exec " + cmdline
	if strings.TrimSpace(setup) != "" {
		full = "{ " + setup + " ; } ; " + full
	}
	return []string{LoginShell(), "-l", "-i", "-c", full}
}

// ShellArgv starts a plain interactive login shell.
func ShellArgv() []string { return []string{LoginShell(), "-l", "-i"} }

// WriteClaudeSettings writes the settings file passed to `claude --settings`.
// It adds hooks that report status and a statusLine that reports usage.
// exe is this app's binary; it handles the `hook` and `statusline` subcommands.
func WriteClaudeSettings(path, exe string) error {
	cmd := func(args string) map[string]any {
		return map[string]any{"type": "command", "command": shellQuote(exe) + " " + args}
	}
	hook := func(event string) []any {
		return []any{map[string]any{"hooks": []any{cmd("hook " + event)}}}
	}
	settings := map[string]any{
		"hooks": map[string]any{
			"UserPromptSubmit": hook("prompt"),
			"Stop":             hook("stop"),
			"Notification":     hook("notification"),
			"SessionStart":     hook("start"),
		},
		"statusLine": map[string]any{
			"type":    "command",
			"command": shellQuote(exe) + " statusline",
			"padding": 0,
		},
	}
	b, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// Usage is what the bottom bar shows.
type Usage struct {
	FiveHour *Window `json:"fiveHour,omitempty"`
	SevenDay *Window `json:"sevenDay,omitempty"`
	Model    string  `json:"model,omitempty"`
	At       int64   `json:"at"` // unix seconds
	Source   string  `json:"source"`
}

type Window struct {
	UsedPercentage float64 `json:"usedPercentage"`
	ResetsAt       int64   `json:"resetsAt"`             // unix seconds, from statusLine
	ResetsText     string  `json:"resetsText,omitempty"` // as printed by /usage
}

// ParseStatusLine extracts usage from the JSON Claude Code sends to statusLine.
// It returns nil when the payload has no rate limits.
func ParseStatusLine(payload []byte) *Usage {
	var p struct {
		Model struct {
			DisplayName string `json:"display_name"`
		} `json:"model"`
		RateLimits struct {
			FiveHour *struct {
				UsedPercentage float64 `json:"used_percentage"`
				ResetsAt       int64   `json:"resets_at"`
			} `json:"five_hour"`
			SevenDay *struct {
				UsedPercentage float64 `json:"used_percentage"`
				ResetsAt       int64   `json:"resets_at"`
			} `json:"seven_day"`
		} `json:"rate_limits"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil
	}
	if p.RateLimits.FiveHour == nil && p.RateLimits.SevenDay == nil {
		return nil
	}
	u := &Usage{Model: p.Model.DisplayName, Source: "statusline"}
	if w := p.RateLimits.FiveHour; w != nil {
		u.FiveHour = &Window{UsedPercentage: w.UsedPercentage, ResetsAt: w.ResetsAt}
	}
	if w := p.RateLimits.SevenDay; w != nil {
		u.SevenDay = &Window{UsedPercentage: w.UsedPercentage, ResetsAt: w.ResetsAt}
	}
	return u
}

// FileRef formats a file reference for a prompt, e.g. "@a/b.go:12-30".
func (p Preset) FormatRef(path string, from, to int) string {
	ref := strings.ReplaceAll(p.FileRef, "{path}", path)
	if ref == "" {
		ref = path
	}
	switch {
	case from > 0 && to > 0 && to != from:
		ref += fmt.Sprintf(":%d-%d", from, to)
	case from > 0:
		ref += fmt.Sprintf(":%d", from)
	}
	return ref
}

var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]`)

// ClaudeProjectDir is where Claude Code keeps conversations for a folder:
// ~/.claude/projects/<path with every non-alphanumeric char replaced by "-">.
func ClaudeProjectDir(cwd string) string {
	home, _ := os.UserHomeDir()
	if abs, err := filepath.Abs(cwd); err == nil {
		cwd = abs
	}
	if real, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = real
	}
	return filepath.Join(home, ".claude", "projects", nonAlnum.ReplaceAllString(cwd, "-"))
}

// HasConversation reports whether `claude --continue` has something to resume in cwd.
func HasConversation(cwd string) bool {
	for _, dir := range []string{ClaudeProjectDir(cwd), filepath.Join(filepath.Dir(ClaudeProjectDir(cwd)), nonAlnum.ReplaceAllString(cwd, "-"))} {
		m, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
		if len(m) > 0 {
			return true
		}
	}
	return false
}
