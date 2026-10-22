# Secret protection and security checks

Shibu's source and docs may be pushed only to `ddeyalla/kero`. Personal keys,
sessions, raw research transcripts and local configuration stay on the Mac.

## Installed protections

- Repository-local `core.hooksPath=.githooks` runs Gitleaks before commits and
  pushes. Missing/broken scanners fail closed. Matching values are not printed.
- The commit hook scans the actual index, so cleaning the working file after
  staging a key does not hide it. Private credential filenames are blocked too.
- The push hook scans the full history reachable from every outgoing revision.
  A key committed and later deleted is still blocked, including on new branches
  and tags. Destinations are restricted to the exact HTTPS/SSH forms of the
  personal fork; the original Kero repository is rejected independently of its
  remote name.
- The default Gitleaks rules remain enabled, with additional Linear and AI-key
  patterns. A narrow exception covers keyboard accelerator values such as
  `CmdOrCtrl+Shift+U`; no entire source files are exempted.
- GitHub secret scanning and push protection were verified enabled on the fork.
  A read-only CI workflow scans history on pushes/PRs, and the release workflow
  waits for that check before publishing.
- `.gitignore` excludes `.env` files, private key containers, credential stores,
  Python bytecode and raw research probe output. The hook also rejects private
  files force-added despite ignore rules.

New clones must install the local hooks:

```sh
brew install gitleaks
sh scripts/install-git-guards.sh
python3 scripts/secret_guard.py history
python3 -m unittest discover -s scripts -p 'test_secret_guard.py'
```

An existing custom hooks path is preserved: the installer stops so the hooks can
be merged deliberately. Repository configuration does not travel with a clone.

## Review completed for Linear v1

The initial history scan reported ten keyboard-shortcut false positives. After
adding the value-specific exception, reachable history passed. Source and raw
probe output were also checked locally; bytecode containing a synthetic test
token was identified and excluded from Git. No real leaked API key was found
by these scans.

The feature clears credential inputs, uses atomic private credential writes,
preserves malformed existing stores, rejects credential-bearing HTTP redirects,
bounds API responses, and never forwards remote response bodies to logs/state.
Remote issue links are limited to ordinary web URLs. Dependency reachability
scanning with `govulncheck` found no known vulnerabilities in the checked build.

The socket startup guard prevents test daemons using a different data directory
from replacing a live Shibu socket. Regression tests cover both legacy listeners
and daemons using the new address lock. Every test app also isolates all five
`KERO_HOME`, `KERO_CONFIG`, `KERO_SOCKET`, `KERO_PANE`, `KERO_SESSION` variables.

## Linear as a plugin

The Linear key stays in core. The daemon reads `credentials.json` or
`LINEAR_API_KEY` and makes every request to Linear; the Linear plugin never
reads, receives or logs the key. The plugin is a client of the daemon, over the
same socket as the CLI: it reads the state the daemon holds and asks it to set
an issue's state, comment, load an issue or link a session. That shared socket
is a privilege of the plugins that ship in the binary. Withholding it from a
third-party plugin is a policy, not a boundary: a plugin is a native
executable that the host starts from the manifest's `run` argv
(`internal/plugin/host/host.go`, `exec.Command`), with the user's own rights,
so any process of the same user, a plugin included, can open the daemon's
socket. Until plugins run in a sandbox, treat an installed plugin as trusted
code of the user, and do not count on the socket's absence to keep it from
the daemon or the Linear key's requests. A permission the app mediates would
be the way to give a third-party plugin the daemon.

The plugin opens links only through the `openURL` permission, and only `http`
and `https` with a host and no username (`linearSafeURL` and the plugin's own
`safeURL` refuse the rest, and `linearSafeURL` also guards the Files pane).
It hears which session is selected only with the `focus` permission. Tests
never touch real Linear data.

The `clipboard` permission lets a plugin put text on the clipboard
(`clipboard.write`, up to 64 KB). It is write-only: a plugin cannot read the
clipboard. The host takes a write only within about one second after it
delivered a user event to that plugin (a press, a key, a menu choice or a
command; `userAt` and `userActionWindow` in `host.go`). A write at any other
time is refused and logged, so a plugin cannot replace what the user copied
in the background. The Linear plugin uses it for Copy ID and Copy Link.

## Limits

Local hooks can be disabled by someone with write access to the clone. GitHub
push protection covers supported secret patterns, not every possible password.
No scanner guarantees that an unknown/custom credential will be detected. Keep
credentials outside the repository and review staged files before committing.
CI runs after upload and is an additional check, not the pre-push barrier.

If a real secret is found: stop the push, revoke/rotate it at its provider, then
remove it from all outgoing commits. Removing only the current file is not
sufficient. Never paste a replacement key into chat or a Git command argument.
