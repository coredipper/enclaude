package config

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// ConfigVersion tracks the config schema version for one-time migrations.
// Bump this when adding migrations in Load().
//
// v3: introduced the `source` profile and renamed `claude_dir` to `source_dir`.
const ConfigVersion = 3

// Supported source profiles. A source profile defines the default directory,
// include/exclude patterns, and merge strategies for one agent tool.
const (
	SourceClaude = "claude"
	SourcePi     = "pi"
)

type Config struct {
	Version int               `toml:"config_version"`
	Seal    SealSection       `toml:"seal"`
	Sync    SyncSection       `toml:"sync"`
	Include PatternSection    `toml:"include"`
	Exclude PatternSection    `toml:"exclude"`
	Merge   map[string]string `toml:"merge_strategies"`
}

type SealSection struct {
	// Source selects the default profile ("claude" or "pi").
	Source string `toml:"source,omitempty"`
	// SourceDir is the directory being sealed (e.g. ~/.claude or ~/.pi/agent).
	SourceDir string `toml:"source_dir"`
	// LegacyClaudeDir accepts the pre-v3 "claude_dir" key. It is migrated into
	// SourceDir on load and never written back.
	LegacyClaudeDir string `toml:"claude_dir,omitempty"`
	SealDir         string `toml:"seal_dir"`
	DeviceID        string `toml:"device_id"`
}

type SyncSection struct {
	AutoSealOnSessionEnd     bool `toml:"auto_seal_on_session_end"`
	AutoUnsealOnSessionStart bool `toml:"auto_unseal_on_session_start"`
	AutoPush                 bool `toml:"auto_push"`
	AutoPull                 bool `toml:"auto_pull"`
}

type PatternSection struct {
	Patterns []string `toml:"patterns"`
}

// NormalizeSource maps user-provided source names onto the canonical values.
// Unknown or empty values fall back to the Claude profile so existing seals
// keep working.
func NormalizeSource(source string) string {
	switch source {
	case SourcePi, "pi.dev", "pi-coding-agent", "picodingagent":
		return SourcePi
	default:
		return SourceClaude
	}
}

// DefaultConfig returns the Claude Code profile. Kept for backwards
// compatibility; prefer DefaultConfigForSource for new code.
func DefaultConfig(claudeDir, sealDir string) *Config {
	return DefaultConfigForSource(SourceClaude, claudeDir, sealDir)
}

// DefaultConfigForSource returns a config populated with the defaults for the
// given source profile.
func DefaultConfigForSource(source, sourceDir, sealDir string) *Config {
	source = NormalizeSource(source)
	cfg := &Config{
		Version: ConfigVersion,
		Seal: SealSection{
			Source:    source,
			SourceDir: sourceDir,
			SealDir:   sealDir,
			DeviceID:  generateDeviceID(),
		},
		Sync: SyncSection{
			AutoSealOnSessionEnd:     true,
			AutoUnsealOnSessionStart: true,
			AutoPush:                 false,
			AutoPull:                 false,
		},
	}

	switch source {
	case SourcePi:
		cfg.Include = PatternSection{Patterns: piIncludePatterns()}
		cfg.Exclude = PatternSection{Patterns: piExcludePatterns()}
		cfg.Merge = piMergeStrategies()
	default:
		cfg.Include = PatternSection{Patterns: claudeIncludePatterns()}
		cfg.Exclude = PatternSection{Patterns: claudeExcludePatterns()}
		cfg.Merge = claudeMergeStrategies()
	}

	return cfg
}

func claudeIncludePatterns() []string {
	return []string{
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
	}
}

func claudeExcludePatterns() []string {
	return []string{
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
	}
}

func claudeMergeStrategies() map[string]string {
	return map[string]string{
		"history.jsonl":                   "jsonl_dedup",
		"projects/*/sessions-index.json":  "sessions_index",
		"stats-cache.json":                "last_write_wins",
		"settings.json":                   "last_write_wins",
		"projects/*/*.jsonl":              "immutable",
		"projects/*/subagents/**/*.jsonl": "immutable",
		"projects/*/subagents/**/*.json":  "immutable",
		"projects/*/memory/**":            "text_merge",
		"**/*.md":                         "text_merge",
	}
}

