package acp

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type rec struct {
	notes chan string
}

func (r *rec) HandleNotification(method string, params json.RawMessage) {
	r.notes <- method + " " + string(params)
}

func (r *rec) HandleRequest(method string, params json.RawMessage) (any, *RPCError) {
	if method == "fs/read_text_file" {
		return map[string]string{"content": "hello"}, nil
	}
	return nil, &RPCError{Code: -32601, Message: "no"}
}

// A fake agent in sh: answers initialize, sends an update, asks the client
// to read a file and echoes the answer.
const fake = `
echo 'shell noise'
read -r line; echo '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1}}'
read -r line; echo '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s","update":{"sessionUpdate":"agent_message_chunk"}}}'
echo '{"jsonrpc":"2.0","id":"r1","method":"fs/read_text_file","params":{"path":"/x"}}'
read -r answer
echo "{\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"echo\":$answer}}"
`

func TestConn(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip()
	}
	script := filepath.Join(t.TempDir(), "agent.sh")
	_ = os.WriteFile(script, []byte(fake), 0o755)
	r := &rec{notes: make(chan string, 4)}
	c, err := Start([]string{"sh", script}, t.TempDir(), os.Environ(), r)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var init struct {
		ProtocolVersion int `json:"protocolVersion"`
	}
	if err := c.Call("initialize", map[string]any{"protocolVersion": 1}, &init); err != nil || init.ProtocolVersion != 1 {
		t.Fatalf("init: %v %+v", err, init)
	}
	var out struct {
		Echo json.RawMessage `json:"echo"`
	}
	if err := c.Call("session/prompt", map[string]any{}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out.Echo), `"content":"hello"`) || !strings.Contains(string(out.Echo), `"id":"r1"`) {
		t.Fatalf("echo = %s", out.Echo)
	}
	select {
	case n := <-r.notes:
		if !strings.HasPrefix(n, "session/update") {
			t.Fatal(n)
		}
	case <-time.After(time.Second):
		t.Fatal("no notification")
	}
	select {
	case <-c.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("agent should exit")
	}
	if err := c.Call("x", nil, nil); err == nil {
		t.Fatal("call after exit must fail")
	}
}
