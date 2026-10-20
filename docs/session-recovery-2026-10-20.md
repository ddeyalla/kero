# Session recovery — 2026-10-20

The user authorized restarting Shibu's terminal service and restoring sessions
after a test daemon had replaced its Unix socket address.

## Cause and recovery

The test used its own `KERO_HOME` but inherited the daily app's `KERO_SOCKET`.
The older daemon startup code unlinked that address before listening. Existing
GUI connections survived, but new clients could not reach the original daemon.
The “Can't reach kerod” screenshot at 21:23 was taken during this recovery,
before the replacement service was ready. It is not the final verified state.

The personal GUI was closed, and the original daemon was stopped with SIGTERM
so it could save state and terminal histories. The stray test daemon had already
exited. Remaining old terminal processes were ended before resuming their
conversations. The original Kero application and a separate audit daemon were
not targeted.

The installed Shibu Code `2.0.5-local.7` was reopened with the original window
selection. The service restored all three sessions and six panes. Claude and
Codex used their saved conversation IDs. The two OpenCode IDs were recovered
from the local session index and associated with their known original panes.

One OpenCode pane closed shortly after restoration. On the user's request it
was recreated with its previous conversation and backed-up terminal history,
and the original split layout was reapplied. That replacement has a new pane
ID. The other five shell PIDs stayed unchanged during this additional repair.

## Verification and backup

Verified against the live daemon and native app window:

- The daemon reports `2.0.5-local.7` on the personal socket.
- Three original session IDs are present, with six live panes.
- The recreated pane runs the intended OpenCode conversation.
- Terminal settings and usage-credential fingerprints match the pre-update
  baseline. Provider secrets were not printed or committed.
- The sidebar update button is visible in the running app.

Local backup, including state, preferences, terminal config and histories:

```text
~/Library/Application Support/kero-local/backups/before-authorized-restart-20261020-212107
```

The backup also retains state and histories saved by the clean service
shutdown, before OpenCode resume metadata was added. The private verification
report is `/tmp/shibu-restart-report.json`.

The preventive socket guard is tested and committed on the fork's
`fix/shibu-socket-isolation` branch (`78de2fc`). It is not part of the installed
local.7 binary. Future test runs must still isolate all Kero environment
overrides; restarting alone does not change older test binaries' behavior.
