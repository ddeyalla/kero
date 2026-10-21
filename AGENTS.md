# Shibu Code personal fork

This repository is the user's personal fork of Kero, with the new name **Shibu Code**.
Shibu Code is a macOS terminal for coding agents, written in Go with MyGo (`go tool mygo`).
The customizations are for the user's own use.

Start with [docs/README.md](docs/README.md). Two other documents help:

- The [feature tracker](docs/features.md) lists what Shibu adds.
- The [changelog](docs/changelog.md) lists what each release shipped.

## Direction: everything is a pane or a plugin

Shibu Code moves towards an app where every capability is a pane or a plugin.
In that app, the user:

- adds plugins to their panes;
- uses the plugins side by side with terminals and agents;
- writes new plugins of their own.

Plan and build towards that app:

- Today a pane is a terminal or one of the built-in views: Tasks, Files and
  Browser. See `pane_views.go`, and `Pane.View` and `Pane.Target` in
  `internal/proto/proto.go`.
- kerod keeps each view and its target, so the view comes back after a
  restart. Agents open views with `kero split`.
- There is no plugin API yet. These views are the first steps towards it.
- Shape a new capability as a pane, or as a plugin that a pane can host.
  Prefer this to a special screen, modal or sidebar feature.
- Give the new capability what the built-in views have:
  - its state in the pane's target, which comes back after a restart;
  - access from the New Pane chooser, a shortcut, the command palette and the
    `kero` CLI.
- Keep the code of a view self-contained behind the shared pane seam. Then each
  built-in view can become a plugin later, and a plugin can do what a built-in
  view does.
- When you plan, tell how the work fits this direction.
- If a plan works against this direction, push back. Examples of such a plan:
  - a one-off screen outside panes;
  - a feature coupled into the core that could be a pane;
  - a design that makes a later plugin API harder.
- Offer the pane- or plugin-shaped alternative. Let the user decide.
- Some older planning notes rule out plugins or a plugin SDK. These notes are
  older than this direction, and this section takes precedence.

## Fork safety

- Never push commits, branches or tags to the original repository, `egoist/kero`.
  This rule applies to every remote, direct URL, CLI, API and automation.
- Never open upstream pull requests.
- Never publish these customizations to the original repository.
- Use `upstream` only to read and fetch Kero updates. Keep its disabled push URL.
- The user's fork is `ddeyalla/kero` (`origin`). This file calls it "the fork".
- Any authorized push or release must target the fork.
- Before you publish, verify the destination.
- The active GitHub CLI account may not have push access to the fork. Push with
  the fork owner's token for that one command:
  `git -c credential.helper= -c credential.helper='!f() { echo username=ddeyalla; echo "password=$(gh auth token --user ddeyalla)"; }; f' push origin <branch>`
- Never switch the global `gh` account to push.
- When you merge upstream updates, keep Shibu's app identity and personal
  customizations:
  - the name;
  - the icon;
  - the bundle ID `dev.ddeyalla.kero`;
  - the data identity `kero-local`;
  - the CLI name `kero`;
  - the Shibu-only update feed.

## Branches

- `shibu` is the maintained release branch. The daily checkout stays on `shibu`.
- The fork's `main` is an old upstream mirror. Do not touch it.
- Do feature work on its own branch off `shibu` (`feat/<name>`, `fix/<name>`).
  If you can, use a separate worktree.
- Keep unfinished work out of release commits.
- Merge a finished branch into `shibu`. After its release, delete the branch
  locally and on the fork.
- Before you delete a branch or worktree, make sure that:
  - its commits are in `shibu`;
  - it has no uncommitted work.

  If something is missing, bring it over first.
- Commit or push only when the user asks.
- You may commit documentation-only changes directly to `shibu`.

## Making a change

- Write code like the code around it. Follow Kero's prose comments, naming and
  MyGo idioms.
- Where possible, keep changes in Shibu-owned files. This makes upstream merges
  easier.
- Put user-visible strings in `locales/en.json`. Then run `go tool mygo generate`.
- Add or update tests with the change.
- Test the UI with MyGo's tester (see `pane_details_test.go`).
- Keep heavy work off the main thread.
- Before you commit, run the checks:
  ```sh
  env PATH="/usr/bin:/bin:/usr/sbin:/sbin:$PATH" CGO_ENABLED=0 go test ./...
  go tool mygo vet .
  python3 -m unittest discover -s scripts -p 'test_*.py'
  ```
  The system-first `PATH` is important, because a developer zsh breaks two
  shell tests.
