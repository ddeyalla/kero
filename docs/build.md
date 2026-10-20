# Build, verify and install Shibu Code

Run commands from the repository root. Version policy and release steps are
in [Versioning](versioning.md); the current runtime version is in
[`mygo.json`](../mygo.json).

## Requirements

- macOS with Xcode Command Line Tools for the native build (`xcode-select -p`
  checks the selected tools).
- A Go toolchain meeting `go.mod` (currently Go 1.27.1 or later). This checkout
  has Go 1.27.3 under `.toolchain/go/bin/go`.
- Network access for missing Go/MyGo dependencies on the first build.

MyGo is pinned in `go.mod`/`go.sum`; use `go tool mygo`, not an independently
installed CLI. `.toolchain/` and build outputs are local, ignored directories.

If using the checkout's bundled Go, configure the current shell:

```sh
export PATH="$PWD/.toolchain/go/bin:$PATH"
export GOPATH="$PWD/.toolchain/gopath"
export GOCACHE="$PWD/.toolchain/gocache"
go version
go env GOHOSTOS GOHOSTARCH
```

On Apple Silicon, expect `darwin` and `arm64`. An Intel toolchain running under
Rosetta can report `amd64`; use a native ARM64 Go toolchain in that case.

## Verify

```sh
env PATH="/usr/bin:/bin:/usr/sbin:/sbin:$PATH" CGO_ENABLED=0 go test ./...
go tool mygo vet .
git diff --check
```

The PATH order selects system zsh for subprocess tests; the Homebrew-zsh
environment previously failed two existing daemon tests. No global shell
configuration needs changing. Use focused race/UI checks when the relevant
code changes; see the [verification record](local-v1-verification.md).

## Build

```sh
sh scripts/local-build.sh
```

[`local-build.sh`](../scripts/local-build.sh) selects the bundled Go when
present and builds for its host OS/architecture. It signs ad hoc and skips
DMG creation and notarization. On this Mac the output is:

```text
build/darwin-arm64/Shibu Code.app
```

Inspect the bundle before installing:

```sh
APP="build/darwin-arm64/Shibu Code.app"
file "$APP/Contents/MacOS/Shibu Code"
/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$APP/Contents/Info.plist"
codesign --verify --deep --strict "$APP"
```

The bundle version must match `mygo.json`. This local ad-hoc build is not a
signed/notarized public release. Do not use the inherited upstream release
workflow until personal signing, publishing and update settings are configured.

## Install or replace the personal app

Replacing a bundle requires a clean application restart. Save work first;
when a daemon restart is needed, let agent tasks reach a safe stopping point.
Use the app's normal quit action. Do not force-kill processes or stop the
daemon as a routine copy step. Existing daemon behavior and version changes
can require a restart; shell PIDs are not guaranteed to survive an upgrade.

After the GUI has exited, keep the previous bundle for rollback, then install:

```sh
mkdir -p "$HOME/Applications" "$HOME/Library/Application Support/kero-local/backups"
BACKUP="$HOME/Library/Application Support/kero-local/backups/app-$(date +%Y%m%d-%H%M%S)"
if [ -d "$HOME/Applications/Shibu Code.app" ]; then
  mkdir -p "$BACKUP"
  mv "$HOME/Applications/Shibu Code.app" "$BACKUP/Shibu Code.app"
fi
ditto "build/darwin-arm64/Shibu Code.app" "$HOME/Applications/Shibu Code.app"
codesign --verify --deep --strict "$HOME/Applications/Shibu Code.app"
open "$HOME/Applications/Shibu Code.app"
```

Check the app version, existing sessions, usage modal, meters and icon. If the
app reports a daemon version mismatch, use its Restart Service action after
tasks are safe to restart. The original `/Applications/Kero.app` is separate.
Use Shibu's executable directly to avoid an older `kero` on PATH:

```sh
"$HOME/Applications/Shibu Code.app/Contents/MacOS/Shibu Code" resources --json
```

For rollback, quit Shibu and restore the saved bundle to
`~/Applications/Shibu Code.app`. A bundle backup does not back up sessions or
settings. Before any future data migration, stop the personal daemon safely
and separately back up its data/settings; do not restore mutable session state
while its daemon is running. Never commit credentials, histories or backups.
