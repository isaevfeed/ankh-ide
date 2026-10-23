package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"ankh-ide/internal/acp"
	"ankh-ide/internal/agent"
	"ankh-ide/internal/fstree"
	"ankh-ide/internal/state"
)

// acpSession is a chat-mode agent: the same CLI agent, but driven through
// the Agent Client Protocol, so messages, tool calls and permission requests
// arrive as structured data instead of terminal output.
type acpSession struct {
	s       *Service
	id      string // Ankh session
	root    string
	conn    *acp.Conn
	agentID string // ACP sessionId

	mu      sync.Mutex
	state   string // starting | ready | working | closed
	errText string
	events  []ACPEvent // replayed when the chat view mounts again
	perms   map[string]chan string
	permSeq int
}

// ACPEvent is one item of the chat transcript.
type ACPEvent struct {
	Kind   string          `json:"kind"` // user | update | permission | permission:done | error | stop
	Text   string          `json:"text,omitempty"`
	Update json.RawMessage `json:"update,omitempty"`
	Perm   *ACPPermission  `json:"perm,omitempty"`
}

type ACPPermission struct {
	ID       string          `json:"id"`
	ToolCall json.RawMessage `json:"toolCall"`
	Options  json.RawMessage `json:"options"`
	Chosen   string          `json:"chosen,omitempty"`
}

// ACPInfo is the chat state for the UI.
type ACPInfo struct {
	State   string     `json:"state"`
	Error   string     `json:"error,omitempty"`
	Command string     `json:"command"`
	Events  []ACPEvent `json:"events"`
}

const maxACPEvents = 4000

func (a *acpSession) push(e ACPEvent) {
	a.mu.Lock()
	a.events = append(a.events, e)
	if len(a.events) > maxACPEvents {
		a.events = a.events[len(a.events)-maxACPEvents:]
	}
	a.mu.Unlock()
	a.s.emit("acp:event", map[string]any{"session": a.id, "event": e})
}

func (a *acpSession) setState(st, errText string) {
	a.mu.Lock()
	a.state, a.errText = st, errText
	a.mu.Unlock()
	a.s.emit("acp:state", map[string]any{"session": a.id, "state": st, "error": errText})
	switch st {
	case "working":
		a.s.setStatus(a.id, StatusWorking)
	case "ready":
		a.s.setStatus(a.id, StatusWaiting)
	case "closed":
		a.s.setStatus(a.id, StatusIdle)
	}
}

func (a *acpSession) HandleNotification(method string, params json.RawMessage) {
	if method != "session/update" {
		return
	}
	var p struct {
		Update json.RawMessage `json:"update"`
	}
	if json.Unmarshal(params, &p) == nil && len(p.Update) > 0 {
		a.push(ACPEvent{Kind: "update", Update: p.Update})
	}
}

func (a *acpSession) HandleRequest(method string, params json.RawMessage) (any, *acp.RPCError) {
	switch method {
	case "session/request_permission":
		var p struct {
			ToolCall json.RawMessage `json:"toolCall"`
			Options  json.RawMessage `json:"options"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &acp.RPCError{Code: -32602, Message: err.Error()}
		}
		a.mu.Lock()
		a.permSeq++
		id := fmt.Sprint(a.permSeq)
		ch := make(chan string, 1)
		a.perms[id] = ch
		a.mu.Unlock()
		a.push(ACPEvent{Kind: "permission", Perm: &ACPPermission{ID: id, ToolCall: p.ToolCall, Options: p.Options}})
		a.s.setStatus(a.id, StatusWaiting)
		choice := <-ch
		a.push(ACPEvent{Kind: "permission:done", Perm: &ACPPermission{ID: id, Chosen: choice}})
		if choice == "" {
			return map[string]any{"outcome": map[string]any{"outcome": "cancelled"}}, nil
		}
		a.s.setStatus(a.id, StatusWorking)
		return map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": choice}}, nil

	case "fs/read_text_file":
		var p struct {
			Path  string `json:"path"`
			Line  int    `json:"line"`
			Limit int    `json:"limit"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &acp.RPCError{Code: -32602, Message: err.Error()}
		}
		full, err := a.inside(p.Path)
		if err != nil {
			return nil, &acp.RPCError{Code: -32602, Message: err.Error()}
		}
		b, err := os.ReadFile(full)
		if err != nil {
			return nil, &acp.RPCError{Code: -32002, Message: err.Error()}
		}
		content := string(b)
		if p.Line > 0 || p.Limit > 0 {
			lines := strings.SplitAfter(content, "\n")
			from := max(p.Line-1, 0)
			from = min(from, len(lines))
			to := len(lines)
			if p.Limit > 0 {
				to = min(from+p.Limit, len(lines))
			}
			content = strings.Join(lines[from:to], "")
		}
		return map[string]string{"content": content}, nil

	case "fs/write_text_file":
		var p struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &acp.RPCError{Code: -32602, Message: err.Error()}
		}
		full, err := a.inside(p.Path)
		if err != nil {
			return nil, &acp.RPCError{Code: -32602, Message: err.Error()}
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return nil, &acp.RPCError{Code: -32603, Message: err.Error()}
		}
		mode := os.FileMode(0o644)
		if fi, err := os.Stat(full); err == nil {
			mode = fi.Mode().Perm()
		}
		if err := os.WriteFile(full, []byte(p.Content), mode); err != nil {
			return nil, &acp.RPCError{Code: -32603, Message: err.Error()}
		}
		return nil, nil
	}
	return nil, &acp.RPCError{Code: -32601, Message: "method not found: " + method}
}

