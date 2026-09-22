package client

import (
	"os"
	"testing"

	"jev-cm/internal/config"
)

func TestLiveSystemOne(t *testing.T) {
	if os.Getenv("JEV_CM_LIVE") == "" {
		t.Skip("set JEV_CM_LIVE=1 to call the configured Jev provider")
	}
	saved, err := config.ReadSettings(config.SettingsPath(os.Getenv))
	if err != nil {
		t.Fatal(err)
	}
	lookup := config.MergeLookup(saved, os.Getenv)
	cfg, err := config.Load(nil, lookup)
	if err != nil {
		t.Fatal(err)
	}
	jev := &Client{Config: cfg, LookupEnv: func(key string) (string, bool) {
		value := lookup(key)
		if value == "" {
			return "", false
		}
		return value, true
	}}
	result, err := jev.Evaluate("The refund window is 30 days for unopened items.", map[string]Question{
		"keep": {Type: "noul", Instructions: "High probability means this text states a refund deadline."},
	})
	if err != nil {
		t.Fatal(err)
	}
	answer, ok := result.Answers["keep"]
	if !ok || answer.Probability == nil || result.ModelVersion == "" || jev.Calls != 1 {
		t.Fatalf("provider=%s model=%s version=%s calls=%d answer=%v", result.Provider, result.RequestedModel, result.ModelVersion, jev.Calls, answer)
	}
	t.Logf("provider=%s endpoint=%s requested=%s version=%s noul=%.3f", result.Provider, cfg.Endpoint(), result.RequestedModel, result.ModelVersion, *answer.Probability)
}
