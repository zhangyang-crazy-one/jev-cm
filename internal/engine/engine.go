package engine

import (
	"strings"

	"jev-cm/internal/client"
	"jev-cm/internal/config"
	"jev-cm/internal/model"
	"jev-cm/internal/store"
	"jev-cm/internal/tokens"
)

const keepInstructions = "High probability means this tool output is still needed verbatim. Low probability means the live window can drop it and keep a pointer to the original."

type Compaction struct {
	Status     string
	Messages   []model.Message
	Pointers   []string
	UsedTokens int
	Budget     int
	Estimator  string
}

type FreshWindow struct {
	Messages   []model.Message
	Injection  []string
	UsedTokens int
	Budget     int
	Estimator  string
	OverBudget []string
}

type Engine struct {
	Config config.Config
	Client *client.Client
	Store  *store.Store
}

func PointerLine(pointer string) string {
	return "[jev-cm: tool output elided from context; original retained. Retrieve with expand pointer=" + pointer + " page=0. Do not rerun the tool.]"
}

func FrozenPrefix(messages []model.Message) []model.Message {
	var prefix []model.Message
	for _, message := range messages {
		if message.Kind != "prefix" {
			break
		}
		prefix = append(prefix, message)
	}
	return prefix
}

func (e *Engine) Compact(messages []model.Message) Compaction {
	original := append([]model.Message(nil), messages...)
	budget := e.Config.TokenBudget
	used := sessionTokens(original)
	base := Compaction{Status: "compacted", Messages: original, UsedTokens: used, Budget: budget, Estimator: tokens.EstimatorName}
	if budget <= 0 || float64(used) < e.Config.PressureRatio*float64(budget) {
		return base
	}
	protected := protectedIDs(original, e.Config.ProtectedTailTokens)
	var eligible []model.Message
	for _, message := range original {
		if message.Kind == "tool" && !protected[message.ID] {
			eligible = append(eligible, message)
		}
	}
	if len(eligible) == 0 {
		return base
	}
	parts := make([]string, len(eligible))
	questions := make(map[string]client.Question, len(eligible))
	for i, message := range eligible {
		parts[i] = "[" + message.ID + "]\n" + message.Content
		questions[message.ID] = client.Question{Type: "noul", Instructions: keepInstructions}
	}
	judged, err := e.Client.Evaluate(strings.Join(parts, "\n\n"), questions)
	if err != nil {
		return Compaction{Status: "fallback", Messages: original, UsedTokens: used, Budget: budget, Estimator: tokens.EstimatorName}
	}
	for _, message := range eligible {
		answer, ok := judged.Answers[message.ID]
		if !ok || answer.Probability == nil {
			return Compaction{Status: "fallback", Messages: original, UsedTokens: used, Budget: budget, Estimator: tokens.EstimatorName}
		}
	}
	rewritten := make([]model.Message, 0, len(original))
	var pointers []string
	for _, message := range original {
		answer, ok := judged.Answers[message.ID]
		if !ok || answer.Probability == nil || *answer.Probability >= e.Config.DropThreshold {
			rewritten = append(rewritten, message)
			continue
		}
		pointer, err := e.Store.PutElision([]byte(message.Content))
		if err != nil {
			return Compaction{Status: "fallback", Messages: original, UsedTokens: used, Budget: budget, Estimator: tokens.EstimatorName}
		}
		pointers = append(pointers, pointer)
		message.Content = PointerLine(pointer)
		rewritten = append(rewritten, message)
	}
	return Compaction{
		Status:     "compacted",
		Messages:   rewritten,
		Pointers:   pointers,
		UsedTokens: sessionTokens(rewritten),
		Budget:     budget,
		Estimator:  tokens.EstimatorName,
	}
}

func (e *Engine) Expand(pointer string, page int) (store.ExpandResult, error) {
	return e.Store.Expand(pointer, page, e.Config.PageBytes)
}

func (e *Engine) FreshWindow(messages []model.Message, pointers []string, passages []model.Passage, pinned []string) FreshWindow {
	var body []model.Message
	for _, message := range FrozenPrefix(messages) {
		body = append(body, message)
	}
	var injection []string
	for i, decision := range pinned {
		body = append(body, model.Message{ID: "pin-" + itoa(i), Role: "user", Kind: "pinned", Content: decision})
	}
	for _, pointer := range pointers {
		line := PointerLine(pointer)
		body = append(body, model.Message{ID: pointer, Role: "system", Kind: "pointer", Content: line})
		injection = append(injection, line)
	}
	ordered := append([]model.Passage(nil), passages...)
	sortPassages(ordered)
	running := sessionTokens(body)
	budget := e.Config.TokenBudget
	blocked := false
	var over []string
	for _, passage := range ordered {
		cost := tokens.Estimate(passage.Text)
		if blocked || running+cost > budget {
			blocked = true
			over = append(over, passage.SourceID)
			continue
		}
		line := "[source:" + passage.SourceID + "]\n" + passage.Text
		body = append(body, model.Message{ID: "mem-" + passage.SourceID, Role: "user", Kind: "memory", Content: line})
		injection = append(injection, line)
		running += cost
	}
	return FreshWindow{
		Messages:   body,
		Injection:  injection,
		UsedTokens: sessionTokens(body),
		Budget:     budget,
		Estimator:  tokens.EstimatorName,
		OverBudget: over,
	}
}

func sessionTokens(messages []model.Message) int {
	total := 0
	for _, message := range messages {
		total += tokens.Estimate(message.Content)
	}
	return total
}

func protectedIDs(messages []model.Message, tail int) map[string]bool {
	protected := map[string]bool{}
	remaining := tail
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.Kind != "tool" {
			continue
		}
		cost := tokens.Estimate(message.Content)
		if remaining <= 0 || cost > remaining {
			break
		}
		protected[message.ID] = true
		remaining -= cost
	}
	return protected
}

func sortPassages(passages []model.Passage) {
	score := func(passage model.Passage) float64 {
		if passage.Probability == nil {
			return 0
		}
		return *passage.Probability
	}
	for i := 1; i < len(passages); i++ {
		for j := i; j > 0; j-- {
			left, right := score(passages[j]), score(passages[j-1])
			if left > right || (left == right && passages[j].SourceID < passages[j-1].SourceID) {
				passages[j], passages[j-1] = passages[j-1], passages[j]
				continue
			}
			break
		}
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