func piIncludePatterns() []string {
	return []string{
		// User configuration.
		"settings.json",
		"models.json",
		"models-store.json",
		"keybindings.json",
		"mcp.json",
		// Instructions and prompt overrides.
		"AGENTS.md",
		"AGENTS.override.md",
		"AGENTS.MD",
		"CLAUDE.md",
		"CLAUDE.MD",
		"SYSTEM.md",
		"APPEND_SYSTEM.md",
		// Conversation history.
		"sessions/**/*.jsonl",
		// User resources.
		"extensions/**",
		"skills/**",
		"prompts/**",
		"themes/**",
		"tools/**",
	}
}

func piExcludePatterns() []string {
	return []string{
		// Bundled tool binaries and installed packages are large and
		// regenerated by pi.
		"bin/**",
		"npm/**",
		"tmp/**",
		"logs/**",
		// The enclaude extension is device-local (re)created by
		// "enclaude hooks install". Excluding it keeps an unseal from
		// deleting a freshly installed hook before the first seal.
		"extensions/enclaude.ts",
		"extensions/enclaude/**",
		// Credentials and trust decisions are device-local secrets/state and
		// are never sealed.
		"auth.json",
		"oauth.json",
		"mcp-auth.json",
		"trust.json",
		// Machine-local diagnostics and logs.
		"crashes.json",
		"diagnostics.json",
		"*.log",
		"mcp.log*",
		// Editor and OS noise.
		"**/*.swp",
		"**/*.swo",
		"**/*.tmp",
		"**/*~",
		"**/.DS_Store",
		"**/.git/**",
	}
}

func piMergeStrategies() map[string]string {
	return map[string]string{
		// Pi session files are unique per session and append-only while
		// active, so union-merging is never lossy.
		"sessions/**/*.jsonl": "immutable",
		// Small machine config: last writer wins.
		"settings.json":     "last_write_wins",
		"models.json":       "last_write_wins",
		"models-store.json": "last_write_wins",
		"keybindings.json":  "last_write_wins",
		"mcp.json":          "last_write_wins",
		// Human-authored resources merge as text.
		"**/*.md": "text_merge",
		"**/*.ts": "text_merge",
		"**/*.js": "text_merge",
		// Remaining JSON (resource manifests, themes, etc.) is last-write-wins
		// rather than a structural merge.
		"**/*.json": "last_write_wins",
	}
}

// Source returns the normalized source profile for this config.
func (c *Config) Source() string {
	return NormalizeSource(c.Seal.Source)
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

	cfg.migrate()

	// The store is synced across machines, so the seal_dir baked in by `init`
	// on another device (with a different home) is stale. The manifest and
	// objects always live next to the seal.toml we just read, so pin seal_dir
	// to the load location.
	cfg.Seal.SealDir = sealDir

	// Overlay default merge strategies for keys not present in the loaded config.
	defaults := DefaultConfigForSource(cfg.Source(), "", "")
	if cfg.Merge == nil {
		cfg.Merge = defaults.Merge
	} else {
		for pattern, strategy := range defaults.Merge {
			if _, exists := cfg.Merge[pattern]; !exists {
				cfg.Merge[pattern] = strategy
			}
		}
	}

	// Overlay include/exclude defaults only when the config omitted them.
	if cfg.Include.Patterns == nil {
		cfg.Include.Patterns = defaults.Include.Patterns
	}
	if cfg.Exclude.Patterns == nil {
		cfg.Exclude.Patterns = defaults.Exclude.Patterns
	}

	return &cfg, nil
}

// migrate upgrades an in-memory config from an older schema.
func (c *Config) migrate() {
	if c.Seal.SourceDir == "" && c.Seal.LegacyClaudeDir != "" {
		c.Seal.SourceDir = c.Seal.LegacyClaudeDir
	}
	c.Seal.LegacyClaudeDir = ""

	if c.Seal.Source == "" {
		c.Seal.Source = SourceClaude
	} else {
		c.Seal.Source = NormalizeSource(c.Seal.Source)
	}
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
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp if random generation fails
		return fmt.Sprintf("%s-%x", hostname, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s-%x", hostname, b)
}
