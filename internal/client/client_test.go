package client

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"jev-cm/internal/config"
	"jev-cm/internal/tokens"
)

func testConfig(url, provider, model string) config.Config {
	file := map[string]string{"base_url": url, "provider": provider}
	if model != "" {
		file["model"] = model
	}
	cfg, err := config.Load(file, func(string) string { return "" })
	if err != nil {
		panic(err)
	}
	return cfg
}

func TestTargetsAuthAndDefaultModel(t *testing.T) {
	direct := &Client{Config: func() config.Config {
		cfg, err := config.Load(map[string]string{"provider": "typesafe"}, func(string) string { return "" })
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}(), HTTP: &http.Client{}, LookupEnv: func(key string) (string, bool) { return "ts_test", key == "TYPESAFE_API_KEY" }}
	direct.HTTP.Transport = roundTrip(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://api.typesafe.ai/v1/systemone" {
			t.Fatalf("url %s", req.URL)
		}
		if req.Header.Get("Authorization") != "Bearer ts_test" {
			t.Fatalf("auth %s", req.Header.Get("Authorization"))
		}
		return jsonResponse(map[string]any{"model_version": "jev-1.13.0", "answers": map[string]any{"q": map[string]any{"noul": 0.4}}}), nil
	})
	if _, err := direct.Evaluate("state", map[string]Question{"q": {Type: "noul", Instructions: "Is it true?"}}); err != nil {
		t.Fatal(err)
	}

	goKey := &Client{Config: func() config.Config {
		cfg, err := config.Load(map[string]string{"provider": "opencode-go"}, func(string) string { return "" })
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}(), HTTP: &http.Client{Transport: roundTrip(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://opencode.ai/zen/v1/systemone" || req.Header.Get("Authorization") != "Bearer go_test" {
			t.Fatalf("go request %s %s", req.URL, req.Header.Get("Authorization"))
		}
		var body map[string]any
		json.NewDecoder(req.Body).Decode(&body)
		if body["model"] != "jev-1.13" {
			t.Fatalf("model %v", body["model"])
		}
		return jsonResponse(map[string]any{"model_version": "jev-1.13.0", "answers": map[string]any{"q": map[string]any{"noul": 0.2}}}), nil
	})}, LookupEnv: func(key string) (string, bool) {
		switch key {
		case "OPENCODE_GO_API_KEY":
			return "go_test", true
		case "OPENCODE_API_KEY":
			return "console_test", true
		default:
			return "", false
		}
	}}
	if _, err := goKey.Evaluate("state", map[string]Question{"q": {Type: "noul", Instructions: "Is it true?"}}); err != nil {
		t.Fatal(err)
	}
	consoleKey := &Client{Config: goKey.Config, HTTP: &http.Client{Transport: roundTrip(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Authorization") != "Bearer console_test" {
			t.Fatalf("fallback auth %s", req.Header.Get("Authorization"))
		}
		return jsonResponse(map[string]any{"answers": map[string]any{"q": map[string]any{"noul": 0.2}}}), nil
	})}, LookupEnv: func(key string) (string, bool) { return "console_test", key == "OPENCODE_API_KEY" }}
	if _, err := consoleKey.Evaluate("state", map[string]Question{"q": {Type: "noul", Instructions: "Is it true?"}}); err != nil {
		t.Fatal(err)
	}
}

func TestGoURLRejected(t *testing.T) {
	_, err := config.Load(map[string]string{"base_url": "https://opencode.ai/zen/go/v1/chat/completions"}, func(string) string { return "" })
	jev, ok := err.(*config.Error)
	if !ok || jev.Kind != "provider" {
		t.Fatalf("got %#v", err)
	}
}

