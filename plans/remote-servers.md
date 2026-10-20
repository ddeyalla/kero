# Plan: Kero on remote servers

Status: future plan, not started. Written 2026-10-20 after looking at Rex
(superlogical.com/rex), which ships a Linux server mode, multi-user servers and
remote access over Tailscale. Updated after 2.0 shipped for macOS, Windows
and Linux.

## Goal

Run sessions on another machine, such as a Linux dev box, a cloud VM or a
beefy desktop, and see them in the same Kero window as local ones: the same
sidebar, statuses, notifications, `kero` CLI and agent skill. Quitting Kero or
closing the laptop leaves remote agents running, as it does locally.

Not goals, for now: sharing one session between two people, a web client, an
iOS client.

## Where we start

kerod is already most of a server:

- It owns the sessions and runs them on their own. Quitting the app leaves them
  running.
- Each pane has its own headless libghostty-vt emulator (`internal/daemon/session.go`).
  An attach stream carries rendered diffs from `terminal.Mirror`, not raw PTY
  bytes. A client that falls behind is dropped and reattaches from a fresh
  frame. Both suit slow or lossy links better than forwarding a PTY.
- Control is newline-delimited JSON (`Hello`, then `{id, method, params}`), and
  attach uses binary frames (`internal/proto/frame.go`). Neither cares what
  carries it.
- `pty_linux.go` and `proc_linux.go` exist, and libghostty-vt ships for linux
  amd64 and arm64.

What ties it to one machine today:

- **Transport:** a Unix socket only (`proto.SocketPath`), mode 0600.
  - There's no authentication and no peer-credential check. The 0600 chmod
    comes after `listen`, so there's a short window before it applies.
- **Lifetime:** kerod exits 30s after its last session and connection close,
  and the app starts it on demand (`client.Connect` runs `kero __kerod`).
- **Packaging:** there's one binary. `main.go` imports the UI, and the daemon
  imports `mygo` only for `mygo.App.Version()`.
  - Releases ship for macOS, Windows and Linux, on arm64 and amd64. Linux gets
    a `.deb`, plus an `install.sh` that installs into `~/.local` without root.
  - Every one is the full app. On Linux it links GTK, so a headless box needs
    GTK installed just to run kerod.
- **The app assumes local paths:**
  - The Open Project dialog picks a local folder.
  - ⌘-clicking a file path runs `os.Stat` and opens the file locally.
  - Image paste gives programs local file paths.
  - `ShortDir` and display names compare against the local home.
- **Shell lookup:** `internal/shell/account_unix.go` reads `/etc/passwd`
  directly, so LDAP and SSSD accounts are missed.

## Phase 1: headless kerod on Linux

The smallest useful step: kerod running as a service on a Linux box, driven by
the `kero` CLI there. That already helps agents on that box, before any remote
UI exists.

1. **A daemon-only build** (`cmd/kerod`, or a build tag) without the UI
   packages.
   - Pass the version in with `-ldflags` instead of `mygo.App.Version()`.
   - Check that it starts on a machine with no display.
2. **`kero daemon serve`:** run in the foreground, with no idle exit, logging
   to stderr. This is the entry point for systemd.
3. **`kero daemon install-service`:** write and enable a systemd *user* unit
   (`~/.config/systemd/user/kerod.service`), plus `loginctl enable-linger` so
   it survives logout.
   - Remove the service with `kero daemon uninstall-service`.
4. **Release artifacts:** the release workflow already builds linux
   amd64/arm64 `.deb`s and `install.sh` from the same tags as the other
   platforms. Add a server flavour beside them:
   - a tarball with the daemon-only `kero` and `libghostty-vt.so`, and no GTK
     dependency;
   - `install.sh --server`, which installs that build and runs
     `kero daemon install-service`.

   Arch packages and a Nix flake come later, on demand.
5. **Fixes that matter once a box has several users:**
   - Set the umask before `listen` instead of chmodding after.
   - Check the peer's uid with `SO_PEERCRED` and reject other users.
   - Look up the shell through NSS (`getent passwd`) instead of parsing
     `/etc/passwd`.

## Phase 2: remote access over the network, one user

The app connects to a kerod on another machine that runs as the same person.

### Transport: HTTP, with Tailscale saying who connects

kerod gets network listeners beside its Unix socket, off by default. As in
Rex, there's no SSH: the connection is HTTP(S), and the network says who the
caller is.

