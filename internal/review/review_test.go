package review

import (
	"strings"
	"testing"

	"ankh-ide/internal/state"
)

func TestBuildPrompt(t *testing.T) {
	cs := []state.Comment{
		{Path: "b.go", Line: 10, Loc: "b.go:10:2", Text: "второе"},
		{Path: "a.go", Line: 142, Loc: "a.go:142-150", Text: "добавь обработку\nошибки"},
		{Path: "a.go", Line: 7, Loc: "a.go:7", Text: "старое", Deleted: true},
		{Path: "a.go", Line: 1, Loc: "a.go:1", Text: "уже ушло", Sent: true},
		{Path: "a.go", Line: 2, Loc: "a.go:2", Text: "   "},
	}
	got := BuildPrompt(cs)
	want := "Исправь замечания по ревью (3):\n" +
		"a.go:7 - старое (удалённый код)\n" +
		"a.go:142-150 - добавь обработку\n  ошибки\n" +
		"b.go:10:2 - второе"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if BuildPrompt(nil) != "" {
		t.Fatal("empty input must give empty prompt")
	}
}

func TestLoc(t *testing.T) {
	cases := map[string]string{
		Loc("x.go", 5, 0, 0): "x.go:5",
		Loc("x.go", 5, 0, 3): "x.go:5:3",
		Loc("x.go", 9, 5, 3): "x.go:5-9",
		Loc("x.go", 5, 5, 3): "x.go:5:3",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
}

func TestRelocate(t *testing.T) {
	file := func(s string) []string { return strings.Split(s, "\n") }
	c := state.Comment{Path: "a.go", Side: "new", Line: 3, Col: 2, Anchor: "\treturn x", Before: "\tx := 1", Loc: "a.go:3:2"}

	// unchanged
	cc := c
	if Relocate(&cc, file("func f() {\n\tx := 1\n\treturn x\n}")) || cc.Line != 3 {
		t.Fatalf("unchanged: %+v", cc)
	}
	// two lines inserted above
	cc = c
	if !Relocate(&cc, file("// a\n// b\nfunc f() {\n\tx := 1\n\treturn x\n}")) {
		t.Fatal("must move")
	}
	if cc.Line != 5 || cc.Loc != "a.go:5:2" || cc.MovedFrom != 3 || cc.Outdated {
		t.Fatalf("moved: %+v", cc)
	}
	// moving back to the original line clears MovedFrom
	Relocate(&cc, file("func f() {\n\tx := 1\n\treturn x\n}"))
	if cc.Line != 3 || cc.MovedFrom != 0 {
		t.Fatalf("back: %+v", cc)
	}
	// line gone
	cc = c
	if !Relocate(&cc, file("func f() {\n\tx := 1\n\treturn y\n}")) || !cc.Outdated || cc.Line != 3 {
		t.Fatalf("outdated: %+v", cc)
	}
	// file deleted
	cc = c
	Relocate(&cc, nil)
	if !cc.Outdated {
		t.Fatal("deleted file must be outdated")
	}
	// identical lines: the one with matching context wins over the nearer one
	r := state.Comment{Path: "a.go", Side: "new", Line: 3, LineEnd: 4, Anchor: "}", Before: "\tb()", Loc: "a.go:3-4"}
	Relocate(&r, file("x\n\ta()\n}\ny\n\tb()\n}"))
	if r.Line != 5 || r.LineEnd != 6 || r.Loc != "a.go:5-6" {
		t.Fatalf("range: %+v", r)
	}
	// old-side comments stay
	o := state.Comment{Path: "a.go", Side: "old", Line: 9, Anchor: "gone"}
	if Relocate(&o, file("a")) {
		t.Fatal("old side must not move")
	}
}
