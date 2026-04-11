# enclaude

Encrypted, git-backed, cross-device sync for `~/.claude/`.

## The Problem

Claude Code stores everything in plaintext at `~/.claude/`:

- **`history.jsonl`** — every prompt you've ever typed, timestamped
- **Session JSONL files** — full conversation transcripts including tool calls, tool results, and any file content Claude read during the session
- **Memory files** — project-specific context Claude remembers between sessions
- **Settings and stats** — your configuration, usage patterns, plugin list

This means your `~/.claude/` directory contains a detailed record of your work: code snippets, error messages, file paths, environment variables, and anything else that appeared in a session. It sits on disk as readable text with no encryption, no signatures, and no tamper detection.

This isn't a bug — it's how every AI coding assistant works today (Cursor, Copilot, Windsurf all store history in plaintext too). But it means:

1. **Anyone with access to your disk can read your full Claude history.** If your laptop is lost, stolen, or accessed by another user, all session data is exposed.
2. **There's no way to sync sessions across devices.** Your history on your work laptop and your personal machine are completely separate.
3. **There's no version history.** If a session file is corrupted or a memory file is overwritten, there's no way to recover a previous state.

`enclaude` addresses all three.

## What It Does

`enclaude` sits between Claude Code and your filesystem. It doesn't modify Claude Code — it works alongside it using a two-directory architecture:

```
~/.claude/              plaintext (what Claude Code reads/writes)
     |
     |  seal (encrypt)
     v
~/.enclaude/         encrypted git repo
  manifest.json         file index: path -> SHA-256 hash, merge strategy
  seal.toml             config: include/exclude patterns, device ID
  key.age.backup        passphrase-encrypted key backup
  objects/              content-addressed age-encrypted blobs
     |
     |  git push/pull
     v
  remote repo           synced across devices
```

