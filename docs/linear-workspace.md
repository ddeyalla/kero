# Linear board and session links

This v1 finishes the existing Linear board and local session links. The Home
composer, task creation, milestone worktrees and queued writes remain separate
work, as requested. This feature is developed on `feat/linear-workspace-v1`;
the daily app is not restarted or replaced during verification.

## Use it

Open **Linear** in the sidebar or press **Cmd+Shift+L**. Choose Board or List,
search by issue identifier/title, and filter by project, milestone and state.
The view and filters survive window relaunch. Removed projects or milestones
fall back to their parent with an explanation.

Click an issue for its description, activity, attachments and related issues.
On narrow windows, detail uses the main card; Close returns to the board.
Web links open in the browser. Remote issue content cannot open local files or
custom command URL schemes; Markdown images do not fetch automatically.

Choose an existing session in the issue detail and click **Link session**.
The detail can open that session, copy its resume ID when known, or unlink it.
This changes local Shibu metadata; it does not change the issue in Linear.
Explicit links survive daemon restarts alongside the agent resume metadata.

Agents can report their task without deriving it from the branch:

```sh
kero link <session> TEAM-123
kero report working "Checking the change" --id TEAM-123 --app linear --progress 40
kero link <session> # remove the explicit link
```

OSC reports are live associations. Clearing a report removes that association;
removing an explicit link does not clear a separate report still emitted by an
agent. An issue's badge shows the linked session and reported progress.

## Connect and rotate a key

In Settings → Agents, enter a Linear personal API key in the masked field,
save it, or use **Remove Linear key**. The input clears after saving and when
Settings closes. The key stays in `credentials.json` under the
personal app data directory, written atomically with file mode `0600`.
It is absent from preferences, session state and source control.

`LINEAR_API_KEY` overrides the saved key; `LINEAR_TEAM` selects the team by key
or ID. Otherwise Shibu reads the first accessible team. Removing a saved key
does not remove an environment override. Keys are reloaded for each operation.

CLI authentication accepts stdin rather than a secret command argument:

```sh
kero linear auth --stdin # pipe from a password manager; do not paste into shell history
kero linear auth --remove
kero linear status
kero linear board --json
kero linear issue TEAM-123 --json
```

A key previously pasted into chat should be rotated in Linear and replaced
locally. Secret scanning cannot revoke a credential or erase a transcript.

## Reliability and limits

- Polls run in the daemon, outside its state mutex: deltas every 30 seconds,
  full refreshes every five minutes. Manual refreshes are coalesced.
- Full reads paginate issues, projects and milestones. Issue details paginate
  comments, attachments, relations and children. Fractional workflow positions
  and inverse dependency relations are handled.
- Delta watermarks precede requests, preventing edits during a request from
  falling into a gap. A full refresh also removes deleted/archived issues.
- Offline/error states retain the last board in private session state. Rate
  limits respect reset headers; authentication failures back off. Requests have
  deadlines. No failure restarts the daemon or closes a terminal.
- An open issue refreshes after ten seconds following a successful read. Failed
  reads stop that loop and show Retry. Late responses cannot replace a different
  issue or reopen a closed detail pane.
- Board cards summarize up to ten relations in each direction. Open detail for
  the complete dependency list. A badge currently shows one associated session;
  a list of multiple sessions on the same issue is future work.
- The existing explicit CLI `set-state` and `comment` commands perform one
  request. They are not automatically queued/replayed after an uncertain result.
  No real Linear mutation is sent during verification.

The wire additions are optional fields on protocol 2 and state schema 2. An old
daemon can keep existing terminals running but cannot serve the new Linear RPCs.
Activating this feature in the daily app requires a planned daemon restart;
restarting shells is not a seamless live handoff. Build/test success alone never
authorizes ending the user's active work.

## Verification

Verification includes the full Go suite, MyGo vet, race checks for the client
and daemon, real read-only workspace queries, native preview inspection, and
secret-guard fixtures. See [Security checks](security.md) for the local hooks,
GitHub protection and their limits. The native preview uses its own bundle ID,
data directory and socket, with automatic app updates disabled.

API behavior follows Linear's [pagination](https://linear.app/developers/pagination)
and [rate-limit](https://linear.app/developers/rate-limiting) documentation.
