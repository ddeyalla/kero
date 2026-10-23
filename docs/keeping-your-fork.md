# Keeping your Shibu changes

Keep finished source code and documentation on **your GitHub fork**,
[`ddeyalla/kero`](https://github.com/ddeyalla/kero). Keep sessions, credentials
and personal settings on your Mac. You do not need to send anything to the
original Kero repository to receive its updates.

## What each copy contains

| Location | Purpose |
| --- | --- |
| This local checkout | Edit and test changes; uncommitted work exists only here |
| Git commits | Save a local source checkpoint |
| `origin` — `ddeyalla/kero` | Your remote source backup, branches and Shibu releases |
| `upstream` — `egoist/kero` | Read/fetch original Kero updates; pushing is disabled |
| `shibu` on your fork | Maintained branch used by the daily update workflow |
| `~/Applications/Shibu Code.app` | Installed app; pushing source does not update it |

The fork is currently public. Upload source and docs only. Runtime data stays
outside the repository:

- Sessions, histories and provider overrides: `~/Library/Application Support/kero-local`.
- Terminal settings: `~/.config/kero-local/config`.
- Updater preferences: `~/Library/Application Support/Shibu Code/updater.json`.
- Private update-signing key: `~/.config/shibu-code/update-keys/`.

A source push does not back up those files or preserve running processes.
Keep their local backups separately; the last recovery is documented in
[Session recovery](session-recovery-2026-10-20.md).

## Save a finished change

Work on a feature/fix branch. Review and stage only files belonging to that
change, especially while another coding task has unfinished edits here:

```sh
git status --short
git diff
git add path/to/changed-file path/to/related-doc
git diff --cached
git commit -m "Describe the change"
git push -u origin HEAD
```

`commit` saves locally. `push` copies those commits to your fork. Neither
command installs an app or restarts a shell. The paths above are examples;
choose the actual reviewed files rather than adding the whole checkout.

When the feature is tested, merge it into **your fork's `shibu` branch**, from a
clean checkout/worktree. If using a pull request, its base repository must be
`ddeyalla/kero` and its base branch `shibu`. GitHub fork UI can default to the
original repository, so check those fields. No upstream pull request is needed.
Never force-push over changes made by the update workflow or another task.

## Receive Kero updates and install Shibu builds

The daily workflow reads stable Kero releases and combines them with committed
changes on `shibu`. It tests/builds/signs candidates and publishes only to your
fork. Conflicts and failed checks pause publication; they need a reviewed fix.
**Settings > General > Updates > Check Upstream & Build** starts the same
upstream check. **Check for App Updates** checks published builds separately.

For a personal feature release when Kero has no new release, run
[Shibu updates](https://github.com/ddeyalla/kero/actions/workflows/shibu-update.yml)
on `shibu` with **mode: release**. Use **publish: false** for a build trial or
**publish: true** to publish after checks pass. The workflow increments the
version; do not also increment it manually for that run. See
[Automatic updates](automatic-updates.md) and [Versioning](versioning.md).

Signed Shibu releases reach the local app through its update checker. Source
push, app download, GUI relaunch and terminal-service restart are separate
steps. Downloads do not automatically restart your coding sessions. Review
compatibility before a service restart because it ends running shell processes.

## Push safeguards

This checkout is configured with:

```text
remote.pushDefault = origin
origin push URL = https://github.com/ddeyalla/kero.git
upstream fetch URL = https://github.com/egoist/kero.git
upstream push URL = DISABLED
```

These settings make your fork the default destination and block a push through
the configured `upstream` remote. Local Git settings do not travel with a clone.
When cloning again, use your fork URL and reapply:

```sh
git config --local remote.pushDefault origin
git remote add upstream https://github.com/egoist/kero.git
git remote set-url --push upstream DISABLED
git remote -v
```

If `upstream` already exists, skip the `remote add` command. Use explicit
`git push origin …`; never push to an original-repository URL directly.
The repository's [AGENTS.md](../AGENTS.md) carries the same fork-only rule for
future coding agents.

## Recorded status — 2026-10-20

- Released features through `2.0.5-local.7` and the session-recovery record are
  committed on your fork's `shibu` branch.
- The tested socket startup guard is committed/pushed separately on
  `fix/shibu-socket-isolation` (`78de2fc`). It still needs integration and a new
  app release; it is not in the installed local.7 binary.
- The Linear feature has unfinished, uncommitted edits in this checkout. Those
  edits have not been included in these documentation pushes or a Shibu release.

Check `git status` and the GitHub branch history for the current state; this
dated list is a checkpoint, not a live status display.
