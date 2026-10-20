# Shibu Code personal fork

This repository is the user's personal fork of Kero, renamed **Shibu Code**:
a macOS terminal for coding agents, written in Go with MyGo (`go tool mygo`).
Customizations are for the user's own use. Start with
[docs/README.md](docs/README.md); the [feature tracker](docs/features.md) lists
what Shibu adds and the [changelog](docs/changelog.md) what each release shipped.

## Direction: everything is a pane or a plugin

Shibu Code is heading towards an app where every capability is a pane or a
plugin. The user adds plugins to their panes, uses them side by side with
terminals and agents, and writes new plugins of their own. Plan and build
towards that:

- Today a pane is a terminal or one of the built-in views: Tasks, Files and
  Browser (`pane_views.go`, `Pane.View` and `Pane.Target` in
  `internal/proto/proto.go`). kerod keeps each view and its target, so it
  comes back after a restart, and agents open views with `kero split`. There
  is no plugin API yet; these views are the first steps towards it.
- Shape a new capability as a pane, or as a plugin a pane can host, rather
  than a special screen, modal or sidebar feature. Give it what the built-in
  views have: its state in the pane's target, restored after a restart, and
  reachable from the New Pane chooser, a shortcut, the command palette and the
  `kero` CLI.
- Keep a view's code self-contained behind the shared pane seam, so each built-in
  view can later become a plugin and a plugin can do what a built-in view does.
- When planning, say how the work fits this direction. Push back when a plan
  works against it: a one-off screen outside panes, a feature coupled into the
  core that could be a pane, or a design that would make a later plugin API
  harder. Offer the pane- or plugin-shaped alternative and let the user decide.
- Older planning notes that rule out plugins or a plugin SDK predate this
  direction; this section takes precedence.

## Fork safety

- Never push commits, branches, or tags to the original repository, `egoist/kero`,
  through any remote, direct URL, CLI, API, or automation.
- Never open upstream pull requests or publish these customizations to the
  original repository.
- `upstream` is for reading and fetching Kero updates only. Preserve its disabled
  push URL.
- The user's fork is `ddeyalla/kero` (`origin`). Any authorized push or release
  must target that fork. Verify the destination before publishing.
- The active GitHub CLI account may not have push access to the fork. Push with
  the fork owner's token for that one command, never by switching the global
  `gh` account:
  `git -c credential.helper= -c credential.helper='!f() { echo username=ddeyalla; echo "password=$(gh auth token --user ddeyalla)"; }; f' push origin <branch>`
- Keep Shibu's app identity and personal customizations when incorporating
  upstream updates: the name, icon, bundle ID `dev.ddeyalla.kero`, data identity
  `kero-local`, CLI name `kero` and the Shibu-only update feed.

## Branches

- `shibu` is the maintained release branch, and the daily checkout stays on it.
  The fork's `main` is an old upstream mirror; leave it alone.
- Do feature work on its own branch off `shibu` (`feat/<name>`, `fix/<name>`),
  ideally in a separate worktree. Keep unfinished work out of release commits.
- Merge a finished branch into `shibu` and delete it, locally and on the fork,
  once it is released. Before deleting any branch or worktree, check that its
  commits are in `shibu` and that it has no uncommitted work; bring over what
  is missing first.
- Commit or push only when the user asks. Documentation-only commits may go
  straight to `shibu`.

## Making a change

- Write code like the code around it: Kero's prose comments, naming and MyGo
  idioms. Keep changes in Shibu-owned files where possible to ease upstream
  merges.
- User-visible strings go in `locales/en.json`; then run `go tool mygo generate`.
- Add or update tests with the change. UI is tested with MyGo's tester (see
  `pane_details_test.go`); heavy work belongs off the main thread.
- Run the checks before committing:
  ```sh
  env PATH="/usr/bin:/bin:/usr/sbin:/sbin:$PATH" CGO_ENABLED=0 go test ./...
  go tool mygo vet .
  python3 -m unittest discover -s scripts -p 'test_*.py'
  ```
  The system-first `PATH` matters: a developer zsh breaks two shell tests.
