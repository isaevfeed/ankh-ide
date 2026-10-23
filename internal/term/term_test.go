package term

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type sink struct {
	mu   sync.Mutex
	out  strings.Builder
	done chan int
}

func (s *sink) Output(id string, b []byte)                  { s.mu.Lock(); s.out.Write(b); s.mu.Unlock() }
func (s *sink) Exited(id string, code int, _ time.Duration) { s.done <- code }
func (s *sink) text() string                                { s.mu.Lock(); defer s.mu.Unlock(); return s.out.String() }

func TestPasteReachesProgram(t *testing.T) {
	sh, err := os.Stat("/bin/sh")
	if err != nil || sh.IsDir() {
		t.Skip("no /bin/sh")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "got")
	s := &sink{done: make(chan int, 1)}
	m := NewManager(s)
	// The fake "agent" reads one line and stores it.
	err = m.Start(Spec{ID: "a", Dir: dir, Argv: []string{"/bin/sh", "-c", "IFS= read -r l; printf '%s' \"$l\" > " + out + "; echo done"}, Env: os.Environ()})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Paste("a", "hello world", true); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-s.done:
		if code != 0 {
			t.Fatalf("exit code %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("program did not exit")
	}
	b, _ := os.ReadFile(out)
	// sh's read keeps the bracketed-paste markers; the agent TUI strips them.
	if !strings.Contains(string(b), "hello world") {
		t.Fatalf("program got %q", b)
	}
	if !strings.Contains(s.text(), "done") {
		t.Fatalf("output = %q", s.text())
	}
	if m.Running("a") {
		t.Fatal("terminal still registered after exit")
	}
}
