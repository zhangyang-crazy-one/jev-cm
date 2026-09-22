package service

import (
	"jev-cm/internal/client"
	"jev-cm/internal/config"
	"jev-cm/internal/engine"
	"jev-cm/internal/memory"
	"jev-cm/internal/model"
	"jev-cm/internal/store"
)

type Plan struct {
	Status     string   `json:"status"`
	Context    []string `json:"context"`
	UsedTokens int      `json:"used_tokens"`
	Budget     int      `json:"budget"`
	Estimator  string   `json:"estimator"`
	OverBudget []string `json:"over_budget,omitempty"`
}

func Normalize(raw []any) []model.Message {
	var messages []model.Message
	for index, item := range raw {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if _, hasKind := obj["kind"]; hasKind {
			if _, hasContent := obj["content"]; hasContent {
				messages = append(messages, model.Message{
					ID:      stringField(obj, "id", itoa(index)),
					Role:    stringField(obj, "role", "user"),
					Kind:    stringField(obj, "kind", "prose"),
					Content: stringField(obj, "content", ""),
				})
				continue
			}
		}
		info, _ := obj["info"].(map[string]any)
		if info == nil {
			info = obj
		}
		role := stringField(info, "role", stringField(obj, "role", "user"))
		messageID := stringField(info, "id", stringField(obj, "id", itoa(index)))
		parts, _ := obj["parts"].([]any)
		if parts == nil {
			text := stringField(obj, "content", stringField(obj, "text", ""))
			kind := "prose"
			if role == "system" {
				kind = "prefix"
			}
			messages = append(messages, model.Message{ID: messageID, Role: role, Kind: kind, Content: text})
			continue
		}
		for partIndex, part := range parts {
			partObj, ok := part.(map[string]any)
			if !ok {
				continue
			}
			partType := stringField(partObj, "type", "text")
			if partType == "tool" || partType == "tool-result" || partType == "tool_result" {
				content := stringField(partObj, "output", stringField(partObj, "content", stringField(partObj, "text", "")))
				messages = append(messages, model.Message{
					ID:      stringField(partObj, "callID", stringField(partObj, "id", messageID+"-"+itoa(partIndex))),
					Role:    "tool",
					Kind:    "tool",
					Content: content,
				})
				continue
			}
			text := stringField(partObj, "text", stringField(partObj, "content", ""))
			kind := "prose"
			if role == "system" {
				kind = "prefix"
			}
			messages = append(messages, model.Message{ID: messageID + "-" + itoa(partIndex), Role: role, Kind: kind, Content: text})
		}
	}
	return messages
}

func Prepare(payload map[string]any, cfg config.Config, jev *client.Client) (Plan, error) {
	raw, _ := payload["messages"].([]any)
	if raw == nil {
		if nested, ok := payload["data"].([]any); ok {
			raw = nested
		}
	}
	messages := Normalize(raw)
	opened, err := store.Open(cfg.SQLitePath)
	if err != nil {
		return Plan{}, err
	}
	defer opened.Close()
	if jev == nil {
		jev = &client.Client{Config: cfg}
	}
	eng := engine.Engine{Config: cfg, Client: jev, Store: opened}
	compacted := eng.Compact(messages)
	if compacted.Status == "fallback" {
		return Plan{Status: "fallback", UsedTokens: compacted.UsedTokens, Budget: compacted.Budget, Estimator: compacted.Estimator}, nil
	}
	mem := memory.Memory{Config: cfg, Client: jev, Store: opened}
	query := lastUserText(messages)
	var selected []model.Passage
	for _, collection := range []string{"default", memory.ConversationCollection} {
		budget := cfg.TokenBudget
		if collection == memory.ConversationCollection {
			budget = memory.InjectBudget
		}
		recalled := mem.Recall(query, collection, budget)
		for _, passage := range recalled.Passages {
			if !passage.Unranked {
				selected = append(selected, passage)
			}
		}
	}
	window := eng.FreshWindow(messages, compacted.Pointers, selected, nil)
	return Plan{
		Status:     "compacted",
		Context:    window.Injection,
		UsedTokens: window.UsedTokens,
		Budget:     window.Budget,
		Estimator:  window.Estimator,
		OverBudget: window.OverBudget,
	}, nil
}

func lastUserText(messages []model.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.Role == "user" && message.Kind == "prose" && message.Content != "" {
			return message.Content
		}
	}
	return ""
}

func stringField(obj map[string]any, key, fallback string) string {
	value, ok := obj[key]
	if !ok || value == nil {
		return fallback
	}
	text, ok := value.(string)
	if !ok || text == "" {
		return fallback
	}
	return text
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
