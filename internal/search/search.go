// Package search finds text in a session's files: through `git grep` in a
// repository (fast, honours .gitignore), by walking the folder otherwise.
package search

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Options struct {
	Case    bool   `json:"case"`    // match case
	Word    bool   `json:"word"`    // whole words
	Regex   bool   `json:"regex"`   // query is a regular expression
	Include string `json:"include"` // comma-separated globs or folders, e.g. "*.go, internal/"
}

type Hit struct {
	Path   string   `json:"path"`
	Line   int      `json:"line"`
	Text   string   `json:"text"`
	Ranges [][2]int `json:"ranges"` // byte offsets → converted to UTF-16 for JS
}

type Result struct {
	Hits      []Hit `json:"hits"`
	Files     int   `json:"files"`
	Truncated bool  `json:"truncated"`
}

const (
	MaxHits  = 2000
	maxText  = 400
	maxFile  = 2 << 20
	maxWalk  = 100_000
	maxLines = 64 << 10
)

var ErrBadRegex = errors.New("неверное регулярное выражение")

// Compile builds the matcher the UI also mirrors in JS.
func Compile(q string, o Options) (*regexp.Regexp, error) {
	if q == "" {
		return nil, errors.New("пустой запрос")
	}
	pat := q
	if !o.Regex {
		pat = regexp.QuoteMeta(q)
	}
	if o.Word {
		pat = `\b(?:` + pat + `)\b`
	}
	if !o.Case {
		pat = `(?i)` + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil, ErrBadRegex
	}
	return re, nil
}

// includes turns "*.go, internal/" into git pathspecs and walk filters.
func includes(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func included(rel string, inc []string) bool {
	if len(inc) == 0 {
		return true
	}
	for _, p := range inc {
		if strings.HasSuffix(p, "/") && strings.HasPrefix(rel, p) {
			return true
		}
		if ok, _ := filepath.Match(p, filepath.Base(rel)); ok {
			return true
		}
		if ok, _ := filepath.Match(p, rel); ok {
			return true
		}
		if !strings.ContainsAny(p, "*?[") && strings.HasPrefix(rel, strings.TrimSuffix(p, "/")+"/") {
			return true
		}
	}
	return false
}

// utf16 converts a byte offset in s to a UTF-16 offset (JS string index).
func utf16(s string, b int) int {
	n := 0
	for _, r := range s[:b] {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

func hit(path string, line int, text string, re *regexp.Regexp) (Hit, bool) {
	text = strings.TrimRight(text, "\r")
	idx := re.FindAllStringIndex(text, 50)
	if len(idx) == 0 {
		return Hit{}, false
	}
	// Long lines (minified files): keep a window around the first match.
	if len(text) > maxText {
		start := max(0, idx[0][0]-80)
		for start > 0 && !utf8.RuneStart(text[start]) {
			start--
		}
		end := min(len(text), start+maxText)
		for end < len(text) && !utf8.RuneStart(text[end]) {
			end++
		}
		cut := text[start:end]
		var shifted [][]int
		for _, r := range idx {
			if r[0] >= start && r[1] <= end {
				shifted = append(shifted, []int{r[0] - start, r[1] - start})
			}
		}
		text, idx = cut, shifted
		if start > 0 {
			text = "…" + text
			for _, r := range idx {
				r[0] += len("…")
				r[1] += len("…")
			}
		}
	}
	h := Hit{Path: path, Line: line, Text: text}
	for _, r := range idx {
		h.Ranges = append(h.Ranges, [2]int{utf16(text, r[0]), utf16(text, r[1])})
	}
	return h, true
}

// Run searches root. isRepo selects git grep.
func Run(root, q string, o Options, isRepo bool) (*Result, error) {
	re, err := Compile(q, o)
	if err != nil {
		return nil, err
	}
	res := &Result{Hits: []Hit{}}
	if isRepo {
		err = gitGrep(root, q, o, re, res)
	} else {
		err = walk(root, o, re, res)
	}
	files := map[string]bool{}
	for _, h := range res.Hits {
		files[h.Path] = true
	}
	res.Files = len(files)
	return res, err
}

func gitGrep(root, q string, o Options, re *regexp.Regexp, res *Result) error {
	args := []string{"-c", "core.quotepath=off", "grep", "-n", "-I", "-z", "--no-color", "--untracked", "--full-name"}
	if !o.Case {
		args = append(args, "-i")
	}
	if o.Word {
		args = append(args, "-w")
	}
	if o.Regex {
		args = append(args, "-E")
	} else {
		args = append(args, "-F")
	}
	args = append(args, "-e", q, "--")
	for _, p := range includes(o.Include) {
		if strings.ContainsAny(p, "*?[") && !strings.Contains(p, "/") {
			p = ":(glob)**/" + p
		} else if strings.ContainsAny(p, "*?[") {
			p = ":(glob)" + p
		}
		args = append(args, p)
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var errb bytes.Buffer
	cmd.Stderr = &errb
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	for sc.Scan() {
		// path \0 line \0 text
		parts := bytes.SplitN(sc.Bytes(), []byte{0}, 3)
		if len(parts) != 3 {
			continue
		}
		n, _ := strconv.Atoi(string(parts[1]))
		// git's ERE and Go's RE2 can disagree: Go decides what is highlighted.
		if h, ok := hit(string(parts[0]), n, string(parts[2]), re); ok {
			res.Hits = append(res.Hits, h)
		}
		if len(res.Hits) >= MaxHits {
			res.Truncated = true
			_ = cmd.Process.Kill()
			break
		}
	}
	err = cmd.Wait()
	if res.Truncated {
		return nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		return nil // nothing found
	}
	if err != nil {
		msg := strings.TrimSpace(errb.String())
		if strings.Contains(msg, "regex") || strings.Contains(msg, "Unmatched") || strings.Contains(msg, "Invalid") {
			return ErrBadRegex
		}
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(msg)
	}
	return nil
}

var skipDirs = map[string]bool{".git": true, "node_modules": true, ".hg": true, ".svn": true, ".venv": true, "vendor": true, "target": true, "dist": true, "build": true}

func walk(root string, o Options, re *regexp.Regexp, res *Result) error {
	inc := includes(o.Include)
	seen := 0
	return filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		seen++
		if seen > maxWalk || res.Truncated {
			return filepath.SkipAll
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if !included(rel, inc) {
			return nil
		}
		fi, err := d.Info()
		if err != nil || fi.Size() > maxFile || !fi.Mode().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil || bytes.IndexByte(b, 0) >= 0 {
			return nil
		}
		for i, line := range strings.Split(string(b), "\n") {
			if h, ok := hit(rel, i+1, line, re); ok {
				res.Hits = append(res.Hits, h)
				if len(res.Hits) >= MaxHits {
					res.Truncated = true
					return filepath.SkipAll
				}
			}
		}
		return nil
	})
}
