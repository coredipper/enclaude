
## 2024-05-23 - Prevent Flag Injection in Git Commands
**Vulnerability:** Raw `exec.Command("git", ...)` invocations in commands like `init` and `seal` accepted dynamically resolved directory arguments (`sealDir`) positionally without escaping. If a directory started with a dash (e.g., `-f`), it could be interpreted as a Git option, leading to flag injection and potentially command injection.
**Learning:** Directly wrapping external CLIs requires ensuring arguments cannot masquerade as flags. The `internal/gitops` package acts as a security boundary enforcing `--` and `-C`, but was being bypassed by bare `exec.Command` calls in the application code.
**Prevention:** Always route Git operations through the central `gitops.Git` wrapper. Never invoke `exec.Command("git", ...)` directly in command handlers.
