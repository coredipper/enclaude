## 2025-02-23 - Prevent ext:: Command Injection in Git Pull/Push/Fetch
**Vulnerability:** Git remote operations (`pull`, `push`, `fetch`) via `exec.Command` allowed direct arbitrary command execution via `ext::` protocols and `-` flags injected as remote positional parameters (even after `--`).
**Learning:** Checking remote validity only on `remote add` or `set-url` is insufficient, because commands that accept the remote dynamically via parameters (e.g., `enclaude sync ext::sh...`) bypass the remote configuration validation.
**Prevention:** Validate remote input inline within every Git operation method (`Fetch`, `Pull`, `Push`, `PushWithUpstream`) by ensuring inputs are passed to a centralized validation function like `rejectUnsafeRemoteURL` before execution.
## 2025-02-23 - Prevent Path Traversal in Unseal and Repair
**Vulnerability:** The `Unseal` and `Repair` operations read `relPath` directly from `manifest.json` and joined it with `ClaudeDir` without checking if it escapes the directory. This could allow an attacker with a manipulated repository or manifest to write arbitrary files (Path Traversal/ZipSlip variant) outside `ClaudeDir`.
**Learning:** Always validate relative paths read from externally provided manifests or archives before joining them with base directories.
**Prevention:** Use `filepath.IsLocal` to verify that external paths do not escape the expected directory boundaries before acting on them.
## 2026-06-17 - Prevent Git Flag Injection in Refs
**Vulnerability:** Git operations (`checkout`, `merge`, `show`) accepted user-provided refs directly via `exec.Command`. A ref starting with a dash (e.g., `-p`, `--orphan`) could be interpreted as a Git option instead of a ref, allowing flag injection.
**Learning:** The `--` separator does not protect against positional arguments that Git expects to be refs but which start with `-` (e.g., in `git checkout <ref>`, if `<ref>` is `--orphan`, Git parses it as an option). Even when using `--` later in the command line (e.g., `git checkout <ref> -- <file>`), the `<ref>` parameter itself is evaluated before the `--`.
**Prevention:** Always explicitly validate and reject user-provided Git refs that start with a dash before passing them to `exec.Command`.
