// Package review turns draft review comments into one prompt for the agent.
package review

import (
	"fmt"
	"sort"
	"strings"

	"ankh-ide/internal/state"
)

// BuildPrompt formats unsent comments as:
//
//	Исправь замечания по ревью (2):
//	path/to/file.go:137:10 - переименуй переменную tmp в total
//	path/to/file.go:142-150 - добавь обработку ошибки
func BuildPrompt(comments []state.Comment) string {
	var cs []state.Comment
	for _, c := range comments {
		if !c.Sent && strings.TrimSpace(c.Text) != "" {
			cs = append(cs, c)
		}
	}
	if len(cs) == 0 {
		return ""
	}
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].Path != cs[j].Path {
			return cs[i].Path < cs[j].Path
		}
		return cs[i].Line < cs[j].Line
	})
	var b strings.Builder
	fmt.Fprintf(&b, "Исправь замечания по ревью (%d):", len(cs))
	for _, c := range cs {
		text := strings.TrimSpace(c.Text)
		text = strings.ReplaceAll(text, "\r\n", "\n")
		text = strings.ReplaceAll(text, "\n", "\n  ")
		b.WriteString("\n")
		b.WriteString(c.Loc)
		b.WriteString(" - ")
		b.WriteString(text)
		if c.Deleted {
			b.WriteString(" (удалённый код)")
		}
	}
	return b.String()
}

// Loc builds the location string for a comment.
func Loc(path string, line, lineEnd, col int) string {
	switch {
	case lineEnd > 0 && lineEnd != line:
		lo, hi := line, lineEnd
		if lo > hi {
			lo, hi = hi, lo
		}
		return fmt.Sprintf("%s:%d-%d", path, lo, hi)
	case col > 0:
		return fmt.Sprintf("%s:%d:%d", path, line, col)
	default:
		return fmt.Sprintf("%s:%d", path, line)
	}
}

// Relocate keeps a new-side comment on its line when the file changes:
// it looks for the anchor text nearest to the old position (preferring a
// match whose previous line also matches) and shifts Line/LineEnd. When the
// line is gone it marks the comment Outdated. lines is the current file
// split by "\n"; nil means the file no longer exists. It reports whether c
// changed.
func Relocate(c *state.Comment, lines []string) bool {
	if c.Side == "old" || c.Anchor == "" {
		return false
	}
	before := *c
	end := c.Line
	if c.LineEnd > 0 {
		end = c.LineEnd
	}
	at := func(i int) string {
		if i < 0 || i >= len(lines) {
			return "\x00"
		}
		return strings.TrimRight(lines[i], "\r")
	}
	idx := end - 1
	ctxAt := func(j int) bool { return c.Before == "" || at(j-1) == c.Before }
	exact := at(idx) == c.Anchor
	if exact && ctxAt(idx) {
		c.Outdated = false
		return *c != before
	}
	blank := strings.TrimSpace(c.Anchor) == ""
	found, best := -1, 1<<30
	for j := range lines {
		if at(j) != c.Anchor {
			continue
		}
		ctx := ctxAt(j) && c.Before != ""
		if (blank || exact) && !ctx {
			continue // ambiguous: needs the line above to match too
		}
		d := j - idx
		if d < 0 {
			d = -d
		}
		if !ctx {
			d += 1 << 20
		}
		if d < best {
			best, found = d, j
		}
	}
	if found < 0 {
		c.Outdated = !exact
		return *c != before
	}
	delta := found - idx
	if c.MovedFrom == 0 {
		c.MovedFrom = end
	}
	c.Line += delta
	if c.LineEnd > 0 {
		c.LineEnd += delta
	}
	if c.MovedFrom == c.LineEnd || (c.LineEnd == 0 && c.MovedFrom == c.Line) {
		c.MovedFrom = 0
	}
	c.Outdated = false
	c.Loc = Loc(c.Path, c.Line, c.LineEnd, c.Col)
	return *c != before
}
