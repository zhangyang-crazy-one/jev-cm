package memory

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"jev-cm/internal/tokens"
)

func TestConversationIngestIgnoresToolsSummariesAndDuplicates(t *testing.T) {
	mem, db, _ := openMemory(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("ingest must not call Jev")
	}, nil)
	turn := Turn{Text: "Refunds close on Tuesday", Role: "assistant", Cwd: "/proj/a", SessionID: "sess-a", SourceID: "sess-a"}
	if got := mem.Ingest(turn); got.Status != "stored" || mem.Client.Calls != 0 {
		t.Fatal(got, mem.Client.Calls)
	}
	if got := mem.Ingest(turn); got.Status != "duplicate" {
		t.Fatal(got)
	}
	if n, _ := db.CountMemory(ConversationCollection, ""); n != 1 {
		t.Fatal(n)
	}
	rows, err := db.MemoryRows(ConversationCollection, "sess-a")
	if err != nil || len(rows) != 1 || rows[0].Text != turn.Text || rows[0].Cwd != "/proj/a" || rows[0].SessionID != "sess-a" || rows[0].Role != "assistant" {
		t.Fatalf("%v %#v", err, rows)
	}
	for _, ignored := range []Turn{
		{Text: "tool bytes", Role: "toolResult", Cwd: "/proj/a", SessionID: "sess-a"},
		{Text: "args", Role: "tool", Cwd: "/proj/a", SessionID: "sess-a"},
		{Text: "   ", Role: "user", Cwd: "/proj/a", SessionID: "sess-a"},
		{Text: "generated recap", Role: "assistant", GeneratedSummary: true, Cwd: "/proj/a", SessionID: "sess-a"},
	} {
		if got := mem.Ingest(ignored); got.Status == "stored" || mem.Client.Calls != 0 {
			t.Fatalf("%#v %#v", ignored, got)
		}
	}
	if n, _ := db.CountMemory(ConversationCollection, ""); n != 1 {
		t.Fatal(n)
	}
}

func TestConversationRecallIsGlobalAndBudgeted(t *testing.T) {
	mem, db, _ := openMemory(t, func(w http.ResponseWriter, r *http.Request) {
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
			score := 0.2
			if strings.Contains(passage.Text, "beta refund") {
				score = 0.8
			}
			if strings.Contains(passage.Text, "huge refund") {
				score = 0.95
			}
			answers[passage.ID] = map[string]any{"noul": score}
		}
		json.NewEncoder(w).Encode(map[string]any{"model_version": "jev-1.13.0", "answers": answers})
	}, nil)
	_ = db
	mem.Ingest(Turn{Text: "alpha refund policy", Role: "user", Cwd: "/proj/a", SessionID: "s1", SourceID: "s1"})
	mem.Ingest(Turn{Text: "beta refund policy", Role: "assistant", Cwd: "/proj/b", SessionID: "s2", SourceID: "s2"})
	before := mem.Client.Calls
	empty := mem.Recall("zzzz-no-such-token", ConversationCollection, InjectBudget)
	if empty.Status != "ok" || len(empty.Passages) != 0 || mem.Client.Calls != before {
		t.Fatalf("empty shortlist called jev: %#v calls %d", empty, mem.Client.Calls)
	}
	ranked := mem.Recall("refund policy", ConversationCollection, InjectBudget)
	section := Section(ranked)
	if !strings.Contains(section, "beta refund policy") || !strings.Contains(section, "cwd:/proj/b") || strings.Contains(section, "alpha refund policy") {
		t.Fatalf("%q", section)
	}
	huge := "huge refund " + strings.Repeat("x", 9000)
	mem.Ingest(Turn{Text: huge, Role: "user", Cwd: "/proj/c", SessionID: "s3", SourceID: "s3"})
	capped := mem.Recall("refund", ConversationCollection, InjectBudget)
	joined := Section(capped)
	if strings.Contains(joined, huge) || len(capped.OverBudget) == 0 {
		t.Fatalf("partial or missing over_budget: %d %#v", len(joined), capped.OverBudget)
	}
	if tokens.Estimate(huge) <= InjectBudget {
		t.Fatal("fixture fits")
	}
	slow, kept, _ := openMemory(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}, map[string]string{"timeout_seconds": "0.1"})
	slow.Ingest(Turn{Text: "gamma refund policy", Role: "user", Cwd: "/proj/d", SessionID: "s4", SourceID: "s4"})
	failed := slow.Recall("refund", ConversationCollection, InjectBudget)
	if Section(failed) != "" {
		t.Fatal(Section(failed))
	}
	if n, _ := kept.CountMemory(ConversationCollection, "s4"); n != 1 {
		t.Fatal(n)
	}
	if len(failed.Passages) == 0 {
		t.Fatal("expected unranked shortlist")
	}
	for _, passage := range failed.Passages {
		if !passage.Unranked || passage.Probability != nil {
			t.Fatalf("%#v", passage)
		}
	}
}

func TestRecallUsesTheEndOfTheCurrentSituation(t *testing.T) {
	mem, _, _ := openMemory(t, func(w http.ResponseWriter, r *http.Request) {
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
			score := 0.2
			if strings.Contains(passage.Text, "beta refund") {
				score = 0.9
			}
			answers[passage.ID] = map[string]any{"noul": score}
		}
		json.NewEncoder(w).Encode(map[string]any{"model_version": "jev-1.13.0", "answers": answers})
	}, nil)
	mem.Ingest(Turn{Text: "beta refund policy", Role: "assistant", Cwd: "/proj/b", SessionID: "s2", SourceID: "s2"})
	mem.Ingest(Turn{Text: "shipping dock schedule", Role: "user", Cwd: "/proj/a", SessionID: "s1", SourceID: "s1"})
	ranked := mem.Recall(strings.Repeat("alpha ", 40)+"refund policy", ConversationCollection, InjectBudget)
	section := Section(ranked)
	if !strings.Contains(section, "beta refund policy") || strings.Contains(section, "shipping dock") {
		t.Fatalf("%q", section)
	}
}
