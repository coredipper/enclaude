## 2025-02-23 - Prevent ext:: Command Injection in Git Pull/Push/Fetch
**Vulnerability:** Git remote operations (`pull`, `push`, `fetch`) via `exec.Command` allowed direct arbitrary command execution via `ext::` protocols and `-` flags injected as remote positional parameters (even after `--`).
**Learning:** Checking remote validity only on `remote add` or `set-url` is insufficient, because commands that accept the remote dynamically via parameters (e.g., `enclaude sync ext::sh...`) bypass the remote configuration validation.
**Prevention:** Validate remote input inline within every Git operation method (`Fetch`, `Pull`, `Push`, `PushWithUpstream`) by ensuring inputs are passed to a centralized validation function like `rejectUnsafeRemoteURL` before execution.
## 2025-02-23 - Prevent Path Traversal in Unseal and Repair
**Vulnerability:** The `Unseal` and `Repair` operations read `relPath` directly from `manifest.json` and joined it with `ClaudeDir` without checking if it escapes the directory. This could allow an attacker with a manipulated repository or manifest to write arbitrary files (Path Traversal/ZipSlip variant) outside `ClaudeDir`.
**Learning:** Always validate relative paths read from externally provided manifests or archives before joining them with base directories.
**Prevention:** Use `filepath.IsLocal` to verify that external paths do not escape the expected directory boundaries before acting on them.
