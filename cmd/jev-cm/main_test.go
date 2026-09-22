package main

import (
	"bytes"
	"encoding/json"
	"jev-cm/internal/store"
	"os"
	"path/filepath"
	"testing"
)

func TestPurgeRequiresYes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jev.sqlite")
	lookup := func(key string) string {
		if key == "JEV_CM_SQLITE" {
			return path
		}
		return ""
	}
	opened, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := opened.Close(); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if code := run([]string{"purge"}, lookup, bytes.NewReader(nil), &stdout, &bytes.Buffer{}); code != 0 {
		t.Fatal(code, stdout.String())
	}
	var kept map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &kept); err != nil || kept["deleted"] != false {
		t.Fatal(err, stdout.String())
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if code := run([]string{"purge", "--yes"}, lookup, bytes.NewReader(nil), &stdout, &bytes.Buffer{}); code != 0 {
		t.Fatal(code, stdout.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
