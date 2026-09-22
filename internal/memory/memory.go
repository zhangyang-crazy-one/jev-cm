package memory

import (
	"strings"

	"jev-cm/internal/client"
	"jev-cm/internal/config"
	"jev-cm/internal/model"
	"jev-cm/internal/store"
	"jev-cm/internal/tokens"
)

const rememberInstructions = "High probability means this entry is durable, non-obvious, and useful on a later turn. Low probability means it is filler, a progress log, or a completed-work note that should not be stored."
const recallInstructions = "High probability means this passage materially helps with the current request. Low probability means it does not."
const linkInstructions = "High probability means the new passage and this older passage belong to the same ongoing thread of work. Low probability means they are unrelated."
const linkSeedLimit = 8

type WriteResult struct {
	Status      string
	Probability *float64
	Reason      string
	ID          int64
}

type RecallResult struct {
	Status     string
	Passages   []model.Passage
	OverBudget []string
	Estimator  string
	UsedTokens int
	Budget     int
}

type Memory struct {
	Config config.Config
	Client *client.Client
	Store  *store.Store
}

const ConversationCollection = "conversation"
const InjectBudget = 2000

type Turn struct {
	Text             string
	Role             string
	Cwd              string
	SessionID        string
	SourceID         string
	GeneratedSummary bool
}

func (m *Memory) Remember(text, sourceID, collection string, generatedSummary bool) WriteResult {
	if collection == "" {
		collection = "default"
	}
	if generatedSummary {
		return WriteResult{Status: "refused", Reason: "summary"}
	}
	if collection == ConversationCollection {
		return m.Ingest(Turn{Text: text, Role: "user", SourceID: sourceID, GeneratedSummary: false})
	}
	result, err := m.Client.Evaluate(text, map[string]client.Question{
		"worth_remembering": {Type: "noul", Instructions: rememberInstructions},
	})
	if err != nil {
		return WriteResult{Status: "unavailable"}
	}
	answer := result.Answers["worth_remembering"]
	if answer.Probability == nil || *answer.Probability < m.Config.RememberThreshold {
		return WriteResult{Status: "refused", Probability: answer.Probability, Reason: "below_threshold"}
	}
	if err := m.Store.InsertMemory(collection, sourceID, text, *answer.Probability, result.ModelVersion); err != nil {
		return WriteResult{Status: "unavailable", Reason: "store"}
	}
	return WriteResult{Status: "stored", Probability: answer.Probability}
}

func (m *Memory) ImportSource(collection, sourceID string, texts []string) error {
	entries := make([]store.Entry, len(texts))
	for i, text := range texts {
		entries[i] = store.Entry{Text: text}
	}
	return m.Store.ReplaceSource(collection, sourceID, entries)
}

