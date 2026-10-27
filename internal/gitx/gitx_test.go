package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, path, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiffModesAndWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "develop")
	write(t, filepath.Join(dir, "a.go"), "package a\n\nfunc A() {}\n")
	write(t, filepath.Join(dir, ".gitignore"), "tmp/\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "init")
	git(t, dir, "checkout", "-q", "-b", "feature/x")
	write(t, filepath.Join(dir, "a.go"), "package a\n\nfunc A() { println(1) }\n")
	git(t, dir, "commit", "-q", "-am", "change")
	write(t, filepath.Join(dir, "a.go"), "package a\n\nfunc A() { println(2) }\n")
	write(t, filepath.Join(dir, "new.txt"), "hello\nworld\n")
	write(t, filepath.Join(dir, "tmp", "junk"), "x")

	if b := Branch(dir); b != "feature/x" {
		t.Fatalf("branch = %q", b)
	}
	st, err := Status(dir)
	if err != nil || st["a.go"] != "M" || st["new.txt"] != "A" || len(st) != 2 {
		t.Fatalf("status = %v, %v", st, err)
	}

	wip, err := Diff(dir, "wip", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(wip.Files) != 2 || wip.Files[0].Path != "a.go" || wip.Files[1].Path != "new.txt" || wip.Files[1].Add != 2 {
		t.Fatalf("wip = %+v", wip.Files)
	}

	br, err := Diff(dir, "branch", "")
	if err != nil || br.Base != "develop" {
		t.Fatalf("branch diff: %v base=%q", err, br.Base)
	}
	if br.Files[0].Hunks[0].Lines[len(br.Files[0].Hunks[0].Lines)-1].Text != "func A() { println(2) }" {
		t.Fatalf("branch diff should include working tree: %+v", br.Files[0].Hunks)
	}

	last, err := Diff(dir, "last", "")
	if err != nil || len(last.Files) != 1 {
		t.Fatalf("last = %+v %v", last, err)
	}

	ign := CheckIgnored(dir, []string{"tmp", "a.go"})
	if !ign["tmp"] || ign["a.go"] {
		t.Fatalf("ignored = %v", ign)
	}

	wt := filepath.Join(t.TempDir(), "wt", "feature-y")
	if err := WorktreeAdd(dir, wt, "feature/y", "develop"); err != nil {
		t.Fatal(err)
	}
	if Branch(wt) != "feature/y" {
		t.Fatalf("worktree branch = %q", Branch(wt))
	}
	if HasChanges(wt) {
		t.Fatal("fresh worktree must be clean")
	}
	if err := WorktreeRemove(dir, wt, false); err != nil {
		t.Fatal(err)
	}
	if err := DeleteBranch(dir, "feature/y"); err != nil {
		t.Fatal(err)
	}
	if bs := Branches(dir); bs[0] != "feature/x" {
		t.Fatalf("branches = %v", bs)
	}
}

func TestStageUnstageDiscardCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	for _, kv := range [][2]string{{"GIT_AUTHOR_NAME", "t"}, {"GIT_AUTHOR_EMAIL", "t@t"}, {"GIT_COMMITTER_NAME", "t"}, {"GIT_COMMITTER_EMAIL", "t@t"}} {
		t.Setenv(kv[0], kv[1])
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	write(t, filepath.Join(dir, "a.txt"), "one\n")
	write(t, filepath.Join(dir, "b.txt"), "bee\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "init")

	write(t, filepath.Join(dir, "a.txt"), "two\n")
	write(t, filepath.Join(dir, "new.txt"), "n\n")
	git(t, dir, "mv", "b.txt", "c.txt")

	files, err := Files(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"a.txt": true, "c.txt": true, "new.txt": true}
	if len(files) != 3 {
		t.Fatalf("files = %v", files)
	}
	for _, f := range files {
		if !want[f] {
			t.Fatalf("unexpected file %s in %v", f, files)
		}
	}

	if err := Stage(dir, []string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := StatusRaw(dir)
	idx := Index(raw)
	if idx["a.txt"] != "staged" || idx["new.txt"] != "" || idx["c.txt"] != "staged" {
		t.Fatalf("index = %v", idx)
	}
	write(t, filepath.Join(dir, "a.txt"), "three\n")
	raw, _ = StatusRaw(dir)
	if Index(raw)["a.txt"] != "partial" {
		t.Fatalf("want partial, got %v", Index(raw))
	}
	if err := Unstage(dir, []string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	raw, _ = StatusRaw(dir)
	if Index(raw)["a.txt"] != "" {
		t.Fatalf("unstage: %v", Index(raw))
	}

	// discard: modified, untracked and renamed
	if err := Discard(dir, []string{"a.txt", "new.txt", "c.txt", "b.txt"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "one\n" {
		t.Fatalf("a.txt = %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); !os.IsNotExist(err) {
		t.Fatal("new.txt must be gone")
	}
	if HasChanges(dir) {
		raw, _ := StatusRaw(dir)
		t.Fatalf("still changed: %q", raw)
	}
	if err := Discard(dir, []string{"../x"}); err == nil {
		t.Fatal("path outside must be refused")
	}

	// commit all
	write(t, filepath.Join(dir, "a.txt"), "four\n")
	if _, err := Commit(dir, "  ", true); err == nil {
		t.Fatal("empty message must fail")
	}
	h, err := Commit(dir, "change a", true)
	if err != nil || h == "" {
		t.Fatalf("commit: %v %q", err, h)
	}
	if HasChanges(dir) {
		t.Fatal("tree must be clean after commit")
	}
	if _, err := Commit(dir, "again", false); err == nil {
		t.Fatal("empty index must fail")
	}
	if GitDir(dir) == "" {
		t.Fatal("git dir")
	}
}

func TestStageRemovalTwiceAndRename(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	for _, kv := range [][2]string{{"GIT_AUTHOR_NAME", "t"}, {"GIT_AUTHOR_EMAIL", "t@t"}, {"GIT_COMMITTER_NAME", "t"}, {"GIT_COMMITTER_EMAIL", "t@t"}} {
		t.Setenv(kv[0], kv[1])
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	body := ""
	for i := 0; i < 30; i++ {
		body += "line\n"
	}
	write(t, filepath.Join(dir, "old_test.go"), body)
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "init")
	_ = os.Remove(filepath.Join(dir, "old_test.go"))
	write(t, filepath.Join(dir, "new.go"), body+"more\n")
	git(t, dir, "add", "new.go")

	// staging the removal works, and works again when it is staged already
	for i := 0; i < 2; i++ {
		if err := Stage(dir, []string{"old_test.go"}); err != nil {
			t.Fatalf("stage %d: %v", i, err)
		}
	}
	raw, _ := StatusRaw(dir)
	idx := Index(raw)
	if idx["old_test.go"] != "staged" || idx["new.go"] != "staged" {
		t.Fatalf("index after rename = %v (raw %q)", idx, raw)
	}
	// unstage tolerates paths git does not know
	write(t, filepath.Join(dir, "untracked.txt"), "x")
	if err := Unstage(dir, []string{"old_test.go", "untracked.txt"}); err != nil {
		t.Fatal(err)
	}
	raw, _ = StatusRaw(dir)
	if Index(raw)["old_test.go"] != "" {
		t.Fatalf("old_test.go must be unstaged: %q", raw)
	}
	// diff lists every path once
	d, err := Diff(dir, "wip", "")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, f := range d.Files {
		if seen[f.Path] {
			t.Fatalf("duplicate %s in %+v", f.Path, d.Files)
		}
		seen[f.Path] = true
	}
}

// Commit all takes every change even when something is staged already
// (an agent often runs `git add` on part of its work).
func TestCommitAllWithPartlyStaged(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	for _, kv := range [][2]string{{"GIT_AUTHOR_NAME", "t"}, {"GIT_AUTHOR_EMAIL", "t@t"}, {"GIT_COMMITTER_NAME", "t"}, {"GIT_COMMITTER_EMAIL", "t@t"}} {
		t.Setenv(kv[0], kv[1])
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	write(t, filepath.Join(dir, "a.txt"), "one\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "init")
	write(t, filepath.Join(dir, "a.txt"), "two\n")
	write(t, filepath.Join(dir, "b.txt"), "new\n")
	write(t, filepath.Join(dir, "sub", "c.txt"), "deep\n")
	git(t, dir, "add", "a.txt")
	if _, err := Commit(dir, "all", true); err != nil {
		t.Fatal(err)
	}
	if HasChanges(dir) {
		st, _ := Status(dir)
		t.Fatalf("left uncommitted: %v", st)
	}
}
