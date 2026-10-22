# Automatic updates

Shibu tracks stable Kero releases and publishes combined builds with your
personal features. It downloads only Shibu releases from `ddeyalla/kero`,
never the original Kero application.

## How it runs

- Maintained/default branch: `shibu`. Original fork branch `main` remains separate.
- Daily upstream check: 03:23 UTC (08:53 India time), subject to GitHub scheduling delays.
- Release checks use a lightweight Linux runner; Mac build capacity is requested
  only when there is a candidate to verify and build.
- Input: the latest published, stable `egoist/kero` release, recorded in
  [`shibu-upstream.json`](../shibu-upstream.json).
- Clean candidates run release-guard tests, all application tests and MyGo vet.
  An ARM64 Mac build is published only when checks/build/signing pass.
- Releases use `shibu-v<version>`. App archive signatures use Shibu's own
  Ed25519 key; the private key is stored in GitHub Actions secrets and a private
  local directory. The app contains only the public key.
- Source publication refuses to overwrite customization commits made while
  the workflow ran. Releases remain drafts until all update assets are uploaded.

Workflow: [Shibu updates](https://github.com/ddeyalla/kero/actions/workflows/shibu-update.yml).
Run manually with **mode: check** to check upstream. To release personal
changes without a newer Kero release, use **mode: release**. **publish: false**
builds/tests a candidate artifact without changing the branch or update feed.
Scheduled checks publish automatically after passing the same gates.

## Manual update controls and Codex repairs

In **Settings > General > Updates**, **Check for App Updates** checks published
Shibu builds. **Check Upstream & Build** starts an immediate Kero source check
and shows its progress and last result. The source action is also in the
command palette and available as
`kero do updates.sync`. Repeated clicks while a check runs are ignored.
Work happens off the UI thread and the selected coding session stays selected.

The source action uses the existing GitHub CLI login for **ddeyalla**, dispatches only
the `shibu` branch's guarded workflow in `ddeyalla/kero`, and follows its result.
It never switches the default CLI account or stores that token. Install `gh`
and authenticate that account to use the source action. Ordinary signed app downloads
do not require `gh`.

- Success: open the existing signed update checker, which offers the available
  Shibu build. Installation and relaunch remain under its existing controls.
- Build/merge/test failure: keep the installed app and coding sessions running.
  Create a separate `repair/…` branch/worktree and a **Shibu update repair**
  Codex session in the background, without switching to it.
- Auth/network/cancellation errors: show the error; do not start a repair agent
  simply because the workflow could not be monitored.

The Codex prompt requires preserving custom features and compatibility, running
tests, and producing a repair commit for review. It explicitly forbids publishing,
merging to `shibu`, installing the app or restarting processes. These are agent
instructions, not a substitute for human review. The workflow cannot resolve
arbitrary conflicts or prove every behavior correct merely because tests pass.

This Mac's private `kero-local/source-checkout` file identifies the source repo
for repairs. Its origin must be `ddeyalla/kero`; upstream-origin checkouts are
rejected before fetching or creating worktrees. The source repo's current branch
and uncommitted work stay in place. Repair checkouts remain available for review;
remove them with `git worktree remove` only after their agent has finished.
Codex CLI must already be installed/authenticated. Repair sessions explicitly
use its `workspace-write` sandbox and `on-request` approval policy; its terminal
may ask for trust, network access or command approval. There is no automatic
publication of AI-written fixes.

## On the Mac

Shibu's existing Settings → General → Updates controls enable automatic
checks and downloads. The updater checks shortly after launch and daily while
running. Automatic download replaces the app bundle in the background and
uses it on the next launch. It never automatically relaunches the app or
restarts the daemon/shells. Manual checks can offer a restart, which you can defer.

A disposable native-app test verified that a MyGo app relaunch can retain a
daemon and shell with identical process IDs. This is a compatibility observation,
not a promise that sessions can never fail. Automatic window relaunch is not
enabled: this version avoids interrupting typing and requires a normal
user-initiated relaunch. Protocol/storage changes still require a manual review.

The updater preserves data outside the bundle:

- Sessions, histories and provider overrides: `~/Library/Application Support/kero-local`.
- Terminal settings: `~/.config/kero-local/config`.
- Updater preferences: `~/Library/Application Support/Shibu Code/updater.json`.

Existing versions with updates disabled need one updater-enabled bundle
installation and a normal app relaunch before they can begin background checks.
The bootstrap installation must leave active coding processes running.

## When automation pauses

Conflicts, divergent upstream history, protocol changes or changes to session
persistence require a manual review. Failed tests/builds stop publication.
The installed app keeps working; failure details appear in Actions. No automatic
conflict resolution, data migration or shell restart is attempted.

After review, resolve the upstream merge on `shibu`, verify compatibility and
update `shibu-upstream.json` to the reviewed tag/commit. Run a manual release.
Keep `name`, `identifier`, `icon`, updater feed and `kero-local` identity stable.

## Testing and recovery

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s scripts -p 'test_shibu_*.py'
python3 scripts/test-shibu-update.py
```

The second command builds temporary probe apps. It exercises the real MyGo
updater against the public feed: an incorrect key must be rejected without
replacing the probe; the correct key must download/install a signed release
into the probe and pass native signature verification. It never opens the
daily app or its daemon/state. Requires macOS ARM64 and this repo's Go toolchain.

To pause client updates, turn off automatic checks/downloads in Settings.
To pause publishing, disable the Shibu updates workflow in Actions. Bundle
rollback follows [Build and install](build.md). Rolling back a bundle does not
roll back data; leave live session state alone.

These are personal, ad-hoc macOS builds with cryptographically signed update
archives. Public Mac distribution/notarization would require separate Apple
signing configuration. Keep the private updater key backed up; future releases
must use the same key trusted by installed apps.

## Isolated test daemons and socket recovery

Every test process must clear or isolate `KERO_HOME`, `KERO_CONFIG`,
`KERO_SOCKET`, `KERO_PANE` and `KERO_SESSION`. Setting only `KERO_HOME` can
still direct a test daemon to the daily app's inherited socket address.

Daemon startup now takes a socket-level lock as well as the data-directory
lock. It refuses to replace a live listener, including older daemons without
that lock. Only an unchanged socket that refuses connections is reclaimed.
Shutdown removes only the socket inode created by that listener. This guard
needs a build containing the fix; already running binaries retain their old
behavior, and older external test binaries can still unlink paths themselves.

If an old test binary has already unlinked a live daemon's socket, its existing
GUI and terminal connections can keep working, but new connections cannot
reach that daemon. Stopping the test does not recreate the unlinked address.
The app has no supported way to rebind that existing listener without restarting
the terminal service. Keep the working window open, save/checkpoint running
programs, back up saved state/history and schedule a controlled restart. The
restart ends shell processes; restoring saved sessions is not process survival.
Never use a successful build as evidence that this live recovery has happened.

## Fork-only publishing

The commit history shown on `ddeyalla/kero` is your fork's history. Fetching or
merging `egoist/kero` copies its changes into that history; it sends nothing back.
The workflow runs only in `ddeyalla/kero` and pushes only to that checkout's
origin and Shibu release tags. The local upstream remote has its push URL set to
`DISABLED`. Keep it that way; upstream is read/fetch only. No upstream pull request
is created by this automation.

For daily source work, see [Keeping your Shibu changes](keeping-your-fork.md).
Finished code/docs belong on your fork; session data, credentials and personal
settings remain local. A source push is separate from publishing/installing a
new app build.
