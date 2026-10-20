# Changelog

Shibu Code personal-fork releases are tracked in
[docs/changelog.md](docs/changelog.md). The upstream Kero history follows.

## 2.0.7

- A split pane's title leads with its program's logo: Claude Code's,
  Codex's and other agents', Node.js's for a script node runs, as vite,
  and those of bun, deno, python, vim, git and docker; a shell's is a
  bolder terminal icon.
- A split pane shows its status once, by its title, not again in the
  chip beside it.
- An unnamed session takes its name from the program in the pane you're
  in, else in the one that most needs you; `session-name` chooses
  between these and the first pane's folder.
- Clear Scrollback in a pane's context menu clears as Cmd+K does, the
  screen too, and works on a full-screen program's.

## 2.0.6

- Images programs show appear in their panes, as grok's preview of a
  pasted image, through the Kitty graphics protocol, as in Ghostty.
- Command-hovering an image's path in a pane previews the image.
- Files dropped on a pane paste their paths, escaped for the shell, as in
  Ghostty: an agent takes a dropped image as a file it attaches.
- The mouse lands where it points in programs that take it in pixels:
  Claude Code's "Jump to bottom" lights up and takes a click, and dragging
  selects its text again.
- Programs asking the terminal's colors get the theme's: Copilot CLI's
  input box is light on a light theme. Colors a program sets for the
  terminal show, and programs that follow the appearance, as Claude Code
  and Copilot CLI, change with it when it goes from light to dark.
- Box drawing and blocks keep their colors under `minimum-contrast`, and
  only rows a program fills extend into a pane's padding, as in Ghostty:
  Copilot CLI's input box and tabs draw as they should.
- A name an agent gave its session with `kero rename` goes when a new
  conversation starts in the session's first pane.
- After updating, Kero asks to restart kerod, whose sessions restore as
  after a restart of the Mac.

## 2.0.5

- Find in a pane's scrollback with Cmd+F (Ctrl+F on Windows).
- Text stays readable on a light background: colors meant for a dark one,
  as Claude Code's near-white grays, get enough contrast with it. The new
  `minimum-contrast` setting sets how much, as in Ghostty.
- Command-hovering a link in a terminal underlines it.
- A pane shows where its agent works: the worktree Claude Code went into,
  rather than the folder its shell started in. Links, Copy Directory and
  Reveal follow it.
- Codex sessions are told to the pane that runs them, for resuming after
  kerod restarts, rather than to the pane that first started Codex's
  background service.

## 2.0.4

- Terminals show the cursor chosen in Settings, its shape and whether it
  blinks, rather than always a blinking block.

## 2.0.3

- An agent sitting at its prompt no longer shows as needing you, as Claude
  Code did a while after answering. A bell or a program's desktop
  notification leaves its status alone; programs' notifications show as
  they are.

## 2.0.2

- Installing the command line tool on macOS asks for an administrator's
  password instead of failing.

## 2.0.1

- Kero speaks Simplified Chinese; pick the language in Settings.
- Rename a project from its menu in the sidebar.
- `kero skill setup` hooks Claude Code, Codex, Pi and OpenCode into Kero as
  they start: an agent learns its session and pane, names an unnamed session
  after its task, and resumes when kerod restarts.
- `kero new|split --wait` and `kero wait --exit` exit with the command's
  status.
- When kerod runs another version of Kero, a Restart Service chip offers to
  restart it. Settings shows updates on its General page.
- A home directory pane shows `~` once in its header.

## 2.0.0

Kero, rewritten from the ground up: sessions grouped by project in a sidebar
that shows which are working, which need you and which are done. Sessions run
in kerod, a daemon of their own, so quitting Kero leaves them running.

- Runs on macOS, Windows and Linux.
- The `kero` CLI drives sessions from a shell or an agent.
- Updates itself from Kero's releases.
