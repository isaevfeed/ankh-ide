package app

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"ankh-ide/internal/state"
)

type events struct {
	mu   sync.Mutex
	list []string
	data []any
}

func (e *events) emit(name string, data ...any) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.list = append(e.list, name)
	if len(data) > 0 {
		e.data = append(e.data, data[0])
	} else {
		e.data = append(e.data, nil)
	}
	return false
}

func (e *events) wait(t *testing.T, match func(name string, data any) bool) {
	t.Helper()
	end := time.Now().Add(5 * time.Second)
	for time.Now().Before(end) {
		e.mu.Lock()
		for i, n := range e.list {
			if match(n, e.data[i]) {
				e.mu.Unlock()
				return
			}
		}
		e.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("event not seen; got %v", e.list)
}

func newTestService(t *testing.T) (*Service, *events, string) {
	home := shortHome(t)
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/sh")
	for _, kv := range [][2]string{{"GIT_AUTHOR_NAME", "t"}, {"GIT_AUTHOR_EMAIL", "t@t"}, {"GIT_COMMITTER_NAME", "t"}, {"GIT_COMMITTER_EMAIL", "t@t"}} {
		t.Setenv(kv[0], kv[1])
	}
	repo := filepath.Join(home, "proj")
	_ = os.MkdirAll(repo, 0o755)
	run(t, repo, "init", "-q", "-b", "main")
	_ = os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n\nfunc A() {\n\treturn\n}\n"), 0o644)
	run(t, repo, "add", ".")
	run(t, repo, "commit", "-q", "-m", "init")
	ev := &events{}
	s, err := New(&Host{Emit: ev.emit}, filepath.Join(home, "cfg"), "/bin/true")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Shutdown(s) })
	return s, ev, repo
}

func TestFilesIndexCommitAndRelocate(t *testing.T) {
	s, _, repo := newTestService(t)
	p, err := s.AddProjectPath(repo)
	if err != nil {
		t.Fatal(err)
	}
	sid := p.Sessions[0].ID
	_ = os.WriteFile(filepath.Join(repo, "b.go"), []byte("package a\n"), 0o644)
	files, err := s.ListFiles(sid)
	sort.Strings(files)
	if err != nil || strings.Join(files, ",") != "a.go,b.go" {
		t.Fatalf("files = %v %v", files, err)
	}

	// comment on line 4, then two lines are inserted above it
	if _, err := s.AddComment(sid, state.Comment{Path: "a.go", Side: "new", Line: 4, Col: 2, Anchor: "\treturn", Before: "func A() {", Text: "верни ошибку"}); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n\n// A does a.\n// More.\nfunc A() {\n\treturn\n}\n"), 0o644)
	d, err := s.GetDiff(sid, "wip")
	if err != nil {
		t.Fatal(err)
	}
	if d.Relocated != 1 || d.Index["a.go"] != "" || d.Index["b.go"] != "" {
		t.Fatalf("diff = %+v", d)
	}
	_, ss, _ := s.session(sid)
	if c := ss.Comments[0]; c.Line != 6 || c.Loc != "a.go:6:2" || c.MovedFrom != 4 {
		t.Fatalf("comment = %+v", c)
	}

	if err := s.Stage(sid, []string{"a.go"}); err != nil {
		t.Fatal(err)
	}
	d, _ = s.GetDiff(sid, "wip")
	if d.Index["a.go"] != "staged" {
		t.Fatalf("index = %v", d.Index)
	}
	h, err := s.Commit(sid, "docs", false)
	if err != nil || h == "" {
		t.Fatalf("commit %q %v", h, err)
	}
	if err := s.Discard(sid, []string{"b.go"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "b.go")); !os.IsNotExist(err) {
		t.Fatal("b.go must be deleted")
	}
}

func TestDockBadge(t *testing.T) {
	s, _, repo := newTestService(t)
	var mu sync.Mutex
	var badges []string
	s.host.SetBadge = func(l string) { mu.Lock(); badges = append(badges, l); mu.Unlock() }
	p, _ := s.AddProjectPath(repo)
	sid := p.Sessions[0].ID
	s.setStatus(sid, StatusWaiting)
	s.setStatus("other", StatusWaiting)
	s.MarkSeen(sid)
	s.setStatus("other", StatusWorking)
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(badges, "|") != "1|2|1|" {
		t.Fatalf("badges = %q", badges)
	}
}

