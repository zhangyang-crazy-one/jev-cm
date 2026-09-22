package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"jev-cm/internal/client"
	"jev-cm/internal/config"
	"jev-cm/internal/memory"
	"jev-cm/internal/store"
)

func TestPrepareIncludesConversationMemory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			State struct {
				Passages []struct {
					ID   string `json:"id"`
					Text string `json:"text"`
				} `json:"passages"`
			} `json:"state"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		answers := map[string]any{}
		for _, passage := range body.State.Passages {
			answers[passage.ID] = map[string]any{"noul": 0.9}
		}
		json.NewEncoder(w).Encode(map[string]any{"model_version": "jev-1.13.0", "answers": answers})
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "jev.sqlite")
	cfg, err := config.Load(map[string]string{"base_url": server.URL, "sqlite_path": path, "pressure_ratio": "0.99"}, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	opened, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	mem := memory.Memory{Config: cfg, Store: opened}
	mem.Ingest(memory.Turn{Text: "ship the refund patch", Role: "assistant", Cwd: "/other", SessionID: "sess", SourceID: "sess"})
	jev := &client.Client{Config: cfg, LookupEnv: func(key string) (string, bool) {
		return "ts_test", key == "TYPESAFE_API_KEY"
	}}
	plan, err := Prepare(map[string]any{"messages": []any{map[string]any{"role": "user", "content": "refund patch"}}}, cfg, jev)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(plan.Context, "\n")
	if plan.Status != "compacted" || !strings.Contains(joined, "ship the refund patch") || !strings.Contains(joined, "cwd:/other") {
		t.Fatalf("%#v", plan)
	}
}
func TestPrepareHostBudgetAndGraphNeighbor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			State struct {
				Passages []struct {
					ID   string `json:"id"`
					Text string `json:"text"`
				} `json:"passages"`
			} `json:"state"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		answers := map[string]any{}
		for _, passage := range body.State.Passages {
			score := 0.1
			if strings.Contains(passage.Text, "beta shipping") {
				score = 0.9
			}
			answers[passage.ID] = map[string]any{"noul": score}
		}
		json.NewEncoder(w).Encode(map[string]any{"model_version": "jev-1.13.0", "answers": answers})
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "jev.sqlite")
	cfg, err := config.Load(map[string]string{"base_url": server.URL, "sqlite_path": path, "pressure_ratio": "0.99"}, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	opened, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	mem := memory.Memory{Config: cfg, Store: opened}
	seed := mem.Ingest(memory.Turn{Text: "alpha refund policy", Role: "user", Cwd: "/other", SessionID: "sess-a", SourceID: "sess-a"})
	neighbor := mem.Ingest(memory.Turn{Text: "beta shipping dock schedule", Role: "assistant", Cwd: "/other", SessionID: "sess-b", SourceID: "sess-b"})
	if err := opened.InsertEdge(seed.ID, neighbor.ID, "related", 0.9); err != nil {
		t.Fatal(err)
	}
	jev := &client.Client{Config: cfg, LookupEnv: func(key string) (string, bool) {
		return "ts_test", key == "TYPESAFE_API_KEY"
	}}
	plan, err := Prepare(map[string]any{
		"messages":    []any{map[string]any{"role": "user", "content": "refund"}},
		"host_budget": 100,
	}, cfg, jev)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(plan.Context, "\n")
	if plan.Status != "compacted" || plan.Budget != 100 {
		t.Fatalf("%#v", plan)
	}
	if !strings.Contains(joined, "beta shipping dock schedule") || strings.Contains(joined, "alpha refund policy") {
		t.Fatalf("%q", joined)
	}
	for _, line := range plan.Context {
		if !strings.HasPrefix(line, "[source:") && !strings.HasPrefix(line, "[jev-cm:") {
			t.Fatalf("generated summary line: %q", line)
		}
	}
}
