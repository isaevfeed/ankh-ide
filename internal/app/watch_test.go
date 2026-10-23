package app

import "testing"

func TestClassify(t *testing.T) {
	dirs, work, meta := classify("/r", "/r/.git", []string{"/r/.git/objects/ab/cd", "/r/.git/index.lock"})
	if work || meta || len(dirs) != 0 {
		t.Fatalf("objects and locks must be ignored: %v %v %v", dirs, work, meta)
	}
	_, work, meta = classify("/r", "/r/.git", []string{"/r/.git/index", "/r/.git/refs/heads/main"})
	if work || !meta {
		t.Fatal("index change is git meta")
	}
	dirs, work, _ = classify("/r", "/r/.git", []string{"/r/a.go", "/r/internal/x/b.go", "/r/internal/x/c.go"})
	if !work || len(dirs) != 2 || dirs[0] != "" || dirs[1] != "internal/x" {
		t.Fatalf("dirs = %v", dirs)
	}
	// worktree: git dir elsewhere
	_, _, meta = classify("/w", "/r/.git/worktrees/w", []string{"/r/.git/worktrees/w/HEAD"})
	if !meta {
		t.Fatal("worktree HEAD is git meta")
	}
	_, work, _ = classify("/w", "/r/.git/worktrees/w", []string{"/elsewhere/x"})
	if work {
		t.Fatal("outside paths ignored")
	}
}
