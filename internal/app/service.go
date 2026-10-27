// Package app is the service the UI talks to. It owns projects, sessions,
// terminals, git access and review comments. It does not import Wails:
// the host (main.go) plugs in event emitting, dialogs and the clipboard.
package app

import (
	"encoding/base64"
	"errors"
	"fmt"
	"hash/fnv"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"ankh-ide/internal/agent"
	"ankh-ide/internal/fstree"
	"ankh-ide/internal/fswatch"
	"ankh-ide/internal/gitx"
	"ankh-ide/internal/review"
	"ankh-ide/internal/state"
	"ankh-ide/internal/term"
)

// Host is what the desktop shell provides.
type Host struct {
	Emit         func(name string, data ...any) bool
	PickFolder   func() (string, error)
	SetClipboard func(text string) bool
	// SetBadge sets the Dock icon badge ("" removes it).
	SetBadge func(label string)
}

const (
	StatusIdle    = "idle"
	StatusWorking = "working"
	StatusWaiting = "waiting"
)

type Service struct {
	host         *Host
	store        *state.Store
	terms        *term.Manager
	dir          string
	exe          string
	settingsPath string
	sockPath     string
	listener     net.Listener

	mu        sync.Mutex
	status    map[string]string
	usage     *agent.Usage
	active    string
	fps       map[string]string
	wantUsage map[string]bool
	resumed   map[string]bool // agent was started with --continue
	captures  map[string]*capture
	hooks     map[string]*hookState  // per session: what Claude Code's hooks reported
	statuses  map[string]statusEntry // git status per session, refreshed by poll
	repos     map[string]repoCheck   // path → is a git work tree
	noLimits  bool                   // statusLine came without rate_limits
	shellSeq  int
	stop      chan struct{}
	indexes   map[string]fileIndex // quick-open file lists per session
	badge     string

	watchMu   sync.Mutex
	watcher   fswatch.Watcher // FSEvents on the active session; nil → polling
	watchSess string
	lastCheck time.Time // last git check of the active session
	tmuxPath  string    // "" when tmux is not installed
	tmuxConf  string
	acp       map[string]*acpSession
}

// New creates the service. dir is the config directory, exe this binary.
func New(host *Host, dir, exe string) (*Service, error) {
	st, err := state.Open(filepath.Join(dir, "state.json"))
	if err != nil {
		return nil, err
	}
	s := &Service{
		host:         host,
		store:        st,
		dir:          dir,
		exe:          exe,
		settingsPath: filepath.Join(dir, "claude-settings.json"),
		sockPath:     filepath.Join(dir, "ankh.sock"),
		status:       map[string]string{},
		fps:          map[string]string{},
		wantUsage:    map[string]bool{},
		resumed:      map[string]bool{},
		captures:     map[string]*capture{},
		hooks:        map[string]*hookState{},
		statuses:     map[string]statusEntry{},
		repos:        map[string]repoCheck{},
		stop:         make(chan struct{}),
		indexes:      map[string]fileIndex{},
		acp:          map[string]*acpSession{},
	}
	s.terms = term.NewManager(sink{s})
	s.tmuxPath = agent.FindTmux()
	s.tmuxConf = filepath.Join(dir, "tmux.conf")
	if s.tmuxPath != "" {
		if err := agent.WriteTmuxConf(s.tmuxConf); err != nil {
			s.tmuxPath = ""
		}
	}
	if err := agent.WriteClaudeSettings(s.settingsPath, exe); err != nil {
		return nil, fmt.Errorf("claude settings: %w", err)
	}
	l, err := agent.Listen(s.sockPath, s.onMessage)
	if err != nil {
		return nil, fmt.Errorf("socket: %w", err)
	}
	s.listener = l
	st.View(func(d *state.Data) {
		if p := d.Project(d.ActiveProject); p != nil {
			s.active = p.ActiveSession
		}
	})
	return s, nil
}

func (s *Service) emit(name string, data any) {
	if s.host != nil && s.host.Emit != nil {
		s.host.Emit(name, data)
	}
}

// Start begins background polling; called once by main. It is a function,
// not a method, so the UI cannot call it.
func Start(s *Service) {
	if err := s.EnsureAssistant(); err != nil {
		s.logf("assistant: %v", err)
	}
	go s.rewatch()
	go s.poll()
}

// Shutdown stops terminals and the socket.
func Shutdown(s *Service) {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
	s.stopWatch()
	s.closeAllACP()
	s.terms.KillAll()
	if s.listener != nil {
		_ = s.listener.Close()
	}
	_ = os.Remove(s.sockPath)
}

/* ---------- state ---------- */

type StateDTO struct {
	Data    state.Data        `json:"data"`
	Status  map[string]string `json:"status"`
	Usage   *agent.Usage      `json:"usage"`
	Presets []agent.Preset    `json:"presets"`
	Home    string            `json:"home"`
	Tmux    string            `json:"tmux"` // path of tmux, "" if not installed
	Glab    string            `json:"glab"` // path of glab, "" if not installed
}

