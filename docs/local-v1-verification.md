# Local v1 verification

Verified on this Apple Silicon Mac, 2026-10-20, against upstream `a920e3a`
(Kero 2.0.5).

## Checks

- `CGO_ENABLED=0 go test ./...`: passed with the macOS system directories
  ahead of Homebrew on PATH. The original environment reproducibly fails
  two existing Homebrew-zsh subprocess tests before and after this change;
  the system-zsh run passes both.
- `go test -race ./internal/usage`: passed.
- `go tool mygo vet .` and `git diff --check`: passed.
- Native `darwin/arm64` build, ad hoc signing, installation in
  `~/Applications/Kero Local.app`, and launch: passed.
- Installed executable's `usage` command: live Claude, Codex and OpenCode Go
  allowance windows and reset times returned. DeepSeek has no configured
  direct API key and correctly reports not connected.
- Restarting the local GUI retained its daemon session and shell PID.
  The original Kero continued running.
- UI test renderer: light/dark sidebar, usage detail cards, stale/error states,
  compact window, and masked API-key editing checked. PNG screenshots were
  inspected locally under `/private/tmp/kero-local-shots`.
- Attachment checks: Finder path quoting, saved PNG integrity, private file
  permissions, normal-text rejection, and invalid PNG rejection passed.
- Provider fixtures cover both Claude response formats, Codex variable window
  durations, OpenCode legacy/current field names, DeepSeek currencies, invalid
  numeric data, authentication errors, 429 backoff and retained stale values.

## Review

Reviewed the current changes inline, following the repository instruction to
run subagent work in the main thread. Correctness, credential handling,
background work, failure states, UI lifetime, maintainability and test value
were checked. No independent reviewer or external peer ran.

The review caught and resolved the API-key field lifetime issue and OpenCode's
current response shape. No remaining blocker was found for this local preview.

## Verification limits

Computer Use did not have permission to inspect or operate the new application.
Native clipboard shortcuts and drag/drop therefore still need a user trial
inside Claude Code, Codex and OpenCode. The attachment conversion and input
binding tests pass, but do not establish end-to-end native event delivery.
Performance checks establish a native ARM64 process and removal of continuous
status animation, not a controlled before/after latency benchmark. New usage
strings are English in this first local version.
