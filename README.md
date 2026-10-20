# Kero

**Shibu Code personal fork:** see the [documentation hub](docs/README.md),
[local changelog](docs/changelog.md), [build guide](docs/build.md) and
[versioning policy](docs/versioning.md). The original Kero overview follows.

A terminal for coding agents, on macOS, Windows and Linux.

Run Claude Code, Codex, a dev server or a test watcher each in its own session. Kero groups sessions by project in a sidebar that shows which are working, which need you and which are done.

- **Sessions outlive the window.** They run in `kerod`, a small daemon, so quitting Kero leaves them running. Reopen it and every pane and its scrollback is back.
- **Status at a glance.** Each pane is working, waiting, idle, error or exited. <kbd>⌘</kbd> <kbd>J</kbd> jumps to the one that needs you, and Kero notifies you when an agent finishes, fails or is blocked.
- **A CLI agents can drive.** `kero` starts sessions, splits panes, types into them, waits on them and reads their screens.
- **Ghostty under the hood.** Terminals render with libghostty, so Ghostty themes, fonts and config keys just work.

## Install

Download Kero from the [releases page](https://github.com/egoist/kero/releases), then choose **Kero › Install Command Line Tool…** to put `kero` on your `PATH`.

```sh
kero new -n fix-login -- claude
```

See the [docs](docs/index.md) for more.

## Prior art

- [**Rex**](https://www.superlogical.com/rex/docs/installation): Mitchell Hashimoto's terminal and multiplexer in one native app, with sessions that persist through a server and a scriptable CLI.
- [**Herdr**](https://github.com/herdrdev/herdr): a terminal multiplexer for running many coding agents in one terminal, with agent status detection.

## License

MIT
