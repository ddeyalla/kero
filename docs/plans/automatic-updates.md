# Automatic Shibu updates

## Goal

Deliver stable Kero releases with Shibu customizations and existing local
configuration, without interrupting daily coding.

## Implementation plan

1. Make `shibu` the fork's maintained/default branch. Keep original Kero's
   `main` branch available separately.
2. Use one daily/manual GitHub Actions workflow to prepare a disposable merge
   candidate from the latest stable upstream release. Preserve Shibu's app
   identity, icon, updater feed and personal data identity.
3. Stop on conflicts, divergent history, daemon protocol or persistence
   changes. Run release guard tests, the application suite and MyGo vet.
4. Build an ARM64 Mac app with an Ed25519-signed update archive. Keep the
   signing secret in GitHub Actions and a private local backup, never Git.
5. Publish only after checks pass, using `shibu-v` release tags. Use a draft
   release until all assets are uploaded. Never force-push over concurrent
   customization work.
6. Exercise an actual release check/download/install in disposable probe
   bundles, including rejection of an incorrect signing key.
7. Replace the installed bundle without restarting the current GUI, daemon
   or shells. Enable automatic checks/downloads for this Mac. The new updater
   starts on the user's next normal app launch; daemon restarts remain explicit.

## Boundaries

Stable upstream releases are the automation input; unreleased upstream main
commits are not installed automatically. Test/build failures are visible in
GitHub Actions. This workflow does not automatically resolve conflicts or
apply session migrations. Existing binary versions cannot be made to run
new updater code until the app is relaunched by the user.