func (m *Memory) Recall(request, collection string, budget int) RecallResult {
	if collection == "" {
		collection = "default"
	}
	if budget <= 0 {
		budget = m.Config.TokenBudget
	}
	rows, err := m.Store.Shortlist(collection, request, m.Config.ShortlistLimit)
	if err != nil || len(rows) == 0 {
		return RecallResult{Status: "ok", Budget: budget, Estimator: tokens.EstimatorName}
	}
	if collection == ConversationCollection {
		seedIDs := make([]int64, len(rows))
		for i, row := range rows {
			seedIDs[i] = row.ID
		}
		if neighbors, err := m.Store.Neighborhood(collection, seedIDs, m.Config.ShortlistLimit); err == nil && len(neighbors) > 0 {
			rows = neighbors
		}
	}
	questions := make(map[string]client.Question, len(rows))
	passages := make([]any, len(rows))
	for i, row := range rows {
		key := "rel_" + itoa(i)
		questions[key] = client.Question{Type: "noul", Instructions: recallInstructions}
		passages[i] = map[string]string{"id": key, "text": row.Text}
	}
	judged, err := m.Client.Evaluate(map[string]any{"request": request, "passages": passages}, questions)
	if err != nil {
		out := make([]model.Passage, len(rows))
		for i, row := range rows {
			out[i] = model.Passage{SourceID: row.SourceID, Text: row.Text, SHA256: row.SHA256, Unranked: true}
		}
		return RecallResult{Status: "ok", Passages: out, Estimator: tokens.EstimatorName, Budget: budget}
	}
	type ranked struct {
		probability float64
		index       int
		row         store.Row
	}
	order := make([]ranked, len(rows))
	for i, row := range rows {
		probability := 0.0
		if answer, ok := judged.Answers["rel_"+itoa(i)]; ok && answer.Probability != nil {
			probability = *answer.Probability
		}
		order[i] = ranked{probability: probability, index: i, row: row}
	}
	for i := 1; i < len(order); i++ {
		for j := i; j > 0; j-- {
			if order[j].probability > order[j-1].probability || (order[j].probability == order[j-1].probability && order[j].index < order[j-1].index) {
				order[j], order[j-1] = order[j-1], order[j]
				continue
			}
			break
		}
	}
	var selected []model.Passage
	var over []string
	used := 0
	blocked := false
	for _, item := range order {
		if item.probability < m.Config.RecallCutoff {
			continue
		}
		body := item.row.Text
		if collection == ConversationCollection {
			body = labelPassage(item.row)
		}
		cost := tokens.Estimate(body)
		if blocked || used+cost > budget {
			blocked = true
			over = append(over, item.row.SourceID)
			continue
		}
		probability := item.probability
		selected = append(selected, model.Passage{
			SourceID:    item.row.SourceID,
			Text:        body,
			SHA256:      store.HashText(item.row.Text),
			Probability: &probability,
			Unranked:    false,
		})
		used += cost
	}
	return RecallResult{
		Status:     "ok",
		Passages:   selected,
		OverBudget: over,
		Estimator:  tokens.EstimatorName,
		UsedTokens: used,
		Budget:     budget,
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for value > 0 {
		i--
		digits[i] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[i:])
}

func (m *Memory) Ingest(turn Turn) WriteResult {
	if turn.GeneratedSummary || isGeneratedSummary(turn.Text) {
		return WriteResult{Status: "refused", Reason: "summary"}
	}
	role := turn.Role
	if role == "" {
		role = "user"
	}
	if ignoredRole(role) || strings.TrimSpace(turn.Text) == "" {
		return WriteResult{Status: "ignored", Reason: "ignored"}
	}
	if role != "user" && role != "assistant" {
		return WriteResult{Status: "ignored", Reason: "ignored"}
	}
	sourceID := turn.SourceID
	if sourceID == "" {
		sourceID = turn.SessionID
	}
	if sourceID == "" {
		sourceID = "manual"
	}
	id, inserted, err := m.Store.InsertIfNew(ConversationCollection, sourceID, turn.Cwd, turn.SessionID, role, turn.Text)
	if err != nil {
		return WriteResult{Status: "unavailable", Reason: "store"}
	}
	if !inserted {
		return WriteResult{Status: "duplicate"}
	}
	return WriteResult{Status: "stored", ID: id}
}

func (m *Memory) LinkNew(id int64, text string) {
	if m.Client == nil || id == 0 {
		return
	}
	rows, err := m.Store.Shortlist(ConversationCollection, text, linkSeedLimit+1)
	if err != nil || len(rows) == 0 {
		return
	}
	seeds := make([]store.Row, 0, linkSeedLimit)
	for _, row := range rows {
		if row.ID == id {
			continue
		}
		seeds = append(seeds, row)
		if len(seeds) == linkSeedLimit {
			break
		}
	}
	if len(seeds) == 0 {
		return
	}
	parts := make([]string, len(seeds))
	questions := make(map[string]client.Question, len(seeds))
	for i, row := range seeds {
		key := "link_" + itoa(i)
		parts[i] = "[" + key + "]\n" + row.Text
		questions[key] = client.Question{Type: "noul", Instructions: linkInstructions}
	}
	judged, err := m.Client.Evaluate(strings.Join(parts, "\n\n"), questions)
	if err != nil {
		return
	}
	for i, row := range seeds {
		answer, ok := judged.Answers["link_"+itoa(i)]
		if !ok || answer.Probability == nil || *answer.Probability < m.Config.RecallCutoff {
			continue
		}
		_ = m.Store.InsertEdge(id, row.ID, "related", *answer.Probability)
	}
}

func Section(result RecallResult) string {
	var parts []string
	for _, passage := range result.Passages {
		if passage.Unranked || passage.Text == "" {
			continue
		}
		parts = append(parts, passage.Text)
	}
	return strings.Join(parts, "\n\n")
}

func labelPassage(row store.Row) string {
	return "[session:" + row.SessionID + " cwd:" + row.Cwd + "]\n" + row.Text
}

func ignoredRole(role string) bool {
	switch role {
	case "tool", "toolResult", "tool_result", "bashExecution":
		return true
	default:
		return false
	}
}

func isGeneratedSummary(text string) bool {
	trimmed := strings.TrimSpace(text)
	return strings.HasPrefix(trimmed, "[jev-cm: fresh window")
}
