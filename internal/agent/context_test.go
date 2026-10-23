package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseContext(t *testing.T) {
	c := ParseContext([]byte(`{"context_window":{"used_percentage":42.5,"context_window_size":200000,"total_input_tokens":85000}}`))
	if c == nil || c.UsedPercentage != 42.5 || c.Tokens != 85000 {
		t.Fatalf("%+v", c)
	}
	dir := t.TempDir()
	tr := filepath.Join(dir, "t.jsonl")
	_ = os.WriteFile(tr, []byte(`{"type":"user","message":{"content":"hi"}}
{"type":"assistant","message":{"usage":{"input_tokens":10,"cache_creation_input_tokens":1000,"cache_read_input_tokens":20000,"output_tokens":500}}}
{"type":"assistant","message":{"usage":{"input_tokens":5,"cache_creation_input_tokens":0,"cache_read_input_tokens":99995,"output_tokens":0}}}
`), 0o644)
	c = ParseContext([]byte(`{"transcript_path":"` + tr + `","model":{"id":"claude-opus"}}`))
	if c == nil || c.Tokens != 100000 || c.UsedPercentage != 50 {
		t.Fatalf("%+v", c)
	}
	c = ParseContext([]byte(`{"transcript_path":"` + tr + `","model":{"id":"claude-sonnet[1m]"}}`))
	if c == nil || c.Window != 1_000_000 || c.UsedPercentage != 10 {
		t.Fatalf("%+v", c)
	}
	if ParseContext([]byte(`{}`)) != nil {
		t.Fatal("want nil")
	}
}
