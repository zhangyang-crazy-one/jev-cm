package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jev-cm/internal/client"
	"jev-cm/internal/config"
	"jev-cm/internal/memory"
	"jev-cm/internal/store"
)

func TestLivePrepare(t *testing.T) {
	if os.Getenv("JEV_CM_LIVE") != "1" {
		t.Skip("set JEV_CM_LIVE=1 to call the configured Jev provider")
	}
	db := filepath.Join(t.TempDir(), "jev.sqlite")
	t.Setenv("JEV_CM_SQLITE", db)
	t.Setenv("JEV_CM_TOKEN_BUDGET", "1200")
	t.Setenv("JEV_CM_PRESSURE_RATIO", "0.4")
	t.Setenv("JEV_CM_PROTECTED_TAIL_TOKENS", "15")
	t.Setenv("JEV_CM_DROP_THRESHOLD", "0.99")
	cfg, jev := liveClient(t)
	old := strings.Repeat("OBSOLETE 1999 cafeteria note: pea soup on Tuesday. Do not use this for current policy. ", 30)
	recent := "Current refund policy: unopened items can be returned within 30 days."
	plan, err := Prepare(map[string]any{"messages": []any{
		map[string]any{"id": "prefix", "role": "system", "kind": "prefix", "content": "Frozen rules: answer from original text only."},
		map[string]any{"id": "old", "role": "tool", "kind": "tool", "content": old},
		map[string]any{"id": "recent", "role": "tool", "kind": "tool", "content": recent},
		map[string]any{"id": "user", "role": "user", "kind": "prose", "content": "What is the refund window?"},
	}}, cfg, jev)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != "compacted" {
		t.Fatalf("status %s", plan.Status)
	}
	joined := strings.Join(plan.Context, "\n")
	marker := "pointer="
	start := strings.Index(joined, marker)
	if start < 0 || strings.Contains(joined, "pea soup") {
		t.Fatalf("old tool was not replaced by a pointer: %s", joined)
	}
	pointer := joined[start+len(marker):]
	if end := strings.IndexAny(pointer, " \n]"); end >= 0 {
		pointer = pointer[:end]
	}
	opened, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	page, err := opened.Expand(pointer, 0, cfg.PageBytes)
	if err != nil || !page.Found || string(page.Data) != old {
		t.Fatalf("expand found=%v err=%v bytes=%d", page.Found, err, len(page.Data))
	}
	fact := "The refund window is 30 days for unopened items."
	mem := memory.Memory{Config: cfg, Client: jev, Store: opened}
	if err := mem.ImportSource("live", "policy", []string{fact}); err != nil {
		t.Fatal(err)
	}
	recalled := mem.Recall("What is the refund window?", "live", cfg.TokenBudget)
	if recalled.Status != "ok" || len(recalled.Passages) == 0 || recalled.Passages[0].Text != fact || recalled.Passages[0].Unranked || recalled.Passages[0].Probability == nil {
		t.Fatalf("recall status=%s passages=%d", recalled.Status, len(recalled.Passages))
	}
	t.Logf("elided pointer=%s recall_probability=%.3f", pointer, *recalled.Passages[0].Probability)
}

func liveClient(t *testing.T) (config.Config, *client.Client) {
	t.Helper()
	saved, err := config.ReadSettings(config.SettingsPath(os.Getenv))
	if err != nil {
		t.Fatal(err)
	}
	lookup := config.MergeLookup(saved, os.Getenv)
	cfg, err := config.Load(nil, lookup)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, &client.Client{Config: cfg, LookupEnv: func(key string) (string, bool) {
		value := lookup(key)
		if value == "" {
			return "", false
		}
		return value, true
	}}
}
