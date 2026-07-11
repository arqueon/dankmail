# dankmail — project rules

## Language policy (issue #1 — do not regress)

**English is the source language for EVERY user-visible string**: QML UI,
daemon desktop notifications, CLI output, OAuth callback pages, README and
docs. Spanish (and any future language) is an override, never the source.

- Go (daemon/CLI): wrap the English string in `i18n.T(...)` from
  `core/internal/i18n` and add the Spanish override there. Locale comes
  from `DMAIL_LANG` → `LC_ALL` → `LC_MESSAGES` → `LANG`.
- QML: use `I18n.tr("English text")` and add the override to
  `quickshell/translations/es.json` (`en.json` stays empty — English IS
  the source string).
- `make -C core test` runs `scripts/check-i18n.sh`, which fails on Spanish
  in Go/QML string literals outside those two layers. CI enforces it.
- Spanish in code comments and test data is fine.

## Build & test

- Go core: `make -C core build|test|vet` (test includes check-i18n).
- The AUR release flow bumps `packaging/` and tags `vX.Y.Z`.