- **One connection per client, multiplexed.** Locally, the app opens one
  socket for control and one per visible pane. Over a network, that would
  mean a handshake per pane and per reconnect.
  - Instead, the client opens one HTTP connection and carries the existing
    control JSON and attach frames as streams in it: WebSockets, or HTTP/2
    streams. A spike picks one.
  - The Unix socket keeps today's protocol.
- **Why HTTP:** Tailscale Serve proxies HTTPS to a local socket, so an HTTP
  transport works with every Tailscale setup below. HTTP also passes
  corporate proxies, and it's what an iOS client would speak.
- **Why not SSH:**
  - It needs a process and a channel per connection.
  - It renegotiates on every reconnect, which happens each time a laptop
    wakes.
  - It depends on each user's `~/.ssh/config`.
  - It can't sit behind Tailscale Serve, and it gives no path to iOS.

  SSH keys may come back later as one way to authenticate (Phase 4), not as
  the transport.

### Listeners

Each listener lets in only callers it can identify:

| Listener | Needs | Identity from |
| --- | --- | --- |
| Built-in Tailscale (`tsnet` in kerod) | A Tailscale account; the machine needs no Tailscale | `WhoIs` on the embedded node |
| Tailscale Serve, to a local socket | Tailscale on the machine | The headers Serve adds for tailnet users |
| Tailnet address, such as `tailnet://:<port>` | Tailscale on the machine | `whois` on the local tailscaled API |

- By default, only the Tailscale user who owns the machine gets in. An
  allowlist, or a flag, lets in other tailnet members.
- Serve doesn't identify tagged devices, such as CI runners. They use a
  tailnet address instead.
- No anonymous listener at first. Rex has one behind `--allow-anonymous` for
  people who bring their own VPN. Wait until someone asks.
- Tailscale's ACLs do the network-level restriction. Kero doesn't need its own
  rules for that.

### Protocol changes

- `Hello` negotiates **compression** for attach frames (zstd, or deflate from
  the standard library). Rendered diffs compress well, and they cost bandwidth
  only on scroll-heavy output.
- **Version skew:** today a client refuses a kerod of another
  `proto.Version` and offers to restart it. That's fine locally, but a remote
  kerod can't be restarted quietly, because it may hold someone's
  long-running agents.
  - Show it per host, with "Update Kero on <host>". That has kerod fetch the
    matching release, using the updater the app already has, then restart
    when the user confirms.
  - Kero is released now, so hosts will run older versions than the app for
    days at a time. Decide whether the app speaks the previous
    `proto.Version` too, or only offers the update. Either way, the UI has to
    explain the mismatch rather than fail.
- **Heartbeats** on the connection, so a dead link shows as "reconnecting"
  within seconds, and the app reattaches when it comes back.

### App changes

- **Hosts:**
  - Settings › Hosts lists remote kerods by address, such as
    `https://build.<tailnet>.ts.net`.
  - In the config file: `host = name=address`.
  - The sidebar groups projects by host, with the local machine first and
    unlabeled. Each host has its own connection state.
- **Open Project on a host:** a folder picker served by a new `fs.list` RPC.
  The NSOpenPanel stays for the local machine.
- **⌘-click a path in a remote pane:** resolve it on the host (`fs.stat`).
  Open it there in `$EDITOR` in a new pane, or copy the path. Never open a
  local file with the same name.
- **Image paste into a remote pane:** upload the image to a temp dir on the
  host (`paste.file`), then paste the *remote* path, so TUIs that take image
  paths keep working.
- **Settings split:**
  - Appearance and keyboard come from the app.
  - Shell, `env`, scrollback and `restore-sessions` come from the host's own
    config, since kerod reads them there.
  - Settings shows which is which, and `kero config` edits the host's.

### CLI and agents

- `kero -H <host> ls` and `KERO_HOST` drive a remote kerod over the same
  transport. `kero hosts add|ls|remove` saves addresses.
- Inside a remote pane, `kero` already talks to the remote kerod through
  `KERO_SOCKET`, so the skill works there unchanged. `kero skill setup` needs
  running on the host.