func (s *Service) GetState() StateDTO {
	// Refresh branch names: the user may switch branches in a shell.
	_ = s.store.Update(func(d *state.Data) error {
		for _, p := range d.Projects {
			for _, ss := range p.Sessions {
				if b := gitx.Branch(ss.Path); b != "" {
					ss.Branch = b
				}
			}
		}
		return nil
	})
	home, _ := os.UserHomeDir()
	s.mu.Lock()
	defer s.mu.Unlock()
	st := map[string]string{}
	for k, v := range s.status {
		st[k] = v
	}
	return StateDTO{Data: s.store.Snapshot(), Status: st, Usage: s.usage, Presets: agent.Presets, Home: home, Tmux: s.tmuxPath, Glab: glabPath()}
}

func (s *Service) UpdateSettings(st state.Settings) error {
	return s.store.Update(func(d *state.Data) error { d.Settings = st; return nil })
}

/* ---------- projects & sessions ---------- */

// AddProject asks for a folder and adds it. Returns nil when cancelled.
func (s *Service) AddProject() (*state.Project, error) {
	if s.host == nil || s.host.PickFolder == nil {
		return nil, errors.New("выбор папки недоступен")
	}
	path, err := s.host.PickFolder()
	if err != nil || path == "" {
		return nil, err
	}
	return s.AddProjectPath(path)
}

func (s *Service) AddProjectPath(path string) (*state.Project, error) {
	path = filepath.Clean(expandHome(path))
	fi, err := os.Stat(path)
	if err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("папка не найдена: %s", path)
	}
	var res *state.Project
	err = s.store.Update(func(d *state.Data) error {
		for _, p := range d.Projects {
			if p.Path == path {
				res = p
				d.ActiveProject = p.ID
				return nil
			}
		}
		main := &state.Session{ID: state.NewID(), Branch: gitx.Branch(path), Path: path, Agent: d.Settings.DefaultAgent, Comments: []state.Comment{}}
		p := &state.Project{ID: state.NewID(), Name: filepath.Base(path), Path: path, Sessions: []*state.Session{main}, ActiveSession: main.ID}
		d.Projects = append(d.Projects, p)
		d.ActiveProject = p.ID
		res = p
		return nil
	})
	if res != nil {
		s.setActive(res.ActiveSession)
	}
	return res, err
}

// EnsureAssistant adds the pinned assistant: an agent that is not about any
// project and works in ~/.ankh/assistant.
func (s *Service) EnsureAssistant() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, ".ankh", "assistant")
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	return s.store.Update(func(d *state.Data) error {
		for _, p := range d.Projects {
			if p.Assistant {
				return nil
			}
		}
		main := &state.Session{ID: state.NewID(), Path: path, Agent: d.Settings.DefaultAgent, Comments: []state.Comment{}}
		d.Projects = append(d.Projects, &state.Project{ID: state.NewID(), Name: "Ассистент", Path: path, Assistant: true, Sessions: []*state.Session{main}, ActiveSession: main.ID})
		return nil
	})
}

func (s *Service) RemoveProject(id string) error {
	var ids []string
	err := s.store.Update(func(d *state.Data) error {
		if p := d.Project(id); p != nil && p.Assistant {
			return errors.New("ассистента нельзя убрать")
		}
		out := d.Projects[:0]
		for _, p := range d.Projects {
			if p.ID == id {
				for _, ss := range p.Sessions {
					ids = append(ids, ss.ID)
				}
				continue
			}
			out = append(out, p)
		}
		d.Projects = out
		if d.ActiveProject == id {
			d.ActiveProject = ""
			if len(out) > 0 {
				d.ActiveProject = out[0].ID
			}
		}
		return nil
	})
	for _, sid := range ids {
		s.killSessionTerms(sid)
		s.killTmux(sid)
		s.ACPClear(sid)
		s.forget(sid)
	}
	return err
}

// forget drops runtime state of a removed session.
func (s *Service) forget(sessionID string) {
	s.mu.Lock()
	delete(s.status, sessionID)
	delete(s.statuses, sessionID)
	delete(s.indexes, sessionID)
	delete(s.fps, sessionID)
	s.mu.Unlock()
	s.updateBadge()
}

func (s *Service) SetActive(projectID, sessionID string) error {
	err := s.store.Update(func(d *state.Data) error {
		p := d.Project(projectID)
		if p == nil {
			return errors.New("проект не найден")
		}
		d.ActiveProject = p.ID
		if sessionID != "" {
			p.ActiveSession = sessionID
		}
		sessionID = p.ActiveSession
		return nil
	})
	if err == nil {
		s.setActive(sessionID)
		s.MarkSeen(sessionID)
	}
	return err
}

func (s *Service) setActive(id string) {
	s.mu.Lock()
	changed := s.active != id
	s.active = id
	s.mu.Unlock()
	if changed {
		go s.rewatch()
	}
}

// MarkSeen turns "waiting" into "idle" once the user looks at the session.
func (s *Service) MarkSeen(sessionID string) {
	s.mu.Lock()
	changed := s.status[sessionID] == StatusWaiting
	if changed {
		s.status[sessionID] = StatusIdle
	}
	s.mu.Unlock()
	if changed {
		s.emit("status", map[string]string{"session": sessionID, "status": StatusIdle})
		s.updateBadge()
	}
}