**Seal** encrypts changed files from `~/.claude/` into content-addressed objects using [age](https://age-encryption.org/) (ChaCha20-Poly1305 + X25519). Each file is hashed (SHA-256) and encrypted individually. Only changed files are re-encrypted — unchanged files are skipped by comparing hashes.

**Unseal** decrypts the objects back to `~/.claude/` so Claude Code can use them.

**Git** provides the transport layer. The encrypted objects are committed to a git repository, giving you full version history, branching, and remote sync — all on encrypted data. Your plaintext never leaves your machine; only encrypted blobs are pushed.

### Why This Works Well for Claude Data

Claude Code's session files have a property that makes sync trivial: **they're immutable after completion**. Once a session ends, its JSONL file is never modified again. This means:

- Two devices that both ran sessions produce different files with different hashes — no conflicts, just union both sides
- `history.jsonl` is append-only — merging two diverged copies means deduplicating lines and sorting by timestamp
- Memory files are small markdown — standard 3-way text merge handles them

The only files that need real merge logic are `settings.json` (last-write-wins) and `history.jsonl` (line-level dedup). Everything else is either immutable or trivially mergeable.

## Quick Start

```bash
# Install
go install github.com/coredipper/enclaude@latest

# Initialize — generates an age key, stores it in your OS keychain,
# encrypts all ~/.claude/ data into ~/.enclaude/
enclaude init

# See what's changed since last seal
enclaude status

# Encrypt changes
enclaude seal

# Decrypt back to ~/.claude/
enclaude unseal
```

### Set Up Cross-Device Sync

```bash
# Create a private repo for your encrypted data
# (only encrypted blobs are pushed — your plaintext never leaves your machine)
enclaude remote add origin git@github.com:you/enclaude-data.git
enclaude push

# On another device — clone the encrypted repo and import your key
git clone git@github.com:you/enclaude-data.git ~/.enclaude
enclaude key import --from-backup   # or: enclaude key import keyfile.txt
enclaude unseal
enclaude hooks install
```

### Auto-Sync with Hooks

```bash
enclaude hooks install
```

This adds `SessionStart` and `SessionEnd` hooks to `~/.claude/settings.json`. When a session starts, `enclaude` unseals the latest sealed data. When it ends, it seals changes locally. To enable automatic remote sync, set `auto_push = true` and `auto_pull = true` in `~/.enclaude/seal.toml`. Your existing hooks (peon-ping, notchi, etc.) are preserved — the installer appends to the hooks array, never overwrites.

## Commands

### Core
| Command | Description |
|---------|-------------|
| `init` | Generate age keypair, store in OS keychain, initial seal |
| `seal` | Encrypt changed files, commit to seal store |
| `unseal` | Decrypt seal store to `~/.claude/` |
| `status` | Show changes since last seal |
| `sync` | Seal + pull + push (the daily driver) |
| `push` | Seal + git push |
| `pull` | Git pull + merge + unseal |

### History & Recovery
| Command | Description |
|---------|-------------|
| `log` | Show seal history with commit messages |
| `diff [ref]` | Decrypt and diff between current state and a previous commit |
| `rollback <ref>` | Restore `~/.claude/` to a previous commit (creates a safety seal first, so you can always undo) |

### Key Management
| Command | Description |
|---------|-------------|
| `key show` | Display public key and source |
| `key export` | Print private key to stdout (pipe to a password manager) |
| `key import <file>` | Import key from file, stdin (`-`), or `--from-backup` |
| `key rotate` | Generate new key, re-encrypt all objects, update keychain |

### Maintenance
| Command | Description |
|---------|-------------|
| `repair` | Verify integrity and fix missing objects by re-sealing from plaintext |
| `repair --check` | Verify-only mode (exit code 1 if issues found, useful for CI) |
| `repair --delete-orphans` | Also remove unreferenced object files |
| `hooks install` | Add auto-sync hooks to Claude Code settings |
| `hooks remove` | Remove auto-sync hooks |
| `hooks status` | Check if hooks are installed |

## Merge Strategies

When pulling from a remote, two devices may have diverged. `enclaude` uses a custom git merge driver that applies different strategies depending on the file type:

| Strategy | Used for | How it works |
|----------|----------|-------------|
| `immutable` | Session JSONL files | Union both sides — sessions never change after completion, so there are no conflicts |
| `jsonl_dedup` | `history.jsonl` | Parse each line as JSON, SHA-256 hash for dedup, sort by timestamp |
| `sessions_index` | `sessions-index.json` | Deduplicate entries by `sessionId`, preserving unique sessions from both sides |
| `last_write_wins` | `settings.json`, `stats-cache.json` | Keep whichever version has the later modification time |
| `text_merge` | Memory files (`.md`) | Standard 3-way text merge; conflict markers if both sides changed |

These strategies are configurable per path pattern in `seal.toml`.

## Configuration

`~/.enclaude/seal.toml` controls what gets synced and how:

```toml
[seal]
claude_dir = "~/.claude"
seal_dir = "~/.enclaude"
device_id = "macbook-ab12cd34"

[sync]
auto_seal_on_session_end = true
auto_unseal_on_session_start = true
auto_push = false
auto_pull = false

[include]
patterns = [
  "history.jsonl",
  "settings.json",
  "projects/*/*.jsonl",
  "projects/*/memory/**",
  "projects/*/subagents/**",
]

[exclude]
patterns = [
  "statsig/**",       # feature flag caches — regenerated automatically
  "plugins/**",       # 200+ MB of cached plugin data — regenerated
  "debug/**",         # debug logs
  "hooks/**",         # your hook scripts — version these separately
  "settings.local.json",  # device-specific paths and permissions
]

[merge_strategies]
"history.jsonl" = "jsonl_dedup"
"projects/*/*.jsonl" = "immutable"
"settings.json" = "last_write_wins"
"projects/*/memory/**" = "text_merge"
```

## Security Model

| Property | Status |
|----------|--------|
| Encrypted at rest (between sessions) | Yes — completed session plaintext can be shredded after seal |
| Encrypted at rest (during active session) | No — Claude Code requires plaintext to function |
| Encrypted in transit (git push/pull) | Yes — only age-encrypted blobs are pushed |
| Key storage | OS keychain (macOS Keychain, Linux secret-service, Windows Credential Manager) |
| Key backup | Passphrase-encrypted `key.age.backup` travels with the repo |
| Tamper detection | SHA-256 content hashes in manifest; `repair --check` verifies integrity |
| Key never in git | Correct — only the passphrase-encrypted backup is committed |

### Honest Limitations

- **During an active Claude Code session, plaintext exists on disk.** This is unavoidable — Claude Code reads `~/.claude/` directly and cannot be modified to read encrypted data. Use OS-level disk encryption (FileVault, BitLocker, LUKS) for protection during sessions.
- **Application-level encryption does not protect against a malicious process running as your user.** If an attacker has code execution as your user, they can read decrypted files in memory or extract the key from the keychain. OS-level protections are the right defense layer here.
- **The encryption key must be shared across devices.** This is inherent to any cross-device sync scheme. Use `key export` to save your key in a password manager, or rely on the passphrase-encrypted `key.age.backup` that travels with the repo.

## How It Works Under the Hood

### Content-Addressed Storage

Like git itself, `enclaude` stores objects by their content hash. When you seal a file:

1. Read the plaintext from `~/.claude/`
2. Compute SHA-256 of the plaintext → this becomes the content address
3. Encrypt the plaintext with your age public key
4. Store the encrypted blob at `objects/<hash[0:2]>/<hash[2:]>.age`
5. Record the mapping in `manifest.json`

On the next seal, unchanged files produce the same hash and are skipped entirely. Only new or modified files are encrypted. This makes incremental seals fast — typically under 1 second after a normal session.

### Session Lifecycle

With hooks installed, the flow is:

```
Session starts → hook fires → pull latest + unseal
    ↓
Claude Code runs (reads/writes ~/.claude/ as normal)
    ↓
Session ends → hook fires → seal changes + push
```

The hook handler acquires a file lock (`~/.enclaude/.seal.lock`) to prevent concurrent seal/unseal operations. If the lock can't be acquired within 5 seconds, the hook exits silently — it never blocks Claude Code.

### New Device Onboarding

```
1. Install enclaude
2. Clone your encrypted data repo
3. Import your key (from password manager, file, or backup passphrase)
4. Pull + unseal
5. Install hooks
```

Your full Claude Code history, memory, and settings appear on the new device, encrypted in transit and at rest.

## License

MIT