// inside resolves an absolute path the agent asked for and keeps it within
// the session folder.
func (a *acpSession) inside(p string) (string, error) {
	if !filepath.IsAbs(p) {
		return fstree.Resolve(a.root, p)
	}
	rel, err := filepath.Rel(a.root, filepath.Clean(p))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("путь вне папки сессии: %s", p)
	}
	return filepath.Join(a.root, rel), nil
}

func (s *Service) acpCommand(ss *state.Session) string {
	pr := agent.Find(ss.Agent)
	cmd := pr.ACP
	s.store.View(func(d *state.Data) {
		if v := strings.TrimSpace(d.Settings.ACP[pr.ID]); v != "" {
			cmd = v
		}
	})
	return cmd
}

func (s *Service) getACP(sessionID string) *acpSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.acp[sessionID]
}

// ACPState returns the chat transcript and state of a session ("" state:
// chat mode was not started).
func (s *Service) ACPState(sessionID string) (*ACPInfo, error) {
	_, ss, err := s.session(sessionID)
	if err != nil {
		return nil, err
	}
	info := &ACPInfo{Command: s.acpCommand(ss), Events: []ACPEvent{}}
	if a := s.getACP(sessionID); a != nil {
		a.mu.Lock()
		info.State, info.Error = a.state, a.errText
		info.Events = append(info.Events, a.events...)
		a.mu.Unlock()
	}
	return info, nil
}

// ACPStart launches the session's agent in chat mode.
func (s *Service) ACPStart(sessionID string) error {
	p, ss, err := s.session(sessionID)
	if err != nil {
		return err
	}
	if a := s.getACP(sessionID); a != nil {
		a.mu.Lock()
		st := a.state
		a.mu.Unlock()
		if st != "closed" {
			return nil
		}
	}
	command := s.acpCommand(ss)
	if command == "" {
		return errors.New("для этого агента не задана команда ACP")
	}
	a := &acpSession{s: s, id: sessionID, root: ss.Path, state: "starting", perms: map[string]chan string{}}
	if real, err := filepath.EvalSymlinks(a.root); err == nil {
		a.root = real
	}
	s.mu.Lock()
	if old := s.acp[sessionID]; old != nil {
		a.events = old.events // keep the transcript across restarts
	}
	s.acp[sessionID] = a
	s.mu.Unlock()
	a.setState("starting", "")
	env := s.env(p, ss)
	go func() {
		conn, err := acp.Start(agent.Argv(command, ""), ss.Path, env, a)
		if err != nil {
			a.setState("closed", err.Error())
			return
		}
		a.conn = conn
		go func() {
			<-conn.Done()
			a.mu.Lock()
			st := a.state
			for id, ch := range a.perms {
				ch <- ""
				delete(a.perms, id)
			}
			a.mu.Unlock()
			if st != "closed" {
				msg := ""
				if err := conn.Err(); err != nil {
					msg = err.Error()
				}
				a.setState("closed", msg)
			}
		}()
		var init struct {
			ProtocolVersion int `json:"protocolVersion"`
		}
		err = conn.Call("initialize", map[string]any{
			"protocolVersion": acp.ProtocolVersion,
			"clientCapabilities": map[string]any{
				"fs":       map[string]bool{"readTextFile": true, "writeTextFile": true},
				"terminal": false,
			},
			"clientInfo": map[string]string{"name": "ankh", "title": "Ankh", "version": "0.2.0"},
		}, &init)
		if err != nil {
			s.logf("acp: initialize: %v", err)
			a.setState("closed", "initialize: "+err.Error())
			conn.Close()
			return
		}
		var sess struct {
			SessionID string `json:"sessionId"`
		}
		if err := conn.Call("session/new", map[string]any{"cwd": ss.Path, "mcpServers": []any{}}, &sess); err != nil {
			msg := err.Error()
			if strings.Contains(strings.ToLower(msg), "auth") {
				msg += " — войдите в агента в обычном терминале (например, claude → /login) и перезапустите чат"
			}
			a.setState("closed", "session/new: "+msg)
			conn.Close()
			return
		}
		a.agentID = sess.SessionID
		s.logf("acp: session %s started (%s)", sess.SessionID, command)
		a.setState("ready", "")
	}()
	return nil
}