func TestZenAliasUsesGoProvider(t *testing.T) {
	cfg, err := config.Load(map[string]string{"provider": "opencode-zen"}, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != config.ProviderOpenCodeGo || cfg.Endpoint() != "https://opencode.ai/zen/v1/systemone" {
		t.Fatalf("%s %s", cfg.Provider, cfg.Endpoint())
	}
}

func TestParallelQuestionsAndVersion(t *testing.T) {
	var count int
	var model string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		model, _ = body["model"].(string)
		json.NewEncoder(w).Encode(map[string]any{
			"model_version": "jev-1.13.0",
			"answers": map[string]any{
				"flag":  map[string]any{"noul": 0.4},
				"team":  map[string]any{"choice": "returns", "probability": 0.8, "probabilities": map[string]any{"returns": 0.8}, "confidence": 0.7},
				"level": map[string]any{"score": 3, "probability": 0.6},
			},
		})
	}))
	defer server.Close()
	jev := &Client{Config: testConfig(server.URL, "typesafe", "jev-latest"), LookupEnv: key("TYPESAFE_API_KEY", "ts_test")}
	result, err := jev.Evaluate("state", map[string]Question{
		"flag":  {Type: "noul", Instructions: "Yes or no?"},
		"team":  {Type: "choice", Instructions: "Which team?", Options: []string{"returns", "sales"}},
		"level": {Type: "score", Instructions: "How severe?"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || len(result.Answers) != 3 || result.RequestedModel != "jev-latest" || result.ModelVersion != "jev-1.13.0" || result.Estimator != tokens.EstimatorName || model != "jev-latest" {
		t.Fatalf("count %d model %s result %#v", count, model, result)
	}
}

func TestMissingInstructionsDoesNotSend(t *testing.T) {
	jev := &Client{Config: testConfig("http://127.0.0.1:1", "typesafe", ""), HTTP: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		t.Fatal("sent")
		return nil, nil
	})}, LookupEnv: key("TYPESAFE_API_KEY", "ts_test")}
	_, err := jev.Evaluate("state", map[string]Question{"q": {Type: "noul"}})
	jevErr, ok := err.(*Error)
	if !ok || jevErr.Kind != "validation" || jev.Calls != 0 {
		t.Fatal(err)
	}
}

func TestSplitAndOversized(t *testing.T) {
	var keys []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Questions map[string]Question `json:"questions"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		answers := map[string]any{}
		for key := range body.Questions {
			keys = append(keys, key)
			answers[key] = map[string]any{"noul": 0.5}
		}
		json.NewEncoder(w).Encode(map[string]any{"model_version": "jev-1.13.0", "answers": answers})
	}))
	defer server.Close()
	cfg := testConfig(server.URL, "typesafe", "")
	if cfg.TokenLimit != 32000 {
		t.Fatalf("limit %d", cfg.TokenLimit)
	}
	jev := &Client{Config: cfg, LookupEnv: key("TYPESAFE_API_KEY", "ts_test")}
	big := strings.Repeat("x", 100000)
	result, err := jev.Evaluate("s", map[string]Question{
		"a": {Type: "noul", Instructions: big},
		"b": {Type: "noul", Instructions: big + "y"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || keys[0] == keys[1] || len(result.Answers) != 2 {
		t.Fatalf("keys %v", keys)
	}
	calls := jev.Calls
	_, err = jev.Evaluate("s", map[string]Question{"huge": {Type: "noul", Instructions: strings.Repeat("z", 200000)}})
	jevErr, ok := err.(*Error)
	if !ok || jevErr.Kind != "budget" || len(jevErr.Keys) != 1 || jevErr.Keys[0] != "huge" || jev.Calls != calls {
		t.Fatalf("%#v calls %d/%d", err, jev.Calls, calls)
	}
}

func TestMalformedTimeoutAndMissingKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Questions map[string]Question `json:"questions"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body.Questions["slow"]; ok {
			time.Sleep(300 * time.Millisecond)
		}
		json.NewEncoder(w).Encode(map[string]any{"answers": map[string]any{"bad": map[string]any{}}})
	}))
	defer server.Close()
	cfg := testConfig(server.URL, "typesafe", "")
	cfg.Timeout = 100 * time.Millisecond
	jev := &Client{Config: cfg, LookupEnv: key("TYPESAFE_API_KEY", "ts_test")}
	_, err := jev.Evaluate("state", map[string]Question{"bad": {Type: "noul", Instructions: "Check"}})
	jevErr, ok := err.(*Error)
	if !ok || jevErr.Kind != "parse" || jevErr.Provider != "typesafe" || len(jevErr.Keys) == 0 {
		t.Fatal(err)
	}
	_, err = jev.Evaluate("state", map[string]Question{"slow": {Type: "noul", Instructions: "Check"}})
	jevErr, ok = err.(*Error)
	if !ok || jevErr.Kind != "timeout" {
		t.Fatal(err)
	}
	offline := &Client{Config: testConfig(server.URL, "typesafe", ""), LookupEnv: func(string) (string, bool) { return "", false }}
	_, err = offline.Evaluate("state", map[string]Question{"q": {Type: "noul", Instructions: "Check"}})
	jevErr, ok = err.(*Error)
	if !ok || jevErr.Kind != "authentication" || offline.Calls != 0 {
		t.Fatal(err)
	}
}

func key(name, value string) func(string) (string, bool) {
	return func(got string) (string, bool) {
		if got == name {
			return value, true
		}
		return "", false
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (fn roundTrip) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func jsonResponse(body any) *http.Response {
	raw, _ := json.Marshal(body)
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw))), Header: make(http.Header)}
}
