// Package state keeps projects, sessions, review comments and settings,
// persisted as one JSON file in the app's config directory.
package state

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

type Settings struct {
	AutoSend     bool   `json:"autoSend"`
	DefaultAgent string `json:"defaultAgent"`
	ShowIgnored  bool   `json:"showIgnored"`
	DiffMode     string `json:"diffMode"` // wip | branch | last
	Editor       string `json:"editor"`   // app for "Open in editor", e.g. "Cursor"
	// Tmux runs agents inside tmux so they outlive the app.
	Tmux bool `json:"tmux"`
	// ACP overrides the command that starts an agent in chat (ACP) mode, per agent id.
	ACP map[string]string `json:"acp,omitempty"`
}

// Comment is one review remark left on a diff line.
type Comment struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Side    string `json:"side"` // "new" or "old" — which line numbering Line/LineEnd use
	Line    int    `json:"line"`
	LineEnd int    `json:"lineEnd,omitempty"`
	Col     int    `json:"col,omitempty"`
	Loc     string `json:"loc"`              // what goes into the prompt, e.g. "a/b.go:137:10"
	Anchor  string `json:"anchor"`           // text of the anchored line, to follow it when lines shift
	Before  string `json:"before,omitempty"` // text of the line above the anchor, to tell identical lines apart
	Deleted bool   `json:"deleted,omitempty"`
	// Outdated: the anchored line is gone from the file.
	Outdated bool `json:"outdated,omitempty"`
	// MovedFrom: the line number the comment was left on, when it has moved since.
	MovedFrom int `json:"movedFrom,omitempty"`
	// Target is where a sent comment went: "" — the agent, "mr" — the GitLab MR.
	Target string `json:"target,omitempty"`
	// MRNote is the GitLab note a comment was imported from.
	MRNote int    `json:"mrNote,omitempty"`
	Author string `json:"author,omitempty"`
	Text   string `json:"text"`
	Sent   bool   `json:"sent,omitempty"`
}

type Session struct {
	ID       string    `json:"id"`
	Branch   string    `json:"branch"`
	Path     string    `json:"path"`
	Worktree bool      `json:"worktree"`
	Base     string    `json:"base,omitempty"`
	Agent    string    `json:"agent"`
	Started  bool      `json:"started"`         // agent ran here before → resume with --continue
	Setup    bool      `json:"setup,omitempty"` // run project setup script before first agent start
	Comments []Comment `json:"comments"`
}

type Project struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Path          string     `json:"path"`
	SetupScript   string     `json:"setupScript"`
	Sessions      []*Session `json:"sessions"`
	ActiveSession string     `json:"activeSession"`
}

type Data struct {
	Projects      []*Project `json:"projects"`
	ActiveProject string     `json:"activeProject"`
	Settings      Settings   `json:"settings"`
}

type Store struct {
	mu   sync.Mutex
	path string
	data Data
}

func NewID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Open loads the store from path, creating defaults when the file is missing.
func Open(path string) (*Store, error) {
	s := &Store{path: path, data: Data{Settings: Settings{AutoSend: true, DefaultAgent: "claude", DiffMode: "wip", Editor: "Cursor"}}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, err
	}
	if s.data.Settings.DiffMode == "" {
		s.data.Settings.DiffMode = "wip"
	}
	if s.data.Settings.DefaultAgent == "" {
		s.data.Settings.DefaultAgent = "claude"
	}
	return s, nil
}

// Update runs fn under the lock and saves the result.
func (s *Store) Update(fn func(d *Data) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fn(&s.data); err != nil {
		return err
	}
	return s.saveLocked()
}

// View runs fn under the lock without saving.
func (s *Store) View(fn func(d *Data)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.data)
}

// Snapshot returns a deep copy of the data.
func (s *Store) Snapshot() Data {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := json.Marshal(s.data)
	var d Data
	_ = json.Unmarshal(b, &d)
	return d
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (d *Data) Project(id string) *Project {
	for _, p := range d.Projects {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// Session finds a session and its project by session id.
func (d *Data) Session(id string) (*Project, *Session) {
	for _, p := range d.Projects {
		for _, s := range p.Sessions {
			if s.ID == id {
				return p, s
			}
		}
	}
	return nil, nil
}
