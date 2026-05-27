// Command enclaude provides encrypted, git-backed, cross-device sync
// for ~/.claude/.
//
// Claude Code stores prompts, session transcripts, memory files, and
// settings as plaintext under ~/.claude/. enclaude sits alongside it,
// encrypting that state into a content-addressed, git-tracked store at
// ~/.enclaude/ so it can be safely backed up and synced across machines
// without exposing raw history on disk or in the remote.
//
// # Architecture
//
// Two directories, one direction of trust:
//
//   - ~/.claude/   — plaintext working tree, owned by Claude Code.
//   - ~/.enclaude/ — encrypted store: age-encrypted blobs in
//     objects/, a manifest mapping paths to SHA-256 hashes and per-file
//     merge strategies, a passphrase-encrypted key backup, and a git
//     repository wrapping the lot.
//
// Encryption uses age with a per-user key stored in the OS keyring
// (falling back to a passphrase-encrypted key file). Only ciphertext
// ever reaches the git remote.
//
// # Typical workflow
//
//	enclaude init                 # create the seal store and key
//	enclaude seal                 # encrypt ~/.claude/ into ~/.enclaude/
//	enclaude push                 # publish to the configured git remote
//	enclaude pull && enclaude unseal   # on another device
//
// # Subcommands
//
// The CLI is built on cobra; each subcommand lives in its own file under
// the cmd/ package. See `enclaude help` for the full list, or the
// individual files in cmd/ for source.
//
// # Documentation
//
// User-facing documentation lives in the repository README at
// https://github.com/coredipper/enclaude. This package itself is the
// CLI entry point and exposes no stable Go API — internal logic lives
// under internal/ and is intentionally not importable.
package main
