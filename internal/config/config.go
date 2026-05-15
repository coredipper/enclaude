package config

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// ConfigVersion tracks the config schema version for one-time migrations.
// Bump this when adding migrations in Load().
const ConfigVersion = 2

type Config struct {
	Version  int            `toml:"config_version"`
	Seal    SealSection    `toml:"seal"`
	Sync     SyncSection     `toml:"sync"`
	Include  PatternSection  `toml:"include"`
	Exclude  PatternSection  `toml:"exclude"`
	Merge    map[string]string `toml:"merge_strategies"`
}

type SealSection struct {
	ClaudeDir string `toml:"claude_dir"`
	SealDir  string `toml:"seal_dir"`
	DeviceID  string `toml:"device_id"`
}

type SyncSection struct {
	AutoSealOnSessionEnd   bool `toml:"auto_seal_on_session_end"`
	AutoUnsealOnSessionStart bool `toml:"auto_unseal_on_session_start"`
	AutoPush               bool `toml:"auto_push"`
	AutoPull               bool `toml:"auto_pull"`
}

type PatternSection struct {
	Patterns []string `toml:"patterns"`
}

func DefaultConfig(claudeDir, sealDir string) *Config {
	return &Config{
		Version: ConfigVersion,
		Seal: SealSection{
			ClaudeDir: claudeDir,
			SealDir:  sealDir,
			DeviceID:  generateDeviceID(),
		},
		Sync: SyncSection{
			AutoSealOnSessionEnd:     true,
			AutoUnsealOnSessionStart: true,
			AutoPush:                 false,
			AutoPull:                 false,
		},
		Include: PatternSection{
			Patterns: []string{
				"history.jsonl",
				"settings.json",
				"stats-cache.json",
				"CLAUDE.md",
				"RTK.md",
				"projects/*/sessions-index.json",
				"projects/*/*.jsonl",
				"projects/*/memory/**",
				"projects/*/subagents/**",
				"sessions/*.json",
				"commands/**",
				"backups/**",
			},
		},
		Exclude: PatternSection{
			Patterns: []string{
				"statsig/**",
				"plugins/**",
				"debug/**",
				"shell-snapshots/**",
				"file-history/**",
				"cache/**",
				"telemetry/**",
				"ide/**",
				"todos/**",
				"tasks/**",
				"plans/**",
				"hooks/**",
				"paste-cache/**",
				"session-env/**",
				"usage-data/**",
				"settings.local.json",
				"*.lock",
			},
		},
		Merge: map[string]string{
			"history.jsonl":                    "jsonl_dedup",
			"projects/*/sessions-index.json":   "sessions_index",
			"stats-cache.json":                 "last_write_wins",
			"settings.json":                    "last_write_wins",
			"projects/*/*.jsonl":               "immutable",
			"projects/*/subagents/**/*.jsonl":  "immutable",
			"projects/*/subagents/**/*.json":   "immutable",
			"projects/*/memory/**":             "text_merge",
			"**/*.md":                          "text_merge",
		},
	}
}

func Load(sealDir string) (*Config, error) {
	path := filepath.Join(sealDir, "seal.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading seal.toml: %w", err)
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing seal.toml: %w", err)
	}

	// Overlay default merge strategies for keys not present in the loaded config.
	defaults := DefaultConfig("", "")
	if cfg.Merge == nil {
		cfg.Merge = defaults.Merge
	} else {
		for pattern, strategy := range defaults.Merge {
			if _, exists := cfg.Merge[pattern]; !exists {
				cfg.Merge[pattern] = strategy
			}
		}
	}

	return &cfg, nil
}

func (c *Config) Save(sealDir string) error {
	path := filepath.Join(sealDir, "seal.toml")
	data, err := toml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

func generateDeviceID() string {
	hostname, _ := os.Hostname()
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("%s-%x", hostname, b)
}
