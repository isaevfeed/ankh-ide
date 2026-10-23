// Package fstree lists directories and reads files inside a session root,
// refusing paths that escape it.
package fstree

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"ankh-ide/internal/gitx"
)

const MaxPreview = 1 << 20 // 1 MiB

type Entry struct {
	Name    string `json:"name"`
	Path    string `json:"path"` // relative to root, forward slashes
	Dir     bool   `json:"dir"`
	Ignored bool   `json:"ignored"`
}

type File struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Binary  bool   `json:"binary"`
	TooBig  bool   `json:"tooBig"`
	Size    int64  `json:"size"`
}

var ErrOutside = errors.New("путь вне папки проекта")

// Resolve joins rel to root and checks the result stays inside root.
func Resolve(root, rel string) (string, error) {
	rel = filepath.FromSlash(strings.TrimPrefix(rel, "/"))
	full := filepath.Clean(filepath.Join(root, rel))
	r := filepath.Clean(root)
	if full != r && !strings.HasPrefix(full, r+string(filepath.Separator)) {
		return "", ErrOutside
	}
	return full, nil
}

type cached struct {
	dirMod, ignoreMod time.Time
	entries           []Entry
}

var (
	cacheMu sync.Mutex
	cache   = map[string]cached{}
)

func modTime(p string) time.Time {
	if fi, err := os.Stat(p); err == nil {
		return fi.ModTime()
	}
	return time.Time{}
}

// List returns the entries of rel (dirs first, then files, by name). Results
// are cached until the folder or the root .gitignore changes, so reopening a
// big project is instant.
func List(root, rel string) ([]Entry, error) {
	dir, err := Resolve(root, rel)
	if err != nil {
		return nil, err
	}
	dm, im := modTime(dir), modTime(filepath.Join(root, ".gitignore"))
	cacheMu.Lock()
	c, ok := cache[dir]
	cacheMu.Unlock()
	if ok && !dm.IsZero() && c.dirMod.Equal(dm) && c.ignoreMod.Equal(im) {
		return append([]Entry(nil), c.entries...), nil
	}
	res, err := list(root, rel, dir)
	if err == nil {
		cacheMu.Lock()
		cache[dir] = cached{dirMod: dm, ignoreMod: im, entries: res}
		cacheMu.Unlock()
	}
	return res, err
}

func list(root, rel, dir string) ([]Entry, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	res := make([]Entry, 0, len(des))
	var rels []string
	for _, d := range des {
		name := d.Name()
		if name == ".git" || name == ".DS_Store" {
			continue
		}
		p := name
		if rel != "" && rel != "." {
			p = strings.TrimSuffix(filepath.ToSlash(rel), "/") + "/" + name
		}
		isDir := d.IsDir()
		if d.Type()&os.ModeSymlink != 0 {
			if fi, err := os.Stat(filepath.Join(dir, name)); err == nil {
				isDir = fi.IsDir()
			}
		}
		res = append(res, Entry{Name: name, Path: p, Dir: isDir})
		rels = append(rels, p)
	}
	ign := gitx.CheckIgnored(root, rels)
	for i := range res {
		res[i].Ignored = ign[res[i].Path]
	}
	sort.SliceStable(res, func(i, j int) bool {
		if res[i].Dir != res[j].Dir {
			return res[i].Dir
		}
		return strings.ToLower(res[i].Name) < strings.ToLower(res[j].Name)
	})
	return res, nil
}

// Read returns a file for preview, flagging binary or oversized content.
func Read(root, rel string) (*File, error) {
	full, err := Resolve(root, rel)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(full)
	if err != nil {
		return nil, err
	}
	f := &File{Path: filepath.ToSlash(rel), Size: fi.Size()}
	if fi.Size() > MaxPreview {
		f.TooBig = true
		return f, nil
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(b) || strings.IndexByte(string(b), 0) >= 0 {
		f.Binary = true
		return f, nil
	}
	f.Content = string(b)
	return f, nil
}

// skipDirs are never descended into by Walk.
var skipDirs = map[string]bool{".git": true, "node_modules": true, ".hg": true, ".svn": true, "dist": true, "build": true, ".next": true, ".venv": true, "vendor": true, "target": true}

// Walk lists files under root (relative, forward slashes) for folders that
// are not git repositories. It stops after limit files.
func Walk(root string, limit int) []string {
	res := []string{}
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if rel, err := filepath.Rel(root, p); err == nil {
			res = append(res, filepath.ToSlash(rel))
		}
		if len(res) >= limit {
			return filepath.SkipAll
		}
		return nil
	})
	return res
}