- Document every user-visible change in the same commit:
  - `docs/changelog.md` under **Unreleased** (Added, Fixed or Documentation);
  - a row in `docs/features.md` (feature, how to reach it, release, status);
  - how it works and where its code lives in `docs/changes.md`, and shortcuts
    or commands in the relevant guide.
  The root `CHANGELOG.md` is upstream's; only Shibu's header and upstream
  release notes go there. Never claim an install or a hand test that has not
  happened.

## Versions and releases

- `mygo.json` holds the version, `<kero base>-local.<n>` (see
  [versioning](docs/versioning.md)). Never bump it by hand for a workflow
  release; the workflow does it and moves **Unreleased** into a dated section.
- Documentation-only changes never cut a release.
- Release only when the user asks, from `shibu` on the fork:
  `gh workflow run shibu-update.yml --repo ddeyalla/kero --ref shibu -f mode=release -f publish=true`
  Then watch it with `gh run list --repo ddeyalla/kero --workflow shibu-update.yml`.
  It runs the secret scan and tests, builds and signs the ARM64 app, tags
  `shibu-v<version>` and publishes the update the installed app downloads.
- A release that changes the daemon protocol (`internal/proto/proto.go`) needs
  the user to restart the terminal service once; say so in its changelog entry.
  A change to saved state (`internal/daemon/store.go`) needs a reviewed migration.

## Upstream Kero updates

- The `shibu-update.yml` workflow checks Kero's latest stable release daily and
  merges, tests and releases it when it merges cleanly. It stops for review on
  conflicts other than `mygo.json`, on a protocol or saved-state change, or on
  an icon change. The in-app update button can open a Codex repair session in
  its own worktree; its commits need review before release.
- To merge by hand, use a `merge/kero-<version>` branch off `shibu` in its own
  worktree. Keep Shibu's side of identity files, port any MyGo API changes, set
  `shibu-upstream.json` to the new tag and commit, and add the upstream notes
  to `CHANGELOG.md` and an **Upstream** entry to `docs/changelog.md`. Run the
  full checks and an isolated dev app before merging into `shibu`.

## The daily app and test apps

- The user runs the installed Shibu Code all day, and agents often run inside
  it (`KERO_SESSION` is set). Never restart, stop or replace the daily app or
  its `kerod`, and never end its sessions. Service restarts are the user's to
  schedule.
- Before launching any test app or daemon, clear or isolate `KERO_HOME`,
  `KERO_CONFIG`, `KERO_SOCKET`, `KERO_PANE` and `KERO_SESSION`. A test data directory
  alone does not prevent an inherited socket from targeting the daily app.
- Run a dev build from the checkout that built it (`.mygo/dev`); a binary from
  another checkout can mismatch the daemon version.
- Screen capture is not available; verify UI with the tester's shots and the
  isolated app's state.

## Secrets and private files

- Install this clone's guards with `sh scripts/install-git-guards.sh`. Never
  bypass a secret-scan failure. Keep keys, private runtime state and raw research
  transcripts out of commits; review and stage exact feature-owned files.
- Never print a credential. Read the Linear key only inside a command, as
  `$(cat ~/.config/shibu/linear.key)`. Tests never touch real Linear data; the
  read-only live test runs only with `SHIBU_LINEAR_LIVE_TEST=1`.
- `plans/`, `docs/research/`, `.agents/skills/`, `.claude/skills/` and
  `skills-lock.json` are the user's private working files. Leave them
  uncommitted unless asked.

## Tasks

Work is tracked in Linear. When a task has an issue (`BRU-<n>`), link the
session with `kero task start <issue> --session $KERO_SESSION` before starting,
and move the issue on as the work does (`kero linear set-state <issue> "In Review"`).
Don't create issues unasked.
