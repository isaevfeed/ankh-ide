package fswatch

import (
	"sort"
	"testing"
	"time"
)

func TestDebouncer(t *testing.T) {
	got := make(chan []string, 4)
	d := newDebouncer(30*time.Millisecond, func(p []string) { got <- p })
	d.add([]string{"a"})
	time.Sleep(10 * time.Millisecond)
	d.add([]string{"b", "a"})
	select {
	case p := <-got:
		sort.Strings(p)
		if len(p) != 2 || p[0] != "a" || p[1] != "b" {
			t.Fatalf("batch = %v", p)
		}
	case <-time.After(time.Second):
		t.Fatal("no batch")
	}
	d.stop()
	d.add([]string{"c"})
	select {
	case p := <-got:
		t.Fatalf("after stop: %v", p)
	case <-time.After(80 * time.Millisecond):
	}
}
