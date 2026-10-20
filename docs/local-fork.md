# Shibu Code

Personal fork: https://github.com/ddeyalla/kero. Upstream:
https://github.com/egoist/kero. The local build follows Kero's native UI.

## Build and run on macOS

```sh
sh scripts/local-build.sh
open "build/darwin-arm64/Shibu Code.app"
```

The script builds for the Go toolchain's native host architecture. On Apple
Silicon it must report `arm64`. The app is signed ad hoc for local use.
It has its own app ID, daemon, settings and sessions, so the original Kero
can keep running. Existing sessions stay in the original app.
Install the built app in `~/Applications/Shibu Code.app` if desired.

Shibu Code is the personal edition previously named Kero Local. Its bundle
identifier and data directory stay the same, preserving local sessions,
settings and provider keys. The supplied golden retriever terminal artwork
is stored in `resources/shibu-icon.png` for the app icon. The CLI remains `kero`.

Data lives in `~/Library/Application Support/kero-local` on macOS, or
`$KERO_HOME` when explicitly configured. Development builds use
`kero-local-dev`. Installing this app does not replace the original CLI;
use its executable directly, or its menu's CLI installer when ready to switch.

## Usage

Click **Usage** in the sidebar or press **Cmd+Shift+U** for an in-app modal
with allowance windows, reset times, and manual refresh. **Cmd+R** refreshes
while the modal is open; **Esc** or **Cmd+W** closes it and returns to the
terminal. On Linux/Windows the open shortcut is **Ctrl+Alt+U**. The action is
also available in View, the command palette and `kero do usage.open`.
Reads run in the background every two minutes. The last successful
observation remains visible, marked stale, after a failed request. A 429
backs off before trying again; refresh does not bypass it.

The same readout is available without a window using `kero usage --json`.
Each CLI invocation performs a fresh read; automatic polling and backoff
retention belong to the running app.

CPU and RAM sit below the account readouts as two equal-width meters; their
bars show CPU busy percentage and the used fraction of physical RAM. Click them
for details in the same modal. They measure the whole Mac, across all apps.
Native Mach calls sample every two seconds on a background goroutine, without
spawning `top` or shell processes. CPU is the busy share of all cores. RAM is
active, wired and compressed physical memory, with reclaimable cache excluded.
`kero resources --json` exposes the same measurements without a window.

- **Claude:** reads the active Claude Code Keychain login on macOS, or its
  `.credentials.json`. Supports `CLAUDE_CONFIG_DIR`. It never renews tokens.
- **Codex:** reads `$CODEX_HOME/auth.json` (default `~/.codex/auth.json`), with
  an OpenCode OpenAI OAuth fallback. API-key logins do not expose plan quota.
- **OpenCode Go:** reads OpenCode's workspace key from its auth file or
  `OPENCODE_API_KEY`. A replacement workspace key can be entered in Usage.
  A valid key and Go entitlement are required.
- **DeepSeek:** direct API balance via `DEEPSEEK_API_KEY`, an existing
  OpenCode DeepSeek key, or a key entered in Usage. DeepSeek through an
  OpenCode subscription consumes the OpenCode allowance instead.

Explicitly entered keys are stored only in `usage-credentials.json` in the
local app's data directory, atomically written with mode `0600`. They are not
stored in reports or logs. To remove an override, quit the local app and remove
its provider entry from that file. Existing CLI credentials are read-only.
Credentials are sent only to their own provider's fixed HTTPS endpoint.
Browser cookies and sign-ins are never imported. Claude/Codex account endpoints
can change; unsupported responses show unavailable, not zero.

## Attachments and performance

Copy Finder files and paste them into a pane, or drag files onto its terminal.
Kero inserts quoted absolute paths without submitting. Clipboard screenshots
and dropped PNGs are saved with private permissions under `attachments/`, then
their paths are inserted. The program in that pane must support reading local
files and images. Remote shells need their own upload workflow.

Screenshots remain on disk for agents to read. Delete them from `attachments/`
when no longer needed. Text paste keeps the terminal's normal behavior.

Status indicators retain Kero's status colors but do not animate continuously.
The local ARM64 build avoids the Rosetta overhead observed in the installed
Intel app. This does not establish that every source of terminal lag is fixed.

Local.2 also fixes completed synchronized redraws waiting behind a 50ms retry:
new output brings that retry forward to the usual 4ms coalescing deadline.
The [Herdr study](herdr-porting.md) records the regression, benchmark limits and
recommended next ports. Apple Silicon is arm64; amd64 is Intel/AMD x86-64.

## Incorporate upstream changes

Keep feature work on its branch, with `origin` pointing to the personal fork
and `upstream` to the original repository:

```sh
git fetch upstream
git merge upstream/main
# Resolve conflicts, retaining the local app identity and disabled updates.
sh scripts/local-build.sh
```

Run tests and lifecycle vet with the repository's Go toolchain before updating
the local app. Source merges preserve custom features; upstream binary updates
are disabled because they would replace them. Do not publish the upstream
release workflow without configuring your own signing and release settings.
