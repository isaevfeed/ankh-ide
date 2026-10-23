// Package fswatch reports file changes under a set of folders. On macOS it
// uses FSEvents; elsewhere New returns ErrUnsupported and callers poll.
package fswatch

import (
	"errors"
	"sync"
	"time"
)

var ErrUnsupported = errors.New("fswatch: not supported on this platform")

// Watcher delivers batches of changed absolute paths until Close.
type Watcher interface {
	Close()
}

// debouncer collects paths and hands them over once events calm down.
type debouncer struct {
	mu    sync.Mutex
	wait  time.Duration
	paths map[string]bool
	timer *time.Timer
	cb    func([]string)
	done  bool
}

func newDebouncer(wait time.Duration, cb func([]string)) *debouncer {
	return &debouncer{wait: wait, cb: cb, paths: map[string]bool{}}
}

func (d *debouncer) add(paths []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.done {
		return
	}
	for _, p := range paths {
		d.paths[p] = true
	}
	if d.timer == nil {
		d.timer = time.AfterFunc(d.wait, d.fire)
	} else {
		d.timer.Reset(d.wait)
	}
}

func (d *debouncer) fire() {
	d.mu.Lock()
	if d.done || len(d.paths) == 0 {
		d.mu.Unlock()
		return
	}
	out := make([]string, 0, len(d.paths))
	for p := range d.paths {
		out = append(out, p)
	}
	d.paths = map[string]bool{}
	d.mu.Unlock()
	d.cb(out)
}

func (d *debouncer) stop() {
	d.mu.Lock()
	d.done = true
	if d.timer != nil {
		d.timer.Stop()
	}
	d.mu.Unlock()
}
