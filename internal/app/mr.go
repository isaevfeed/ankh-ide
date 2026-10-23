package app

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"ankh-ide/internal/fstree"
	"ankh-ide/internal/gitx"
	"ankh-ide/internal/glab"
	"ankh-ide/internal/review"
	"ankh-ide/internal/state"
)

var (
	glabOnce sync.Once
	glabBin  string
)

func glabPath() string {
	glabOnce.Do(func() { glabBin = glab.Path() })
	return glabBin
}

// MRInfo is what the changes panel shows about the branch's merge request.
type MRInfo struct {
	MR     *glab.MR `json:"mr"`     // nil: no open MR for the branch
	Pushed bool     `json:"pushed"` // local HEAD is the MR head
	Branch string   `json:"branch"`
	Base   string   `json:"base"`
}

func (s *Service) mrSession(sessionID string) (*state.Session, error) {
	if glabPath() == "" {
		return nil, glab.ErrNoGlab
	}
	return s.repoOf(sessionID)
}

// MRStatus returns the open MR of the session's branch.
func (s *Service) MRStatus(sessionID string) (*MRInfo, error) {
	ss, err := s.mrSession(sessionID)
	if err != nil {
		return nil, err
	}
	mr, err := glab.View(ss.Path)
	if err != nil {
		return nil, err
	}
	info := &MRInfo{MR: mr, Branch: gitx.Branch(ss.Path), Base: ss.Base}
	if info.Base == "" {
		info.Base = strings.TrimPrefix(gitx.DefaultBase(ss.Path), "origin/")
	}
	if mr != nil {
		info.Pushed = mr.DiffRefs != nil && mr.DiffRefs.HeadSHA == gitx.Head(ss.Path)
		if ds, err := glab.Discussions(ss.Path, mr); err == nil {
			mr.Unresolved = glab.Unresolved(ds)
		}
	}
	return info, nil
}

// MRCreate pushes the branch and opens a merge request into the base branch.
func (s *Service) MRCreate(sessionID string, draft bool) (*MRInfo, error) {
	ss, err := s.mrSession(sessionID)
	if err != nil {
		return nil, err
	}
	target := ss.Base
	if target == "" {
		target = strings.TrimPrefix(gitx.DefaultBase(ss.Path), "origin/")
	}
	if err := glab.Create(ss.Path, target, draft); err != nil {
		return nil, err
	}
	s.logf("mr: created for session=%s target=%s", sessionID, target)
	return s.MRStatus(sessionID)
}

// MRPostComments publishes the draft comments as MR threads: on their line
// when the MR has the same code as the work tree, otherwise as one general
// note with locations. Returns a summary for the toast.
func (s *Service) MRPostComments(sessionID string) (string, error) {
	ss, err := s.mrSession(sessionID)
	if err != nil {
		return "", err
	}
	mr, err := glab.View(ss.Path)
	if err != nil {
		return "", err
	}
	if mr == nil {
		return "", errors.New("у ветки нет открытого MR")
	}
	var drafts []state.Comment
	for _, c := range ss.Comments {
		if !c.Sent && strings.TrimSpace(c.Text) != "" {
			drafts = append(drafts, c)
		}
	}
	if len(drafts) == 0 {
		return "", errors.New("нет черновиков")
	}
	pushed := mr.DiffRefs != nil && mr.DiffRefs.HeadSHA == gitx.Head(ss.Path)
	dirty := map[string]bool{}
	if st, err := gitx.Status(ss.Path); err == nil {
		for p := range st {
			dirty[p] = true
		}
	}
	var inline int
	var rest []state.Comment
	posted := map[string]bool{}
	for _, c := range drafts {
		if pushed && !dirty[c.Path] && c.Side != "old" && !c.Outdated {
			line := c.Line
			if c.LineEnd > 0 {
				line = c.LineEnd
			}
			if err := glab.PostLine(ss.Path, mr, c.Path, line, c.Text); err == nil {
				inline++
				posted[c.ID] = true
				continue
			} else {
				s.logf("mr: inline note failed %s: %v", c.Loc, err)
			}
		}
		rest = append(rest, c)
	}
	if len(rest) > 0 {
		body := review.BuildPrompt(rest)
		body = strings.Replace(body, "Исправь замечания по ревью", "Замечания по ревью", 1)
		if err := glab.PostNote(ss.Path, mr, body); err != nil {
			if inline == 0 {
				return "", err
			}
			return fmt.Sprintf("В MR: %d на строках; остальные не отправлены: %v", inline, err), s.markMR(sessionID, posted)
		}
		for _, c := range rest {
			posted[c.ID] = true
		}
	}
	msg := fmt.Sprintf("В MR !%d: %d на строках", mr.IID, inline)
	if len(rest) > 0 {
		msg += fmt.Sprintf(", %d общим комментарием", len(rest))
		if !pushed {
			msg += " (локальный код не запушен)"
		}
	}
	return msg, s.markMR(sessionID, posted)
}

