// Package glab talks to GitLab through the user's own `glab` CLI, so its
// login (glab auth login), hosts and tokens apply.
package glab

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Path finds glab, also where Homebrew installs it.
func Path() string {
	if p, err := exec.LookPath("glab"); err == nil {
		return p
	}
	for _, d := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/opt/local/bin"} {
		p := filepath.Join(d, "glab")
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

// ErrNoGlab means glab is not installed.
var ErrNoGlab = errors.New("glab не найден — установите: brew install glab, затем glab auth login")

func run(dir string, args ...string) ([]byte, error) {
	bin := Path()
	if bin == "" {
		return nil, ErrNoGlab
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "NO_COLOR=1", "GLAB_NO_PROMPT=1", "GIT_TERMINAL_PROMPT=0", "NO_PROMPT=1")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = strings.TrimSpace(out.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return out.Bytes(), errors.New(msg)
	}
	return out.Bytes(), nil
}

type DiffRefs struct {
	BaseSHA  string `json:"base_sha"`
	HeadSHA  string `json:"head_sha"`
	StartSHA string `json:"start_sha"`
}

type MR struct {
	IID          int       `json:"iid"`
	ProjectID    int       `json:"project_id"`
	Title        string    `json:"title"`
	WebURL       string    `json:"web_url"`
	State        string    `json:"state"`
	Draft        bool      `json:"draft"`
	SourceBranch string    `json:"source_branch"`
	TargetBranch string    `json:"target_branch"`
	MergeStatus  string    `json:"detailed_merge_status"`
	Notes        int       `json:"user_notes_count"`
	DiffRefs     *DiffRefs `json:"diff_refs"`
	HeadPipeline *struct {
		Status string `json:"status"`
		WebURL string `json:"web_url"`
	} `json:"head_pipeline"`
	// Unresolved is filled by Discussions callers, not by GitLab.
	Unresolved int `json:"unresolved"`
}

// noMR matches glab's messages when the branch has no merge request.
func noMR(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "no open merge request") || strings.Contains(m, "no merge request") || strings.Contains(m, "not found")
}

// View returns the open MR of the current branch, or nil when there is none.
func View(dir string) (*MR, error) {
	out, err := run(dir, "mr", "view", "--output", "json")
	if err != nil {
		if noMR(err.Error()) {
			return nil, nil
		}
		return nil, err
	}
	var mr MR
	if err := json.Unmarshal(out, &mr); err != nil {
		return nil, fmt.Errorf("glab mr view: %w", err)
	}
	if mr.IID == 0 {
		return nil, nil
	}
	return &mr, nil
}

// Create pushes the branch and opens an MR titled from its commits.
func Create(dir, target string, draft bool) error {
	args := []string{"mr", "create", "--fill", "--yes", "--push"}
	if target != "" {
		args = append(args, "--target-branch", target)
	}
	if draft {
		args = append(args, "--draft")
	}
	_, err := run(dir, args...)
	return err
}

type Position struct {
	NewPath string `json:"new_path"`
	OldPath string `json:"old_path"`
	NewLine int    `json:"new_line"`
	OldLine int    `json:"old_line"`
}

type Note struct {
	ID     int    `json:"id"`
	Body   string `json:"body"`
	System bool   `json:"system"`
	Author struct {
		Username string `json:"username"`
	} `json:"author"`
	Resolvable bool      `json:"resolvable"`
	Resolved   bool      `json:"resolved"`
	Position   *Position `json:"position"`
}

type Discussion struct {
	ID    string `json:"id"`
	Notes []Note `json:"notes"`
}

func mrPath(mr *MR) string {
	return fmt.Sprintf("projects/%d/merge_requests/%d", mr.ProjectID, mr.IID)
}

// Discussions lists the MR's threads (up to 100 per page, 5 pages).
func Discussions(dir string, mr *MR) ([]Discussion, error) {
	var all []Discussion
	for page := 1; page <= 5; page++ {
		out, err := run(dir, "api", fmt.Sprintf("%s/discussions?per_page=100&page=%d", mrPath(mr), page))
		if err != nil {
			return nil, err
		}
		var ds []Discussion
		if err := json.Unmarshal(out, &ds); err != nil {
			return nil, fmt.Errorf("discussions: %w", err)
		}
		all = append(all, ds...)
		if len(ds) < 100 {
			break
		}
	}
	return all, nil
}

// Unresolved counts open review threads.
func Unresolved(ds []Discussion) int {
	n := 0
	for _, d := range ds {
		if len(d.Notes) > 0 && d.Notes[0].Resolvable && !d.Notes[0].Resolved {
			n++
		}
	}
	return n
}

// PostLine starts a thread on a line of the MR diff (new side).
func PostLine(dir string, mr *MR, path string, line int, body string) error {
	if mr.DiffRefs == nil {
		return errors.New("у MR нет diff_refs")
	}
	_, err := run(dir, "api", "-X", "POST", mrPath(mr)+"/discussions",
		"-f", "body="+body,
		"-f", "position[position_type]=text",
		"-f", "position[base_sha]="+mr.DiffRefs.BaseSHA,
		"-f", "position[start_sha]="+mr.DiffRefs.StartSHA,
		"-f", "position[head_sha]="+mr.DiffRefs.HeadSHA,
		"-f", "position[new_path]="+path,
		"-f", "position[old_path]="+path,
		"-F", "position[new_line]="+strconv.Itoa(line),
	)
	return err
}

// PostNote adds a general comment to the MR.
func PostNote(dir string, mr *MR, body string) error {
	_, err := run(dir, "api", "-X", "POST", mrPath(mr)+"/notes", "-f", "body="+body)
	return err
}
