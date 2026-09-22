package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"jev-cm/internal/config"
	"jev-cm/internal/tokens"
)

type Question struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
	Options      any    `json:"options,omitempty"`
	Legend       any    `json:"legend,omitempty"`
}

type Answer struct {
	Key           string
	Type          string
	Probability   *float64
	Choice        string
	Score         *float64
	Probabilities map[string]float64
	Confidence    *float64
}

type Result struct {
	Provider       string
	RequestedModel string
	ModelVersion   string
	Estimator      string
	Answers        map[string]Answer
}

type Error struct {
	Kind     string
	Provider string
	Keys     []string
	Msg      string
}

func (e *Error) Error() string { return e.Msg }

type Client struct {
	Config    config.Config
	HTTP      *http.Client
	LookupEnv func(string) (string, bool)
	Calls     int
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	timeout := c.Config.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &http.Client{Timeout: timeout}
}

func (c *Client) apiKey() string {
	lookup := c.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	for _, name := range c.Config.APIKeyEnvs() {
		value, _ := lookup(name)
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (c *Client) Evaluate(state any, questions map[string]Question) (Result, error) {
	if err := validateState(state); err != nil {
		return Result{}, err
	}
	if len(questions) == 0 {
		return Result{}, &Error{Kind: "validation", Provider: c.Config.Provider, Msg: "questions must be a non-empty object"}
	}
	keys := make([]string, 0, len(questions))
	cleaned := make(map[string]Question, len(questions))
	for key, question := range questions {
		normalized, err := validateQuestion(key, question)
		if err != nil {
			return Result{}, err
		}
		cleaned[key] = normalized
		keys = append(keys, key)
	}
	if c.apiKey() == "" {
		return Result{}, &Error{Kind: "authentication", Provider: c.Config.Provider, Keys: keys, Msg: c.Config.APIKeyEnv() + " is not set"}
	}
	batches, oversized := partition(state, cleaned, c.Config.TokenLimit)
	if len(oversized) > 0 {
		return Result{}, &Error{Kind: "budget", Provider: c.Config.Provider, Keys: oversized, Msg: "a question plus its state exceeds the 32K token cap"}
	}
	merged := map[string]Answer{}
	version := ""
	for _, batch := range batches {
		gotVersion, answers, err := c.post(state, batch)
		if err != nil {
			var jev *Error
			if errors.As(err, &jev) && jev.Provider == "" {
				jev.Provider = c.Config.Provider
			}
			return Result{}, err
		}
		if version == "" {
			version = gotVersion
		}
		for key, answer := range answers {
			if _, exists := merged[key]; exists {
				return Result{}, &Error{Kind: "parse", Provider: c.Config.Provider, Keys: []string{key}, Msg: "duplicate answer key"}
			}
			merged[key] = answer
		}
	}
	var missing []string
	for key := range cleaned {
		if _, ok := merged[key]; !ok {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return Result{}, &Error{Kind: "parse", Provider: c.Config.Provider, Keys: missing, Msg: "response omitted question keys"}
	}
	return Result{
		Provider:       c.Config.Provider,
		RequestedModel: c.Config.RequestedModel(),
		ModelVersion:   version,
		Estimator:      tokens.EstimatorName,
		Answers:        merged,
	}, nil
}

func validateState(state any) error {
	switch value := state.(type) {
	case string:
		return nil
	case map[string]any:
		return nil
	case []string:
		return nil
	case []any:
		for _, item := range value {
			if _, ok := item.(string); !ok {
				return &Error{Kind: "validation", Msg: "state must be a string, a JSON object, or an array of strings"}
			}
		}
		return nil
	default:
		return &Error{Kind: "validation", Msg: "state must be a string, a JSON object, or an array of strings"}
	}
}

func validateQuestion(key string, question Question) (Question, error) {
	switch question.Type {
	case "noul", "choice", "score":
	default:
		return Question{}, &Error{Kind: "validation", Keys: []string{key}, Msg: "question " + key + " has an unknown type"}
	}
	if strings.TrimSpace(question.Instructions) == "" {
		return Question{}, &Error{Kind: "validation", Keys: []string{key}, Msg: "question " + key + " requires instructions"}
	}
	return question, nil
}

func payloadTokens(state any, questions map[string]Question) int {
	raw, err := json.Marshal(map[string]any{"questions": questions, "state": state})
	if err != nil {
		return tokens.Limit + 1
	}
	return tokens.Estimate(string(raw))
}

func partition(state any, questions map[string]Question, limit int) ([]map[string]Question, []string) {
	if limit <= 0 {
		limit = tokens.Limit
	}
	keys := make([]string, 0, len(questions))
	for key := range questions {
		keys = append(keys, key)
	}
	sortStrings(keys)
	var batches []map[string]Question
	current := map[string]Question{}
	var oversized []string
	for _, key := range keys {
		question := questions[key]
		alone := payloadTokens(state, map[string]Question{key: question})
		if alone > limit {
			oversized = append(oversized, key)
			continue
		}
		trial := copyQuestions(current)
		trial[key] = question
		if len(current) > 0 && payloadTokens(state, trial) > limit {
			batches = append(batches, current)
			current = map[string]Question{key: question}
			continue
		}
		current = trial
	}
	if len(current) > 0 {
		batches = append(batches, current)
	}
	return batches, oversized
}

func copyQuestions(in map[string]Question) map[string]Question {
	out := make(map[string]Question, len(in)+1)
	for key, value := range in {
		out[key] = value
	}
	return out
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

func (c *Client) post(state any, questions map[string]Question) (string, map[string]Answer, error) {
	keys := make([]string, 0, len(questions))
	for key := range questions {
		keys = append(keys, key)
	}
	body := map[string]any{
		"model":     c.Config.RequestedModel(),
		"state":     state,
		"questions": questions,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", nil, &Error{Kind: "validation", Provider: c.Config.Provider, Keys: keys, Msg: err.Error()}
	}
	req, err := http.NewRequest(http.MethodPost, c.Config.Endpoint(), bytes.NewReader(raw))
	if err != nil {
		return "", nil, &Error{Kind: "provider", Provider: c.Config.Provider, Keys: keys, Msg: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "jev-cm/0.1.0")
	c.Calls++
	resp, err := c.httpClient().Do(req)
	if err != nil {
		kind := "http"
		if isTimeout(err) {
			kind = "timeout"
		}
		return "", nil, &Error{Kind: kind, Provider: c.Config.Provider, Keys: keys, Msg: err.Error()}
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, &Error{Kind: "http", Provider: c.Config.Provider, Keys: keys, Msg: err.Error()}
	}
	if resp.StatusCode != http.StatusOK {
		return "", nil, &Error{Kind: "http", Provider: c.Config.Provider, Keys: keys, Msg: resp.Status}
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return "", nil, &Error{Kind: "parse", Provider: c.Config.Provider, Keys: keys, Msg: "System One returned a body that is not JSON"}
	}
	answers, err := parseAnswers(questions, decoded)
	if err != nil {
		var jev *Error
		if errors.As(err, &jev) {
			jev.Provider = c.Config.Provider
		}
		return "", nil, err
	}
	return modelVersion(decoded), answers, nil
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Timeout() {
		return true
	}
	return false
}

func modelVersion(decoded map[string]any) string {
	for _, field := range []string{"model_version", "modelVersion", "version", "model"} {
		if value, ok := decoded[field].(string); ok && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func answerMap(decoded map[string]any) map[string]any {
	for _, field := range []string{"answers", "questions"} {
		if value, ok := decoded[field].(map[string]any); ok {
			return value
		}
	}
	return map[string]any{}
}

func parseAnswers(questions map[string]Question, decoded map[string]any) (map[string]Answer, error) {
	found := answerMap(decoded)
	parsed := make(map[string]Answer, len(questions))
	for key, question := range questions {
		raw, ok := found[key].(map[string]any)
		if !ok {
			return nil, &Error{Kind: "parse", Keys: []string{key}, Msg: "missing answer for " + key}
		}
		answer, err := parseOne(key, question.Type, raw)
		if err != nil {
			return nil, err
		}
		parsed[key] = answer
	}
	return parsed, nil
}

func parseOne(key, qtype string, raw map[string]any) (Answer, error) {
	answer := Answer{Key: key, Type: qtype, Probabilities: floatMap(raw["probabilities"]), Confidence: floatPtr(raw["confidence"])}
	switch qtype {
	case "noul":
		value, ok := firstFloat(raw["noul"], raw["probability"])
		if !ok {
			return Answer{}, &Error{Kind: "parse", Keys: []string{key}, Msg: "noul " + key + " is missing a probability"}
		}
		answer.Probability = &value
	case "choice":
		choice, _ := raw["choice"].(string)
		if choice == "" {
			return Answer{}, &Error{Kind: "parse", Keys: []string{key}, Msg: "choice " + key + " is missing a label"}
		}
		answer.Choice = choice
		if value, ok := firstFloat(raw["probability"]); ok {
			answer.Probability = &value
		}
	default:
		value, ok := firstFloat(raw["score"])
		if !ok {
			return Answer{}, &Error{Kind: "parse", Keys: []string{key}, Msg: "score " + key + " is missing a score"}
		}
		answer.Score = &value
		if probability, ok := firstFloat(raw["probability"]); ok {
			answer.Probability = &probability
		}
	}
	return answer, nil
}

func firstFloat(values ...any) (float64, bool) {
	for _, value := range values {
		switch typed := value.(type) {
		case float64:
			return typed, true
		case json.Number:
			parsed, err := typed.Float64()
			if err == nil {
				return parsed, true
			}
		}
	}
	return 0, false
}

func floatPtr(value any) *float64 {
	parsed, ok := firstFloat(value)
	if !ok {
		return nil
	}
	return &parsed
}

func floatMap(value any) map[string]float64 {
	raw, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]float64, len(raw))
	for key, item := range raw {
		if parsed, ok := firstFloat(item); ok {
			out[key] = parsed
		}
	}
	return out
}
