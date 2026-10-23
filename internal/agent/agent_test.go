package agent

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCommandLine(t *testing.T) {
	c := Find("claude").CommandLine("/tmp/a b/s.json", true)
	if c != "claude --settings '/tmp/a b/s.json' --continue" {
		t.Fatalf("got %q", c)
	}
	if Find("codex").CommandLine("/x", true) != "codex" {
		t.Fatal("non-claude agents get no flags")
	}
	a := Argv("claude", "cp ../.env .")
	if a[len(a)-1] != "{ cp ../.env . ; } ; exec claude" {
		t.Fatalf("argv = %v", a)
	}
}

func TestFormatRef(t *testing.T) {
	p := Find("claude")
	if got := p.FormatRef("a/b.go", 12, 30); got != "@a/b.go:12-30" {
		t.Fatal(got)
	}
	if got := p.FormatRef("a/b.go", 0, 0); got != "@a/b.go" {
		t.Fatal(got)
	}
}

func TestParseStatusLine(t *testing.T) {
	u := ParseStatusLine([]byte(`{"model":{"display_name":"Opus"},"rate_limits":{"five_hour":{"used_percentage":34.5,"resets_at":1790000000},"seven_day":{"used_percentage":61,"resets_at":1790500000}}}`))
	if u == nil || u.FiveHour.UsedPercentage != 34.5 || u.SevenDay.ResetsAt != 1790500000 || u.Model != "Opus" {
		t.Fatalf("%+v", u)
	}
	if ParseStatusLine([]byte(`{"model":{}}`)) != nil {
		t.Fatal("no rate limits must give nil")
	}
}

func TestSettingsAndIPC(t *testing.T) {
	dir := t.TempDir()
	sp := filepath.Join(dir, "claude-settings.json")
	if err := WriteClaudeSettings(sp, "/Apps/Ankh.app/Contents/MacOS/ankh"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(sp)
	var s map[string]any
	if err := json.Unmarshal(b, &s); err != nil || !strings.Contains(string(b), `hook stop`) || s["statusLine"] == nil {
		t.Fatalf("settings: %s", b)
	}

	sock := filepath.Join(dir, "s.sock")
	got := make(chan Message, 2)
	l, err := Listen(sock, func(m Message) { got <- m })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	t.Setenv(EnvSocket, sock)
	t.Setenv(EnvSession, "s1")
	t.Setenv("HOME", dir) // no user statusLine to chain
	var out bytes.Buffer
	RunCLI([]string{"hook", "stop"}, strings.NewReader(`{"session_id":"x"}`), &out)
	RunCLI([]string{"statusline"}, strings.NewReader(`{"rate_limits":{}}`), &out)
	seen := map[string]Message{}
	for i := 0; i < 2; i++ {
		select {
		case m := <-got:
			seen[m.Type] = m
		case <-time.After(2 * time.Second):
			t.Fatalf("message %d not received", i)
		}
	}
	if seen["hook"].Event != "stop" || seen["hook"].Session != "s1" || seen["statusline"].Session != "s1" {
		t.Fatalf("messages = %+v", seen)
	}
}

func TestHasConversation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := filepath.Join(home, "my_proj.v2")
	_ = os.MkdirAll(cwd, 0o755)
	if HasConversation(cwd) {
		t.Fatal("no conversations yet")
	}
	dir := ClaudeProjectDir(cwd)
	if !strings.HasSuffix(dir, strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(cwd, "/", "-"), "_", "-"), ".", "-")) {
		t.Fatalf("dir = %s", dir)
	}
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "a.jsonl"), []byte("{}"), 0o644)
	if !HasConversation(cwd) {
		t.Fatal("conversation not found")
	}
}

func TestTmuxCommandLine(t *testing.T) {
	got := TmuxCommandLine("/opt/homebrew/bin/tmux", "/c/tmux.conf", "ankh-1", "/p/it's", []string{"ANKH_SESSION=1"}, "claude --settings '/x y'")
	want := `'/opt/homebrew/bin/tmux' -u -L ankh -f '/c/tmux.conf' new-session -A -s 'ankh-1' -c '/p/it'\''s' -e 'ANKH_SESSION=1' 'claude --settings '\''/x y'\'''`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestTmuxLive(t *testing.T) {
	tmux := FindTmux()
	if tmux == "" {
		t.Skip("no tmux")
	}
	dir := t.TempDir()
	conf := filepath.Join(dir, "tmux.conf")
	if err := WriteTmuxConf(conf); err != nil {
		t.Fatal(err)
	}
	name := "ankh-test-" + filepath.Base(dir)
	cmd := exec.Command(tmux, "-L", TmuxSocket, "-f", conf, "new-session", "-d", "-s", name, "sleep 30")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("tmux cannot start here: %v %s", err, out)
	}
	if !TmuxHas(tmux, name) {
		t.Fatal("session must exist")
	}
	TmuxKill(tmux, name)
	if TmuxHas(tmux, name) {
		t.Fatal("session must be gone")
	}
}
