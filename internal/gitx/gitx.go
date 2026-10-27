// Package gitx wraps the user's own `git` binary, so their config, hooks and
// credentials apply.
package gitx

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"ankh-ide/internal/diffparse"
)

const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// maxUntracked limits how big an untracked file may be to be shown as a diff.
const maxUntracked = 512 * 1024

// Run executes git in dir and returns stdout.
func Run(dir string, args ...string) (string, error) {
	full := append([]string{"-c", "core.quotepath=off", "-c", "color.ui=false"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return out.String(), nil
}

func IsRepo(dir string) bool {
	out, err := Run(dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}

func Branch(dir string) string {
	out, err := Run(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func hasHead(dir string) bool {
	_, err := Run(dir, "rev-parse", "--verify", "-q", "HEAD")
	return err == nil
}

// Branches lists local branches, current first.
func Branches(dir string) []string {
	out, err := Run(dir, "for-each-ref", "--format=%(refname:short)", "--sort=-committerdate", "refs/heads")
	if err != nil {
		return nil
	}
	cur := Branch(dir)
	res := []string{}
	if cur != "" && cur != "HEAD" {
		res = append(res, cur)
	}
	for _, b := range strings.Split(strings.TrimSpace(out), "\n") {
		if b != "" && b != cur {
			res = append(res, b)
		}
	}
	return res
}

// DefaultBase picks the branch a feature branch is compared against.
func DefaultBase(dir string) string {
	for _, b := range []string{"develop", "main", "master", "origin/develop", "origin/main", "origin/master"} {
		if _, err := Run(dir, "rev-parse", "--verify", "-q", b); err == nil {
			return b
		}
	}
	return ""
}

// Status maps changed paths to M / A / D (untracked files count as A).
func Status(dir string) (map[string]string, error) {
	raw, err := StatusRaw(dir)
	if err != nil {
		return nil, err
	}
	return ParseStatus(raw), nil
}

// StatusRaw is `git status --porcelain=v1 -z` output.
func StatusRaw(dir string) (string, error) {
	return Run(dir, "status", "--porcelain=v1", "-z", "--untracked-files=all")
}

// ParseStatus turns StatusRaw output into path → M / A / D.
func ParseStatus(out string) map[string]string {
	res := map[string]string{}
	parts := strings.Split(out, "\x00")
	for i := 0; i < len(parts); i++ {
		p := parts[i]
		if len(p) < 4 {
			continue
		}
		xy, path := p[:2], p[3:]
		switch {
		case xy == "??" || strings.ContainsRune(xy, 'A'):
			res[path] = "A"
		case strings.ContainsRune(xy, 'D'):
			res[path] = "D"
		default:
			res[path] = "M"
		}
		if xy[0] == 'R' || xy[0] == 'C' {
			i++ // the original path follows
		}
	}
	return res
}

// Head returns the current commit hash ("" in an empty repo).
func Head(dir string) string {
	out, _ := Run(dir, "rev-parse", "HEAD")
	return strings.TrimSpace(out)
}

type DiffResult struct {
	Mode   string               `json:"mode"`
	Base   string               `json:"base"`
	Branch string               `json:"branch"`
	Files  []diffparse.FileDiff `json:"files"`
	// Index (wip mode): path → "staged" | "partial" | "" (nothing staged).
	Index map[string]string `json:"index,omitempty"`
	// Relocated: how many review comments moved or went outdated while
	// building this diff — the UI reloads them.
	Relocated int `json:"relocated,omitempty"`
}

// Diff returns changes for a mode:
//
//	wip    — everything not committed (staged, unstaged, untracked) vs HEAD
//	branch — the whole branch vs merge-base with base (plus uncommitted work)
//	last   — the last commit
func Diff(dir, mode, base string) (*DiffResult, error) {
	res := &DiffResult{Mode: mode, Branch: Branch(dir)}
	args := []string{"diff", "--no-ext-diff", "--no-color", "-U3", "-M"}
	untracked := true
	switch mode {
	case "branch":
		if base == "" {
			base = DefaultBase(dir)
		}
		if base == "" {
			return nil, errors.New("не нашёл базовую ветку (develop/main/master)")
		}
		mb, err := Run(dir, "merge-base", "HEAD", base)
		if err != nil {
			return nil, err
		}
		res.Base = base
		args = append(args, strings.TrimSpace(mb))
	case "last":
		args = []string{"show", "--format=", "--no-ext-diff", "--no-color", "-U3", "-M", "HEAD"}
		untracked = false
		res.Base = "HEAD~1"
	default:
		res.Mode = "wip"
		if hasHead(dir) {
			args = append(args, "HEAD")
		} else {
			args = append(args, emptyTree)
		}
		res.Base = "HEAD"
		if raw, err := StatusRaw(dir); err == nil {
			res.Index = Index(raw)
		}
	}
	out, err := Run(dir, args...)
	if err != nil {
		return nil, err
	}
	res.Files = diffparse.Parse(out)
	if untracked {
		list, err := Run(dir, "ls-files", "--others", "--exclude-standard", "-z")
		if err == nil {
			for _, p := range strings.Split(list, "\x00") {
				if p == "" {
					continue
				}
				full := filepath.Join(dir, p)
				fi, err := os.Stat(full)
				if err != nil || fi.IsDir() {
					continue
				}
				if fi.Size() > maxUntracked {
					res.Files = append(res.Files, diffparse.FileDiff{Path: p, Status: "A", Binary: true})
					continue
				}
				b, err := os.ReadFile(full)
				if err != nil {
					continue
				}
				res.Files = append(res.Files, diffparse.AddedFile(p, string(b)))
			}
		}
	}
	// one row per path: untracked listing can repeat a path the diff has
	seen := map[string]bool{}
	files := res.Files[:0]
	for _, f := range res.Files {
		if seen[f.Path] {
			continue
		}
		seen[f.Path] = true
		files = append(files, f)
	}
	res.Files = files
	if res.Files == nil {
		res.Files = []diffparse.FileDiff{}
	}
	return res, nil
}

// WorktreeAdd creates a worktree at path. A new branch is created from base
// unless the branch already exists.
func WorktreeAdd(repo, path, branch, base string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if _, err := Run(repo, "rev-parse", "--verify", "-q", "refs/heads/"+branch); err == nil {
		_, err := Run(repo, "worktree", "add", path, branch)
		return err
	}
	args := []string{"worktree", "add", "-b", branch, path}
	if base != "" {
		args = append(args, base)
	}
	_, err := Run(repo, args...)
	return err
}

func WorktreeRemove(repo, path string, force bool) error {
	args := []string{"worktree", "remove", path}
	if force {
		args = []string{"worktree", "remove", "--force", path}
	}
	_, err := Run(repo, args...)
	return err
}

func DeleteBranch(repo, branch string) error {
	_, err := Run(repo, "branch", "-D", branch)
	return err
}

// HasChanges reports uncommitted changes in dir.
func HasChanges(dir string) bool {
	st, err := Status(dir)
	return err == nil && len(st) > 0
}

// CheckIgnored returns the subset of rel paths ignored by git.
func CheckIgnored(dir string, rel []string) map[string]bool {
	res := map[string]bool{}
	if len(rel) == 0 {
		return res
	}
	cmd := exec.Command("git", "check-ignore", "-z", "--stdin")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(strings.Join(rel, "\x00") + "\x00")
	out, _ := cmd.Output() // exit 1 means "nothing ignored"
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			res[p] = true
		}
	}
	return res
}

// Files lists tracked and untracked (not ignored) files that exist on disk.
func Files(dir string) ([]string, error) {
	out, err := Run(dir, "ls-files", "-co", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	gone := map[string]bool{}
	if del, err := Run(dir, "ls-files", "-d", "-z"); err == nil {
		for _, p := range strings.Split(del, "\x00") {
			if p != "" {
				gone[p] = true
			}
		}
	}
	seen := map[string]bool{}
	res := []string{}
	for _, p := range strings.Split(out, "\x00") {
		if p == "" || gone[p] || seen[p] {
			continue
		}
		seen[p] = true
		res = append(res, p)
	}
	return res, nil
}

// GitDir is the absolute git directory of a work tree (for worktrees it is
// <repo>/.git/worktrees/<name>).
func GitDir(dir string) string {
	out, err := Run(dir, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// Index tells, per changed path, how much of it is staged:
// "staged" — all of it, "partial" — some, "" — nothing (or untracked).
func Index(raw string) map[string]string {
	res := map[string]string{}
	parts := strings.Split(raw, "\x00")
	for i := 0; i < len(parts); i++ {
		p := parts[i]
		if len(p) < 4 {
			continue
		}
		x, y, path := p[0], p[1], p[3:]
		switch {
		case x == '?' || x == '!':
			res[path] = ""
		case x != ' ' && y == ' ':
			res[path] = "staged"
		case x != ' ':
			res[path] = "partial"
		default:
			res[path] = ""
		}
		if (x == 'R' || x == 'C') && i+1 < len(parts) {
			i++
			if x == 'R' && parts[i] != "" {
				// the old path's removal is in the index already
				res[parts[i]] = "staged"
			}
		}
	}
	return res
}

// Stage adds paths (including deletions) to the index; no paths means all.
// Paths gone from the work tree are staged as removals, which also works
// when the removal is staged already (git add would fail: "did not match").
func Stage(dir string, paths []string) error {
	if len(paths) == 0 {
		_, err := Run(dir, "add", "-A", "--", ".")
		return err
	}
	var present, gone []string
	for _, p := range paths {
		if _, err := os.Lstat(filepath.Join(dir, p)); err == nil {
			present = append(present, p)
		} else {
			gone = append(gone, p)
		}
	}
	if len(present) > 0 {
		if _, err := Run(dir, append([]string{"add", "-A", "--"}, present...)...); err != nil {
			return err
		}
	}
	if len(gone) > 0 {
		if _, err := Run(dir, append([]string{"rm", "-r", "-q", "--cached", "--ignore-unmatch", "--"}, gone...)...); err != nil {
			return err
		}
	}
	return nil
}

// Unstage removes paths from the index, keeping the work tree; no paths means all.
func Unstage(dir string, paths []string) error {
	if len(paths) == 0 {
		paths = []string{"."}
	}
	if !hasHead(dir) {
		_, err := Run(dir, append([]string{"rm", "-r", "-q", "--cached", "--ignore-unmatch", "--"}, paths...)...)
		return err
	}
	// reset, unlike restore --staged, accepts paths unknown to the index
	_, err := Run(dir, append([]string{"reset", "-q", "--"}, paths...)...)
	return err
}

// Discard throws away uncommitted changes of paths: tracked files go back to
// HEAD, untracked ones are deleted. It refuses paths outside dir.
func Discard(dir string, paths []string) error {
	if len(paths) == 0 {
		return errors.New("не выбраны файлы")
	}
	raw, err := StatusRaw(dir)
	if err != nil {
		return err
	}
	untracked := map[string]bool{}
	added := map[string]bool{}
	for _, p := range strings.Split(raw, "\x00") {
		if len(p) < 4 {
			continue
		}
		if p[:2] == "??" {
			untracked[p[3:]] = true
		} else if p[0] == 'A' {
			added[p[3:]] = true
		}
	}
	var tracked []string
	for _, p := range paths {
		full := filepath.Join(dir, p)
		if rel, err := filepath.Rel(dir, full); err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("путь вне проекта: %s", p)
		}
		switch {
		case untracked[p]:
			if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
				return err
			}
		case added[p]:
			// new file that was only staged: drop it from the index and disk
			if _, err := Run(dir, "rm", "-q", "-f", "--cached", "--", p); err != nil {
				return err
			}
			if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
				return err
			}
		default:
			tracked = append(tracked, p)
		}
	}
	if len(tracked) == 0 {
		return nil
	}
	if !hasHead(dir) {
		return errors.New("в репозитории нет коммитов — откатывать не к чему")
	}
	_, err = Run(dir, append([]string{"restore", "--source=HEAD", "--staged", "--worktree", "--"}, tracked...)...)
	return err
}

// Commit commits the index; with all it stages every change first, even
// when part of the work is staged already. It returns the new commit's
// short hash.
func Commit(dir, message string, all bool) (string, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "", errors.New("пустое сообщение коммита")
	}
	if all {
		if err := Stage(dir, nil); err != nil {
			return "", err
		}
	}
	if !HasStaged(dir) {
		return "", errors.New("нечего коммитить: в индексе пусто")
	}
	cmd := exec.Command("git", "commit", "-q", "-F", "-")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	cmd.Stdin = strings.NewReader(message + "\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	h, _ := Run(dir, "rev-parse", "--short", "HEAD")
	return strings.TrimSpace(h), nil
}

// HasStaged reports whether the index differs from HEAD.
func HasStaged(dir string) bool {
	raw, err := StatusRaw(dir)
	if err != nil {
		return false
	}
	for _, s := range Index(raw) {
		if s != "" {
			return true
		}
	}
	return false
}
