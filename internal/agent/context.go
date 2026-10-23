package agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
)

// Context is how full the agent's context window is.
type Context struct {
	UsedPercentage float64 `json:"usedPercentage"`
	Tokens         int     `json:"tokens"`
	Window         int     `json:"window"`
}

// ParseContext reads context usage from a statusLine payload. It prefers a
// context_window block when Claude Code sends one and otherwise counts the
// tokens of the last assistant turn in the transcript.
func ParseContext(payload []byte) *Context {
	var p struct {
		TranscriptPath string `json:"transcript_path"`
		Model          struct {
			ID string `json:"id"`
		} `json:"model"`
		ContextWindow *struct {
			UsedPercentage    *float64 `json:"used_percentage"`
			ContextWindowSize int      `json:"context_window_size"`
			TotalInputTokens  int      `json:"total_input_tokens"`
		} `json:"context_window"`
	}
	if json.Unmarshal(payload, &p) != nil {
		return nil
	}
	window := 200_000
	if strings.Contains(strings.ToLower(p.Model.ID), "1m") {
		window = 1_000_000
	}
	if cw := p.ContextWindow; cw != nil && cw.UsedPercentage != nil {
		if cw.ContextWindowSize > 0 {
			window = cw.ContextWindowSize
		}
		return &Context{UsedPercentage: *cw.UsedPercentage, Tokens: cw.TotalInputTokens, Window: window}
	}
	if p.TranscriptPath == "" {
		return nil
	}
	tokens := lastTurnTokens(p.TranscriptPath)
	if tokens <= 0 {
		return nil
	}
	return &Context{UsedPercentage: float64(tokens) * 100 / float64(window), Tokens: tokens, Window: window}
}

// lastTurnTokens returns input+cache tokens of the last assistant message,
// which is what the next request will carry.
func lastTurnTokens(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	const tail = 512 * 1024
	if fi, err := f.Stat(); err == nil && fi.Size() > tail {
		_, _ = f.Seek(fi.Size()-tail, io.SeekStart)
	}
	b, _ := io.ReadAll(f)
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 1024*1024), 8*1024*1024)
	last := 0
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.Contains(line, []byte(`"usage"`)) {
			continue
		}
		var m struct {
			Type    string `json:"type"`
			Message struct {
				Usage struct {
					Input       int `json:"input_tokens"`
					CacheCreate int `json:"cache_creation_input_tokens"`
					CacheRead   int `json:"cache_read_input_tokens"`
					Output      int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &m) != nil || m.Type != "assistant" {
			continue
		}
		u := m.Message.Usage
		if t := u.Input + u.CacheCreate + u.CacheRead + u.Output; t > 0 {
			last = t
		}
	}
	return last
}
