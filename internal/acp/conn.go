// Package acp is a minimal client for the Agent Client Protocol: JSON-RPC 2.0
// over the agent's stdin/stdout, one message per line.
package acp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"syscall"
)

// ProtocolVersion is the ACP version Ankh speaks.
const ProtocolVersion = 1

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	if len(e.Data) > 0 && string(e.Data) != "null" {
		return fmt.Sprintf("%s (%s)", e.Message, e.Data)
	}
	return e.Message
}

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// Handler answers what the agent sends: notifications and requests.
// HandleRequest runs in its own goroutine and may block (permission prompts).
type Handler interface {
	HandleNotification(method string, params json.RawMessage)
	HandleRequest(method string, params json.RawMessage) (any, *RPCError)
}

type Conn struct {
	cmd     *exec.Cmd
	w       io.WriteCloser
	wmu     sync.Mutex
	mu      sync.Mutex
	nextID  int64
	pending map[string]chan message
	h       Handler
	done    chan struct{}
	err     error
	stderr  *tail
}

// Start runs argv in dir and speaks JSON-RPC over its stdio.
func Start(argv []string, dir string, env []string, h Handler) (*Conn, error) {
	if len(argv) == 0 {
		return nil, errors.New("acp: empty command")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	c := &Conn{cmd: cmd, w: stdin, pending: map[string]chan message{}, h: h, done: make(chan struct{}), stderr: &tail{max: 8 << 10}}
	cmd.Stderr = c.stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go c.read(stdout)
	return c, nil
}

func (c *Conn) read(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<16), 64<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue // shell noise from dotfiles
		}
		var m message
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		c.dispatch(m)
	}
	err := c.cmd.Wait()
	c.mu.Lock()
	if err == nil {
		err = errors.New("агент завершился")
	} else {
		err = fmt.Errorf("агент завершился: %v", err)
	}
	if s := c.stderr.String(); s != "" {
		err = fmt.Errorf("%w\n%s", err, s)
	}
	c.err = err
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.mu.Unlock()
	close(c.done)
}

func (c *Conn) dispatch(m message) {
	switch {
	case m.Method != "" && len(m.ID) > 0:
		go func() {
			res, rerr := c.h.HandleRequest(m.Method, m.Params)
			out := message{JSONRPC: "2.0", ID: m.ID}
			if rerr != nil {
				out.Error = rerr
			} else {
				b, err := json.Marshal(res)
				if err != nil {
					out.Error = &RPCError{Code: -32603, Message: err.Error()}
				} else {
					out.Result = b
				}
			}
			_ = c.write(out)
		}()
	case m.Method != "":
		c.h.HandleNotification(m.Method, m.Params)
	case len(m.ID) > 0:
		c.mu.Lock()
		ch := c.pending[string(m.ID)]
		delete(c.pending, string(m.ID))
		c.mu.Unlock()
		if ch != nil {
			ch <- m
		}
	}
}

func (c *Conn) write(m message) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err = c.w.Write(append(b, '\n'))
	return err
}

// Call sends a request and waits for its result (decoded into out, if not nil).
func (c *Conn) Call(method string, params, out any) error {
	p, err := json.Marshal(params)
	if err != nil {
		return err
	}
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return err
	}
	c.nextID++
	id := json.RawMessage(fmt.Sprint(c.nextID))
	ch := make(chan message, 1)
	c.pending[string(id)] = ch
	c.mu.Unlock()
	if err := c.write(message{JSONRPC: "2.0", ID: id, Method: method, Params: p}); err != nil {
		return err
	}
	m, ok := <-ch
	if !ok {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.err
	}
	if m.Error != nil {
		return m.Error
	}
	if out != nil && len(m.Result) > 0 {
		return json.Unmarshal(m.Result, out)
	}
	return nil
}

// Notify sends a notification.
func (c *Conn) Notify(method string, params any) error {
	p, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.write(message{JSONRPC: "2.0", Method: method, Params: p})
}

// Done is closed when the agent exits; Err tells why.
func (c *Conn) Done() <-chan struct{} { return c.done }

func (c *Conn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Close stops the agent and its children.
func (c *Conn) Close() {
	_ = c.w.Close()
	if c.cmd.Process != nil {
		_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGTERM)
	}
}

// tail keeps the last bytes written to it (the agent's stderr, for errors).
type tail struct {
	mu  sync.Mutex
	max int
	b   []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > t.max {
		t.b = t.b[len(t.b)-t.max:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(bytes.TrimSpace(t.b))
}