// ACPPrompt sends a message to the chat agent; the answer streams as events.
func (s *Service) ACPPrompt(sessionID, text string) error {
	a := s.getACP(sessionID)
	if a == nil {
		return errors.New("чат не запущен")
	}
	a.mu.Lock()
	st := a.state
	a.mu.Unlock()
	if st == "working" {
		return errors.New("агент ещё отвечает — дождитесь или нажмите «Стоп»")
	}
	if st != "ready" || a.conn == nil {
		return errors.New("чат не готов")
	}
	a.push(ACPEvent{Kind: "user", Text: text})
	a.setState("working", "")
	go func() {
		var res struct {
			StopReason string `json:"stopReason"`
		}
		err := a.conn.Call("session/prompt", map[string]any{
			"sessionId": a.agentID,
			"prompt":    []any{map[string]string{"type": "text", "text": text}},
		}, &res)
		a.mu.Lock()
		closed := a.state == "closed"
		a.mu.Unlock()
		if closed {
			return
		}
		if err != nil {
			a.push(ACPEvent{Kind: "error", Text: err.Error()})
		} else {
			a.push(ACPEvent{Kind: "stop", Text: res.StopReason})
		}
		a.setState("ready", "")
	}()
	return nil
}

// ACPCancel stops the current answer.
func (s *Service) ACPCancel(sessionID string) error {
	a := s.getACP(sessionID)
	if a == nil || a.conn == nil {
		return nil
	}
	a.mu.Lock()
	for id, ch := range a.perms {
		ch <- ""
		delete(a.perms, id)
	}
	a.mu.Unlock()
	return a.conn.Notify("session/cancel", map[string]string{"sessionId": a.agentID})
}

// ACPPermission answers a permission request; "" rejects it.
func (s *Service) ACPPermission(sessionID, permID, optionID string) error {
	a := s.getACP(sessionID)
	if a == nil {
		return errors.New("чат не запущен")
	}
	a.mu.Lock()
	ch := a.perms[permID]
	delete(a.perms, permID)
	a.mu.Unlock()
	if ch == nil {
		return errors.New("запрос уже закрыт")
	}
	ch <- optionID
	return nil
}

// ACPStop ends the chat agent; the transcript stays until ACPClear.
func (s *Service) ACPStop(sessionID string) {
	a := s.getACP(sessionID)
	if a == nil {
		return
	}
	a.setState("closed", "")
	a.mu.Lock()
	for id, ch := range a.perms {
		ch <- ""
		delete(a.perms, id)
	}
	a.mu.Unlock()
	if a.conn != nil {
		a.conn.Close()
	}
}

// ACPClear stops the chat and forgets its transcript.
func (s *Service) ACPClear(sessionID string) {
	s.ACPStop(sessionID)
	s.mu.Lock()
	delete(s.acp, sessionID)
	s.mu.Unlock()
}

func (s *Service) closeAllACP() {
	s.mu.Lock()
	ids := make([]string, 0, len(s.acp))
	for id := range s.acp {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.ACPStop(id)
	}
}
