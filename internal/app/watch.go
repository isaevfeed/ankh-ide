package app

import (
	"path/filepath"
	"strings"
	"time"

	"ankh-ide/internal/fswatch"
	"ankh-ide/internal/gitx"
)

// rewatch points the file watcher at the active session: its folder and,
// for worktrees, the git dir that lives elsewhere. Without FSEvents (not
// macOS) the poller keeps checking every 1.5 s.
func (s *Service) rewatch() {
	s.mu.Lock()
	active := s.active
	s.mu.Unlock()
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	if s.watchSess == active && s.watcher != nil {
		return
	}
	if s.watcher != nil {
		s.watcher.Close()
		s.watcher = nil
	}
	s.watchSess = active
	if active == "" {
		return
	}
	_, ss, err := s.session(active)
	if err != nil {
		return
	}
	root := ss.Path
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	paths := []string{root}
	gitDir := ""
	if s.isRepo(ss.Path) {
		gitDir = gitx.GitDir(ss.Path)
		if real, err := filepath.EvalSymlinks(gitDir); err == nil {
			gitDir = real
		}
		if gitDir != "" && !strings.HasPrefix(gitDir, root+string(filepath.Separator)) {
			paths = append(paths, gitDir)
		}
	}
	sid := active
	w, err := fswatch.New(paths, 150*time.Millisecond, func(changed []string) {
		s.onFS(sid, root, gitDir, changed)
	})
	if err != nil {
		if err != fswatch.ErrUnsupported {
			s.logf("watch: %v", err)
		}
		return
	}
	s.watcher = w
	s.logf("watch: %v session=%s", paths, sid)
}

func (s *Service) stopWatch() {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	if s.watcher != nil {
		s.watcher.Close()
		s.watcher = nil
	}
}

// gitMeta reports whether a change inside the git dir matters: HEAD, the
// index and refs do; objects, logs and lock files do not.
func gitMeta(rel string) bool {
	rel = filepath.ToSlash(rel)
	base := filepath.Base(rel)
	if strings.HasSuffix(base, ".lock") {
		return false
	}
	switch base {
	case "HEAD", "index", "ORIG_HEAD", "MERGE_HEAD", "packed-refs":
		return true
	}
	return strings.HasPrefix(rel, "refs/") || strings.Contains(rel, "/refs/")
}

// classify splits FSEvents paths into folders of the work tree that changed
// (relative, "" for the root) and whether git metadata changed.
func classify(root, gitDir string, changed []string) (dirs []string, work, meta bool) {
	seen := map[string]bool{}
	for _, p := range changed {
		if gitDir != "" && (p == gitDir || strings.HasPrefix(p, gitDir+"/")) {
			if gitMeta(strings.TrimPrefix(strings.TrimPrefix(p, gitDir), "/")) {
				meta = true
			}
			continue
		}
		rel, err := filepath.Rel(root, p)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		rel = filepath.ToSlash(rel)
		if rel == ".git" || strings.HasPrefix(rel, ".git/") {
			if gitMeta(strings.TrimPrefix(rel, ".git/")) {
				meta = true
			}
			continue
		}
		work = true
		d := filepath.ToSlash(filepath.Dir(rel))
		if d == "." {
			d = ""
		}
		if !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	return dirs, work, meta
}

func (s *Service) onFS(sessionID, root, gitDir string, changed []string) {
	dirs, work, meta := classify(root, gitDir, changed)
	if !work && !meta {
		return
	}
	if work {
		s.invalidate(sessionID, true)
		if len(dirs) > 200 {
			dirs = nil // too many: the tree reloads what is open
		}
		s.emit("fs:changed", map[string]any{"session": sessionID, "dirs": dirs})
	} else {
		s.invalidate(sessionID, false)
	}
	s.checkGit(sessionID, false)
}
