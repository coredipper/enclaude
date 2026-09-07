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
## 2026-06-29 - Harden Pull/Push Refs Against Flag Injection
**Vulnerability:** Git operations (`pull`, `push`, `PushWithUpstream`) passed user-provided branch names to `exec.Command` without an explicit dash check. The branch already sat after the `--` separator (`git push … -- <remote> <branch>`), so git parsed it as a refspec rather than an option — this is defense-in-depth hardening, not an exploitable flag injection like the earlier ref cases.
**Learning:** `--` is a hard end-of-options marker: every positional after it is a non-option, so a branch placed after `--` cannot be parsed as a flag. The genuinely exploitable case (see the 2026-06-17 entry) is a ref placed *before* `--`, e.g. `git checkout <ref> -- <file>`, where `<ref>` is evaluated as an option first. Validate dash-prefixed refs anyway so every git call site shares one guard regardless of `--` placement.
**Prevention:** Reject dash-prefixed branch inputs inline in every Git operation method (`Pull`, `Push`, `PushWithUpstream`) via `rejectUnsafeRef` before execution, matching the `Merge` belt-and-suspenders pattern.
## 2026-07-06 - Prevent Path Traversal in ObjectStore
**Vulnerability:** The `ObjectStore` methods (`Write`, `Read`, `Delete`, `Exists`, `Size`) constructed file paths using `filepath.Join(s.dir, hash[:2], hash[2:]+".age")` without verifying that `hash` was a valid SHA-256 hex string. A malicious manifest could provide a `content_hash` like `"../../etc/passwd"`, causing the joined path to escape the `objects/` directory and allowing arbitrary file reads/writes/deletions.
**Learning:** Even when a string is intended to be an internal ID or hash, if it originates from external/untrusted sources (like a synced manifest) and is used in path construction, it must be strictly validated.
**Prevention:** Enforce strict validation on object IDs (e.g., ensuring they are exactly 64 characters of lowercase hexadecimal `[0-9a-f]`) before using them in filesystem operations.
## 2026-07-06 - Unhandled rand.Read Error Vulnerability
**Vulnerability:** Unchecked errors from `crypto/rand.Read`. If it fails, the provided buffer remains uninitialized (typically all zeros).
**Learning:** In security-sensitive operations (e.g., generating device IDs or shredding files with random data), silently proceeding after `rand.Read` fails can lead to predictable identifiers or insecure data wiping.
**Prevention:** Always check the error returned by `rand.Read` and handle it securely, either by aborting the operation or using a safe fallback (like a high-precision timestamp for non-cryptographic identifiers).

## 2026-07-06 - Unhandled rand.Read Error in Benchmarks Vulnerability
**Vulnerability:** Unchecked errors from `math/rand.Read` or `crypto/rand.Read` in benchmark tests.
**Learning:** Unhandled random generation errors in benchmarks can lead to measuring the performance of predictable (zeroed) data generation or processing instead of actual random data processing, potentially skewing benchmark results or hiding actual errors.
**Prevention:** Even in tests and benchmarks, always check the error returned by `rand.Read` and use `b.Fatalf` or `t.Fatalf` to abort the execution if it fails.
## 2026-07-07 - Prevent Path Traversal in Rotate Object Paths
**Vulnerability:** The `stagedObjectPath` function used in object rotation didn't validate if `hash` was a valid SHA-256 hex string before constructing paths, leading to path traversal vulnerability if a malicious manifest provides `../../etc/passwd` style `content_hash`.
**Learning:** Functions that generate paths based on external inputs such as manifest contents, even in auxiliary workflows like migration or rotation, must always validate those inputs against the expected format.
**Prevention:** In functions like `stagedObjectPath`, always apply the same validation `isValidHash` as used in the primary `ObjectStore` read/write paths, and return an error if validation fails.
