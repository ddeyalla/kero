# Kero Local v1

Keep Kero's native layout, typography, colors, session behavior, and shortcuts.
Build on upstream 2.0.5; keep the personal fork small and easy to merge.

## Deliverables

1. **Local build:** Kero Local, ARM64 on this Mac, separate application ID,
   settings and daemon. Disable upstream binary updates. Keep `upstream` for
   source updates and `origin` for ddeyalla/kero. Leave the installed Kero and
   its running sessions intact.
2. **Usage:** a compact sidebar section and a details window, with refresh,
   allowance used, reset time, balance where appropriate, source freshness,
   and account/login failures. Read existing Claude/Codex/OpenCode credentials;
   use a configured DeepSeek API key for direct API balance. Never infer plan
   quota from token counts, store credentials in reports, scrape browser
   sessions, refresh login tokens, or display failed reads as zero usage.
3. **Clipboard:** paste copied Finder files as quoted paths. Save clipboard
   screenshots privately and paste their paths for local agents. Support file
   and image drops. Preserve normal text paste, never auto-submit, and report
   errors visibly. Retain screenshots for the agent to read after paste.
4. **Responsiveness:** native architecture build; replace continuous status
   indicator animation with quiet static indicators, preserving status colors
   and meanings. Network and attachment I/O stay off the UI thread.

## Verification

- Baseline and final Go tests, MyGo lifecycle vet, provider fixture tests,
  attachment quoting/privacy tests, and native UI tests/screenshots.
- Build and launch the local app beside upstream. Verify ARM64, live usage,
  text/file/image paste and file drops where native automation permits.
- Record pre-existing failures separately from regressions.
- Preserve pre-existing `.gitignore` edits and everything under `plans/`.

## Provider boundary

Claude and Codex account usage endpoints may change; keep readers isolated.
OpenCode Go requires a valid workspace key. DeepSeek balance is direct API
credit, not a third-party subscription. Missing credentials and expired
logins remain explicit setup states with a link to the provider.

## Follow-up collaboration

Tune sidebar density, quota display and reset wording after trying v1.
Keep broader analytics, routing, dashboards and agent orchestration out of v1.
