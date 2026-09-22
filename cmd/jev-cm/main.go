package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"jev-cm/internal/client"
	"jev-cm/internal/config"
	"jev-cm/internal/engine"
	"jev-cm/internal/memory"
	"jev-cm/internal/service"
	"jev-cm/internal/store"
)

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, lookup func(string) string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: jev-cm prepare|expand|remember|recall|import-source|capture|inject|memory|recent|purge")
		return 2
	}
	saved, err := config.ReadSettings(config.SettingsPath(lookup))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	lookup = config.MergeLookup(saved, lookup)
	cfg, err := config.Load(nil, lookup)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	switch args[0] {
	case "prepare":
		var payload map[string]any
		if err := json.NewDecoder(stdin).Decode(&payload); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		plan, err := service.Prepare(payload, cfg, &client.Client{Config: cfg, LookupEnv: pairLookup(lookup)})
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return writeJSON(stdout, plan)
	case "purge":
		if !hasFlag(args[1:], "--yes") {
			return writeJSON(stdout, map[string]any{"deleted": false, "path": cfg.SQLitePath})
		}
		opened, err := store.Open(cfg.SQLitePath)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := opened.Purge(); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return writeJSON(stdout, map[string]any{"deleted": true, "path": cfg.SQLitePath})
	case "expand":
		pointer, page := flagValue(args[1:], "--pointer"), 0
		if pointer == "" {
			fmt.Fprintln(stderr, "--pointer is required")
			return 2
		}
		fmt.Sscanf(flagValue(args[1:], "--page"), "%d", &page)
		opened, err := store.Open(cfg.SQLitePath)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		defer opened.Close()
		eng := engine.Engine{Config: cfg, Store: opened}
		result, err := eng.Expand(pointer, page)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if !result.Found {
			return writeJSON(stdout, map[string]any{"found": false})
		}
		if _, err := stdout.Write(result.Data); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	case "remember":
		source := flagValue(args[1:], "--source")
		collection := flagValue(args[1:], "--collection")
		body, _ := io.ReadAll(stdin)
		opened, jev, err := openStack(cfg, lookup)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		defer opened.Close()
		mem := memory.Memory{Config: cfg, Client: jev, Store: opened}
		var result memory.WriteResult
		if collection == memory.ConversationCollection {
			result = mem.Ingest(memory.Turn{
				Text: string(body), Role: flagOr(args[1:], "--role", "user"), Cwd: flagValue(args[1:], "--cwd"),
				SessionID: flagValue(args[1:], "--session"), SourceID: source, GeneratedSummary: hasFlag(args[1:], "--summary"),
			})
		} else {
			result = mem.Remember(string(body), source, collection, hasFlag(args[1:], "--summary"))
		}
		code := 0
		if result.Status != "stored" && result.Status != "duplicate" {
			code = 1
		}
		if err := json.NewEncoder(stdout).Encode(map[string]any{"status": result.Status, "probability": result.Probability, "reason": result.Reason}); err != nil {
			return 1
		}
		return code
	case "recall":
		collection := flagValue(args[1:], "--collection")
		body, _ := io.ReadAll(stdin)
		opened, jev, err := openStack(cfg, lookup)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		defer opened.Close()
		mem := memory.Memory{Config: cfg, Client: jev, Store: opened}
		result := mem.Recall(string(body), collection, cfg.TokenBudget)
		return writeJSON(stdout, result)
	case "import-source":
		var texts []string
		if err := json.NewDecoder(stdin).Decode(&texts); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		opened, err := store.Open(cfg.SQLitePath)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		defer opened.Close()
		mem := memory.Memory{Config: cfg, Store: opened}
		if err := mem.ImportSource(flagValue(args[1:], "--collection"), flagValue(args[1:], "--source"), texts); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return writeJSON(stdout, map[string]any{"replaced": true})
	case "capture":
		var payload struct {
			Cwd       string `json:"cwd"`
			SessionID string `json:"session_id"`
			SourceID  string `json:"source_id"`
			Messages  []struct {
				Role             string `json:"role"`
				Content          string `json:"content"`
				GeneratedSummary bool   `json:"generated_summary"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(stdin).Decode(&payload); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		opened, err := store.Open(cfg.SQLitePath)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		defer opened.Close()
		mem := memory.Memory{Config: cfg, Store: opened}
		stored, ignored := 0, 0
		for _, message := range payload.Messages {
			result := mem.Ingest(memory.Turn{
				Text: message.Content, Role: message.Role, Cwd: payload.Cwd, SessionID: payload.SessionID,
				SourceID: payload.SourceID, GeneratedSummary: message.GeneratedSummary,
			})
			if result.Status == "stored" {
				stored++
			} else {
				ignored++
			}
		}
		return writeJSON(stdout, map[string]any{"stored": stored, "ignored": ignored})
	case "inject":
		body, _ := io.ReadAll(stdin)
		opened, jev, err := openStack(cfg, lookup)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		defer opened.Close()
		mem := memory.Memory{Config: cfg, Client: jev, Store: opened}
		section := memory.Section(mem.Recall(string(body), memory.ConversationCollection, memory.InjectBudget))
		if section != "" {
			if _, err := io.WriteString(stdout, section); err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
		}
		return 0
	case "recent":
		opened, err := store.Open(cfg.SQLitePath)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		defer opened.Close()
		mem := memory.Memory{Config: cfg, Store: opened}
		if _, err := io.WriteString(stdout, mem.LatestConversation(memory.InjectBudget)); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	case "memory":
		opened, err := store.Open(cfg.SQLitePath)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		defer opened.Close()
		count, err := opened.CountMemory(memory.ConversationCollection, "")
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		keySet := false
		for _, env := range cfg.APIKeyEnvs() {
			if lookup != nil && lookup(env) != "" {
				keySet = true
			}
		}
		return writeJSON(stdout, map[string]any{"path": cfg.SQLitePath, "count": count, "key_set": keySet})
	default:
		fmt.Fprintln(stderr, "unknown command")
		return 2
	}
}

func openStack(cfg config.Config, lookup func(string) string) (*store.Store, *client.Client, error) {
	opened, err := store.Open(cfg.SQLitePath)
	if err != nil {
		return nil, nil, err
	}
	return opened, &client.Client{Config: cfg, LookupEnv: pairLookup(lookup)}, nil
}

func pairLookup(lookup func(string) string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		if lookup == nil {
			return os.LookupEnv(key)
		}
		value := lookup(key)
		if value == "" {
			return "", false
		}
		return value, true
	}
}

func hasFlag(args []string, name string) bool {
	for _, arg := range args {
		if arg == name {
			return true
		}
	}
	return false
}

func flagOr(args []string, name, fallback string) string {
	if value := flagValue(args, name); value != "" {
		return value
	}
	return fallback
}

func flagValue(args []string, name string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == name {
			return args[i+1]
		}
	}
	return ""
}

func writeJSON(stdout io.Writer, value any) int {
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return 1
	}
	return 0
}
