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

## On the Mac

Shibu's existing Settings → General → Updates controls enable automatic
checks and downloads. The updater checks shortly after launch and daily while
running. Automatic download replaces the app bundle in the background and
uses it on the next launch. It never automatically relaunches the app or
restarts the daemon/shells. Manual checks can offer a restart, which you can defer.

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
