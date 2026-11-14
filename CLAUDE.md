# Dux Mail (`dux-mail`) — project rules

## Language policy (issue #1 — do not regress)

**English is the source language for EVERY user-visible string**: QML UI,
daemon desktop notifications, CLI output, MCP descriptions, OAuth callback pages, README and
docs. Spanish, Portuguese (and any future language) are overrides, never
the source. Locales are narrowed to their base language, so `pt_BR` and
`pt_PT` share `pt` (worded as Brazilian Portuguese).

- Go (daemon/CLI): wrap the English string in `i18n.T(...)` from
  `core/internal/i18n` and add the override to **every** language table
  there. Locale comes from `DMAIL_LANG` → `LC_ALL` → `LC_MESSAGES` →
  `LANG`.
- QML: use `I18n.tr("English text")` and add the override to **every**
  `quickshell/translations/<lang>.json` (`en.json` stays empty — English
  IS the source string).
- DMS plugin (`dms-plugin/`): use `I18n.trFor("dankmailUnread", "English text")`
  and keep `dms-plugin/translations/es.json` 1-to-1 with QML call sites
  (internal plugin ID stays `"dankmailUnread"` for seamless DMS settings compatibility).
- `make -C core test` runs `scripts/check-i18n.sh`, which fails on
  translated text in Go/QML string literals outside those layers, and
  on any locale file that has drifted from the `I18n.tr()` call sites in
  either direction. `TestOverridesCoverSameKeys` does the same for the Go
  tables. CI enforces both.
- Translated text in code comments and test data is fine.

## Compatibility invariants (`dankmail` → `dux-mail`)

- Binary & systemd unit stay `dmail` / `dmail.service`.
- Primary runtime paths use `duxmail` (`duxmail.sock`, `~/.config/duxmail`,
  `~/.local/share/duxmail`, `~/.cache/duxmail`, keyring service `duxmail`),
  automatically falling back to existing `dankmail` directories/keyring entries
  and creating a `dankmail.sock -> duxmail.sock` compatibility symlink.

## Build & test

- Go core + i18n + JS/plugin tests: `make -C core build|vet|test`.
- Headless MCP server: `dmail mcp [--mode full|draft|draft-strict|readonly]`.
- The AUR release flow updates `packaging/aur/{dux-mail,dux-mail-git,dms-shell-plugin-dux-mail}` and tags `vX.Y.Z`.
