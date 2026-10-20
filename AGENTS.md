# Shibu Code personal fork

This repository is the user's personal fork of Kero, renamed **Shibu Code**.
Customizations are for the user's own use.

- Never push commits, branches, or tags to the original repository, `egoist/kero`,
  through any remote, direct URL, CLI, API, or automation.
- Never open upstream pull requests or publish these customizations to the
  original repository.
- `upstream` is for reading and fetching Kero updates only. Preserve its disabled
  push URL.
- The user's fork is `ddeyalla/kero` (`origin`). Any authorized push or release
  must target that fork. Verify the destination before publishing.
- Keep Shibu's app identity and personal customizations when incorporating
  upstream updates.
- The maintained release branch on the fork is `shibu`. Keep unfinished feature
  work on its own branch and out of release commits.
- Install this clone's guards with `sh scripts/install-git-guards.sh`. Never
  bypass a secret-scan failure. Keep keys, private runtime state and raw research
  transcripts out of commits; review and stage exact feature-owned files.
- Before launching any test app or daemon, clear or isolate `KERO_HOME`,
  `KERO_CONFIG`, `KERO_SOCKET`, `KERO_PANE` and `KERO_SESSION`. A test data directory
  alone does not prevent an inherited socket from targeting the daily app.
