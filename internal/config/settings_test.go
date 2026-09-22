package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadSettingsFillsMissingEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jev-cm.json")
	body := []byte(`{"provider":"opencode-go","model":"jev-1.13-free","opencode_go_api_key":"go-secret"}`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	saved, err := ReadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	lookup := MergeLookup(saved, func(key string) string {
		if key == "JEV_CM_PROVIDER" {
			return "typesafe"
		}
		return ""
	})
	if lookup("JEV_CM_PROVIDER") != "typesafe" {
		t.Fatalf("env should win, got %s", lookup("JEV_CM_PROVIDER"))
	}
	if lookup("JEV_CM_MODEL") != "jev-1.13-free" || lookup("OPENCODE_GO_API_KEY") != "go-secret" {
		t.Fatalf("settings lookup %#v", saved)
	}
	missing, err := ReadSettings(filepath.Join(dir, "absent.json"))
	if err != nil || len(missing) != 0 {
		t.Fatal(err, missing)
	}
}

func TestDefaultSQLitePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".pi", "agent", "jev-cm.sqlite")
	if got := DefaultSQLitePath(func(string) string { return "" }); got != want {
		t.Fatalf("default %s", got)
	}
	if got := DefaultSQLitePath(func(string) string { return "" }); got != want {
		t.Fatal("paths diverged")
	}
	dir := t.TempDir()
	if got := DefaultSQLitePath(func(key string) string {
		if key == "PI_CODING_AGENT_DIR" {
			return dir
		}
		return ""
	}); got != filepath.Join(dir, "jev-cm.sqlite") {
		t.Fatal(got)
	}
	explicit := filepath.Join(dir, "custom.sqlite")
	cfg, err := configLoad(explicit)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SQLitePath != explicit {
		t.Fatal(cfg.SQLitePath)
	}
}

func configLoad(path string) (Config, error) {
	return Load(nil, func(key string) string {
		if key == "JEV_CM_SQLITE" {
			return path
		}
		return ""
	})
}
