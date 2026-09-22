package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func SettingsPath(lookup Lookup) string {
	if lookup != nil {
		if path := strings.TrimSpace(lookup("JEV_CM_CONFIG")); path != "" {
			return path
		}
		if dir := strings.TrimSpace(lookup("PI_CODING_AGENT_DIR")); dir != "" {
			return filepath.Join(dir, "jev-cm.json")
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".pi", "agent", "jev-cm.json")
}

func ReadSettings(path string) (map[string]string, error) {
	if path == "" {
		return map[string]string{}, nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var body struct {
		Provider   string `json:"provider"`
		Model      string `json:"model"`
		Typesafe   string `json:"typesafe_api_key"`
		OpenCodeGo string `json:"opencode_go_api_key"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	saved := map[string]string{}
	if body.Provider != "" {
		saved["JEV_CM_PROVIDER"] = body.Provider
	}
	if body.Model != "" {
		saved["JEV_CM_MODEL"] = body.Model
	}
	if body.Typesafe != "" {
		saved["TYPESAFE_API_KEY"] = body.Typesafe
	}
	if body.OpenCodeGo != "" {
		saved["OPENCODE_GO_API_KEY"] = body.OpenCodeGo
	}
	return saved, nil
}

func MergeLookup(saved map[string]string, base Lookup) Lookup {
	return func(key string) string {
		if base != nil {
			if value := strings.TrimSpace(base(key)); value != "" {
				return value
			}
		}
		return strings.TrimSpace(saved[key])
	}
}