func (s *Service) markMR(sessionID string, ids map[string]bool) error {
	return s.store.Update(func(d *state.Data) error {
		_, x := d.Session(sessionID)
		if x == nil {
			return nil
		}
		for i := range x.Comments {
			if ids[x.Comments[i].ID] {
				x.Comments[i].Sent = true
				x.Comments[i].Target = "mr"
			}
		}
		return nil
	})
}

// MRImport turns unresolved MR threads on lines into draft comments, so a
// reviewer's remarks can go to the agent with Submit. Returns how many were added.
func (s *Service) MRImport(sessionID string) (int, error) {
	ss, err := s.mrSession(sessionID)
	if err != nil {
		return 0, err
	}
	mr, err := glab.View(ss.Path)
	if err != nil {
		return 0, err
	}
	if mr == nil {
		return 0, errors.New("у ветки нет открытого MR")
	}
	ds, err := glab.Discussions(ss.Path, mr)
	if err != nil {
		return 0, err
	}
	have := map[int]bool{}
	for _, c := range ss.Comments {
		if c.MRNote != 0 {
			have[c.MRNote] = true
		}
	}
	lines := map[string][]string{}
	fileLines := func(p string) []string {
		if l, ok := lines[p]; ok {
			return l
		}
		var l []string
		if f, err := fstree.Read(ss.Path, p); err == nil && !f.Binary && !f.TooBig {
			l = strings.Split(f.Content, "\n")
		}
		lines[p] = l
		return l
	}
	var add []state.Comment
	for _, d := range ds {
		if len(d.Notes) == 0 {
			continue
		}
		first := d.Notes[0]
		if first.System || !first.Resolvable || first.Resolved || first.Position == nil || have[first.ID] {
			continue
		}
		pos := first.Position
		c := state.Comment{ID: state.NewID(), MRNote: first.ID, Author: first.Author.Username}
		var texts []string
		for _, n := range d.Notes {
			if !n.System && strings.TrimSpace(n.Body) != "" {
				texts = append(texts, strings.TrimSpace(n.Body))
			}
		}
		c.Text = strings.Join(texts, "\n")
		if pos.NewLine > 0 {
			c.Path, c.Side, c.Line = pos.NewPath, "new", pos.NewLine
			if l := fileLines(c.Path); pos.NewLine-1 < len(l) {
				c.Anchor = strings.TrimRight(l[pos.NewLine-1], "\r")
				if pos.NewLine >= 2 {
					c.Before = strings.TrimRight(l[pos.NewLine-2], "\r")
				}
			}
		} else {
			c.Path, c.Side, c.Line, c.Deleted = pos.OldPath, "old", pos.OldLine, true
		}
		c.Loc = review.Loc(c.Path, c.Line, 0, 0)
		add = append(add, c)
	}
	if len(add) == 0 {
		return 0, nil
	}
	err = s.store.Update(func(d *state.Data) error {
		_, x := d.Session(sessionID)
		if x == nil {
			return errors.New("сессия не найдена")
		}
		x.Comments = append(x.Comments, add...)
		return nil
	})
	return len(add), err
}

// OpenURL opens an http(s) link in the default browser.
func (s *Service) OpenURL(url string) error {
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		return errors.New("неверная ссылка")
	}
	return exec.Command("open", url).Start()
}