- Document every user-visible change in the same commit:
  - `docs/changelog.md` under **Unreleased** (Added, Fixed or Documentation);
  - a row in `docs/features.md` (feature, how to reach it, release, status);
  - how it works and where its code lives, in `docs/changes.md`;
  - shortcuts or commands, in the relevant guide.
- The root `CHANGELOG.md` belongs to upstream. Put only Shibu's header and
  upstream release notes there.
- Never claim an install or a hand test that has not happened.

## Versions and releases

- `mygo.json` holds the version, `<kero base>-local.<n>` (see
  [versioning](docs/versioning.md)).
- Never bump the version by hand for a workflow release. The workflow bumps it
  and moves **Unreleased** into a dated section.
- Never cut a release for documentation-only changes.
- Release only when the user asks. Release from `shibu` on the fork:
  1. Start the workflow:
     `gh workflow run shibu-update.yml --repo ddeyalla/kero --ref shibu -f mode=release -f publish=true`
  2. Watch it:
     `gh run list --repo ddeyalla/kero --workflow shibu-update.yml`

  The workflow does these steps:
  - It runs the secret scan and tests.
  - It builds and signs the ARM64 app.
  - It tags `shibu-v<version>`.
  - It publishes the update that the installed app downloads.
- If a release changes the daemon protocol (`internal/proto/proto.go`), the user
  must restart the terminal service once. Say so in the changelog entry of that
  release.
- A change to saved state (`internal/daemon/store.go`) needs a reviewed migration.

## Upstream Kero updates

- The `shibu-update.yml` workflow checks Kero's latest stable release daily. If
  the release merges cleanly, the workflow merges, tests and releases it.
- The workflow stops for review in these cases:
  - a conflict other than in `mygo.json`;
  - a protocol or saved-state change;
  - an icon change.
- The in-app update button can open a Codex repair session in its own worktree.
  The commits of that session need review before release.
- To merge by hand, do these steps:
  1. Use a `merge/kero-<version>` branch off `shibu` in its own worktree.
  2. Keep Shibu's side of the identity files.
  3. Port any MyGo API changes.
  4. Set `shibu-upstream.json` to the new tag, and commit.
  5. Add the upstream notes to `CHANGELOG.md`.
  6. Add an **Upstream** entry to `docs/changelog.md`.
  7. Before you merge into `shibu`, run the full checks and an isolated dev app.

## The daily app and test apps

- The user runs the installed Shibu Code all day. This file calls it "the daily
  app". Agents often run inside the daily app (`KERO_SESSION` is set).
- Never restart, stop or replace the daily app or its `kerod`.
- Never end the sessions of the daily app.
- Only the user schedules service restarts.
- Before you start any test app or daemon, clear or isolate `KERO_HOME`,
  `KERO_CONFIG`, `KERO_SOCKET`, `KERO_PANE` and `KERO_SESSION`.
- A test data directory alone does not stop an inherited socket from targeting
  the daily app.
- Run a dev build from the checkout that built it (`.mygo/dev`). A binary from
  another checkout can fail to match the daemon version.
- Screen capture is not available. Verify the UI with the tester's shots and the
  state of the isolated app.

## Secrets and private files

- Install the guards of this clone with `sh scripts/install-git-guards.sh`.
- Never bypass a secret-scan failure.
- Keep keys, private runtime state and raw research transcripts out of commits.
- Review and stage the exact files that the feature owns.
- Never print a credential.
- Read the Linear key only inside a command, as
  `$(cat ~/.config/shibu/linear.key)`.
- Tests never touch real Linear data. The read-only live test runs only with
  `SHIBU_LINEAR_LIVE_TEST=1`.
- `plans/`, `docs/research/`, `.agents/skills/`, `.claude/skills/` and
  `skills-lock.json` are the user's private working files. Do not commit them
  unless the user asks.

## Tasks

Work is tracked in Linear. If a task has an issue (`BRU-<n>`), do these steps:

1. Before you start, link the session with
   `kero task start <issue> --session $KERO_SESSION`.
2. As the work moves forward, move the issue forward too
   (`kero linear set-state <issue> "In Review"`).

Do not create issues unless the user asks.
