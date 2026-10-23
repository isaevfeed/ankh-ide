package glab

import (
	"encoding/json"
	"testing"
)

func TestParse(t *testing.T) {
	raw := `[{"id":"a","notes":[{"id":1,"body":"rename","author":{"username":"ann"},"resolvable":true,"resolved":false,"position":{"new_path":"x.go","new_line":12}}]},
	{"id":"b","notes":[{"id":2,"body":"ok","resolvable":true,"resolved":true}]},
	{"id":"c","notes":[{"id":3,"body":"merged","system":true}]}]`
	var ds []Discussion
	if err := json.Unmarshal([]byte(raw), &ds); err != nil {
		t.Fatal(err)
	}
	if Unresolved(ds) != 1 || ds[0].Notes[0].Position.NewLine != 12 || ds[0].Notes[0].Author.Username != "ann" {
		t.Fatalf("%+v", ds)
	}
	if !noMR("no open merge request available for \"feature/x\"") {
		t.Fatal("noMR")
	}
}
