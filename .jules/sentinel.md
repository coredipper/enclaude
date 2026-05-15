## 2026-05-15 - Command Option Injection in Git Wrapper
**Vulnerability:** Argument injection risk in `internal/gitops/git.go` due to unsafe passing of user-provided branch names, remotes, and file paths to `git` CLI operations.
**Learning:** Even when wrapping CLI tools using parameterized arrays (like `exec.Command` in Go), options can still be injected if user input starts with a dash (e.g. `-o malicious`).
**Prevention:** Always use the standard `--` separator to explicitly mark the end of options and force subsequent arguments to be treated as positionals (e.g. `git push -- remote branch`).
