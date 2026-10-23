//go:build darwin && cgo

package fswatch

/*
#cgo LDFLAGS: -framework CoreServices
#include <stdint.h>
#include <stdlib.h>

void *ankhFSStart(uintptr_t handle, char **paths, int n, double latency);
void ankhFSStop(void *stream);
*/
import "C"

import (
	"errors"
	"runtime/cgo"
	"time"
	"unsafe"
)

type darwinWatcher struct {
	stream unsafe.Pointer
	handle cgo.Handle
	deb    *debouncer
}

// New starts an FSEvents stream over paths. latency is how long FSEvents
// coalesces events; cb receives changed paths after a further 100 ms of calm.
func New(paths []string, latency time.Duration, cb func([]string)) (Watcher, error) {
	if len(paths) == 0 {
		return nil, errors.New("fswatch: no paths")
	}
	w := &darwinWatcher{deb: newDebouncer(100*time.Millisecond, cb)}
	w.handle = cgo.NewHandle(w)
	cpaths := make([]*C.char, len(paths))
	for i, p := range paths {
		cpaths[i] = C.CString(p)
	}
	defer func() {
		for _, p := range cpaths {
			C.free(unsafe.Pointer(p))
		}
	}()
	w.stream = C.ankhFSStart(C.uintptr_t(w.handle), (**C.char)(unsafe.Pointer(&cpaths[0])), C.int(len(cpaths)), C.double(latency.Seconds()))
	if w.stream == nil {
		w.handle.Delete()
		return nil, errors.New("fswatch: FSEventStreamStart failed")
	}
	return w, nil
}

func (w *darwinWatcher) Close() {
	if w.stream == nil {
		return
	}
	C.ankhFSStop(w.stream) // synchronous: no callbacks run after it returns
	w.stream = nil
	w.deb.stop()
	w.handle.Delete()
}

//export ankhFSEvents
func ankhFSEvents(handle C.uintptr_t, n C.size_t, paths **C.char) {
	w, ok := cgo.Handle(handle).Value().(*darwinWatcher)
	if !ok || n == 0 {
		return
	}
	list := unsafe.Slice(paths, int(n))
	out := make([]string, 0, len(list))
	for _, p := range list {
		out = append(out, C.GoString(p))
	}
	w.deb.add(out)
}
