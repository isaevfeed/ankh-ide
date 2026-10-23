package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// TmuxSocket is the tmux server Ankh uses (`tmux -L ankh`), kept apart from
// the user's own tmux sessions and config.
const TmuxSocket = "ankh"

// tmuxConf hides tmux itself: no status line, mouse wheel scrolls history,
// colours and keys pass through to the agent. -q: older tmux versions skip
// options they do not know.
const tmuxConf = `# Written by Ankh. Agent sessions run here to survive app restarts.
set -gq status off
set -gq mouse on
set -gq history-limit 100000
set -gq default-terminal "xterm-256color"
set -gqa terminal-overrides ",xterm-256color:Tc"
set -sgq terminal-features ",xterm-256color:RGB"
set -sgq escape-time 0
set -gq focus-events on
set -gq window-size latest
set -gq allow-passthrough on
set -gq set-titles off
set -gq exit-empty on
set -gq destroy-unattached off
`

// FindTmux returns the tmux binary, looking also where Homebrew installs it
// (apps started from the Dock have a short PATH).
func FindTmux() string {
	if p, err := exec.LookPath("tmux"); err == nil {
		return p
	}
	for _, d := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/opt/local/bin", "/usr/bin"} {
		p := filepath.Join(d, "tmux")
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// WriteTmuxConf writes the config Ankh's tmux server starts with.
func WriteTmuxConf(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(tmuxConf), 0o600)
}

// TmuxName is the tmux session of an Ankh session.
func TmuxName(sessionID string) string { return "ankh-" + sessionID }

// TmuxCommandLine attaches to the agent's tmux session, creating it with
// cmdline when it does not exist. env (KEY=VALUE) is set in a new session
// only — an existing one keeps what it started with.
func TmuxCommandLine(tmux, conf, name, dir string, env []string, cmdline string) string {
	parts := []string{shellQuote(tmux), "-u", "-L", TmuxSocket, "-f", shellQuote(conf), "new-session", "-A", "-s", shellQuote(name), "-c", shellQuote(dir)}
	for _, kv := range env {
		parts = append(parts, "-e", shellQuote(kv))
	}
	parts = append(parts, shellQuote(cmdline))
	return strings.Join(parts, " ")
}

// TmuxHas reports whether the tmux session exists.
func TmuxHas(tmux, name string) bool {
	if tmux == "" {
		return false
	}
	return exec.Command(tmux, "-L", TmuxSocket, "has-session", "-t", "="+name).Run() == nil
}

// TmuxKill ends the tmux session and the agent in it.
func TmuxKill(tmux, name string) {
	if tmux == "" {
		return
	}
	_ = exec.Command(tmux, "-L", TmuxSocket, "kill-session", "-t", "="+name).Run()
}
