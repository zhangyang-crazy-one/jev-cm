package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"jev-cm/internal/tokens"
)

const (
	ProviderTypesafe   = "typesafe"
	ProviderOpenCodeGo = "opencode-go"
)

var (
	defaultURLs = map[string]string{
		ProviderTypesafe:   "https://api.typesafe.ai/v1/systemone",
		ProviderOpenCodeGo: "https://opencode.ai/zen/v1/systemone",
	}
	defaultModels = map[string]string{
		ProviderTypesafe:   "jev-1.13.0",
		ProviderOpenCodeGo: "jev-1.13",
	}
	allowedModels = map[string]map[string]struct{}{
		ProviderTypesafe:   {"jev-1.13.0": {}, "jev-latest": {}},
		ProviderOpenCodeGo: {"jev-1.13": {}, "jev-1.13-free": {}},
	}
	apiKeyEnvs = map[string][]string{
		ProviderTypesafe:   {"TYPESAFE_API_KEY"},
		ProviderOpenCodeGo: {"OPENCODE_GO_API_KEY", "OPENCODE_API_KEY"},
	}
)

type Error struct {
	Kind     string
	Provider string
	Keys     []string
	Msg      string
}

func (e *Error) Error() string { return e.Msg }

type Config struct {
	Provider            string
	Model               string
	DropThreshold       float64
	RememberThreshold   float64
	RecallCutoff        float64
	ProtectedTailTokens int
	PressureRatio       float64
	TokenBudget         int
	SQLitePath          string
	PageBytes           int
	Timeout             time.Duration
	ShortlistLimit      int
	BaseURL             string
	TokenLimit          int
}

func (c Config) Endpoint() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return defaultURLs[c.Provider]
}

func (c Config) RequestedModel() string {
	if c.Model != "" {
		return c.Model
	}
	return defaultModels[c.Provider]
}

func (c Config) APIKeyEnvs() []string { return apiKeyEnvs[c.Provider] }

func (c Config) APIKeyEnv() string { return strings.Join(c.APIKeyEnvs(), " or ") }

func IsOpenCodeGoChatURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	path := strings.ToLower(parsed.Path)
	return strings.Contains(path, "chat/completions") || strings.Contains(path, "/zen/go")
}

type Lookup func(string) string

func Load(file map[string]string, lookup Lookup) (Config, error) {
	if lookup == nil {
		lookup = os.Getenv
	}
	get := func(key, envKey, fallback string) string {
		if file != nil {
			if value, ok := file[key]; ok && value != "" {
				return value
			}
		}
		if envKey != "" {
			if value := lookup(envKey); value != "" {
				return value
			}
		}
		return fallback
	}
	provider := get("provider", "JEV_CM_PROVIDER", ProviderTypesafe)
	if provider == "opencode-zen" {
		provider = ProviderOpenCodeGo
	}
	if provider != ProviderTypesafe && provider != ProviderOpenCodeGo {
		return Config{}, &Error{Kind: "provider", Msg: "unknown provider " + provider}
	}
	model := get("model", "JEV_CM_MODEL", "")
	if model != "" {
		if _, ok := allowedModels[provider][model]; !ok {
			return Config{}, &Error{Kind: "provider", Msg: fmt.Sprintf("model %q is not valid for %s", model, provider)}
		}
	}
	baseURL := get("base_url", "JEV_CM_BASE_URL", "")
	if baseURL != "" && IsOpenCodeGoChatURL(baseURL) {
		return Config{}, &Error{Kind: "provider", Msg: "OpenCode Go chat-completions URLs cannot host System One requests"}
	}
	drop := parseFloat(get("drop_threshold", "JEV_CM_DROP_THRESHOLD", "0.25"), 0.25)
	remember := parseFloat(get("remember_threshold", "JEV_CM_REMEMBER_THRESHOLD", "0.75"), 0.75)
	cfg := Config{
		Provider:            provider,
		Model:               model,
		DropThreshold:       drop,
		RememberThreshold:   remember,
		RecallCutoff:        parseFloat(get("recall_cutoff", "JEV_CM_RECALL_CUTOFF", "0.5"), 0.5),
		ProtectedTailTokens: parseInt(get("protected_tail_tokens", "JEV_CM_PROTECTED_TAIL_TOKENS", "12000"), 12000),
		PressureRatio:       parseFloat(get("pressure_ratio", "JEV_CM_PRESSURE_RATIO", "0.65"), 0.65),
		TokenBudget:         parseInt(get("token_budget", "JEV_CM_TOKEN_BUDGET", strconv.Itoa(tokens.Limit)), tokens.Limit),
		SQLitePath:          get("sqlite_path", "JEV_CM_SQLITE", DefaultSQLitePath(lookup)),
		PageBytes:           parseInt(get("page_bytes", "JEV_CM_PAGE_BYTES", "4096"), 4096),
		Timeout:             time.Duration(parseFloat(get("timeout_seconds", "JEV_CM_TIMEOUT", "30"), 30) * float64(time.Second)),
		ShortlistLimit:      parseInt(get("shortlist_limit", "JEV_CM_SHORTLIST", "20"), 20),
		BaseURL:             baseURL,
		TokenLimit:          parseInt(get("token_limit", "", strconv.Itoa(tokens.Limit)), tokens.Limit),
	}
	if cfg.DropThreshold < 0 || cfg.DropThreshold > 1 || cfg.RememberThreshold < 0 || cfg.RememberThreshold > 1 {
		return Config{}, &Error{Kind: "provider", Msg: "thresholds must be between 0 and 1"}
	}
	return cfg, nil
}

func parseFloat(value string, fallback float64) float64 {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func parseInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func DefaultSQLitePath(lookup Lookup) string {
	if lookup != nil {
		if path := strings.TrimSpace(lookup("JEV_CM_SQLITE")); path != "" {
			return path
		}
		if dir := strings.TrimSpace(lookup("PI_CODING_AGENT_DIR")); dir != "" {
			return filepath.Join(dir, "jev-cm.sqlite")
		}
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "jev-cm.sqlite"
	}
	return filepath.Join(home, ".pi", "agent", "jev-cm.sqlite")
}
