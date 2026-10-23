package agent

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Message travels from the `hook`/`statusline` subcommands to the app.
type Message struct {
	Type    string          `json:"type"`  // "hook" | "statusline"
	Event   string          `json:"event"` // prompt | stop | notification
	Session string          `json:"session"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Env names passed into agent terminals.
const (
	EnvSession = "ANKH_SESSION"
	EnvSocket  = "ANKH_SOCK"
)

// Listen serves messages on a unix socket until the listener is closed.
func Listen(path string, handle func(Message)) (net.Listener, error) {
	_ = os.Remove(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
				sc := bufio.NewScanner(c)
				sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
				for sc.Scan() {
					var m Message
					if json.Unmarshal(sc.Bytes(), &m) == nil {
						handle(m)
					}
				}
			}(c)
		}
	}()
	return l, nil
}

func send(m Message) {
	sock := os.Getenv(EnvSocket)
	if sock == "" {
		return
	}
	c, err := net.DialTimeout("unix", sock, 500*time.Millisecond)
	if err != nil {
		return
	}
	defer c.Close()
	_ = c.SetWriteDeadline(time.Now().Add(time.Second))
	b, _ := json.Marshal(m)
	_, _ = c.Write(append(b, '\n'))
}

// RunCLI handles `ankh hook <event>` and `ankh statusline`. It never fails
// loudly: a broken hook must not get in the agent's way.
func RunCLI(args []string, stdin io.Reader, stdout io.Writer) int {
	payload, _ := io.ReadAll(io.LimitReader(stdin, 4*1024*1024))
	if !json.Valid(payload) {
		payload = nil
	}
	session := os.Getenv(EnvSession)
	switch args[0] {
	case "hook":
		ev := ""
		if len(args) > 1 {
			ev = args[1]
		}
		send(Message{Type: "hook", Event: ev, Session: session, Payload: payload})
	case "statusline":
		send(Message{Type: "statusline", Session: session, Payload: payload})
		chainUserStatusLine(payload, stdout)
	}
	return 0
}

// chainUserStatusLine runs the statusLine command from ~/.claude/settings.json,
// if there is one, so the user's own status line keeps working.
func chainUserStatusLine(payload []byte, stdout io.Writer) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	b, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		return
	}
	var s struct {
		StatusLine struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		} `json:"statusLine"`
	}
	if json.Unmarshal(b, &s) != nil || s.StatusLine.Command == "" || strings.Contains(s.StatusLine.Command, " statusline") {
		return
	}
	cmd := exec.Command("/bin/sh", "-c", s.StatusLine.Command)
	cmd.Stdin = strings.NewReader(string(payload))
	cmd.Stdout = stdout
	cmd.Stderr = io.Discard
	done := make(chan struct{})
	go func() { _ = cmd.Run(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
}
