// Package term runs processes in pseudo-terminals and streams their output
// in small batches.
package term

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// FlushEvery is how often buffered output is handed to the sink.
const FlushEvery = 16 * time.Millisecond

const maxBatch = 256 * 1024

// Sink receives terminal output and exit notifications.
type Sink interface {
	Output(id string, data []byte)
	Exited(id string, code int, ranFor time.Duration)
}

type Spec struct {
	ID   string
	Dir  string
	Argv []string
	Env  []string
	Cols uint16
	Rows uint16
}

type Term struct {
	ID      string
	Spec    Spec
	cmd     *exec.Cmd
	pty     *os.File
	started time.Time

	mu      sync.Mutex
	buf     []byte
	lastOut time.Time
	closed  bool
}

type Manager struct {
	sink  Sink
	mu    sync.Mutex
	terms map[string]*Term
}

func NewManager(sink Sink) *Manager {
	return &Manager{sink: sink, terms: map[string]*Term{}}
}

var ErrNotFound = errors.New("терминал не найден")

func (m *Manager) Get(id string) *Term {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.terms[id]
}

func (m *Manager) Running(id string) bool { return m.Get(id) != nil }

// Start launches spec.Argv in a new PTY. Starting an id that is already
// running is a no-op.
func (m *Manager) Start(spec Spec) error {
	m.mu.Lock()
	if _, ok := m.terms[spec.ID]; ok {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()
	if len(spec.Argv) == 0 {
		return errors.New("пустая команда")
	}
	if spec.Cols == 0 {
		spec.Cols = 120
	}
	if spec.Rows == 0 {
		spec.Rows = 32
	}
	cmd := exec.Command(spec.Argv[0], spec.Argv[1:]...)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: spec.Cols, Rows: spec.Rows})
	if err != nil {
		return err
	}
	t := &Term{ID: spec.ID, Spec: spec, cmd: cmd, pty: f, started: time.Now()}
	m.mu.Lock()
	m.terms[spec.ID] = t
	m.mu.Unlock()
	go m.read(t)
	go m.flushLoop(t)
	go m.wait(t)
	return nil
}

func (m *Manager) read(t *Term) {
	b := make([]byte, 32*1024)
	for {
		n, err := t.pty.Read(b)
		if n > 0 {
			t.mu.Lock()
			t.buf = append(t.buf, b[:n]...)
			t.lastOut = time.Now()
			full := len(t.buf) >= maxBatch
			t.mu.Unlock()
			if full {
				m.flush(t)
			}
		}
		if err != nil {
			return
		}
	}
}

func (m *Manager) flushLoop(t *Term) {
	tk := time.NewTicker(FlushEvery)
	defer tk.Stop()
	for range tk.C {
		m.flush(t)
		t.mu.Lock()
		closed := t.closed
		t.mu.Unlock()
		if closed {
			return
		}
	}
}

func (m *Manager) flush(t *Term) {
	t.mu.Lock()
	if len(t.buf) == 0 {
		t.mu.Unlock()
		return
	}
	data := t.buf
	t.buf = nil
	t.mu.Unlock()
	m.sink.Output(t.ID, data)
}

func (m *Manager) wait(t *Term) {
	err := t.cmd.Wait()
	code := 0
	if err != nil {
		code = -1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		}
	}
	// Let the reader drain what is left.
	time.Sleep(50 * time.Millisecond)
	m.flush(t)
	t.mu.Lock()
	t.closed = true
	t.mu.Unlock()
	_ = t.pty.Close()
	m.mu.Lock()
	if m.terms[t.ID] == t {
		delete(m.terms, t.ID)
	}
	m.mu.Unlock()
	m.sink.Exited(t.ID, code, time.Since(t.started))
}

func (m *Manager) Write(id string, data []byte) error {
	t := m.Get(id)
	if t == nil {
		return ErrNotFound
	}
	_, err := t.pty.Write(data)
	return err
}

func (m *Manager) Resize(id string, cols, rows uint16) error {
	t := m.Get(id)
	if t == nil {
		return ErrNotFound
	}
	if cols == 0 || rows == 0 {
		return nil
	}
	return pty.Setsize(t.pty, &pty.Winsize{Cols: cols, Rows: rows})
}

// Paste writes text as a bracketed paste, so multi-line text is not
// submitted line by line. With submit, Enter follows the paste.
func (m *Manager) Paste(id, text string, submit bool) error {
	t := m.Get(id)
	if t == nil {
		return ErrNotFound
	}
	if _, err := io.WriteString(t.pty, "\x1b[200~"+text+"\x1b[201~"); err != nil {
		return err
	}
	if submit {
		// Give the TUI a moment to take the paste before Enter arrives.
		time.Sleep(60 * time.Millisecond)
		_, err := io.WriteString(t.pty, "\r")
		return err
	}
	return nil
}

// StartedAt reports when the terminal's process was started.
func (m *Manager) StartedAt(id string) time.Time {
	t := m.Get(id)
	if t == nil {
		return time.Time{}
	}
	return t.started
}

// LastOutput reports when the terminal last printed something.
func (m *Manager) LastOutput(id string) time.Time {
	t := m.Get(id)
	if t == nil {
		return time.Time{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastOut
}

// Kill stops one terminal (SIGHUP first, like closing a terminal window).
func (m *Manager) Kill(id string) {
	t := m.Get(id)
	if t == nil || t.cmd.Process == nil {
		return
	}
	_ = t.cmd.Process.Signal(syscall.SIGHUP)
	go func() {
		time.Sleep(2 * time.Second)
		if m.Get(id) == t {
			_ = t.cmd.Process.Kill()
		}
	}()
}

// IDs returns running terminal ids.
func (m *Manager) IDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.terms))
	for id := range m.terms {
		ids = append(ids, id)
	}
	return ids
}

// KillAll stops every terminal; used on app shutdown.
func (m *Manager) KillAll() {
	for _, id := range m.IDs() {
		m.Kill(id)
	}
}
