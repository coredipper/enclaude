# enclaude project notes

Encrypted, git-backed, cross-device sync for `~/.claude/`. See [`README.md`](README.md) for the user-facing pitch. This file is for working on the code.

## Important rules

- **Sealing then unsealing must give back exactly the same files.** Only `internal/merge` and `internal/store/remap.go` may change `~/.claude` content. Remap renames the `projects/<encoded>` folder and never changes paths written inside transcripts.
- **Never touch the real machine.** Don't run enclaude commands against the real `~/.claude`, `~/.enclaude`, keyring or key file. Point `HOME`, `XDG_CONFIG_HOME` and `ENCLAUDE_KEY_FILE` at a temporary folder instead.
- **Don't break existing stores.** Ask before changing the `manifest.json` format, the `objects/<hash[0:2]>/<hash[2:]>.age` layout or `ContentHash`, `encodePath`, the age key or key file format, `seal.toml` or `config.ConfigVersion`.
- **Hooks must never block Claude Code.** Anything a hook prints to stdout can end up in the Claude session, so hook code exits quietly when the store is missing or broken and only writes to stderr.
- **Never push, tag, release or force-push without asking**, even in auto-accept mode.
- **Keep it simple (KISS).** Write the plainest code that works. Don't add error handling, validation or code "for future use" before it's needed.
- **Don't repeat yourself (DRY).** Reuse existing helpers before writing new ones. Follow the rule of three, so once the same logic appears a third time, move it into a shared function.
- **Process data in bulk.** Do work once per batch rather than once per item, and keep repeated work and memory allocation out of loops over files and JSONL lines.
- **Use clear, specific types.** Give values named types and structs rather than `any`, `interface{}` or loose maps, so the compiler catches mistakes.

## Layout

- `cmd/` holds the CLI commands, one file per command (`init`, `seal`, `unseal`, `key`, `push`, `pull`, `status` and so on). `cmd/root.go` sets up shared startup, for example `crypto.DefaultPassphraseFunc = ui.ReadPassphrase`.
- `internal/config` loads, saves and combines the TOML config.
- `internal/crypto` handles age encryption and key storage. Keys are kept in the OS keyring, or in a passphrase-protected key file when the keyring isn't available (`keychain.go`, `keyfile.go`).
- `internal/gitops` runs git commands and installs hooks.
- `internal/merge` holds the merge strategies and JSONL handling.
- `internal/session` detects running Claude sessions.
- `internal/store` manages the seal store. `remap.go` renames the `projects/<encoded>` folder on unseal, so a store synced from a machine with a different home folder ends up where the local Claude Code looks for it. Per-machine overrides live in `~/.enclaude/projectmap.local.toml`, which is gitignored and never synced.
- `internal/ui` handles interactive prompts. Passphrases are read without echoing, using `golang.org/x/term`. Other prompts (`Confirm`, `Choose`, `EditString`) go through `ui.DefaultPrompt`, which tests can replace.

A new command goes in a new file under `cmd/`. New encryption or storage code goes under `internal/<package>`.

## Build and test

| Task    | Command                                                                                |
|---------|----------------------------------------------------------------------------------------|
| Build   | `make build`                                                                           |
| Install | `make install`                                                                         |
| Test    | `make test` (runs `go test ./... -count=1`), or `make test-verbose` for verbose output |
| Lint    | `make lint` (golangci-lint)                                                            |

CI (`.github/workflows/ci.yml`) runs the tests, golangci-lint and a build for Linux and macOS on both amd64 and arm64, on every PR and every push to `main`. All of these must pass before merging.

## Version number

`enclaude --version` reads `cmd.Version`, which is set at build time with `-X github.com/coredipper/enclaude/cmd.Version=…` by both the `Makefile` and `.goreleaser.yaml`. Don't write version numbers into the code.

## Test conventions

- Tests sit next to the code they test (`foo.go` and `foo_test.go`). Use the standard `testing` package only, not testify.
- Every test function has a comment above it in this shape:

  ```go
  // TestX_Subcase verifies/exercises/covers/guards [what], [why if
  // non-obvious, e.g. a prior bug or rule being pinned].
  func TestX_Subcase(t *testing.T) { ... }
  ```

  Keep each comment directly above its own function. Putting a new function between an existing comment and its function leaves that comment attached to the wrong test.
- For tests that need the keyring to fail in a controlled way, replace the package's `keyringSet`, `keyringGet` and `keyringDelete` variables rather than using `go-keyring`'s global mock.

