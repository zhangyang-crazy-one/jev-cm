package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jev-cm/internal/client"
	"jev-cm/internal/config"
	"jev-cm/internal/store"
	"jev-cm/internal/tokens"
)

func openMemory(t *testing.T, handler http.HandlerFunc, extra map[string]string) (*Memory, *store.Store, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	file := map[string]string{
		"base_url": server.URL, "sqlite_path": filepath.Join(t.TempDir(), "memory.sqlite"),
		"remember_threshold": "0.75", "recall_cutoff": "0.5", "token_budget": "100", "shortlist_limit": "10",
	}
	for key, value := range extra {
		file[key] = value
	}
	cfg, err := config.Load(file, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	opened, err := store.Open(cfg.SQLitePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { opened.Close() })
	return &Memory{Config: cfg, Client: &client.Client{Config: cfg, LookupEnv: func(key string) (string, bool) {
		return "ts_test", key == "TYPESAFE_API_KEY"
	}}, Store: opened}, opened, server
}

func TestWriteGateSummaryAndUnavailable(t *testing.T) {
	var calls int
	mem, db, _ := openMemory(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct {
			State     string                     `json:"state"`
			Questions map[string]client.Question `json:"questions"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if !strings.Contains(body.Questions["worth_remembering"].Instructions, "High probability means this entry is durable") {
			t.Fatalf("polarity missing: %s", body.Questions["worth_remembering"].Instructions)
		}
		score := 0.91
		if strings.Contains(body.State, "filler") {
			score = 0.2
		}
		json.NewEncoder(w).Encode(map[string]any{"model_version": "jev-1.13.0", "answers": map[string]any{"worth_remembering": map[string]any{"noul": score}}})
	}, nil)
	if got := mem.Remember("filler note", "src-a", "default", false); got.Status != "refused" {
		t.Fatal(got)
	}
	if n, _ := db.CountMemory("", ""); n != 0 {
		t.Fatal(n)
	}
	stored := mem.Remember("Keep the deploy window on Tuesday", "src-b", "default", false)
	if stored.Status != "stored" {
		t.Fatal(stored)
	}
	rows, err := db.MemoryRows("default", "src-b")
	if err != nil || len(rows) != 1 || rows[0].Text != "Keep the deploy window on Tuesday" {
		t.Fatal(err, rows)
	}
	sum := sha256.Sum256([]byte(rows[0].Text))
	if rows[0].SHA256 != hex.EncodeToString(sum[:]) || !rows[0].ModelVersion.Valid || rows[0].ModelVersion.String != "jev-1.13.0" {
		t.Fatalf("%#v", rows[0])
	}
	before := calls
	if got := mem.Remember("invented recap", "src-c", "default", true); got.Status != "refused" || got.Reason != "summary" || calls != before {
		t.Fatalf("%#v calls %d", got, calls)
	}
	bareCfg, _ := config.Load(map[string]string{"sqlite_path": mem.Config.SQLitePath}, func(string) string { return "" })
	bare := Memory{Config: bareCfg, Client: &client.Client{Config: bareCfg, LookupEnv: func(string) (string, bool) { return "", false }}, Store: db}
	if got := bare.Remember("anything", "src-d", "default", false); got.Status != "unavailable" {
		t.Fatal(got)
	}
	if n, _ := db.CountMemory("", ""); n != 1 {
		t.Fatal(n)
	}
}

func TestRecallRankEmptyUnrankedAndReplace(t *testing.T) {
	mem, db, server := openMemory(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/systemone") && r.URL.Path != "/" && !strings.Contains(r.URL.String(), serverURLPath(r)) {
			// httptest path is "/" when base url has no path. Accept that and still require no other routes.
		}
		var body struct {
			State struct {
				Passages []struct {
					ID   string `json:"id"`
					Text string `json:"text"`
				} `json:"passages"`
			} `json:"state"`
			Questions map[string]client.Question `json:"questions"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		answers := map[string]any{}
		for key := range body.Questions {
			text := ""
			for _, passage := range body.State.Passages {
				if passage.ID == key {
					text = passage.Text
				}
			}
			score := 0.7
			if strings.Contains(text, "obsolete") {
				score = 0.2
			} else if strings.Contains(text, "alpha") {
				score = 0.95
			}
			answers[key] = map[string]any{"noul": score}
		}
		json.NewEncoder(w).Encode(map[string]any{"model_version": "jev-1.13.0", "answers": answers})
	}, nil)
	if err := mem.ImportSource("default", "policy", []string{"one"}); err != nil {
		t.Fatal(err)
	}
	if err := mem.ImportSource("default", "other", []string{"unrelated deploy guide"}); err != nil {
		t.Fatal(err)
	}
	if err := mem.ImportSource("default", "policy", []string{"refund policy allows ten days", "refund policy allows thirty days"}); err != nil {
		t.Fatal(err)
	}
	rows, _ := db.MemoryRows("default", "policy")
	if len(rows) != 2 || rows[0].Text != "refund policy allows ten days" || rows[1].Text != "refund policy allows thirty days" {
		t.Fatalf("%#v", rows)
	}
	other, _ := db.MemoryRows("default", "other")
	if len(other) != 1 {
		t.Fatal(other)
	}
	short := "refund alpha"
	long := "refund " + strings.Repeat("x", 80)
	if err := mem.ImportSource("ranked", "policy", []string{short, long, "obsolete note about refund"}); err != nil {
		t.Fatal(err)
	}
	mem.Config.TokenBudget = tokens.Estimate(short)
	ranked := mem.Recall("refund", "ranked", tokens.Estimate(short))
	if ranked.Status != "ok" || len(ranked.Passages) != 1 || ranked.Passages[0].Text != short || ranked.Passages[0].SourceID != "policy" {
		t.Fatalf("%#v", ranked)
	}
	if len(ranked.OverBudget) != 1 || ranked.OverBudget[0] != "policy" {
		t.Fatalf("over %#v", ranked.OverBudget)
	}
	if err := mem.ImportSource("quiet", "policy", []string{"obsolete note about refund"}); err != nil {
		t.Fatal(err)
	}
	empty := mem.Recall("refund", "quiet", 100)
	if empty.Status != "ok" || len(empty.Passages) != 0 {
		t.Fatalf("%#v", empty)
	}
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer slow.Close()
	cfg, err := config.Load(map[string]string{"base_url": slow.URL, "sqlite_path": mem.Config.SQLitePath, "timeout_seconds": "0.1"}, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	stalled := Memory{Config: cfg, Client: &client.Client{Config: cfg, LookupEnv: func(key string) (string, bool) { return "ts_test", key == "TYPESAFE_API_KEY" }}, Store: db}
	if err := stalled.ImportSource("stalled", "policy", []string{"refund policy allows thirty days"}); err != nil {
		t.Fatal(err)
	}
	unranked := stalled.Recall("refund", "stalled", 100)
	if unranked.Status != "ok" || len(unranked.Passages) != 1 || !unranked.Passages[0].Unranked || unranked.Passages[0].Probability != nil {
		t.Fatalf("%#v", unranked)
	}
	if unranked.Passages[0].Text != "refund policy allows thirty days" {
		t.Fatal(unranked.Passages[0].Text)
	}
	_ = server
}

func serverURLPath(r *http.Request) string { return r.URL.Path }
