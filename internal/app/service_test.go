package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"ankh-ide/internal/state"
)

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// shortHome is a temp HOME short enough for the unix socket inside it:
// t.TempDir() under macOS $TMPDIR overflows the 104-byte sun_path limit.
func shortHome(t *testing.T) string {
	home, err := os.MkdirTemp("", "ankh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	return home
}

func TestSessionsAndReview(t *testing.T) {
	home := shortHome(t)
	t.Setenv("HOME", home)
	repo := filepath.Join(home, "proj")
	_ = os.MkdirAll(repo, 0o755)
	run(t, repo, "init", "-q", "-b", "develop")
	_ = os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o644)
	run(t, repo, "add", ".")
	run(t, repo, "commit", "-q", "-m", "init")

	var events []string
	host := &Host{Emit: func(name string, _ ...any) bool { events = append(events, name); return false }}
	s, err := New(host, filepath.Join(home, "cfg"), "/bin/true")
	if err != nil {
		t.Fatal(err)
	}
	defer Shutdown(s)

	p, err := s.AddProjectPath(repo)
	if err != nil || p.Name != "proj" || len(p.Sessions) != 1 || p.Sessions[0].Branch != "develop" {
		t.Fatalf("project = %+v, %v", p, err)
	}
	ss, err := s.CreateSession(p.ID, "feature/x", "develop", "claude", false)
	if err != nil {
		t.Fatal(err)
	}
	if ss.Path != filepath.Join(home, ".ankh", "worktrees", "proj", "feature-x") {
		t.Fatalf("worktree path = %s", ss.Path)
	}
	_ = os.WriteFile(filepath.Join(ss.Path, "a.go"), []byte("package a\n\nvar X = 1\n"), 0o644)
	d, err := s.GetDiff(ss.ID, "wip")
	if err != nil || len(d.Files) != 1 || d.Files[0].Add != 2 {
		t.Fatalf("diff = %+v %v", d, err)
	}
	c, err := s.AddComment(ss.ID, state.Comment{Path: "a.go", Side: "new", Line: 3, Col: 5, Text: "переименуй X"})
	if err != nil || c.Loc != "a.go:3:5" {
		t.Fatalf("comment = %+v %v", c, err)
	}
	if err := s.CloseSession(ss.ID, false, false); err == nil || err.Error() != "has_changes" {
		t.Fatalf("close without force = %v", err)
	}
	if err := s.DiscardDrafts(ss.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := s.CloseSession(ss.ID, true, true); err != nil {
		t.Fatal(err)
	}
	st := s.GetState()
	if len(st.Data.Projects[0].Sessions) != 1 {
		t.Fatalf("sessions after close = %d", len(st.Data.Projects[0].Sessions))
	}
	if _, err := os.Stat(ss.Path); !os.IsNotExist(err) {
		t.Fatal("worktree folder still exists")
	}

	// Reload from disk.
	s2, err := New(host, filepath.Join(home, "cfg2"), "/bin/true")
	if err != nil {
		t.Fatal(err)
	}
	Shutdown(s2)
	if _, err := state.Open(filepath.Join(home, "cfg", "state.json")); err != nil {
		t.Fatal(err)
	}
}