## Release flow

1. Merge the PRs for the release into `main` and check that `make test` passes.
2. Pick the new version number. A new feature raises the minor number, a release with only fixes raises the patch number.
3. Tag the release and push the tag.

   ```sh
   git tag -a vX.Y.Z -m "<short summary>"
   git push origin vX.Y.Z
   ```

4. Build and publish with goreleaser, using the GitHub CLI's login token. This builds the archives, writes the changelog, creates the GitHub release and uploads the archives and `checksums.txt`.

   ```sh
   GITHUB_TOKEN="$(gh auth token)" goreleaser release --clean
   ```

5. You can add a highlights section above the generated notes. `--notes` replaces the whole release text, so to keep goreleaser's changelog you have to fetch it and include it again.

   ```sh
   existing=$(gh release view vX.Y.Z --json body -q .body)
   gh release edit vX.Y.Z --notes "## Highlights

   …

   $existing"
   ```

## PRs from forks

If a PR from a fork has "Allow edits from maintainers" ticked (check with `gh pr view <num> --json maintainerCanModify`), you can push straight to the fork's branch without adding a remote.

```sh
git push https://github.com/<fork-owner>/enclaude.git <local>:<branch>
```

## PRs written by AI agents

Several AI coding agents write PRs on this repo and push them under the maintainer's git account, so `gh pr list` always shows `coredipper` as the author. To tell which agent wrote a PR, filter on the start of `headRefName` and check the title emoji.

| Branch prefix | Agent    | Area                      | Title emoji |
|---------------|----------|---------------------------|-------------|
| `bolt-*`      | Bolt     | Performance optimizations | ⚡           |
| `sentinel-*`  | Sentinel | Security fixes            | 🛡️ / 🔒     |
| `jules-*`     | Jules    | Code cleanup              | 🧹           |

A separate background reviewer, `codex`, reviews PRs automatically through the local `roborev` tool. `roborev fix --open --list` lists open reviews, and `roborev show --job <id> --json` fetches one (the findings are in `.output` and the verdict in `.job.verdict`). After fixing a review, add a comment with `roborev comment --commenter roborev-fix --job <id> "<summary>"` and then close it with `roborev close <id>`. Each push to a branch usually starts a new review.

## Rules for AI agents

Every AI agent working on this repo, including Bolt, Sentinel, Jules and the roborev fix loop, follows these rules for every change.

**Tests are required.**
- Every change in behaviour comes with tests. Test what happens when things go wrong as well as when they go right, such as bad input, a missing store or manifest, the wrong key or passphrase, an unavailable keyring and running without a terminal.
- A bug fix needs a test that fails without the fix and passes with it.
- **Sentinel** includes a test that reproduces the security problem, for example a path that tries to escape its folder and is now refused.
- **Bolt** adds a benchmark next to the existing `Benchmark*` functions and puts the before and after numbers in the PR description. Existing tests must still pass unchanged.
- **Jules** must not change behaviour. Existing tests pass without changes to what they check. If a test has to change, explain why in the PR description.

**Keep tests away from real data.**
- Only write inside `t.TempDir()`, and use `t.Setenv` to point `HOME`, `XDG_CONFIG_HOME` and `ENCLAUDE_KEY_FILE` there.
- Never touch the real OS keyring. Encryption tests use `withTestEnv(t)` from `internal/crypto/keychain_test.go`, and any other package that could reach the keyring calls `keyring.MockInit()` first.
- Tests that run real `git`, as `internal/gitops` does, only use temporary repos and never contact a remote.

**Before opening a PR**, run these from the top of the repo in this order. All of them must pass.

```sh
gofmt -l .     # must print nothing
make lint
make test
```

**Commit messages** start with a type such as `fix:`, `feat:`, `docs:`, `test:` or `ci:`. goreleaser uses these to leave docs and test commits out of the changelog.

## Code style

- Comments explain why the code does something, not what it does. Don't put issue or PR numbers in code comments. Those belong in the PR description or commit message.
- The `cmd.Version` build setting, the `crypto.Default*` and `store.DefaultRemapResolver` package variables, `ui.DefaultPrompt` and the `keyringSet`, `keyringGet` and `keyringDelete` variables all exist so that function signatures don't have to change across the codebase. Extend these patterns rather than adding a new parameter to every caller, as `store.Unseal` does with `...UnsealOption`.
