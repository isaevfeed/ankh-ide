//go:build !darwin || !cgo

package fswatch

import "time"

// New is not available here; callers fall back to polling.
func New(paths []string, latency time.Duration, cb func([]string)) (Watcher, error) {
	_ = newDebouncer // keep the shared helper compiled on every platform
	return nil, ErrUnsupported
}
