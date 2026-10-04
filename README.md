# enclaude

Encrypted, git-backed, cross-device sync for `~/.claude/`.

## The Problem

Claude Code stores everything in `~/.claude/` as plaintext. That includes:

- **`history.jsonl`** - every prompt you've typed, timestamped
- **Session JSONL files** - full transcripts, including tool calls, tool results and any file contents Claude read
- **Memory files** - project context Claude remembers between sessions
- **Settings and stats** - your configuration, usage patterns and plugin list

So `~/.claude/` holds a detailed record of your work, including code snippets, error messages, file paths, environment variables and anything else that came up in a session. None of it is encrypted, signed or checked for tampering.

Most AI coding assistants work this way (Cursor, Copilot and Windsurf also store history in plaintext). It means:

1. **Anyone with access to your disk can read your full Claude history.** If your laptop is lost, stolen or used by someone else, all session data is exposed.
2. **There's no way to sync sessions across devices.** Your history on your work laptop and your personal machine stay separate.
3. **There's no version history.** If a session file is corrupted or a memory file is overwritten, there's no way to recover a previous state.

`enclaude` addresses all three.

## What It Does

`enclaude` sits between Claude Code and your filesystem and leaves Claude Code itself untouched. It uses two directories:

```
~/.claude/              plaintext (what Claude Code reads/writes)
     |
     |  seal (encrypt)
     v
~/.enclaude/         encrypted git repo
  manifest.json         file index: path -> SHA-256 hash, merge strategy (optionally encrypted)
  seal.toml             config: include/exclude patterns, device ID
  key.age.backup        passphrase-encrypted key backup
  objects/              content-addressed age-encrypted blobs
     |
     |  git push/pull
     v
  remote repo           synced across devices
```

