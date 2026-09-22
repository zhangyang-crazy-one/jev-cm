package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jev-cm/internal/client"
	"jev-cm/internal/config"
	"jev-cm/internal/model"
	"jev-cm/internal/store"
	"jev-cm/internal/tokens"
)

func newEngine(t *testing.T, handler http.HandlerFunc, extra map[string]string) (*Engine, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	file := map[string]string{
		"base_url": server.URL, "sqlite_path": filepath.Join(t.TempDir(), "jev.sqlite"),
		"pressure_ratio": "0", "token_budget": "10000", "drop_threshold": "0.25", "page_bytes": "4",
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
	return &Engine{Config: cfg, Client: &client.Client{Config: cfg, LookupEnv: func(key string) (string, bool) {
		return "ts_test", key == "TYPESAFE_API_KEY"
	}}, Store: opened}, server
}

func answers(scores map[string]float64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			State     string                     `json:"state"`
			Questions map[string]client.Question `json:"questions"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		out := map[string]any{}
		for key := range body.Questions {
			score := 0.9
			if value, ok := scores[key]; ok {
				score = value
			}
			out[key] = map[string]any{"noul": score}
		}
		json.NewEncoder(w).Encode(map[string]any{"model_version": "jev-1.13.0", "answers": out, "echo": body.State})
	}
}

func TestElisionPrefixTailAndExpand(t *testing.T) {
	protected := "NEW-OUTPUT-PROTECTED"
	old := "OLD-OUTPUT-SHOULD-ELIDE-AND-STAY-ORIGINAL"
	var seen string
	eng, _ := newEngine(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			State     string                     `json:"state"`
			Questions map[string]client.Question `json:"questions"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		seen = body.State
		out := map[string]any{}
		for key := range body.Questions {
			score := 0.9
			if key == "old" {
				score = 0.1
			}
			if key == "keep" {
				score = 0.25
			}
			out[key] = map[string]any{"noul": score}
		}
		json.NewEncoder(w).Encode(map[string]any{"model_version": "jev-1.13.0", "answers": out})
	}, map[string]string{"protected_tail_tokens": itoa(tokens.Estimate(protected))})
	messages := []model.Message{
		{ID: "prefix", Role: "system", Kind: "prefix", Content: "RULES-DO-NOT-REWRITE"},
		{ID: "old", Role: "tool", Kind: "tool", Content: old},
		{ID: "keep", Role: "tool", Kind: "tool", Content: "MAYBE-KEEP"},
		{ID: "tail", Role: "tool", Kind: "tool", Content: protected},
	}
	result := eng.Compact(messages)
	if result.Status != "compacted" || len(result.Messages) != 4 {
		t.Fatalf("%#v", result)
	}
	if result.Messages[0].Content != "RULES-DO-NOT-REWRITE" || result.Messages[2].Content != "MAYBE-KEEP" || result.Messages[3].Content != protected {
		t.Fatalf("%#v", result.Messages)
	}
	if result.Messages[1].Content == old || len(result.Pointers) != 1 {
		t.Fatalf("not elided: %#v", result.Messages[1])
	}
	if contains(seen, protected) || !contains(seen, old) {
		t.Fatalf("state leaked protected or missed old: %s", seen)
	}
	before := eng.Client.Calls
	page0, err := eng.Expand(result.Pointers[0], 0)
	page1, err2 := eng.Expand(result.Pointers[0], 1)
	if err != nil || err2 != nil || !page0.Found {
		t.Fatal(err, err2)
	}
	joined := append(append([]byte{}, page0.Data...), page1.Data...)
	want := []byte(old)
	if string(page0.Data) != string(want[:eng.Config.PageBytes]) || string(joined) != string(want[:eng.Config.PageBytes*2]) {
		t.Fatalf("pages %q %q", page0.Data, page1.Data)
	}
	if eng.Client.Calls != before {
		t.Fatal("expand called jev")
	}
	missing, err := eng.Expand("e:missing", 0)
	if err != nil || missing.Found || len(missing.Data) != 0 {
		t.Fatalf("%#v %v", missing, err)
	}
}

