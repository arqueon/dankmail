# dankmail — project rules

## Language policy (issue #1 — do not regress)

**English is the source language for EVERY user-visible string**: QML UI,
daemon desktop notifications, CLI output, OAuth callback pages, README and
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
- `make -C core test` runs `scripts/check-i18n.sh`, which fails on
  translated text in Go/QML string literals outside those two layers, and
  on any locale file that has drifted from the `I18n.tr()` call sites in
  either direction. `TestOverridesCoverSameKeys` does the same for the Go
  tables. CI enforces both.
- Translated text in code comments and test data is fine.

## Build & test

- Go core: `make -C core build|test|vet` (test includes check-i18n).
- The AUR release flow bumps `packaging/` and tags `vX.Y.Z`.