**Seal** encrypts changed files from `~/.claude/` into content-addressed objects using [age](https://age-encryption.org/) (ChaCha20-Poly1305 + X25519). Each file is hashed (SHA-256) and encrypted individually. Unchanged files are skipped by comparing hashes.

**Unseal** decrypts the objects back to `~/.claude/` so Claude Code can use them.

**Git** moves the data between machines. The encrypted objects are committed to a git repository, which gives you version history and remote sync. Only encrypted blobs are pushed, so your plaintext stays on your machine.

**Purge plaintext** is a separate step. `seal` leaves `~/.claude/` in place because Claude Code reads it directly. After sealing, `enclaude purge-plaintext` removes session transcripts that have a recoverable encrypted copy. Add `--shred` to overwrite before removal.

### Why This Works Well for Claude Data

Once a Claude Code session ends, its JSONL file never changes again. This makes syncing simple:

- Sessions from two devices are different files with different hashes, so both sides are kept
- `history.jsonl` is append-only, so two copies are merged by removing duplicate lines and sorting by timestamp
- Memory files are small Markdown files, merged with a 3-way merge that adds conflict markers if both sides changed

Only `settings.json` (last write wins) and `history.jsonl` (line-level dedup) need real merge logic. Everything else is either immutable or trivially mergeable.

## Quick Start

```bash
# Install
go install github.com/coredipper/enclaude@latest

# Initialize: generates an age key, stores it in your OS keychain,
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
# (only encrypted blobs are pushed, your plaintext stays on your machine)
enclaude remote add origin git@github.com:you/enclaude-data.git
enclaude push

# On another device, clone the encrypted repo and import your key
git clone git@github.com:you/enclaude-data.git ~/.enclaude
enclaude key import --from-backup   # or: enclaude key import keyfile.txt
enclaude unseal
enclaude hooks install
```

### Projects Across Devices

Claude Code stores per-project state under `~/.claude/projects/<encoded>/`, where
`<encoded>` is the project's **absolute path** with `/` and `.` turned into `-`
(e.g. `/Users/you/code/app` → `-Users-you-code-app`). That path differs between
machines with different home directories or checkout locations, so a project
sealed on one machine would otherwise restore under a key the other machine's
Claude Code never looks at. The data is present and decrypted, just invisible.

`enclaude unseal` detects project dirs sealed on another machine and offers to
remap them to this machine's key. By default it asks you to accept the
proposed local key, edit it or skip it, and your choices are remembered
per-device. Control it with `--remap`:

```bash
enclaude unseal --remap=interactive   # default on a terminal: ask per project
enclaude unseal --remap=auto          # apply the home-prefix swap, no prompts
enclaude unseal --remap=off           # restore verbatim (legacy behavior)
enclaude unseal --yes                 # accept all proposals non-interactively
```

The session-start hook always runs in `auto` mode (it never blocks on a prompt),
applying unambiguous home-prefix swaps and leaving anything else for an
interactive `unseal`. Inspect and pin mappings directly:

```bash
enclaude project list                 # show project dirs and local/foreign status
enclaude project map  <src> <target>  # pin a mapping (encoded keys or paths)
enclaude project unmap <src>          # remove a pinned mapping
```

Mappings live in `~/.enclaude/projectmap.local.toml`, which is device-local and
never synced. Only the project *directory key* is remapped. Absolute paths
inside transcripts (cwd, tool arguments) keep the original machine's paths.

### Auto-Sync with Hooks

```bash
enclaude hooks install
```

This adds `SessionStart` and `SessionEnd` hooks to `~/.claude/settings.json`. When a session starts, `enclaude` unseals the latest sealed data. When it ends, it seals changes locally. To enable automatic remote sync, set `auto_push = true` and `auto_pull = true` in `~/.enclaude/seal.toml`. The installer adds to your existing hooks (peon-ping, notchi, etc.) and never overwrites them.

### Hiding Folder and File Names

The encrypted objects are named by their hash, but `manifest.json` is plaintext by default and lists every folder and file name. A project folder like `job_hunting_canada` can say a lot on its own. To encrypt the manifest too, set up with:

```bash
enclaude init --encrypt-names
```

For an existing store, set `encrypt_names = true` under `[seal]` in `~/.enclaude/seal.toml` and run `enclaude seal`. The setting is synced with `seal.toml`, so every device writes the manifest the same way. Every device needs an `enclaude` version that supports it, as older versions refuse to seal a store with an encrypted manifest. Commits made before you turned it on still hold the plaintext manifest in your git history.

Two things to know before turning it on. Git can't store an encrypted manifest as a small change to the previous one, so every seal commit adds the whole manifest to `~/.enclaude/.git`, which grows faster than with a plaintext manifest. And after `enclaude key rotate`, commits from before the rotation hold manifests encrypted to the old key, so `enclaude diff` against one of them fails.

## Commands

### Core
| Command | Description |
|---------|-------------|
| `init` | Generate age keypair, store in OS keychain, initial seal, write README.md to seal store |
| `seal` | Encrypt changed files, commit to seal store |
| `unseal` | Decrypt seal store to `~/.claude/` |
| `status` | Show changes since last seal |
| `sync` | Seal + pull + push |
| `push` | Seal + git push |
| `pull` | Git pull + merge + unseal |
| `purge-plaintext` | Remove sealed completed session plaintext from `~/.claude/` (`--all-managed` removes every recoverable managed file) |

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
| `remote add/list/edit/remove` | Manage git remotes used for sync |
| `upgrade` | Upgrade `seal.toml` to the latest config version |
| `hooks install` | Add auto-sync hooks to Claude Code settings |
| `hooks remove` | Remove auto-sync hooks |
| `hooks status` | Check if hooks are installed |
| `readme-regen` | Regenerate and commit README.md in the seal store |
| `project list` | List synced project dirs and their local/foreign status |
| `project map <src> <target>` | Pin a device-local project-key remap |
| `project unmap <src>` | Remove a pinned project-key remap |

## Merge Strategies

When pulling from a remote, two devices may have diverged. `enclaude` uses a custom git merge driver that applies different strategies depending on the file type:

| Strategy | Used for | How it works |
|----------|----------|-------------|
| `immutable` | Session JSONL files | Distinct session files are unioned at the manifest level; if the same path diverges unexpectedly, keep the local side |
| `jsonl_dedup` | `history.jsonl` | Parse each line as JSON, SHA-256 hash for dedup, sort by timestamp |
| `sessions_index` | `sessions-index.json` | Deduplicate entries by `sessionId`, preserving unique sessions from both sides |
| `last_write_wins` | `settings.json`, `stats-cache.json` | Keep whichever version has the later modification time |
| `text_merge` | Memory files (`.md`) | Simple whole-file 3-way merge; conflict markers if both sides changed |

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
  "statsig/**",       # feature flag caches, regenerated automatically
  "plugins/**",       # 200+ MB of cached plugin data, regenerated
  "debug/**",         # debug logs
  "hooks/**",         # your hook scripts, version these separately
  "settings.local.json",  # device-specific paths and permissions
]

