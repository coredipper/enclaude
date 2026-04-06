# claude-seal

Encrypted, git-backed, cross-device sync for `~/.claude/`.

Claude Code stores conversation history, tool results, and session data as unencrypted plaintext in `~/.claude/`. `claude-seal` encrypts this data with [age](https://age-encryption.org/), stores it in a git repository, and syncs it across devices with JSONL-aware merge.

## Quick Start

```bash
go install github.com/coredipper/claude-seal@latest

claude-seal init          # generate key, encrypt ~/.claude/
claude-seal status        # see what changed
claude-seal seal          # encrypt changes
claude-seal unseal        # decrypt to ~/.claude/
```

## Architecture

```
~/.claude/              plaintext (what Claude Code reads/writes)
     │
     │  seal (encrypt)
     ▼
~/.claude-seal/        encrypted git repo
  manifest.json         file index: path → SHA-256 hash, merge strategy
  seal.toml            config: include/exclude, device ID
  key.age.backup        passphrase-encrypted key backup
  objects/              content-addressed age-encrypted blobs
     │
     │  git push/pull
     ▼
  remote repo           synced across devices
```

Files are content-addressed by SHA-256 of their plaintext. Session JSONL files are immutable after completion, so they produce identical hashes across devices — zero merge conflicts.

## Commands

### Core
| Command | Description |
|---------|-------------|
| `init` | Generate age keypair, store in OS keychain, initial seal |
| `seal` | Encrypt changed files, commit to seal store |
| `unseal` | Decrypt seal to `~/.claude/` |
| `status` | Show changes since last seal |
| `sync` | Seal + pull + push (daily driver) |
| `push` | Seal + git push |
| `pull` | Git pull + merge + unseal |

### History
| Command | Description |
|---------|-------------|
| `log` | Seal store commit history |
| `diff [ref]` | Plaintext diff between seal states |
| `rollback <ref>` | Restore to a previous commit (creates safety seal first) |

### Key Management
| Command | Description |
|---------|-------------|
| `key show` | Display public key |
| `key export` | Export private key for backup |
| `key import <file>` | Import key (from file, stdin, or `--from-backup`) |
| `key rotate` | Generate new key, re-encrypt all objects |

### Maintenance
| Command | Description |
|---------|-------------|
| `repair` | Integrity check + fix missing objects from plaintext |
| `repair --check` | Verify-only (exit code 1 if issues) |
| `hooks install` | Auto-sync on Claude Code session start/end |
| `hooks remove` | Remove auto-sync hooks |

## Cross-Device Sync

```bash
# Device A
claude-seal remote add origin git@github.com:you/claude-seal-data.git
claude-seal push

# Device B
claude-seal init --import-key
claude-seal remote add origin git@github.com:you/claude-seal-data.git
claude-seal pull
claude-seal hooks install
```

## Hook Integration

`claude-seal hooks install` adds hooks to `~/.claude/settings.json` that auto-seal on session end and auto-unseal on session start. Existing hooks (peon-ping, notchi, etc.) are preserved.

## Merge Strategies

| Strategy | Used for | How it works |
|----------|----------|-------------|
| `immutable` | Session JSONL files | Union — sessions never change after completion |
| `jsonl_dedup` | `history.jsonl` | Deduplicate lines by SHA-256, sort by timestamp |
| `last_write_wins` | `settings.json`, `stats-cache.json` | Keep the version with later mtime |
| `text_merge` | Memory files (`.md`) | 3-way merge with conflict markers |

## Configuration

`~/.claude-seal/seal.toml` controls what gets synced:

```toml
[include]
patterns = ["history.jsonl", "settings.json", "projects/*/*.jsonl", "projects/*/memory/**"]

[exclude]
patterns = ["statsig/**", "plugins/**", "debug/**", "hooks/**"]
```

## Security Model

| Property | Status |
|----------|--------|
| Encrypted at rest (between sessions) | Yes |
| Encrypted at rest (during active session) | No — Claude Code requires plaintext |
| Encrypted in transit (git push/pull) | Yes — age-encrypted blobs |
| Key in OS keychain | Yes (macOS Keychain, Linux secret-service) |
| Tamper detection | Yes — SHA-256 content hashes in manifest |
| Key never in git | Correct — only passphrase-encrypted backup |

## License

MIT
