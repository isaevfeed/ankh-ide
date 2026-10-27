package app

import (
	"errors"
	"os"
	"strings"
	"time"

	"ankh-ide/internal/fstree"
	"ankh-ide/internal/gitx"
	"ankh-ide/internal/review"
	"ankh-ide/internal/search"
	"ankh-ide/internal/state"
)

/* ---------- quick open ---------- */

type fileIndex struct {
	at    time.Time
	files []string
}

// maxIndexed caps the quick-open index for huge folders.
const maxIndexed = 200_000

// ListFiles returns every file of the session for quick open (⌘P): tracked
// and untracked files that git does not ignore, or a plain walk outside git.
// The list is cached until the folder changes.
func (s *Service) ListFiles(sessionID string) ([]string, error) {
	_, ss, err := s.session(sessionID)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	ix, ok := s.indexes[sessionID]
	s.mu.Unlock()
	if ok && time.Since(ix.at) < 2*time.Minute {
		return ix.files, nil
	}
	var files []string
	if s.isRepo(ss.Path) {
		files, err = gitx.Files(ss.Path)
		if err != nil {
			return nil, err
		}
		if len(files) > maxIndexed {
			files = files[:maxIndexed]
		}
	} else {
		files = fstree.Walk(ss.Path, maxIndexed)
	}
	s.mu.Lock()
	s.indexes[sessionID] = fileIndex{at: time.Now(), files: files}
	s.mu.Unlock()
	return files, nil
}

// invalidate drops cached git state and the file index of a session.
func (s *Service) invalidate(sessionID string, filesToo bool) {
	s.mu.Lock()
	delete(s.statuses, sessionID)
	if filesToo {
		delete(s.indexes, sessionID)
	}
	s.mu.Unlock()
}

/* ---------- search ---------- */

// Search finds text in the session's files (⌘⇧F).
func (s *Service) Search(sessionID, query string, opts search.Options) (*search.Result, error) {
	_, ss, err := s.session(sessionID)
	if err != nil {
		return nil, err
	}
	return search.Run(ss.Path, query, opts, s.isRepo(ss.Path))
}

/* ---------- index & commit ---------- */

func (s *Service) repoOf(sessionID string) (*state.Session, error) {
	_, ss, err := s.session(sessionID)
	if err != nil {
		return nil, err
	}
	if !s.isRepo(ss.Path) {
		return nil, errors.New("папка сессии не git-репозиторий")
	}
	return ss, nil
}

// afterGit refreshes everything that depends on git state right away,
// without waiting for the watcher.
func (s *Service) afterGit(sessionID string, filesToo bool) {
	s.invalidate(sessionID, filesToo)
	s.checkGit(sessionID, true)
}

// Stage adds paths to the index; no paths stages everything.
func (s *Service) Stage(sessionID string, paths []string) error {
	ss, err := s.repoOf(sessionID)
	if err != nil {
		return err
	}
	defer s.afterGit(sessionID, false)
	return gitx.Stage(ss.Path, paths)
}

// Unstage takes paths out of the index; no paths unstages everything.
func (s *Service) Unstage(sessionID string, paths []string) error {
	ss, err := s.repoOf(sessionID)
	if err != nil {
		return err
	}
	defer s.afterGit(sessionID, false)
	return gitx.Unstage(ss.Path, paths)
}

// Discard drops uncommitted changes of paths (untracked files are deleted).
func (s *Service) Discard(sessionID string, paths []string) error {
	ss, err := s.repoOf(sessionID)
	if err != nil {
		return err
	}
	s.logf("discard %v session=%s", paths, sessionID)
	defer s.afterGit(sessionID, true)
	return gitx.Discard(ss.Path, paths)
}

// Commit commits the index (with all and nothing staged — every change) and
// returns the short hash.
func (s *Service) Commit(sessionID, message string, all bool) (string, error) {
	ss, err := s.repoOf(sessionID)
	if err != nil {
		return "", err
	}
	defer s.afterGit(sessionID, false)
	h, err := gitx.Commit(ss.Path, message, all)
	if err != nil {
		return "", err
	}
	s.logf("commit %s session=%s", h, sessionID)
	// The work on sent review comments is committed: they are done.
	return h, s.DiscardSent(sessionID)
}

// ListBranches returns the session's local and remote branches.
func (s *Service) ListBranches(sessionID string) (gitx.BranchList, error) {
	ss, err := s.repoOf(sessionID)
	if err != nil {
		return gitx.BranchList{}, err
	}
	return gitx.ListBranches(ss.Path), nil
}

// SwitchBranch checks out (or with create — creates from HEAD) a branch in
// the project's main session; a worktree session is bound to its branch.
func (s *Service) SwitchBranch(sessionID, branch string, create bool) error {
	ss, err := s.repoOf(sessionID)
	if err != nil {
		return err
	}
	if ss.Worktree {
		return errors.New("ветка worktree-сессии закреплена — для другой ветки создайте новую сессию")
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return errors.New("не указана ветка")
	}
	defer s.afterGit(sessionID, true)
	if err := gitx.Switch(ss.Path, branch, create); err != nil {
		return err
	}
	cur := gitx.Branch(ss.Path)
	s.logf("switch %s session=%s", cur, sessionID)
	return s.store.Update(func(d *state.Data) error {
		if _, x := d.Session(sessionID); x != nil {
			x.Branch = cur
		}
		return nil
	})
}

// Push pushes the session's branch, publishing it when it has no upstream.
func (s *Service) Push(sessionID string) (string, error) {
	ss, err := s.repoOf(sessionID)
	if err != nil {
		return "", err
	}
	defer s.afterGit(sessionID, false)
	out, err := gitx.Push(ss.Path)
	if err == nil {
		s.logf("push session=%s", sessionID)
	}
	return out, err
}

/* ---------- comments follow their lines ---------- */

// relocateComments moves the session's comments after their anchor lines in
// the current files and marks the ones whose line is gone. It returns how
// many comments changed.
func (s *Service) relocateComments(sessionID string) int {
	_, ss, err := s.session(sessionID)
	if err != nil || len(ss.Comments) == 0 {
		return 0
	}
	type content struct {
		lines []string
		ok    bool // false: unreadable, binary or too big — leave comments alone
	}
	files := map[string]content{}
	load := func(path string) content {
		if c, ok := files[path]; ok {
			return c
		}
		var c content
		f, err := fstree.Read(ss.Path, path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			c.ok = true // deleted: its comments are outdated
		case err == nil && !f.Binary && !f.TooBig:
			c = content{lines: strings.Split(f.Content, "\n"), ok: true}
		}
		files[path] = c
		return c
	}
	moved := map[string]state.Comment{}
	for _, c := range ss.Comments {
		f := load(c.Path)
		if !f.ok {
			continue
		}
		cc := c
		if review.Relocate(&cc, f.lines) {
			moved[c.ID] = cc
		}
	}
	if len(moved) == 0 {
		return 0
	}
	_ = s.store.Update(func(d *state.Data) error {
		_, x := d.Session(sessionID)
		if x == nil {
			return nil
		}
		for i := range x.Comments {
			if m, ok := moved[x.Comments[i].ID]; ok && m.Text == x.Comments[i].Text {
				m.Sent = x.Comments[i].Sent
				x.Comments[i] = m
			}
		}
		return nil
	})
	return len(moved)
}
