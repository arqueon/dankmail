# dankmail

> Working title. Multi-account mail **notifier with triage** for Linux —
> a Go daemon plus a Quickshell/QML UI in the DankMaterialShell
> aesthetic, architecturally modeled on
> [dankcalendar](https://github.com/AvengeMedia/dankcalendar).
> Functionally inspired by Checker Plus for Gmail, but native,
> browser-independent, and with generic IMAP support.

**Status: pre-alpha skeleton.** The provider contract, Ent schema, and
repo layout exist; no provider is implemented yet. See
[docs/architecture.md](docs/architecture.md) and the delivery rings
there for the plan.

## What it is / isn't

Triage only: read, star, archive, trash, snooze, plain-text quick
reply. No attachments, no HTML rendering, no folder management — those
open your webmail via deep link. "Delete" always means *move to trash*;
permanent deletion is not implemented, by design. Secrets live in the
system keyring; the Gmail provider uses the minimal OAuth scopes
(`gmail.modify` + `gmail.send`), never full mailbox access.

## Build

```sh
make            # builds core/bin/dmail (CGO-free)
make test
make install    # binary, quickshell config, icon, desktop entry
make install-systemd && systemctl --user enable --now dmail
```

## Layout

- `core/` — Go daemon: `cmd/dmail` (CLI), `internal/` (providers, sync,
  queue, notify, ipc, oauth, rules), `api/` (localhost HTTP for the UI),
  `ent/` (SQLite schema), `repo/`.
- `quickshell/` — QML UI: `Modules/` (tray, triage popup), `Modals/`,
  `Services/` (daemon bridge), `Common/`, `translations/` (es, en).
- `assets/` — desktop entry and systemd user unit.

Note for the snooze feature: snoozing is local — Gmail's own "Snoozed"
view won't show it, and waking requires the daemon to be running.