// MarkActiveSeen marks the open session seen; called when the app becomes
// active, since the webview does not always get a focus event.
func (s *Service) MarkActiveSeen() {
	s.mu.Lock()
	id := s.active
	s.mu.Unlock()
	if id != "" {
		s.MarkSeen(id)
	}
}

func (s *Service) Branches(projectID string) []string {
	var path string
	s.store.View(func(d *state.Data) {
		if p := d.Project(projectID); p != nil {
			path = p.Path
		}
	})
	if path == "" {
		return nil
	}
	return gitx.Branches(path)
}

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func worktreeDir(project, branch string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ankh", "worktrees", unsafeChars.ReplaceAllString(project, "-"), unsafeChars.ReplaceAllString(branch, "-"))
}

// CreateSession adds a session working in a new git worktree.
func (s *Service) CreateSession(projectID, branch, base, agentID string, setup bool) (*state.Session, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return nil, errors.New("укажите ветку")
	}
	var p state.Project
	s.store.View(func(d *state.Data) {
		if pp := d.Project(projectID); pp != nil {
			p = *pp
		}
	})
	if p.ID == "" {
		return nil, errors.New("проект не найден")
	}
	if !gitx.IsRepo(p.Path) {
		return nil, errors.New("папка проекта не git-репозиторий")
	}
	path := worktreeDir(p.Name, branch)
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("папка уже существует: %s", path)
	}
	if err := gitx.WorktreeAdd(p.Path, path, branch, base); err != nil {
		return nil, err
	}
	sess := &state.Session{ID: state.NewID(), Branch: branch, Path: path, Worktree: true, Base: base, Agent: agentID, Setup: setup, Comments: []state.Comment{}}
	err := s.store.Update(func(d *state.Data) error {
		pp := d.Project(projectID)
		if pp == nil {
			return errors.New("проект не найден")
		}
		pp.Sessions = append(pp.Sessions, sess)
		pp.ActiveSession = sess.ID
		d.ActiveProject = pp.ID
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.setActive(sess.ID)
	return sess, nil
}

// CloseSession removes a worktree session. Without force it refuses when the
// worktree has uncommitted changes (error text "has_changes").
func (s *Service) CloseSession(sessionID string, force, deleteBranch bool) error {
	var p *state.Project
	var ss *state.Session
	s.store.View(func(d *state.Data) {
		pp, x := d.Session(sessionID)
		if pp != nil {
			cp, cs := *pp, *x
			p, ss = &cp, &cs
		}
	})
	if ss == nil {
		return errors.New("сессия не найдена")
	}
	if !ss.Worktree {
		return errors.New("основную сессию проекта закрыть нельзя")
	}
	if !force && gitx.HasChanges(ss.Path) {
		return errors.New("has_changes")
	}
	s.killSessionTerms(sessionID)
	s.killTmux(sessionID)
	s.ACPClear(sessionID)
	s.forget(sessionID)
	time.Sleep(200 * time.Millisecond)
	if err := gitx.WorktreeRemove(p.Path, ss.Path, force); err != nil && !errors.Is(err, os.ErrNotExist) {
		if _, statErr := os.Stat(ss.Path); statErr == nil {
			return err
		}
	}
	if deleteBranch {
		_ = gitx.DeleteBranch(p.Path, ss.Branch)
	}
	return s.store.Update(func(d *state.Data) error {
		pp := d.Project(p.ID)
		if pp == nil {
			return nil
		}
		out := pp.Sessions[:0]
		for _, x := range pp.Sessions {
			if x.ID != sessionID {
				out = append(out, x)
			}
		}
		pp.Sessions = out
		if pp.ActiveSession == sessionID && len(out) > 0 {
			pp.ActiveSession = out[0].ID
		}
		return nil
	})
}

func (s *Service) UpdateProject(projectID, setupScript string) error {
	return s.store.Update(func(d *state.Data) error {
		p := d.Project(projectID)
		if p == nil {
			return errors.New("проект не найден")
		}
		p.SetupScript = setupScript
		return nil
	})
}

func (s *Service) SetSessionAgent(sessionID, agentID string) error {
	return s.store.Update(func(d *state.Data) error {
		_, ss := d.Session(sessionID)
		if ss == nil {
			return errors.New("сессия не найдена")
		}
		ss.Agent = agentID
		ss.Started = false
		return nil
	})
}

/* ---------- terminals ---------- */

func agentTermID(sessionID string) string { return sessionID + "/agent" }

func (s *Service) env(p *state.Project, ss *state.Session) []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "ANKH_") || strings.HasPrefix(kv, "TERM=") || strings.HasPrefix(kv, "TERM_PROGRAM=") || strings.HasPrefix(kv, "TMUX=") || strings.HasPrefix(kv, "TMUX_PANE=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env,
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"TERM_PROGRAM=ankh",
		"LANG="+envOr("LANG", "en_US.UTF-8"),
	)
	return append(env, s.ankhEnv(p, ss)...)
}

// ankhEnv is what hooks need to report back to the right session.
func (s *Service) ankhEnv(p *state.Project, ss *state.Session) []string {
	return []string{
		agent.EnvSession + "=" + ss.ID,
		agent.EnvSocket + "=" + s.sockPath,
		"ANKH_PROJECT=" + p.Path,
		"COLORTERM=truecolor",
	}
}

