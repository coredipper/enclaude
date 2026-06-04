# enclaude — Claude Code project notes

Encrypted, git-backed, cross-device sync for `~/.claude/`. See
[`README.md`](README.md) for the user-facing pitch — this file is for
working *on* the repo.

## Layout

- `cmd/` — cobra subcommands, one file per command (`init`, `seal`,
  `unseal`, `key`, `push`, `pull`, `status`, …). `cmd/root.go` wires
  shared startup, e.g. `crypto.DefaultPassphraseFunc = ui.ReadPassphrase`.
- `internal/config` — TOML config (load / save / overlay).
- `internal/crypto` — age encryption + key storage. OS-keyring is the
  primary backend; falls back to a passphrase-encrypted key file
  (`keychain.go`, `keyfile.go`).
- `internal/gitops` — git plumbing + hook install.
- `internal/merge` — merge strategies + JSONL handling.
- `internal/session` — Claude session detection.
- `internal/store` — seal-store management. `remap.go` rewrites the
  `projects/<encoded>` directory key on unseal so a store synced from a
  machine with a different home lands where the local Claude Code looks;
  device-local overrides live in `~/.enclaude/projectmap.local.toml`
  (gitignored, never synced).
- `internal/ui` — interactive prompts; passphrase reads are silent via
  `golang.org/x/term`. General prompts (`Confirm`/`Choose`/`EditString`)
  go through the injectable `ui.DefaultPrompt` seam.

New cobra subcommand → new file under `cmd/`. New crypto/storage logic →
under `internal/<package>`.

## Build & test

| Task    | Command                                       |
|---------|-----------------------------------------------|
| Build   | `make build`                                  |
| Install | `make install`                                |
| Test    | `make test` (= `go test ./... -count=1`)      |
| Verbose | `make test-verbose`                           |
| Lint    | `make lint` (golangci-lint)                   |

CI (`.github/workflows/ci.yml`) runs `go test ./... -count=1` plus a
cross-compile matrix (linux/darwin × amd64/arm64) on PRs and pushes to
`main`. Keep these green before merging.

## Version injection

`enclaude --version` reads `cmd.Version`, set at link time via
`-X github.com/coredipper/enclaude/cmd.Version=…`. Both the `Makefile`
(which derives the value from `git describe --tags --always --dirty`)
and `.goreleaser.yaml` set it. Don't hard-code version strings — keep
using the ldflag path.

## Test conventions

- Tests live next to the code they exercise (`foo.go` → `foo_test.go`).
  Stdlib `testing` only — no testify.
- Each test function has a doc comment in this shape:

  ```go
  // TestX_Subcase verifies/exercises/covers/guards [what], [why if
  // non-obvious — e.g. a prior bug or invariant being pinned].
  func TestX_Subcase(t *testing.T) { ... }
  ```

  When adding a new test near existing ones, keep the doc comment
  immediately above its function. Don't insert a new function between
  an existing comment and its function — it silently orphans the
  comment.
- Crypto tests use `withTestEnv(t)` from
  `internal/crypto/keychain_test.go` for isolated keyring / file /
  passphrase state. Reuse it; don't reinvent per-test setup.
- For tests that need controlled keyring failures, prefer the package's
  `keyringSet` / `keyringGet` / `keyringDelete` indirection vars over
  `go-keyring`'s global mock.

## Release flow

1. Merge target PR(s) to `main`; verify `make test` is green.
2. Decide the semver bump: new feature → minor, fix-only → patch.
3. Tag and push:

   ```sh
   git tag -a vX.Y.Z -m "<short summary>"
   git push origin vX.Y.Z
   ```

4. Build and publish via goreleaser, reusing gh's token:

   ```sh
   GITHUB_TOKEN="$(gh auth token)" goreleaser release --clean
   ```

   This builds darwin/linux × amd64/arm64 archives, generates the
   changelog (`^docs:` / `^test:` filtered per `.goreleaser.yaml`),
   creates the GitHub release, and uploads the tarballs + `checksums.txt`.

5. Optionally add a highlights section above the auto-generated notes.
   `--notes` *replaces* the body, so to keep goreleaser's changelog you
   have to fetch and re-include it:

   ```sh
   existing=$(gh release view vX.Y.Z --json body -q .body)
   gh release edit vX.Y.Z --notes "## Highlights

   …

   $existing"
   ```

Known nit: goreleaser warns that `archives.format` is deprecated in
favour of `formats: [...]`. Fix opportunistically when touching
`.goreleaser.yaml`.

## Working with PRs from forks

If a fork PR has `maintainerCanModify: true` (the "Allow edits from
maintainers" checkbox), a target-repo maintainer can push directly to
the fork's branch over HTTPS using gh's stored token — no remote add
required:

```sh
git push https://github.com/<fork-owner>/enclaude.git <local>:<branch>
```

Check the flag first: `gh pr view <num> --json maintainerCanModify`.

## Multi-agent PR workflow

Multiple AI coding agents author PRs on this repo locally and push under
the maintainer's git identity, so `gh pr list` always shows
`coredipper` as author. To attribute correctly, look at the branch-name
prefix and title emoji together:

| Branch prefix | Agent    | Domain                    | Title emoji |
|---------------|----------|---------------------------|-------------|
| `bolt-*`      | Bolt     | Performance optimizations | ⚡           |
| `sentinel-*`  | Sentinel | Security fixes            | 🛡️ / 🔒     |
| `jules-*`     | Jules    | Refactoring / code health | 🧹           |

When asked to "review the latest Bolt/Sentinel/Jules PR(s)", filter on
`headRefName` prefix — author filtering returns nothing useful. PR bodies
usually also carry an "PR created automatically by …" footer that
confirms the agent. Older PRs (pre-`jules-*`) used inconsistent prefixes
like `refactor-*` or `code-health/*`; for those, fall back to emoji +
body.

A separate review daemon, `codex`, runs autoreviews via the local
`roborev` CLI: `roborev fix --open --list` enumerates open jobs,
`roborev show --job <id> --json` fetches a review (findings live under
`.output`; verdict under `.job.verdict`), and the fix loop closes with
`roborev comment --commenter roborev-fix --job <id> "<summary>"` then
`roborev close <id>`. Each push to a branch typically triggers a fresh
review.

## Local conventions

- Comments explain *why*, not *what*. Don't pin issue / PR numbers or
  "added for the X flow" in code comments — that belongs in the PR
  description or commit body.
- Don't introduce error-handling layers, validation, or "future use"
  abstractions speculatively. Three similar lines beat a premature
  helper.
- The `cmd.Version` ldflag, the `crypto.Default*` and
  `store.DefaultRemapResolver` package-level callback vars, the
  `ui.DefaultPrompt` seam, and the `keyringSet` / `keyringGet` /
  `keyringDelete` indirections all exist to keep call-site signatures
  stable across the codebase. Prefer extending those patterns over adding
  a parameter to every caller. (`store.Unseal`'s `...UnsealOption` is the
  same instinct: new behavior, untouched existing call sites.)
