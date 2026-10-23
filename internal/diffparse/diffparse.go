// Package diffparse parses unified diffs produced by `git diff` / `git show`.
package diffparse

import (
	"strconv"
	"strings"
)

// Line is one row of a hunk. O and N are 1-based line numbers in the old and
// new file; 0 means the line does not exist on that side.
type Line struct {
	T    string `json:"t"` // " ", "+", "-"
	O    int    `json:"o"`
	N    int    `json:"n"`
	Text string `json:"text"`
}

type Hunk struct {
	Header string `json:"header"`
	Lines  []Line `json:"lines"`
}

type FileDiff struct {
	Path    string `json:"path"`
	OldPath string `json:"oldPath,omitempty"`
	Status  string `json:"status"` // M, A, D, R
	Add     int    `json:"add"`
	Del     int    `json:"del"`
	Binary  bool   `json:"binary"`
	Hunks   []Hunk `json:"hunks"`
}

// Parse splits a unified diff into files and hunks.
func Parse(text string) []FileDiff {
	var (
		files []FileDiff
		cur   *FileDiff
		h     *Hunk
		o, n  int
	)
	flush := func() {
		if cur == nil {
			return
		}
		if h != nil {
			cur.Hunks = append(cur.Hunks, *h)
			h = nil
		}
		if cur.Status == "" {
			cur.Status = "M"
		}
		files = append(files, *cur)
		cur = nil
	}
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		// A trailing empty element comes from the final newline.
		if i == len(lines)-1 && l == "" {
			break
		}
		switch {
		case strings.HasPrefix(l, "diff --git "):
			flush()
			cur = &FileDiff{}
			// Best effort until ---/+++ lines arrive (they are authoritative).
			if a, b, ok := splitGitHeader(strings.TrimPrefix(l, "diff --git ")); ok {
				cur.OldPath, cur.Path = a, b
			}
		case cur == nil:
			continue
		case h == nil && strings.HasPrefix(l, "new file mode"):
			cur.Status = "A"
		case h == nil && strings.HasPrefix(l, "deleted file mode"):
			cur.Status = "D"
		case h == nil && strings.HasPrefix(l, "rename from "):
			cur.Status = "R"
			cur.OldPath = strings.TrimPrefix(l, "rename from ")
		case h == nil && strings.HasPrefix(l, "rename to "):
			cur.Status = "R"
			cur.Path = strings.TrimPrefix(l, "rename to ")
		case h == nil && strings.HasPrefix(l, "Binary files ") && strings.HasSuffix(l, " differ"):
			cur.Binary = true
		case h == nil && strings.HasPrefix(l, "--- "):
			p := strings.TrimPrefix(l, "--- ")
			if p != "/dev/null" {
				cur.OldPath = unquote(strings.TrimPrefix(unquote(p), "a/"))
			}
		case h == nil && strings.HasPrefix(l, "+++ "):
			p := strings.TrimPrefix(l, "+++ ")
			if p == "/dev/null" {
				cur.Path = cur.OldPath
				if cur.Status == "" {
					cur.Status = "D"
				}
			} else {
				cur.Path = unquote(strings.TrimPrefix(unquote(p), "b/"))
			}
		case strings.HasPrefix(l, "@@ "):
			if h != nil {
				cur.Hunks = append(cur.Hunks, *h)
			}
			h = &Hunk{Header: l}
			o, n = parseRange(l)
		case h != nil && len(l) > 0 && l[0] == '+':
			h.Lines = append(h.Lines, Line{T: "+", N: n, Text: l[1:]})
			n++
			cur.Add++
		case h != nil && len(l) > 0 && l[0] == '-':
			h.Lines = append(h.Lines, Line{T: "-", O: o, Text: l[1:]})
			o++
			cur.Del++
		case h != nil && len(l) > 0 && l[0] == '\\':
			// "\ No newline at end of file"
		case h != nil && (len(l) == 0 || l[0] == ' '):
			t := ""
			if len(l) > 0 {
				t = l[1:]
			}
			h.Lines = append(h.Lines, Line{T: " ", O: o, N: n, Text: t})
			o++
			n++
		}
	}
	flush()
	for i := range files {
		if files[i].Path == "" {
			files[i].Path = files[i].OldPath
		}
		if files[i].OldPath == files[i].Path {
			files[i].OldPath = ""
		}
	}
	return files
}

// parseRange reads "@@ -a,b +c,d @@" and returns the start lines a and c.
func parseRange(header string) (int, int) {
	parts := strings.Fields(header)
	if len(parts) < 3 {
		return 1, 1
	}
	return startOf(parts[1]), startOf(parts[2])
}

func startOf(s string) int {
	s = strings.TrimLeft(s, "-+")
	if i := strings.IndexByte(s, ','); i >= 0 {
		s = s[:i]
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 1
	}
	if v == 0 {
		// "-0,0": the side is empty; first real line would be 1.
		return 1
	}
	return v
}

func splitGitHeader(s string) (string, string, bool) {
	// "a/path b/path" — only reliable when paths have no spaces.
	if !strings.HasPrefix(s, "a/") {
		return "", "", false
	}
	i := strings.Index(s, " b/")
	if i < 0 {
		return "", "", false
	}
	return s[2:i], s[i+3:], true
}

func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
	}
	return s
}

// AddedFile builds a FileDiff for an untracked file: every line is an addition.
func AddedFile(path, content string) FileDiff {
	f := FileDiff{Path: path, Status: "A"}
	if strings.IndexByte(content, 0) >= 0 {
		f.Binary = true
		return f
	}
	content = strings.TrimSuffix(content, "\n")
	if content == "" {
		return f
	}
	ls := strings.Split(content, "\n")
	h := Hunk{Header: "@@ -0,0 +1," + strconv.Itoa(len(ls)) + " @@"}
	for i, l := range ls {
		h.Lines = append(h.Lines, Line{T: "+", N: i + 1, Text: l})
	}
	f.Add = len(ls)
	f.Hunks = []Hunk{h}
	return f
}