// killTmux ends the session's tmux session, if any.
func (s *Service) killTmux(sessionID string) {
	if s.tmuxPath != "" {
		agent.TmuxKill(s.tmuxPath, agent.TmuxName(sessionID))
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func (s *Service) session(sessionID string) (*state.Project, *state.Session, error) {
	var p *state.Project
	var ss *state.Session
	s.store.View(func(d *state.Data) {
		pp, x := d.Session(sessionID)
		if pp != nil {
			cp, cs := *pp, *x
			p, ss = &cp, &cs
		}
	})
	if ss == nil {
		return nil, nil, errors.New("сессия не найдена")
	}
	return p, ss, nil
}

// StartAgent starts the session's agent if it is not running and returns the
// terminal id.
func (s *Service) StartAgent(sessionID string, cols, rows int) (string, error) {
	return s.startAgent(sessionID, cols, rows, false)
}

// RestartAgent kills and starts the agent again; fresh skips --continue.
func (s *Service) RestartAgent(sessionID string, cols, rows int, fresh bool) (string, error) {
	id := agentTermID(sessionID)
	if s.terms.Running(id) {
		s.terms.Kill(id)
		for i := 0; i < 40 && s.terms.Running(id); i++ {
			time.Sleep(50 * time.Millisecond)
		}
	}
	s.killTmux(sessionID)
	return s.startAgent(sessionID, cols, rows, fresh)
}

func (s *Service) startAgent(sessionID string, cols, rows int, fresh bool) (string, error) {
	p, ss, err := s.session(sessionID)
	if err != nil {
		return "", err
	}
	id := agentTermID(sessionID)
	if s.terms.Running(id) {
		return id, nil
	}
	pr := agent.Find(ss.Agent)
	// --continue only when Claude Code has a saved conversation for this
	// folder; otherwise it prints "No conversation found to continue" and exits.
	resume := !fresh && pr.Claude && agent.HasConversation(ss.Path)
	setup := ""
	if ss.Setup {
		setup = p.SetupScript
	}
	cmdline := pr.CommandLine(s.settingsPath, resume)
	var useTmux bool
	s.store.View(func(d *state.Data) { useTmux = d.Settings.Tmux })
	if s.tmuxPath != "" {
		name := agent.TmuxName(sessionID)
		// A live tmux session is attached even if the setting was turned off.
		if agent.TmuxHas(s.tmuxPath, name) {
			useTmux, resume, setup = true, false, ""
			s.logf("tmux: reattach %s", name)
			defer s.emit("term:restored", map[string]string{"id": id})
		}
		if useTmux {
			cmdline = agent.TmuxCommandLine(s.tmuxPath, s.tmuxConf, name, ss.Path, s.ankhEnv(p, ss), cmdline)
		}
	}
	spec := term.Spec{
		ID:   id,
		Dir:  ss.Path,
		Argv: agent.Argv(cmdline, setup),
		Env:  s.env(p, ss),
		Cols: uint16(cols), Rows: uint16(rows),
	}
	if err := s.terms.Start(spec); err != nil {
		return "", err
	}
	s.mu.Lock()
	s.resumed[sessionID] = resume
	delete(s.hooks, sessionID)
	s.mu.Unlock()
	_ = s.store.Update(func(d *state.Data) error {
		if _, x := d.Session(sessionID); x != nil {
			x.Started = true
			x.Setup = false
		}
		return nil
	})
	return id, nil
}

// StartShell opens a new plain shell in the session folder.
func (s *Service) StartShell(sessionID string, cols, rows int) (string, error) {
	p, ss, err := s.session(sessionID)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.shellSeq++
	id := fmt.Sprintf("%s/shell/%d", sessionID, s.shellSeq)
	s.mu.Unlock()
	err = s.terms.Start(term.Spec{ID: id, Dir: ss.Path, Argv: agent.ShellArgv(), Env: s.env(p, ss), Cols: uint16(cols), Rows: uint16(rows)})
	return id, err
}

func (s *Service) TermWrite(id, data string) error { return s.terms.Write(id, []byte(data)) }

func (s *Service) TermResize(id string, cols, rows int) error {
	return s.terms.Resize(id, uint16(cols), uint16(rows))
}

func (s *Service) TermKill(id string) { s.terms.Kill(id) }

func (s *Service) TermRunning(id string) bool { return s.terms.Running(id) }

func (s *Service) killSessionTerms(sessionID string) {
	for _, id := range s.terms.IDs() {
		if strings.HasPrefix(id, sessionID+"/") {
			s.terms.Kill(id)
		}
	}
}

type hookState struct {
	seen    bool      // hooks work for this agent process
	started time.Time // SessionStart
	prompt  time.Time // last UserPromptSubmit
}

func (s *Service) hookInfo(sessionID string) hookState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if h := s.hooks[sessionID]; h != nil {
		return *h
	}
	return hookState{}
}

// waitReady waits until the agent can take input: for Claude Code until its
// SessionStart hook fired, otherwise until its output has settled.
func (s *Service) waitReady(sessionID, id string, claude bool) {
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		last := s.terms.LastOutput(id)
		quiet := !last.IsZero() && time.Since(last) > 400*time.Millisecond
		if claude {
			h := s.hookInfo(sessionID)
			if !h.started.IsZero() && quiet {
				return
			}
			// Hooks may be unavailable (old Claude Code): fall back to output.
			if h.started.IsZero() && time.Since(s.terms.StartedAt(id)) > 8*time.Second && quiet {
				return
			}
		} else if quiet && time.Since(last) > 700*time.Millisecond {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// ErrNotAccepted means the agent did not confirm the prompt.
var ErrNotAccepted = errors.New("агент не подтвердил получение промпта — текст мог остаться в поле ввода, проверьте окно агента")

// SendPrompt pastes text into the session's agent and, with submit, presses
// Enter. For Claude Code it waits for the UserPromptSubmit hook to confirm
// the prompt was taken, presses Enter once more if not, and reports failure.
func (s *Service) SendPrompt(sessionID, text string, submit bool) error {
	_, ss, err := s.session(sessionID)
	if err != nil {
		return err
	}
	claude := agent.Find(ss.Agent).Claude
	id := agentTermID(sessionID)
	if !s.terms.Running(id) {
		s.logf("send: agent not running, starting session=%s", sessionID)
		if _, err := s.startAgent(sessionID, 0, 0, false); err != nil {
			return err
		}
	}
	s.waitReady(sessionID, id, claude)
	before := time.Now()
	s.logf("send: paste %d bytes submit=%v session=%s", len(text), submit, sessionID)
	if err := s.terms.Paste(id, text, submit); err != nil {
		s.logf("send: paste failed: %v", err)
		return err
	}
	if !submit || !claude || !s.hookInfo(sessionID).seen {
		return nil
	}
	accepted := func(wait time.Duration) bool {
		end := time.Now().Add(wait)
		for time.Now().Before(end) {
			if s.hookInfo(sessionID).prompt.After(before) {
				return true
			}
			time.Sleep(100 * time.Millisecond)
		}
		return false
	}
	if accepted(3 * time.Second) {
		s.logf("send: accepted")
		return nil
	}
	s.logf("send: no UserPromptSubmit, pressing Enter again")
	_ = s.terms.Write(id, []byte("\r"))
	if accepted(4 * time.Second) {
		s.logf("send: accepted after second Enter")
		return nil
	}
	s.logf("send: not accepted")
	return ErrNotAccepted
}

// SendCommand types a slash command (/compact, /clear) into the agent and
// presses Enter. Built-in commands do not fire UserPromptSubmit, so there is
// nothing to wait for.
func (s *Service) SendCommand(sessionID, cmd string) error {
	id := agentTermID(sessionID)
	if !s.terms.Running(id) {
		return errors.New("агент не запущен")
	}
	if err := s.terms.Write(id, []byte(cmd)); err != nil {
		return err
	}
	time.Sleep(150 * time.Millisecond)
	s.logf("command %s session=%s", cmd, sessionID)
	return s.terms.Write(id, []byte("\r"))
}

// logf appends a line to ankh.log in the config folder (diagnostics).
func (s *Service) logf(format string, args ...any) {
	f, err := os.OpenFile(filepath.Join(s.dir, "ankh.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05.000"), fmt.Sprintf(format, args...))
}

/* ---------- files & git ---------- */

func (s *Service) ListDir(sessionID, rel string) ([]fstree.Entry, error) {
	_, ss, err := s.session(sessionID)
	if err != nil {
		return nil, err
	}
	return fstree.List(ss.Path, rel)
}

func (s *Service) ReadFile(sessionID, rel string) (*fstree.File, error) {
	_, ss, err := s.session(sessionID)
	if err != nil {
		return nil, err
	}
	return fstree.Read(ss.Path, rel)
}

type statusEntry struct {
	at time.Time
	m  map[string]string
}

type repoCheck struct {
	ok bool
	at time.Time
}

// notRepoTTL: how long a "not a repo" answer is trusted. A folder opened
// before `git init` must become a repo without restarting the app.
var notRepoTTL = 5 * time.Second

// isRepo caches `git rev-parse --is-inside-work-tree` per folder: a yes for
// good, a no for notRepoTTL.
func (s *Service) isRepo(path string) bool {
	s.mu.Lock()
	c, ok := s.repos[path]
	s.mu.Unlock()
	if ok && (c.ok || time.Since(c.at) < notRepoTTL) {
		return c.ok
	}
	v := gitx.IsRepo(path)
	s.mu.Lock()
	s.repos[path] = repoCheck{ok: v, at: time.Now()}
	s.mu.Unlock()
	return v
}

// GitStatus returns changed files; big repos are slow to scan, so a status
// up to 3 s old (the poller refreshes it) is served from cache.
func (s *Service) GitStatus(sessionID string) (map[string]string, error) {
	_, ss, err := s.session(sessionID)
	if err != nil {
		return nil, err
	}
	if !s.isRepo(ss.Path) {
		return map[string]string{}, nil
	}
	s.mu.Lock()
	e, ok := s.statuses[sessionID]
	s.mu.Unlock()
	if ok && time.Since(e.at) < 3*time.Second {
		return e.m, nil
	}
	raw, err := gitx.StatusRaw(ss.Path)
	if err != nil {
		return nil, err
	}
	m := gitx.ParseStatus(raw)
	s.mu.Lock()
	s.statuses[sessionID] = statusEntry{at: time.Now(), m: m}
	s.mu.Unlock()
	return m, nil
}

func (s *Service) GetDiff(sessionID, mode string) (*gitx.DiffResult, error) {
	_, ss, err := s.session(sessionID)
	if err != nil {
		return nil, err
	}
	if !s.isRepo(ss.Path) {
		return &gitx.DiffResult{Mode: mode}, nil
	}
	d, err := gitx.Diff(ss.Path, mode, ss.Base)
	if err != nil {
		return nil, err
	}
	d.Relocated = s.relocateComments(sessionID)
	return d, nil
}

func (s *Service) Reveal(sessionID, rel string) error {
	_, ss, err := s.session(sessionID)
	if err != nil {
		return err
	}
	full, err := fstree.Resolve(ss.Path, rel)
	if err != nil {
		return err
	}
	return exec.Command("open", "-R", full).Run()
}

// OpenExternal opens a file in an app ("Cursor", "Zed", "Visual Studio Code")
// or, with an empty app, in the default one.
func (s *Service) OpenExternal(sessionID, rel, app string) error {
	_, ss, err := s.session(sessionID)
	if err != nil {
		return err
	}
	full, err := fstree.Resolve(ss.Path, rel)
	if err != nil {
		return err
	}
	if app == "" {
		return exec.Command("open", full).Run()
	}
	return exec.Command("open", "-a", app, full).Run()
}

func (s *Service) Copy(text string) bool {
	if s.host == nil || s.host.SetClipboard == nil {
		return false
	}
	return s.host.SetClipboard(text)
}

// Notify shows a macOS notification.
func (s *Service) Notify(title, body string) {
	q := func(v string) string { return strings.ReplaceAll(strings.ReplaceAll(v, `\`, `\\`), `"`, `\"`) }
	_ = exec.Command("osascript", "-e", fmt.Sprintf(`display notification "%s" with title "%s"`, q(body), q(title))).Start()
}

/* ---------- review ---------- */

func (s *Service) AddComment(sessionID string, c state.Comment) (state.Comment, error) {
	c.ID = state.NewID()
	c.Sent = false
	if c.Loc == "" {
		c.Loc = review.Loc(c.Path, c.Line, c.LineEnd, c.Col)
	}
	err := s.store.Update(func(d *state.Data) error {
		_, ss := d.Session(sessionID)
		if ss == nil {
			return errors.New("сессия не найдена")
		}
		ss.Comments = append(ss.Comments, c)
		return nil
	})
	return c, err
}

func (s *Service) UpdateComment(sessionID, id, text string) error {
	return s.store.Update(func(d *state.Data) error {
		_, ss := d.Session(sessionID)
		if ss == nil {
			return errors.New("сессия не найдена")
		}
		for i := range ss.Comments {
			if ss.Comments[i].ID == id {
				ss.Comments[i].Text = text
				return nil
			}
		}
		return errors.New("замечание не найдено")
	})
}

func (s *Service) DeleteComment(sessionID, id string) error {
	return s.store.Update(func(d *state.Data) error {
		_, ss := d.Session(sessionID)
		if ss == nil {
			return errors.New("сессия не найдена")
		}
		out := ss.Comments[:0]
		for _, c := range ss.Comments {
			if c.ID != id {
				out = append(out, c)
			}
		}
		ss.Comments = out
		return nil
	})
}

// DiscardDrafts deletes unsent comments; with all it also clears sent ones.
func (s *Service) DiscardDrafts(sessionID string, all bool) error {
	return s.store.Update(func(d *state.Data) error {
		_, ss := d.Session(sessionID)
		if ss == nil {
			return errors.New("сессия не найдена")
		}
		out := ss.Comments[:0]
		for _, c := range ss.Comments {
			if c.Sent && !all {
				out = append(out, c)
			}
		}
		ss.Comments = out
		return nil
	})
}

// DiscardSent deletes sent comments, keeping drafts.
func (s *Service) DiscardSent(sessionID string) error {
	return s.store.Update(func(d *state.Data) error {
		_, ss := d.Session(sessionID)
		if ss == nil {
			return errors.New("сессия не найдена")
		}
		out := ss.Comments[:0]
		for _, c := range ss.Comments {
			if !c.Sent {
				out = append(out, c)
			}
		}
		ss.Comments = out
		return nil
	})
}

// SubmitReview sends all draft comments to the agent as one prompt — to the
// terminal agent, or with chat to the ACP chat.
func (s *Service) SubmitReview(sessionID string, chat bool) (string, error) {
	_, ss, err := s.session(sessionID)
	if err != nil {
		return "", err
	}
	prompt := review.BuildPrompt(ss.Comments)
	if prompt == "" {
		return "", errors.New("нет замечаний для отправки")
	}
	var autoSend bool
	s.store.View(func(d *state.Data) { autoSend = d.Settings.AutoSend })
	if chat {
		err = s.ACPPrompt(sessionID, prompt)
	} else {
		err = s.SendPrompt(sessionID, prompt, autoSend)
	}
	if err != nil {
		return "", err
	}
	err = s.store.Update(func(d *state.Data) error {
		_, x := d.Session(sessionID)
		if x == nil {
			return nil
		}
		for i := range x.Comments {
			if !x.Comments[i].Sent {
				x.Comments[i].Sent = true
				x.Comments[i].Target = ""
			}
		}
		return nil
	})
	return prompt, err
}

/* ---------- usage & status ---------- */

func (s *Service) Usage() *agent.Usage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.usage
}

// RequestUsage runs /usage in the session's agent now, or after its current
// answer if it is busy.
func (s *Service) RequestUsage(sessionID string) (string, error) {
	_, ss, err := s.session(sessionID)
	if err != nil {
		return "", err
	}
	if !agent.Find(ss.Agent).Claude {
		return "", errors.New("/usage есть только у Claude Code")
	}
	id := agentTermID(sessionID)
	if !s.terms.Running(id) {
		return "", errors.New("агент не запущен")
	}
	s.mu.Lock()
	busy := s.status[sessionID] == StatusWorking
	if busy {
		s.wantUsage[sessionID] = true
	}
	s.mu.Unlock()
	if busy {
		return "queued", nil
	}
	return "sent", s.runUsage(sessionID)
}

type capture struct {
	mu  sync.Mutex
	buf []byte
}

// runUsage types /usage into the agent, reads what it prints for a few
// seconds, parses the limits and closes the panel with Esc.
func (s *Service) runUsage(sessionID string) error {
	id := agentTermID(sessionID)
	c := &capture{}
	s.mu.Lock()
	s.captures[id] = c
	s.mu.Unlock()
	if err := s.terms.Write(id, []byte("/usage")); err != nil {
		return err
	}
	time.Sleep(150 * time.Millisecond)
	if err := s.terms.Write(id, []byte("\r")); err != nil {
		return err
	}
	go func() {
		var u *agent.Usage
		for i := 0; i < 40 && u == nil; i++ {
			time.Sleep(150 * time.Millisecond)
			c.mu.Lock()
			raw := string(c.buf)
			c.mu.Unlock()
			if v := agent.ParseUsageText(raw); v != nil && v.FiveHour != nil && v.SevenDay != nil {
				u = v
			}
		}
		s.mu.Lock()
		delete(s.captures, id)
		s.mu.Unlock()
		if u == nil {
			c.mu.Lock()
			raw := string(c.buf)
			c.mu.Unlock()
			u = agent.ParseUsageText(raw)
			_ = os.WriteFile(filepath.Join(s.dir, "usage-last.txt"), []byte(agent.StripANSI(raw)), 0o600)
		}
		if u == nil {
			s.emit("usage:error", "Не удалось прочитать вывод /usage")
			return
		}
		u.At = time.Now().Unix()
		s.mu.Lock()
		if s.usage != nil && s.usage.Source == "statusline" {
			// keep exact reset timestamps from statusLine if we have them
			if u.FiveHour != nil && s.usage.FiveHour != nil {
				u.FiveHour.ResetsAt = s.usage.FiveHour.ResetsAt
			}
			if u.SevenDay != nil && s.usage.SevenDay != nil {
				u.SevenDay.ResetsAt = s.usage.SevenDay.ResetsAt
			}
		}
		s.usage = u
		s.mu.Unlock()
		s.emit("usage", u)
		time.Sleep(300 * time.Millisecond)
		_ = s.terms.Write(id, []byte("\x1b")) // close the /usage panel
	}()
	return nil
}

func (s *Service) setStatus(sessionID, st string) {
	s.mu.Lock()
	if s.status[sessionID] == st {
		s.mu.Unlock()
		return
	}
	s.status[sessionID] = st
	want := st == StatusWaiting && s.wantUsage[sessionID]
	if want {
		delete(s.wantUsage, sessionID)
	}
	s.mu.Unlock()
	s.emit("status", map[string]string{"session": sessionID, "status": st})
	s.updateBadge()
	if want {
		go func() {
			time.Sleep(400 * time.Millisecond)
			_ = s.runUsage(sessionID)
		}()
	}
}

func (s *Service) onMessage(m agent.Message) {
	switch m.Type {
	case "hook":
		s.logf("hook %s session=%s", m.Event, m.Session)
		s.mu.Lock()
		h := s.hooks[m.Session]
		if h == nil {
			h = &hookState{}
			s.hooks[m.Session] = h
		}
		h.seen = true
		switch m.Event {
		case "start":
			h.started = time.Now()
		case "prompt":
			h.prompt = time.Now()
		}
		s.mu.Unlock()
		switch m.Event {
		case "prompt":
			s.setStatus(m.Session, StatusWorking)
		case "stop", "notification":
			s.setStatus(m.Session, StatusWaiting)
		}
	case "statusline":
		// Kept for diagnostics: what Claude Code actually sends.
		_ = os.WriteFile(filepath.Join(s.dir, "statusline-last.json"), m.Payload, 0o600)
		if c := agent.ParseContext(m.Payload); c != nil {
			s.emit("context", map[string]any{"session": m.Session, "context": c})
		}
		u := agent.ParseStatusLine(m.Payload)
		if u == nil {
			s.mu.Lock()
			first := !s.noLimits
			s.noLimits = true
			s.mu.Unlock()
			if first {
				s.emit("usage:missing", true)
			}
		}
		if u != nil {
			u.At = time.Now().Unix()
			s.mu.Lock()
			s.usage = u
			s.mu.Unlock()
			s.emit("usage", u)
		}
	}
}

type sink struct{ s *Service }

func (k sink) Output(id string, data []byte) {
	k.s.emit("term:data", map[string]string{"id": id, "data": base64.StdEncoding.EncodeToString(data)})
	k.s.mu.Lock()
	if c := k.s.captures[id]; c != nil {
		c.mu.Lock()
		c.buf = append(c.buf, data...)
		c.mu.Unlock()
	}
	k.s.mu.Unlock()
	// Agents without hooks: treat output as "working".
	if sid, ok := strings.CutSuffix(id, "/agent"); ok {
		if _, ss, err := k.s.session(sid); err == nil && !agent.Find(ss.Agent).Claude {
			k.s.setStatus(sid, StatusWorking)
		}
	}
}

func (k sink) Exited(id string, code int, ranFor time.Duration) {
	s := k.s
	s.emit("term:exit", map[string]any{"id": id, "code": code})
	sid, ok := strings.CutSuffix(id, "/agent")
	if !ok {
		return
	}
	s.mu.Lock()
	resumed := s.resumed[sid]
	delete(s.resumed, sid)
	s.mu.Unlock()
	// `claude --continue` exits at once when there is nothing to continue:
	// start a fresh conversation instead.
	if resumed && code != 0 && ranFor < 30*time.Second {
		go func() {
			if _, err := s.startAgent(sid, 0, 0, true); err == nil {
				s.emit("term:restarted", map[string]string{"id": id})
			}
		}()
		return
	}
	s.setStatus(sid, StatusIdle)
}

// poll checks the active session's git state (every 1.5 s without a file
// watcher, every 15 s as a safety net with one) and quiet non-Claude agents.
func (s *Service) poll() {
	tk := time.NewTicker(1500 * time.Millisecond)
	defer tk.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-tk.C:
		}
		s.mu.Lock()
		active := s.active
		s.mu.Unlock()
		s.watchMu.Lock()
		watched := s.watcher != nil && s.watchSess == active
		due := !watched || time.Since(s.lastCheck) > 15*time.Second
		s.watchMu.Unlock()
		if active != "" && due {
			s.checkGit(active, false)
		}
		for _, id := range s.terms.IDs() {
			sid, ok := strings.CutSuffix(id, "/agent")
			if !ok {
				continue
			}
			s.mu.Lock()
			working := s.status[sid] == StatusWorking
			s.mu.Unlock()
			if !working {
				continue
			}
			if _, ss, err := s.session(sid); err == nil && !agent.Find(ss.Agent).Claude {
				if time.Since(s.terms.LastOutput(id)) > 4*time.Second {
					s.setStatus(sid, StatusWaiting)
				}
			}
		}
	}
}

// checkGit compares the session's status and HEAD with the last check and
// emits git:changed when they differ (always, with force).
func (s *Service) checkGit(sessionID string, force bool) {
	_, ss, err := s.session(sessionID)
	if err != nil || !s.isRepo(ss.Path) {
		return
	}
	raw, _ := gitx.StatusRaw(ss.Path)
	m := gitx.ParseStatus(raw)
	fp := gitx.Head(ss.Path) + "|" + raw + "|" + changedStamp(ss.Path, m)
	s.mu.Lock()
	s.statuses[sessionID] = statusEntry{at: time.Now(), m: m}
	changed := s.fps[sessionID] != "" && s.fps[sessionID] != fp
	s.fps[sessionID] = fp
	s.mu.Unlock()
	s.watchMu.Lock()
	if sessionID == s.watchSess {
		s.lastCheck = time.Now()
	}
	s.watchMu.Unlock()
	if changed || force {
		s.emit("git:changed", map[string]string{"session": sessionID})
	}
}

// changedStamp fingerprints the contents of already-changed files by size
// and mtime: editing a file that is modified already does not change
// `git status`, but the diff does.
func changedStamp(root string, st map[string]string) string {
	if len(st) > 2000 {
		return ""
	}
	var sum uint64 // order-independent: map iteration order is random
	for p := range st {
		if fi, err := os.Stat(filepath.Join(root, p)); err == nil {
			h := fnv.New64a()
			fmt.Fprintf(h, "%s|%d|%d", p, fi.Size(), fi.ModTime().UnixNano())
			sum += h.Sum64()
		}
	}
	return fmt.Sprint(sum)
}

// updateBadge shows the number of agents waiting for input on the Dock icon.
func (s *Service) updateBadge() {
	s.mu.Lock()
	n := 0
	for _, st := range s.status {
		if st == StatusWaiting {
			n++
		}
	}
	label := ""
	if n > 0 {
		label = fmt.Sprint(n)
	}
	same := label == s.badge
	s.badge = label
	s.mu.Unlock()
	if !same && s.host != nil && s.host.SetBadge != nil {
		s.host.SetBadge(label)
	}
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[2:])
		}
	}
	return p
}
