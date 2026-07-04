# dankmail

> Multi-account mail **notifier with triage** for Linux — a Go daemon
> plus a Quickshell/QML UI in the DankMaterialShell aesthetic,
> architecturally modeled on
> [dankcalendar](https://github.com/AvengeMedia/dankcalendar).
> Functionally inspired by Checker Plus for Gmail, but native,
> browser-independent, and with generic IMAP support.

**Status: working (ring 1).** Gmail accounts sync, notify and triage
end to end. Generic IMAP accounts can be added and verified (their
sync engine lands in ring 2). See the
[architecture](docs/architecture.md) and the delivery rings for the
roadmap.

## What it is / isn't

Triage only: read, star, archive, trash, snooze, plain-text preview
with quote formatting. No attachments, no HTML rendering, no folder
management — those open your webmail via deep link. "Delete" always
means *move to trash*; permanent deletion is not implemented, by
design.

- **Daemon first**: runs as a systemd user service; closing the window
  never stops sync or notifications, and `dmail show` brings it back.
- **Native notifications** with configurable inline actions
  (read / archive / trash / snooze / open in web).
- **Minimal OAuth scopes** for Gmail (`gmail.modify` + `gmail.send`,
  never full mailbox access), with a built-in wizard that walks you
  through creating your own OAuth client — or just feed it the
  downloaded `client_secret_*.json`.
- **Secrets in the system keyring** (tokens, passwords, your OAuth
  client). Never in files.
- **Scriptable**: unix-socket IPC + `dmail` CLI
  (`status`, `list --json`, `sync`, `dnd`, `toggle`, …) and a
  localhost HTTP API for widgets and bars.

## Install

From source (Arch/CachyOS-friendly; AUR package planned):

```sh
git clone https://github.com/arqueon/dankmail && cd dankmail
make build
make install PREFIX=~/.local          # or sudo make install
make install-systemd PREFIX=~/.local
systemctl --user enable --now dmail
```

Requirements: Go ≥ 1.22 to build; [Quickshell](https://quickshell.org)
for the UI; a Secret Service keyring and a notification daemon in your
session (DankMaterialShell covers both).

## Accounts

- **Gmail**: tray → Open Dank Mail → add-account button. The wizard
  serves the step-by-step Google Cloud setup with direct links, then
  runs the OAuth consent. CLI: `dmail account add-gmail
  --client-json ~/Downloads/client_secret_*.json`.
- **IMAP** (iCloud, Yahoo, Fastmail, Proton via Bridge, custom):
  presets in the same wizard; the connection is tested before anything
  is stored. CLI: `dmail account add-imap you@icloud.com --preset icloud`.

See [docs/gmail-setup.md](docs/gmail-setup.md) and
[docs/design/providers-roadmap.md](docs/design/providers-roadmap.md)
(Microsoft via Graph API is planned; Proton works through Bridge).

## Layout

- `core/` — Go daemon: `cmd/dmail` (CLI), `internal/` (providers, sync
  queue with optimistic UI, notify, ipc, oauth, rules), `api/`
  (localhost HTTP), `ent/` (SQLite schema).
- `quickshell/` — QML UI: triage window, account wizard, tray;
  es + en translations.
- `assets/` — desktop entry and systemd user unit.

Note on snooze: it is simulated locally (archive + scheduled wake), so
it won't show in Gmail's own "Snoozed" view and waking requires the
daemon to be running.

## License

MIT — see [LICENSE](LICENSE). UI infrastructure adapted from
dankcalendar (MIT, Avenge Media LLC); see `quickshell/NOTICE`.