- **Remote Control from a remote agent:** `kero focus` and `kero do` reach the
  local app, which is a new kind of trust (code on a server driving my
  laptop's UI). Add a per-host toggle, off by default for remote hosts.

## Phase 3: multi-user servers

Rex has three server modes:

- **Personal:** one kerod per user.
- **Shared:** one service account that a group connects to.
- **Login:** many OS users, with one front door that routes each to their own
  account.

For Kero:

- **Personal is multi-user-safe locally once Phase 1's fixes land.** Each user
  has their own kerod, `~/.config/kero` at 0700, a 0600 socket and a uid
  check.
- **Login server**, for reaching several accounts on one machine over the
  network: a root-owned `kero gateway` (a systemd system service). It:
  - holds the network listener, in place of each user's kerod;
  - identifies the caller through Tailscale (Phase 2);
  - maps the caller to the OS accounts they may use, through an accounts
    file, such as `alice@example.com` to `alice`, and to `root` only when the
    file says so;
  - starts that account's kerod through PAM, so limits, `nologin` and session
    setup apply as at a console login (for example with
    `systemd-run --uid=<user> --user`);
  - relays the connection to that account's socket.

  It's the only piece that runs privileged, so keep it small and audit it.
  The client picks an account as `https://alice@host`.
- **Shared server** (several people's programs running as one `kero` account)
  is the weakest model: everyone can read everyone's sessions. Skip it unless
  someone asks. A shared session for pairing is a different feature, with its
  own design.

## Phase 4: Kero's own authentication

For networks without Tailscale, and for the iOS client.

- **Pair a device with a host once**, from the host's CLI or an
  already-trusted client. The device gets a key. kerod, or the gateway, keeps
  a list of trusted keys per account, which `kero devices` lists and revokes.
- **Existing SSH keys as one more way to log in:** a client proves it holds a
  key listed in the account's `~/.ssh/authorized_keys`. That's authentication
  only, still over the HTTP transport. Rex plans the same.
- **TLS:** automatic through Tailscale. Without it, use a self-signed
  certificate pinned on first connect, or the user's own. Plain unencrypted
  HTTP is never offered for an authenticated listener.

## Security notes

- **A connection can do anything its account can,** including starting
  programs. Every listener is off by default, and every one identifies its
  callers.
- **Remote panes are untrusted, as local ones are.** OSC 52 clipboard reads
  keep their per-pane `ask` policy, and OSC 7501 text is still neutralized.
  - A remote host must not be able to pose as a local pane: show the host's
    name on its notifications and in the sidebar.
- **Remote Control** is per host and off by default for remote hosts (above).
- **Identify first:** listeners and the gateway identify the caller before
  they parse any `Hello` past its version.
- **Seeing who's connected:** `kero clients` on a host lists who is connected,
  with each one's identity.

## Order and rough size

1. **Phase 1, headless Linux kerod:** small to medium. Mostly packaging and
   CI, plus the socket and uid fixes. Worth doing on its own.
2. **Phase 2, network transport and Tailscale:** large.
   - The multiplexed HTTP transport, and the three Tailscale listeners.
   - Hosts in the app, compression and reconnects.
   - The `fs.*` and `paste.file` RPCs, and the settings split.
3. **Phase 3, login gateway:** medium. The accounts file, PAM, and starting
   and relaying to each account's kerod.
4. **Phase 4, own authentication:** medium. Device keys first, then SSH keys.

## Open questions

- Does `kero __kerod` from the Linux `.deb` start on a machine with no
  display, given that it links GTK? Try it on a fresh server VM. Even if it
  starts, needing GTK installed argues for the daemon-only build.
- **WebSockets or HTTP/2 streams** for the multiplexed transport? Check what
  passes Tailscale Serve, and what an iOS client can speak, before choosing.
- **Built-in tsnet or the machine's Tailscale?**
  - Built-in tsnet needs nothing installed on the server, but it adds size to
    kerod and a node per server.
  - The machine's Tailscale is lighter, but it's one more thing to have set
    up.
  - Rex offers both. Start with whichever the spike finds simpler, and add
    the other on demand.
- How does input latency feel over a slow link? kerod renders every frame, so
  a typed key needs a round trip before it shows. Measure before considering
  local echo prediction.
- Windows hosts: Kero ships for Windows and kerod runs there with ConPTY, and
  the listeners don't depend on the OS. Are there users who'd run one?
- Should a remote session show the host's own terminal theme, or always the
  app's? Today the app styles the terminal, so it would be the app's.