[merge_strategies]
"history.jsonl" = "jsonl_dedup"
"projects/*/*.jsonl" = "immutable"
"settings.json" = "last_write_wins"
"projects/*/memory/**" = "text_merge"
```

The example above is abbreviated; the default configuration includes additional include/exclude patterns and merge strategies.

## Security Model

| Property | Status |
|----------|--------|
| Encrypted at rest (between sessions) | Encrypted copy yes; local plaintext remains unless you run `purge-plaintext` |
| Encrypted at rest (during active session) | No, Claude Code needs plaintext to run |
| Encrypted in transit (git push/pull) | Yes, only age-encrypted blobs are pushed |
| Key storage | OS keychain (macOS Keychain, Linux secret-service) |
| Key backup | Passphrase-encrypted `key.age.backup` travels with the repo |
| Tamper detection | SHA-256 content hashes in manifest; `repair --check` verifies integrity |
| Key never in git | Yes, only the passphrase-encrypted backup is committed |

### Limitations

- **During an active Claude Code session, plaintext exists on disk.** Claude Code reads `~/.claude/` directly and can't read encrypted data. Use disk encryption (FileVault, LUKS) to cover active sessions.
- **Sealing does not delete plaintext.** Run `enclaude purge-plaintext` after a successful seal to remove completed session transcripts, or `enclaude purge-plaintext --all-managed --yes` if you intentionally want to remove every managed plaintext file that still matches a recoverable sealed object.
- **Malicious code running as your user can still read your data.** It can read decrypted files or take the key from the keychain. Rely on OS-level protections for this.
- **Every device needs the same encryption key.** Use `key export` to save your key in a password manager, or rely on the passphrase-encrypted `key.age.backup` that travels with the repo.
- **Cross-device project remap only fixes the directory key.** `unseal` places a synced project where the local Claude Code looks (see [Projects Across Devices](#projects-across-devices)), but absolute paths inside transcripts (cwd, tool arguments) keep the original machine's paths. Claude Code doesn't re-run them.

## How It Works Under the Hood

### Content-Addressed Storage

Like git itself, `enclaude` stores objects by their content hash. When you seal a file:

1. Read the plaintext from `~/.claude/`
2. Compute SHA-256 of the plaintext → this becomes the content address
3. Encrypt the plaintext with your age public key
4. Store the encrypted blob at `objects/<hash[0:2]>/<hash[2:]>.age`
5. Record the mapping in `manifest.json`

On the next seal, unchanged files produce the same hash and are skipped entirely. Only new or modified files are encrypted. This makes incremental seals fast, typically under 1 second after a normal session.

### Session Lifecycle

With hooks installed, the flow is:

```
Session starts → hook fires → pull (if auto_pull) + unseal
    ↓
Claude Code runs (reads/writes ~/.claude/ as normal)
    ↓
Session ends → hook fires → seal changes + push (if auto_push)
```

The hook handler acquires a file lock (`~/.enclaude/.seal.lock`) to prevent concurrent seal/unseal operations. If it can't get the lock within 5 seconds, the hook exits quietly so Claude Code is never blocked.

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
