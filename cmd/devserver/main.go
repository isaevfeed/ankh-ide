// Command devserver runs the Ankh backend over plain HTTP so the UI can be
// developed and tested in a browser, without Wails:
//
//	cd frontend && npm run build:http && cd .. && go run ./cmd/devserver
//	open http://localhost:7777
//
// POST /call {"method":"GetState","args":[...]} invokes a Service method;
// GET /events streams backend events (Server-Sent Events).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sync"

	"ankh-ide/internal/app"
)

type hub struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

func (h *hub) emit(name string, data ...any) bool {
	var d any
	if len(data) == 1 {
		d = data[0]
	} else if len(data) > 1 {
		d = data
	}
	b, _ := json.Marshal(map[string]any{"name": name, "data": d})
	h.mu.Lock()
	for c := range h.subs {
		select {
		case c <- b:
		default:
		}
	}
	h.mu.Unlock()
	return false
}

func main() {
	addr := flag.String("addr", "127.0.0.1:7777", "listen address")
	dist := flag.String("dist", "frontend/dist", "built frontend")
	cfg := flag.String("config", "", "config dir (default: <user config>/Ankh-dev)")
	flag.Parse()

	if *cfg == "" {
		d, _ := os.UserConfigDir()
		*cfg = filepath.Join(d, "Ankh-dev")
	}
	exe, _ := os.Executable()
	h := &hub{subs: map[chan []byte]struct{}{}}
	host := &app.Host{
		Emit: h.emit,
		PickFolder: func() (string, error) {
			return "", fmt.Errorf("в браузере выбор папки недоступен: используйте AddProjectPath")
		},
		SetClipboard: func(string) bool { return false },
	}
	svc, err := app.New(host, *cfg, exe)
	if err != nil {
		log.Fatal(err)
	}
	app.Start(svc)
	defer app.Shutdown(svc)

	sv := reflect.ValueOf(svc)
	http.HandleFunc("/call", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string            `json:"method"`
			Args   []json.RawMessage `json:"args"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		m := sv.MethodByName(req.Method)
		if !m.IsValid() {
			writeJSON(w, map[string]any{"error": "no method " + req.Method})
			return
		}
		t := m.Type()
		if t.NumIn() != len(req.Args) {
			writeJSON(w, map[string]any{"error": fmt.Sprintf("%s expects %d args, got %d", req.Method, t.NumIn(), len(req.Args))})
			return
		}
		in := make([]reflect.Value, t.NumIn())
		for i := range in {
			p := reflect.New(t.In(i))
			if err := json.Unmarshal(req.Args[i], p.Interface()); err != nil {
				writeJSON(w, map[string]any{"error": fmt.Sprintf("arg %d: %v", i, err)})
				return
			}
			in[i] = p.Elem()
		}
		out := m.Call(in)
		res := map[string]any{}
		errT := reflect.TypeOf((*error)(nil)).Elem()
		for _, v := range out {
			if v.Type() == errT {
				if !v.IsNil() {
					res["error"] = v.Interface().(error).Error()
				}
				continue
			}
			res["result"] = v.Interface()
		}
		writeJSON(w, res)
	})
	http.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flush", 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		c := make(chan []byte, 1024)
		h.mu.Lock()
		h.subs[c] = struct{}{}
		h.mu.Unlock()
		defer func() { h.mu.Lock(); delete(h.subs, c); h.mu.Unlock() }()
		fl.Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case b := <-c:
				fmt.Fprintf(w, "data: %s\n\n", b)
				fl.Flush()
			}
		}
	})
	http.Handle("/", http.FileServer(http.Dir(*dist)))
	log.Printf("Ankh devserver on http://%s (config %s)", *addr, *cfg)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
