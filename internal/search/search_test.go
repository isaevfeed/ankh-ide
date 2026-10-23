package search

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func setup(t *testing.T, git bool) string {
	dir := t.TempDir()
	files := map[string]string{
		"a.go":            "package a\n\nfunc Handler() {}\nvar handlerName = \"x\"\n",
		"internal/b.go":   "package b // Handler here\n",
		"web/x.ts":        "const handler = 1; const h2 = handler;\n",
		"ignored/skip.go": "Handler\n",
	}
	for p, c := range files {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755)
		_ = os.WriteFile(filepath.Join(dir, p), []byte(c), 0o644)
	}
	_ = os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored/\n"), 0o644)
	if git {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("no git")
		}
		for _, args := range [][]string{{"init", "-q"}, {"add", "a.go"}} {
			c := exec.Command("git", args...)
			c.Dir = dir
			if out, err := c.CombinedOutput(); err != nil {
				t.Fatalf("%v %s", err, out)
			}
		}
	}
	return dir
}

func paths(r *Result) map[string]int {
	m := map[string]int{}
	for _, h := range r.Hits {
		m[h.Path]++
	}
	return m
}

func TestSearch(t *testing.T) {
	for _, git := range []bool{true, false} {
		dir := setup(t, git)
		r, err := Run(dir, "handler", Options{}, git)
		if err != nil {
			t.Fatal(err)
		}
		m := paths(r)
		// untracked files are searched, ignored ones are not
		if m["a.go"] != 2 || m["internal/b.go"] != 1 || m["web/x.ts"] != 1 || m["ignored/skip.go"] != 0 && git {
			t.Fatalf("git=%v hits %v", git, m)
		}
		if r.Files != 3 && git {
			t.Fatalf("files = %d", r.Files)
		}
		for _, h := range r.Hits {
			if h.Path == "web/x.ts" && len(h.Ranges) != 2 {
				t.Fatalf("two ranges expected: %+v", h)
			}
		}
		// case, whole word, regex, include
		r, _ = Run(dir, "Handler", Options{Case: true, Word: true}, git)
		if m := paths(r); m["a.go"] != 1 || m["internal/b.go"] != 1 || m["web/x.ts"] != 0 {
			t.Fatalf("git=%v case+word %v", git, m)
		}
		r, _ = Run(dir, `handler\w+`, Options{Regex: true}, git)
		if m := paths(r); m["a.go"] != 1 || len(m) != 1 {
			t.Fatalf("git=%v regex %v", git, m)
		}
		r, _ = Run(dir, "handler", Options{Include: "*.ts"}, git)
		if m := paths(r); len(m) != 1 || m["web/x.ts"] != 1 {
			t.Fatalf("git=%v include %v", git, m)
		}
		r, _ = Run(dir, "handler", Options{Include: "internal/"}, git)
		if m := paths(r); len(m) != 1 || m["internal/b.go"] != 1 {
			t.Fatalf("git=%v include dir %v", git, m)
		}
		if _, err := Run(dir, "(", Options{Regex: true}, git); err != ErrBadRegex {
			t.Fatalf("bad regex err = %v", err)
		}
		r, err = Run(dir, "nothing-like-this", Options{}, git)
		if err != nil || len(r.Hits) != 0 {
			t.Fatalf("empty: %v %v", r, err)
		}
	}
}

func TestUTF16Ranges(t *testing.T) {
	re, _ := Compile("нашёл", Options{})
	h, ok := hit("x", 1, "// 😀 нашёл", re)
	if !ok || h.Ranges[0] != [2]int{6, 11} {
		t.Fatalf("%+v", h)
	}
}