// fakeACP answers initialize and session/new, and on a prompt streams a
// chunk, asks for permission and finishes with the chosen option.
const fakeACP = `import json, sys
def send(m):
    sys.stdout.write(json.dumps(m) + "\n"); sys.stdout.flush()
def read():
    while True:
        line = sys.stdin.readline()
        if not line: sys.exit(0)
        line = line.strip()
        if line: return json.loads(line)
while True:
    m = read()
    if m.get("method") == "initialize":
        send({"jsonrpc": "2.0", "id": m["id"], "result": {"protocolVersion": 1, "agentCapabilities": {}}})
    elif m.get("method") == "session/new":
        send({"jsonrpc": "2.0", "id": m["id"], "result": {"sessionId": "s1"}})
    elif m.get("method") == "session/prompt":
        text = m["params"]["prompt"][0]["text"]
        send({"jsonrpc": "2.0", "method": "session/update", "params": {"sessionId": "s1", "update": {"sessionUpdate": "agent_message_chunk", "content": {"type": "text", "text": "echo: " + text}}}})
        send({"jsonrpc": "2.0", "id": 99, "method": "fs/read_text_file", "params": {"sessionId": "s1", "path": sys.argv[1] + "/a.go", "line": 1, "limit": 1}})
        r = read()
        send({"jsonrpc": "2.0", "method": "session/update", "params": {"sessionId": "s1", "update": {"sessionUpdate": "agent_message_chunk", "content": {"type": "text", "text": r["result"]["content"]}}}})
        send({"jsonrpc": "2.0", "id": 100, "method": "session/request_permission", "params": {"sessionId": "s1", "toolCall": {"toolCallId": "t1", "title": "Edit a.go"}, "options": [{"optionId": "allow", "name": "Allow", "kind": "allow_once"}]}})
        r = read()
        send({"jsonrpc": "2.0", "id": m["id"], "result": {"stopReason": r["result"]["outcome"].get("optionId", "cancelled")}})
`

func TestACPChat(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("no python3")
	}
	s, ev, repo := newTestService(t)
	script := filepath.Join(t.TempDir(), "fake_acp.py")
	_ = os.WriteFile(script, []byte(fakeACP), 0o644)
	real, _ := filepath.EvalSymlinks(repo)
	_ = s.store.Update(func(d *state.Data) error {
		d.Settings.ACP = map[string]string{"claude": py + " " + script + " " + real}
		return nil
	})
	p, _ := s.AddProjectPath(repo)
	sid := p.Sessions[0].ID
	if err := s.ACPStart(sid); err != nil {
		t.Fatal(err)
	}
	isState := func(st string) func(string, any) bool {
		return func(n string, d any) bool {
			m, ok := d.(map[string]any)
			return n == "acp:state" && ok && m["state"] == st
		}
	}
	ev.wait(t, isState("ready"))
	if err := s.ACPPrompt(sid, "hi"); err != nil {
		t.Fatal(err)
	}
	var permID string
	ev.wait(t, func(n string, d any) bool {
		m, ok := d.(map[string]any)
		if n != "acp:event" || !ok {
			return false
		}
		e := m["event"].(ACPEvent)
		if e.Kind == "permission" {
			permID = e.Perm.ID
			return true
		}
		return false
	})
	if s.status[sid] != StatusWaiting {
		t.Fatalf("status while asking = %s", s.status[sid])
	}
	if err := s.ACPPermission(sid, permID, "allow"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, func(n string, d any) bool {
		m, ok := d.(map[string]any)
		return n == "acp:event" && ok && m["event"].(ACPEvent).Kind == "stop" && m["event"].(ACPEvent).Text == "allow"
	})
	info, _ := s.ACPState(sid)
	var texts []string
	for _, e := range info.Events {
		if e.Kind == "update" {
			var u struct {
				Content struct{ Text string } `json:"content"`
			}
			_ = json.Unmarshal(e.Update, &u)
			texts = append(texts, u.Content.Text)
		}
	}
	if strings.Join(texts, "|") != "echo: hi|package a\n" {
		t.Fatalf("texts = %q", texts)
	}
	s.ACPStop(sid)
	if info, _ := s.ACPState(sid); info.State != "closed" {
		t.Fatalf("state = %s", info.State)
	}
}