func TestFallbackLeavesMessages(t *testing.T) {
	eng, _ := newEngine(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}, map[string]string{"timeout_seconds": "0.1", "protected_tail_tokens": "0"})
	messages := []model.Message{{ID: "prefix", Role: "system", Kind: "prefix", Content: "RULES"}, {ID: "old", Role: "tool", Kind: "tool", Content: "OUTPUT"}}
	result := eng.Compact(messages)
	if result.Status != "fallback" || result.Messages[0].Content != "RULES" || result.Messages[1].Content != "OUTPUT" {
		t.Fatalf("%#v", result)
	}
}

func TestFreshWindow(t *testing.T) {
	eng, _ := newEngine(t, answers(nil), nil)
	prefix := model.Message{ID: "prefix", Role: "system", Kind: "prefix", Content: "FROZEN"}
	prose := model.Message{ID: "user", Role: "user", Kind: "prose", Content: "please summarize the whole chat"}
	fit := 0.9
	big := 0.95
	window := eng.FreshWindow([]model.Message{prefix, prose}, []string{"e:abc"}, []model.Passage{{SourceID: "notes", Text: "refund policy", Probability: &fit}}, []string{"ship on Tuesday"})
	joined := ""
	for _, message := range window.Messages {
		joined += message.Content + "\n"
	}
	if window.Messages[0].Content != "FROZEN" || !contains(joined, "refund policy") || !contains(joined, "notes") || !contains(joined, "e:abc") || contains(joined, "please summarize") {
		t.Fatalf("%s", joined)
	}
	if window.Estimator != tokens.EstimatorName || window.Budget != eng.Config.TokenBudget {
		t.Fatalf("%#v", window)
	}
	eng.Config.TokenBudget = tokens.Estimate("FROZEN")
	huge := model.Passage{SourceID: "huge", Text: string(make([]byte, 80)), Probability: &big}
	for i := range huge.Text {
		huge.Text = huge.Text[:i] + "y" + huge.Text[i+1:]
	}
	tiny := eng.FreshWindow([]model.Message{prefix}, nil, []model.Passage{huge}, nil)
	if len(tiny.OverBudget) != 1 || tiny.OverBudget[0] != "huge" || contains(join(tiny.Messages), huge.Text) {
		t.Fatalf("%#v", tiny)
	}
}

func contains(text, part string) bool {
	return len(part) == 0 || (len(text) >= len(part) && (text == part || len(text) > 0 && (stringIndex(text, part) >= 0)))
}

func stringIndex(text, part string) int {
	for i := 0; i+len(part) <= len(text); i++ {
		if text[i:i+len(part)] == part {
			return i
		}
	}
	return -1
}

func join(messages []model.Message) string {
	out := ""
	for _, message := range messages {
		out += message.Content
	}
	return out
}

func TestOversizedTailIsEligible(t *testing.T) {
	huge := strings.Repeat("H", 4000)
	var seen string
	eng, _ := newEngine(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			State     string                     `json:"state"`
			Questions map[string]client.Question `json:"questions"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		seen = body.State
		out := map[string]any{}
		for key := range body.Questions {
			score := 0.9
			if key == "huge" {
				score = 0.1
			}
			out[key] = map[string]any{"noul": score}
		}
		json.NewEncoder(w).Encode(map[string]any{"model_version": "jev-1.13.0", "answers": out})
	}, map[string]string{"protected_tail_tokens": "10"})
	messages := []model.Message{
		{ID: "prefix", Role: "system", Kind: "prefix", Content: "RULES"},
		{ID: "old", Role: "tool", Kind: "tool", Content: "KEEP-THIS"},
		{ID: "huge", Role: "tool", Kind: "tool", Content: huge},
	}
	result := eng.Compact(messages)
	if result.Status != "compacted" || result.Messages[1].Content != "KEEP-THIS" {
		t.Fatalf("%#v", result)
	}
	if result.Messages[2].Content == huge || len(result.Pointers) != 1 || !contains(seen, huge[:32]) {
		t.Fatalf("huge tail stayed protected: %#v", result.Messages[2])
	}
}
