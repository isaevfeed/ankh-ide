package fstree

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAndList(t *testing.T) {
	root := t.TempDir()
	if _, err := Resolve(root, "../etc/passwd"); err != ErrOutside {
		t.Fatalf("escape not refused: %v", err)
	}
	if _, err := Resolve(root, "a/../b"); err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Join(root, "src"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "b.txt"), []byte("hi"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "A.txt"), []byte{0, 1, 2}, 0o644)
	_ = os.WriteFile(filepath.Join(root, "src", "x.go"), []byte("package x"), 0o644)
	es, err := List(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 3 || es[0].Name != "src" || !es[0].Dir || es[1].Name != "A.txt" {
		t.Fatalf("entries = %+v", es)
	}
	sub, _ := List(root, "src")
	if sub[0].Path != "src/x.go" {
		t.Fatalf("sub = %+v", sub)
	}
	f, _ := Read(root, "A.txt")
	if !f.Binary {
		t.Fatal("binary not detected")
	}
	f, _ = Read(root, "src/x.go")
	if f.Content != "package x" {
		t.Fatalf("content = %q", f.Content)
	}
}
